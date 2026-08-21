package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/user/tgchat/internal/auth"
	"github.com/user/tgchat/internal/config"
	"github.com/user/tgchat/internal/telegram"
	"github.com/user/tgchat/internal/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tgchat:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.SessionDir, 0o700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	sessionFile := filepath.Join(cfg.SessionDir, "session.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := os.Stat(sessionFile); err != nil {
		fmt.Fprintln(os.Stderr, "first run: starting interactive auth...")
		// First run — interactive auth needs the raw gotd client.
		client, err := telegram.New(ctx, cfg.AppID, cfg.APIHash, sessionFile)
		if err != nil {
			return err
		}
		if err := auth.Run(ctx, client.Raw()); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		fmt.Fprintln(os.Stderr, "auth complete, session persisted")
	}

	client, err := telegram.New(ctx, cfg.AppID, cfg.APIHash, sessionFile)
	if err != nil {
		return err
	}
	defer client.Close()

	// Pass SelfName+TUI into Run's callback so they run only AFTER
	// restoreConnection + first init have completed. Doing them in a separate
	// goroutine that races against Run's startup would hit the pre-restore
	// conn (which never gets its gotConfig signaled — see telegram.Client.Run
	// for the full trace).
	return client.Run(ctx, func(ctx context.Context) error {
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 30*time.Second)
		selfName, err := client.SelfName(fetchCtx)
		fetchCancel()
		if err != nil {
			return fmt.Errorf("fetch self: %w", err)
		}
		fmt.Fprintln(os.Stderr, "connected as", selfName)
		return tui.Run(ctx, client, selfName)
	})
}
