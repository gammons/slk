package sidebar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/cache"
)

func TestChannelIndent_ShiftsSectionRowsOnly(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "a_rather_long_channel_name", Type: "channel"},
		{ID: "D1", Name: "priya", Type: "dm"},
	})
	m.ToggleCollapse("Channels")
	m.SetChannelIndent(3)
	view := m.View(12, 30)
	plain := func(row string) string { return ansiRe.ReplaceAllString(row, "") }

	ch := plain(rowFor(t, view, "a_rather"))
	if !strings.HasPrefix(ch, "     # a_rather") {
		t.Errorf("channel row not indented by 3: %q", ch)
	}
	if n := len([]rune(ch)); n != 28 {
		t.Errorf("indented row is %d cells, want 28 (name must truncate): %q", n, ch)
	}
	if hdr := plain(rowFor(t, view, "Channels")); strings.HasPrefix(hdr, "     ") {
		t.Errorf("section header indented too: %q", hdr)
	}
	if th := plain(rowFor(t, view, "Threads")); strings.HasPrefix(th, "     ") {
		t.Errorf("Threads row indented: %q", th)
	}
}

func TestChannelIndent_Clamped(t *testing.T) {
	m := New(nil)
	m.SetChannelIndent(40)
	if m.channelIndent != maxChannelIndent {
		t.Errorf("indent = %d, want clamp to %d", m.channelIndent, maxChannelIndent)
	}
	m.SetChannelIndent(-2)
	if m.channelIndent != 0 {
		t.Errorf("indent = %d, want 0", m.channelIndent)
	}
}

// TestChannelIndent_NarrowRowStaysOneRow: at a narrow width the indent
// gives way before the name's 5-cell floor pushes the badge off the row.
func TestChannelIndent_NarrowRowStaysOneRow(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "a_rather_long_channel_name", Type: "channel"}})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true, MentionCount: 120}}
	})
	m.ToggleCollapse("Channels")
	m.SetChannelIndent(6)
	row := ansiRe.ReplaceAllString(rowFor(t, m.View(12, 20), "a_ra"), "")
	if !strings.HasSuffix(row, "99+  ") || lipgloss.Width(row) > 20 {
		t.Errorf("row wrapped or overflowed 20 cells (%d): %q", lipgloss.Width(row), row)
	}
}

// TestPrivateGlyph_WideGlyphStaysInTheRow: a glyph wider than the
// default's 2 cells is charged to the name. An overflow by one cell
// doesn't always wrap: lipgloss may eat the row's right padding instead,
// so the row must still end with the badge and that padding.
func TestPrivateGlyph_WideGlyphStaysInTheRow(t *testing.T) {
	for _, glyph := range []string{"[priv]", "🔒"} {
		m := New([]ChannelItem{{ID: "G1", Name: "a_rather_long_private_name", Type: "private"}})
		m.SetReadStateReader(func() map[string]cache.ReadState {
			return map[string]cache.ReadState{"G1": {HasUnread: true, MentionCount: 120}}
		})
		m.ToggleCollapse("Channels")
		m.SetPrivateGlyph(glyph)
		view := m.View(12, 30)
		row := ansiRe.ReplaceAllString(rowFor(t, view, glyph), "")
		if !strings.HasSuffix(row, "99+  ") || lipgloss.Width(row) > 30 || strings.Count(view, "\n") != 11 {
			t.Errorf("%q: row overflowed (width %d, want the badge then its padding): %q", glyph, lipgloss.Width(row), row)
		}
	}
}
