// internal/ui/marks_mode_test.go
//
// Tests for the marks overlay at the App level: the ' chord opening it
// (or not, with the option off), immediate jumps, row selection,
// deletion, and snapshot-only rendering.
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
)

func TestMarksOverlay_OpensOnJumpByDefault(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	if app.mode != ModeMarks || !app.marksOverlay.IsVisible() {
		t.Fatalf("' must open the marks overlay, mode=%v visible=%v", app.mode, app.marksOverlay.IsVisible())
	}
}

func TestMarksOverlay_LetterPressJumpsImmediately(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})

	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})

	// The letter jumps in one press — the overlay must not add a
	// keystroke to the 'a flow.
	_, cmd := app.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("letter press while the overlay is open must jump immediately")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok {
		t.Fatalf("want ChannelSelectedMsg, got %T", cmd())
	}
	if cs.ID != "C2" {
		t.Fatalf("jumped to %q, want C2", cs.ID)
	}
	if app.mode != ModeNormal || app.marksOverlay.IsVisible() {
		t.Fatal("the jump must close the overlay")
	}
}

func TestMarksOverlay_SelectRowJumps(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	seedMark(t, app, "b", Location{TeamID: "T1", ChannelID: "C3", MessageTS: "20.0"})

	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	app.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must jump to the highlighted mark")
	}
	cs, ok := cmd().(ChannelSelectedMsg)
	if !ok || cs.ID != "C3" {
		t.Fatalf("selected row jumped to %T %+v, want C3", cmd(), cmd())
	}
	if app.mode != ModeNormal {
		t.Fatal("a row selection must close the overlay")
	}
}

func TestMarksOverlay_DeleteRemovesRowAndKeepsRest(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"})
	seedMark(t, app, "b", Location{TeamID: "T1", ChannelID: "C3", MessageTS: "20.0"})

	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	if _, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}); cmd != nil {
		t.Fatalf("delete produced a cmd: %v", cmd)
	}

	if _, ok, _ := app.marks.Load("T1", "a"); ok {
		t.Fatal("mark a must be removed by the delete action")
	}
	if _, ok, _ := app.marks.Load("T1", "b"); !ok {
		t.Fatal("mark b must remain")
	}
	// The overlay stays open showing the remaining marks.
	if !app.marksOverlay.IsVisible() || app.mode != ModeMarks {
		t.Fatal("the overlay must stay open after a delete")
	}
	view := app.marksOverlay.View(80)
	if !strings.Contains(view, "b") || strings.Contains(view, "No marks set") {
		t.Fatalf("overlay must show the remaining mark:\n%s", view)
	}
}

func TestMarksOverlay_EmptyState(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	if !strings.Contains(app.marksOverlay.View(80), "No marks set") {
		t.Fatalf("overlay with no marks must report that:\n%s", app.marksOverlay.View(80))
	}
}

// The overlay renders exclusively from the preview snapshot stored at
// mark time: opening and rendering it must make zero channel lookups.
func TestMarksOverlay_RendersFromSnapshotWithoutServiceCalls(t *testing.T) {
	app := jumpMarkTestApp(t)
	lookups := 0
	app.setChannelLookupFuncForTest(func(channelID ids.ChannelID) (string, string, bool) {
		lookups++
		return string(channelID) + "-name", "channel", true
	})
	if err := app.marks.Set("T1", "a", Mark{
		Location:    Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"},
		Letter:      "a",
		ChannelName: "general",
		AuthorName:  "alice",
		Excerpt:     "hello world",
	}); err != nil {
		t.Fatalf("Set mark: %v", err)
	}

	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	view := app.marksOverlay.View(80)

	if lookups != 0 {
		t.Fatalf("opening/rendering the overlay made %d channel lookups, want 0 (snapshot only)", lookups)
	}
	if !strings.Contains(view, "alice: hello world") {
		t.Fatalf("overlay did not render the stored snapshot:\n%s", view)
	}
}

func TestMarksOverlay_SuppressedWhenOptionOff(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.SetShowJumpOverlay(false)

	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	if app.mode == ModeMarks || app.marksOverlay.IsVisible() {
		t.Fatal("the overlay must not open when show_jump_overlay is off")
	}
	if !app.pendingJumpMark {
		t.Fatal("with the overlay off, ' must arm the silent pending-key flow")
	}
}

// Overlay rows render shortcodes as glyphs and stay on one line.
// A raw excerpt spilled newlines down the box, breaking the
// one-row-per-mark layout; unresolved :shortcode: text is also just
// wrong next to every other surface in the app.
func TestMarksOverlay_PreviewResolvesEmojiAndCollapsesNewlines(t *testing.T) {
	if got := previewText(":pretzel: lunch :pretzel:"); strings.Contains(got, ":pretzel:") {
		t.Errorf("shortcodes not resolved: %q", got)
	}
	got := previewText("Hello team,\nthe demo moves\n\nto Friday.")
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("newlines survived into a row: %q", got)
	}
	if !strings.Contains(got, "Hello team, the demo moves") {
		t.Errorf("collapse mangled the text: %q", got)
	}
}

// Ctrl+C leaves ModeMarks through the global quit intercept, not
// through handleMarksMode. The list must close with the mode, or it
// stays drawn over the quit prompt and, once the prompt is cancelled,
// over a channel whose keys are live underneath.
func TestMarksOverlay_ClosesWhenQuitPromptInterrupts(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.Update(keyPress('\''))
	if !app.marksOverlay.IsVisible() {
		t.Fatal("precondition: ' did not open the marks list")
	}

	app.Update(keyMod('c', tea.ModCtrl))
	if app.mode != ModeConfirm {
		t.Fatalf("mode = %v, want ModeConfirm", app.mode)
	}
	if app.marksOverlay.IsVisible() {
		t.Fatal("marks list still visible behind the quit prompt")
	}

	app.Update(keyCode(tea.KeyEscape))
	if app.mode != ModeNormal || app.marksOverlay.IsVisible() {
		t.Fatalf("after cancelling the prompt: mode=%v list visible=%v, want normal and closed", app.mode, app.marksOverlay.IsVisible())
	}
}
