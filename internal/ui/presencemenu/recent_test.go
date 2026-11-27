package presencemenu

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/ansi"

	"github.com/gammons/slk/internal/core"
)

func recentLabels(m Model) []string {
	var out []string
	for _, it := range m.items {
		if it.action == ActionSetRecent {
			out = append(out, it.label)
		}
	}
	return out
}

func TestRecentRowsDedupedAndCapped(t *testing.T) {
	m := New()
	m.SetRecent([]core.RecentStatus{
		{Text: "One", Emoji: ":one:"},
		{Text: "Working from home", Emoji: ":house:"}, // a preset
		{Text: "One", Emoji: ":one:"},                 // repeats a newer entry
		{Text: "One", Emoji: ":two:"},                 // same text, other emoji: kept
		{Text: "Three"},
		{Emoji: ":four:"},
		{Text: "Five"},
		{Text: "Six"},
	})
	m.OpenWith("W", "active", false, time.Time{})
	want := []string{":one: One", ":two: One", "Three", ":four:", "Five"}
	if got := recentLabels(m); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("recent rows = %q, want %q", got, want)
	}
	// After the presets, before the snooze rows.
	if m.items[5].label != ":one: One" || m.items[10].label != "Snooze for 20 minutes" {
		t.Errorf("rows 5 and 10 = %q, %q", m.items[5].label, m.items[10].label)
	}
}

func TestRecentRowMarkedCurrent(t *testing.T) {
	for _, tc := range []struct {
		name, text, emoji string // the status now set
		want              string // the recent row marked current
	}{
		{"standard emoji", "On a break", "coffee", ":coffee: On a break"},
		// Both customs draw as the same fallback glyph; names tell them apart.
		{"custom emoji", "On a break", "fine-fire", ":fine-fire: On a break"},
		{"text only", "On a break", "", "On a break"},
	} {
		m := New()
		m.SetCurrentCustomStatus(tc.text, "💬")
		m.SetCurrentStatusEmojiName(tc.emoji)
		m.SetRecent([]core.RecentStatus{
			{Text: "On a break", Emoji: ":coffee:"},
			{Text: "On a break", Emoji: ":fine-fire:"},
			{Text: "On a break", Emoji: ":party-parrot:"},
			{Text: "On a break"},
		})
		m.OpenWith("W", "active", false, time.Time{})
		if got := m.currentItemLabels(); len(got) != 2 || got[1] != tc.want {
			t.Errorf("%s: current rows = %q, want Active and %q", tc.name, got, tc.want)
		}
	}
}

// "Snooze until tomorrow morning" recomputes its minutes on every
// rebuild; rows landing must not move the highlight off it.
func TestRecentRowsKeepRecomputedRowSelected(t *testing.T) {
	m := New()
	m.OpenWith("W", "active", false, time.Time{})
	for m.items[m.filtered[m.selected]].label != "Snooze until tomorrow morning" {
		m.HandleKey("down")
	}
	m.items[m.filtered[m.selected]].minutes-- // as if built a minute earlier
	m.SetRecent([]core.RecentStatus{{Text: "Lunch"}})
	if got := m.items[m.filtered[m.selected]].label; got != "Snooze until tomorrow morning" {
		t.Errorf("highlight moved to %q", got)
	}
}

// The separator line before the recent rows is drawn, counted in
// BoxSize, and skipped by clicks.
func TestRecentSeparatorGeometry(t *testing.T) {
	m := New()
	m.SetRecent([]core.RecentStatus{{Text: "Lunch"}})
	m.OpenWith("W", "active", false, time.Time{})

	box := m.renderBox(80)
	if _, h := m.BoxSize(80, 24); h != lipgloss.Height(box) {
		t.Errorf("BoxSize height = %d, rendered %d", h, lipgloss.Height(box))
	}
	sep := m.recentSeparator(m.window())
	if sep != 5 {
		t.Fatalf("separator before row %d, want 5", sep)
	}
	if !strings.Contains(strings.Split(box, "\n")[listTopOffset()+sep], "─") {
		t.Error("no separator line drawn above the first recent row")
	}
	top := listTopOffset()
	if m.ClickRow(80, 24, top+sep) {
		t.Error("click on the separator selected a row")
	}
	if !m.ClickRow(80, 24, top+sep+1) || m.items[m.filtered[m.selected]].label != "Lunch" {
		t.Errorf("click below the separator selected %q, want Lunch", m.items[m.filtered[m.selected]].label)
	}
	if !m.ClickRow(80, 24, top+sep+2) || m.items[m.filtered[m.selected]].label != "Snooze for 20 minutes" {
		t.Errorf("click two below the separator selected %q", m.items[m.filtered[m.selected]].label)
	}

	m.HandleKey("l") // a query drops the line
	if m.recentSeparator(m.window()) != -1 {
		t.Error("separator shown with a query")
	}
}

func TestRecentLabelDrawsEmoji(t *testing.T) {
	var m Model // image mode off: glyphs, else shortcodes
	for _, tc := range []struct {
		r    core.RecentStatus
		want string
	}{
		{core.RecentStatus{Text: "On a break", Emoji: ":coffee:"}, "☕ On a break"},
		{core.RecentStatus{Text: "Fire", Emoji: ":this-is-fine-fire:"}, ":this-is-fine-fire: Fire"},
		{core.RecentStatus{Emoji: ":coffee:"}, "☕"},
		{core.RecentStatus{Text: "Heads down"}, "Heads down"},
	} {
		if got, img := m.recentLabel(tc.r); got != tc.want || img != "" {
			t.Errorf("recentLabel(%+v) = %q, %q; want %q", tc.r, got, img, tc.want)
		}
	}
}

// With every optional row the menu scrolls instead of outgrowing an
// 80x24 terminal, and its geometry follows the window.
func TestMenuScrollsWithAllRows(t *testing.T) {
	m := New()
	var recent []core.RecentStatus
	for i := range MaxRecentRows {
		recent = append(recent, core.RecentStatus{Text: fmt.Sprintf("Recent %d", i)})
	}
	m.SetCurrentCustomStatus("Recent 0", "💬")
	m.SetRecent(recent)
	m.OpenWith("W", "active", true, time.Time{}) // DND on: End snooze row too

	for range len(m.filtered) {
		m.HandleKey("down")
	}
	box := m.renderBox(80)
	_, h := m.BoxSize(80, 24)
	if h != lipgloss.Height(box) || h > 24 {
		t.Fatalf("BoxSize height = %d, rendered %d, want them equal and <= 24", h, lipgloss.Height(box))
	}
	if !strings.Contains(box, "End snooze / DND") || strings.Contains(box, "Active") {
		t.Errorf("window should have scrolled to the last row:\n%s", box)
	}
	start, end := m.window()
	if !m.ClickRow(80, 24, listTopOffset()+(end-start)-1) || m.selected != len(m.filtered)-1 {
		t.Errorf("click on the last visible row selected %d, want %d", m.selected, len(m.filtered)-1)
	}
}

// A long recent status is cut between emoji, never inside a ZWJ one.
func TestRecentRowTruncatesWholeEmoji(t *testing.T) {
	fam := "👨\u200d👩\u200d👧"
	m := New()
	m.SetRecent([]core.RecentStatus{{Text: "Family day " + strings.Repeat(fam, 40)}})
	m.OpenWith("W", "active", false, time.Time{})
	box := m.renderBox(80)
	if !strings.Contains(box, "…") {
		t.Fatal("precondition: the row was not truncated")
	}
	if strings.Contains(box, "\u200d…") || strings.Contains(box, "👨…") || strings.Contains(box, "👩…") {
		t.Errorf("row cut inside a ZWJ emoji:\n%s", box)
	}
	w, _ := m.BoxSize(80, 24)
	for i, line := range strings.Split(box, "\n") {
		if got := ansi.Cells(line); got != w {
			t.Errorf("line %d is %d cells, want %d", i, got, w)
		}
	}
}
