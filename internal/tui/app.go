// Package tui renders the terminal UI for tgchat.
package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/user/tgchat/internal/telegram"
)

// App holds the tview Application and our state.
type App struct {
	tv        *tview.Application
	header    *tview.TextView
	sidebar   *tview.List
	chat      *tview.TextView
	input     *tview.InputField
	status    *tview.TextView
	api       telegram.API
	dialogs   []telegram.Dialog
	activeIdx int
	ctx       context.Context
}

// Run blocks until the user quits. api must be connected before calling.
func Run(ctx context.Context, api telegram.API, selfName string) error {
	app := &App{
		tv:        tview.NewApplication(),
		api:       api,
		ctx:       ctx,
		activeIdx: -1,
	}
	app.build(selfName)
	return app.tv.Run()
}

func (a *App) build(selfName string) {
	a.header = tview.NewTextView().
		SetText(fmt.Sprintf(" tgchat — %s   Ctrl+C quit · Tab switch ", selfName)).
		SetTextColor(tcell.ColorWhite)
	a.header.SetBorder(false)

	a.sidebar = tview.NewList().ShowSecondaryText(false)
	a.sidebar.SetBorder(true).SetTitle(" Dialogs ")

	a.chat = tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetChangedFunc(func() { a.tv.Draw() })
	a.chat.SetBorder(true).SetTitle(" Chat ")

	a.input = tview.NewInputField().
		SetLabel(" > ").
		SetFieldWidth(0)
	a.input.SetBorder(true).SetTitle(" Input ")

	a.status = tview.NewTextView().SetTextAlign(tview.AlignCenter)
	a.status.SetBorder(false)

	// Layout: header on top, main row (sidebar | (chat over input)), status at bottom.
	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.chat, 0, 4, false).
		AddItem(a.input, 3, 1, true)
	body := tview.NewFlex().
		AddItem(a.sidebar, 24, 0, true).
		AddItem(right, 0, 1, false)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.header, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(a.status, 1, 0, false)

	a.tv.SetRoot(root, true).EnableMouse(true)
	a.bindKeys()
	a.bindInput()
	a.refreshDialogs()
}

func (a *App) bindKeys() {
	a.tv.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		switch e.Key() {
		case tcell.KeyCtrlC:
			a.tv.Stop()
			return nil
		case tcell.KeyTab:
			// Cycle focus: sidebar <-> input.
			if a.tv.GetFocus() == a.sidebar {
				a.tv.SetFocus(a.input)
			} else {
				a.tv.SetFocus(a.sidebar)
			}
			return nil
		}
		return e
	})
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
	a.dialogs = dialogs
	a.sidebar.Clear()
	rows := RenderSidebar(dialogs, a.activeIdx)
	for i, r := range rows {
		idx := i
		a.sidebar.AddItem(r, "", 0, func() {
			a.openByArgs([]string{fmt.Sprint(idx + 1)})
		})
	}
	if a.activeIdx >= 0 && a.activeIdx < len(dialogs) {
		a.sidebar.SetCurrentItem(a.activeIdx)
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
	a.chat.SetText(RenderHistory(msgs))
	// Track-end mode: keeps the newest line visible when SetText is called,
	// so opening a chat or sending a message auto-scrolls to the bottom
	// (matches how chat UIs behave).
	a.chat.ScrollToEnd()
}

func (a *App) sendMessage(text string) {
	if a.activeIdx < 0 {
		a.toast("no dialog selected")
		return
	}
	d := a.dialogs[a.activeIdx]
	peer := telegram.Peer{ID: d.ID, Kind: d.Kind, AccessHash: d.AccessHash}
	if _, err := a.api.Send(a.ctx, peer, text); err != nil {
		a.toast(fmt.Sprintf("send error: %v", err))
		return
	}
	// Optimistic: reload history to show the new outgoing message.
	a.loadHistory(50)
}

func (a *App) showHelp() {
	a.chat.SetText(
		"Commands:\n" +
			"  /dialogs            refresh dialog list\n" +
			"  /open <index>       switch to dialog at 1-based sidebar index\n" +
			"  /history [n=50]     reload last N messages\n" +
			"  /send <text>        send text to active dialog\n" +
			"  /help               this help\n" +
			"  /quit               exit\n\n" +
			"Or just type and press Enter to send.\n" +
			"Tab cycles focus between sidebar and input.",
	)
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
