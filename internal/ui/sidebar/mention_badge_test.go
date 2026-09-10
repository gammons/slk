package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
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
