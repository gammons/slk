package userprofile

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const testSixel = "\x1bPq#0~-\x1b\\"

// locatePlainText returns the 0-based row and display column of the
// first occurrence of needle in a plain (ANSI-stripped) frame.
func locatePlainText(plain, needle string) (row, col int, ok bool) {
	for r, line := range strings.Split(plain, "\n") {
		if i := strings.Index(line, needle); i >= 0 {
			return r, lipgloss.Width(line[:i]), true
		}
	}
	return 0, 0, false
}

// The sixel avatar blanks its cells in the box text and reports the
// placement where the painter draws it: exactly the cells the
// half-block avatar would have drawn on.
func TestViewOverlay_SixelAvatarIsABlankedPlacement(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})

	// The half-block frame shows the avatar text; its top-left cell is
	// the one the sixel placement must land on. Both frames draw with
	// the same Live footprint (two 4-col avatar lines), so the box and
	// everything in it land identically.
	blocky := ansi.Strip(m.ViewOverlay(80, 24, "", Live{Now: time.Now(), Avatar: "AAAA\nAAAA"}))
	avatarRow, avatarCol, ok := locatePlainText(blocky, "AAAA")
	if !ok {
		t.Fatalf("half-block frame shows no avatar:\n%s", blocky)
	}

	live := Live{
		Now:            time.Now(),
		Avatar:         "AAAA\nAAAA",
		AvatarSixelKey: "avatar-U1-abc",
		AvatarSixel:    []byte(testSixel),
	}
	out := m.ViewOverlay(80, 24, "", live)
	if stripped := ansi.Strip(out); strings.Contains(stripped, "AAAA") {
		t.Errorf("sixel frame still draws the half-block avatar:\n%s", stripped)
	}
	p := m.SixelPaint()
	if p == nil {
		t.Fatal("SixelPaint() = nil after a frame with a sixel avatar")
	}
	if p.Key != "avatar-U1-abc" || p.Rows != 2 || p.Cols != 4 {
		t.Errorf("placement = %+v, want key avatar-U1-abc, 2 rows x 4 cols", p)
	}
	if p.Row != avatarRow || p.Col != avatarCol {
		t.Errorf("placement at (%d,%d), want the half-block avatar's top-left cell (%d,%d)", p.Row, p.Col, avatarRow, avatarCol)
	}
	if string(p.Bytes) != testSixel {
		t.Errorf("placement bytes = %q, want %q", p.Bytes, testSixel)
	}
}

// Without sixel bytes the half-block avatar draws as text and nothing
// is placed — the pre-sixel behavior, which is also the panes'
// fallback.
func TestViewOverlay_HalfBlockAvatarPlacesNothing(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	out := m.ViewOverlay(80, 24, "", Live{Now: time.Now(), Avatar: "AAAA\nAAAA"})
	if !strings.Contains(ansi.Strip(out), "AAAA") {
		t.Errorf("half-block frame lost its avatar:\n%s", ansi.Strip(out))
	}
	if p := m.SixelPaint(); p != nil {
		t.Errorf("SixelPaint() = %+v, want nil without sixel bytes", p)
	}
}

// An avatar with no sixel bytes and sixel bytes with no avatar are both
// the half-block path: blanking needs the footprint, placing needs the
// image.
func TestViewOverlay_SixelAvatarNeedsAvatarAndBytes(t *testing.T) {
	for name, live := range map[string]Live{
		"no avatar": {Now: time.Now(), AvatarSixelKey: "k", AvatarSixel: []byte(testSixel)},
		"no sixel":  {Now: time.Now(), Avatar: "AAAA\nAAAA"},
		"neither":   {Now: time.Now()},
	} {
		t.Run(name, func(t *testing.T) {
			m := New()
			m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
			m.ViewOverlay(80, 24, "", live)
			if p := m.SixelPaint(); p != nil {
				t.Errorf("SixelPaint() = %+v, want nil", p)
			}
		})
	}
}

// The painter draws exactly where it is told and never clips: a slot
// that reaches past a short terminal's frame keeps the half-block
// avatar, whose text clips with the card — not blanked cells with no
// image over them.
func TestViewOverlay_SixelAvatarFitsTheScreen(t *testing.T) {
	live := Live{
		Now:            time.Now(),
		Avatar:         "AAAA\nAAAA",
		AvatarSixelKey: "avatar-U1-abc",
		AvatarSixel:    []byte(testSixel),
	}

	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	m.ViewOverlay(80, 24, "", live)
	if p := m.SixelPaint(); p == nil {
		t.Fatal("SixelPaint() = nil on a screen the card fits")
	}

	// A 4-row terminal is too short for the card: the avatar's cells
	// fall past the clipped frame, so the half-block avatar draws in
	// their place and nothing is placed.
	m = New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	out := m.ViewOverlay(80, 4, "", live)
	if p := m.SixelPaint(); p != nil {
		t.Errorf("SixelPaint() = %+v on a 4-row screen, want nil", p)
	}
	plain := ansi.Strip(out)
	if row, _, ok := locatePlainText(plain, "AAAA"); !ok {
		t.Errorf("4-row screen lost the half-block avatar:\n%s", plain)
	} else if row >= 4 {
		t.Errorf("half-block avatar on row %d of a 4-row screen (not visible)", row)
	}
	if strings.Contains(plain, "Priya") && !strings.Contains(plain, "AAAA  Priya") {
		t.Errorf("name row lost its avatar gutter:\n%s", plain)
	}
}
