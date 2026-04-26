// internal/ui/app_test.go
package ui

import (
	"testing"
	"time"
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

func TestTypingStateAddAndExpire(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"

	// Simulate receiving a typing event
	app.addTypingUser("C1", "U1")

	users := app.getTypingUsers("C1")
	if len(users) != 1 || users[0] != "U1" {
		t.Errorf("expected [U1], got %v", users)
	}

	// Add another user
	app.addTypingUser("C1", "U2")
	users = app.getTypingUsers("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users, got %d", len(users))
	}

	// Expire all
	app.expireTypingUsers()
	// They shouldn't be expired yet (TTL is 5 seconds)
	users = app.getTypingUsers("C1")
	if len(users) != 2 {
		t.Errorf("expected 2 users still active, got %d", len(users))
	}
}

func TestTypingStateFiltersSelf(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.currentUserID = "U1"

	app.addTypingUser("C1", "U1")
	app.addTypingUser("C1", "U2")

	users := app.getTypingUsersFiltered("C1")
	if len(users) != 1 || users[0] != "U2" {
		t.Errorf("expected [U2] (self filtered), got %v", users)
	}
}

func TestTypingIndicatorText(t *testing.T) {
	app := NewApp()

	text := app.typingIndicatorText(nil)
	if text != "" {
		t.Errorf("expected empty for nil, got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice"})
	if text != "Alice is typing..." {
		t.Errorf("expected 'Alice is typing...', got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice", "Bob"})
	if text != "Alice and Bob are typing..." {
		t.Errorf("expected 'Alice and Bob are typing...', got %q", text)
	}

	text = app.typingIndicatorText([]string{"Alice", "Bob", "Charlie"})
	if text != "Several people are typing..." {
		t.Errorf("expected 'Several people are typing...', got %q", text)
	}
}

func TestRenderTypingIndicator(t *testing.T) {
	app := NewApp()
	app.activeChannelID = "C1"
	app.typingEnabled = true
	app.currentUserID = "U_SELF"

	// Set up user names
	app.messagepane.SetUserNames(map[string]string{"U1": "Alice", "U2": "Bob"})

	// No one typing — should return empty
	line := app.renderTypingLine()
	if line != "" {
		t.Errorf("expected empty, got %q", line)
	}

	// One person typing
	app.addTypingUser("C1", "U1")
	line = app.renderTypingLine()
	if line == "" {
		t.Error("expected typing indicator, got empty")
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

func TestTypingClearedOnChannelSwitch(t *testing.T) {
	app := NewApp()
	app.typingEnabled = true
	app.activeChannelID = "C1"

	app.addTypingUser("C1", "U1")
	app.addTypingUser("C2", "U2")

	// Typing indicator should show for C1
	users := app.getTypingUsersFiltered("C1")
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C1, got %d", len(users))
	}

	// After "switching" to C2, reset throttle
	app.activeChannelID = "C2"
	app.lastTypingSent = time.Time{} // reset throttle on switch

	// C2 should show its typers
	users = app.getTypingUsersFiltered("C2")
	if len(users) != 1 {
		t.Errorf("expected 1 user typing in C2, got %d", len(users))
	}
}

func TestTypingThrottle(t *testing.T) {
	app := NewApp()
	app.typingEnabled = true
	app.activeChannelID = "C1"

	// First call should allow sending
	if !app.shouldSendTyping() {
		t.Error("expected first typing send to be allowed")
	}

	// Mark as just sent
	app.lastTypingSent = time.Now()

	// Immediate second call should be throttled
	if app.shouldSendTyping() {
		t.Error("expected typing send to be throttled")
	}

	// After 3 seconds, should allow again
	app.lastTypingSent = time.Now().Add(-4 * time.Second)
	if !app.shouldSendTyping() {
		t.Error("expected typing send to be allowed after 3s")
	}
}
