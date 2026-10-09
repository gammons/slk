package threadsview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
)

// manySummaries returns n distinct summaries whose timestamps are far
// enough in the past that formatRelTime ("Nd ago") is stable across the
// two renders a test compares.
func manySummaries(n int) []cache.ThreadSummary {
	out := make([]cache.ThreadSummary, n)
	for i := range out {
		out[i] = cache.ThreadSummary{
			ChannelID:    fmt.Sprintf("C%04d", i),
			ChannelName:  fmt.Sprintf("ch-%04d", i),
			ChannelType:  []string{"channel", "private", "dm"}[i%3],
			ThreadTS:     fmt.Sprintf("%d.000000", 1700000000+i),
			ParentUserID: "U1",
			ParentText:   fmt.Sprintf("parent %d with *bold* and `code` and <@U2> mention", i),
			ParentTS:     fmt.Sprintf("%d.000000", 1700000000+i),
			ReplyCount:   1 + i%4,
			LastReplyTS:  fmt.Sprintf("%d.000000", 1700000100+i),
			LastReplyBy:  "U2",
			Unread:       i%3 == 0,
		}
	}
	return out
}

// referenceView is the original, un-windowed View algorithm: render every
// card, then slice out the viewport. Windowed rendering must stay
// byte-identical to it (and leave yOffset in the same place).
func referenceView(m *Model, height, width int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if len(m.summaries) == 0 || !m.subscriptionsAvailable {
		// Not under test here: these branches don't depend on windowing.
		panic("referenceView: only covers the non-empty, no-banner body")
	}
	separator := blankLine(width)
	var lines []string
	for i, s := range m.summaries {
		if i > 0 {
			lines = append(lines, separator)
		}
		lines = append(lines, m.renderCard(s, width, i == m.selected)...)
	}
	if !m.hasSnapped || m.snappedSelection != m.selected {
		m.snapToSelected(height, len(lines))
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	maxOffset := len(lines) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	end := m.yOffset + height
	if end > len(lines) {
		end = len(lines)
	}
	visible := append([]string(nil), lines[m.yOffset:end]...)
	for len(visible) < height {
		visible = append(visible, blankLine(width))
	}
	return strings.Join(visible, "\n")
}

// TestView_MatchesUnwindowedReference drives two identical models through
// the same selection / scroll / focus operations and requires View to
// match the un-windowed reference render at every step, including the
// resulting scroll offset.
func TestView_MatchesUnwindowedReference(t *testing.T) {
	type op func(m *Model)
	ops := []struct {
		name string
		do   op
	}{
		{"initial", func(*Model) {}},
		{"down", func(m *Model) { m.MoveDown() }},
		{"down x7", func(m *Model) {
			for i := 0; i < 7; i++ {
				m.MoveDown()
			}
		}},
		{"unfocus", func(m *Model) { m.SetFocused(false) }},
		{"focus", func(m *Model) { m.SetFocused(true) }},
		{"scroll down 5", func(m *Model) { m.ScrollDown(5) }},
		{"scroll down 2", func(m *Model) { m.ScrollDown(2) }},
		{"scroll up 3", func(m *Model) { m.ScrollUp(3) }},
		{"scroll down 10000", func(m *Model) { m.ScrollDown(10000) }},
		{"up", func(m *Model) { m.MoveUp() }},
		{"bottom", func(m *Model) { m.GoToBottom() }},
		{"top", func(m *Model) { m.GoToTop() }},
	}
	// Heights straddle a card boundary (3 content lines, stride 4) and
	// include viewports taller than the whole list (n<=10 at h=40).
	for _, n := range []int{1, 2, 10, 57} {
		for _, height := range []int{1, 3, 4, 7, 40} {
			for _, width := range []int{20, 80} {
				name := fmt.Sprintf("n=%d/h=%d/w=%d", n, height, width)
				t.Run(name, func(t *testing.T) {
					names := map[string]string{"U1": "alice", "U2": "bob"}
					got := New(names, "USELF")
					want := New(names, "USELF")
					got.SetSummaries(manySummaries(n))
					want.SetSummaries(manySummaries(n))
					got.SetFocused(true)
					want.SetFocused(true)
					for _, o := range ops {
						o.do(&got)
						o.do(&want)
						g := got.View(height, width)
						w := referenceView(&want, height, width)
						if g != w {
							t.Fatalf("after %q: View differs from reference\n got: %q\nwant: %q", o.name, g, w)
						}
						if got.yOffset != want.yOffset {
							t.Fatalf("after %q: yOffset = %d, reference %d", o.name, got.yOffset, want.yOffset)
						}
					}
				})
			}
		}
	}
}

// TestView_CostIndependentOfListLength pins the fix for the h/l focus
// lag in the Threads view: View must render only the cards in the
// viewport, so a 1000-thread list costs about the same as one that just
// fills the screen. Before windowing, a 1000-thread View allocated ~33x
// as much as a screenful (and took ~300ms at 420x121).
func TestView_CostIndependentOfListLength(t *testing.T) {
	const height, width = 119, 120
	screenful := (height + 1) / cardStride // cards that fill the viewport
	allocs := func(n int) float64 {
		m := New(map[string]string{"U1": "alice", "U2": "bob"}, "USELF")
		m.SetSummaries(manySummaries(n))
		_ = m.View(height, width)
		return testing.AllocsPerRun(3, func() {
			m.SetFocused(!m.Focused())
			_ = m.View(height, width)
		})
	}
	small, large := allocs(screenful), allocs(1000)
	if large > 2*small {
		t.Errorf("View with 1000 threads allocates %.0f, vs %.0f for a screenful (%d); want < 2x — View is rendering off-screen cards",
			large, small, screenful)
	}
}

// TestSetSummaries_IdenticalListDoesNotBumpVersion: the threads list is
// reloaded on every thread reply and mark-read, usually unchanged. An
// unconditional dirty() there forces a full panel re-render on each
// reload, so identical contents must be a no-op.
func TestSetSummaries_IdenticalListDoesNotBumpVersion(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(manySummaries(5))
	m.MoveDown()
	v0 := m.Version()

	m.SetSummaries(manySummaries(5)) // fresh slice, same contents
	if v := m.Version(); v != v0 {
		t.Errorf("SetSummaries(identical contents) bumped Version: v0=%d v=%d", v0, v)
	}
	if m.SelectedIndex() != 1 {
		t.Errorf("SelectedIndex = %d, want 1 (unchanged)", m.SelectedIndex())
	}

	changed := manySummaries(5)
	changed[3].Unread = !changed[3].Unread
	m.SetSummaries(changed)
	if v := m.Version(); v == v0 {
		t.Errorf("SetSummaries(changed contents) did not bump Version")
	}
}

// A user scroll must survive an identical reload: the reload is not a
// selection move, so it must not snap the viewport back to the selection.
func TestSetSummaries_IdenticalListPreservesScroll(t *testing.T) {
	m := New(map[string]string{}, "USELF")
	m.SetSummaries(manySummaries(50))
	_ = m.View(10, 40)
	m.ScrollDown(20)
	before := m.View(10, 40)

	m.SetSummaries(manySummaries(50))
	if after := m.View(10, 40); after != before {
		t.Errorf("identical SetSummaries moved the viewport")
	}
}
