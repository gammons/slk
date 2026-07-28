package slackdesktop

import (
	"path/filepath"
	"testing"
)

func TestProfileCandidatePathsLinux(t *testing.T) {
	env := func(k string) string {
		if k == "HOME" {
			return "/home/x"
		}
		return ""
	}
	got := profileCandidatePaths("linux", env)
	want := map[string]string{
		"/home/x/.config/Slack":                         "native",
		"/home/x/.var/app/com.slack.Slack/config/Slack": "flatpak",
		"/home/x/snap/slack/current/.config/Slack":      "snap",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d candidates, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		if want[c.Path] != c.Kind {
			t.Errorf("path %q: kind=%q, want %q", c.Path, c.Kind, want[c.Path])
		}
	}
}

func TestCookieDBPath(t *testing.T) {
	if got := cookieDBPath("linux", "/cfg/Slack"); got != filepath.Join("/cfg/Slack", "Cookies") {
		t.Errorf("linux cookie path = %q", got)
	}
	if got, want := cookieDBPath("windows", "SlackDir"), filepath.Join("SlackDir", "Network", "Cookies"); got != want {
		t.Errorf("windows cookie path = %q, want %q", got, want)
	}
}
