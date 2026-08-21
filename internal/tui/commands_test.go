package tui

import "testing"

func TestParseCommand_Dialogs(t *testing.T) {
	cmd, args, text, isCmd := ParseCommand("/dialogs")
	if !isCmd {
		t.Fatal("expected isCmd=true")
	}
	if cmd != CmdDialogs {
		t.Errorf("cmd = %v, want CmdDialogs", cmd)
	}
	if text != "" {
		t.Errorf("text = %q, want empty", text)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestParseCommand_OpenByIndex(t *testing.T) {
	cmd, args, _, _ := ParseCommand("/open 3")
	if cmd != CmdOpen {
		t.Errorf("cmd = %v, want CmdOpen", cmd)
	}
	if len(args) != 1 || args[0] != "3" {
		t.Errorf("args = %v, want [3]", args)
	}
}

func TestParseCommand_HistoryDefault(t *testing.T) {
	cmd, args, _, _ := ParseCommand("/history")
	if cmd != CmdHistory {
		t.Errorf("cmd = %v, want CmdHistory", cmd)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty (default applied later)", args)
	}
}

func TestParseCommand_HistoryWithLimit(t *testing.T) {
	cmd, args, _, _ := ParseCommand("/history 100")
	if cmd != CmdHistory {
		t.Errorf("cmd = %v, want CmdHistory", cmd)
	}
	if len(args) != 1 || args[0] != "100" {
		t.Errorf("args = %v, want [100]", args)
	}
}

func TestParseCommand_Help(t *testing.T) {
	cmd, _, _, _ := ParseCommand("/help")
	if cmd != CmdHelp {
		t.Errorf("cmd = %v, want CmdHelp", cmd)
	}
}

func TestParseCommand_Quit(t *testing.T) {
	cmd, _, _, _ := ParseCommand("/quit")
	if cmd != CmdQuit {
		t.Errorf("cmd = %v, want CmdQuit", cmd)
	}
}

func TestParseCommand_SendExplicit(t *testing.T) {
	cmd, args, text, isCmd := ParseCommand("/send hello world")
	if !isCmd {
		t.Fatal("expected isCmd=true")
	}
	if cmd != CmdSend {
		t.Errorf("cmd = %v, want CmdSend", cmd)
	}
	if text != "hello world" {
		t.Errorf("text = %q, want 'hello world'", text)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty (text consumed)", args)
	}
}

func TestParseCommand_RawMessage(t *testing.T) {
	_, _, text, isCmd := ParseCommand("just a regular message")
	if isCmd {
		t.Fatal("expected isCmd=false for raw text")
	}
	if text != "just a regular message" {
		t.Errorf("text = %q", text)
	}
}

func TestParseCommand_UnknownCommand(t *testing.T) {
	cmd, _, text, isCmd := ParseCommand("/foo bar")
	if isCmd {
		t.Fatal("unknown command should not be treated as command")
	}
	if cmd != CmdUnknown {
		// implementation-defined; just check no panic and text preserved
	}
	if text != "/foo bar" {
		t.Errorf("text = %q, want '/foo bar' (passed through as message)", text)
	}
}

func TestParseCommand_EmptyInput(t *testing.T) {
	_, _, text, isCmd := ParseCommand("")
	if isCmd || text != "" {
		t.Errorf("empty input: isCmd=%v text=%q", isCmd, text)
	}
}

func TestParseCommand_SlashOnlyIsMessage(t *testing.T) {
	_, _, text, isCmd := ParseCommand("/")
	if isCmd {
		t.Fatal("bare slash should not be a command")
	}
	if text != "/" {
		t.Errorf("text = %q, want '/'", text)
	}
}