package compose

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/ui/channelpicker"
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

func chans() []channelpicker.Channel {
	return []channelpicker.Channel{
		{ID: "C111", Name: "general", Type: "channel"},
		{ID: "C222", Name: "general-help", Type: "channel"},
		{ID: "C333", Name: "engineering", Type: "channel"},
		{ID: "C444", Name: "secrets", Type: "private"},
	}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		var code rune = r
		if r == '\n' {
			code = tea.KeyEnter
		}
		m, _ = m.Update(tea.KeyPressMsg{Code: code, Text: string(r)})
	}
	return m
}

func TestChannelTriggersOnHashAtWordBoundary(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m, _ = m.Update(tea.KeyPressMsg{Code: '#', Text: "#"})
	if !m.IsChannelActive() {
		t.Error("expected channel picker to activate after # at start of input")
	}
}

func TestChannelTriggersAfterSpace(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "see ")
	m, _ = m.Update(tea.KeyPressMsg{Code: '#', Text: "#"})
	if !m.IsChannelActive() {
		t.Error("expected channel picker to activate after space + #")
	}
}

func TestChannelDoesNotTriggerMidWord(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "issue")
	m, _ = m.Update(tea.KeyPressMsg{Code: '#', Text: "#"})
	if m.IsChannelActive() {
		t.Error("expected channel picker NOT to activate inside a word (issue#42)")
	}
}

func TestChannelQueryFilters(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "#gen")
	if !m.IsChannelActive() {
		t.Fatal("expected picker active after #gen")
	}
	filtered := m.channelPicker.Filtered()
	if len(filtered) != 2 {
		t.Errorf("expected 2 matches for 'gen', got %d", len(filtered))
	}
}

func TestChannelEscDismisses(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "#")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.IsChannelActive() {
		t.Error("Esc should close the channel picker")
	}
}

func TestChannelBackspaceCancelsBeforeHash(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "#g")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}) // delete 'g'
	if !m.IsChannelActive() {
		t.Fatal("picker should still be active after deleting query but not the trigger")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}) // delete '#'
	if m.IsChannelActive() {
		t.Error("picker should close once backspace removes the # trigger")
	}
}

func TestChannelEnterInsertsName(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "#gen")
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.IsChannelActive() {
		t.Error("picker should close after Enter")
	}
	got := m.Value()
	if !strings.HasPrefix(got, "#general ") {
		t.Errorf("expected value to start with '#general ', got %q", got)
	}
}

func TestCloseChannel(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "#")
	m.CloseChannel()
	if m.IsChannelActive() {
		t.Error("CloseChannel should dismiss the picker")
	}
}

func TestChannelPickerViewWhenNotActive(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	if m.ChannelPickerView(40) != "" {
		t.Error("ChannelPickerView should be empty when picker is not active")
	}
}

func TestTranslateChannels(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())

	in := "see #general for details"
	out := m.TranslateMentionsForSend(in)
	want := "see <#C111|general> for details"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestTranslateChannelLongestNameFirst(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())

	// #general-help must not get mangled by a partial #general match.
	in := "ping #general-help please"
	out := m.TranslateMentionsForSend(in)
	want := "ping <#C222|general-help> please"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestTranslateChannelWordBoundary(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())

	// '-' is intentionally NOT in the word-boundary set, mirroring the
	// existing @mention behavior. So a bare #general followed by
	// "-help" must NOT translate (it'd be the wrong channel anyway --
	// the user meant #general-help, which is a separate entry).
	in := "see #general-help and #general now"
	out := m.TranslateMentionsForSend(in)
	want := "see <#C222|general-help> and <#C111|general> now"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestTranslateUserAndChannelTogether(t *testing.T) {
	m := New("general")
	m.SetUsers([]mentionpicker.User{
		{ID: "U1", DisplayName: "Alice", Username: "alice"},
	})
	m.SetChannels(chans())

	in := "@Alice see #engineering"
	out := m.TranslateMentionsForSend(in)
	want := "<@U1> see <#C333|engineering>"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestTranslatePrivateChannel(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())

	in := "see #secrets"
	out := m.TranslateMentionsForSend(in)
	want := "see <#C444|secrets>"
	if out != want {
		t.Errorf("expected %q, got %q", want, out)
	}
}

func TestResetClearsChannelPicker(t *testing.T) {
	m := New("general")
	m.SetChannels(chans())
	m.SetWidth(80)
	m.Focus()

	m = typeText(m, "#")
	if !m.IsChannelActive() {
		t.Fatal("setup: picker should be active")
	}
	m.Reset()
	if m.IsChannelActive() {
		t.Error("Reset should close the channel picker")
	}
}
