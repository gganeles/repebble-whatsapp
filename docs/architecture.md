# Repebble WhatsApp: Architecture

A small "WhatsApp Web" for the Pebble Time 2: browse recent chats, read messages, reply by voice dictation or canned replies.

```
┌──────────────────────── Android phone ────────────────────────┐
│                                                               │
│  Termux                              Pebble mobile app        │
│  ┌──────────────────────┐  HTTP +    ┌─────────────────────┐  │   Bluetooth   ┌───────────────┐
│  │ pebblewa (Go)        │  WebSocket │ PebbleKit JS        │  │  AppMessage   │ Watchapp (C)  │
│  │  whatsmeow client    │◄──────────►│  (pkjs/index.js)    │◄─┼──────────────►│ Pebble Time 2 │
│  │  SQLite msg store    │ 127.0.0.1  │  cache + JID map    │  │               │ (emery)       │
│  │  REST/WS API         │   :8723    └─────────────────────┘  │               └───────────────┘
│  └──────────┬───────────┘                                     │
└─────────────┼─────────────────────────────────────────────────┘
              │ WhatsApp multi-device protocol (linked device)
              ▼
        WhatsApp servers
```

## Responsibilities

| Layer | Owns | Does not do |
|---|---|---|
| **pebblewa** (Go, Termux) | WhatsApp session, pairing, persistent message history, contact/group names, text sanitizing for the watch (truncation, emoji), canned reply list | Anything Pebble-specific beyond producing watch-safe strings |
| **PebbleKit JS** | Translating watch commands to HTTP calls, the AppMessage send queue, mapping long JIDs to short numeric handles, settings page (server URL, token, replies) | Business logic or long-term storage |
| **Watchapp** (C) | UI: chat list, conversation view, reply menu, dictation, pairing screen | Holding more than one page of chats and one conversation in RAM |

Rule of thumb: the further from the watch, the more work happens. The server does all formatting so JS stays thin and the watch only renders.

## Key decisions

1. **Server owns message history.** whatsmeow does not persist messages, only the device/session store. pebblewa writes every incoming/outgoing message (live events plus the initial `HistorySync`) into its own SQLite tables. Use `modernc.org/sqlite` (pure Go, no cgo) so it builds cleanly in Termux; one DB file holds both the whatsmeow store and our tables.
2. **Localhost only, plus a token.** Server binds to `127.0.0.1:8723`. Every request carries `Authorization: Bearer <token>`; the token is generated on first run, printed in Termux, and pasted into the watchapp's settings page. This stops other apps on the phone from reading WhatsApp.
3. **Pull while open, live events via WebSocket.** PebbleKit JS only runs while the watchapp is open, so it opens a WebSocket on launch for new-message events and closes it on exit. When the app is closed, the user still gets normal WhatsApp notifications from the phone; we do not build our own notification path in v1.
4. **Short handles instead of JIDs on the watch.** JIDs (`4915123456789@s.whatsapp.net`, group JIDs longer still) waste watch RAM and AppMessage bytes. JS assigns a `uint16` handle per chat and per message for the session and keeps the map.
5. **Watch-safe text is produced server-side.** Server truncates on UTF-8 boundaries, replaces emoji the Pebble fonts can't draw with a short `:name:` or drops them (configurable), and turns media into placeholders like `[Photo]`, `[Voice note 0:12]`, `[Sticker]`.
6. **Text-only replies in v1.** Replies are plain text sent to the chat. Quoting a specific message is a stretch goal.

## Pairing (auth)

The WhatsApp phone and the Termux phone are usually the *same* phone, so scanning a QR code is awkward (you'd need to scan your own screen). Primary path is **phone-number pairing code** (whatsmeow `Client.PairPhone`):

1. First run: `pebblewa` starts with no session, state `unpaired`.
2. User enters their phone number either in Termux (`pebblewa pair +49...`) or in the watchapp settings page.
3. Server calls `PairPhone`, gets an 8-character code, exposes it via `/v1/status`.
4. Watch shows the code in large text (and Termux prints it). User opens WhatsApp → Linked devices → Link with phone number instead → types the code.
5. whatsmeow emits `PairSuccess`, state becomes `connected`, history sync begins.

QR fallback: the server also renders the QR as text in Termux and exposes the raw QR string; the watch can draw it (200×228 screen is enough for a version-3 QR) for pairing from a *second* phone. Low priority.

## Watch UI

- **Pairing / status window**: shown when the server is unreachable, unpaired, or pairing. Clear message for "Termux server not running".
- **Chat list**: `MenuLayer`. Row: name (bold), last-message preview, relative time, unread badge. Up/Down scroll, Select opens, scrolling to the bottom requests the next page.
- **Conversation**: newest at bottom, scrollable, bubbles left/right with sender name in groups. Opening it marks the chat read. Scrolling to top loads older messages.
- **Reply**: Select in a conversation opens an `ActionMenu`: 🎤 Dictate, then the canned replies. Dictation uses `dictation_session_create` with confirmation enabled; result is sent as-is. Long-press Select could jump straight to dictation.
- **Send feedback**: optimistic bubble marked "sending…", updated on `SEND_RESULT`.

## Memory budget (watch)

Design for a 64 KB-class app heap even if emery turns out larger; verify with `heap_bytes_free()` on device.

| Data | Limit | Approx. RAM |
|---|---|---|
| Chat list in RAM | 20 rows (2 pages of 10) | 20 × ~110 B ≈ 2.2 KB |
| Conversation in RAM | 20 messages, older evicted when paging | 20 × ≤ 300 B ≈ 6 KB |
| Canned replies | 8 × ≤ 40 B | 320 B (also in persist storage) |

Exact per-field limits are in [protocol.md](protocol.md#limits).

## Running in Termux

- `pkg install golang git` then `go build ./cmd/pebblewa`, or download a prebuilt `android/arm64` binary from releases.
- `termux-wake-lock` while running; recommend exempting Termux from battery optimization.
- Optional `Termux:Boot` script (`scripts/termux-boot/start-pebblewa`) so it starts after reboot.
- Data dir: `~/.local/share/pebblewa/` (`wa.db`, `token`, `config.toml`).

## Risks to check first (spikes)

1. **Cleartext localhost from PebbleKit JS.** The current Pebble Android app must allow `http://127.0.0.1` XHR and `ws://` from pkjs. If its network security config blocks cleartext, we need a fallback (e.g. the server exposing TLS with a cert the app trusts, which is harder). Test this before anything else with a 20-line Go server and a hello-world watchapp.
2. **Dictation availability** in the Pebble app version Gabe uses on Android. If unavailable, canned replies still work and dictation shows a clear error.
3. **Termux being killed** in the background by Android. Mitigated by wake lock and battery exemption; JS shows a clear "server not running" state.
4. **WhatsApp account risk.** whatsmeow is an unofficial client; linked-device usage at normal personal volume is generally fine, but bans are possible. Worth accepting knowingly.
5. **AppMessage size and emery heap.** Check `app_message_inbox_size_maximum()` and free heap on the real watch and tune page sizes.

## Milestones

1. **Spike**: localhost XHR + WebSocket from pkjs on the real phone; dictation hello-world.
2. **Server MVP**: pairing code, connect, persist messages, `GET /chats`, `GET /messages`, `POST /messages`.
3. **Watch read-only**: status window, chat list, conversation view, paging.
4. **Replies**: canned replies, dictation, send feedback, mark read.
5. **Live updates**: WebSocket events, new message refresh, vibrate on message in the open chat.
6. **Polish**: settings page (Clay), emoji handling, Termux:Boot, release binaries.

## Starter project layout

```
repebble-whatsapp/
├── README.md
├── server/                         # Go module: github.com/<owner>/repebble-whatsapp/server
│   ├── go.mod
│   ├── cmd/pebblewa/main.go        # flags, data dir, wiring, `pair` subcommand
│   └── internal/
│       ├── wa/                     # whatsmeow client: connect, pair, event handler, send
│       │   ├── client.go
│       │   ├── events.go
│       │   └── history.go          # HistorySync → store
│       ├── store/                  # SQLite: chats, messages, contacts
│       │   ├── store.go
│       │   └── schema.sql
│       ├── api/                    # HTTP + WebSocket, auth middleware
│       │   ├── server.go
│       │   ├── handlers.go
│       │   └── events_ws.go
│       └── watchtext/              # UTF-8-safe truncation, emoji mapping, media placeholders
│           └── watchtext.go
├── watchapp/                       # Pebble SDK project (targets: emery, basalt for emulator)
│   ├── package.json                # messageKeys, capabilities: ["configurable"]
│   ├── wscript
│   ├── resources/
│   └── src/
│       ├── c/
│       │   ├── main.c
│       │   ├── comms.c / comms.h           # AppMessage in/out, request ids
│       │   ├── model.c / model.h           # chat list + conversation buffers
│       │   ├── status_window.c             # pairing / errors
│       │   ├── chat_list_window.c
│       │   ├── conversation_window.c
│       │   └── reply_menu.c                # ActionMenu + dictation
│       └── pkjs/
│           ├── index.js                    # appmessage + ready handlers
│           ├── api.js                      # fetch/XHR + WebSocket client
│           ├── queue.js                    # AppMessage send queue with retry
│           ├── handles.js                  # JID ↔ uint16 maps
│           └── config.js                   # Clay settings: URL, token, replies
├── scripts/
│   ├── termux-install.sh
│   └── termux-boot/start-pebblewa
└── docs/
    ├── architecture.md
    └── protocol.md
```
