package appview

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func TestView_EmptyModelDrawsNothing(t *testing.T) {
	var m Model
	if got := m.View(80, 24, blockkit.Context{}); got != "" {
		t.Errorf("empty model drew %q, want nothing", got)
	}
	if w, h := m.BoxSize(80, 24); w != 0 || h != 0 {
		t.Errorf("BoxSize = %d,%d, want 0,0", w, h)
	}
}

func TestView_DrawsTitleAndBlocks(t *testing.T) {
	m := New()
	m.SetView(&core.AppView{
		ViewID: "V1",
		Title:  "AI Budget Support",
		Blocks: []blocks.Block{
			blocks.HeaderBlock{Text: "Your AI Budget"},
			blocks.SectionBlock{Text: "Budget: $400"},
		},
		Actions: []core.AppHomeAction{{Label: "Request budget increase"}},
	})
	got := m.View(80, 24, blockkit.Context{})
	for _, want := range []string{"AI Budget Support", "Your AI Budget", "Budget: $400", "b buttons", "esc close"} {
		if !strings.Contains(got, want) {
			t.Errorf("dialog missing %q:\n%s", want, got)
		}
	}
	if w, h := m.BoxSize(80, 24); w <= 0 || h <= 0 {
		t.Errorf("BoxSize = %d,%d, want the drawn box", w, h)
	}
}

func TestView_ScrollsAndClamps(t *testing.T) {
	m := New()
	var blocksIn []blocks.Block
	for i := 0; i < 40; i++ {
		blocksIn = append(blocksIn, blocks.SectionBlock{Text: "row " + string(rune('a'+i%26))})
	}
	m.SetView(&core.AppView{ViewID: "V1", Title: "Tall", Blocks: blocksIn})

	m.View(80, 24, blockkit.Context{})
	if m.PageSize() != 16 {
		t.Fatalf("PageSize = %d, want 16 (24 minus the frame)", m.PageSize())
	}
	m.ScrollDown(1000)
	if m.yOffset != 24 {
		t.Errorf("yOffset = %d after scrolling past the end, want 24", m.yOffset)
	}
	m.ScrollUp(1000)
	if m.yOffset != 0 {
		t.Errorf("yOffset = %d after scrolling to the start, want 0", m.yOffset)
	}
	m.GoToBottom()
	if m.yOffset != 24 {
		t.Errorf("yOffset = %d after GoToBottom, want 24", m.yOffset)
	}
}

func TestSetView_ResetsScroll(t *testing.T) {
	m := New()
	var blocksIn []blocks.Block
	for i := 0; i < 40; i++ {
		blocksIn = append(blocksIn, blocks.SectionBlock{Text: "row"})
	}
	m.SetView(&core.AppView{ViewID: "V1", Blocks: blocksIn})
	m.View(80, 24, blockkit.Context{})
	m.GoToBottom()
	m.SetView(&core.AppView{ViewID: "V2", Blocks: blocksIn})
	if m.yOffset != 0 {
		t.Errorf("yOffset = %d after a new view, want 0", m.yOffset)
	}
}

func buttonView() *core.AppView {
	return &core.AppView{
		ViewID: "V1",
		Title:  "AI Budget Support",
		Blocks: []blocks.Block{
			blocks.SectionBlock{Text: "Your AI Budget"},
			blocks.ActionsBlock{BlockID: "b1", Elements: []blocks.ActionElement{
				{Kind: "button", Label: "Request budget increase", ActionID: "req_open"},
			}},
		},
		Actions: []core.AppHomeAction{
			{ActionID: "req_open", BlockID: "b1", Label: "Request budget increase"},
		},
	}
}

func TestClickAt_PressesTheButtonUnderTheCells(t *testing.T) {
	m := New()
	m.SetView(buttonView())
	m.View(80, 24, blockkit.Context{})
	if len(m.hits) != 1 {
		t.Fatalf("hits = %d, want one button", len(m.hits))
	}
	h := m.hits[0]
	if !m.ClickAt(80, 24, h.colStart, h.rowStart) {
		t.Fatal("a click on the button's first cell missed")
	}
	if m.LastClickAction() != 0 {
		t.Errorf("clicked action = %d, want 0", m.LastClickAction())
	}
	if m.ClickAt(80, 24, h.colStart, h.rowStart+2) {
		t.Error("a click below the button landed on it")
	}
}

// A button scrolled below the window is not clickable until it is drawn
// again.
func TestClickAt_OnlyVisibleButtons(t *testing.T) {
	m := New()
	v := buttonView()
	var tall []blocks.Block
	for i := 0; i < 40; i++ {
		tall = append(tall, blocks.SectionBlock{Text: "row"})
	}
	v.Blocks = append(tall, v.Blocks...)
	m.SetView(v)
	m.View(80, 24, blockkit.Context{})
	if len(m.hits) != 0 {
		t.Fatalf("hits = %d before scrolling, want none", len(m.hits))
	}
	m.GoToBottom()
	m.View(80, 24, blockkit.Context{})
	if len(m.hits) != 1 {
		t.Fatalf("hits = %d after scrolling, want the button", len(m.hits))
	}
}
