package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestVersionCommandShowsBuildInfo drives :version through the prompt
// and expects the line main set to land in the status-bar toast.
func TestVersionCommandShowsBuildInfo(t *testing.T) {
	a := NewApp()
	a.SetBuildInfo("v1.2.3 · abc1234")

	a.enterCommandMode()
	typeCommand(a, "version")
	cmd := handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected the toast-clear cmd")
	}
	if out := a.statusbar.View(160); !strings.Contains(out, "v1.2.3 · abc1234") {
		t.Errorf("status bar should show the build info, got:\n%s", out)
	}
}

// TestVersionCommandWithoutBuildInfo covers an App main never
// configured: the toast says so instead of showing an empty line.
func TestVersionCommandWithoutBuildInfo(t *testing.T) {
	a := NewApp()

	_ = executeCommand(a, "version")
	if out := a.statusbar.View(120); !strings.Contains(out, "slk (unknown build)") {
		t.Errorf("status bar should show the unknown-build fallback, got:\n%s", out)
	}
}
