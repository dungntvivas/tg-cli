// Package tui renders the terminal UI for tgchat.
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/user/tgchat/internal/telegram"
)

// App holds the tview Application and our state.
type App struct {
	tv           *tview.Application
	header       *tview.TextView
	sidebar      *tview.List
	chat         *tview.TextView
	input        *tview.InputField
	status       *tview.TextView
	body         *tview.Flex
	right        *tview.Flex // chat-over-input pane, child of body
	api          telegram.API
	dialogs      []telegram.Dialog
	activeIdx    int
	sidebarShown bool
	ctx          context.Context
	chatRaw      string // formatted chat text, kept so visual-mode re-renders preserve selection without losing scroll position
	visMode      bool
	visStart     cursorPos // anchor set when 'v' was pressed (inclusive)
	visEnd       cursorPos // current cursor (inclusive)
	// activePeer is the dialog currently shown in the chat pane. Live
	// incoming messages are routed here: only matching PeerID/PeerKind are
	// appended to `messages`.
	activePeer telegram.Peer
	messages  []telegram.Message
}

// cursorPos is a 1-based (line, col) position in display cells.
type cursorPos struct {
	Line, Col int
}

// Run blocks until the user quits. api must be connected before calling.
func Run(ctx context.Context, api telegram.API, selfName string) error {
	app := &App{
		tv:           tview.NewApplication(),
		api:          api,
		ctx:          ctx,
		activeIdx:    -1,
		sidebarShown: true,
	}
	app.build(selfName)
	// Subscribe to live updates BEFORE the tview loop starts so the
	// handler is wired before any update arrives. The handler hops onto
	// the UI goroutine via QueueUpdateDraw — OnMessage itself runs on
	// gotd's update goroutine, but tview primitives are not thread-safe.
	api.OnMessage(app.onIncoming)
	return app.tv.Run()
}

// onIncoming is invoked by telegram.Client from gotd's update goroutine.
// We bounce onto the UI goroutine via QueueUpdateDraw before touching
// any tview widgets or shared App state.
func (a *App) onIncoming(m telegram.Message) {
	a.tv.QueueUpdateDraw(func() {
		a.handleIncoming(m)
	})
}

// handleIncoming routes a live message to the right surface: append to the
// active chat when it matches, otherwise refresh the sidebar so the
// dialog's last message + unread count update. Dedups by ID so our own
// optimistic append (from sendMessage) doesn't double up if the server
// echoes back via an update we forgot to filter on the client side.
func (a *App) handleIncoming(m telegram.Message) {
	if m.PeerID == a.activePeer.ID && m.PeerKind == a.activePeer.Kind {
		for _, existing := range a.messages {
			if existing.ID == m.ID && m.ID != 0 {
				return // already have it (sendMessage added it optimistically)
			}
		}
		a.messages = append(a.messages, m)
		a.chatRaw = RenderHistory(a.messages, chatPaneWidth(a.chat))
		a.refreshChat()
		a.chat.ScrollToEnd()
		return
	}
	// Non-active chat — refresh the sidebar so it shows the new last
	// message preview and bumps the unread count.
	a.refreshDialogs()
}

func (a *App) build(selfName string) {
	// Color palette — one accent (cyan), one muted gray. Keeps the UI
	// readable without competing with chat content for attention.
	accent := tcell.ColorAqua
	muted := tcell.ColorDarkSlateGray
	dimText := tcell.ColorLightSlateGray

	// Header: single line, accent color. Acts as a status bar.
	a.header = tview.NewTextView().
		SetText(fmt.Sprintf(" tgchat  %s   F10 sidebar · Tab switch · Ctrl+Y copy · Ctrl+C quit ", selfName)).
		SetTextColor(dimText)
	a.header.SetBorder(false)

	// Sidebar: bordered list with rounded-corner title.
	a.sidebar = tview.NewList().ShowSecondaryText(false)
	a.sidebar.SetBorder(true).
		SetTitle(" Dialogs ").
		SetTitleColor(accent).
		SetBorderColor(muted)

	// Chat: scrollable, dynamic colors so FormatMessage can colorize senders.
	// Word wrap is disabled because formatOutgoing pre-wraps long outgoing
	// lines for right-alignment; letting tview re-wrap would break the
	// padding logic and misalign the right edge.
	// Regions are enabled so visual-mode selection can be highlighted via
	// tview.Highlight("sel").
	a.chat = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetRegions(true).
		SetWrap(false).
		SetWordWrap(false).
		SetChangedFunc(func() { a.tv.Draw() })
	a.chat.SetBorder(true).
		SetTitle(" Chat ").
		SetTitleColor(accent).
		SetBorderColor(muted)

	// Input: framed box with cyan prompt label, full width (FieldWidth=0).
	// tview's selectedStyle paints a PrimaryTextColor background when the
	// field is focused, which on many terminals shows as a blue block. Pin
	// both field and selection backgrounds to the terminal default so the
	// input area stays transparent and matches the surrounding border.
	a.input = tview.NewInputField().
		SetLabel(" > ").
		SetLabelColor(accent).
		SetFieldWidth(0).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldStyle(tcell.StyleDefault.Background(tcell.ColorDefault)).
		SetLabelStyle(tcell.StyleDefault.Background(tcell.ColorDefault).Foreground(accent))
	a.input.SetBorder(true).
		SetTitle(" Message ").
		SetTitleColor(accent).
		SetBorderColor(muted)

	// Status: thin muted line, centered text.
	a.status = tview.NewTextView().SetTextAlign(tview.AlignCenter)
	a.status.SetBorder(false)
	a.status.SetTextColor(dimText)

	// Layout: header on top, main row (sidebar | (chat over input)), status at bottom.
	a.right = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.chat, 0, 4, false).
		AddItem(a.input, 3, 1, true)
	a.body = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.sidebar, 36, 0, true).
		AddItem(a.right, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 1, 0, false).
		AddItem(a.body, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.tv.SetRoot(root, true).EnableMouse(true)
	a.bindKeys()
	a.bindInput()
	a.refreshDialogs()
}

func (a *App) bindKeys() {
	a.tv.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		// Global keys work regardless of focus.
		switch e.Key() {
		case tcell.KeyCtrlC:
			a.tv.Stop()
			return nil
		case tcell.KeyF10:
			a.toggleSidebar()
			return nil
		case tcell.KeyCtrlY:
			// Copy current chat to system clipboard. tview's mouse mode
			// blocks the terminal's native selection, so we expose the text
			// explicitly via the OS clipboard.
			a.yankChat()
			return nil
		case tcell.KeyTab:
			// Cycle focus: sidebar <-> input. Skip sidebar when hidden.
			if !a.sidebarShown {
				a.tv.SetFocus(a.input)
				return nil
			}
			if a.tv.GetFocus() == a.sidebar {
				a.tv.SetFocus(a.input)
			} else {
				a.tv.SetFocus(a.sidebar)
			}
			return nil
		}

		// Visual mode + vim-style 'v' toggle are chat-scoped: only when the
		// chat TextView has focus, otherwise the input field would eat 'v'.
		if a.tv.GetFocus() != a.chat {
			return e
		}

		if a.visMode {
			if a.handleVisKey(e) {
				return nil
			}
		} else if e.Key() == tcell.KeyRune && e.Rune() == 'v' {
			a.enterVisualMode()
			return nil
		}
		return e
	})
}

// handleVisKey dispatches a key while in visual mode. Returns true if the key
// was consumed (caller should drop it).
func (a *App) handleVisKey(e *tcell.EventKey) bool {
	// Arrow keys + Home/End move the cursor; vim letters do the same.
	switch e.Key() {
	case tcell.KeyEscape:
		a.exitVisualMode()
		return true
	case tcell.KeyLeft, tcell.KeyBackspace:
		a.moveVisCursor(0, -1)
		return true
	case tcell.KeyRight:
		a.moveVisCursor(0, 1)
		return true
	case tcell.KeyUp:
		a.moveVisCursor(-1, 0)
		return true
	case tcell.KeyDown:
		a.moveVisCursor(1, 0)
		return true
	case tcell.KeyHome:
		a.setVisCursorCol(1)
		return true
	case tcell.KeyEnd:
		a.setVisCursorCol(lineLastCell(a.chatRaw, a.visEnd.Line))
		return true
	}
	if e.Key() != tcell.KeyRune {
		return false
	}
	switch e.Rune() {
	case 'v':
		a.exitVisualMode()
		return true
	case 'y':
		a.visYank()
		return true
	case 'h':
		a.moveVisCursor(0, -1)
		return true
	case 'j':
		a.moveVisCursor(1, 0)
		return true
	case 'k':
		a.moveVisCursor(-1, 0)
		return true
	case 'l':
		a.moveVisCursor(0, 1)
		return true
	case '0':
		a.setVisCursorCol(1)
		return true
	case '$':
		a.setVisCursorCol(lineLastCell(a.chatRaw, a.visEnd.Line))
		return true
	case 'g':
		a.setVisCursorPos(1, 1)
		return true
	case 'G':
		lastLine := strings.Count(a.chatRaw, "\n") + 1
		a.setVisCursorPos(lastLine, 1)
		return true
	}
	return false
}

// toggleSidebar removes or re-adds the sidebar in the body flex. When shown,
// focus returns to the sidebar so arrow keys navigate dialogs immediately;
// when hidden, focus stays on the input.
//
// Implementation note: tview's Flex always appends new items, so re-adding
// the sidebar with AddItem would put it AFTER the chat pane (right side).
// We Clear() and rebuild the body in the correct order: sidebar first, then
// chat pane.
func (a *App) toggleSidebar() {
	if a.sidebarShown {
		a.body.Clear()
		a.body.AddItem(a.right, 0, 1, false)
		a.sidebarShown = false
		a.tv.SetFocus(a.input)
		a.toast("sidebar hidden (F10 to show)")
	} else {
		a.body.Clear()
		a.body.AddItem(a.sidebar, 36, 0, true)
		a.body.AddItem(a.right, 0, 1, false)
		a.sidebarShown = true
		a.tv.SetFocus(a.sidebar)
		a.toast("sidebar shown")
	}
}

func (a *App) bindInput() {
	a.input.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		text := a.input.GetText()
		a.input.SetText("")
		a.handleInput(text)
	})
}

func (a *App) handleInput(text string) {
	cmd, args, body, isCmd := ParseCommand(text)
	switch cmd {
	case CmdDialogs:
		a.refreshDialogs()
	case CmdOpen:
		a.openByArgs(args)
	case CmdHistory:
		a.loadHistory(50)
	case CmdSend:
		a.sendMessage(body)
	case CmdHelp:
		a.showHelp()
	case CmdQuit:
		a.tv.Stop()
	case CmdUnknown:
		if isCmd {
			return
		}
		// Raw text: send to active peer.
		if text != "" {
			a.sendMessage(text)
		}
	}
}

func (a *App) refreshDialogs() {
	dialogs, err := a.api.Dialogs(a.ctx)
	if err != nil {
		a.toast(fmt.Sprintf("dialogs error: %v", err))
		return
	}
	// Preserve active selection across refresh by remapping the index from
	// the active peer ID+Kind. Without this, activeIdx could point at a
	// different dialog after the list reorders (e.g. another chat bubbles
	// to the top because it received a new message).
	var activeID int64
	var activeKind string
	if a.activeIdx >= 0 && a.activeIdx < len(a.dialogs) {
		activeID = a.dialogs[a.activeIdx].ID
		activeKind = a.dialogs[a.activeIdx].Kind
	}
	a.dialogs = dialogs
	a.sidebar.Clear()
	rows := RenderSidebar(dialogs, a.activeIdx)
	for i, r := range rows {
		idx := i
		a.sidebar.AddItem(r, "", 0, func() {
			a.openByArgs([]string{fmt.Sprint(idx + 1)})
		})
	}
	if activeID != 0 {
		for i, d := range dialogs {
			if d.ID == activeID && d.Kind == activeKind {
				a.activeIdx = i
				a.sidebar.SetCurrentItem(i)
				return
			}
		}
		// Active dialog dropped out of the top-100 — clamp activeIdx so
		// the sidebar stays in range.
		if a.activeIdx >= len(dialogs) {
			a.activeIdx = len(dialogs) - 1
		}
	}
}

func (a *App) openByArgs(args []string) {
	if len(args) == 0 || len(a.dialogs) == 0 {
		return
	}
	// 1-based index from sidebar.
	var idx int
	fmt.Sscanf(args[0], "%d", &idx)
	idx-- // to 0-based
	if idx < 0 || idx >= len(a.dialogs) {
		a.toast("invalid dialog index")
		return
	}
	a.activeIdx = idx
	d := a.dialogs[idx]
	a.sidebar.SetCurrentItem(idx)
	a.toast(fmt.Sprintf("opened %s", d.Title))
	a.loadHistory(50)
}

func (a *App) loadHistory(limit int) {
	if a.activeIdx < 0 {
		a.toast("no dialog selected")
		return
	}
	d := a.dialogs[a.activeIdx]
	peer := telegram.Peer{ID: d.ID, Kind: d.Kind, AccessHash: d.AccessHash}
	msgs, err := a.api.History(a.ctx, peer, limit)
	if err != nil {
		a.toast(fmt.Sprintf("history error: %v", err))
		return
	}
	// Cache peer + messages so handleIncoming can route live updates to
	// this same window and sendMessage can append the new row without a
	// full history refetch.
	a.activePeer = peer
	a.messages = msgs
	a.chatRaw = RenderHistory(a.messages, chatPaneWidth(a.chat))
	a.refreshChat()
	// Track-end mode: keeps the newest line visible when SetText is called,
	// so opening a chat or sending a message auto-scrolls to the bottom
	// (matches how chat UIs behave).
	a.chat.ScrollToEnd()
	// Tell the server we've seen this dialog so the unread badge clears.
	// Failure is non-fatal — the chat still works, the badge just lingers
	// until the next refreshDialogs picks up the server-side state.
	if err := a.api.MarkRead(a.ctx, peer); err != nil {
		a.toast(fmt.Sprintf("mark-read failed: %v", err))
		return
	}
	a.refreshDialogs()
}

func (a *App) sendMessage(text string) {
	if a.activeIdx < 0 {
		a.toast("no dialog selected")
		return
	}
	d := a.dialogs[a.activeIdx]
	peer := telegram.Peer{ID: d.ID, Kind: d.Kind, AccessHash: d.AccessHash}
	sent, err := a.api.Send(a.ctx, peer, text)
	if err != nil {
		a.toast(fmt.Sprintf("send error: %v", err))
		return
	}
	// Optimistic append — no need to round-trip History() to see our own
	// message. handleIncoming dedups by ID if the server also echoes the
	// message back through an update we forgot to filter.
	if sent.ID == 0 {
		// P2P send with no echoed ID — fall back to a local placeholder.
		sent.ID = -int64(len(a.messages) + 1)
	}
	a.messages = append(a.messages, sent)
	a.chatRaw = RenderHistory(a.messages, chatPaneWidth(a.chat))
	a.refreshChat()
	a.chat.ScrollToEnd()
}

func (a *App) showHelp() {
	a.chatRaw =
		"Commands:\n" +
			"  /dialogs            refresh dialog list\n" +
			"  /open <index>       switch to dialog at 1-based sidebar index\n" +
			"  /history [n=50]     reload last N messages\n" +
			"  /send <text>        send text to active dialog\n" +
			"  /help               this help\n" +
			"  /quit               exit\n\n" +
			"Or just type and press Enter to send.\n" +
			"Tab cycles focus between sidebar and input.\n" +
			"F10 toggles sidebar visibility.\n" +
			"Ctrl+Y copies the current chat to the system clipboard.\n" +
			"v enters visual mode (when chat is focused) — use h/j/k/l to\n" +
			"extend the selection, y to yank, v or Esc to cancel."
	a.refreshChat()
}

func (a *App) toast(msg string) {
	a.status.SetText(msg)
	go func() {
		time.Sleep(3 * time.Second)
		a.tv.QueueUpdateDraw(func() {
			if a.status.GetText(true) == msg {
				a.status.SetText("")
			}
		})
	}()
}

// chatPaneWidth returns the chat TextView's drawable width in cells (rect
// width minus the 2-cell border). Falls back to a sane default before the
// view has been laid out (e.g. during construction).
func chatPaneWidth(tv *tview.TextView) int {
	_, _, w, _ := tv.GetRect()
	if w <= 2 {
		return 80
	}
	return w - 2
}

// yankChat copies the current chat view contents to the system clipboard,
// stripping the ANSI color codes that tview would otherwise display. Toasts
// the number of lines copied (or the failure reason).
//
// Why this exists: tview's EnableMouse(true) requests SGR mouse reporting, so
// the terminal stops allowing native drag-to-select. The only way to get chat
// text out is to push it through the OS clipboard ourselves.
func (a *App) yankChat() {
	raw := a.chat.GetText(true)
	if strings.TrimSpace(raw) == "" {
		a.toast("nothing to copy")
		return
	}
	text := StripANSI(raw)
	if err := copyToClipboard(text); err != nil {
		a.toast(fmt.Sprintf("copy failed: %v (install xclip/xsel on Linux)", err))
		return
	}
	lines := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	a.toast(fmt.Sprintf("copied %d lines to clipboard", lines))
}

// copyToClipboard writes `text` to the OS clipboard via the platform-native
// helper (clip.exe on Windows, pbcopy on macOS, xclip or xsel on Linux).
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		// Linux: prefer xclip, fall back to xsel. Both are de-facto standard
		// on desktop distros. wayland-clipboard is another candidate, but
		// wayland users typically have one of these aliased anyway.
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool found")
		}
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// --- Visual selection (vim-style) -------------------------------------------
//
// Click on the chat pane, press 'v' to enter visual mode, navigate with
// h/j/k/l (or arrow keys), then 'y' to copy the selected cells to the
// clipboard. 'v' or Esc exits visual mode without copying.

// enterVisualMode starts visual mode anchored at the top-left of the chat.
// Subsequent 'h/j/k/l' extends the selection.
func (a *App) enterVisualMode() {
	if a.visMode {
		return
	}
	a.visMode = true
	a.visStart = cursorPos{Line: 1, Col: 1}
	a.visEnd = a.visStart
	a.refreshChat()
	a.status.SetText("-- VISUAL --")
	a.tv.SetFocus(a.chat)
}

// exitVisualMode leaves visual mode without copying.
func (a *App) exitVisualMode() {
	if !a.visMode {
		return
	}
	a.visMode = false
	a.refreshChat()
	a.status.SetText("")
}

// visYank copies the cell range from visStart..visEnd to the clipboard, then
// exits visual mode.
func (a *App) visYank() {
	if !a.visMode {
		return
	}
	text := StripANSI(ExtractSelection(a.chatRaw, a.visStart.Line, a.visStart.Col, a.visEnd.Line, a.visEnd.Col))
	a.exitVisualMode()
	if strings.TrimSpace(text) == "" {
		a.toast("nothing selected")
		return
	}
	if err := copyToClipboard(text); err != nil {
		a.toast(fmt.Sprintf("yank failed: %v", err))
		return
	}
	lines := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	a.toast(fmt.Sprintf("yanked %d line%s (%d chars)", lines, plural(lines), len(text)))
}

// moveVisCursor shifts the visual cursor by (dLine, dCol) cells, clamped to the
// chat bounds, then re-renders the selection highlight.
func (a *App) moveVisCursor(dLine, dCol int) {
	if !a.visMode {
		return
	}
	lines := strings.Split(a.chatRaw, "\n")
	a.visEnd.Line += dLine
	a.visEnd.Col += dCol
	a.clampVisCursor(lines)
	a.refreshChat()
}

// setVisCursorCol jumps the cursor to `col` on the current line, clamped.
func (a *App) setVisCursorCol(col int) {
	if !a.visMode {
		return
	}
	lines := strings.Split(a.chatRaw, "\n")
	a.visEnd.Col = col
	a.clampVisCursor(lines)
	a.refreshChat()
}

// setVisCursorPos jumps to (line, col), clamped.
func (a *App) setVisCursorPos(line, col int) {
	if !a.visMode {
		return
	}
	lines := strings.Split(a.chatRaw, "\n")
	a.visEnd.Line = line
	a.visEnd.Col = col
	a.clampVisCursor(lines)
	a.refreshChat()
}

// clampVisCursor ensures visEnd stays inside the chat text. A col value of 0
// is allowed (one past the last cell, equivalent to vim's "$" + 1).
func (a *App) clampVisCursor(lines []string) {
	if len(lines) == 0 {
		a.visEnd.Line = 1
		a.visEnd.Col = 1
		return
	}
	if a.visEnd.Line < 1 {
		a.visEnd.Line = 1
	}
	if a.visEnd.Line > len(lines) {
		a.visEnd.Line = len(lines)
	}
	lineW := displayWidth(lines[a.visEnd.Line-1])
	if a.visEnd.Col < 1 {
		a.visEnd.Col = 1
	}
	if a.visEnd.Col > lineW+1 {
		a.visEnd.Col = lineW + 1
	}
}

// refreshChat re-renders the chat view, preserving the user's scroll position
// and (when in visual mode) applying the ["sel"] region around the selection.
func (a *App) refreshChat() {
	row, col := a.chat.GetScrollOffset()
	text := a.chatRaw
	if a.visMode {
		text = ApplySelection(text, a.visStart.Line, a.visStart.Col, a.visEnd.Line, a.visEnd.Col)
		a.chat.SetRegions(true)
		a.chat.Highlight("sel")
	} else {
		a.chat.Highlight()
	}
	a.chat.SetText(text)
	a.chat.ScrollTo(row, col)
}

// lineLastCell returns 1 + displayWidth(line), the column just past the last
// cell. Used for '$' / End in visual mode.
func lineLastCell(text string, lineNum int) int {
	lines := strings.Split(text, "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return 1
	}
	return displayWidth(lines[lineNum-1]) + 1
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
