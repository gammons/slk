package compose

import (
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/styles"
)

// A theme change pushed through RefreshStyles must reach the channel
// picker popup, and a fresh composer must already be themed.
func TestRefreshStylesReachesChannelPicker(t *testing.T) {
	t.Cleanup(func() { styles.Apply("dark", core.Theme{}) })
	styles.Apply("dark", core.Theme{})

	m := New("general")
	m.SetChannels(chans())
	m.channelActive = true
	m.channelPicker.Open()

	before := m.ChannelPickerView(40)
	if before == "" {
		t.Fatal("expected a rendered popup")
	}

	styles.Apply("light", core.Theme{})
	if got := m.ChannelPickerView(40); got != before {
		t.Fatal("popup changed before RefreshStyles pushed the new theme")
	}
	m.RefreshStyles()
	if got := m.ChannelPickerView(40); got == before {
		t.Fatal("RefreshStyles did not reach the channel picker")
	}
}
