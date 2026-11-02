package sidebar

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
)

// A section header must fit the one line its row is counted as. These
// tests scan View() like the rest of the package; a header that wraps
// shows up as its text split over two lines, so the exact stripped
// header and the line after it prove the fit.

const longSectionName = "Engineering announcements and rollouts"

// sectionModel is a Slack-mode sidebar with one section "Docs" carrying
// the given emoji shortcode, holding one channel.
func sectionModel(emojiCode string) Model {
	return namedSectionModel("Docs", emojiCode, cache.ReadState{})
}

// longSectionModel is sectionModel with a long section name and the
// given read state for its channel.
func longSectionModel(emojiCode string, rs cache.ReadState) Model {
	return namedSectionModel(longSectionName, emojiCode, rs)
}

func namedSectionModel(name, emojiCode string, rs cache.ReadState) Model {
	m := New([]ChannelItem{{ID: "C1", Name: "handbook", Type: "channel", Section: "S1"}})
	m.SetSectionsProvider(&fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "S1", Name: name, Emoji: emojiCode, Type: "standard"}},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": rs}
	})
	return m
}

// selectHeader moves the cursor onto the first section header.
func selectHeader(t *testing.T, m *Model) {
	t.Helper()
	m.SetFocused(true)
	for range 50 {
		if _, ok := m.IsSectionHeaderSelected(); ok {
			return
		}
		m.MoveDown()
	}
	t.Fatal("no section header to select")
}

// lineAfter returns the stripped, trimmed line after the first line
// containing marker.
func lineAfter(t *testing.T, view, marker string) string {
	t.Helper()
	lines := strings.Split(view, "\n")
	for i, l := range lines {
		if strings.Contains(l, marker) && i+1 < len(lines) {
			return strings.TrimSpace(ansiRe.ReplaceAllString(lines[i+1], ""))
		}
	}
	t.Fatalf("no line after %q:\n%s", marker, view)
	return ""
}

// assertFits fails when the line holding marker is wider than width.
func assertFits(t *testing.T, view, marker string, width int) {
	t.Helper()
	if w := lipgloss.Width(rowFor(t, view, marker)); w > width {
		t.Errorf("header line is %d wide in a %d-wide sidebar", w, width)
	}
}

// assertOneLinePerRow fails when any line of view is wider than width
// or the view does not have exactly height lines: a header that wraps
// adds a line to a row counted as one.
func assertOneLinePerRow(t *testing.T, view string, height, width int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Errorf("view has %d lines, want %d:\n%s", len(lines), height, view)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > width {
			t.Errorf("line is %d wide in a %d-wide sidebar: %q", w, width, l)
		}
	}
}

// A long name at the minimum sidebar width truncates instead of
// wrapping: the header is one line and the channel row follows it.
func TestHeaderFit_LongNameStaysOneLine(t *testing.T) {
	m := longSectionModel("", cache.ReadState{})
	view := m.View(20, 20)

	// 20 wide: 16 columns of label, 4 of chrome, 12 for the name
	// including the "…".
	if got, want := headerFor(t, view, "▾ Eng"), "▾ Engineering…"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
	if got := lineAfter(t, view, "▾ Eng"); got != "# handbook" {
		t.Errorf("line after the header = %q, want the channel row: the header wrapped\n%s", got, view)
	}
	assertFits(t, view, "▾ Eng", 20)
}

// The same for a config-mode section (no Slack sections provider): the
// view keeps exactly the height it was asked for. Adapted from
// TestSectionHeader_LongNameDoesNotWrap in gammons/slk#315.
func TestHeaderFit_ConfigSectionLongNameDoesNotWrap(t *testing.T) {
	longName := "A Very Long Custom Section Name That Overflows"
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel", Section: longName, SectionOrder: 1},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	view := m.View(15, 20)
	assertOneLinePerRow(t, view, 15, 20)
	if row := rowFor(t, view, "A Very"); !strings.Contains(row, "…") {
		t.Errorf("truncated header has no ellipsis: %q", row)
	}
}

// Collapsed, the aggregates are what the user needs from the header;
// the name gives way to them.
func TestHeaderFit_AggregatesSurviveTruncation(t *testing.T) {
	m := longSectionModel("", cache.ReadState{HasUnread: true, MentionCount: 3})
	m.ToggleCollapse("S1")
	view := m.View(20, 30)

	// 30 wide: 26 columns, 4 of chrome, " •1" and " " + " 3 " take 7,
	// leaving 15 for the name.
	if got, want := headerFor(t, view, "▸ Eng"), "▸ Engineering an… •1  3"; got != want {
		t.Errorf("collapsed header = %q, want %q", got, want)
	}
	assertFits(t, view, "▸ Eng", 30)
}

// Narrower than the figures allow, the name goes first, then the emoji,
// then the aggregates. The triangle always stays and the header is
// always one line.
func TestHeaderFit_NameThenEmojiThenAggregatesGiveWay(t *testing.T) {
	cases := []struct {
		width int
		want  string
	}{
		// 14 columns: chrome 4, the glyph prefix "📙 " 3 and the
		// aggregates 7 fit exactly; the name has none left.
		{18, "▸ 📙  •1  3"},
		// 11 columns: without the emoji, chrome 4 + aggregates 7 fit.
		{15, "▸  •1  3"},
		// 8 columns: only the triangle.
		{12, "▸"},
	}
	for _, tc := range cases {
		m := longSectionModel("orange_book", cache.ReadState{HasUnread: true, MentionCount: 3})
		m.ToggleCollapse("S1")
		view := m.View(20, tc.width)
		if got := headerFor(t, view, "▸"); got != tc.want {
			t.Errorf("width %d: header = %q, want %q", tc.width, got, tc.want)
		}
		assertFits(t, view, "▸", tc.width)
	}
}

// A header that fits is drawn as it always was: no "…", nothing dropped.
func TestHeaderFit_FittingHeaderUnchanged(t *testing.T) {
	m := namedSectionModel("Docs", "orange_book", cache.ReadState{HasUnread: true, MentionCount: 3})
	m.ToggleCollapse("S1")
	if got, want := headerFor(t, m.View(10, 30), "Docs"), "▸ 📙 Docs •1  3"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
}

// The ":code:" text standing in for an emoji the sidebar cannot draw
// shows when there is room and gives way to the name when there is not.
func TestHeaderFit_TextFallbackGivesWayToName(t *testing.T) {
	short := sectionModel("no_such_emoji_anywhere")
	if got, want := headerFor(t, short.View(10, 40), "Docs"), "▾ :no_such_emoji_anywhere: Docs"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}

	m := longSectionModel("no_such_emoji_anywhere", cache.ReadState{})
	if got, want := headerFor(t, m.View(20, 20), "▾ Eng"), "▾ Engineering…"; got != want {
		t.Errorf("header = %q, want %q", got, want)
	}
}

// lipgloss expands a tab to spaces and breaks a line at "\n" after the
// header was measured, so a name holding either is shown with spaces.
func TestHeaderFit_TabsAndLineBreaksStayOneLine(t *testing.T) {
	for _, name := range []string{"A\tBBBBBBBBBBB", "A\nBBBB", "A\r\nB"} {
		m := namedSectionModel(name, "", cache.ReadState{})
		view := m.View(20, 20)
		assertOneLinePerRow(t, view, 20, 20)
		if got := lineAfter(t, view, "▾ A"); got != "# handbook" {
			t.Errorf("%q: line after the header = %q, want the channel row\n%s", name, got, view)
		}
	}
}

// Every header variant stays one line at every width the sidebar
// allows: normal and selected, expanded and collapsed with each badge
// size, Slack style or not, for each kind of emoji code (none, a glyph,
// a skin-toned code, a VS16 code, a ZWJ sequence, a long unknown custom
// one).
func TestHeaderFit_EveryVariantOneLine(t *testing.T) {
	codes := []string{
		"",
		"orange_book",
		"male-construction-worker::skin-tone-2",
		"hammer_and_wrench",
		"heart_on_fire",
		"a_very_long_custom_party_parrot_emoji",
	}
	for _, code := range codes {
		for _, mentions := range []int{0, 1, 99, 150} {
			for _, collapsed := range []bool{false, true} {
				for _, selected := range []bool{false, true} {
					for _, slack := range []bool{false, true} {
						for width := minWidth; width <= 40; width++ {
							name := fmt.Sprintf("%s/m%d/c%v/s%v/slack%v/w%d", code, mentions, collapsed, selected, slack, width)
							m := longSectionModel(code, cache.ReadState{HasUnread: true, MentionCount: mentions})
							m.SetSlackStyle(slack)
							if collapsed {
								m.ToggleCollapse("S1")
							}
							if selected {
								selectHeader(t, &m)
							}
							view := m.View(12, width)
							t.Run(name, func(t *testing.T) { assertOneLinePerRow(t, view, 12, width) })
						}
					}
				}
			}
		}
	}
}

// SectionHeader is bold. After the cursor glyph's reset the selected
// header re-applies bold along with its colours, as channel rows do.
func TestHeaderFit_SelectedHeaderStaysBold(t *testing.T) {
	m := sectionModel("")
	selectHeader(t, &m)
	row := rowFor(t, m.View(10, 30), "Docs")
	want := messages.SidebarBgANSI() + messages.SidebarFgANSI() + "\x1b[1m▾ Docs"
	if !strings.Contains(row, want) {
		t.Errorf("selected header is not bold after the cursor:\n%q", row)
	}
}

// Slack style draws headers in regular weight, selected or not.
func TestHeaderFit_SlackStyleSelectedHeaderNotBold(t *testing.T) {
	m := sectionModel("")
	m.SetSlackStyle(true)
	selectHeader(t, &m)
	row := rowFor(t, m.View(10, 30), "Docs")
	if strings.Contains(row, "\x1b[1m") || strings.Contains(row, "\x1b[1;") {
		t.Errorf("Slack-style selected header is bold:\n%q", row)
	}
}

// A section emoji is drawn as a glyph only when it is one codepoint, as
// message bodies and reaction pills draw it: terminals draw VS16, ZWJ
// and skin-toned sequences at widths lipgloss does not predict
// (gammons/slk#301). Anything else is its ":code:" text, skin tone
// stripped first.
func TestHeaderEmoji_OnlySingleCodepointGlyphs(t *testing.T) {
	cases := []struct{ code, want string }{
		{"orange_book", "▾ 📙 Docs"},
		{"hammer_and_wrench", "▾ :hammer_and_wrench: Docs"},
		{"mountain", "▾ :mountain: Docs"},
		{"heart_on_fire", "▾ :heart_on_fire: Docs"},
		{"wave::skin-tone-3", "▾ 👋 Docs"},
		{"male-construction-worker::skin-tone-2", "▾ :male-construction-worker: Docs"},
		{"partyparrot::skin-tone-2", "▾ :partyparrot: Docs"},
	}
	for _, tc := range cases {
		m := sectionModel(tc.code)
		got := headerFor(t, m.View(10, 50), "Docs")
		if got != tc.want {
			t.Errorf("%s: header = %q, want %q", tc.code, got, tc.want)
		}
		if strings.ContainsRune(got, '\ufe0f') {
			t.Errorf("%s: header keeps a variation selector: %q", tc.code, got)
		}
	}
}
