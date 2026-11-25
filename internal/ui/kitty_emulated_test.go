package ui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/overlay"
)

// kittyEmoji is a 2x1 kitty placeholder image with the given ID, laid
// out the way the kitty renderer emits it.
func kittyEmoji(id uint32) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", byte(id>>16), byte(id>>8), byte(id)) +
		"\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
}

// emulatedKittyApp is a w x h App running kitty with emulated
// placement, wired to a frame store and a FrameOutput over out.
func emulatedKittyApp(t *testing.T, w, h int, mode imgpkg.KittyPlacement) (*App, *imgpkg.FrameOutput, *bytes.Buffer) {
	t.Helper()
	imgpkg.SetKittyPlacement(mode)
	t.Cleanup(func() { imgpkg.SetKittyPlacement(imgpkg.KittyNative) })
	a := newTestApp(t, withSize(w, h))
	a.SetImageProtocol(imgpkg.ProtoKitty)
	frames := imgpkg.NewSixelFrameStore()
	a.SetSixelFrameStore(frames)
	var out bytes.Buffer
	return a, imgpkg.NewFrameOutput(&out, frames), &out
}

// flushView writes v the way bubbletea flushes a frame: its title
// (which carries the frame marker), then the text.
func flushView(t *testing.T, fo *imgpkg.FrameOutput, v tea.View) {
	t.Helper()
	if _, err := fo.Write([]byte("\x1b]2;" + v.WindowTitle + "\a" + v.Content)); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeSixelView_EmulatedKittyResolvesPlaceholders(t *testing.T) {
	a, fo, out := emulatedKittyApp(t, 12, 2, imgpkg.KittyEmulated)
	content := "hi " + kittyEmoji(42) + " there\nline two"

	v := a.finalizeSixelView(tea.NewView(content), nil)
	if strings.ContainsRune(v.Content, imgpkg.PlaceholderRune) {
		t.Fatalf("placeholders reached bubbletea: %q", v.Content)
	}
	if got := ansi.Strip(v.Content); !strings.HasPrefix(got, "hi    there") {
		t.Fatalf("frame text = %q, want the emoji replaced by two spaces", got)
	}

	flushView(t, fo, v)
	got := out.String()
	if !strings.Contains(got, "\x1b[1;4H\x1b_Ga=p,i=42,p=") || !strings.Contains(got, ",c=2,r=1,C=1,q=2") {
		t.Fatalf("flush does not place image 42 at row 1 col 4: %q", got)
	}

	// A steady frame (the screen memo hands back the same string) is
	// not parsed again.
	a.finalizeSixelView(tea.NewView(content), nil)
	if a.kittyFrame.resolved != 1 {
		t.Fatalf("resolved %d frames, want 1 for two identical ones", a.kittyFrame.resolved)
	}
}

// Native placement (kitty, Ghostty) leaves the placeholders for the
// terminal and publishes no frames.
func TestFinalizeSixelView_NativeKittyLeavesPlaceholders(t *testing.T) {
	a, _, _ := emulatedKittyApp(t, 12, 2, imgpkg.KittyNative)
	a.windowTitle = "slk"
	content := "hi " + kittyEmoji(42)
	v := a.finalizeSixelView(tea.NewView(content), nil)
	if v.Content != content || v.WindowTitle != "slk" {
		t.Fatalf("native kitty frame changed: content %q title %q", v.Content, v.WindowTitle)
	}
}

// Under a modal, DimmedOverlay blanks the backdrop's placeholders and
// keeps the modal's own (the reaction picker's emoji), so only those
// are painted. The sixel rule that withholds every placement while an
// overlay is open must not apply here.
func TestFinalizeSixelView_EmulatedKittyUnderModal(t *testing.T) {
	const w, h = 30, 5
	a, _, _ := emulatedKittyApp(t, w, h, imgpkg.KittyEmulated)
	var bg strings.Builder
	for y := 0; y < h; y++ {
		bg.WriteString(kittyEmoji(77) + strings.Repeat(" ", w-2) + "\n")
	}
	box := "[" + kittyEmoji(42) + "]"
	screen := overlay.DimmedOverlay(w, h, strings.TrimSuffix(bg.String(), "\n"), box, 0.5)

	_, placements := a.resolveKittyFrame(screen)
	if len(placements) != 1 || !strings.Contains(string(placements[0].Bytes), "i=42,") {
		t.Fatalf("placements = %+v, want only the modal's image 42", placements)
	}
	if placements[0].Guard == "" {
		t.Fatal("placement has no guard")
	}
}

func TestReduceTerminalModes_TracksUnicodeCore(t *testing.T) {
	a := newTestApp(t)
	a.Update(tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeSet})
	if a.graphemeWidth {
		t.Fatal("synchronized-output report switched the width method")
	}
	a.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeNotRecognized})
	if a.graphemeWidth {
		t.Fatal("unrecognized unicode core switched the width method")
	}
	a.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModePermanentlySet})
	if !a.graphemeWidth {
		t.Fatal("unicode core report did not switch to grapheme widths")
	}
}
