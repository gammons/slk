package slackdesktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// probeFrom builds a profileProber from a map of dir -> last-used time. Any
// directory absent from the map probes as not a usable profile.
func probeFrom(live map[string]time.Time) profileProber {
	return func(dir string) profileInfo {
		if t, ok := live[dir]; ok {
			return profileInfo{lastUsed: t, live: true, exists: true}
		}
		return profileInfo{}
	}
}

func TestConfigDirForOS(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"linux", map[string]string{"HOME": "/home/x"}, "/home/x/.config/Slack"},
		{"linux", map[string]string{"HOME": "/home/x", "XDG_CONFIG_DIR": "/cfg"}, "/cfg/Slack"},
		{"windows", map[string]string{"APPDATA": `C:\Users\x\AppData\Roaming`}, filepath.Join(`C:\Users\x\AppData\Roaming`, "Slack")},
	}
	for _, c := range cases {
		got := configDirForOS(c.goos, env(c.env), probeFrom(nil))
		if got != c.want {
			t.Errorf("configDirForOS(%s) = %q, want %q", c.goos, got, c.want)
		}
	}
}

func TestConfigDirForOSDarwinPrefersLiveProfile(t *testing.T) {
	home := "/Users/x"
	first := filepath.Join(home, "Library", "Application Support", "Slack")
	sandboxed := filepath.Join(home, "Library", "Containers", "com.tinyspeck.slackmacgap", "Data", "Library", "Application Support", "Slack")
	getenv := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	stale := time.Date(2025, 6, 5, 0, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)

	if got := configDirForOS("darwin", getenv, probeFrom(map[string]time.Time{first: stale})); got != first {
		t.Errorf("darwin config dir = %q, want %q", got, first)
	}
	// A leftover unsandboxed profile must not shadow the App Store one.
	if got := configDirForOS("darwin", getenv, probeFrom(map[string]time.Time{first: stale, sandboxed: fresh})); got != sandboxed {
		t.Errorf("darwin config dir = %q, want %q", got, sandboxed)
	}
}

func TestConfigDirForOSLinuxPackaging(t *testing.T) {
	home := "/home/x"
	native := filepath.Join(home, ".config", "Slack")
	flatpak := filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack")
	snap := filepath.Join(home, "snap", "slack", "current", ".config", "Slack")
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		env  map[string]string
		live map[string]time.Time
		want string
	}{
		{
			name: "flatpak only",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{flatpak: now},
			want: flatpak,
		},
		{
			name: "snap only",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{snap: now},
			want: snap,
		},
		{
			name: "native wins ties against flatpak",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{native: now, flatpak: now},
			want: native,
		},
		{
			name: "flatpak wins ties against snap",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{flatpak: now, snap: now},
			want: flatpak,
		},
		{
			name: "xdg config home wins ties against flatpak",
			env:  map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/cfg"},
			live: map[string]time.Time{"/cfg/Slack": now, flatpak: now},
			want: "/cfg/Slack",
		},
		{
			name: "xdg dir set but not a profile falls through to flatpak",
			env:  map[string]string{"HOME": home, "XDG_CONFIG_DIR": "/cfg"},
			live: map[string]time.Time{flatpak: now},
			want: flatpak,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := configDirForOS("linux", func(k string) string { return c.env[k] }, probeFrom(c.live))
			if got != c.want {
				t.Errorf("configDirForOS(linux) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestConfigDirForOSLinuxPrefersFreshestProfile(t *testing.T) {
	home := "/home/x"
	native := filepath.Join(home, ".config", "Slack")
	flatpak := filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack")
	snap := filepath.Join(home, "snap", "slack", "current", ".config", "Slack")

	// A leftover native profile from an uninstalled .deb must not shadow the
	// flatpak profile actually in use. Both directories exist and both hold a
	// full set of session files, so only recency tells them apart.
	stale := time.Date(2025, 6, 5, 0, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 8, 21, 21, 23, 0, 0, time.UTC)

	cases := []struct {
		name string
		env  map[string]string
		live map[string]time.Time
		want string
	}{
		{
			name: "stale native loses to fresh flatpak",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{native: stale, flatpak: fresh},
			want: flatpak,
		},
		{
			name: "fresh native beats stale flatpak",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{native: fresh, flatpak: stale},
			want: native,
		},
		{
			name: "stale snap loses to fresh flatpak",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{snap: stale, flatpak: fresh},
			want: flatpak,
		},
		{
			name: "a native dir that is not a profile never wins",
			env:  map[string]string{"HOME": home},
			live: map[string]time.Time{flatpak: stale},
			want: flatpak,
		},
		{
			name: "xdg config home competes on recency like any native path",
			env:  map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/cfg"},
			live: map[string]time.Time{"/cfg/Slack": stale, flatpak: fresh},
			want: flatpak,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := configDirForOS("linux", func(k string) string { return c.env[k] }, probeFrom(c.live))
			if got != c.want {
				t.Errorf("configDirForOS(linux) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestProfileInfoAtRequiresSessionFiles(t *testing.T) {
	dir := t.TempDir()
	if got := profileInfoAt(dir); got.live {
		t.Errorf("empty dir probed live, want not live")
	}

	writeFile(t, filepath.Join(dir, "storage", "root-state.json"), `{"workspaces":{}}`)
	if got := profileInfoAt(dir); got.live {
		t.Errorf("dir with only root-state.json probed live, want not live")
	}

	writeFile(t, filepath.Join(dir, "Cookies"), "x")
	got := profileInfoAt(dir)
	if !got.live {
		t.Fatalf("dir with root-state.json and Cookies probed not live, want live")
	}
	if got.lastUsed.IsZero() {
		t.Errorf("lastUsed is zero, want the Cookies mtime")
	}
}

func TestProfileInfoAtUsesNewestSessionFile(t *testing.T) {
	dir := t.TempDir()
	rootState := filepath.Join(dir, "storage", "root-state.json")
	cookies := filepath.Join(dir, "Cookies")
	writeFile(t, rootState, `{"workspaces":{}}`)
	writeFile(t, cookies, "x")

	old := time.Now().Add(-72 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	chtimes(t, rootState, recent)
	chtimes(t, cookies, old)

	got := profileInfoAt(dir)
	if !got.live {
		t.Fatalf("probed not live, want live")
	}
	// root-state.json is the newer of the two, so it sets lastUsed.
	if got.lastUsed.Before(recent.Add(-time.Minute)) {
		t.Errorf("lastUsed = %v, want about %v (the newest session file)", got.lastUsed, recent)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func chtimes(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}
