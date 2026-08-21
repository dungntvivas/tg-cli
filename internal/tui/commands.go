package tui

import "strings"

type Command int

const (
	CmdUnknown Command = iota
	CmdDialogs
	CmdOpen
	CmdHistory
	CmdSend
	CmdHelp
	CmdQuit
)

// ParseCommand splits user input. If input starts with "/" and matches a known command,
// returns (cmd, args, "", true). If input is "/send <text>", text is the message body.
// Otherwise returns (CmdUnknown, nil, input, false) — raw message to send.
// Bare "/" with nothing after is treated as a raw message (not a command).
func ParseCommand(input string) (Command, []string, string, bool) {
	input = strings.TrimRight(input, " \t")
	if input == "" || input == "/" {
		return CmdUnknown, nil, input, false
	}
	if !strings.HasPrefix(input, "/") {
		return CmdUnknown, nil, input, false
	}

	parts := strings.SplitN(input[1:], " ", 2)
	name := parts[0]
	rest := ""
	if len(parts) == 2 {
		rest = parts[1]
	}

	switch name {
	case "dialogs":
		return CmdDialogs, nil, "", true
	case "open":
		args := splitArgs(rest)
		return CmdOpen, args, "", true
	case "history":
		args := splitArgs(rest)
		return CmdHistory, args, "", true
	case "send":
		return CmdSend, nil, rest, true
	case "help":
		return CmdHelp, nil, "", true
	case "quit":
		return CmdQuit, nil, "", true
	default:
		return CmdUnknown, nil, input, false
	}
}

func splitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}