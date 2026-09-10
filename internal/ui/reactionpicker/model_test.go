package reactionpicker

import (
	"context"
	"fmt"
	goimage "image"
	"strings"
	"testing"

	slkemoji "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
)

func TestNewModel(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Error("expected picker to start hidden")
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", []string{"thumbsup"})
	if !m.IsVisible() {
		t.Error("expected picker to be visible after Open")
	}
	if m.channelID != "C123" {
		t.Errorf("expected channelID C123, got %s", m.channelID)
	}
	m.Close()
	if m.IsVisible() {
		t.Error("expected picker to be hidden after Close")
	}
}

func TestFilterByQuery(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)

	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")

	if m.query != "rock" {
		t.Errorf("expected query 'rock', got '%s'", m.query)
	}
	if len(m.filtered) == 0 {
		t.Error("expected filtered results for 'rock'")
	}
	for _, e := range m.filtered {
		if !stringContains(e.Name, "rock") {
			t.Errorf("filtered entry %s doesn't match query 'rock'", e.Name)
		}
	}
}

func TestNavigationUpDown(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("h")
	m.HandleKey("e")
	m.HandleKey("a")
	m.HandleKey("r")
	m.HandleKey("t")

	if len(m.filtered) < 2 {
		t.Skip("not enough filtered results for navigation test")
	}

	if m.selected != 0 {
		t.Errorf("expected selected 0, got %d", m.selected)
	}

	m.HandleKey("down")
	if m.selected != 1 {
		t.Errorf("expected selected 1 after down, got %d", m.selected)
	}

	m.HandleKey("up")
	if m.selected != 0 {
		t.Errorf("expected selected 0 after up, got %d", m.selected)
	}
}

func TestSelectEmoji(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")
	m.HandleKey("e")
	m.HandleKey("t")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on enter")
	}
	if result.Emoji == "" {
		t.Error("expected non-empty emoji in result")
	}
	if result.Remove {
		t.Error("expected Remove=false for new reaction")
	}
}

func TestSelectExistingReactionTogglesRemove(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", []string{"rocket"})
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	m.HandleKey("k")
	m.HandleKey("e")
	m.HandleKey("t")

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected a result on enter")
	}
	if !result.Remove {
		t.Error("expected Remove=true for existing reaction")
	}
}

func TestEscapeCloses(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	result := m.HandleKey("esc")
	if result != nil {
		t.Error("expected nil result on esc")
	}
	if m.IsVisible() {
		t.Error("expected picker to be hidden after esc")
	}
}

func TestBackspace(t *testing.T) {
	m := New()
	m.Open("C123", "1234.5678", nil)
	m.HandleKey("r")
	m.HandleKey("o")
	m.HandleKey("c")
	if m.query != "roc" {
		t.Errorf("expected query 'roc', got '%s'", m.query)
	}
	m.HandleKey("backspace")
	if m.query != "ro" {
		t.Errorf("expected query 'ro' after backspace, got '%s'", m.query)
	}
}

func TestFrecentShownWhenQueryEmpty(t *testing.T) {
	m := New()
	m.SetFrecentEmoji([]EmojiEntry{
		{Name: "thumbsup", Unicode: "\U0001f44d"},
		{Name: "rocket", Unicode: "\U0001f680"},
	})
	m.Open("C123", "1234.5678", nil)

	displayed := m.displayedList()
	if len(displayed) < 2 {
		t.Fatalf("expected at least 2 frecent entries, got %d", len(displayed))
	}
	if displayed[0].Name != "thumbsup" {
		t.Errorf("expected first frecent entry thumbsup, got %s", displayed[0].Name)
	}
}

func TestCustomEmojiAppearsInSearch(t *testing.T) {
	m := New()
	// A workspace returns a mix of URL-backed and alias-backed customs
	// from emoji.list. Both should be searchable in the reaction picker.
	m.SetCustomEmoji(map[string]string{
		"partyparrot":  "https://emoji.example.com/partyparrot.gif",
		"shipit_squir": "alias:rocket",
	})
	m.Open("C123", "1234.5678", nil)

	m.HandleKey("p")
	m.HandleKey("a")
	m.HandleKey("r")
	m.HandleKey("t")
	m.HandleKey("y")
	m.HandleKey("p")

	found := false
	for _, e := range m.filtered {
		if e.Name == "partyparrot" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected custom emoji 'partyparrot' in filtered results, got %v", m.filtered)
	}
}

func TestSubstringSearchSelectsCustomEmoji(t *testing.T) {
	m := New()
	m.SetCustomEmoji(map[string]string{
		"custom_needle": "https://emoji.example.com/custom_needle.gif",
	})
	m.Open("C123", "1234.5678", nil)

	for _, ch := range "needle" {
		m.HandleKey(string(ch))
	}

	result := m.HandleKey("enter")
	if result == nil {
		t.Fatal("expected substring search to produce a selectable result")
	}
	if result.Emoji != "custom_needle" {
		t.Fatalf("selected emoji = %q, want %q", result.Emoji, "custom_needle")
	}
}

func TestCustomEmojiOverridesBuiltin(t *testing.T) {
	m := New()
	m.SetCustomEmoji(map[string]string{
		"rocket": "https://emoji.example.com/rocket.gif",
	})
	m.Open("C123", "1234.5678", nil)

	count := 0
	for _, e := range m.allEmoji {
		if e.Name == "rocket" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one 'rocket' entry, got %d", count)
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// fakePickerFetcher is a test fake for emojiutil.PlaceFetcher. v1
// duplicates the equivalent fakes in messages/render_test.go and
// thread/model_test.go (rather than factoring into a shared testutil)
// — see polish list for the follow-up factor-out item.
type fakePickerFetcher struct {
	prerender map[string]imgpkg.Render
}

func newFakePickerFetcher() *fakePickerFetcher {
	return &fakePickerFetcher{prerender: map[string]imgpkg.Render{}}
}

func (f *fakePickerFetcher) setPrerendered(key string, t goimage.Point, r imgpkg.Render) {
	f.prerender[fmt.Sprintf("%s|%dx%d", key, t.X, t.Y)] = r
}

func (f *fakePickerFetcher) Prerendered(key string, t goimage.Point, _ imgpkg.Protocol) (imgpkg.Render, bool) {
	r, ok := f.prerender[fmt.Sprintf("%s|%dx%d", key, t.X, t.Y)]
	return r, ok
}

func (f *fakePickerFetcher) Fetch(_ context.Context, _ imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

func TestPicker_View_ImageMode_UsesPlacement(t *testing.T) {
	slkemoji.SetImageMode(true, 2)
	t.Cleanup(func() { slkemoji.SetImageMode(false, 2) })

	thumbURL := slkemoji.CDNBaseURL + "1f44d.png"
	ff := newFakePickerFetcher()
	ff.setPrerendered(slkemoji.EmojiCacheKey(thumbURL), goimage.Pt(2, 1), imgpkg.Render{
		Cells: goimage.Pt(2, 1),
		Lines: []string{"\U0010EEEE\U0010EEEE"},
	})

	m := New()
	m.Open("C123", "1234.5678", nil)
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: slkemoji.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil,
	})

	// Filter to a small set so the assert is unambiguous.
	for _, ch := range "thumbsup" {
		m.HandleKey(string(ch))
	}

	out := m.View(80)
	if !strings.Contains(out, "\U0010EEEE") {
		t.Errorf("picker View does not contain kitty placeholder runes\noutput=%q", out)
	}
}
