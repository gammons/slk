package statusbar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestSetCommandLine_RendersPromptInView(t *testing.T) {
	m := New()
	m.SetCommandLine(":vsp")
	out := m.View(80)
	if !strings.Contains(out, ":vsp") {
		t.Fatalf("expected view to contain %q, got:\n%s", ":vsp", out)
	}
	if !strings.Contains(out, "▌") {
		t.Fatalf("expected view to contain block-cursor glyph %q, got:\n%s", "▌", out)
	}
}

func TestSetCommandLine_HidesChannelSegmentWhileActive(t *testing.T) {
	m := New()
	m.SetChannel("general")
	m.SetCommandLine(":ws")
	if out := m.View(80); strings.Contains(out, "general") {
		t.Fatalf("channel segment should be hidden while command line active:\n%s", out)
	}
	m.SetCommandLine("")
	if out := m.View(80); !strings.Contains(out, "general") {
		t.Fatalf("channel segment should return after clearing command line:\n%s", out)
	}
}

func TestSetCommandLine_BumpsVersion(t *testing.T) {
	m := New()
	v0 := m.Version()
	m.SetCommandLine(":q")
	if m.Version() == v0 {
		t.Fatal("SetCommandLine should bump version")
	}
	v1 := m.Version()
	m.SetCommandLine(":q") // no change → no bump
	if m.Version() != v1 {
		t.Fatal("identical SetCommandLine should not bump version")
	}
}

// TestSetCommandCandidates_RendersAfterPrompt pins the wildmenu layout:
// the list follows the prompt, in list order, with two spaces between
// names (the same gap the bar puts between its other segments).
func TestSetCommandCandidates_RendersAfterPrompt(t *testing.T) {
	m := New()
	m.SetCommandLine(":o")
	m.SetCommandCandidates([]string{"on", "only"}, 0)

	plain := ansi.Strip(m.View(80))
	i := strings.Index(plain, ":o▌")
	if i < 0 {
		t.Fatalf("prompt missing:\n%s", plain)
	}
	rest := plain[i:]
	if j := strings.Index(rest, "on"); j < 0 {
		t.Fatalf("first candidate missing after the prompt:\n%s", plain)
	}
	if want := "on  only"; !strings.Contains(rest, want) {
		t.Fatalf("candidates not rendered in order with the two-space gap (%q):\n%s", want, plain)
	}
}

// TestSetCommandCandidates_CurrentCarriesSelectionStyle proves the name the
// prompt is sitting on is the highlighted one. It compares against the same
// style the sidebar and message panes use for their selected row rather than
// a hand-picked escape sequence, so a theme change cannot vacate the check.
func TestSetCommandCandidates_CurrentCarriesSelectionStyle(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New()
	m.SetCommandLine(":o")
	m.SetCommandCandidates([]string{"on", "only"}, 1)

	out := m.View(80)
	if !strings.Contains(out, styles.SelectionStyle().Render("only")) {
		t.Fatalf("current candidate not highlighted with the selection style:\n%q", out)
	}
	if strings.Contains(out, styles.SelectionStyle().Render("on")) {
		t.Fatalf("non-current candidate wrongly highlighted:\n%q", out)
	}
}

// TestSetCommandLine_ClearsCandidates pins that the completion list cannot
// outlive the prompt it was matched against: clearing the line is enough.
// The prompt is reopened before checking, because View hides the list
// anyway while the line is empty.
func TestSetCommandLine_ClearsCandidates(t *testing.T) {
	m := New()
	m.SetCommandLine(":o")
	m.SetCommandCandidates([]string{"on", "only"}, 0)

	m.SetCommandLine("")
	m.SetCommandLine(":ws")
	if out := ansi.Strip(m.View(80)); strings.Contains(out, "only") {
		t.Fatalf("candidates survived SetCommandLine(\"\") into the next prompt:\n%s", out)
	}
}

// TestSetCommandCandidates_BumpsVersionOnChangeOnly keeps the render
// caches honest: a repeat of the same list and cursor must not invalidate
// anything, a real change must.
func TestSetCommandCandidates_BumpsVersionOnChangeOnly(t *testing.T) {
	m := New()
	m.SetCommandLine(":o")
	v0 := m.Version()
	m.SetCommandCandidates([]string{"on", "only"}, 0)
	if m.Version() == v0 {
		t.Fatal("setting candidates should bump the version")
	}
	v1 := m.Version()
	m.SetCommandCandidates([]string{"on", "only"}, 0) // same list, same cursor
	if m.Version() != v1 {
		t.Fatal("an identical SetCommandCandidates should not bump version")
	}
	m.SetCommandCandidates([]string{"on", "only"}, 1) // cursor moved
	if m.Version() == v1 {
		t.Fatal("moving the cursor should bump the version")
	}
}

// TestSetCommandCandidates_CopiesTheSlice pins that the model owns its list:
// the caller must be able to reuse its backing array afterwards.
func TestSetCommandCandidates_CopiesTheSlice(t *testing.T) {
	m := New()
	m.SetCommandLine(":o")
	names := []string{"on", "only"}
	m.SetCommandCandidates(names, 0)

	names[0] = "mutated"
	if out := ansi.Strip(m.View(80)); !strings.Contains(out, " on ") {
		t.Fatalf("caller-side mutation leaked into the bar:\n%s", out)
	}
}

// TestSetCommandCandidates_NeverWidensTheBar pins the wildmenu's budget at
// every width: the candidates may only use the room the prompt leaves before
// the right-side pills, so a long list scrolls instead of pushing the bar any
// wider than it is without them (even on a terminal too narrow for the
// prompt's fixed segments, where the list simply has no room).
func TestSetCommandCandidates_NeverWidensTheBar(t *testing.T) {
	candidates := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	// Every width and every cursor position: the boundary where the current
	// name fits but the "< " marker in front of it does not sits at a
	// different width for each name.
	for width := 20; width <= 120; width++ {
		// The same bar without candidates: the ceiling the list must respect.
		plainBar := New()
		plainBar.SetCommandLine(":d")
		want := lipgloss.Width(plainBar.View(width))

		for idx := range candidates {
			m := New()
			m.SetCommandLine(":d")
			m.SetCommandCandidates(candidates, idx)

			out := m.View(width)
			if got := lipgloss.Width(out); got != want {
				t.Errorf("View(%d) with candidate %d current is %d cells wide, want the %d it is without them:\n%q",
					width, idx, got, want, out)
			}
		}
	}
}

// TestSetCommandCandidates_CurrentNameStaysVisible checks the scroll anchor:
// as names are dropped from the left to fit, the one the prompt is sitting on
// stays on screen — it is the name the user is cycling to.
func TestSetCommandCandidates_CurrentNameStaysVisible(t *testing.T) {
	candidates := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	for _, width := range []int{45, 60, 120} {
		m := New()
		m.SetCommandLine(":d")
		m.SetCommandCandidates(candidates, 3)

		stripped := ansi.Strip(m.View(width))
		if !strings.Contains(stripped, "delta") {
			t.Errorf("View(%d): current candidate missing from the scrolled list:\n%s", width, stripped)
		}
	}
}

// TestSetCommandCandidates_MarksHiddenNames pins the "< " prefix: it appears
// exactly when names to the left of the window were dropped, and never when
// the whole list fits.
func TestSetCommandCandidates_MarksHiddenNames(t *testing.T) {
	candidates := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	wide := New()
	wide.SetCommandLine(":d")
	wide.SetCommandCandidates(candidates, 0)
	if stripped := ansi.Strip(wide.View(120)); strings.Contains(stripped, "<") {
		t.Fatalf("no names were dropped, yet the hidden-name marker is drawn:\n%s", stripped)
	}

	narrow := New()
	narrow.SetCommandLine(":d")
	narrow.SetCommandCandidates(candidates, 3)
	stripped := ansi.Strip(narrow.View(45))
	if !strings.Contains(stripped, "< ") {
		t.Fatalf("dropped names are not marked with a %q prefix:\n%s", "< ", stripped)
	}
	if strings.Contains(stripped, "alpha") {
		t.Fatalf("a name the window dropped is still drawn:\n%s", stripped)
	}
	if !strings.Contains(stripped, "delta") {
		t.Fatalf("current candidate missing from the scrolled list:\n%s", stripped)
	}
}

// TestSetCommandCandidates_MarksNamesCutOffOnTheRight pins the " >" suffix:
// it appears exactly when names past the right edge of the window were
// dropped, as in vim's wildmenu, and never when the list reaches its end.
func TestSetCommandCandidates_MarksNamesCutOffOnTheRight(t *testing.T) {
	candidates := []string{"alpha", "beta", "gamma", "delta", "epsilon"}

	wide := New()
	wide.SetCommandLine(":d")
	wide.SetCommandCandidates(candidates, 0)
	if stripped := ansi.Strip(wide.View(120)); strings.Contains(stripped, ">") {
		t.Fatalf("every name fits, yet the cut-off marker is drawn:\n%s", stripped)
	}

	narrow := New()
	narrow.SetCommandLine(":d")
	narrow.SetCommandCandidates(candidates, 0)
	stripped := ansi.Strip(narrow.View(50))
	if !strings.Contains(stripped, "alpha") || strings.Contains(stripped, "epsilon") {
		t.Fatalf("want the window to start at the current name and drop the last one:\n%s", stripped)
	}
	if !strings.Contains(stripped, " >") {
		t.Fatalf("names cut off on the right are not marked with %q:\n%s", " >", stripped)
	}
}

// TestSetCommandCandidates_GapBeforePills checks that a list filling its
// room still leaves a blank column before the right-side pills, at every
// width and with every name current.
func TestSetCommandCandidates_GapBeforePills(t *testing.T) {
	candidates := []string{"activity", "delmarks", "help", "huddle", "marks", "on", "only", "q", "sp", "version", "vsp", "ws"}
	for width := 40; width <= 120; width++ {
		for idx := range candidates {
			m := New()
			m.SetCommandLine(":")
			m.SetCommandCandidates(candidates, idx)
			stripped := ansi.Strip(m.View(width))
			i := strings.Index(stripped, "●")
			if i <= 0 {
				t.Fatalf("View(%d): connection pill missing:\n%s", width, stripped)
			}
			if stripped[i-1] != ' ' {
				t.Errorf("View(%d) with candidate %d current: the list runs into the pills:\n%s", width, idx, stripped)
			}
		}
	}
}

// TestSetCommandCandidates_CustomStatusTakesWhatTheListLeaves checks the
// custom-status segment against the completion list: the list is sized
// first, so a long status can't hide it, the status gets only what the
// list leaves (keeping the gap before the pills), and the bar never
// widens past width.
func TestSetCommandCandidates_CustomStatusTakesWhatTheListLeaves(t *testing.T) {
	candidates := []string{"activity", "delmarks", "help", "huddle", "marks", "on", "only", "q", "sp", "version", "vsp", "ws"}
	for width := 40; width <= 160; width++ {
		for idx := range candidates {
			m := New()
			m.SetConnectionState(StateConnected)
			m.SetCustomStatus(strings.Repeat("a very long status indeed ", 5), "🏠")
			m.SetCommandLine(":")
			m.SetCommandCandidates(candidates, idx)

			out := m.View(width)
			if got := lipgloss.Width(out); got != width {
				t.Fatalf("View(%d) with candidate %d current is %d cells wide:\n%q", width, idx, got, out)
			}
			stripped := ansi.Strip(out)
			// The same bar without the status: where it shows the current
			// name, the status must not take that name's room.
			bare := New()
			bare.SetConnectionState(StateConnected)
			bare.SetCommandLine(":")
			bare.SetCommandCandidates(candidates, idx)
			shown := strings.Contains(ansi.Strip(bare.View(width)), candidates[idx])
			if shown && !strings.Contains(stripped, candidates[idx]) {
				t.Errorf("View(%d): the status hid the current candidate %q:\n%s", width, candidates[idx], stripped)
			}
			if i := strings.Index(stripped, "🏠"); i > 0 && stripped[i-1] != ' ' {
				t.Errorf("View(%d) with candidate %d current: the list runs into the status:\n%s", width, idx, stripped)
			}
		}
	}
}
