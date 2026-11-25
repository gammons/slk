package apphome

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func testHome() *core.AppHome {
	return &core.AppHome{
		AppID:     "A1",
		ViewID:    "V1",
		ServiceID: "B1",
		Blocks: []blocks.Block{
			blocks.HeaderBlock{Text: "Your Budget"},
			blocks.SectionBlock{Text: "Spent so far: $335"},
			blocks.ActionsBlock{Elements: []blocks.ActionElement{
				{Kind: "button", Label: "Refresh"},
			}},
		},
		Actions: []core.AppHomeAction{{ActionID: "refresh", Label: "Refresh"}},
	}
}

func TestViewShowsTitleTabsAndBlocks(t *testing.T) {
	m := New()
	m.SetChannelName("AI Budget Help")
	m.SetHome(testHome())
	out := ansi.Strip(m.View(12, 60, blockkit.Context{}))
	for _, want := range []string{"AI Budget Help", "Home", "Messages", "Your Budget", "[ Refresh ]"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	if lines := strings.Count(out, "\n") + 1; lines != 12 {
		t.Errorf("view has %d lines, want 12", lines)
	}
}

func TestViewHintNamesButtonsOnlyWhenThereAreSome(t *testing.T) {
	m := New()
	m.SetChannelName("app")
	m.SetHome(testHome())
	if out := ansi.Strip(m.View(8, 60, blockkit.Context{})); !strings.Contains(out, "b buttons") {
		t.Errorf("hint does not offer b for a view with buttons:\n%s", out)
	}

	home := testHome()
	home.Actions = nil
	m.SetHome(home)
	if out := ansi.Strip(m.View(8, 60, blockkit.Context{})); strings.Contains(out, "b buttons") {
		t.Errorf("hint offers b for a view with no buttons:\n%s", out)
	}
}

func TestScrollClamps(t *testing.T) {
	home := &core.AppHome{}
	for i := 0; i < 20; i++ {
		home.Blocks = append(home.Blocks, blocks.DividerBlock{})
	}
	m := New()
	m.SetHome(home)
	// 6 rows: 2 header rows + 4 content rows.
	m.View(6, 40, blockkit.Context{})

	m.ScrollUp(10)
	if m.yOffset != 0 {
		t.Errorf("ScrollUp past the top: offset = %d, want 0", m.yOffset)
	}
	m.ScrollDown(1)
	if m.yOffset != 1 {
		t.Fatalf("ScrollDown(1): offset = %d, want 1", m.yOffset)
	}
	m.GoToBottom()
	// 20 dividers are 20 content lines in a 4-line window.
	if m.yOffset != 16 {
		t.Errorf("GoToBottom: offset = %d, want 16", m.yOffset)
	}
	m.GoToTop()
	if m.yOffset != 0 {
		t.Errorf("GoToTop: offset = %d, want 0", m.yOffset)
	}
}

func TestVersionBumps(t *testing.T) {
	m := New()
	v := m.Version()
	m.SetChannelName("app")
	if m.Version() == v {
		t.Error("SetChannelName did not bump the version")
	}
	v = m.Version()
	m.SetChannelName("app")
	if m.Version() != v {
		t.Error("an unchanged name bumped the version")
	}
	m.SetHome(testHome())
	if m.Version() == v {
		t.Error("SetHome did not bump the version")
	}
	v = m.Version()
	m.View(20, 60, blockkit.Context{})
	if m.Version() != v {
		t.Error("View bumped the version")
	}

	// A view taller than the window can scroll; that redraws.
	tall := New()
	tall.SetHome(&core.AppHome{Blocks: []blocks.Block{
		blocks.DividerBlock{}, blocks.DividerBlock{},
		blocks.DividerBlock{}, blocks.DividerBlock{},
		blocks.DividerBlock{}, blocks.DividerBlock{},
	}})
	tall.View(4, 40, blockkit.Context{})
	v = tall.Version()
	tall.ScrollDown(1)
	if tall.Version() == v {
		t.Error("ScrollDown did not bump the version")
	}
}

func TestClear(t *testing.T) {
	m := New()
	m.SetChannelName("app")
	m.SetHome(testHome())
	m.Clear()
	if m.Home() != nil {
		t.Error("Clear left the home tab installed")
	}
	if out := ansi.Strip(m.View(6, 40, blockkit.Context{})); strings.Contains(out, "AI Budget") {
		t.Errorf("view after Clear still names the app:\n%s", out)
	}
}
