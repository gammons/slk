package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/userprofile"
)

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

// profileModalApp opens the profile modal for U9 on an App whose avatar
// service renders "AVAV\nAVAV" as the half-block avatar, with sixel
// bytes only when withSixel. The App is viewed once before returning.
func profileModalApp(t *testing.T, withSixel bool) *App {
	t.Helper()
	a := newTestApp(t, withSize(80, 30))
	a.imgProtocol = imgpkg.ProtoSixel
	var sixelFn func(string) (string, []byte, bool)
	if withSixel {
		sixelFn = func(userID string) (string, []byte, bool) {
			return "avatar-" + userID, []byte("\x1bPq#0~-\x1b\\"), true
		}
	}
	a.SetAvatarService(core.NewSixelAvatarService(
		func(string) string { return "AVAV\nAVAV" }, nil, sixelFn,
	))
	a.userProfile.Open(userprofile.Seed{TeamID: "T1", UserID: "U9", DisplayName: "Dana"})
	a.SetMode(ModeUserProfile)
	_ = a.View()
	return a
}

// The profile modal's sixel avatar is published as the frame's one
// placement while the modal is open — pane images are withheld under an
// overlay — on exactly the cells the half-block avatar would draw on.
func TestCollectSixelPlacements_ProfileModalAvatar(t *testing.T) {
	blocky := profileModalApp(t, false)
	blockyPlain := ansi.Strip(blocky.View().Content)
	avatarRow, avatarCol, ok := locatePlainText(blockyPlain, "AVAV")
	if !ok {
		t.Fatalf("half-block frame shows no avatar:\n%s", blockyPlain)
	}

	a := profileModalApp(t, true)
	if plain := ansi.Strip(a.View().Content); strings.Contains(plain, "AVAV") {
		t.Errorf("sixel frame still draws the half-block avatar:\n%s", plain)
	}
	got := a.collectSixelPlacements(a.computeFrame())
	if len(got) != 1 {
		t.Fatalf("placements = %+v, want the profile avatar's one", got)
	}
	p := got[0]
	if p.Key != "avatar-U9" || p.Rows != 2 || p.Cols != 4 {
		t.Errorf("placement = %+v, want key avatar-U9, 2 rows x 4 cols", p)
	}
	if p.Row-1 != avatarRow || p.Col-1 != avatarCol {
		t.Errorf("placement at (%d,%d), want the half-block avatar's top-left cell (%d,%d)",
			p.Row-1, p.Col-1, avatarRow, avatarCol)
	}
}

// Without sixel bytes the modal draws the half-block avatar and places
// nothing: the dialog falls back exactly like the panes do.
func TestCollectSixelPlacements_ProfileModalWithoutSixel(t *testing.T) {
	a := profileModalApp(t, false)
	if got := a.collectSixelPlacements(a.computeFrame()); len(got) != 0 {
		t.Errorf("placements = %+v, want none", got)
	}
}

// Closing the modal hands placements back to the base screen: with no
// messages on it, none.
func TestCollectSixelPlacements_ProfileModalClosed(t *testing.T) {
	a := profileModalApp(t, true)
	a.userProfile.Close()
	a.SetMode(ModeNormal)
	_ = a.View()
	if got := a.collectSixelPlacements(a.computeFrame()); len(got) != 0 {
		t.Errorf("placements after close = %+v, want none", got)
	}
}
