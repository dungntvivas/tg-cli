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
		fmt.Fprintln(os.Stderr, "[tgchat] first run, starting auth...")
		// First run — interactive auth needs the raw gotd client.
		client, err := telegram.New(ctx, cfg.AppID, cfg.APIHash, sessionFile)
		if err != nil {
			return err
		}
		if err := auth.Run(ctx, client.Raw()); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		fmt.Fprintln(os.Stderr, "[tgchat] auth completed, session persisted")
	} else {
		fmt.Fprintln(os.Stderr, "[tgchat] session found, skipping auth")
	}

	fmt.Fprintln(os.Stderr, "[tgchat] constructing client...")
	client, err := telegram.New(ctx, cfg.AppID, cfg.APIHash, sessionFile)
	if err != nil {
		return err
	}
	defer client.Close()

	fmt.Fprintln(os.Stderr, "[tgchat] starting connection goroutine...")
	// Run gotd in background; client.Run blocks until ctx is done.
	go func() {
		if err := client.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "[tgchat] connection error:", err)
			cancel()
		}
	}()

	fmt.Fprintln(os.Stderr, "[tgchat] waiting for connection, fetching self...")
	selfName, err := client.SelfName(ctx)
	if err != nil {
		return fmt.Errorf("fetch self: %w", err)
	}
	fmt.Fprintln(os.Stderr, "[tgchat] connected as", selfName, "starting TUI...")

	return tui.Run(ctx, client, selfName)
}
