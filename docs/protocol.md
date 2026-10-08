# Repebble WhatsApp: Protocols

Two interfaces: **Server API** (pebblewa ↔ PebbleKit JS, HTTP/WebSocket on localhost) and **AppMessage schema** (PebbleKit JS ↔ watchapp, over Bluetooth). See [architecture.md](architecture.md) for the big picture.

## 1. Server API (pebblewa ↔ PebbleKit JS)

Base URL `http://127.0.0.1:8723/v1`. JSON everywhere. Every request needs `Authorization: Bearer <token>` (WebSocket: `?token=` query param, since pkjs WebSocket can't set headers). All text fields arrive already watch-safe (truncated, emoji handled), using the limits in [§3](#limits).

Errors: non-2xx with `{"error": {"code": "not_paired" | "bad_request" | "not_found" | "wa_disconnected" | "send_failed", "message": "..."}}`.

### Status and pairing

`GET /status`
```json
{ "state": "unpaired | pairing | connecting | connected | logged_out",
  "phone": "+491512345678", "push_name": "Gabe",
  "pairing_code": "ABCD-EFGH", "qr": null, "history_sync": "running | done" }
```

`POST /pair` `{ "phone": "+491512345678" }` → `{ "pairing_code": "ABCD-EFGH" }`
Starts phone-number pairing. Code expires after ~3 minutes; poll `/status` or watch the WS `connection` event.

`POST /logout` → `204`. Unlinks the device.

### Chats

`GET /chats?limit=10&before=<cursor>`
```json
{ "chats": [
    { "jid": "491512345678@s.whatsapp.net", "name": "Alice",
      "preview": "Alice: see you at 8?", "ts": 1791489600,
      "unread": 2, "group": false, "muted": false, "from_me": false } ],
  "next": "opaque-cursor-or-null" }
```
Sorted by last-message time, newest first. Archived chats excluded by default (`?archived=1` to include). `limit` max 20.

### Messages

`GET /chats/{jid}/messages?limit=15&before=<message_id>`
```json
{ "messages": [
    { "id": "3EB0C767D26A1F4C2B41", "from_me": false, "sender": "Alice",
      "text": "see you at 8?", "ts": 1791489600,
      "kind": "text | image | video | audio | voice | sticker | document | location | contact | other",
      "status": "pending | sent | delivered | read" } ],
  "next": "3EB0...older-id-or-null" }
```
Oldest-first within the page, so the client can append. `before` pages backwards. `limit` max 30. `sender` is only set in groups. Non-text kinds carry a placeholder `text` such as `[Photo] caption`.

Each message may also carry `"truncated": true` (text was cut to the 320-byte list limit) and `"has_image": true` (a picture can be fetched).

`GET /chats/{jid}/messages/{id}` → `{ "message": { ...same fields... } }` with the full text (cleaned, capped at 4000 bytes).

`GET /chats/{jid}/messages/{id}/image?w=192&h=192&format=color|bw`
```json
{ "width": 192, "height": 144, "format": "color", "row_bytes": 192, "data": "<base64>" }
```
The picture (full download for images and stickers, poster frame for videos, embedded thumbnail as a fallback) scaled to fit `w`×`h` (max 200×228, never upscaled) and Floyd–Steinberg dithered. `color` is one byte per pixel in Pebble `GColor8` layout (`0b11RRGGBB`); `bw` is one bit per pixel, least significant bit leftmost, 1 = white, rows padded to whole bytes. Rows are tightly packed (`row_bytes`); the watch copies them into its own bitmap stride. Base64 in JSON because PebbleKit JS can't be relied on for binary responses.

`POST /chats/{jid}/messages` `{ "text": "On my way", "client_id": "k7" }` → `{ "id": "3EB0...", "ts": 1791489700, "client_id": "k7" }`
`client_id` makes retries idempotent (server remembers the last 100).

`POST /chats/{jid}/read` `{ "up_to": "<message_id>" }` → `204`. Sends read receipts and clears `unread`.

### Canned replies

`GET /replies` → `{ "replies": ["On my way", "Can't talk now", "👍", "Call you later"] }`
`PUT /replies` same shape. Stored server-side so they survive reinstalling the watchapp; JS can also override from its settings page.

### Live events

`GET /events` (WebSocket upgrade). Server → client only, one JSON object per frame:
```json
{ "type": "message",    "chat": "<jid>", "message": { ...same as above... } }
{ "type": "chat",       "chat": { ...same as chat list item... } }
{ "type": "receipt",    "chat": "<jid>", "ids": ["..."], "status": "read" }
{ "type": "connection", "state": "connected" }
```
Server sends a ping every 25 s; JS reconnects with backoff (1, 2, 5, 10 s). If `WebSocket` is missing in pkjs or fails to open 3 times in a row, JS falls back to polling `/chats` (and the open chat's messages) every 15 s.

## 2. AppMessage schema (PebbleKit JS ↔ watchapp)

### Message keys (`package.json` → `messageKeys`)

| Key | Type | Notes |
|---|---|---|
| `CMD` | uint8 | Command code, present on every message |
| `REQ` | uint8 | Request id from the watch, echoed in responses (0 for unsolicited) |
| `STATE` | uint8 | Server state: 0 unreachable, 1 unpaired, 2 pairing, 3 connecting, 4 connected, 5 logged out, 6 not configured (no/bad token) |
| `CHAT` | uint16 | Chat handle (JS-assigned, maps to JID) |
| `MSG` | uint16 | Message handle (JS-assigned, maps to message id) |
| `INDEX` | uint8 | Position of this item in the response |
| `NAME` | cstring | Chat name or group sender |
| `TEXT` | cstring | Preview, message body, reply text, pairing code, or error text |
| `TS` | int32 | Unix seconds |
| `UNREAD` | uint8 | Capped at 99 |
| `FLAGS` | uint8 | bit0 group (chats) / has picture (messages), bit1 muted (chats) / text truncated (messages), bit2 from_me, bit3 has_more, bit4 media, bits5-6 status (0 pending,1 sent,2 delivered,3 read) |
| `WIDTH`, `HEIGHT` | uint8/uint16 | Requested max picture size (watch → JS) or actual size (JS → watch) |
| `FORMAT` | uint8 | 0 colour (`GColor8`), 1 black and white (1 bit) |
| `STRIDE` | uint16 | Bytes per packed source row in `IMAGE_CHUNK` data |
| `OFFSET`, `TOTAL` | uint32 | Byte offset of this chunk, total bytes of the transfer |
| `DATA` | byte array | Chunk payload, at most 1500 bytes |
| `ERR` | uint8 | 0 ok, 1 server unreachable, 2 not paired, 3 not found, 4 send failed, 5 bad token, 6 other |

### Commands: watch → JS

| CMD | Name | Payload | JS does |
|---|---|---|---|
| 1 | `GET_STATUS` | | `GET /status` |
| 2 | `GET_CHATS` | `INDEX` = page (0 = first) | `GET /chats` (cursor kept in JS) |
| 3 | `GET_MESSAGES` | `CHAT`, optional `MSG` = load older than this | `GET /chats/{jid}/messages` |
| 4 | `SEND_TEXT` | `CHAT`, `TEXT` | `POST /chats/{jid}/messages` |
| 5 | `MARK_READ` | `CHAT`, `MSG` | `POST /chats/{jid}/read` |
| 6 | `GET_REPLIES` | | `GET /replies` |
| 7 | `CLOSE_CHAT` | `CHAT` | Stops forwarding live events for that chat |
| 9 | `GET_FULL_TEXT` | `CHAT`, `MSG` | `GET /chats/{jid}/messages/{id}`, replies with `TEXT_CHUNK`s |
| 10 | `GET_IMAGE` | `CHAT`, `MSG`, `WIDTH`, `HEIGHT`, `FORMAT` | `GET .../image`, replies with `IMAGE_BEGIN`, `IMAGE_CHUNK`s, `IMAGE_END` |
| 8 | `PAIR` | | `POST /pair` with the number from the settings page; replies with `STATUS` (pairing + code) |

### Commands: JS → watch

| CMD | Name | Payload |
|---|---|---|
| 64 | `STATUS` | `STATE`, `TEXT` (pairing code or detail), `ERR` |
| 65 | `CHAT_ITEM` | `REQ`, `INDEX`, `CHAT`, `NAME`, `TEXT` (preview), `TS`, `UNREAD`, `FLAGS` |
| 66 | `CHATS_END` | `REQ`, `FLAGS` (bit3 = more pages available) |
| 67 | `MSG_ITEM` | `REQ`, `INDEX`, `CHAT`, `MSG`, `NAME`, `TEXT`, `TS`, `FLAGS` |
| 68 | `MSGS_END` | `REQ`, `CHAT`, `FLAGS` (bit3 = older available) |
| 69 | `SEND_RESULT` | `REQ`, `CHAT`, `MSG`, `ERR` |
| 70 | `REPLY_ITEM` | `REQ`, `INDEX`, `TEXT` |
| 71 | `REPLIES_END` | `REQ` |
| 72 | `EVT_MESSAGE` | `CHAT`, `MSG`, `NAME`, `TEXT`, `TS`, `FLAGS` (only for the open chat) |
| 73 | `EVT_CHAT` | Same as `CHAT_ITEM` with `REQ`=0: watch moves or inserts the row |
| 74 | `ERROR` | `REQ`, `ERR`, `TEXT` |
| 75 | `TEXT_CHUNK` | `REQ`, `MSG`, `OFFSET`, `TOTAL`, `DATA` (UTF-8 bytes; no `DATA` when `TOTAL` is 0) |
| 76 | `IMAGE_BEGIN` | `REQ`, `MSG`, `WIDTH`, `HEIGHT`, `FORMAT`, `STRIDE`, `TOTAL` |
| 77 | `IMAGE_CHUNK` | `REQ`, `OFFSET`, `DATA` |
| 78 | `IMAGE_END` | `REQ`, `MSG` |

### Transport rules

- **Chunked transfers** (full text, pictures): the watch picks a picture size that fits its screen and free heap, allocates the buffer on the first chunk, and copies chunks in place. Only one detail view is open at a time, so JS drops queued chunks for older requests.
- **One item per AppMessage.** Each stays well under 1 KB, which avoids inbox-size surprises and lets the UI render progressively. Lists end with an explicit `*_END`.
- **JS send queue**: one outstanding `sendAppMessage` at a time; next one on ACK. On NACK retry up to 3 times with 100/300/1000 ms backoff, then drop and send `ERROR`.
- **Stale responses**: the watch ignores items whose `REQ` doesn't match the latest request for that screen (e.g. user backed out of a chat mid-load).
- **Watch inbox/outbox**: `app_message_open(2048, 512)`. Increase only if testing shows a need.
- **Startup**: on pkjs `ready`, JS sends `STATUS`. Watch sends `GET_CHATS` once state is connected.
- **Handles**: reset each time the watchapp launches. JS keeps `jid→handle` and `handle→jid` maps and the same for message ids within loaded chats.

## 3. Limits

All byte counts are UTF-8 bytes, including the NUL on the watch. The server enforces them; the watch truncates again defensively.

| Field | Max bytes | Notes |
|---|---|---|
| Chat name | 32 | |
| Chat preview | 64 | Prefixed with `You: ` or group sender's first name |
| Group sender name | 24 | |
| Message text (list) | 320 | Longer messages end with `…` and are flagged truncated |
| Message text (detail) | 4000 | Fetched on demand in chunks |
| Picture | 192×192 colour on the Time 2 (≈37 KB), smaller if the heap is tight | Fetched on demand |
| Canned reply | 40 | Max 8 replies |
| Dictated reply | 512 | Dictation buffer size |
| Chat page | 10 chats | Watch keeps at most 20 in RAM |
| Message page | 15 messages | Watch keeps at most 20; loading older evicts newest beyond 20 |

Emoji: the server maps a small table of common emoji (👍 ❤️ 😂 🙏 …) to Pebble-renderable text like `(y)` `<3` `:joy:` and drops the rest, unless the firmware fonts are confirmed to render them.
