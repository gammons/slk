package activityview

import (
	"strings"
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

// A card is always exactly cardStride (2) lines and neither line contains a
// newline — the flat-list windowing / snap / click math depends on it. A
// body with embedded newlines must be collapsed, not split.
func TestRenderCardAlwaysTwoLines(t *testing.T) {
	cases := []struct {
		name string
		it   slack.ActivityItem
		body slack.ActivityMessage
	}{
		{"newline body", slack.ActivityItem{Type: "at_user", ChannelID: "C1", TS: "1.1", AuthorID: "U1"},
			slack.ActivityMessage{Text: "line one\nline two\nline three", UserID: "U1"}},
		{"empty body", slack.ActivityItem{Type: "thread_v2", ChannelID: "C1", TS: "1.1"},
			slack.ActivityMessage{}},
		{"wide CJK", slack.ActivityItem{Type: "dm", ChannelID: "C1", TS: "1.1", AuthorID: "U1"},
			slack.ActivityMessage{Text: "あいうえお　かきくけこ　さしすせそ　たちつてと", UserID: "U1"}},
		{"absent body key", slack.ActivityItem{Type: "message_reaction", ChannelID: "CX", TS: "9.9", Reaction: "tada", AuthorID: "U1"},
			slack.ActivityMessage{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New(map[string]string{"U1": "alice"}, "")
			m.SetItems([]slack.ActivityItem{c.it})
			m.SetBodies(map[string]slack.ActivityMessage{
				slack.ActivityMsgKey(c.it.ChannelID, c.it.TS): c.body,
			})
			l1, l2 := m.renderCard(c.it, 40, false)
			if strings.Contains(l1, "\n") || strings.Contains(l2, "\n") {
				t.Fatalf("card line contains a newline:\n1=%q\n2=%q", l1, l2)
			}
		})
	}
}

// renderRows flattens each item into exactly cardStride lines, none with a
// newline.
func TestRenderRowsStride(t *testing.T) {
	m := New(nil, "")
	m.SetItems([]slack.ActivityItem{
		{Type: "at_user", ChannelID: "C1", TS: "1.1", Key: "a"},
		{Type: "dm", ChannelID: "C2", TS: "2.2", Key: "b"},
		{Type: "thread_v2", ChannelID: "C3", TS: "3.3", Key: "c"},
	})
	lines := m.renderRows(60)
	if len(lines) != 3*cardStride {
		t.Fatalf("want %d lines (3 cards x %d), got %d", 3*cardStride, cardStride, len(lines))
	}
	for i, l := range lines {
		if strings.Contains(l, "\n") {
			t.Fatalf("line %d contains a newline: %q", i, l)
		}
	}
}

func TestContextVerb(t *testing.T) {
	cases := map[string]string{
		"at_user": "Mention", "at_channel": "Mention", "at_everyone": "Mention",
		"at_user_group": "Mention", "keyword": "Mention", "list_user_mentioned": "Mention",
		"thread_v2": "Thread", "message_reaction": "Reacted",
		"dm": "DM", "bot_dm_bundle": "DM",
		"": "", "something_new": "",
	}
	for typ, want := range cases {
		if got := contextVerb(typ); got != want {
			t.Errorf("contextVerb(%q) = %q, want %q", typ, got, want)
		}
	}
}

// A DM's context omits the channel; a mention's context includes "#<channel>".
func TestContextLabel(t *testing.T) {
	m := New(nil, "")
	m.SetChannelNames(map[string]string{"C1": "general"})
	dm := m.contextLabel(slack.ActivityItem{Type: "dm", ChannelID: "C1"})
	if strings.Contains(dm, "general") || strings.Contains(dm, "#") {
		t.Fatalf("DM context should omit channel, got %q", dm)
	}
	mention := m.contextLabel(slack.ActivityItem{Type: "at_user", ChannelID: "C1"})
	if !strings.Contains(mention, "#general") {
		t.Fatalf("mention context should contain #general, got %q", mention)
	}
}

// thread_v2 / dm refs carry no author on the feed item; the card falls back
// to the hydrated message's user so line 1 still names someone.
func TestRenderCard_AuthorFallbackToBody(t *testing.T) {
	m := New(map[string]string{"U9": "alice"}, "")
	it := slack.ActivityItem{Type: "thread_v2", ChannelID: "C1", TS: "1.1"} // AuthorID == ""
	m.SetItems([]slack.ActivityItem{it})
	m.SetBodies(map[string]slack.ActivityMessage{
		slack.ActivityMsgKey("C1", "1.1"): {Text: "the reply", UserID: "U9"},
	})
	l1, _ := m.renderCard(it, 60, false)
	if !strings.Contains(l1, "alice") {
		t.Fatalf("author should fall back to hydrated body user 'alice', got line1=%q", l1)
	}
}

// With cardStride == 2, a click on either visual line of a card selects the
// same item; a click past the last card returns false.
func TestClickAtStride(t *testing.T) {
	m := New(nil, "")
	m.SetItems([]slack.ActivityItem{
		{Type: "at_user", ChannelID: "C1", TS: "1.1", Key: "a"},
		{Type: "dm", ChannelID: "C2", TS: "2.2", Key: "b"},
		{Type: "thread_v2", ChannelID: "C3", TS: "3.3", Key: "c"},
	})
	// Card 0 occupies visual rows 0,1; card 1 rows 2,3; card 2 rows 4,5.
	for _, tc := range []struct {
		rowY    int
		wantOK  bool
		wantSel int
	}{
		{0, true, 0}, {1, true, 0},
		{2, true, 1}, {3, true, 1},
		{4, true, 2}, {5, true, 2},
		{6, false, 0}, {-1, false, 0},
	} {
		ok := m.ClickAt(tc.rowY)
		if ok != tc.wantOK {
			t.Fatalf("ClickAt(%d) ok=%v, want %v", tc.rowY, ok, tc.wantOK)
		}
		if ok && m.SelectedIndex() != tc.wantSel {
			t.Fatalf("ClickAt(%d) selected=%d, want %d", tc.rowY, m.SelectedIndex(), tc.wantSel)
		}
	}
}
