package channelpicker

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

func testStyles(fg string) Styles {
	return Styles{Name: lipgloss.NewStyle().Foreground(lipgloss.Color(fg))}
}

func TestViewEmptyWhenHiddenOrNoMatches(t *testing.T) {
	m := New(WithStyles(testStyles("#ff0000")), WithWidth(30))
	m.SetChannels([]Channel{{ID: "C1", Name: "general", Type: "channel"}})
	if m.View() != "" {
		t.Error("hidden picker should render empty")
	}
	m.Open()
	if m.View() == "" {
		t.Error("open picker with matches should render")
	}
	m.SetQuery("zzz")
	if m.View() != "" {
		t.Error("picker with no matches should render empty")
	}
}

func TestSetStylesChangesOutput(t *testing.T) {
	m := New(WithStyles(testStyles("#ff0000")), WithWidth(30))
	m.SetChannels([]Channel{{ID: "C1", Name: "general", Type: "channel"}})
	m.Open()
	before := m.View()
	m.SetStyles(testStyles("#00ff00"))
	if m.View() == before {
		t.Error("SetStyles did not change the rendered output")
	}
}

func TestWidthIsRespected(t *testing.T) {
	m := New(WithStyles(Styles{Chrome: chrome.Styles{}}), WithWidth(20))
	m.SetChannels([]Channel{{ID: "C1", Name: "general", Type: "channel"}})
	m.Open()
	narrow := m.View()
	m.SetWidth(40)
	wide := m.View()
	if lipgloss.Width(wide) <= lipgloss.Width(narrow) {
		t.Errorf("width 40 (%d) not wider than width 20 (%d)", lipgloss.Width(wide), lipgloss.Width(narrow))
	}
}

func TestFilterByNamePrefix(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "general-help", Type: "channel"},
	})
	m.Open()
	m.SetQuery("gen")
	if len(m.Filtered()) != 2 {
		t.Fatalf("expected 2 filtered channels, got %d", len(m.Filtered()))
	}
	if m.Filtered()[0].ID != "C1" {
		t.Errorf("expected general first, got %s", m.Filtered()[0].Name)
	}
}

func TestFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "Engineering", Type: "channel"},
	})
	m.Open()
	m.SetQuery("ENG")
	if len(m.Filtered()) != 1 {
		t.Fatalf("expected 1 filtered channel, got %d", len(m.Filtered()))
	}
}

func TestFilterEmptyQueryShowsAllUpToMax(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "alpha", Type: "channel"},
		{ID: "C2", Name: "beta", Type: "channel"},
		{ID: "C3", Name: "gamma", Type: "channel"},
		{ID: "C4", Name: "delta", Type: "channel"},
		{ID: "C5", Name: "epsilon", Type: "channel"},
		{ID: "C6", Name: "zeta", Type: "channel"},
	})
	m.Open()
	m.SetQuery("")
	if len(m.Filtered()) != MaxVisible {
		t.Fatalf("expected %d filtered (max), got %d", MaxVisible, len(m.Filtered()))
	}
}

func TestFilterPrivateAndDM(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "secrets", Type: "private"},
		{ID: "D1", Name: "alice", Type: "dm"},
	})
	m.Open()
	m.SetQuery("")
	if len(m.Filtered()) != 3 {
		t.Fatalf("expected all 3 filtered, got %d", len(m.Filtered()))
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected not visible initially")
	}
	m.Open()
	if !m.IsVisible() {
		t.Error("expected visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("expected not visible after Close")
	}
}

func TestMoveUpDown(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "alpha", Type: "channel"},
		{ID: "C2", Name: "beta", Type: "channel"},
	})
	m.Open()
	m.SetQuery("")
	if m.Selected() != 0 {
		t.Errorf("expected selected=0, got %d", m.Selected())
	}
	m.MoveDown()
	if m.Selected() != 1 {
		t.Errorf("expected selected=1, got %d", m.Selected())
	}
	m.MoveDown()
	if m.Selected() != 1 {
		t.Errorf("expected selected=1 (clamped at end), got %d", m.Selected())
	}
	m.MoveUp()
	if m.Selected() != 0 {
		t.Errorf("expected selected=0, got %d", m.Selected())
	}
	m.MoveUp()
	if m.Selected() != 0 {
		t.Errorf("expected selected=0 (clamped), got %d", m.Selected())
	}
}

func TestSelectReturnsResult(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "general", Type: "channel"},
	})
	m.Open()
	m.SetQuery("gen")
	result := m.Select()
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ChannelID != "C1" {
		t.Errorf("expected C1, got %s", result.ChannelID)
	}
	if result.Name != "general" {
		t.Errorf("expected general, got %s", result.Name)
	}
}

func TestSelectEmptyReturnsNil(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{})
	m.Open()
	m.SetQuery("zzz")
	if m.Select() != nil {
		t.Error("expected nil result for empty filtered list")
	}
}

func TestSetQueryResetsSelection(t *testing.T) {
	m := New()
	m.SetChannels([]Channel{
		{ID: "C1", Name: "alpha"},
		{ID: "C2", Name: "alphabet"},
	})
	m.Open()
	m.SetQuery("a")
	m.MoveDown()
	if m.Selected() != 1 {
		t.Fatalf("test setup: expected selected=1, got %d", m.Selected())
	}
	m.SetQuery("al")
	if m.Selected() != 0 {
		t.Errorf("SetQuery should reset selected to 0, got %d", m.Selected())
	}
}
