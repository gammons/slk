package activityview

import (
	"fmt"
	"maps"
	"strings"
	"testing"
)

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
	if len(m.items) == 0 {
		// Not under test here: the empty state doesn't depend on windowing.
		panic("referenceView: only covers the non-empty body")
	}
	separator := blankLine(width)
	var lines []string
	for i, it := range m.items {
		if i > 0 {
			lines = append(lines, separator)
		}
		l1, l2 := m.renderCard(it, width, i == m.selected)
		lines = append(lines, l1, l2)
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
	ops := []struct {
		name string
		do   func(m *Model)
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
	// Heights straddle a card boundary (2 content lines, stride 3) and
	// include viewports taller than the whole list (n<=10 at h=40).
	for _, n := range []int{1, 2, 10, 57} {
		for _, height := range []int{1, 2, 3, 5, 40} {
			for _, width := range []int{20, 80} {
				t.Run(fmt.Sprintf("n=%d/h=%d/w=%d", n, height, width), func(t *testing.T) {
					got, want := newBenchModel(n), newBenchModel(n)
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

// TestView_CostIndependentOfListLength: View must render only the cards
// in the viewport, so a long feed costs about the same as one that just
// fills the screen.
func TestView_CostIndependentOfListLength(t *testing.T) {
	const height, width = 38, 120
	screenful := (height + 1) / cardStride // cards that fill the viewport
	allocs := func(n int) float64 {
		m := newBenchModel(n)
		_ = m.View(height, width)
		return testing.AllocsPerRun(3, func() {
			m.SetFocused(!m.Focused())
			_ = m.View(height, width)
		})
	}
	small, large := allocs(screenful), allocs(500)
	if large > 2*small {
		t.Errorf("View with 500 items allocates %.0f, vs %.0f for a screenful (%d); want < 2x — View is rendering off-screen cards",
			large, small, screenful)
	}
}

// TestSetItems_IdenticalListDoesNotBumpVersion: every activation of the
// Activity view refetches the feed, usually unchanged. An unconditional
// dirty() forces a full panel re-render, so identical contents must be a
// no-op.
func TestSetItems_IdenticalListDoesNotBumpVersion(t *testing.T) {
	m := newBenchModel(5)
	m.MoveDown()
	v0 := m.Version()

	items, _ := manyItems(5) // fresh slice, same contents
	m.SetItems(items)
	if v := m.Version(); v != v0 {
		t.Errorf("SetItems(identical contents) bumped Version: v0=%d v=%d", v0, v)
	}
	if m.SelectedIndex() != 1 {
		t.Errorf("SelectedIndex = %d, want 1 (unchanged)", m.SelectedIndex())
	}

	items[3].IsUnread = !items[3].IsUnread
	m.SetItems(items)
	if v := m.Version(); v == v0 {
		t.Errorf("SetItems(changed contents) did not bump Version")
	}
}

// A user scroll must survive an identical reload: the reload is not a
// selection move, so it must not snap the viewport back to the selection.
func TestSetItems_IdenticalListPreservesScroll(t *testing.T) {
	m := newBenchModel(50)
	_ = m.View(10, 40)
	m.ScrollDown(20)
	before := m.View(10, 40)

	items, _ := manyItems(50)
	m.SetItems(items)
	if after := m.View(10, 40); after != before {
		t.Errorf("identical SetItems moved the viewport")
	}
}

// TestSetBodies_IdenticalBodiesDoNotBumpVersion: each feed load is
// followed by a hydration fetch whose bodies are usually unchanged.
func TestSetBodies_IdenticalBodiesDoNotBumpVersion(t *testing.T) {
	m := newBenchModel(5)
	v0 := m.Version()

	_, bodies := manyItems(5) // fresh map, same contents
	m.SetBodies(bodies)
	if v := m.Version(); v != v0 {
		t.Errorf("SetBodies(identical contents) bumped Version: v0=%d v=%d", v0, v)
	}

	changed := maps.Clone(bodies)
	for k, b := range changed {
		b.Text += " (edited)"
		changed[k] = b
		break
	}
	m.SetBodies(changed)
	if v := m.Version(); v == v0 {
		t.Errorf("SetBodies(changed contents) did not bump Version")
	}
}
