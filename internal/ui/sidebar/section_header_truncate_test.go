package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/styles"
)

// Regression test for the section-header wrap bug: buildCache renders
// a header through styles.SectionHeader.Width(width-2), and lipgloss
// WRAPS overlong content rather than truncating it, while the
// renderRow it lands in hardcodes height 1. A long section name (or a
// mention badge pushing a borderline-length name over the edge) used
// to emit a multi-line string into a slot counted as one line, which
// shifted every row below it and visibly misaligned the sidebar's
// right border against the message pane.
//
// renderSectionHeaderLabel must now truncate the section name so the
// rendered header is always exactly one line at the given width.
func TestSectionHeader_LongNameDoesNotWrap(t *testing.T) {
	longName := "A Very Long Custom Section Name That Overflows"
	m := New([]ChannelItem{
		{ID: "C1", Name: "deploys", Type: "channel", Section: longName, SectionOrder: 1},
		{ID: "C2", Name: "general", Type: "channel"},
	})

	const width = 20
	view := m.View(15, width)

	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("rendered line exceeds width %d: %q (width=%d)", width, line, w)
		}
	}

	// The header line itself must still be discoverable and contain the
	// truncation ellipsis rather than being wrapped away.
	found := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "A Very") {
			found = true
			if !strings.Contains(line, "…") {
				t.Errorf("expected truncated header to contain an ellipsis, got %q", line)
			}
		}
	}
	if !found {
		t.Fatalf("section header line not found in view:\n%s", view)
	}

	// The number of rendered lines must match height passed to View,
	// i.e. no row silently expanded into two lines.
	lines := strings.Split(view, "\n")
	if len(lines) != 15 {
		t.Errorf("expected exactly 15 rendered lines (height), got %d:\n%s", len(lines), view)
	}
}

// Provider-backed cases exercising the emoji paths: skin tones, joined
// (ZWJ) emoji, long unknown shortcodes, and a long name. Both the normal
// and selected header labels must render as a single line at width 20.
func TestSectionHeader_EmojiPathsStayOneLine(t *testing.T) {
	cases := map[string]string{
		"skin tone":    "wave::skin-tone-3",
		"joined emoji": "male-construction-worker::skin-tone-2",
		"unknown long": "some-very-long-unknown-custom-emoji-shortcode",
		"plain":        "rocket",
		"no emoji":     "",
	}
	const width = 20
	for name, emojiCode := range cases {
		t.Run(name, func(t *testing.T) {
			items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
			m := New(items)
			m.SetSectionsProvider(&fakeProvider{
				ready:    true,
				sections: []SectionMeta{{ID: "A", Name: "A Very Long Section Name Here", Emoji: emojiCode, Type: "standard"}},
			})
			normal, selected, _ := m.renderSectionHeaderLabel("A", "▌", lipgloss.NewStyle(), "", width)
			for _, label := range []string{normal, selected} {
				out := styles.SectionHeader.Width(width - 2).Render(label)
				if strings.Contains(out, "\n") {
					t.Errorf("header wrapped at width %d: %q", width, out)
				}
				if w := lipgloss.Width(out); w > width {
					t.Errorf("header width %d > %d: %q", w, width, out)
				}
			}
		})
	}
}

// Collapsed header (unread aggregate) with a long unknown emoji shortcode
// must still fit in one line: the emoji prefix's separator space counts
// against the budget.
func TestSectionHeader_CollapsedLongEmojiStaysOneLine(t *testing.T) {
	const width = 20
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
	m := New(items)
	m.SetSectionsProvider(&fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Emoji: "some-very-long-unknown-custom-emoji-shortcode", Type: "standard"}},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true}}
	})
	if !m.IsCollapsed("A") {
		m.ToggleCollapse("A")
	}
	normal, selected, _ := m.renderSectionHeaderLabel("A", "▌", lipgloss.NewStyle(), "", width)
	for _, label := range []string{normal, selected} {
		out := styles.SectionHeader.Width(width - 2).Render(label)
		if strings.Contains(out, "\n") {
			t.Errorf("header wrapped at width %d: %q", width, out)
		}
	}
}
