# Repebble WhatsApp

A small WhatsApp client for the Pebble Time 2: browse recent chats, read full messages and pictures, and reply by voice dictation or quick replies.

```
pebblewa (Go, in Termux)  <-- HTTP/WebSocket on 127.0.0.1 -->  PebbleKit JS  <-- AppMessage -->  Watchapp (C)
```

- `server/`: **pebblewa**, a [whatsmeow](https://github.com/tulir/whatsmeow) bridge that links to WhatsApp as a companion device, keeps a local SQLite copy of chats and messages, and serves the API in [docs/protocol.md](docs/protocol.md).
- `watchapp/`: the Pebble app. `src/c` is the watch UI, `src/pkjs` is the phone-side bridge plus the settings page.
- `docs/`: [architecture](docs/architecture.md) and [protocol](docs/protocol.md).

## Setup on the phone (Termux)

```sh
pkg install git golang
git clone <this repo> && cd repebble-whatsapp
bash scripts/termux-install.sh       # builds pebblewa, prints your API token
termux-wake-lock
pebblewa -pair +491512345678         # your WhatsApp number, international format
```

pebblewa prints an 8-character code. In WhatsApp on the phone go to **Linked devices > Link a device > Link with phone number instead** and type it. After that just run `pebblewa`; it stays linked.

You can also pair from the watch: put your number in the watchapp settings, open the app, and press Select on the "Link WhatsApp" screen. The code shows on the watch.

Keep it running: exempt Termux from battery optimisation, and install **Termux:Boot** so `scripts/termux-boot/start-pebblewa` (copied to `~/.termux/boot/` by the installer) starts it after a reboot.

Flags: `-addr` (default `127.0.0.1:8723`), `-data` (default `~/.local/share/pebblewa`), `-keep-emoji` (send emoji to the watch as-is), `-log DEBUG`. `pebblewa token` prints the API token.

## Watchapp

```sh
cd watchapp
pebble build
pebble install --phone <phone-ip>    # or --emulator emery
```

Then open the app's settings in the Pebble phone app and paste the token. Quick replies can be edited there too (up to 8).

On the watch:
- **Chat list**: Up/Down to scroll (more chats load as you reach the bottom), Select to open, long-press Select to refresh.
- **Conversation**: newest at the bottom; scroll up to load older messages. Long messages show "more" and pictures show "view". Select opens a message; long-press Select replies straight away. A failed send shows "Failed. Select to retry".
- **Message**: the full text (up to 4000 bytes) and the picture, dithered to the watch's 64 colours. Up/Down scrolls, Select opens the reply menu: **Dictate** or a quick reply.

## Developing without WhatsApp

`pebblewa-demo` serves the same API with made-up chats, and contacts answer whatever you send after two seconds:

```sh
cd server && go run ./cmd/pebblewa-demo          # token "demo"
```

Tests:

```sh
cd server && go test ./...
cd watchapp && node test/bridge.test.js http://127.0.0.1:8723 demo          # pkjs against the demo server
cd watchapp && NO_WS=1 node test/bridge.test.js http://127.0.0.1:8723 demo  # polling fallback
```

## Known limits (v1)

- Text replies only. Photos and stickers can be viewed; videos show their poster frame; voice notes and documents show as placeholders like `[Voice note 0:12]`.
- In the conversation list messages are cut at 320 bytes; open one to read the rest (up to 4000 bytes).
- Pictures are fetched when you open a message, so expect a few seconds over Bluetooth (about 37 KB for a full-width colour picture).
- The watch keeps 30 chats and 20 messages of one conversation in memory.
- whatsmeow is an unofficial client. Normal personal use is generally fine, but WhatsApp can ban accounts that use unofficial clients.
