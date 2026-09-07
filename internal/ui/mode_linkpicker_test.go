package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/messages"
)

// Named mode_linkpicker_test.go, not mode_link_picker_test.go: the
// production file is internal/ui/mode_linkpicker.go and test files
// mirror their source file's name in this package.

// openLinkPicker drives the production `o` path
// (App.openLinksOfSelected) with a two-link message.
func openLinkPicker(t *testing.T, a *App) {
	a.focusedPanel = PanelMessages
	a.messagepane.SetMessages([]messages.MessageItem{{
		TS:   "1.0",
		Text: "<https://a.example/1|one> and <https://b.example/2|two>",
	}})
	if cmd := a.openLinksOfSelected(); cmd != nil {
		t.Fatalf("precondition: expected the modal path, got a cmd: %#v", cmd())
	}
	if !a.linkPicker.IsVisible() {
		t.Fatal("precondition: link picker did not open")
	}
	if got := len(a.linkPicker.Items()); got != 2 {
		t.Fatalf("precondition: %d picker items, want 2", got)
	}
	if a.pickerKind != "links" {
		t.Fatalf("precondition: pickerKind = %q, want %q", a.pickerKind, "links")
	}
}

// openFilePicker drives the production `d` path
// (App.downloadFilesOfSelected) with a two-attachment message. The
// picker is the same modal; only pickerKind differs.
func openFilePicker(t *testing.T, a *App) {
	a.focusedPanel = PanelMessages
	a.messagepane.SetMessages([]messages.MessageItem{{
		TS:          "1.0",
		Text:        "x",
		Attachments: []messages.Attachment{fileAtt("a.csv"), fileAtt("b.pdf")},
	}})
	if cmd := a.downloadFilesOfSelected(); cmd != nil {
		t.Fatalf("precondition: expected the modal path, got a cmd: %#v", cmd())
	}
	if !a.linkPicker.IsVisible() {
		t.Fatal("precondition: file picker did not open")
	}
	if a.pickerKind != "files" || len(a.pickerFiles) != 2 {
		t.Fatalf("precondition: pickerKind = %q with %d files, want \"files\" with 2",
			a.pickerKind, len(a.pickerFiles))
	}
}

// TestLinkPickerModeKeys characterizes handleLinkPickerMode
// (mode_linkpicker.go:14). Unlike the other eight this one forwards
// msg.String() with no normalisation at all, relying on bubbletea's
// own "esc"/"enter"/"up"/"down" spellings.
func TestLinkPickerModeKeys(t *testing.T) {
	runKeyCases(t, ModeLinkPicker, []keyCase{
		{
			name:     "enter dispatches OpenLinkMsg for the highlighted link",
			setup:    openLinkPicker,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.linkPicker.IsVisible() {
					t.Error("picker still visible after enter")
				}
				if cmd == nil {
					t.Fatal("cmd = nil, want an OpenLinkMsg cmd")
				}
				msg, ok := cmd().(OpenLinkMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want OpenLinkMsg", cmd())
				}
				if msg.URL != "https://a.example/1" {
					t.Errorf("URL = %q, want %q", msg.URL, "https://a.example/1")
				}
				// BUG?: the "links" arm never clears pickerKind, while
				// the "files" arm clears both pickerKind and
				// pickerFiles. The stale "links" is harmless today
				// because the next open always reassigns it, but the
				// asymmetry is unexplained.
				if a.pickerKind != "links" {
					t.Errorf("pickerKind = %q; the links arm is expected to leave it set", a.pickerKind)
				}
			},
		},
		{
			name: "j then enter picks the second link",
			setup: func(t *testing.T, a *App) {
				openLinkPicker(t, a)
				_ = dispatchModeKey(a, keyPress('j'))
				if got := a.linkPicker.Selected(); got != 1 {
					t.Fatalf("precondition: selected = %d, want 1", got)
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil")
				}
				msg, ok := cmd().(OpenLinkMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want OpenLinkMsg", cmd())
				}
				if msg.URL != "https://b.example/2" {
					t.Errorf("URL = %q, want %q", msg.URL, "https://b.example/2")
				}
			},
		},
		{
			name:     "enter in files mode dispatches DownloadFileMsg and clears the picker state",
			setup:    openFilePicker,
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("cmd = nil, want a DownloadFileMsg cmd")
				}
				msg, ok := cmd().(DownloadFileMsg)
				if !ok {
					t.Fatalf("cmd() = %#v, want DownloadFileMsg", cmd())
				}
				if msg.Attachment.Name != "a.csv" {
					t.Errorf("attachment = %q, want %q", msg.Attachment.Name, "a.csv")
				}
				if a.pickerFiles != nil {
					t.Errorf("pickerFiles = %v, want nil", a.pickerFiles)
				}
				if a.pickerKind != "" {
					t.Errorf("pickerKind = %q, want empty", a.pickerKind)
				}
			},
		},
		{
			// The bounds guard exists because pickerFiles and the
			// picker's item list are two separate slices kept in sync
			// only by convention. Desynchronise them and the handler
			// drops the choice instead of panicking.
			name: "enter in files mode with a stale index drops the choice",
			setup: func(t *testing.T, a *App) {
				openFilePicker(t, a)
				a.pickerFiles = a.pickerFiles[:1]
				_ = dispatchModeKey(a, keyPress('j'))
				if got := a.linkPicker.Selected(); got != 1 {
					t.Fatalf("precondition: selected = %d, want 1 (out of range for 1 file)", got)
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil for an out-of-range index", cmd())
				}
				if a.pickerFiles != nil {
					t.Errorf("pickerFiles = %v, want nil", a.pickerFiles)
				}
				if a.pickerKind != "" {
					t.Errorf("pickerKind = %q, want empty", a.pickerKind)
				}
			},
		},
		{
			name:     "esc closes the picker and clears the picker state",
			setup:    openFilePicker,
			key:      keyCode(tea.KeyEscape),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if a.linkPicker.IsVisible() {
					t.Error("picker still visible after esc")
				}
				if a.pickerFiles != nil || a.pickerKind != "" {
					t.Errorf("pickerFiles = %v, pickerKind = %q, want both cleared", a.pickerFiles, a.pickerKind)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "q closes the picker like esc",
			setup:    openLinkPicker,
			key:      keyPress('q'),
			wantMode: ModeNormal,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if a.linkPicker.IsVisible() {
					t.Error("picker still visible after q")
				}
				if a.pickerKind != "" {
					t.Errorf("pickerKind = %q, want empty", a.pickerKind)
				}
			},
		},
		{
			name:     "j moves the selection down",
			setup:    openLinkPicker,
			key:      keyPress('j'),
			wantMode: ModeLinkPicker,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if got := a.linkPicker.Selected(); got != 1 {
					t.Errorf("selected = %d, want 1", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name:     "down moves the selection like j",
			setup:    openLinkPicker,
			key:      keyCode(tea.KeyDown),
			wantMode: ModeLinkPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.linkPicker.Selected(); got != 1 {
					t.Errorf("selected = %d, want 1", got)
				}
			},
		},
		{
			name: "k moves the selection back",
			setup: func(t *testing.T, a *App) {
				openLinkPicker(t, a)
				_ = dispatchModeKey(a, keyPress('j'))
			},
			key:      keyPress('k'),
			wantMode: ModeLinkPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.linkPicker.Selected(); got != 0 {
					t.Errorf("selected = %d, want 0", got)
				}
			},
		},
		{
			name:     "up at the top clamps rather than wrapping",
			setup:    openLinkPicker,
			key:      keyCode(tea.KeyUp),
			wantMode: ModeLinkPicker,
			assert: func(t *testing.T, a *App, _ tea.Cmd) {
				if got := a.linkPicker.Selected(); got != 0 {
					t.Errorf("selected = %d, want 0", got)
				}
			},
		},
		{
			// linkpicker.Open leaves an empty picker visible, so enter
			// returns chosen=false and the handler's IsVisible guard
			// keeps the modal up. Unreachable from the `o`/`d` paths,
			// which only open the modal for 2+ items.
			name: "enter with no items keeps the modal open",
			setup: func(t *testing.T, a *App) {
				a.pickerKind = "links"
				a.linkPicker.Open("Open link", nil)
				if !a.linkPicker.IsVisible() {
					t.Fatal("precondition: empty picker should still be visible")
				}
			},
			key:      keyCode(tea.KeyEnter),
			wantMode: ModeLinkPicker,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if cmd != nil {
					t.Errorf("cmd = %#v, want nil", cmd())
				}
				if !a.linkPicker.IsVisible() {
					t.Error("picker closed on an empty enter")
				}
			},
		},
		{
			name:     "an unhandled key is swallowed",
			setup:    openLinkPicker,
			key:      keyPress('z'),
			wantMode: ModeLinkPicker,
			assert: func(t *testing.T, a *App, cmd tea.Cmd) {
				if !a.linkPicker.IsVisible() {
					t.Error("z should not close the picker")
				}
				if got := a.linkPicker.Selected(); got != 0 {
					t.Errorf("selected = %d, want 0", got)
				}
				if cmd != nil {
					t.Errorf("cmd = %T, want nil", cmd)
				}
			},
		},
		{
			name: "key with the picker closed falls through to Normal",
			setup: func(t *testing.T, a *App) {
				if a.linkPicker.IsVisible() {
					t.Fatal("precondition: picker should start hidden")
				}
			},
			key:      keyPress('x'),
			wantMode: ModeNormal,
		},
	})
}
