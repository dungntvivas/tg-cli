package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_RequiresAppID(t *testing.T) {
	os.Unsetenv("TG_APP_ID")
	os.Setenv("TG_API_HASH", "abc")
	defer os.Unsetenv("TG_API_HASH")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing TG_APP_ID, got nil")
	}
}

func TestLoad_RequiresAPIHash(t *testing.T) {
	os.Setenv("TG_APP_ID", "12345")
	os.Unsetenv("TG_API_HASH")
	defer os.Unsetenv("TG_APP_ID")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing TG_API_HASH, got nil")
	}
}

func TestLoad_DefaultSessionDir(t *testing.T) {
	os.Setenv("TG_APP_ID", "12345")
	os.Setenv("TG_API_HASH", "deadbeef")
	os.Unsetenv("TG_SESSION_DIR")
	defer os.Unsetenv("TG_APP_ID")
	defer os.Unsetenv("TG_API_HASH")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "tgchat")
	if cfg.SessionDir != want {
		t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
	if cfg.AppID != 12345 {
		t.Errorf("AppID = %d, want 12345", cfg.AppID)
	}
	if cfg.APIHash != "deadbeef" {
		t.Errorf("APIHash = %q, want %q", cfg.APIHash, "deadbeef")
	}
}

func TestLoad_CustomSessionDir(t *testing.T) {
	os.Setenv("TG_APP_ID", "999")
	os.Setenv("TG_API_HASH", "cafebabe")
	os.Setenv("TG_SESSION_DIR", `C:\tmp\tgtest`)
	defer os.Unsetenv("TG_APP_ID")
	defer os.Unsetenv("TG_API_HASH")
	defer os.Unsetenv("TG_SESSION_DIR")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SessionDir != `C:\tmp\tgtest` {
		t.Errorf("SessionDir = %q, want %q", cfg.SessionDir, `C:\tmp\tgtest`)
	}
}

func TestLoad_InvalidAppID(t *testing.T) {
	os.Setenv("TG_APP_ID", "not-a-number")
	os.Setenv("TG_API_HASH", "x")
	defer os.Unsetenv("TG_APP_ID")
	defer os.Unsetenv("TG_API_HASH")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric TG_APP_ID, got nil")
	}
}