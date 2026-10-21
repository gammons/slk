package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/help"
)

// TestHelpCommandOpensOverlay drives :help through the prompt, as a
// user types it, and expects the same overlay ? opens.
func TestHelpCommandOpensOverlay(t *testing.T) {
	a := NewApp()
	a.enterCommandMode()
	typeCommand(a, "help")
	_ = handleCommandMode(a, tea.KeyPressMsg{Code: tea.KeyEnter})

	if a.mode != ModeHelp {
		t.Fatalf("mode = %v, want ModeHelp", a.mode)
	}
	if !a.help.IsVisible() {
		t.Fatal("help overlay not visible after :help")
	}
	if n := len(a.help.VisibleEntries()); n == 0 {
		t.Fatal("help overlay opened with no entries")
	}
}

// TestHelpListsEveryCommand pins that the overlay names every ":"
// command, so a command added to the registry without a help entry
// fails here instead of staying undiscoverable. Aliases are exempt.
func TestHelpListsEveryCommand(t *testing.T) {
	aliases := map[string]bool{"on": true} // :only
	var keys []string
	for _, e := range help.FromKeyMap(DefaultKeyMap()) {
		keys = append(keys, e.Key)
	}
	listed := " " + strings.Join(keys, " ") + " "
	for name := range commands {
		if aliases[name] {
			continue
		}
		if !strings.Contains(listed, " :"+name+" ") {
			t.Errorf(":%s has no entry in the help overlay", name)
		}
	}
}
