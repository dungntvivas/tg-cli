# Telegram CLI — Design Spec

**Date:** 2026-08-21
**Status:** Approved (pending written review)
**Author:** brainstorming session with user

## Goal

Build a terminal UI (`tgchat`) for chatting with the user's own Telegram account via MTProto. Multi-pane layout: sidebar with dialog list + main pane with active chat history + input box. Inbound messages are fetched on demand (no background push). Session persists locally so auth happens once.

## Non-goals (explicit)

- Real-time push of incoming messages (read-on-demand only)
- Sending files, images, stickers, voice
- Reply / forward / edit / delete
- Search within dialogs
- Markdown rendering in incoming messages
- Multiple accounts
- Bot API (this is user-account MTProto only)

## Stack

- **Language:** Go 1.24+
- **Telegram:** `github.com/gotd/td` (MTProto client; same lib family as `chaindead/telegram-mcp`)
- **TUI:** `github.com/rivo/tview` + `github.com/gdamore/tcell/v2`
- **Session storage:** `github.com/gotd/td/storage/file` (built-in JSON file storage)
- **Auth flow:** `github.com/gotd/td/auth` + `github.com/gotd/td/telegram/updates`

No other dependencies. No CGO. Single static binary.

## Required environment

| Var            | Source                  | Purpose                          |
|----------------|-------------------------|----------------------------------|
| `TG_APP_ID`    | my.telegram.org         | Telegram app registration        |
| `TG_API_HASH`  | my.telegram.org         | Telegram app registration        |
| `TG_SESSION_DIR` | optional, default `~/.local/share/tgchat` | Where session JSON is stored |

`main` exits with code 1 and a clear stderr message if `TG_APP_ID` or `TG_API_HASH` is missing.

## File layout

```
D:\tg\
├── go.mod
├── go.sum
├── main.go                          # entry: env → auth-if-needed → tui
├── internal/
│   ├── config/config.go             # env loading + defaults
│   ├── auth/auth.go                 # interactive phone/code/2FA → save session
│   ├── telegram/client.go           # wrapper: Dialogs, History, Send
│   └── tui/
│       ├── app.go                   # tview.Application, layout, key bindings
│       ├── sidebar.go               # dialog list widget
│       ├── chat.go                  # history view + input box for active dialog
│       └── format.go                # message formatting (sender, time, text) + tests
```

Target: ~500 LOC across 8 files (1 main + 7 internal).

## Architecture / data flow

```
main
  ├── config.Load() → (appID, hash, sessionDir)
  ├── session.Exists(sessionDir)?
  │     ├── no  → auth.Run(appID, hash, sessionDir)  // interactive
  │     └── yes → load session
  ├── telegram.New(ctx, appID, hash, session)
  └── tui.Run(client) → blocks until user quits
```

`telegram.Client` exposes three methods only:

```go
type Client struct{ ... }
func (c *Client) Dialogs(ctx context.Context) ([]Dialog, error)
func (c *Client) History(ctx context.Context, peer Peer, limit int) ([]Message, error)
func (c *Client) Send(ctx context.Context, peer Peer, text string) (Message, error)
```

`Dialog`, `Message`, `Peer` are plain structs in `telegram/client.go`. No leaking of `gotd` types above this package.

## Auth flow (first run only)

1. Print "Enter phone number in international format (+84...):"
2. Read line, send code via `auth.SendCode`
3. Print "Enter the code Telegram sent:"
4. Read line, call `auth.SignIn`
5. If 2FA required, prompt for password, call `auth.CheckPassword`
6. On success, session is persisted by `gotd/td/storage/file` automatically
7. Subsequent runs load session and skip auth

All prompts go through `bufio.Reader` on stdin; no fancy terminal handling here (TUI is not running yet).

## TUI layout

```
┌─────────────────────────────────────────────────────┐
│ tgchat — <self username>   Ctrl+C quit · Tab switch │   header
├──────────────┬──────────────────────────────────────┤
│ ● Alice (3)  │  Alice  10:23                         │
│   Bob        │    Hey, are you free?                 │
│   Team group │                                       │
│   Channel X  │  You  10:25                           │
│              │    Yes, what's up?                     │
│              │                                       │
│              │  Alice  10:26                         │
│              │    Coffee at 3?                       │
│              ├──────────────────────────────────────┤
│              │ > _                                    │   input
└──────────────┴──────────────────────────────────────┘
```

- Header: 1 line, username + keybinding hint
- Sidebar: width ~24 cols, list of dialogs (`●` unread marker, name, unread count in parens). Selecting a row loads that dialog's history.
- Chat: top = scrollable history (textview), bottom = 3-line input box
- Status bar: bottom of screen, transient toast for errors / network state

## Commands

Input box accepts either raw text (sent as message on Enter) or commands:

| Command             | Behavior                                          |
|---------------------|---------------------------------------------------|
| `/dialogs`          | Refresh sidebar from server                       |
| `/open <index>`     | Switch active dialog by 1-based sidebar index     |
| `/open <peer_id>`   | Switch active dialog by Telegram peer ID (numeric)|
| `/history [n=50]`   | Fetch last N messages into history pane           |
| `/help`             | Show command list                                 |
| `/quit`             | Exit (also Ctrl+C)                                |
| (any other text)    | Sent as message to active dialog                  |

## Key bindings

- `Tab` — cycle focus: sidebar → input → sidebar
- `↑` / `↓` — when sidebar focused, move selection (auto-loads history)
- `Enter` — when input focused, send (or run command)
- `Esc` — when input focused, clear text
- `Ctrl+C` — quit (anywhere)

## Error handling

| Failure                          | Behavior                                       |
|----------------------------------|------------------------------------------------|
| Missing/invalid env              | stderr message + exit 1                        |
| First-run auth fail              | stderr message + exit 1                        |
| Subsequent auth fail (corrupt session) | delete session file, re-run auth          |
| `Dialogs`/`History`/`Send` network error | status-bar toast (red, 3s)              |
| Telegram flood-wait              | toast "Flood wait: Ns" + auto-retry once       |
| Send to invalid peer             | inline red error in input box                  |
| Panic in update goroutine        | recovered, logged, TUI keeps running           |

## Testing

Per ponytail — minimal viable checks:

- **Unit test** (`internal/tui/format_test.go`): `FormatMessage(msg, isOutgoing)` returns expected string for known inputs (sender, time, text, outgoing marker). 3–5 cases.
- **Self-check (`go run .`):** requires real `TG_APP_ID`/`TG_API_HASH`. Not run in CI. Documented in README: "to verify, set env vars and `go run .`, type `/dialogs`, send a message."

No integration tests, no mocked MTProto (gotd has no mock layer; building one is over-engineering for v1).

## Build / run

```sh
go mod init github.com/user/tgchat
go get github.com/gotd/td@latest
go get github.com/rivo/tview@latest
go get github.com/gdamore/tcell/v2@latest
go build -o tgchat .
TG_APP_ID=... TG_API_HASH=... ./tgchat
```

First run: auth flow. Subsequent runs: straight to TUI.

## Open questions

None at design time. Any future changes (push support, file send, etc.) get their own brainstorm → spec → plan cycle.
