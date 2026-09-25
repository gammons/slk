package activityview

import (
	"image/color"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

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

// bgPerCell walks line's escape sequences and returns, for each printable
// rune, the truecolor background in effect ("" when none is set, i.e. the
// terminal default). OSC sequences (hyperlinks) are skipped.
func bgPerCell(line string) []string {
	var out []string
	bg := ""
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], "\x1b]") {
			end := strings.Index(line[i:], "\x1b\\")
			if end < 0 {
				break
			}
			i += end + 2
			continue
		}
		if strings.HasPrefix(line[i:], "\x1b[") {
			end := strings.IndexByte(line[i:], 'm')
			params := strings.Split(line[i+2:i+end], ";")
			for j := 0; j < len(params); j++ {
				switch params[j] {
				case "", "0":
					bg = ""
				case "38", "48":
					if j+1 < len(params) && params[j+1] == "2" && j+4 < len(params) {
						if params[j] == "48" {
							bg = strings.Join(params[j+2:j+5], ";")
						}
						j += 4
					} else if j+1 < len(params) && params[j+1] == "5" {
						j += 2
					}
				}
			}
			i += end + 1
			continue
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		out = append(out, bg)
		i += size
	}
	return out
}

// Every cell of a card carries the row background (the selection tint
// when selected, the theme background otherwise), including
// text that follows a styled span (the bold author, a muted "You:", a
// channel link). A span's reset otherwise drops the row back to the
// terminal's default background.
func TestRenderCard_CardBackgroundIsUnbroken(t *testing.T) {
	m := New(map[string]string{"U2": "alice"}, "U1")
	m.SetChannelNames(map[string]string{"D1": "Alex", "C1": "general", "C9": "hiring"})
	m.SetChannelTypes(map[string]string{"D1": "dm", "C1": "channel"})
	tint := sgrBgRe.FindStringSubmatch(lipgloss.NewStyle().Background(styles.SelectionTintColor(false)).Render("x"))[1]

	for _, c := range []struct {
		name string
		it   core.ActivityItem
		body core.ActivityMessage
	}{
		{"your DM", core.ActivityItem{Type: "dm", ChannelID: "D1", TS: "1.1", IsUnread: true, FeedTS: "1700000000.0"},
			core.ActivityMessage{Text: "thanks", UserID: "U1"}},
		{"mention with link", core.ActivityItem{Type: "at_user", ChannelID: "C1", TS: "1.1", AuthorID: "U2"},
			core.ActivityMessage{Text: "see <#C9> after :wave: *bold* end", UserID: "U2"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m.SetItems([]core.ActivityItem{c.it})
			m.SetBodies(map[string]core.ActivityMessage{core.ActivityMsgKey(c.it.ChannelID, c.it.TS): c.body})
			themeBg := sgrBgRe.FindStringSubmatch(lipgloss.NewStyle().Background(styles.Background).Render("x"))[1]
			for _, sel := range []struct {
				selected bool
				want     string
			}{{true, tint}, {false, themeBg}} {
				l1, l2 := m.renderCard(c.it, 80, sel.selected)
				for n, line := range []string{l1, l2} {
					for i, bg := range bgPerCell(line) {
						if bg != sel.want {
							t.Errorf("selected=%v line %d cell %d background = %q, want %s\n%q",
								sel.selected, n+1, i, bg, sel.want, ansi.Strip(line))
							break
						}
					}
				}
			}
		})
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

// The context names the conversation the way the rest of slk does: "#"
// only for public channels, "◆" for private ones, "●" for group DMs, and
// just "DM" for a 1:1 DM (the other person is already on the card).
func TestContextLabel_ByConversationType(t *testing.T) {
	m := New(nil, "")
	m.SetChannelNames(map[string]string{
		"C1": "general", "G1": "secret", "D1": "Drew Gilliam", "M1": "Pav, Grant Ammons, Tom Hudson",
	})
	m.SetChannelTypes(map[string]string{
		"C1": "channel", "G1": "private", "D1": "dm", "M1": "group_dm",
	})
	cases := []struct {
		name, typ, ch, want string
	}{
		{"public channel", "at_user", "C1", "Mention in #general"},
		{"private channel", "at_user", "G1", "Mention in ◆ secret"},
		{"1:1 DM", "message_reaction", "D1", "Reacted in DM"},
		{"group DM", "at_user", "M1", "Mention in ● Pav, Grant Ammons, Tom Hudson"},
		{"unknown type falls back to #", "at_user", "CX", "Mention in #CX"},
	}
	for _, c := range cases {
		got := ansi.Strip(m.contextLabel(core.ActivityItem{Type: c.typ, ChannelID: c.ch}))
		if got != c.want {
			t.Errorf("%s: contextLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

// A DM row is headed by the conversation (the other person), not by
// whoever sent the latest message; when that was you, the preview says
// so, as Slack does.
func TestRenderCard_DMHeadedByConversation(t *testing.T) {
	m := New(map[string]string{"U1": "Alex Lazar", "USELF": "Grant"}, "USELF")
	m.SetChannelNames(map[string]string{"D1": "Alex Lazar"})
	m.SetChannelTypes(map[string]string{"D1": "dm"})

	for _, c := range []struct {
		name, from, wantPrefix string
	}{
		{"latest from you", "USELF", "You: "},
		{"latest from them", "U1", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			it := core.ActivityItem{Type: "dm", ChannelID: "D1", TS: "1.1"}
			m.SetItems([]core.ActivityItem{it})
			m.SetBodies(map[string]core.ActivityMessage{
				core.ActivityMsgKey("D1", "1.1"): {Text: "thanks", UserID: c.from},
			})
			l1, l2 := m.renderCard(it, 80, false)
			head := ansi.Strip(l1)
			if !strings.Contains(head, "Alex Lazar") || strings.Contains(head, "me ") {
				t.Errorf("header = %q, want it headed by the conversation, not the sender", head)
			}
			body := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(l2), "▌"))
			if want := c.wantPrefix + "thanks"; !strings.HasPrefix(body, want) {
				t.Errorf("preview = %q, want it to start with %q", body, want)
			}
			if got, want := fgBefore(t, l2, "thanks"), fgOf(t, styles.TextPrimary); got != want {
				t.Errorf("message colour = %s, want the text colour %s", got, want)
			}
		})
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
