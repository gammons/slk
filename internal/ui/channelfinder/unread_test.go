package channelfinder

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

func unreadFinder() Model {
	m := New()
	m.SetItems([]core.ChannelFinderItem{
		{ID: "C1", Name: "alpha", Type: "channel", Joined: true, LastVisited: 400},
		{ID: "C2", Name: "beta", Type: "channel", Joined: true, LastVisited: 300},
		{ID: "C3", Name: "gamma", Type: "channel", Joined: true, LastVisited: 200},
	})
	m.SetUnread(map[string]bool{"C3": true})
	m.Open()
	return m
}

// renderedNames is the body's lines from the first list row on, with the
// marker column and padding trimmed.
func renderedNames(m Model) []string {
	lines := strings.Split(ansi.Strip(m.renderBox(80)), "\n")
	var out []string
	for _, l := range lines[listTopOffset() : len(lines)-2] {
		out = append(out, strings.Trim(l, " │▌█"))
	}
	return out
}

// A line divides unread rows from read ones, and the box size and click
// mapping account for it.
func TestUnreadSeparator(t *testing.T) {
	m := unreadFinder()
	got := renderedNames(m)
	if len(got) != 4 || !strings.HasSuffix(got[0], "gamma") || strings.Trim(got[1], "─") != "" || got[1] == "" ||
		!strings.HasSuffix(got[2], "alpha") || !strings.HasSuffix(got[3], "beta") {
		t.Fatalf("rows = %q, want gamma, a ─ line, alpha, beta", got)
	}

	w, h := m.BoxSize(80, 24)
	box := m.renderBox(80)
	if lipgloss.Width(box) != w || lipgloss.Height(box) != h {
		t.Errorf("BoxSize = %dx%d, rendered %dx%d", w, h, lipgloss.Width(box), lipgloss.Height(box))
	}

	if m.ClickRow(80, 24, listTopOffset()+1) {
		t.Error("a click on the separator selected a row")
	}
	if !m.ClickRow(80, 24, listTopOffset()+2) || m.FilteredItems()[m.selected].ID != "C1" {
		t.Errorf("a click below the separator selected %d, want alpha", m.selected)
	}
	if !m.ClickRow(80, 24, listTopOffset()+3) || m.FilteredItems()[m.selected].ID != "C2" {
		t.Errorf("a click on the last row selected %d, want beta", m.selected)
	}
}

// Typing a query ranks by match, so there is no unread group to divide.
func TestUnreadSeparatorHiddenWhileSearching(t *testing.T) {
	m := unreadFinder()
	// alpha ranks first for "a", so an unread alpha would be followed by
	// read rows if the line were drawn for queries.
	m.SetUnread(map[string]bool{"C1": true})
	m.HandleKey("a")
	if m.Query() != "a" {
		t.Fatalf("query = %q, want a", m.Query())
	}
	for _, l := range renderedNames(m) {
		if strings.Contains(l, "──") {
			t.Fatalf("separator shown for a query: %q", renderedNames(m))
		}
	}
}

// The forward picker keeps recency order even after Ctrl+P handed the
// finder an unread set.
func TestForwardingIgnoresUnread(t *testing.T) {
	m := unreadFinder()
	m.Close()
	m.OpenForForwarding()
	var got []string
	for _, it := range m.FilteredItems() {
		got = append(got, it.ID)
	}
	if want := []string{"C1", "C2", "C3"}; !slices.Equal(got, want) {
		t.Errorf("forwarding order = %v, want %v", got, want)
	}
}

// At every scroll position the box is as tall as BoxSize says, and a
// click on each body line selects the row drawn there, or nothing on the
// separator.
func TestUnreadSeparatorAtEveryScrollPosition(t *testing.T) {
	var items []core.ChannelFinderItem
	unread := map[string]bool{}
	for i := range 2 * maxVisibleRows {
		id := fmt.Sprintf("C%02d", i)
		items = append(items, core.ChannelFinderItem{ID: id, Name: "chan" + id, Type: "channel", Joined: true, LastVisited: int64(1000 - i)})
		if i < maxVisibleRows-3 {
			unread[id] = true
		}
	}
	m := New()
	m.SetItems(items)
	m.SetUnread(unread)
	m.Open()
	sawSeparator := false
	for range len(items) {
		w, h := m.BoxSize(80, 40)
		box := m.renderBox(80)
		if lipgloss.Width(box) != w || lipgloss.Height(box) != h {
			t.Fatalf("selected %d: BoxSize %dx%d, rendered %dx%d", m.selected, w, h, lipgloss.Width(box), lipgloss.Height(box))
		}
		sel := m.selected
		for k, line := range renderedNames(m) {
			probe := m
			hit := probe.ClickRow(80, 40, listTopOffset()+k)
			if strings.Trim(line, "─") == "" {
				sawSeparator = true
				if hit {
					t.Fatalf("selected %d: click on the separator line %d selected a row", sel, k)
				}
				continue
			}
			if !hit || !strings.HasSuffix(line, probe.FilteredItems()[probe.selected].Name) {
				t.Fatalf("selected %d: click on line %d (%q) selected %v", sel, k, line, probe.FilteredItems()[probe.selected].Name)
			}
		}
		m.HandleKey("down")
	}
	if !sawSeparator {
		t.Fatal("the separator was never drawn")
	}
}
