package slackdesktop

import (
	"os"
	"path/filepath"
	"runtime"
)

// configDirForOS computes the Slack desktop config dir for a given OS.
// getenv and exists are injected for testability.
func configDirForOS(goos string, getenv func(string) string, exists func(string) bool) string {
	switch goos {
	case "windows":
		return filepath.Join(getenv("APPDATA"), "Slack")
	case "darwin":
		home := getenv("HOME")
		first := filepath.Join(home, "Library", "Application Support", "Slack")
		second := filepath.Join(home, "Library", "Containers", "com.tinyspeck.slackmacgap", "Data", "Library", "Application Support", "Slack")
		if exists(first) {
			return first
		}
		return second
	default: // linux and others
		home := getenv("HOME")
		var candidates []string
		if x := getenv("XDG_CONFIG_HOME"); x != "" {
			candidates = append(candidates, filepath.Join(x, "Slack"))
		}
		if x := getenv("XDG_CONFIG_DIR"); x != "" {
			candidates = append(candidates, filepath.Join(x, "Slack"))
		}
		candidates = append(candidates,
			filepath.Join(home, ".config", "Slack"),
			// flatpak (com.slack.Slack)
			filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack"),
			// snap
			filepath.Join(home, "snap", "slack", "current", ".config", "Slack"),
		)
		for _, c := range candidates {
			if exists(c) {
				return c
			}
		}
		// Nothing found: fall back to the primary location so the
		// not-found error references the conventional path.
		return candidates[0]
	}
}

// ConfigDir returns the Slack desktop config dir, or ErrDesktopNotFound if it
// does not exist on disk.
func ConfigDir() (string, error) {
	dir := configDirForOS(runtime.GOOS, os.Getenv, func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && info.IsDir()
	})
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", ErrDesktopNotFound
	}
	return dir, nil
}
