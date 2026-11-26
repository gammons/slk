package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

type customStatusCall struct {
	text, emoji string
	expiration  int64
}

// statusDialogApp is an App whose presence service records custom
// status calls, with the status dialog opened the way a user opens it:
// Ctrl+S, filter to "Set a status...", Enter.
func statusDialogApp(t *testing.T) (*App, *[]customStatusCall) {
	t.Helper()
	a := newTestApp(t, withActiveTeam("T1"))
	var calls []customStatusCall
	a.SetPresenceService(core.NewPresenceService(nil, nil, func(text, emoji string, expiration int64) {
		calls = append(calls, customStatusCall{text, emoji, expiration})
	}, nil))
	pressKey(t, a, keyMod('s', tea.ModCtrl))
	typeAppKeys(t, a, "set")
	pressKey(t, a, keyCode(tea.KeyEnter))
	if a.mode != ModeStatusDialog || !a.statusDialog.IsVisible() {
		t.Fatalf("precondition: mode = %v, want the status dialog", a.mode)
	}
	return a, &calls
}

func typeAppKeys(t *testing.T, a *App, s string) {
	t.Helper()
	for _, r := range s {
		pressKey(t, a, keyPress(r))
	}
}

func TestStatusDialogSetsEmojiTextAndExpiry(t *testing.T) {
	a, calls := statusDialogApp(t)

	pressKey(t, a, keyCode(tea.KeyTab))
	if a.mode != ModeReactionPicker || !a.reactionPicker.IsVisible() {
		t.Fatalf("Tab: mode = %v, want the emoji picker", a.mode)
	}
	typeAppKeys(t, a, "coffee")
	pressKey(t, a, keyCode(tea.KeyEnter))
	if a.mode != ModeStatusDialog || a.statusDialog.Emoji() != "coffee" {
		t.Fatalf("after the pick: mode = %v emoji = %q, want the dialog with coffee", a.mode, a.statusDialog.Emoji())
	}

	typeAppKeys(t, a, "On a break")
	// Today is the default; three rows up is 30 minutes.
	for range 3 {
		pressKey(t, a, keyCode(tea.KeyUp))
	}
	before := time.Now()
	pressKey(t, a, keyCode(tea.KeyEnter))
	after := time.Now()

	if a.mode != ModeNormal || a.statusDialog.IsVisible() {
		t.Errorf("after Enter: mode = %v, dialog visible = %v", a.mode, a.statusDialog.IsVisible())
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %v, want one", *calls)
	}
	c := (*calls)[0]
	if c.text != "On a break" || c.emoji != ":coffee:" {
		t.Errorf("set %q %q, want \"On a break\" \":coffee:\"", c.text, c.emoji)
	}
	if lo, hi := before.Add(30*time.Minute).Unix(), after.Add(30*time.Minute).Unix(); c.expiration < lo || c.expiration > hi {
		t.Errorf("expiration = %d, want 30 minutes from now (%d..%d)", c.expiration, lo, hi)
	}
}

func TestStatusDialogTextOnlyDontClear(t *testing.T) {
	a, calls := statusDialogApp(t)
	typeAppKeys(t, a, "Heads down")
	for range 10 {
		pressKey(t, a, keyCode(tea.KeyUp)) // clamps at Don't clear
	}
	pressKey(t, a, keyCode(tea.KeyEnter))
	if want := []customStatusCall{{"Heads down", "", 0}}; len(*calls) != 1 || (*calls)[0] != want[0] {
		t.Errorf("calls = %v, want %v", *calls, want)
	}
}

func TestStatusDialogPasteFlattensLines(t *testing.T) {
	a, calls := statusDialogApp(t)
	a.Update(tea.PasteMsg{Content: "In a\nmeeting"})
	pressKey(t, a, keyCode(tea.KeyEnter))
	if len(*calls) != 1 || (*calls)[0].text != "In a meeting" {
		t.Errorf("calls = %v, want the pasted text on one line", *calls)
	}
}

func TestStatusDialogRefusesEmptyStatus(t *testing.T) {
	a, calls := statusDialogApp(t)
	typeAppKeys(t, a, "  ")
	pressKey(t, a, keyCode(tea.KeyEnter))
	if len(*calls) != 0 {
		t.Errorf("calls = %v, want none", *calls)
	}
	if a.mode != ModeStatusDialog || a.statusDialog.ErrText() == "" {
		t.Errorf("mode = %v err = %q, want the dialog with an error", a.mode, a.statusDialog.ErrText())
	}
}

func TestStatusDialogCancel(t *testing.T) {
	t.Run("Esc in the dialog", func(t *testing.T) {
		a, calls := statusDialogApp(t)
		typeAppKeys(t, a, "Lunch")
		pressKey(t, a, keyCode(tea.KeyEscape))
		if a.mode != ModeNormal || a.statusDialog.IsVisible() || len(*calls) != 0 {
			t.Errorf("mode = %v visible = %v calls = %v, want closed with no call", a.mode, a.statusDialog.IsVisible(), *calls)
		}
	})
	t.Run("Esc in the emoji picker returns to the dialog", func(t *testing.T) {
		a, calls := statusDialogApp(t)
		typeAppKeys(t, a, "Lunch")
		pressKey(t, a, keyCode(tea.KeyTab))
		pressKey(t, a, keyCode(tea.KeyEscape))
		if a.mode != ModeStatusDialog || a.statusDialog.Text() != "Lunch" || a.statusDialog.Emoji() != "" {
			t.Fatalf("mode = %v text = %q emoji = %q, want the dialog as it was", a.mode, a.statusDialog.Text(), a.statusDialog.Emoji())
		}
		if a.statusEmojiPick || len(*calls) != 0 {
			t.Errorf("statusEmojiPick = %v calls = %v, want reset and none", a.statusEmojiPick, *calls)
		}
	})
}

// A reaction opened from a message after the dialog closed is still a
// reaction: the emoji-pick routing must not outlive the dialog.
func TestStatusEmojiPickDoesNotLeakIntoReactions(t *testing.T) {
	a, _ := statusDialogApp(t)
	pressKey(t, a, keyCode(tea.KeyTab))
	pressKey(t, a, keyMod('c', tea.ModCtrl)) // quit prompt closes every modal
	if a.statusEmojiPick || a.statusDialog.IsVisible() {
		t.Errorf("after ctrl+c: statusEmojiPick = %v dialog visible = %v, want both reset", a.statusEmojiPick, a.statusDialog.IsVisible())
	}
}
