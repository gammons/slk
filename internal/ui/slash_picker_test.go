package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
)

func TestBuiltinSlashCommandsCoverTheRegistry(t *testing.T) {
	got := map[string]core.SlashCommand{}
	for _, c := range builtinSlashCommands() {
		got[c.Name] = c
		if c.Desc == "" {
			t.Errorf("%s has no description", c.Name)
		}
	}
	for name := range slashCommands {
		c, ok := got[name]
		if !ok {
			t.Errorf("slashCommands %s is missing from the picker catalog", name)
			continue
		}
		if c.Name != name {
			t.Errorf("catalog name = %q, want %q", c.Name, name)
		}
	}
}

func TestSlashCommandsForPickerMergesWorkspaceAndBuiltins(t *testing.T) {
	a := newTestApp(t)
	if got := a.slashCommandsForPicker(); got != nil {
		t.Fatalf("empty workspace list = %v, want nil so the picker stays closed", slashNames(got))
	}
	a.SetCommandService(core.NewCommandService(core.CommandServiceFuncs{
		List: func() []core.SlashCommand {
			return []core.SlashCommand{
				{Name: "/giphy", Usage: "[text]", Desc: "Post a gif"},
				{Name: "/away", Desc: "Slack's away description"},
				{Name: "/dm", AliasOf: "/msg"},
			}
		},
	}))
	got := a.slashCommandsForPicker()
	if n := slashCount(got, "/away"); n != 1 {
		t.Fatalf("/away appears %d times, want 1", n)
	}
	if desc := slashDesc(got, "/away"); desc != "Slack's away description" {
		t.Fatalf("/away desc = %q, want the workspace's", desc)
	}
	if !slashNamed(got, "/giphy") || !slashNamed(got, "/star") {
		t.Fatalf("merged = %v, want /giphy and the slk-only /star", slashNames(got))
	}
}

func TestSlashPickerEnterCompletesInsteadOfRunning(t *testing.T) {
	var ran []string
	a := newTestApp(t, append(insertOpts(), withMode(ModeInsert))...)
	a.SetCommandService(core.NewCommandService(core.CommandServiceFuncs{
		Run: func(_ string, _ ids.ChannelID, _ ids.ThreadTS, command, _ string) error {
			ran = append(ran, command)
			return nil
		},
		List: func() []core.SlashCommand {
			return []core.SlashCommand{{Name: "/giphy", Usage: "[text]", Desc: "Post a gif"}}
		},
	}))
	_ = a.compose.Focus()
	typeInsert(a, "/gi")
	if !a.compose.IsCommandActive() {
		t.Fatal("typing /gi should open the picker")
	}
	a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(ran) != 0 {
		t.Fatalf("Enter ran %v while the picker was open", ran)
	}
	if got := a.compose.Value(); got != "/giphy " {
		t.Fatalf("value = %q, want %q", got, "/giphy ")
	}
	if a.mode != ModeInsert {
		t.Fatalf("mode = %v, want insert", a.mode)
	}
	if a.compose.IsCommandActive() {
		t.Fatal("picker still open after Enter")
	}

	cmd := a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := deliveredMsgs[commandRanMsg](t, cmd)
	if len(ran) != 1 || ran[0] != "/giphy" {
		t.Fatalf("second Enter ran %v, want [/giphy]", ran)
	}
	if len(msgs) != 1 || msgs[0].name != "/giphy" || msgs[0].err != nil {
		t.Fatalf("commandRanMsg = %+v, want /giphy", msgs)
	}
}

func TestSlashPickerStaysClosedWhenTheWorkspaceListIsEmpty(t *testing.T) {
	a := newTestApp(t, append(insertOpts(), withMode(ModeInsert))...)
	_ = a.compose.Focus()
	typeInsert(a, "/away")
	if a.compose.IsCommandActive() {
		t.Fatal("an empty command list should not open the picker")
	}
	if got := a.compose.Value(); got != "/away" {
		t.Fatalf("value = %q, want /away", got)
	}
}

func TestSlashPickerEscStaysInInsert(t *testing.T) {
	a := newTestApp(t, append(insertOpts(), withMode(ModeInsert))...)
	a.SetCommandService(core.NewCommandService(core.CommandServiceFuncs{
		List: func() []core.SlashCommand {
			return []core.SlashCommand{{Name: "/away", Desc: "Toggle away"}}
		},
	}))
	_ = a.compose.Focus()
	typeInsert(a, "/aw")
	if !a.compose.IsCommandActive() {
		t.Fatal("a loaded list should open the picker")
	}
	a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.compose.IsCommandActive() {
		t.Fatal("Esc left the picker open")
	}
	if a.mode != ModeInsert {
		t.Fatalf("mode = %v, want insert", a.mode)
	}
	if got := a.compose.Value(); got != "/aw" {
		t.Fatalf("value = %q, want /aw kept", got)
	}
}

func TestSlashPickerCompletesInTheThreadComposer(t *testing.T) {
	a := newTestApp(t, append(insertOpts(), withMode(ModeInsert))...)
	a.SetCommandService(core.NewCommandService(core.CommandServiceFuncs{
		List: func() []core.SlashCommand {
			return []core.SlashCommand{{Name: "/giphy", Usage: "[text]", Desc: "Post a gif"}}
		},
	}))
	showThread(t, a)
	a.SetMode(ModeInsert)
	_ = a.threadCompose.Focus()
	for _, r := range "/gi" {
		a.handleInsertMode(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !a.threadCompose.IsCommandActive() {
		t.Fatal("the thread composer should open the slash picker")
	}
	if a.compose.IsCommandActive() {
		t.Fatal("the channel composer opened a picker too")
	}
	a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := a.threadCompose.Value(); got != "/giphy " {
		t.Fatalf("thread value = %q, want /giphy completed", got)
	}
	if a.compose.Value() != "" {
		t.Fatalf("channel composer = %q, want it untouched", a.compose.Value())
	}
}

func typeInsert(a *App, s string) {
	for _, r := range s {
		a.handleInsertMode(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func slashNamed(cmds []core.SlashCommand, name string) bool {
	return slashCount(cmds, name) > 0
}

func slashCount(cmds []core.SlashCommand, name string) int {
	n := 0
	for _, c := range cmds {
		if c.Name == name {
			n++
		}
	}
	return n
}

func slashDesc(cmds []core.SlashCommand, name string) string {
	for _, c := range cmds {
		if c.Name == name {
			return c.Desc
		}
	}
	return ""
}

func slashNames(cmds []core.SlashCommand) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
