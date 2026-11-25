package compose

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/channelpicker"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

func slashCommands() []core.SlashCommand {
	return []core.SlashCommand{
		{Name: "/giphy", Usage: "[text]", Desc: "Post a random gif"},
		{Name: "/shrug", Usage: "your text", Desc: "Append a shrug"},
		{Name: "/msg", Usage: "@user [message]", Desc: "Open a DM"},
		{Name: "/dm", AliasOf: "/msg", Desc: "alias"},
		{Name: "/away", Desc: "Toggle away"},
		{Name: "/star", Desc: "Star this conversation"},
	}
}

func withSlash(m Model) Model {
	m.SetCommandSource(func() []core.SlashCommand { return slashCommands() })
	m.SetWidth(80)
	m.Focus()
	return m
}

func TestSlashOpensOnlyAtStartOfEmptyComposer(t *testing.T) {
	m := withSlash(New("general"))
	m = typeText(m, "/")
	if !m.IsCommandActive() {
		t.Fatal("expected the slash picker after / in an empty composer")
	}
	if m.IsChannelActive() || m.IsMentionActive() || m.IsEmojiActive() {
		t.Fatal("slash opened another picker")
	}

	m = withSlash(New("general"))
	m = typeText(m, "see /")
	if m.IsCommandActive() {
		t.Fatal("slash after text should not open the picker")
	}

	m = withSlash(New("general"))
	m = typeText(m, "hello")
	m.MoveCursorToStart()
	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.IsCommandActive() {
		t.Fatal("inserting / at the start of existing text should not open the picker")
	}
	if m.Value() != "/hello" {
		t.Fatalf("value = %q, want /hello", m.Value())
	}
}

func TestSlashQueryFiltersAndSpaceCloses(t *testing.T) {
	m := withSlash(New("general"))
	m = typeText(m, "/gi")
	if !m.IsCommandActive() {
		t.Fatal("picker should stay open while the name is being typed")
	}
	got := namesOf(m.commandPicker.Filtered())
	if len(got) != 1 || got[0] != "/giphy" {
		t.Fatalf("filter = %v, want [/giphy]", got)
	}
	m = typeText(m, " ")
	if m.IsCommandActive() {
		t.Fatal("space should close the picker")
	}
	if m.Value() != "/gi " {
		t.Fatalf("value = %q, want the space typed and the token left as-is", m.Value())
	}
}

func TestSlashTabAndEnterComplete(t *testing.T) {
	m := withSlash(New("general"))
	m = typeText(m, "/gi")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.IsCommandActive() {
		t.Fatal("Tab should close the picker")
	}
	if m.Value() != "/giphy " {
		t.Fatalf("value = %q, want %q", m.Value(), "/giphy ")
	}

	m = withSlash(New("general"))
	m = typeText(m, "/aw")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Value() != "/away " {
		t.Fatalf("Enter value = %q, want %q", m.Value(), "/away ")
	}
	if m.IsCommandActive() {
		t.Fatal("Enter should close the picker instead of leaving it open")
	}
}

func TestSlashEscKeepsTheTypedText(t *testing.T) {
	m := withSlash(New("general"))
	m = typeText(m, "/gi")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.IsCommandActive() {
		t.Fatal("Esc should close the picker")
	}
	if m.Value() != "/gi" {
		t.Fatalf("value = %q, want the typed text kept", m.Value())
	}
}

func TestSlashBackspaceClosesWhenTriggerGoes(t *testing.T) {
	m := withSlash(New("general"))
	m = typeText(m, "/g")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !m.IsCommandActive() {
		t.Fatal("picker should stay open on /")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.IsCommandActive() {
		t.Fatal("picker should close once backspace removes /")
	}
}

func TestSlashEmptySourceDoesNotOpen(t *testing.T) {
	m := New("general")
	m.SetCommandSource(func() []core.SlashCommand { return nil })
	m.SetWidth(80)
	m.Focus()
	m = typeText(m, "/")
	if m.IsCommandActive() {
		t.Fatal("an empty source should not open the picker")
	}
	if m.Value() != "/" {
		t.Fatalf("value = %q, want /", m.Value())
	}
}

func TestSlashNoSourceDoesNotOpen(t *testing.T) {
	m := New("general")
	m.SetWidth(80)
	m.Focus()
	m = typeText(m, "/away")
	if m.IsCommandActive() {
		t.Fatal("a composer with no command source should not open the picker")
	}
}

func TestSlashPrecedenceWithOtherPickers(t *testing.T) {
	m := withSlash(New("general"))
	m.SetChannels([]channelpicker.Channel{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice"}})

	m = typeText(m, "#gen")
	if !m.IsChannelActive() || m.IsCommandActive() {
		t.Fatalf("channel=%v command=%v, want the channel picker", m.IsChannelActive(), m.IsCommandActive())
	}

	m = withSlash(New("general"))
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice"}})
	m = typeText(m, "/")
	if !m.IsCommandActive() {
		t.Fatal("setup: slash picker should be open")
	}
	m = typeText(m, "@")
	if m.IsMentionActive() {
		t.Fatal("@ inside a slash token should not open the mention picker")
	}
	if !m.IsCommandActive() {
		t.Fatal("the slash picker should stay open")
	}

	m = typeText(m, " ")
	// "/@" plus the space. Picker closed. A later # opens channels.
	m.SetChannels([]channelpicker.Channel{{ID: "C1", Name: "general", Type: "channel"}})
	m = typeText(m, "#")
	if !m.IsChannelActive() {
		t.Fatal("after the slash picker closes, # should open the channel picker")
	}
	if m.IsCommandActive() {
		t.Fatal("the slash picker should stay closed")
	}
}

func TestSlashResetClearsPicker(t *testing.T) {
	m := withSlash(New("general"))
	m = typeText(m, "/")
	if !m.IsCommandActive() {
		t.Fatal("setup: picker should be active")
	}
	m.Reset()
	if m.IsCommandActive() {
		t.Fatal("Reset should close the slash picker")
	}
}

func TestPickerActiveAndPopupViewFollowTheOpenPicker(t *testing.T) {
	m := withSlash(New("general"))
	m.SetChannels([]channelpicker.Channel{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice", InChannel: true}})
	if m.PickerActive() || m.PopupView(40) != "" {
		t.Fatal("a fresh composer has a popup")
	}

	m = typeText(m, "#")
	if !m.PickerActive() {
		t.Fatal("channel picker should count as active")
	}
	if m.PopupView(40) != m.ChannelPickerView(40) || m.PopupView(40) == "" {
		t.Fatal("PopupView should be the channel picker")
	}

	m.Reset()
	m = withSlash(m)
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice", InChannel: true}})
	m = typeText(m, "@")
	if m.PopupView(40) != m.MentionPickerView(40) || !m.PickerActive() {
		t.Fatal("PopupView should be the mention picker")
	}

	m.Reset()
	m = withSlash(m)
	m = typeText(m, "/")
	if !m.IsCommandActive() || !m.PickerActive() {
		t.Fatal("slash picker should be active")
	}
	view := m.PopupView(40)
	if view == "" || view != m.CommandPickerView(40) {
		t.Fatal("PopupView should be the slash picker")
	}
	if !strings.Contains(stripANSI(view), "/giphy") {
		t.Fatalf("popup = %q, want it to list /giphy", stripANSI(view))
	}

	m = typeText(m, ":sm")
	// The colon is part of the slash query, not an emoji trigger.
	if m.IsEmojiActive() {
		t.Fatal("emoji picker opened over the slash picker")
	}
}

func namesOf(cmds []core.SlashCommand) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		j := i + 1
		if j < len(s) && s[j] == '[' {
			j++
			for j < len(s) && s[j] != 'm' {
				j++
			}
		}
		i = j
	}
	return b.String()
}
