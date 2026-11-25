package commandpicker

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
)

func commands() []core.SlashCommand {
	return []core.SlashCommand{
		{Name: "/giphy", Usage: "[text]", Desc: "Post a random gif"},
		{Name: "/shrug", Usage: "your text", Desc: "Append a shrug"},
		{Name: "/msg", Usage: "@user [message]", Desc: "Open a DM"},
		{Name: "/dm", AliasOf: "/msg", Desc: "alias of msg"},
		{Name: "/linear", Usage: "issue title", Desc: "Create an issue", AppName: "Linear"},
		{Name: "/café", Desc: "An accented command"},
		{Name: "/status", Desc: "Set your presence"},
		{Name: "/star", Desc: "Star this conversation"},
	}
}

func opened(t *testing.T, src []core.SlashCommand) Model {
	t.Helper()
	m := New(func() []core.SlashCommand { return src }, WithWidth(60), WithStyles(Styles{
		Chrome: chrome.Styles{Popup: lipgloss.NewStyle().Border(lipgloss.RoundedBorder())},
		Name:   lipgloss.NewStyle(),
		Detail: lipgloss.NewStyle(),
	}))
	if !m.Open() {
		t.Fatal("Open returned false")
	}
	return m
}

func names(cmds []core.SlashCommand) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

func keyCode(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c} }

func TestOpenHidesAliases(t *testing.T) {
	m := opened(t, commands())
	got := names(m.Filtered())
	for _, n := range got {
		if n == "/dm" {
			t.Fatalf("alias /dm is visible: %v", got)
		}
	}
	if len(got) != 7 {
		t.Fatalf("visible = %v, want 7 (the alias dropped)", got)
	}
}

func TestEmptySourceStaysClosed(t *testing.T) {
	m := New(func() []core.SlashCommand { return nil })
	if m.Open() {
		t.Fatal("Open returned true for an empty source")
	}
	if m.IsVisible() {
		t.Fatal("picker is visible")
	}
	if m.View() != "" {
		t.Fatal("View of a closed picker is not empty")
	}
}

func TestAliasOnlySourceStaysClosed(t *testing.T) {
	m := New(func() []core.SlashCommand {
		return []core.SlashCommand{{Name: "/dm", AliasOf: "/msg"}}
	})
	if m.Open() || m.IsVisible() {
		t.Fatal("a list of only aliases should stay closed")
	}
}

func TestFilterIsFoldedPrefix(t *testing.T) {
	m := opened(t, commands())
	m.SetQuery("GI")
	got := names(m.Filtered())
	if len(got) != 1 || got[0] != "/giphy" {
		t.Fatalf("query GI = %v, want [/giphy]", got)
	}
	m.SetQuery("cafe")
	got = names(m.Filtered())
	if len(got) != 1 || got[0] != "/café" {
		t.Fatalf("query cafe = %v, want [/café]", got)
	}
	m.SetQuery("zzz")
	if len(m.Filtered()) != 0 {
		t.Fatalf("query zzz = %v, want none", names(m.Filtered()))
	}
	if m.View() != "" {
		t.Fatal("a query with no matches should not draw")
	}
}

func TestTabAndEnterComplete(t *testing.T) {
	m := opened(t, commands())
	m.SetQuery("g")
	res := m.HandleKey(keyCode(tea.KeyTab))
	if !res.Handled || !res.Completed || res.Name != "/giphy" {
		t.Fatalf("Tab = %+v, want completed /giphy", res)
	}
	if m.IsVisible() {
		t.Fatal("picker still visible after Tab")
	}

	m = opened(t, commands())
	m.SetQuery("sh")
	down := m.HandleKey(keyCode(tea.KeyDown))
	if !down.Handled || down.Completed {
		t.Fatalf("Down = %+v, want handled and not completed", down)
	}
	res = m.HandleKey(keyCode(tea.KeyEnter))
	if !res.Completed || res.Name != "/shrug" {
		t.Fatalf("Enter after Down = %+v, want /shrug", res)
	}
}

func TestEscClosesWithoutCompleting(t *testing.T) {
	m := opened(t, commands())
	res := m.HandleKey(keyCode(tea.KeyEscape))
	if !res.Handled || res.Completed || res.Name != "" {
		t.Fatalf("Esc = %+v, want handled, not completed", res)
	}
	if m.IsVisible() {
		t.Fatal("picker still visible after Esc")
	}
}

func TestCtrlNAndCtrlPMove(t *testing.T) {
	m := opened(t, commands())
	down := m.HandleKey(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl | tea.ModNumLock})
	if !down.Handled || down.Completed || m.Selected() != 1 {
		t.Fatalf("ctrl+n = %+v selected %d, want handled at row 1", down, m.Selected())
	}
	up := m.HandleKey(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModNumLock})
	if !up.Handled || m.Selected() != 0 {
		t.Fatalf("ctrl+p = %+v selected %d, want handled at row 0", up, m.Selected())
	}
}

func TestKeyMapIsHonored(t *testing.T) {
	m := opened(t, commands())
	m.SetKeyMap(KeyMap{
		Select: key.NewBinding(key.WithKeys("x")),
		Close:  key.NewBinding(key.WithKeys("esc")),
		Up:     key.NewBinding(key.WithKeys("up")),
		Down:   key.NewBinding(key.WithKeys("down")),
	})
	if res := m.HandleKey(keyCode(tea.KeyTab)); res.Handled {
		t.Fatal("Tab completed after the keymap stopped binding it")
	}
	res := m.HandleKey(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !res.Completed || res.Name != "/giphy" {
		t.Fatalf("x = %+v, want it to complete /giphy", res)
	}
}

func TestRowShowsUsageAndDescriptionTruncated(t *testing.T) {
	long := strings.Repeat("description ", 20)
	m := New(func() []core.SlashCommand {
		return []core.SlashCommand{{Name: "/giphy", Usage: "[text]", Desc: long}}
	}, WithWidth(24), WithStyles(Styles{
		Chrome: chrome.Styles{Popup: lipgloss.NewStyle().Border(lipgloss.RoundedBorder())},
	}))
	if !m.Open() {
		t.Fatal("Open returned false")
	}
	view := m.View()
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "/giphy") || !strings.Contains(plain, "[text]") || !strings.Contains(plain, "—") {
		t.Fatalf("row = %q, want /giphy, usage, and a description dash", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Fatalf("row = %q, want it truncated with an ellipsis", plain)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 24 {
			t.Errorf("line is %d cells wide, wider than 24: %q", w, ansi.Strip(line))
		}
	}
}

func TestViewEmptyWhenClosed(t *testing.T) {
	m := New(func() []core.SlashCommand {
		return []core.SlashCommand{{Name: "/giphy", Desc: "gif"}}
	}, WithWidth(40))
	if m.View() != "" {
		t.Fatal("a closed picker rendered something")
	}
}
