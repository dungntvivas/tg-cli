package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
		// First run — interactive auth needs the raw gotd client.
		client, err := telegram.New(ctx, cfg.AppID, cfg.APIHash, sessionFile)
		if err != nil {
			return err
		}
		if err := auth.Run(ctx, client.Raw()); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	client, err := telegram.New(ctx, cfg.AppID, cfg.APIHash, sessionFile)
	if err != nil {
		return err
	}
	defer client.Close()

	// Run gotd in background; client.Run blocks until ctx is done.
	go func() {
		if err := client.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "client:", err)
			cancel()
		}
	}()

	selfName, err := client.SelfName(ctx)
	if err != nil {
		return fmt.Errorf("fetch self: %w", err)
	}

	return tui.Run(ctx, client, selfName)
}
