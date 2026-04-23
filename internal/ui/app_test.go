// internal/ui/app_test.go
package ui

import (
	"testing"
)

func TestAppFocusCycle(t *testing.T) {
	app := NewApp()

	if app.focusedPanel != PanelSidebar {
		t.Errorf("expected initial focus on sidebar, got %d", app.focusedPanel)
	}

	app.FocusNext()
	if app.focusedPanel != PanelMessages {
		t.Errorf("expected focus on messages, got %d", app.focusedPanel)
	}

	app.FocusNext()
	if app.focusedPanel != PanelSidebar {
		t.Errorf("expected focus to wrap to sidebar, got %d", app.focusedPanel)
	}

	app.FocusPrev()
	if app.focusedPanel != PanelMessages {
		t.Errorf("expected focus on messages after prev, got %d", app.focusedPanel)
	}
}

func TestAppToggleSidebar(t *testing.T) {
	app := NewApp()

	if !app.sidebarVisible {
		t.Error("expected sidebar visible initially")
	}

	app.ToggleSidebar()
	if app.sidebarVisible {
		t.Error("expected sidebar hidden after toggle")
	}

	// When sidebar is hidden and focus was on sidebar, focus should move to messages
	app2 := NewApp()
	app2.focusedPanel = PanelSidebar
	app2.ToggleSidebar()
	if app2.focusedPanel != PanelMessages {
		t.Errorf("expected focus to move to messages when sidebar hidden, got %d", app2.focusedPanel)
	}

	app.ToggleSidebar()
	if !app.sidebarVisible {
		t.Error("expected sidebar visible after second toggle")
	}
}

func TestAppModeTransitions(t *testing.T) {
	app := NewApp()

	if app.mode != ModeNormal {
		t.Error("expected normal mode initially")
	}

	app.SetMode(ModeInsert)
	if app.mode != ModeInsert {
		t.Error("expected insert mode")
	}

	app.SetMode(ModeNormal)
	if app.mode != ModeNormal {
		t.Error("expected normal mode after escape")
	}
}
