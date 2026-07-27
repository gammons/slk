package activityview

import (
	"testing"

	slack "github.com/gammons/slk/internal/slack"
)

func TestActivityGlyph(t *testing.T) {
	cases := map[string]string{
		"at_user":                  "@",
		"at_channel":               "@",
		"at_everyone":              "@",
		"at_user_group":            "@",
		"keyword":                  "@",
		"list_user_mentioned":      "@",
		"unjoined_channel_mention": "@",
		"channel":                  "@",
		"thread_v2":                "⚑",
		"message_reaction":         "☺",
		"dm":                       "✉",
		"bot_dm_bundle":            "✉",
		"something_unknown":        "•",
		"":                         "•",
	}
	for typ, want := range cases {
		if got := activityGlyph(typ); got != want {
			t.Errorf("activityGlyph(%q) = %q, want %q", typ, got, want)
		}
	}
}

func TestActivityDetail(t *testing.T) {
	cases := []struct {
		name string
		it   slack.ActivityItem
		want string
	}{
		{"reaction with name", slack.ActivityItem{Type: "message_reaction", Reaction: "tada"}, ":tada:"},
		{"reaction without name", slack.ActivityItem{Type: "message_reaction"}, ""},
		{"mention has no detail", slack.ActivityItem{Type: "at_user", Reaction: "tada"}, ""},
		{"dm has no detail", slack.ActivityItem{Type: "dm"}, ""},
	}
	for _, c := range cases {
		if got := activityDetail(c.it); got != c.want {
			t.Errorf("%s: activityDetail() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFormatRelTime(t *testing.T) {
	if got := formatRelTime(""); got != "" {
		t.Errorf("formatRelTime(empty) = %q, want empty", got)
	}
	if got := formatRelTime("not-a-ts"); got != "" {
		t.Errorf("formatRelTime(garbage) = %q, want empty", got)
	}
}

func TestSetItemsPreservesSelectionByKey(t *testing.T) {
	m := New(nil, "")
	m.SetItems([]slack.ActivityItem{
		{Key: "a"}, {Key: "b"}, {Key: "c"},
	})
	m.MoveDown()
	m.MoveDown() // select "c"
	if idx := m.SelectedIndex(); idx != 2 {
		t.Fatalf("SelectedIndex = %d, want 2", idx)
	}
	// Re-order; selection should follow "c" to its new position.
	m.SetItems([]slack.ActivityItem{
		{Key: "c"}, {Key: "a"}, {Key: "b"},
	})
	if idx := m.SelectedIndex(); idx != 0 {
		t.Fatalf("after reorder SelectedIndex = %d, want 0", idx)
	}
	it, ok := m.SelectedItem()
	if !ok || it.Key != "c" {
		t.Fatalf("SelectedItem = %+v ok=%v, want key c", it, ok)
	}
}

func TestToggleUnreadOnly(t *testing.T) {
	m := New(nil, "")
	if m.UnreadOnly() {
		t.Fatal("UnreadOnly should default false")
	}
	if got := m.ToggleUnreadOnly(); !got || !m.UnreadOnly() {
		t.Fatalf("ToggleUnreadOnly = %v, want true", got)
	}
	if got := m.ToggleUnreadOnly(); got || m.UnreadOnly() {
		t.Fatalf("ToggleUnreadOnly = %v, want false", got)
	}
}

func TestUnreadCount(t *testing.T) {
	m := New(nil, "")
	m.SetItems([]slack.ActivityItem{
		{Key: "a", IsUnread: true},
		{Key: "b"},
		{Key: "c", IsUnread: true},
	})
	if n := m.UnreadCount(); n != 2 {
		t.Errorf("UnreadCount = %d, want 2", n)
	}
}
