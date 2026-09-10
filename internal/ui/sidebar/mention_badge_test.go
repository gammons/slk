package sidebar

import (
	"image/color"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// rowFor returns the rendered sidebar line containing name, failing the
// test if no such line exists. Mirrors the line-scanning approach in
// muted_test.go — sidebar tests never use golden files.
func rowFor(t *testing.T, view, name string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, name) {
			return l
		}
	}
	t.Fatalf("row %q not rendered:\n%s", name, view)
	return ""
}

func TestMentionBadge_Predicate(t *testing.T) {
	tests := []struct {
		name  string
		item  ChannelItem
		state cache.ReadState
		want  int
	}{
		{
			name:  "unread with mentions",
			item:  ChannelItem{ID: "C1"},
			state: cache.ReadState{HasUnread: true, MentionCount: 3},
			want:  3,
		},
		{
			name:  "unread without mentions",
			item:  ChannelItem{ID: "C1"},
			state: cache.ReadState{HasUnread: true, MentionCount: 0},
			want:  0,
		},
		{
			// Gating on HasUnread means a stale mention_count cannot
			// outlive the unread flag that justifies it.
			name:  "read but stale mention count",
			item:  ChannelItem{ID: "C1"},
			state: cache.ReadState{HasUnread: false, MentionCount: 4},
			want:  0,
		},
		{
			// Mentions pierce mute. Unlike IsVisiblyUnread, this
			// predicate ignores IsMuted: muting a busy channel must not
			// hide a direct @-mention.
			name:  "muted with mentions still badges",
			item:  ChannelItem{ID: "C1", IsMuted: true},
			state: cache.ReadState{HasUnread: true, MentionCount: 2},
			want:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.MentionBadge(tt.state); got != tt.want {
				t.Errorf("MentionBadge() = %d, want %d", got, tt.want)
			}
		})
	}
}

// A channel with mentions shows a count instead of the dot, never both.
func TestMentionBadge_ReplacesUnreadDot(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 3},
			"C2": {HasUnread: true, MentionCount: 0},
		}
	})
	m.ToggleCollapse("Channels")
	view := m.View(10, 30)

	badged := rowFor(t, view, "deploys")
	if !strings.Contains(badged, "3") {
		t.Errorf("mentioned row lacks its count:\n%q", badged)
	}
	if strings.Contains(badged, "●") {
		t.Errorf("mentioned row still shows the unread dot:\n%q", badged)
	}

	plain := rowFor(t, view, "general")
	if !strings.Contains(plain, "●") {
		t.Errorf("unread row without mentions lost its dot:\n%q", plain)
	}
}

// Muting silences the dot and the bold, but not the badge.
func TestMentionBadge_MutedChannelStillBadges(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", IsMuted: true},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 2}}
	})
	m.ToggleCollapse("Channels")
	view := m.View(10, 30)

	line := rowFor(t, view, "noisy")
	if !strings.Contains(line, "2") {
		t.Errorf("muted channel with mentions lost its badge:\n%q", line)
	}
	// The dot suppression that muted_test.go pins must still hold.
	if strings.Contains(line, "●") {
		t.Errorf("muted channel rendered an unread dot:\n%q", line)
	}
}

// Slack caps its badges at 99+. The cap is a render concern only: the DB
// keeps the true count so a later refresh below 100 shows the real number.
func TestMentionBadge_CapsAt99Plus(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "firehose", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 250}}
	})
	m.ToggleCollapse("Channels")
	line := rowFor(t, m.View(10, 30), "firehose")

	if !strings.Contains(line, "99+") {
		t.Errorf("expected 99+ cap:\n%q", line)
	}
	if strings.Contains(line, "250") {
		t.Errorf("raw count leaked into the badge:\n%q", line)
	}
}

func TestMentionBadge_ExactlyNinetyNineIsNotCapped(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "busy", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 99}}
	})
	m.ToggleCollapse("Channels")
	line := rowFor(t, m.View(10, 30), "busy")

	if strings.Contains(line, "99+") {
		t.Errorf("99 should render bare, not capped:\n%q", line)
	}
	if !strings.Contains(line, "99") {
		t.Errorf("expected 99 in the badge:\n%q", line)
	}
}

// ansiRe strips SGR escape sequences so a rendered row can be measured as
// the user sees it. The sidebar injects inline styling for the cursor,
// type prefix, unread dot and mention badge, so raw View() output cannot
// be measured directly.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// The name budget is computed per row, so a channel without a badge keeps
// exactly the width it had before badges existed. Only badged rows pay for
// the badge.
//
// Widths are chosen so both rows truncate deterministically. At width 24:
//
//	unbadged maxNameLen = (24-2) - 6 - 2 = 14
//	badged   maxNameLen = (24-2) - 6 - 5 = 11
//
// The 36-character name exceeds both, so the ellipsis lands three columns
// earlier on the badged row.
func TestMentionBadge_WidthBudgetIsPerRow(t *testing.T) {
	const longName = "engineering-deployments-and-releases"

	render := func(t *testing.T, mentions int) string {
		t.Helper()
		m := New([]ChannelItem{{ID: "C1", Name: longName, Type: "channel"}})
		m.SetReadStateReader(func() map[string]cache.ReadState {
			return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: mentions}}
		})
		m.ToggleCollapse("Channels")
		return rowFor(t, m.View(10, 24), longName[:10])
	}

	plain := ansiRe.ReplaceAllString(render(t, 0), "")
	badged := ansiRe.ReplaceAllString(render(t, 42), "")

	plainCut := strings.Index(plain, "…")
	badgedCut := strings.Index(badged, "…")
	if plainCut < 0 {
		t.Fatalf("unbadged row was not truncated, so the test cannot compare budgets:\n%q", plain)
	}
	if badgedCut < 0 {
		t.Fatalf("badged row was not truncated:\n%q", badged)
	}
	if badgedCut >= plainCut {
		t.Errorf("badged name cut at %d, unbadged at %d; badged must be shorter,"+
			" otherwise the width budget is not per-row\nplain:  %q\nbadged: %q",
			badgedCut, plainCut, plain, badged)
	}
	if !strings.Contains(badged, "42") {
		t.Errorf("badged row lost its count:\n%q", badged)
	}
}

// The converse of the test above: an unbadged row must be byte-identical
// to what it rendered before mention badges existed. rowChromeExcludingTrailer
// plus the dot's 2 cells must still equal the original hardcoded 8.
func TestMentionBadge_UnbadgedRowKeepsOriginalNameWidth(t *testing.T) {
	const longName = "engineering-deployments-and-releases"
	m := New([]ChannelItem{{ID: "C1", Name: longName, Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 0}}
	})
	m.ToggleCollapse("Channels")
	plain := ansiRe.ReplaceAllString(rowFor(t, m.View(10, 24), longName[:10]), "")

	// maxNameLen = (24-2) - 6 - 2 = 14, unchanged from the original
	// hardcoded budget of 8 (rowChromeExcludingTrailer 6 + dot 2).
	//
	// Bracket the budget rather than asserting an exact cut, so the test
	// does not depend on whether truncate.StringWithTail counts the
	// ellipsis inside or outside the limit: 12 characters must survive,
	// 15 must not.
	if !strings.Contains(plain, "engineering-") {
		t.Errorf("unbadged row truncated below the 14-column budget: %q", plain)
	}
	if strings.Contains(plain, "engineering-dep") {
		t.Errorf("unbadged row exceeded the 14-column budget: %q", plain)
	}
}

// ---------------------------------------------------------------------
// Themed rendering
//
// Every test above measures View() output with the styles package left at
// its package defaults, where SelectionBackground and SelectionForeground
// are still nil. MentionBadgeStyle() therefore emits no ANSI whatsoever
// and those tests are really asserting on a plain string. The test below
// is the only one that renders the badge the way a user actually sees it.
//
// It matters because the badge's output then passes through
// messages.ReapplyBgAfterResets, which does a blunt
//
//	strings.ReplaceAll(label, "\x1b[m", "\x1b[m"+rowAttrs)
//
// across the whole row label -- including the resets lipgloss emits
// *inside* the badge, because Padding(0, 1) splits it into three spans.
// A themed 42-badge really does come back looking like this (dark theme;
// row bg #0D0D1A, badge bg #4A9EFF, badge fg #1A1A2E):
//
//	\x1b[48;2;74;158;255m                              badge span 1
//	" "                                                left padding cell
//	\x1b[m                                             span 1 reset
//	\x1b[48;2;13;13;26m\x1b[38;2;224;224;224m\x1b[1m   INJECTED row attrs
//	\x1b[38;2;26;26;46;48;2;74;158;255m                badge span 2
//	"42"                                               the digits
//	\x1b[m                                             span 2 reset
//	\x1b[48;2;13;13;26m\x1b[38;2;224;224;224m\x1b[1m   INJECTED row attrs
//	\x1b[48;2;74;158;255m                              badge span 3
//	" "                                                right padding cell
//	\x1b[m                                             span 3 reset
//
// Each injection lands between one span's reset and the next span's SGR,
// so it is overridden before any cell is painted. That is the property
// asserted here, in the only form that cannot be faked: walk the row
// tracking the active background, and require the cells painted in the
// badge's background to be exactly ONE contiguous run -- " 42 ", the two
// padding cells plus the digits. An injection that painted even a single
// cell in the row's colours would split that run in two.

// paintedCell is one printed character paired with the SGR background
// token active when it was printed ("" meaning terminal default).
type paintedCell struct {
	r  rune
	bg string
}

// scanSGR reports whether s begins with a CSI SGR sequence (ESC [ params m),
// returning its byte length and its parameter substring.
func scanSGR(s string) (n int, params string, ok bool) {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return 0, "", false
	}
	for j := 2; j < len(s); j++ {
		if c := s[j]; c == 'm' {
			return j + 1, s[2:j], true
		} else if c != ';' && (c < '0' || c > '9') {
			return 0, "", false
		}
	}
	return 0, "", false
}

// sgrParamTokens splits an SGR parameter list into attribute tokens,
// keeping extended-colour groups (38/48 followed by ";5;N" or ";2;R;G;B")
// intact. lipgloss bundles attributes into one sequence, so a span like
// "1;38;2;224;224;224;48;2;13;13;26" must yield a single
// "48;2;13;13;26" background token rather than a stray "48".
func sgrParamTokens(params string) []string {
	if params == "" {
		return []string{"0"} // ESC[m is a full reset
	}
	f := strings.Split(params, ";")
	out := make([]string, 0, len(f))
	for i := 0; i < len(f); {
		if f[i] == "38" || f[i] == "48" {
			if i+2 < len(f) && f[i+1] == "5" {
				out = append(out, strings.Join(f[i:i+3], ";"))
				i += 3
				continue
			}
			if i+4 < len(f) && f[i+1] == "2" {
				out = append(out, strings.Join(f[i:i+5], ";"))
				i += 5
				continue
			}
		}
		out = append(out, f[i])
		i++
	}
	return out
}

// paintCells walks s interpreting SGR sequences and returns one
// paintedCell per printed character. Only the background is tracked --
// that is the attribute that decides which cells belong to the badge.
func paintCells(s string) []paintedCell {
	var out []paintedCell
	bg := ""
	for i := 0; i < len(s); {
		if n, params, ok := scanSGR(s[i:]); ok {
			for _, tok := range sgrParamTokens(params) {
				switch {
				case tok == "0", tok == "49":
					bg = ""
				case strings.HasPrefix(tok, "48;"):
					bg = tok
				case len(tok) == 2 && tok[0] == '4' && tok[1] <= '7':
					bg = tok // ANSI-16 background, 40-47
				}
			}
			i += n
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		out = append(out, paintedCell{r: r, bg: bg})
		i += sz
	}
	return out
}

// bgRuns returns the maximal runs of consecutive printed characters whose
// active background is bg.
func bgRuns(cells []paintedCell, bg string) []string {
	var runs []string
	var cur strings.Builder
	for _, c := range cells {
		if c.bg == bg {
			cur.WriteRune(c.r)
			continue
		}
		if cur.Len() > 0 {
			runs = append(runs, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		runs = append(runs, cur.String())
	}
	return runs
}

// badgeBackgroundSGR renders the badge style in isolation and reports the
// background token actually active on its text, so the assertions derive
// the badge's colour from the style itself instead of hardcoding a
// palette value that a theme edit would silently invalidate.
func badgeBackgroundSGR(t *testing.T) string {
	t.Helper()
	for _, c := range paintCells(styles.MentionBadgeStyle().Render("X")) {
		if c.r == 'X' {
			return c.bg
		}
	}
	t.Fatalf("badge style rendered no marker cell")
	return ""
}

// styleGlobals holds every exported styles var the sidebar reads, either
// directly or via messages.Sidebar*ANSI(). styles.Apply mutates
// package-global state and no other sidebar test touches it, so a test
// that themes the package must put it back or it changes what siblings
// observe depending on run order.
type styleGlobals struct {
	primary, warning, accent, textMuted         color.Color
	sidebarBg, sidebarText, sidebarTextMuted    color.Color
	selectionBg, selectionFg                    color.Color
	chSelected, chNormal, chUnread, chMuted     lipgloss.Style
	sectionHeader, presenceOnline, presenceAway lipgloss.Style
}

func snapshotStyleGlobals() styleGlobals {
	return styleGlobals{
		primary: styles.Primary, warning: styles.Warning,
		accent: styles.Accent, textMuted: styles.TextMuted,
		sidebarBg: styles.SidebarBackground, sidebarText: styles.SidebarText,
		sidebarTextMuted: styles.SidebarTextMuted,
		selectionBg:      styles.SelectionBackground,
		selectionFg:      styles.SelectionForeground,
		chSelected:       styles.ChannelSelected, chNormal: styles.ChannelNormal,
		chUnread: styles.ChannelUnread, chMuted: styles.ChannelMuted,
		sectionHeader:  styles.SectionHeader,
		presenceOnline: styles.PresenceOnline, presenceAway: styles.PresenceAway,
	}
}

// restore puts the snapshot back. Restoring to the package's pristine
// state is possible precisely because styles.Apply assigns the Selection*
// pair on both branches of its if (styles.go:348-361): there is no path
// on which Apply leaves them stale, so the snapshot is either a real
// value or the untouched nil, and either round-trips.
//
// The listed set is exhaustive for this package, not for styles: the
// sidebar reads only these vars, directly or through
// messages.Sidebar{Bg,Fg,MutedFg}ANSI. Apply also themes Surface,
// SearchHighlight*, ComposeInsertBG and the message-pane styles, which
// nothing in this package's test binary observes. Go runs each package's
// tests in their own process, so that residue cannot escape here.
func (g styleGlobals) restore() {
	styles.Primary, styles.Warning = g.primary, g.warning
	styles.Accent, styles.TextMuted = g.accent, g.textMuted
	styles.SidebarBackground, styles.SidebarText = g.sidebarBg, g.sidebarText
	styles.SidebarTextMuted = g.sidebarTextMuted
	styles.SelectionBackground, styles.SelectionForeground = g.selectionBg, g.selectionFg
	styles.ChannelSelected, styles.ChannelNormal = g.chSelected, g.chNormal
	styles.ChannelUnread, styles.ChannelMuted = g.chUnread, g.chMuted
	styles.SectionHeader = g.sectionHeader
	styles.PresenceOnline, styles.PresenceAway = g.presenceOnline, g.presenceAway
}

// canonicalSidebarView renders a fixture that exercises every styles var
// in styleGlobals: a badged row (Selection*), a plain unread row
// (Primary dot), a muted row (ChannelMuted), a private and an app row
// (Warning), both DM presence glyphs, the cursor (Accent/TextMuted) and a
// section header. Comparing its bytes before theming and after restoring
// turns "I think I listed every global" into a checked claim.
func canonicalSidebarView() string {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
		{ID: "C3", Name: "quiet", Type: "channel", IsMuted: true},
		{ID: "C4", Name: "secret", Type: "private"},
		{ID: "C5", Name: "alice", Type: "dm", Presence: "active"},
		{ID: "C6", Name: "bob", Type: "dm", Presence: "away"},
		{ID: "C7", Name: "botty", Type: "app"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 42},
			"C2": {HasUnread: true},
			"C3": {HasUnread: true, MentionCount: 7},
			"C4": {HasUnread: true},
		}
	})
	m.ToggleCollapse("Channels")
	return m.View(20, 30)
}

// TestMentionBadge_ThemedBadgePaintsOnlyItsOwnCells is the themed
// counterpart to the plain-string tests above. See the commentary block
// preceding it for why the reapply interaction needs pinning.
func TestMentionBadge_ThemedBadgePaintsOnlyItsOwnCells(t *testing.T) {
	baseline := canonicalSidebarView()

	saved := snapshotStyleGlobals()
	// Deferred so the restore survives an early t.Fatalf below, and so
	// the restore itself is checked rather than trusted.
	defer func() {
		saved.restore()
		got := canonicalSidebarView()
		if got == baseline {
			return
		}
		// Report the first divergent line rather than two 2.5 KB blobs.
		wantL, gotL := strings.Split(baseline, "\n"), strings.Split(got, "\n")
		for i := 0; i < len(wantL) && i < len(gotL); i++ {
			if wantL[i] != gotL[i] {
				t.Errorf("styles globals were not fully restored; sibling tests in"+
					" this package would see a different palette depending on run"+
					" order\nfirst divergent line %d:\n baseline: %q\n restored: %q",
					i, wantL[i], gotL[i])
				return
			}
		}
		t.Errorf("styles globals were not fully restored: line count %d != %d",
			len(gotL), len(wantL))
	}()

	styles.Apply("dark", config.Theme{})

	badgeBg := badgeBackgroundSGR(t)
	if badgeBg == "" {
		t.Fatalf("badge emitted no background after styles.Apply; this test would be vacuous")
	}

	for _, tc := range []struct {
		name   string
		count  int
		digits string
	}{
		{"two digit count", 42, "42"},
		{"capped count", 250, "99+"},
	} {
		m := New([]ChannelItem{{ID: "C1", Name: "deploys", Type: "channel"}})
		m.SetReadStateReader(func() map[string]cache.ReadState {
			return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: tc.count}}
		})
		m.ToggleCollapse("Channels")
		row := rowFor(t, m.View(10, 30), "deploys")

		// (1) The digit run is contiguous. strings.Contains on the raw
		// bytes IS the contiguity assertion: any escape sequence between
		// the digits would break the substring. This is the property
		// every other test in this file relies on to find the badge, and
		// until now it was only ever checked unthemed.
		if !strings.Contains(row, tc.digits) {
			t.Errorf("%s: themed badge digits %q are not a contiguous run;"+
				" an ANSI sequence was emitted between them:\n%q",
				tc.name, tc.digits, row)
		}

		// (2) No injected reapply payload paints a cell. The cells
		// carrying the badge's background must form exactly one run, and
		// that run must be the badge and nothing but the badge:
		// left padding, the digits, right padding.
		cells := paintCells(row)
		runs := bgRuns(cells, badgeBg)
		want := " " + tc.digits + " "
		if len(runs) != 1 || runs[0] != want {
			t.Errorf("%s: expected exactly one run of badge-coloured cells %q,"+
				" got %q -- a reapply injection painted a cell in the row's"+
				" colours inside the badge:\n%q", tc.name, want, runs, row)
		}

		// Guard against a vacuous pass: if the row background happened to
		// equal the badge background, the run check above would be
		// meaningless. Confirm the row really does paint in a different
		// colour outside the badge.
		rowBgSeen := false
		for _, c := range cells {
			if c.bg != badgeBg && c.bg != "" {
				rowBgSeen = true
				break
			}
		}
		if !rowBgSeen {
			t.Errorf("%s: row painted no non-badge background, so the badge"+
				" isolation check proves nothing:\n%q", tc.name, row)
		}
	}
}

// Muting silences chatter, not someone naming you: a muted channel keeps
// its badge. But it should not shout as loudly as an unmuted one, so the
// pill's background is mixed back toward the sidebar background.
//
// Under test the palette is unthemed, so neither style emits ANSI and
// both render the same plain string -- that is why this test themes the
// package. It uses the same snapshot/restore helpers as
// TestMentionBadge_ThemedBadgePaintsOnlyItsOwnCells.
func TestMentionBadge_MutedChannelUsesDimmerPill(t *testing.T) {
	saved := snapshotStyleGlobals()
	defer saved.restore()
	styles.Apply("dark", config.Theme{})

	render := func(t *testing.T, muted bool) string {
		t.Helper()
		m := New([]ChannelItem{{ID: "C1", Name: "noisy", Type: "channel", IsMuted: muted}})
		m.SetReadStateReader(func() map[string]cache.ReadState {
			return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 4}}
		})
		m.ToggleCollapse("Channels")
		return rowFor(t, m.View(10, 30), "noisy")
	}

	loud := render(t, false)
	quiet := render(t, true)

	// Both must still carry the count -- the muted row is dimmed, not
	// suppressed. This is the half that would break if someone "fixed"
	// muting by hiding the badge.
	if !strings.Contains(ansiRe.ReplaceAllString(quiet, ""), "4") {
		t.Errorf("muted row lost its badge:\n%q", quiet)
	}
	if !strings.Contains(ansiRe.ReplaceAllString(loud, ""), "4") {
		t.Errorf("unmuted row lost its badge:\n%q", loud)
	}
	// And they must be visually distinguishable, which under a theme
	// means different SGR bytes.
	if loud == quiet {
		t.Errorf("muted and unmuted badges render identically; the dimmer pill is not being applied:\n%q", loud)
	}
}

// The dimmed background must stay legible: mixing toward the sidebar
// background is only safe if the foreground contrasts with the result.
func TestMutedMentionBadgeStyle_DiffersFromNormalOnBothThemes(t *testing.T) {
	saved := snapshotStyleGlobals()
	defer saved.restore()

	for _, theme := range []string{"dark", "light"} {
		t.Run(theme, func(t *testing.T) {
			styles.Apply(theme, config.Theme{})
			normal := styles.MentionBadgeStyle()
			muted := styles.MutedMentionBadgeStyle()

			if colorsEqualForTest(normal.GetBackground(), muted.GetBackground()) {
				t.Errorf("%s: muted background equals normal; no dimming applied", theme)
			}
			// The dimmed pill must not collapse into the row it sits
			// on, or it stops reading as a badge at all.
			if colorsEqualForTest(muted.GetBackground(), styles.SidebarBackground) {
				t.Errorf("%s: muted badge background equals the sidebar background; the pill is invisible", theme)
			}
			if colorsEqualForTest(muted.GetBackground(), muted.GetForeground()) {
				t.Errorf("%s: muted badge fg and bg are identical", theme)
			}
		})
	}
}

func colorsEqualForTest(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}
