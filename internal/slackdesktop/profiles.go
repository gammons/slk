package slackdesktop

import (
	"os"
	"path/filepath"
	"runtime"
)

// ProfileCandidate describes a possible Slack desktop profile location.
type ProfileCandidate struct {
	Path      string // config dir
	Kind      string // "native" | "flatpak" | "snap"
	Exists    bool   // the dir exists
	HasCookie bool   // a Cookies DB exists under it
	Active    bool   // the profile slk actually uses (ConfigDir)
}

// profileCandidatePaths returns the known Slack desktop config-dir locations
// for a given OS. getenv is injected for testability. On Linux this includes
// Flatpak and Snap install locations in addition to the native dir, since
// reading the wrong (stale/signed-out) profile is a likely cause of mint
// failures (#111).
func profileCandidatePaths(goos string, getenv func(string) string) []ProfileCandidate {
	home := getenv("HOME")
	switch goos {
	case "windows":
		return []ProfileCandidate{{Path: filepath.Join(getenv("APPDATA"), "Slack"), Kind: "native"}}
	case "darwin":
		return []ProfileCandidate{
			{Path: filepath.Join(home, "Library", "Application Support", "Slack"), Kind: "native"},
			{Path: filepath.Join(home, "Library", "Containers", "com.tinyspeck.slackmacgap", "Data", "Library", "Application Support", "Slack"), Kind: "native"},
		}
	default: // linux
		return []ProfileCandidate{
			{Path: filepath.Join(home, ".config", "Slack"), Kind: "native"},
			{Path: filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack"), Kind: "flatpak"},
			{Path: filepath.Join(home, "snap", "slack", "current", ".config", "Slack"), Kind: "snap"},
		}
	}
}

// cookieDBPath returns the Cookies DB path under a Slack config dir for the OS.
func cookieDBPath(goos, configDir string) string {
	if goos == "windows" {
		return filepath.Join(configDir, "Network", "Cookies")
	}
	return filepath.Join(configDir, "Cookies")
}

// ProfileCandidates lists known Slack desktop profile locations for this OS,
// flagging which exist, which have a Cookies DB, and which one slk uses.
// Diagnostic aid for "wrong/stale profile" mint failures (#111).
func ProfileCandidates() []ProfileCandidate {
	active, _ := ConfigDir() // "" if none found; that's fine for comparison
	out := profileCandidatePaths(runtime.GOOS, os.Getenv)
	for i := range out {
		if info, err := os.Stat(out[i].Path); err == nil && info.IsDir() {
			out[i].Exists = true
		}
		if info, err := os.Stat(cookieDBPath(runtime.GOOS, out[i].Path)); err == nil && !info.IsDir() {
			out[i].HasCookie = true
		}
		out[i].Active = active != "" && out[i].Path == active
	}
	return out
}
