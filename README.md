# tgchat

Terminal UI for chatting with your Telegram account via MTProto.

## Build

    go build -o tgchat .

## Configure

Get `TG_APP_ID` and `TG_API_HASH` from https://my.telegram.org.

    set TG_APP_ID=12345
    set TG_API_HASH=your_api_hash
    tgchat.exe

First run prompts for phone, code, and (if enabled) 2FA password. Session is saved to
`%USERPROFILE%\.local\share\tgchat\session.json` (or `$TG_SESSION_DIR`). Subsequent
runs skip auth.

## Smoke test

1. Build: `go build -o tgchat .`
2. Set env vars (above).
3. Run `tgchat.exe`.
4. Enter phone, code from Telegram.
5. Sidebar shows dialogs. Use mouse or `/open <index>` to switch; Tab to focus input.
6. Type `/history` then Enter. Verify history appears.
7. Type a message and Enter. Verify it sends and appears on a second Telegram client.
8. Ctrl+C to quit.

## Key bindings

- `Ctrl+C` — quit
- `Tab` — cycle focus sidebar ↔ input
- `Enter` (in input) — send message or run `/command`
- `↑`/`↓` (in sidebar) — move selection (does not auto-load history; use mouse or `/open <index>`)
- `/help` — show command list

## Known limitations

- No real-time push — `/history` to refresh inbound messages.
- No file/image send.
- `/open <peer_id>` (numeric form) not implemented; only `/open <index>` works.
- `Esc` does not clear input; Enter submits and clears.
- Channel dialog titles may show as empty for some channel types (TODO in `internal/telegram/client.go:351`).
- Corrupt session file: delete `$TG_SESSION_DIR/session.json` and re-run to re-authenticate.
