package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Appearance.Theme != "dark" {
		t.Errorf("expected default theme 'dark', got %q", cfg.Appearance.Theme)
	}
	if cfg.Appearance.TimestampFormat != "3:04 PM" {
		t.Errorf("expected default timestamp format '3:04 PM', got %q", cfg.Appearance.TimestampFormat)
	}
	if !cfg.Animations.Enabled {
		t.Error("expected animations enabled by default")
	}
	if !cfg.Notifications.Enabled {
		t.Error("expected notifications enabled by default")
	}
	if !cfg.Notifications.OnMention {
		t.Error("expected on_mention enabled by default")
	}
	if !cfg.Notifications.OnDM {
		t.Error("expected on_dm enabled by default")
	}
	if cfg.Cache.MessageRetentionDays != 30 {
		t.Errorf("expected 30 day retention, got %d", cfg.Cache.MessageRetentionDays)
	}
	if cfg.Cache.MaxDBSizeMB != 500 {
		t.Errorf("expected 500 MB max, got %d", cfg.Cache.MaxDBSizeMB)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	err := os.WriteFile(configPath, []byte(`
[general]
default_workspace = "myteam"

[appearance]
theme = "light"

[animations]
enabled = false

[cache]
message_retention_days = 7
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.DefaultWorkspace != "myteam" {
		t.Errorf("expected workspace 'myteam', got %q", cfg.General.DefaultWorkspace)
	}
	if cfg.Appearance.Theme != "light" {
		t.Errorf("expected theme 'light', got %q", cfg.Appearance.Theme)
	}
	if cfg.Animations.Enabled {
		t.Error("expected animations disabled")
	}
	// Defaults should fill in unset values
	if cfg.Cache.MaxDBSizeMB != 500 {
		t.Errorf("expected default max_db_size_mb 500, got %d", cfg.Cache.MaxDBSizeMB)
	}
	if cfg.Cache.MessageRetentionDays != 7 {
		t.Errorf("expected 7 day retention, got %d", cfg.Cache.MessageRetentionDays)
	}
}

func TestThemeParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[theme]
primary = "#FF0000"
accent = "#00FF00"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Primary != "#FF0000" {
		t.Errorf("expected primary #FF0000, got %q", cfg.Theme.Primary)
	}
	if cfg.Theme.Accent != "#00FF00" {
		t.Errorf("expected accent #00FF00, got %q", cfg.Theme.Accent)
	}
	if cfg.Theme.Background != "" {
		t.Errorf("expected empty background, got %q", cfg.Theme.Background)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatal("expected no error for missing file, got:", err)
	}
	// Should return defaults
	if cfg.Appearance.Theme != "dark" {
		t.Errorf("expected default theme 'dark', got %q", cfg.Appearance.Theme)
	}
}
