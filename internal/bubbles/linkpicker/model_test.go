package linkpicker

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/chrome"
)

func TestViewHiddenIsEmpty(t *testing.T) {
	m := New(WithWidth(100))
	if v := m.View(); v != "" {
		t.Errorf("hidden View = %q, want empty", v)
	}
}

func TestViewWidthClamping(t *testing.T) {
	cases := []struct{ termWidth, want int }{
		{200, 80}, // capped at 80
		{100, 60}, // 60%
		{50, 40},  // floor 40 (fits: 48 available)
		{30, 28},  // at most width-2
	}
	for _, c := range cases {
		m := New(WithWidth(c.termWidth))
		m.Open("t", items3())
		if got := lipgloss.Width(m.View()); got != c.want {
			t.Errorf("width %d: box width = %d, want %d", c.termWidth, got, c.want)
		}
	}
	m := New()
	m.Open("t", items3())
	m.SetWidth(100)
	if got := lipgloss.Width(m.View()); got != 60 {
		t.Errorf("SetWidth(100): box width = %d, want 60", got)
	}
}

func TestSetStylesChangesOutput(t *testing.T) {
	m := New(WithWidth(100))
	m.Open("t", items3())
	plain := m.View()
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	m.SetStyles(Styles{Chrome: chrome.Styles{}, Badge: badge})
	if m.View() == plain {
		t.Error("SetStyles did not change the rendered output")
	}
	if !strings.Contains(m.View(), "[slk]") {
		t.Error("badge text missing")
	}
}

func items3() []Item {
	return []Item{
		{URL: "https://a.example/1", Label: "first"},
		{URL: "https://b.example/2"},
		{URL: "https://myteam.slack.com/archives/C1/p1700000000000001", InApp: true},
	}
}

func TestOpenAndNavigate(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Fatal("visible before Open")
	}
	m.Open("Open link", items3())
	if !m.IsVisible() {
		t.Fatal("not visible after Open")
	}
	if _, chosen := m.HandleKey("j"); chosen {
		t.Error("j must not choose")
	}
	m.HandleKey("j")
	item, chosen := m.HandleKey("enter")
	if !chosen {
		t.Fatal("enter should choose")
	}
	if item.URL != items3()[2].URL {
		t.Errorf("chose %q", item.URL)
	}
	if m.IsVisible() {
		t.Error("should close after choose")
	}
}

func TestNavigationBounds(t *testing.T) {
	m := New()
	m.Open("Open link", items3())
	m.HandleKey("k") // at top; no-op
	item, chosen := m.HandleKey("enter")
	if !chosen || item.URL != items3()[0].URL {
		t.Errorf("chose %+v chosen=%v", item, chosen)
	}
	m.Open("Open link", items3())
	for i := 0; i < 10; i++ {
		m.HandleKey("j") // clamps at bottom
	}
	item, _ = m.HandleKey("enter")
	if item.URL != items3()[2].URL {
		t.Errorf("chose %q", item.URL)
	}
}

func TestEscCloses(t *testing.T) {
	m := New()
	m.Open("Open link", items3())
	if _, chosen := m.HandleKey("esc"); chosen {
		t.Error("esc must not choose")
	}
	if m.IsVisible() {
		t.Error("esc should close")
	}
}

func TestEnterOnEmptyIsNoop(t *testing.T) {
	m := New()
	m.Open("Open link", nil)
	if _, chosen := m.HandleKey("enter"); chosen {
		t.Error("enter on empty list must not choose")
	}
}

func TestOpenAssignsIndexAndTitle(t *testing.T) {
	m := New()
	m.Open("Download file", items3())
	if m.Title() != "Download file" {
		t.Errorf("title = %q", m.Title())
	}
	m.HandleKey("j")
	item, chosen := m.HandleKey("enter")
	if !chosen || item.Index != 1 {
		t.Errorf("chosen item = %+v chosen=%v", item, chosen)
	}
}
