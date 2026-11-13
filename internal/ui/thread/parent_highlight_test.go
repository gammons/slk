package thread

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// A selected thread parent is highlighted on every row of the message,
// including the body under a two-row avatar, not only the author line.
func TestSelectedParentHighlightCoversBody(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New()
	m.SetFocused(true)
	m.SetShowAvatars(true)
	// A real avatar is two rows of colored ▀ ending in a full reset.
	const avatar = "\x1b[38;2;30;30;128m\x1b[48;2;30;88;128m▀▀▀▀\x1b[0m\n\x1b[38;2;30;152;128m\x1b[48;2;30;210;128m▀▀▀▀\x1b[0m"
	m.SetAvatarFunc(func(string) string { return avatar })
	parent := messages.MessageItem{
		TS: "1.0", UserID: "U1", UserName: "alice",
		Text:      "the body of the parent is long enough that it wraps onto a line under the avatar",
		Timestamp: "4:15 PM",
	}
	m.SetThread(parent, []messages.MessageItem{
		{TS: "2.0", UserID: "U2", UserName: "bob", Text: "reply", Timestamp: "4:16 PM"},
	}, "C1", "1.0")
	m.MoveUp() // the only reply -> the parent

	_ = m.View(24, 60)
	lines := m.list.Entries()[0].Lines
	// Drop the rule under the parent.
	if len(lines) > 0 && strings.Contains(ansi.Strip(lines[len(lines)-1]), "─") {
		lines = lines[:len(lines)-1]
	}
	r, g, b, _ := styles.SelectionTintColor(true).RGBA()
	tint := fmtRGBBg(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	ar, ag, ab, _ := styles.SelectionBorderColor(true).RGBA()
	bar := "38;2;" + itoaU8(uint8(ar>>8)) + ";" + itoaU8(uint8(ag>>8)) + ";" + itoaU8(uint8(ab>>8))
	if len(lines) < 2 {
		t.Fatalf("parent is %d lines, want header and body", len(lines))
	}
	br, bgc, bb, _ := styles.Background.RGBA()
	bg := fmtRGBBg(uint8(br>>8), uint8(bgc>>8), uint8(bb>>8))
	for i, line := range lines {
		plain := ansi.Strip(line)
		if !strings.Contains(line, tint) || !strings.Contains(line, bar) {
			t.Errorf("parent line %d %q missing highlight", i, plain)
		}
		// The row fill, not only the ▌ cell, should be the selection tint.
		// A theme-background run after the bar means that row is unhighlighted.
		afterBar := line
		if j := strings.Index(line, "▌"); j >= 0 {
			afterBar = line[j+len("▌"):]
		}
		if strings.Contains(afterBar, bg) && !strings.Contains(plain, "─") {
			t.Errorf("parent line %d %q is filled with the theme background, not the selection tint", i, plain)
		}
	}
}
