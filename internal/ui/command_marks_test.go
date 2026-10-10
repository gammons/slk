// internal/ui/command_marks_test.go
//
// Tests for the :marks and :delmarks commands: the overlay command
// ignores the show_jump_overlay option, and the delete command reaches
// the same removal the overlay's row-delete action uses — including the
// persistence tier, so a deleted uppercase mark stays gone across a
// restart.
package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCmdMarks_OpensOverlayDespiteOptionOff(t *testing.T) {
	app := jumpMarkTestApp(t)
	app.SetShowJumpOverlay(false) // the option suppresses the ' chord overlay only

	if cmd := executeCommand(app, "marks"); cmd != nil {
		t.Fatalf(":marks returned a cmd: %v", cmd)
	}
	if app.mode != ModeMarks || !app.marksOverlay.IsVisible() {
		t.Fatalf(":marks must open the overlay with show_jump_overlay off, mode=%v visible=%v",
			app.mode, app.marksOverlay.IsVisible())
	}
}

func TestCmdMarks_RendersStoredMarks(t *testing.T) {
	app := jumpMarkTestApp(t)
	if err := app.marks.Set("T1", "a", Mark{
		Location:    Location{TeamID: "T1", ChannelID: "C2", MessageTS: "10.0"},
		Letter:      "a",
		ChannelName: "general",
		AuthorName:  "alice",
		Excerpt:     "hello world",
	}); err != nil {
		t.Fatalf("Set mark: %v", err)
	}

	_ = executeCommand(app, "marks")
	if got := len(mustListMarks(t, app.marks, "T1")); got != 1 {
		t.Fatalf("overlay holds %d rows, want 1", got)
	}
	if !strings.Contains(app.marksOverlay.View(80), "alice: hello world") {
		t.Fatalf("overlay must render the stored snapshot:\n%s", app.marksOverlay.View(80))
	}
}

func TestCmdDelMarks_SingleLetter(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})
	seedMark(t, app, "b", Location{TeamID: "T1", ChannelID: "C2", MessageTS: "2.0"})

	if cmd := executeCommand(app, "delmarks a"); cmd != nil {
		t.Fatalf(":delmarks returned a cmd: %v", cmd)
	}
	if _, ok, _ := app.marks.Load("T1", "a"); ok {
		t.Fatal("mark a must be removed")
	}
	if _, ok, _ := app.marks.Load("T1", "b"); !ok {
		t.Fatal("mark b must remain")
	}
}

// Both the compact run and the spaced form delete the same marks.
func TestCmdDelMarks_MultipleLetters(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"compact", "delmarks abc"},
		{"spaced", "delmarks a b c"},
		{"mixed", "delmarks ab c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := jumpMarkTestApp(t)
			for _, letter := range []string{"a", "b", "c", "d"} {
				seedMark(t, app, letter, Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})
			}

			_ = executeCommand(app, tc.line)
			for _, letter := range []string{"a", "b", "c"} {
				if _, ok, _ := app.marks.Load("T1", letter); ok {
					t.Fatalf("mark %s must be removed by %q", letter, tc.line)
				}
			}
			if _, ok, _ := app.marks.Load("T1", "d"); !ok {
				t.Fatal("mark d must remain")
			}
		})
	}
}

func TestCmdDelMarks_UnsetLetterIsNoop(t *testing.T) {
	app := jumpMarkTestApp(t)
	seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})

	if cmd := executeCommand(app, "delmarks z"); cmd != nil {
		t.Fatalf(":delmarks of an unset letter returned a cmd: %v", cmd)
	}
	if _, ok, _ := app.marks.Load("T1", "a"); !ok {
		t.Fatal("existing marks must be untouched")
	}
}

// The delete reaches the persistence tier: a deleted uppercase mark
// stays gone across a simulated restart (a delete that only cleared the
// session tier would resurrect it).
func TestCmdDelMarks_UppercaseStaysGoneAcrossRestart(t *testing.T) {
	persist := marksPersistForTest(t)
	app := jumpMarkTestApp(t)
	app.SetMarksPersistStore(persist)
	if err := app.marks.Set("T1", "A", Mark{Location: Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"}, Letter: "A"}); err != nil {
		t.Fatalf("Set A: %v", err)
	}

	_ = executeCommand(app, "delmarks A")

	// Restart: fresh session over the same persistence.
	restarted := jumpMarkTestApp(t)
	restarted.SetMarksPersistStore(persist)
	if _, ok, _ := restarted.marks.Load("T1", "A"); ok {
		t.Fatal("deleted uppercase mark resurrected after restart")
	}
}

// The overlay's row-delete action and :delmarks sit on the same
// removal: an uppercase mark deleted from the overlay also stays gone
// across a restart.
func TestCmdDelMarks_OverlayDeleteReachesSameTier(t *testing.T) {
	persist := marksPersistForTest(t)
	app := jumpMarkTestApp(t)
	app.SetMarksPersistStore(persist)
	if err := app.marks.Set("T1", "A", Mark{Location: Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"}, Letter: "A"}); err != nil {
		t.Fatalf("Set A: %v", err)
	}

	app.Update(tea.KeyPressMsg{Code: '\'', Text: "'"})
	app.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

	restarted := jumpMarkTestApp(t)
	restarted.SetMarksPersistStore(persist)
	if _, ok, _ := restarted.marks.Load("T1", "A"); ok {
		t.Fatal("uppercase mark deleted from the overlay resurrected after restart")
	}
}

// failingMarksPersist is a marks table whose every call fails.
func failingMarksPersist() MarksPersistStore {
	fail := errors.New("disk I/O error")
	return NewMarksPersistStore(MarksPersistStoreFuncs{
		Upsert: func(string, string, Mark) error { return fail },
		List:   func(string) ([]Mark, error) { return nil, fail },
		Delete: func(string, string) error { return fail },
	})
}

// A failing marks table must be reported, not read as "no such mark",
// an empty list, or a delete that worked and then comes back after a
// restart.
func TestMarks_TableFailuresToast(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(app *App) tea.Cmd
		want string
	}{
		{
			name: "jump to a saved mark",
			run: func(app *App) tea.Cmd {
				app.SetShowJumpOverlay(false)
				return pressJumpChord(app, 'A')
			},
			want: "Failed to read mark A",
		},
		{
			name: ":marks",
			run:  func(app *App) tea.Cmd { return executeCommand(app, "marks") },
			want: "Failed to load saved marks",
		},
		{
			name: ":delmarks",
			run:  func(app *App) tea.Cmd { return executeCommand(app, "delmarks aB") },
			want: "Failed to delete saved mark aB",
		},
		{
			name: "Backspace in the marks list",
			run: func(app *App) tea.Cmd {
				app.Update(keyPress('\''))
				_, cmd := app.Update(keyCode(tea.KeyBackspace))
				return cmd
			},
			want: "Failed to delete saved mark a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := jumpMarkTestApp(t)
			app.SetMarksPersistStore(failingMarksPersist())
			seedMark(t, app, "a", Location{TeamID: "T1", ChannelID: "C1", MessageTS: "1.0"})

			if cmd := tc.run(app); cmd == nil {
				t.Fatal("expected a toast command")
			}
			if got := statusbarText(app); !strings.Contains(got, tc.want) {
				t.Errorf("status bar = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
