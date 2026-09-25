package activityview

import (
	"image/color"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/styles"
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
		it   core.ActivityItem
		want string
	}{
		{"reaction with name", core.ActivityItem{Type: "message_reaction", Reaction: "tada"}, ":tada:"},
		{"reaction without name", core.ActivityItem{Type: "message_reaction"}, ""},
		{"mention has no detail", core.ActivityItem{Type: "at_user", Reaction: "tada"}, ""},
		{"dm has no detail", core.ActivityItem{Type: "dm"}, ""},
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
	m.SetItems([]core.ActivityItem{
		{Key: "a"}, {Key: "b"}, {Key: "c"},
	})
	m.MoveDown()
	m.MoveDown() // select "c"
	if idx := m.SelectedIndex(); idx != 2 {
		t.Fatalf("SelectedIndex = %d, want 2", idx)
	}
	// Re-order; selection should follow "c" to its new position.
	m.SetItems([]core.ActivityItem{
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
	m.SetItems([]core.ActivityItem{
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
		it   core.ActivityItem
		body core.ActivityMessage
	}{
		{"newline body", core.ActivityItem{Type: "at_user", ChannelID: "C1", TS: "1.1", AuthorID: "U1"},
			core.ActivityMessage{Text: "line one\nline two\nline three", UserID: "U1"}},
		{"empty body", core.ActivityItem{Type: "thread_v2", ChannelID: "C1", TS: "1.1"},
			core.ActivityMessage{}},
		{"wide CJK", core.ActivityItem{Type: "dm", ChannelID: "C1", TS: "1.1", AuthorID: "U1"},
			core.ActivityMessage{Text: "あいうえお　かきくけこ　さしすせそ　たちつてと", UserID: "U1"}},
		{"absent body key", core.ActivityItem{Type: "message_reaction", ChannelID: "CX", TS: "9.9", Reaction: "tada", AuthorID: "U1"},
			core.ActivityMessage{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New(map[string]string{"U1": "alice"}, "")
			m.SetItems([]core.ActivityItem{c.it})
			m.SetBodies(map[string]core.ActivityMessage{
				core.ActivityMsgKey(c.it.ChannelID, c.it.TS): c.body,
			})
			l1, l2 := m.renderCard(c.it, 40, false)
			if strings.Contains(l1, "\n") || strings.Contains(l2, "\n") {
				t.Fatalf("card line contains a newline:\n1=%q\n2=%q", l1, l2)
			}
		})
	}
}

// renderRows lays cards out as two content lines each with one blank
// separator between adjacent cards (none after the last), matching the
// Threads view. No line may contain a newline.
func TestRenderRowsSeparatesCards(t *testing.T) {
	m := New(nil, "")
	m.SetItems([]core.ActivityItem{
		{Type: "at_user", ChannelID: "C1", TS: "1.1", Key: "a"},
		{Type: "dm", ChannelID: "C2", TS: "2.2", Key: "b"},
		{Type: "thread_v2", ChannelID: "C3", TS: "3.3", Key: "c"},
	})
	lines := m.renderRows(60)
	if want := 3*2 + 2; len(lines) != want {
		t.Fatalf("want %d lines (3 two-line cards + 2 separators), got %d", want, len(lines))
	}
	for i, l := range lines {
		if strings.Contains(l, "\n") {
			t.Fatalf("line %d contains a newline: %q", i, l)
		}
		isSeparator := i == 2 || i == 5
		if blank := strings.TrimSpace(ansi.Strip(l)) == ""; blank != isSeparator {
			t.Errorf("line %d blank=%v, want %v: %q", i, blank, isSeparator, ansi.Strip(l))
		}
	}
}

var sgrFgRe = regexp.MustCompile(`\x1b\[[0-9;]*?38;2;(\d+;\d+;\d+)[0-9;]*m`)

// fgOf returns the "r;g;b" truecolor foreground a style emits.
func fgOf(t *testing.T, c color.Color) string {
	t.Helper()
	m := sgrFgRe.FindStringSubmatch(lipgloss.NewStyle().Foreground(c).Render("x"))
	if m == nil {
		t.Fatalf("style for %v emitted no truecolor foreground", c)
	}
	return m[1]
}

// fgBefore returns the last truecolor foreground set before word in line.
func fgBefore(t *testing.T, line, word string) string {
	t.Helper()
	i := strings.Index(line, word)
	if i < 0 {
		t.Fatalf("%q not found in %q", word, ansi.Strip(line))
	}
	all := sgrFgRe.FindAllStringSubmatch(line[:i], -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1][1]
}

// The preview is the message itself, rendered like everywhere else in
// slk: emoji shortcodes become emoji, channel references resolve, and the
// text is in the normal text colour rather than the muted metadata one.
func TestRenderCard_PreviewRendersSlackMarkup(t *testing.T) {
	m := New(map[string]string{"U1": "alice"}, "")
	m.SetChannelNames(map[string]string{"C1": "general", "C9": "hiring"})
	it := core.ActivityItem{Type: "at_user", ChannelID: "C1", TS: "1.1", AuthorID: "U1"}
	m.SetItems([]core.ActivityItem{it})
	m.SetBodies(map[string]core.ActivityMessage{
		core.ActivityMsgKey("C1", "1.1"): {Text: ":wave: hello see <#C9>", UserID: "U1"},
	})

	_, l2 := m.renderCard(it, 80, false)
	plain := ansi.Strip(l2)
	for _, want := range []string{"👋", "hello", "#hiring"} {
		if !strings.Contains(plain, want) {
			t.Errorf("preview missing %q: %q", want, plain)
		}
	}
	for _, raw := range []string{":wave:", "<#C9"} {
		if strings.Contains(plain, raw) {
			t.Errorf("preview shows raw markup %q: %q", raw, plain)
		}
	}
	if text, muted := fgOf(t, styles.TextPrimary), fgOf(t, styles.TextMuted); text == muted {
		t.Fatalf("theme's text and muted colours are equal (%s); test cannot tell them apart", text)
	} else if got := fgBefore(t, l2, "hello"); got != text {
		t.Errorf("preview text colour = %s, want the text colour %s (muted is %s)", got, text, muted)
	}
}

var sgrBgRe = regexp.MustCompile(`\x1b\[[0-9;]*?48;2;(\d+;\d+;\d+)[0-9;]*m`)

// The renderer restores the theme background after every styled span.
// On the selected card that must be the selection tint instead, or the
// text after a link or mention loses the highlight.
func TestRenderCard_SelectedPreviewKeepsSelectionTint(t *testing.T) {
	m := New(nil, "")
	m.SetChannelNames(map[string]string{"C9": "hiring"})
	it := core.ActivityItem{Type: "at_user", ChannelID: "C1", TS: "1.1"}
	m.SetItems([]core.ActivityItem{it})
	m.SetBodies(map[string]core.ActivityMessage{
		core.ActivityMsgKey("C1", "1.1"): {Text: "see <#C9> after"},
	})

	_, l2 := m.renderCard(it, 60, true)
	i := strings.Index(l2, "after")
	if i < 0 {
		t.Fatalf("preview missing text: %q", ansi.Strip(l2))
	}
	bgs := sgrBgRe.FindAllStringSubmatch(l2[:i], -1)
	if len(bgs) == 0 {
		t.Fatal("no background set before the text")
	}
	tint := sgrBgRe.FindStringSubmatch(lipgloss.NewStyle().Background(styles.SelectionTintColor(false)).Render("x"))
	if got := bgs[len(bgs)-1][1]; got != tint[1] {
		t.Errorf("background behind text after a channel link = %s, want the selection tint %s", got, tint[1])
	}
}

// A reaction card leads with the reaction itself, rendered as an emoji.
func TestRenderCard_ReactionRendersEmoji(t *testing.T) {
	m := New(nil, "")
	it := core.ActivityItem{Type: "message_reaction", ChannelID: "C1", TS: "1.1", Reaction: "eyes"}
	m.SetItems([]core.ActivityItem{it})

	_, l2 := m.renderCard(it, 80, false)
	plain := ansi.Strip(l2)
	if !strings.Contains(plain, "👀") || strings.Contains(plain, ":eyes:") {
		t.Errorf("reaction preview = %q, want the 👀 emoji and no :eyes: shortcode", plain)
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
	dm := m.contextLabel(core.ActivityItem{Type: "dm", ChannelID: "C1"})
	if strings.Contains(dm, "general") || strings.Contains(dm, "#") {
		t.Fatalf("DM context should omit channel, got %q", dm)
	}
	mention := m.contextLabel(core.ActivityItem{Type: "at_user", ChannelID: "C1"})
	if !strings.Contains(mention, "#general") {
		t.Fatalf("mention context should contain #general, got %q", mention)
	}
}

// thread_v2 / dm refs carry no author on the feed item; the card falls back
// to the hydrated message's user so line 1 still names someone.
func TestRenderCard_AuthorFallbackToBody(t *testing.T) {
	m := New(map[string]string{"U9": "alice"}, "")
	it := core.ActivityItem{Type: "thread_v2", ChannelID: "C1", TS: "1.1"} // AuthorID == ""
	m.SetItems([]core.ActivityItem{it})
	m.SetBodies(map[string]core.ActivityMessage{
		core.ActivityMsgKey("C1", "1.1"): {Text: "the reply", UserID: "U9"},
	})
	l1, _ := m.renderCard(it, 60, false)
	if !strings.Contains(l1, "alice") {
		t.Fatalf("author should fall back to hydrated body user 'alice', got line1=%q", l1)
	}
}

// A click on either line of a card selects that card; a click on a
// separator or past the last card selects nothing.
func TestClickAtStride(t *testing.T) {
	m := New(nil, "")
	m.SetItems([]core.ActivityItem{
		{Type: "at_user", ChannelID: "C1", TS: "1.1", Key: "a"},
		{Type: "dm", ChannelID: "C2", TS: "2.2", Key: "b"},
		{Type: "thread_v2", ChannelID: "C3", TS: "3.3", Key: "c"},
	})
	// Card 0 occupies rows 0,1; separator 2; card 1 rows 3,4; separator
	// 5; card 2 rows 6,7.
	for _, tc := range []struct {
		rowY    int
		wantOK  bool
		wantSel int
	}{
		{0, true, 0}, {1, true, 0},
		{2, false, 0},
		{3, true, 1}, {4, true, 1},
		{5, false, 0},
		{6, true, 2}, {7, true, 2},
		{8, false, 0}, {-1, false, 0},
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
