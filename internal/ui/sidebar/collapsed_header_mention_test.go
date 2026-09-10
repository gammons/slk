package sidebar

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// A collapsed section header carries two independent figures: the
// existing "•N" count of channels-with-unreads, and a mention badge
// summing MentionBadge across the section. They answer different
// questions and are never merged.
//
// These tests scan View() output for the header line, the way every
// other test in this package works -- no golden files. They reuse
// rowFor and ansiRe from mention_badge_test.go (same package).
//
// Assertions are on the ANSI-stripped, space-trimmed header. That is
// deliberately exact rather than a set of Contains checks: the point of
// several of these cases is that a figure is *absent*, and only an
// exact form can prove nothing extra was appended.
//
// Note the two-space gap in the expected strings, e.g. "•2  3". The
// mention badge is styles.MentionBadgeStyle().Render(...), whose
// Padding(0, 1) supplies one space of its own, and the header adds an
// explicit separator space before it exactly as the channel rows do
// (model.go: `name + " " + unreadDot`). Under the unthemed test palette
// the style emits no ANSI, so the padding is all that survives.
func headerFor(t *testing.T, view, section string) string {
	t.Helper()
	return strings.TrimSpace(ansiRe.ReplaceAllString(rowFor(t, view, section), ""))
}

// The headline case: a collapsed section holding a channel with
// mentions shows both figures.
func TestCollapsedHeader_ShowsUnreadAndMentionAggregates(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 3},
			"C2": {HasUnread: true},
		}
	})
	if !m.IsCollapsed("Channels") {
		t.Fatal("precondition: Channels is collapsed by default in config mode")
	}

	got := headerFor(t, m.View(15, 30), "Channels")
	if want := "▸ Channels •2  3"; got != want {
		t.Errorf("collapsed header = %q, want %q\n"+
			"  •2 = two channels have unreads; 3 = three mentions across the section",
			got, want)
	}
}

// Today's behaviour, unchanged: unreads but no mentions renders the dot
// aggregate alone. The exact match is the assertion -- a badge appended
// at zero would break it.
func TestCollapsedHeader_NoMentionsShowsOnlyUnreadAggregate(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true},
			"C2": {HasUnread: true},
		}
	})

	got := headerFor(t, m.View(15, 30), "Channels")
	if want := "▸ Channels •2"; got != want {
		t.Errorf("collapsed header with no mentions = %q, want %q"+
			" (a zero mention count must render nothing at all)", got, want)
	}
}

// An expanded section shows neither aggregate: the per-row dots and
// badges take over. Unchanged behaviour.
func TestCollapsedHeader_ExpandedShowsNeitherAggregate(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 3},
			"C2": {HasUnread: true},
		}
	})
	m.ToggleCollapse("Channels") // collapsed by default, so this expands

	got := headerFor(t, m.View(15, 30), "Channels")
	if want := "▾ Channels"; got != want {
		t.Errorf("expanded header = %q, want %q (neither aggregate belongs"+
			" on an expanded header)", got, want)
	}
}

// The mention figure sums counts; it does not count mentioned channels.
// At row level the badge answers "how many times was I named here", and
// a header answering a different question with the same glyph would
// teach the user the wrong thing.
//
// The fixture distinguishes the two readings: 2 + 3 mentions across
// three unread channels is 5 summed, but only 2 channels-with-mentions.
func TestCollapsedHeader_MentionAggregateSumsAcrossChannels(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
		{ID: "C3", Name: "random", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 2},
			"C2": {HasUnread: true, MentionCount: 3},
			"C3": {HasUnread: true},
		}
	})

	got := headerFor(t, m.View(15, 30), "Channels")
	if want := "▸ Channels •3  5"; got != want {
		t.Errorf("collapsed header = %q, want %q -- the mention figure must"+
			" SUM counts (5), not count mentioned channels (2)", got, want)
	}
}

// MentionBadge ignores mute where IsVisiblyUnread does not, so a muted
// channel feeds the mention figure but not the unread figure. That
// asymmetry mirrors the rows and is the point: muting silences chatter,
// not someone naming you.
func TestCollapsedHeader_MutedChannelFeedsMentionsNotUnreads(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", IsMuted: true},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 4},
			"C2": {HasUnread: true},
		}
	})

	got := headerFor(t, m.View(15, 30), "Channels")
	// •1 counts C2 only; 4 comes entirely from the muted C1.
	if want := "▸ Channels •1  4"; got != want {
		t.Errorf("collapsed header = %q, want %q -- a muted channel must"+
			" contribute to the mention figure but not the unread figure",
			got, want)
	}
}

// The extreme of the asymmetry above: every channel in the section is
// muted, so the unread aggregate is zero and vanishes, while the
// mention badge stands alone. Proves the two figures are genuinely
// independent rather than one gating the other.
func TestCollapsedHeader_AllMutedShowsMentionsWithoutUnreadAggregate(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", IsMuted: true},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 4},
		}
	})

	got := headerFor(t, m.View(15, 30), "Channels")
	if want := "▸ Channels  4"; got != want {
		t.Errorf("collapsed header = %q, want %q -- the mention badge must"+
			" render even when the unread aggregate is zero", got, want)
	}
}

// The 99+ cap is a render concern, applied to the summed figure. Two
// channels below the cap individually sum past it.
func TestCollapsedHeader_MentionAggregateCapsAt99Plus(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 60},
			"C2": {HasUnread: true, MentionCount: 90},
		}
	})

	got := headerFor(t, m.View(15, 30), "Channels")
	if want := "▸ Channels •2  99+"; got != want {
		t.Errorf("collapsed header = %q, want %q (150 mentions must cap)", got, want)
	}
	if strings.Contains(got, "150") {
		t.Errorf("raw summed count leaked into the header badge: %q", got)
	}
}

// renderSectionHeaderLabel builds two variants -- normal and selected --
// and View picks between them by cursor position. A badge added to only
// one variant would vanish the moment the cursor landed on the header.
// This is the guard against that.
func TestCollapsedHeader_SelectedVariantCarriesBothFigures(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel"},
		{ID: "C2", Name: "general", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true, MentionCount: 3},
			"C2": {HasUnread: true},
		}
	})

	// Nav is [Threads, Channels header]; one MoveDown selects the header.
	m.MoveDown()
	if name, ok := m.IsSectionHeaderSelected(); !ok || name != "Channels" {
		t.Fatalf("precondition: expected the Channels header selected, got name=%q ok=%v", name, ok)
	}

	got := headerFor(t, m.View(15, 30), "Channels")
	// The cursor glyph ▌ replaces the leading space of the normal variant.
	if want := "▌▸ Channels •2  3"; got != want {
		t.Errorf("selected collapsed header = %q, want %q -- both figures must"+
			" survive the cursor landing on the header", got, want)
	}
}

// The themed counterpart, mirroring TestMentionBadge_ThemedBadgePaintsOnlyItsOwnCells
// for the header path and reusing its helpers (same package).
//
// Worth pinning separately from the row test because the header is a
// different rendering path: a different outer style (SectionHeader, not
// ChannelNormal/Selected) and a different reapply payload (plain
// bgAnsi, with no bold attribute). messages.ReapplyBgAfterResets still
// injects that payload after every "\x1b[m" in the label, including the
// two that Padding(0, 1) puts *inside* the badge.
//
// The assertion is the same one and for the same reason: the cells
// carrying the badge's background must form exactly ONE contiguous run
// -- left padding, digits, right padding. An injection that painted a
// single cell in the header's colours would split that run in two.
func TestCollapsedHeader_ThemedMentionBadgePaintsOnlyItsOwnCells(t *testing.T) {
	saved := snapshotStyleGlobals()
	defer saved.restore()
	styles.Apply("dark", config.Theme{})

	badgeBg := badgeBackgroundSGR(t)
	if badgeBg == "" {
		t.Fatalf("badge emitted no background after styles.Apply; this test would be vacuous")
	}

	m := New([]ChannelItem{{ID: "C1", Name: "deploys", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 42}}
	})
	header := rowFor(t, m.View(10, 30), "Channels")

	// The digits are a contiguous byte run: any escape between them
	// would break this substring, and every other test in this file
	// depends on that property to find the badge at all.
	if !strings.Contains(header, "42") {
		t.Errorf("themed header badge digits are not contiguous; an ANSI"+
			" sequence was emitted between them:\n%q", header)
	}

	cells := paintCells(header)
	runs := bgRuns(cells, badgeBg)
	if want := " 42 "; len(runs) != 1 || runs[0] != want {
		t.Errorf("expected exactly one run of badge-coloured cells %q, got %q"+
			" -- a reapply injection painted a cell in the header's colours"+
			" inside the badge:\n%q", want, runs, header)
	}

	// Guard against a vacuous pass: if the header background happened
	// to equal the badge background the run check proves nothing.
	rowBgSeen := false
	for _, c := range cells {
		if c.bg != badgeBg && c.bg != "" {
			rowBgSeen = true
			break
		}
	}
	if !rowBgSeen {
		t.Errorf("header painted no non-badge background, so the isolation"+
			" check proves nothing:\n%q", header)
	}
}
