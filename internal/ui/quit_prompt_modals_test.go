package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/linkpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/userprofile"
)

// promptInFrame reports whether the quit prompt can be read in the
// composited overlays, which is what a modal drawn over it would prevent.
func promptInFrame(a *App) bool {
	blank := strings.TrimSuffix(strings.Repeat(strings.Repeat(" ", a.width)+"\n", a.height), "\n")
	return strings.Contains(ansi.Strip(a.applyOverlays(blank)), "Quit slk?")
}

// Ctrl+C leaves a modal's mode through the global quit intercept, not through
// the modal's own key handler. Every modal must close with its mode, or it is
// drawn over the quit prompt and, once the prompt is cancelled, over a screen
// whose keys are live underneath.
func TestQuitPromptClosesEveryModal(t *testing.T) {
	cases := map[Mode]struct {
		open    func(a *App)
		visible func(a *App) bool // nil: the mode itself draws it
	}{
		ModeChannelFinder: {
			func(a *App) { a.channelFinder.Open() },
			func(a *App) bool { return a.channelFinder.IsVisible() },
		},
		ModeReactionPicker: {
			func(a *App) { a.reactionPicker.Open("C1", "1.0", nil) },
			func(a *App) bool { return a.reactionPicker.IsVisible() },
		},
		ModeWorkspaceFinder: {
			func(a *App) { a.workspaceFinder.Open() },
			func(a *App) bool { return a.workspaceFinder.IsVisible() },
		},
		ModeThemeSwitcher: {
			func(a *App) { a.themeSwitcher.Open() },
			func(a *App) bool { return a.themeSwitcher.IsVisible() },
		},
		ModePresenceMenu: {
			func(a *App) { a.presenceMenu.OpenWith("Acme", "active", false, a.now()) },
			func(a *App) bool { return a.presenceMenu.IsVisible() },
		},
		ModePresenceCustomSnooze: {func(*App) {}, nil},
		ModeHelp: {
			func(a *App) { a.help.Open() },
			func(a *App) bool { return a.help.IsVisible() },
		},
		ModeNewMessage: {
			func(a *App) { a.newMessagePicker.Open() },
			func(a *App) bool { return a.newMessagePicker.IsVisible() },
		},
		ModeReactionsView: {
			func(a *App) { a.reactionsView.Open(nil) },
			func(a *App) bool { return a.reactionsView.IsVisible() },
		},
		ModeLinkPicker: {
			func(a *App) { a.linkPicker.Open("Open link", []linkpicker.Item{{URL: "https://example.com"}}) },
			func(a *App) bool { return a.linkPicker.IsVisible() },
		},
		ModeWorkspaceSearch: {
			func(a *App) { a.searchResults.Open() },
			func(a *App) bool { return a.searchResults.IsVisible() },
		},
		ModeUserProfile: {
			func(a *App) { a.userProfile.Open(userprofile.Seed{TeamID: "T1", UserID: "U1"}) },
			func(a *App) bool { return a.userProfile.IsVisible() },
		},
		ModeMarks: {
			func(a *App) { a.marksOverlay.Open() },
			func(a *App) bool { return a.marksOverlay.IsVisible() },
		},
	}

	// A new modal mode must be added here, and to closeModal.
	for m := Mode(0); m < modeCount; m++ {
		if _, ok := cases[m]; m.IsModalOverlay() && m != ModeConfirm && !ok {
			t.Errorf("modal mode %v has no case", m)
		}
	}

	for mode, c := range cases {
		t.Run(mode.String(), func(t *testing.T) {
			a := newTestApp(t, withSize(120, 40))
			c.open(a)
			a.SetMode(mode)
			if c.visible != nil && !c.visible(a) {
				t.Fatal("precondition: modal did not open")
			}

			a.Update(keyMod('c', tea.ModCtrl))
			if a.mode != ModeConfirm {
				t.Fatalf("mode = %v, want ModeConfirm", a.mode)
			}
			if c.visible != nil && c.visible(a) {
				t.Error("modal still visible behind the quit prompt")
			}
			if !promptInFrame(a) {
				t.Error("quit prompt is not readable in the frame")
			}

			a.Update(keyCode(tea.KeyEscape))
			if a.mode != ModeNormal {
				t.Errorf("after cancelling: mode = %v, want ModeNormal", a.mode)
			}
			if c.visible != nil && c.visible(a) {
				t.Error("modal came back after cancelling the quit prompt")
			}
		})
	}
}

// Ctrl+C closes the picker, so it must cancel its in-flight open as Esc does.
// A late result would otherwise switch channel and enter insert mode with the
// quit prompt still up.
func TestQuitPromptCancelsNewMessageInFlight(t *testing.T) {
	for name, late := range map[string]func(id uint64) tea.Msg{
		"opened": func(id uint64) tea.Msg {
			return NewMessageOpenedMsg{ChannelID: "D1", UserIDs: []string{"U2"}, RequestID: id}
		},
		"failed": func(id uint64) tea.Msg { return NewMessageFailedMsg{RequestID: id, Err: errors.New("boom")} },
	} {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(t, withSize(120, 40))
			a.newMessagePicker.Open()
			a.SetMode(ModeNewMessage)
			a.newMessageInFlightID = 7

			a.Update(keyMod('c', tea.ModCtrl))
			if !a.newMessageCancelled {
				t.Error("in-flight open not cancelled")
			}

			if _, cmd := a.Update(late(7)); cmd != nil {
				t.Error("cancelled result produced a command")
			}
			if a.mode != ModeConfirm || !a.confirmPrompt.IsVisible() {
				t.Errorf("late result disturbed the prompt: mode=%v visible=%v", a.mode, a.confirmPrompt.IsVisible())
			}
		})
	}
}

// Finishing an open is not cancelling it: the mode change it makes on the way
// to insert mode must leave no cancelled flag for the next open to inherit.
func TestNewMessageOpenedDoesNotCancelItself(t *testing.T) {
	a := newTestApp(t, withSize(120, 40))
	a.newMessagePicker.Open()
	a.SetMode(ModeNewMessage)
	a.newMessageInFlightID = 7

	a.Update(NewMessageOpenedMsg{ChannelID: "D1", UserIDs: []string{"U2"}, RequestID: 7})
	if a.mode != ModeInsert || a.newMessageCancelled || a.newMessagePicker.IsVisible() {
		t.Errorf("mode=%v cancelled=%v picker visible=%v, want insert, false, false",
			a.mode, a.newMessageCancelled, a.newMessagePicker.IsVisible())
	}
}

// The link picker's kind and files describe the open modal, so they go with it.
func TestQuitPromptClearsLinkPickerState(t *testing.T) {
	a := newTestApp(t, withSize(120, 40))
	a.pickerKind, a.pickerFiles = "files", []messages.Attachment{{Name: "a.pdf"}}
	a.linkPicker.Open("Download file", []linkpicker.Item{{URL: "https://example.com/a.pdf"}})
	a.SetMode(ModeLinkPicker)

	a.Update(keyMod('c', tea.ModCtrl))
	if a.pickerKind != "" || a.pickerFiles != nil {
		t.Errorf("pickerKind = %q, pickerFiles = %v, want both cleared", a.pickerKind, a.pickerFiles)
	}
}
