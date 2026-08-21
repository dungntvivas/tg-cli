// Package config loads Telegram client configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	AppID      int
	APIHash    string
	SessionDir string
}

func Load() (Config, error) {
	appIDStr := os.Getenv("TG_APP_ID")
	apiHash := os.Getenv("TG_API_HASH")
	if appIDStr == "" {
		return Config{}, fmt.Errorf("TG_APP_ID is required")
	}
	if apiHash == "" {
		return Config{}, fmt.Errorf("TG_API_HASH is required")
	}
	appID, err := strconv.Atoi(appIDStr)
	if err != nil {
		return Config{}, fmt.Errorf("TG_APP_ID must be numeric: %w", err)
	}

	dir := os.Getenv("TG_SESSION_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".local", "share", "tgchat")
	}
	return Config{AppID: appID, APIHash: apiHash, SessionDir: dir}, nil
}