package sidebar

import (
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// items C1..C4; C2 and C4 are unread, C3 is unread-but-muted (excluded),
// C1 is read. NextUnread walks m.filtered (visible section order); a
// single explicit Section on every item keeps that order == input order
// so these assertions read cleanly.
func unreadNavModel() Model {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
		{ID: "C2", Name: "two", Type: "channel", Section: "Eng"},
		{ID: "C3", Name: "muted", Type: "channel", Section: "Eng", IsMuted: true},
		{ID: "C4", Name: "four", Type: "channel", Section: "Eng"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C2": {HasUnread: true},
			"C3": {HasUnread: true}, // muted -> not visibly unread
			"C4": {HasUnread: true},
		}
	})
	return m
}

func TestNextUnread_ForwardSkipsCurrentAndMuted(t *testing.T) {
	m := unreadNavModel()

	// From C1 (read): next unread forward is C2.
	if id, _, _, ok := m.NextUnread("C1", 1); !ok || id != "C2" {
		t.Fatalf("from C1 want C2, got id=%q ok=%v", id, ok)
	}
	// From C2: skip muted C3, land on C4.
	if id, _, _, ok := m.NextUnread("C2", 1); !ok || id != "C4" {
		t.Fatalf("from C2 want C4 (muted C3 skipped), got id=%q ok=%v", id, ok)
	}
	// From C4: wrap around to C2.
	if id, _, _, ok := m.NextUnread("C4", 1); !ok || id != "C2" {
		t.Fatalf("from C4 want wrap to C2, got id=%q ok=%v", id, ok)
	}
}

func TestPrevUnread_Backward(t *testing.T) {
	m := unreadNavModel()

	// From C4: previous unread is C2 (C3 muted).
	if id, _, _, ok := m.NextUnread("C4", -1); !ok || id != "C2" {
		t.Fatalf("prev from C4 want C2, got id=%q ok=%v", id, ok)
	}
	// From C2: wrap backward to C4.
	if id, _, _, ok := m.NextUnread("C2", -1); !ok || id != "C4" {
		t.Fatalf("prev from C2 want wrap to C4, got id=%q ok=%v", id, ok)
	}
}

func TestNextUnread_ReturnsNameAndType(t *testing.T) {
	m := unreadNavModel()
	id, name, chType, ok := m.NextUnread("C1", 1)
	if !ok || id != "C2" || name != "two" || chType != "channel" {
		t.Fatalf("want C2/two/channel, got %q/%q/%q ok=%v", id, name, chType, ok)
	}
}

// The currently-open channel is always skipped even when it is itself
// unread (e.g. just marked unread), so a press always advances.
func TestNextUnread_SkipsCurrentEvenIfUnread(t *testing.T) {
	m := unreadNavModel()
	if id, _, _, ok := m.NextUnread("C2", 1); !ok || id != "C4" {
		t.Fatalf("from unread C2 want advance to C4, got id=%q ok=%v", id, ok)
	}
}

// Only the current channel is unread -> nothing else to jump to.
func TestNextUnread_NoOtherUnread(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel"},
		{ID: "C2", Name: "two", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true}}
	})
	if _, _, _, ok := m.NextUnread("C1", 1); ok {
		t.Fatalf("only C1 unread and it is current: want ok=false")
	}
}

// afterID not present (nothing open yet) still finds the first unread.
func TestNextUnread_UnknownAfterID(t *testing.T) {
	m := unreadNavModel()
	if id, _, _, ok := m.NextUnread("", 1); !ok || id != "C2" {
		t.Fatalf("from empty want first unread C2, got id=%q ok=%v", id, ok)
	}
}

// No reader installed -> safe no-op.
func TestNextUnread_NoReader(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "one", Type: "channel"}})
	if _, _, _, ok := m.NextUnread("", 1); ok {
		t.Fatalf("no reader: want ok=false")
	}
}

// Regression for the anchor off-by-one: with an anchor that isn't in the
// visible set (fresh start before opening anything, or activeChannelID
// dropped out of m.filtered after a rebuild), the first press must land on
// the TOP unread row forward / BOTTOM unread row backward -- not the far
// end after a full wrap. C1 and C3 unread, C2 read, afterID unknown.
func TestNextUnread_UnknownAnchor_LandsOnNearestEnd(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
		{ID: "C2", Name: "two", Type: "channel", Section: "Eng"},
		{ID: "C3", Name: "three", Type: "channel", Section: "Eng"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true},
			"C3": {HasUnread: true},
		}
	})
	if id, _, _, ok := m.NextUnread("", 1); !ok || id != "C1" {
		t.Fatalf("forward from unknown anchor want first unread C1, got id=%q ok=%v", id, ok)
	}
	if id, _, _, ok := m.NextUnread("", -1); !ok || id != "C3" {
		t.Fatalf("backward from unknown anchor want last unread C3, got id=%q ok=%v", id, ok)
	}
}

// The walk intentionally includes unread channels inside collapsed
// sections (m.filtered ignores collapse), and selecting the target
// expands the section -- the documented "catch up on all unreads"
// behavior. C1 has no Section, so it lands in the default "Channels"
// section, which New() collapses.
func TestNextUnread_ReachesCollapsedSectionAndExpands(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "hidden", Type: "channel"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true}}
	})
	if !m.IsCollapsed(defaultChannelsSection) {
		t.Fatalf("precondition: Channels section should start collapsed")
	}
	id, _, _, ok := m.NextUnread("", 1)
	if !ok || id != "C1" {
		t.Fatalf("unread inside a collapsed section must be reachable; got id=%q ok=%v", id, ok)
	}
	m.SelectByID(id)
	if m.IsCollapsed(defaultChannelsSection) {
		t.Fatalf("SelectByID should expand the collapsed section holding the target")
	}
}

// Nothing unread anywhere (distinct from the only-unread-is-current case):
// both an anchored and an unknown-anchor call report ok=false.
func TestNextUnread_NoUnreadAtAll(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "one", Type: "channel", Section: "Eng"},
		{ID: "C2", Name: "two", Type: "channel", Section: "Eng"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState { return map[string]cache.ReadState{} })
	if _, _, _, ok := m.NextUnread("C1", 1); ok {
		t.Fatalf("no unread channels: want ok=false from an anchored call")
	}
	if _, _, _, ok := m.NextUnread("", 1); ok {
		t.Fatalf("no unread channels: want ok=false from an unknown anchor")
	}
}

// Type is forwarded verbatim (it drives messagepane.SetChannelType via
// ChannelSelectedMsg), including DM / group_dm.
func TestNextUnread_ForwardsChannelType(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "D1", Name: "dm-peer", Type: "dm", Section: "Eng"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"D1": {HasUnread: true}}
	})
	id, name, chType, ok := m.NextUnread("", 1)
	if !ok || id != "D1" || name != "dm-peer" || chType != "dm" {
		t.Fatalf("want D1/dm-peer/dm, got %q/%q/%q ok=%v", id, name, chType, ok)
	}
}

// An active name filter narrows the walk to matching rows only.
func TestNextUnread_RespectsActiveFilter(t *testing.T) {
	m := unreadNavModel() // C2 + C4 unread (C3 muted)
	m.SetFilter("four")   // only C4 matches by name
	if id, _, _, ok := m.NextUnread("", 1); !ok || id != "C4" {
		t.Fatalf("with filter 'four' want C4, got id=%q ok=%v", id, ok)
	}
	// C4 is now the only visible unread, so anchoring on it finds no
	// *other* unread and reports ok=false (current is always skipped).
	if _, _, _, ok := m.NextUnread("C4", 1); ok {
		t.Fatalf("with filter 'four' C4 is the only unread; anchoring on it should find nothing else")
	}
}
