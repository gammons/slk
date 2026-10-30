package sidebar

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/cache"
)

func TestSlackStyle_HeadersDotsAndSort(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "eng-gazette", Type: "channel", Section: "Must Read"},
		{ID: "C2", Name: "eng_announcements", Type: "channel", Section: "Must Read"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true}}
	})
	plain := func(s string) string { return ansiRe.ReplaceAllString(s, "") }

	off := plain(m.View(10, 40))
	if !strings.Contains(off, "▾") || !strings.Contains(off, "●") ||
		strings.Index(off, "eng-gazette") > strings.Index(off, "eng_announcements") {
		t.Fatalf("default look changed:\n%s", off)
	}

	m.SetSlackStyle(true)
	raw := m.View(10, 40)
	on := plain(raw)
	if strings.Contains(on, "▾") {
		t.Errorf("expanded header still shows ▾:\n%s", on)
	}
	if strings.Contains(on, "●") {
		t.Errorf("unread dot still drawn:\n%s", on)
	}
	if strings.Index(on, "eng_announcements") > strings.Index(on, "eng-gazette") {
		t.Errorf("punctuation still decides the order:\n%s", on)
	}
	if hdr := rowFor(t, raw, "Must Read"); strings.Contains(hdr, "\x1b[1") {
		t.Errorf("header still bold: %q", hdr)
	}
	if !strings.Contains(plain(rowFor(t, raw, "eng-gazette")), "# eng-gazette") {
		t.Errorf("unread row lost its name")
	}

	m.ToggleCollapse("Must Read")
	if hdr := plain(rowFor(t, m.View(10, 40), "Must Read")); !strings.Contains(hdr, "▸") {
		t.Errorf("collapsed header must keep ▸: %q", hdr)
	}
}

func TestPrivateGlyph(t *testing.T) {
	m := New([]ChannelItem{{ID: "G1", Name: "secret", Type: "private"}})
	m.ToggleCollapse("Channels")
	m.SetPrivateGlyph("L")
	if row := ansiRe.ReplaceAllString(rowFor(t, m.View(10, 30), "secret"), ""); !strings.Contains(row, "L secret") {
		t.Errorf("private glyph not used: %q", row)
	}
}

func TestActiveOnly_HidesInactiveInEverySection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	old := fmt.Sprintf("%d.000000", now.Add(-40*24*time.Hour).Unix())
	recent := fmt.Sprintf("%d.000000", now.Add(-5*24*time.Hour).Unix())
	m := New([]ChannelItem{
		{ID: "C1", Name: "dormant", Type: "channel", Section: "Must Read"},
		{ID: "C2", Name: "lively", Type: "channel", Section: "Must Read"},
		{ID: "C3", Name: "oldunread", Type: "channel", Section: "Must Read"},
		{ID: "C4", Name: "oldopen", Type: "channel", Section: "Must Read"},
	})
	m.nowFn = func() time.Time { return now }
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {LastReadTS: old},
			"C2": {LastReadTS: recent},
			"C3": {LastReadTS: old, HasUnread: true},
			"C4": {LastReadTS: old},
		}
	})
	m.SetActiveChannelID("C4")

	if view := m.View(12, 30); !strings.Contains(view, "dormant") {
		t.Fatalf("active_only off hid a sectioned channel:\n%s", view)
	}
	m.SetActiveOnly(true)
	view := m.View(12, 30)
	if strings.Contains(view, "dormant") {
		t.Errorf("channel read 40 days ago still shown:\n%s", view)
	}
	for _, keep := range []string{"lively", "oldunread", "oldopen"} {
		if !strings.Contains(view, keep) {
			t.Errorf("%s hidden:\n%s", keep, view)
		}
	}
}

// TestActiveOnly_DormantChannelReturnsOnANewMessage: the App calls
// Invalidate on a read-state change; a hidden channel that turned
// unread must come back without waiting for the next full rebuild.
func TestActiveOnly_DormantChannelReturnsOnANewMessage(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	state := map[string]cache.ReadState{
		"C1": {LastReadTS: fmt.Sprintf("%d.000000", now.Add(-40*24*time.Hour).Unix())},
	}
	m := New([]ChannelItem{{ID: "C1", Name: "dormant", Type: "channel", Section: "Must Read"}})
	m.nowFn = func() time.Time { return now }
	m.SetReadStateReader(func() map[string]cache.ReadState { return state })
	m.SetActiveOnly(true)
	if view := m.View(12, 30); strings.Contains(view, "dormant") {
		t.Fatalf("dormant channel shown before its new message:\n%s", view)
	}
	state["C1"] = cache.ReadState{LastReadTS: state["C1"].LastReadTS, HasUnread: true}
	m.Invalidate()
	if view := m.View(12, 30); !strings.Contains(view, "dormant") {
		t.Errorf("dormant channel still hidden after a new message:\n%s", view)
	}
}
