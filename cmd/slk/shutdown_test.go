package main

import "testing"

// recordingSaver counts SavePendingTheme calls.
type recordingSaver struct{ saves int }

func (r *recordingSaver) SavePendingTheme() { r.saves++ }

// TestShutdown_SavesPendingTheme pins the exit flush: a theme cycle
// saves only after a pause, so shutdown must save one still pending.
func TestShutdown_SavesPendingTheme(t *testing.T) {
	app := &recordingSaver{}
	shutdown(app, newWorkspaceRouter())
	if app.saves != 1 {
		t.Errorf("SavePendingTheme called %d times, want 1", app.saves)
	}
}
