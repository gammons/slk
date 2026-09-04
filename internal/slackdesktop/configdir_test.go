package slackdesktop

import (
	"path/filepath"
	"testing"
)

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
		got := configDirForOS(c.goos, env(c.env), func(string) bool { return false })
		if got != c.want {
			t.Errorf("configDirForOS(%s) = %q, want %q", c.goos, got, c.want)
		}
	}
}

func TestConfigDirForOSDarwinPrefersFirstExisting(t *testing.T) {
	home := "/Users/x"
	first := filepath.Join(home, "Library", "Application Support", "Slack")
	got := configDirForOS("darwin", func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, func(p string) bool { return p == first })
	if got != first {
		t.Errorf("darwin config dir = %q, want %q", got, first)
	}
}

func TestConfigDirForOSLinuxPackaging(t *testing.T) {
	home := "/home/x"
	native := filepath.Join(home, ".config", "Slack")
	flatpak := filepath.Join(home, ".var", "app", "com.slack.Slack", "config", "Slack")
	snap := filepath.Join(home, "snap", "slack", "current", ".config", "Slack")

	cases := []struct {
		name   string
		env    map[string]string
		exists func(string) bool
		want   string
	}{
		{
			name:   "flatpak only",
			env:    map[string]string{"HOME": home},
			exists: func(p string) bool { return p == flatpak },
			want:   flatpak,
		},
		{
			name:   "snap only",
			env:    map[string]string{"HOME": home},
			exists: func(p string) bool { return p == snap },
			want:   snap,
		},
		{
			name:   "native preferred over flatpak",
			env:    map[string]string{"HOME": home},
			exists: func(p string) bool { return p == native || p == flatpak },
			want:   native,
		},
		{
			name:   "flatpak preferred over snap",
			env:    map[string]string{"HOME": home},
			exists: func(p string) bool { return p == flatpak || p == snap },
			want:   flatpak,
		},
		{
			name:   "xdg config home preferred when present",
			env:    map[string]string{"HOME": home, "XDG_CONFIG_HOME": "/cfg"},
			exists: func(p string) bool { return p == "/cfg/Slack" || p == flatpak },
			want:   "/cfg/Slack",
		},
		{
			name:   "xdg dir set but missing falls through to flatpak",
			env:    map[string]string{"HOME": home, "XDG_CONFIG_DIR": "/cfg"},
			exists: func(p string) bool { return p == flatpak },
			want:   flatpak,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := configDirForOS("linux", func(k string) string { return c.env[k] }, c.exists)
			if got != c.want {
				t.Errorf("configDirForOS(linux) = %q, want %q", got, c.want)
			}
		})
	}
}
