package help

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

func sampleEntries() []Entry {
	return []Entry{
		{Key: "?", Desc: "show keybindings"},
		{Key: "j/down", Desc: "down"},
		{Key: "k/up", Desc: "up"},
		{Key: "/", Desc: "search"},
		{Key: "ctrl+t", Desc: "fuzzy find"},
		{Key: "Q", Desc: "quit (confirm)"},
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.IsVisible() {
		t.Error("should not be visible initially")
	}
	m.Open()
	if !m.IsVisible() {
		t.Error("should be visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Error("should not be visible after Close")
	}
}

func TestEscClosesModal(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("should be closed after esc")
	}
}

func TestQClosesModal(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("q")
	if m.IsVisible() {
		t.Error("should be closed after q")
	}
}

func TestQuestionMarkClosesModal(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("?")
	if m.IsVisible() {
		t.Error("should be closed after ?")
	}
}

func TestNavigationDown(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	if m.Selected() != 0 {
		t.Errorf("initial Selected = %d, want 0", m.Selected())
	}
	m.HandleKey("j")
	if m.Selected() != 1 {
		t.Errorf("after j, Selected = %d, want 1", m.Selected())
	}
	m.HandleKey("down")
	if m.Selected() != 2 {
		t.Errorf("after down, Selected = %d, want 2", m.Selected())
	}
}

func TestNavigationUp(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("j")
	m.HandleKey("j")
	m.HandleKey("k")
	if m.Selected() != 1 {
		t.Errorf("after j j k, Selected = %d, want 1", m.Selected())
	}
	m.HandleKey("up")
	if m.Selected() != 0 {
		t.Errorf("after up, Selected = %d, want 0", m.Selected())
	}
}

func TestNavigationBoundaries(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("k") // already at top
	if m.Selected() != 0 {
		t.Errorf("up at top, Selected = %d, want 0", m.Selected())
	}
	for i := 0; i < 20; i++ {
		m.HandleKey("j")
	}
	if m.Selected() != len(sampleEntries())-1 {
		t.Errorf("down past bottom, Selected = %d, want %d", m.Selected(), len(sampleEntries())-1)
	}
}

func TestSearchModeFiltersByDescription(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	if !m.IsSearching() {
		t.Error("should be in search mode after /")
	}
	m.HandleKey("d")
	m.HandleKey("o")
	m.HandleKey("w")
	m.HandleKey("n")
	visible := m.VisibleEntries()
	if len(visible) != 1 {
		t.Fatalf("filter 'down' should match 1 entry, got %d: %v", len(visible), visible)
	}
	if visible[0].Desc != "down" {
		t.Errorf("expected 'down', got %q", visible[0].Desc)
	}
}

func TestSearchModeMatchesKeyToo(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	m.HandleKey("c")
	m.HandleKey("t")
	m.HandleKey("r")
	m.HandleKey("l")
	visible := m.VisibleEntries()
	if len(visible) != 1 {
		t.Fatalf("filter 'ctrl' should match 1 entry, got %d", len(visible))
	}
	if visible[0].Key != "ctrl+t" {
		t.Errorf("expected ctrl+t, got %q", visible[0].Key)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	m.HandleKey("D")
	m.HandleKey("O")
	m.HandleKey("W")
	m.HandleKey("N")
	visible := m.VisibleEntries()
	if len(visible) != 1 {
		t.Errorf("uppercase filter should match case-insensitively, got %d entries", len(visible))
	}
}

func TestSearchBackspace(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	m.HandleKey("d")
	m.HandleKey("o")
	m.HandleKey("backspace")
	if m.Query() != "d" {
		t.Errorf("Query after backspace = %q, want d", m.Query())
	}
}

func TestEscFromSearchExitsSearch(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	m.HandleKey("d")
	m.HandleKey("esc")
	if !m.IsVisible() {
		t.Error("first esc should exit search, not close modal")
	}
	if m.IsSearching() {
		t.Error("should no longer be searching")
	}
	if m.Query() != "" {
		t.Errorf("query should be cleared, got %q", m.Query())
	}
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("second esc should close modal")
	}
}

func TestQDoesNotCloseDuringSearch(t *testing.T) {
	// While typing in the search box, q is a filter character, not close.
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	m.HandleKey("q")
	if !m.IsVisible() {
		t.Error("q during search should not close modal")
	}
	if m.Query() != "q" {
		t.Errorf("Query = %q, want q", m.Query())
	}
}

func TestSearchEnterExitsSearchKeepsFilter(t *testing.T) {
	// Enter while searching exits search mode but keeps the filter applied,
	// so the user can navigate with j/k over the filtered list.
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	m.HandleKey("/")
	m.HandleKey("d")
	m.HandleKey("enter")
	if m.IsSearching() {
		t.Error("enter should exit search mode")
	}
	if m.Query() != "d" {
		t.Errorf("Query after enter = %q, want d preserved", m.Query())
	}
}

func TestFromKeyMap(t *testing.T) {
	// FromKeyMap should derive entries from any struct of key.Binding fields.
	type sampleMap struct {
		A key.Binding
		B key.Binding
	}
	km := sampleMap{
		A: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "alpha")),
		B: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "beta")),
	}
	entries := FromKeyMap(km)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Verify both entries are present (order may be sorted).
	keys := make(map[string]string)
	for _, e := range entries {
		keys[e.Key] = e.Desc
	}
	if keys["a"] != "alpha" || keys["b"] != "beta" {
		t.Errorf("expected alpha and beta entries, got %v", keys)
	}
}

func TestFromKeyMapSkipsEmptyHelp(t *testing.T) {
	// Bindings without help metadata should be skipped.
	type sampleMap struct {
		A key.Binding
		B key.Binding
	}
	km := sampleMap{
		A: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "alpha")),
		B: key.NewBinding(key.WithKeys("b")), // no help
	}
	entries := FromKeyMap(km)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Desc != "alpha" {
		t.Errorf("expected alpha, got %q", entries[0].Desc)
	}
}

func TestFromKeyMapSorted(t *testing.T) {
	// Entries returned should be alphabetized by description for consistent display.
	type sampleMap struct {
		A key.Binding
		B key.Binding
		C key.Binding
	}
	km := sampleMap{
		A: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "zeta")),
		B: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "alpha")),
		C: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "mu")),
	}
	entries := FromKeyMap(km)
	descs := []string{entries[0].Desc, entries[1].Desc, entries[2].Desc}
	want := []string{"alpha", "mu", "zeta"}
	for i, w := range want {
		if descs[i] != w {
			t.Errorf("entries[%d].Desc = %q, want %q (full: %v)", i, descs[i], w, descs)
		}
	}
}

func TestViewOverlayInvisibleReturnsBackground(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	// Not open
	out := m.ViewOverlay(80, 24, "background")
	if out != "background" {
		t.Errorf("invisible overlay should return background unchanged, got %q", out)
	}
}

func TestViewOverlayContainsTitle(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open()
	out := m.ViewOverlay(80, 24, strings.Repeat(" ", 80*24))
	if !strings.Contains(out, "Keybindings") {
		t.Error("rendered overlay should contain title 'Keybindings'")
	}
}

// A CJK IME commit arrives as one key per rune: each must reach the
// query, and backspace must drop a whole rune.
func TestHandleKey_NonASCIIQuery(t *testing.T) {
	m := New()
	m.Open()
	m.HandleKey("/")
	m.HandleKey("中")
	m.HandleKey("文")
	if m.query != "中文" {
		t.Fatalf("query = %q, want %q", m.query, "中文")
	}
	m.HandleKey("backspace")
	if m.query != "中" {
		t.Errorf("after backspace query = %q, want %q", m.query, "中")
	}
}
