package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestNewApp_StartsFocused(t *testing.T) {
	// Terminals report focus transitions only, never current state. A
	// terminal without focus-event support sends nothing at all, so
	// defaulting to focused is what preserves today's behavior there.
	if !NewApp().terminalFocused {
		t.Error("terminalFocused must default to true")
	}
}

func TestBlurMsg_ClearsFocus(t *testing.T) {
	app := NewApp()
	_, _ = app.Update(tea.BlurMsg{})
	if app.terminalFocused {
		t.Error("terminalFocused must be false after BlurMsg")
	}
}

func TestFocusMsg_RestoresFocus(t *testing.T) {
	app := NewApp()
	_, _ = app.Update(tea.BlurMsg{})
	_, _ = app.Update(tea.FocusMsg{})
	if !app.terminalFocused {
		t.Error("terminalFocused must be true after FocusMsg")
	}
}

func TestView_EnablesFocusReporting(t *testing.T) {
	app := NewApp()
	app.width, app.height = 80, 24
	if !app.View().ReportFocus {
		t.Error("View must set ReportFocus so the terminal sends focus events")
	}
}
