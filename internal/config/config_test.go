package config

import (
	"os"
	"testing"
)

func TestLoadFromEnvVars(t *testing.T) {
	// Set required env vars
	os.Setenv("TELEGRAM_TOKEN", "test-token-123")
	os.Setenv("CHAT_ID", "12345")
	os.Setenv("LISTEN_ADDR", ":8080")
	os.Setenv("ALERTMANAGER_INTERNAL_URL", "http://am:9093")
	os.Setenv("CONFIG_FILE", "/nonexistent/config.yml") // Force skip file loading
	defer func() {
		os.Unsetenv("TELEGRAM_TOKEN")
		os.Unsetenv("CHAT_ID")
		os.Unsetenv("LISTEN_ADDR")
		os.Unsetenv("ALERTMANAGER_INTERNAL_URL")
		os.Unsetenv("CONFIG_FILE")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.TelegramToken != "test-token-123" {
		t.Errorf("TelegramToken = %q, want %q", cfg.TelegramToken, "test-token-123")
	}
	if cfg.ChatID != 12345 {
		t.Errorf("ChatID = %d, want %d", cfg.ChatID, 12345)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.AlertmanagerInternalURL != "http://am:9093" {
		t.Errorf("AlertmanagerInternalURL = %q, want %q", cfg.AlertmanagerInternalURL, "http://am:9093")
	}
}

func TestLoadMissingToken(t *testing.T) {
	os.Setenv("CONFIG_FILE", "/nonexistent/config.yml")
	os.Unsetenv("TELEGRAM_TOKEN")
	defer os.Unsetenv("CONFIG_FILE")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing token, got nil")
	}
}

func TestLoadDefaults(t *testing.T) {
	os.Setenv("TELEGRAM_TOKEN", "test-token")
	os.Setenv("CONFIG_FILE", "/nonexistent/config.yml")
	defer func() {
		os.Unsetenv("TELEGRAM_TOKEN")
		os.Unsetenv("CONFIG_FILE")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ListenAddr != ":9087" {
		t.Errorf("Default ListenAddr = %q, want %q", cfg.ListenAddr, ":9087")
	}
	if cfg.AlertmanagerInternalURL != "http://localhost:9093" {
		t.Errorf("Default AlertmanagerInternalURL = %q, want %q", cfg.AlertmanagerInternalURL, "http://localhost:9093")
	}
}

func TestLoadRoutesFromEnv(t *testing.T) {
	os.Setenv("TELEGRAM_TOKEN", "test-token")
	os.Setenv("CONFIG_FILE", "/nonexistent/config.yml")
	os.Setenv("ROUTES", "severity=critical:-100123,team=backend:-100456")
	defer func() {
		os.Unsetenv("TELEGRAM_TOKEN")
		os.Unsetenv("CONFIG_FILE")
		os.Unsetenv("ROUTES")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Routes) != 2 {
		t.Fatalf("Routes count = %d, want 2", len(cfg.Routes))
	}
	if cfg.Routes["severity=critical"] != -100123 {
		t.Errorf("Route severity=critical = %d, want %d", cfg.Routes["severity=critical"], -100123)
	}
	if cfg.Routes["team=backend"] != -100456 {
		t.Errorf("Route team=backend = %d, want %d", cfg.Routes["team=backend"], -100456)
	}
}
