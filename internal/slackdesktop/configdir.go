package slackdesktop

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// profileInfo describes a candidate Slack desktop profile directory.
type profileInfo struct {
	// lastUsed is the newest mtime among the profile's session files. It
	// stands in for "when was this Slack install last signed in with", and
	// is only meaningful when live is true.
	lastUsed time.Time
	// live reports whether the directory holds the files needed to pull a
	// session out of it.
	live bool
	// exists reports whether the directory is there at all. A dir that
	// exists but is not live is still the better thing to name in an error
	// than a path that was never created.
	exists bool
}

// profileProber reports whether a directory is a usable Slack profile and when
// it was last used. Injected into configDirForOS for testability.
type profileProber func(dir string) profileInfo

// profileInfoAt probes a real directory on disk. A profile only counts as live
// when it has both the workspace list and the cookie DB, since either one
// missing leaves us unable to authenticate.
func profileInfoAt(dir string) profileInfo {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return profileInfo{}
	}
	cookies, err := os.Stat(filepath.Join(dir, "Cookies"))
	if err != nil {
		return profileInfo{exists: true}
	}
	rootState, err := os.Stat(filepath.Join(dir, "storage", "root-state.json"))
	if err != nil {
		return profileInfo{exists: true}
	}

	newest := cookies.ModTime()
	if t := rootState.ModTime(); t.After(newest) {
		newest = t
	}
	// The leveldb dir is where the xoxc tokens live; a running Slack touches
	// it, so it is often the freshest signal of the three.
	if ldb, err := os.Stat(filepath.Join(dir, "Local Storage", "leveldb")); err == nil {
		if t := ldb.ModTime(); t.After(newest) {
			newest = t
		}
	}
	return profileInfo{lastUsed: newest, live: true, exists: true}
}

// configDirForOS computes the Slack desktop config dir for a given OS.
// getenv and probe are injected for testability.
//
// Several Slack packagings can leave a profile directory behind on one machine
// (a removed .deb next to a flatpak, an unsandboxed profile next to the App
// Store one). Preferring whichever path exists first picks the leftover just as
// happily as the live install, and a stale profile yields credentials that
// Slack rejects with invalid_auth. So among the profiles that look usable, take
// the most recently used one; candidate order only breaks ties.
func configDirForOS(goos string, getenv func(string) string, probe profileProber) string {
	var candidates []string
	switch goos {
	case "windows":
		// Slack on Windows has a single install location.
		return filepath.Join(getenv("APPDATA"), "Slack")
	case "darwin":
		home := getenv("HOME")
		candidates = []string{
			filepath.Join(home, "Library", "Application Support", "Slack"),
			filepath.Join(home, "Library", "Containers", "com.tinyspeck.slackmacgap", "Data", "Library", "Application Support", "Slack"),
		}
	default: // linux and others
		home := getenv("HOME")
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
	}

	best, firstExisting := "", ""
	var bestUsed time.Time
	for _, c := range candidates {
		info := probe(c)
		// Strictly After, so the earliest candidate wins a tie.
		if info.live && (best == "" || info.lastUsed.After(bestUsed)) {
			best, bestUsed = c, info.lastUsed
		}
		if info.exists && firstExisting == "" {
			firstExisting = c
		}
	}
	if best != "" {
		return best
	}
	// No profile has a session in it. Name a directory that is at least
	// there, so the caller's error points at the install the user has,
	// falling back to the conventional path when there is none.
	if firstExisting != "" {
		return firstExisting
	}
	return candidates[0]
}

// ConfigDir returns the Slack desktop config dir, or ErrDesktopNotFound if it
// does not exist on disk.
func ConfigDir() (string, error) {
	dir := configDirForOS(runtime.GOOS, os.Getenv, profileInfoAt)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", ErrDesktopNotFound
	}
	return dir, nil
}
