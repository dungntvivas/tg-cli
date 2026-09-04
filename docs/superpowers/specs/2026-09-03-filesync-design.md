# Filesync mode — Design Spec

**Date:** 2026-09-03
**Status:** Approved (pending written review)
**Author:** brainstorming session with user

## Goal

Mirror recent Telegram conversations as plain text files on disk so the user can read and
reply from any text editor (VS Code being the motivating case). One file per conversation.
The file's tail is an editable compose area: saving the file sends its contents as a message.

This runs **in parallel with** the existing TUI, in the same process. The TUI is unchanged
from the user's point of view.

## Why files

The editor already provides what the TUI would otherwise have to reimplement: cross-conversation
search (`Ctrl+Shift+F` over the whole folder), multi-line composition with undo, split views, and
syntax highlighting. Prior art: `ii` (suckless IRC client), which has used a per-channel
`out` log + `in` input file for two decades.

## Non-goals (v1, explicit)

- Editing or deleting a sent message by editing the log
- Renaming a file to rename a chat
- Creating a new file to start a new conversation
- Inline image rendering
- History older than the initial 200-message fetch
- Syncing every dialog (only the N most recent)

Each of these requires parsing the log back into intent, which is the fragile part of the
design. Deferred until the one-way path is proven in real use.

## Process model

**Filesync must run in the same process as the TUI.** Two processes would share
`session.json` and race on writes to it, and — worse — two update streams under the same auth
key desynchronise Telegram's `pts`/`qts` cursors, silently dropping updates on both sides.

```
main.go
  |- media.Start(ctx, api)                     -> baseURL
  |- go filesync.Run(ctx, api, baseURL, dir)   <- background
  \- tui.Run(ctx, api, selfName, baseURL)      <- blocking, as today
```

## Changes to existing code

### `internal/media/` (new package, moved code)

`internal/tui/download_server.go` moves to `internal/media/server.go` with no logic change:
`dlServer`, `startDownloadServer` → `media.Start`, `randToken`, `ServeHTTP`. It now has two
consumers — the TUI (clickable regions) and filesync (URLs written into text) — so it no
longer belongs to the TUI package.

`openDownloadLink` and `openInBrowser` stay in `internal/tui`: they handle a TUI click, not
the serving.

`tui.Run` gains a `dlBase string` parameter instead of starting the server itself.

### `internal/telegram/client.go`

`OnMessage` currently **replaces** the registered handler. It must **append**:

```go
handlers []func(Message)   // was: onMsg func(Message)
```

`fire` iterates the slice under the existing lock discipline. The interface signature in
`types.go` is unchanged; its doc comment is updated ("Multiple calls replace the previous
handler" → "each registered handler is called"). Both the TUI and filesync need to hear
updates; whoever registered second would otherwise silence the first.

## New package: `internal/filesync`

Three files, each with one job:

| File | Responsibility |
|---|---|
| `sync.go` | Lifecycle: initial dump, update handler, poll loop |
| `chatfile.go` | One conversation's file: render whole file, extract draft |
| `format.go` | Render one `telegram.Message` to a log line |

### File format

```
[10:23] Nam: ok mày đâu rồi
[10:25] Bạn: tôi đến ngay
         kẹt xe quá
[10:31] Nam: 📎 báo_cáo.pdf http://127.0.0.1:5417/dl/a3f/user/88/1204

--- gõ dưới đây ---
```

- One line per message: `[HH:MM] <sender>: <text>`. Outgoing messages
  (`Message.Outgoing`) render the sender as `Bạn`, not the API's `You`.
- Continuation lines of a multi-line message are indented by 8 spaces — the width of the
  `[HH:MM] ` prefix. Fixed, not aligned to the sender name, so the indent is deterministic.
- Media messages render as `<glyph> <label> <url>`, where the URL is the same
  `dlServer` link the TUI uses. `Ctrl+click` in VS Code opens it; the browser saves the file.
  A captioned attachment renders the media line first and the caption underneath — showing
  only the caption would hide the link and make the attachment unreachable from the file.
- Everything after the marker line is the compose area

### Editor workspace

On startup filesync writes `<dir>/.vscode/settings.json`, unless one already exists:

- `explorer.sortOrder: "modified"` — puts the active conversation on top. mtime already
  tracks recency because filesync rewrites a file whenever that chat moves, so no file has
  to be renamed; renaming would break open editor tabs.
- `files.saveConflictResolution: "overwriteFileOnDisk"` — without it VS Code refuses to save
  any file filesync rewrote while the user was typing, and keeps refusing until the user
  resolves the conflict by hand. Overwriting is safe here *only* because of the invariant
  below: memory owns the log, so an overwritten log is repaired by the next write.

The initial dump backdates each file's mtime to the dialog's `LastTime`. Without that the
dump order (newest first) would give the freshest chat the oldest mtime of the batch, and
the explorer would sort exactly backwards.

### Naming

`<dir>/<sanitised title>.md`. Windows-forbidden characters `\ / : * ? " < > |` become `_`.
Collisions get ` (<peer id>)` appended. Directory defaults to
`%USERPROFILE%\.local\share\tgchat\chats`, overridable with `TG_CHAT_DIR`.

### The invariant that keeps this safe

> **Above the marker: memory is the source of truth, the file is output.**
> **Below the marker: the file is the source of truth, memory never writes it.**

filesync holds each conversation's messages in memory and rewrites the *whole* file from
that state on every change. It never parses the log back. So if the editor's buffer was
stale and the user saves an outdated log over the top, filesync rewrites the correct log
immediately after. Nothing is lost.

Conversely filesync only ever *clears* the compose area, after a successful send.

### Data flow

**Startup.** `Dialogs(ctx)` → drop bot conversations (`Dialog.Bot`, from `tg.User.Bot`) →
keep the 50 with the newest `LastTime` → for each,
`History(peer, 200)` → write the file. The synced set is fixed for the process lifetime;
a dialog that becomes active later shows up in the folder on the next restart. Muted dialogs are included: mute suppresses toasts, not reading.

**Incoming message.** The `OnMessage` handler appends to the in-memory log for that peer
and rewrites its file. Messages for peers outside the synced set are ignored — they remain
visible in the TUI.

**Sending a file.** A compose area whose *first* line starts with `@` is an attachment:
everything after the `@` is the path (spaces allowed), the lines below are the caption.
Restricting `@` to the first line is what keeps `gửi cho @dungvivas` and email addresses from
being read as attachments. Relative paths resolve against the chat folder. The file is
`os.Stat`-checked before any upload, so a typo costs no API call and leaves the draft
editable. `telegram.SendFile` uploads via gotd's chunked `uploader`, sending
`.png/.jpg/.jpeg/.webp` as photos and everything else as a document with its filename.

**Outgoing message.** A goroutine polls each file's `mtime` every 500ms (`os.Stat`; chosen
over `fsnotify` to avoid a new dependency, and because `fsnotify` on Windows emits
duplicate events that need the same debounce anyway). On change:

1. Read the file, take everything after the marker, trim trailing whitespace
2. Skip if empty, or if the file's mtime still matches the one recorded after our own last
   write (prevents a write → notice → resend loop)
3. `Send(ctx, peer, draft)` — the entire compose area is **one** message, newlines preserved
4. Append the sent message to the in-memory log and rewrite the file with an empty
   compose area

Send failures leave the draft in place and append a `⚠ gửi lỗi: <err>` line to the log,
so the user sees the failure in the file and can retry by saving again.

## Concurrency

- The `OnMessage` handler runs on gotd's update goroutine; the poll loop runs on its own.
  Both mutate per-conversation state, so each conversation carries a mutex covering its
  message slice and its file write.
- File writes are write-to-temp-then-rename so a reader never sees a half-written file.

## Error handling

| Failure | Behaviour |
|---|---|
| Chat directory not creatable | Log to stderr, filesync disabled, TUI runs normally |
| `History` fails for one dialog | Skip that dialog, continue with the rest |
| File write fails | Log, retry on the next change |
| User deletes a synced file | Recreated from memory on the next write to it |
| `Send` fails | Draft preserved, warning line appended to log |
| Download URL unavailable (`media.Start` failed) | Media renders label-only, no URL |

Filesync never takes down the TUI.

## Testing

Unit tests against `telegram.FakeAPI` and `t.TempDir()`, following the existing package
conventions:

- Render: a message list → expected file text (multi-line, media, outgoing/incoming)
- Draft extraction: text after the marker; empty area; marker absent; trailing whitespace
- Round trip: write file → simulate user appending a draft → poll detects it → `SendFn`
  receives the exact text → compose area is empty afterwards
- Loop guard: after filesync's own rewrite, the next poll sends nothing
- Stale-save recovery: overwrite the file with an outdated log → an incoming message
  rewrites the full correct log
- Sanitising: forbidden characters, name collisions
- `OnMessage` fan-out: two registered handlers both fire (in `internal/telegram`)

## Decisions deliberately fixed

| Decision | Value | Rationale |
|---|---|---|
| Dialogs synced | 50 most recent, bots excluded | Bounds startup fan-out against flood-wait; bots are notification feeds, not conversations |
| History depth | 200 messages | Enough to have context, bounded file size |
| Poll interval | 500ms | Below human perception for a save-to-send round trip |
| Compose granularity | whole area = one message | Matches "add a line = send a message" for the common case |
