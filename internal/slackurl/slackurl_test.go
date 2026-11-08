package slackurl

import (
	"testing"

	"github.com/gammons/slk/internal/ids"
)

func TestParse_Valid(t *testing.T) {
	pl, ok := Parse("https://truelist-workspace.slack.com/archives/C054JFCBN69/p1779284733270139")
	if !ok {
		t.Fatal("expected ok")
	}
	if pl.Subdomain != "truelist-workspace" {
		t.Errorf("Subdomain = %q", pl.Subdomain)
	}
	if string(pl.ChannelID) != "C054JFCBN69" {
		t.Errorf("ChannelID = %q", pl.ChannelID)
	}
	if string(pl.MessageTS) != "1779284733.270139" {
		t.Errorf("MessageTS = %q", pl.MessageTS)
	}
	if pl.ThreadTS != "" {
		t.Errorf("ThreadTS = %q, want empty", pl.ThreadTS)
	}
}

func TestParse_ThreadTSAndCid(t *testing.T) {
	pl, ok := Parse("https://example.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999")
	if !ok {
		t.Fatal("expected ok")
	}
	if string(pl.ThreadTS) != "1700000000.000100" {
		t.Errorf("ThreadTS = %q", pl.ThreadTS)
	}
	if string(pl.ChannelID) != "C999" {
		t.Errorf("ChannelID = %q (path channel ID wins; cid ignored)", pl.ChannelID)
	}
	if string(pl.MessageTS) != "1700000050.000400" {
		t.Errorf("MessageTS = %q", pl.MessageTS)
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := []string{
		"https://github.com/foo/bar",
		"http://example.slack.com/archives/C999/p1700000050000400",
		"https://slack.com/archives/C999/p1700000050000400",
		"https://example.slack.com/archives/C999",
		"https://example.slack.com/archives/C999/p12345",
		"https://example.slack.com/archives/C999/pabcdef",
		"https://example.slack.com/messages/C999/p1700000050000400",
		"mailto:foo@example.com",
		"not a url at all",
		"",
	}
	for _, c := range cases {
		if _, ok := Parse(c); ok {
			t.Errorf("Parse(%q) ok = true, want false", c)
		}
	}
}

func TestHuddleURL(t *testing.T) {
	cases := []struct{ teamID, channelID, want string }{
		{"T04T4TH8W", "C054JFCBN69", "https://app.slack.com/huddle/T04T4TH8W/C054JFCBN69"},
		{"E01ABCDEF", "D0123456789", "https://app.slack.com/huddle/E01ABCDEF/D0123456789"},
		{"T1", "G123", "https://app.slack.com/huddle/T1/G123"},
	}
	for _, c := range cases {
		if got := HuddleURL(c.teamID, c.channelID); got != c.want {
			t.Errorf("HuddleURL(%q, %q) = %q, want %q", c.teamID, c.channelID, got, c.want)
		}
	}
}

func TestBuild(t *testing.T) {
	cases := []struct {
		name     string
		teamURL  string
		channel  ids.ChannelID
		ts       ids.MessageTS
		threadTS ids.ThreadTS
		want     string
		ok       bool
	}{
		{"channel message", "https://acme.slack.com/", "C123", "1700000001.000200", "", "https://acme.slack.com/archives/C123/p1700000001000200", true},
		{"enterprise grid", "https://acme.enterprise.slack.com/", "C123", "1700000001.000200", "", "https://acme.enterprise.slack.com/archives/C123/p1700000001000200", true},
		{"thread reply", "https://acme.slack.com/", "C999", "1700000050.000400", "1700000000.000100",
			"https://acme.slack.com/archives/C999/p1700000050000400?thread_ts=1700000000.000100&cid=C999", true},
		// A thread parent carries its own ts as thread_ts: no query.
		{"thread parent", "https://acme.slack.com/", "C999", "1700000000.000100", "1700000000.000100", "https://acme.slack.com/archives/C999/p1700000000000100", true},
		{"no trailing slash", "https://acme.slack.com", "C1", "1700000001.000200", "", "https://acme.slack.com/archives/C1/p1700000001000200", true},
		{"not slack", "https://evil.example/", "C1", "1700000001.000200", "", "", false},
		{"lookalike", "https://slack.com.evil.example/", "C1", "1700000001.000200", "", "", false},
		{"bare slack.com", "https://slack.com/", "C1", "1700000001.000200", "", "", false},
		{"http", "http://acme.slack.com/", "C1", "1700000001.000200", "", "", false},
		{"empty url", "", "C1", "1700000001.000200", "", "", false},
		{"no channel", "https://acme.slack.com/", "", "1700000001.000200", "", "", false},
		{"bad ts", "https://acme.slack.com/", "C1", "1.0", "", "", false},
		{"bad thread ts is left off", "https://acme.slack.com/", "C1", "1700000001.000200", "x", "https://acme.slack.com/archives/C1/p1700000001000200", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Build(c.teamURL, c.channel, c.ts, c.threadTS)
			if got != c.want || ok != c.ok {
				t.Errorf("Build = (%q, %v), want (%q, %v)", got, ok, c.want, c.ok)
			}
			if !ok {
				return
			}
			// Parse reads back what Build wrote.
			p, ok := Parse(got)
			if !ok || p.ChannelID != c.channel || p.MessageTS != c.ts {
				t.Errorf("Parse(%q) = %+v, %v", got, p, ok)
			}
		})
	}
}
