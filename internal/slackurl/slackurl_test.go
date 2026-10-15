package slackurl

import "testing"

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
