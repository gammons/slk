package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gammons/slk/internal/config"
)

// TestUISettingsFromConfig_NavigationOptions reads the opt-in navigation
// options from a config file the way run() does: unset means off.
func TestUISettingsFromConfig_NavigationOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want uiSettings
	}{
		{"unset", "", uiSettings{}},
		{"workspace picker vim keys", "[general]\nworkspace_picker_vim_keys = true\n", uiSettings{WorkspacePickerVimKeys: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			got := uiSettingsFromConfig(cfg)
			if got.WorkspacePickerVimKeys != tc.want.WorkspacePickerVimKeys {
				t.Errorf("WorkspacePickerVimKeys = %v, want %v", got.WorkspacePickerVimKeys, tc.want.WorkspacePickerVimKeys)
			}
		})
	}
}
