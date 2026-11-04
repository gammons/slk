package emojipicker

import (
	"context"
	goimage "image"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
)

func sampleEntries() []emoji.EmojiEntry {
	return []emoji.EmojiEntry{
		{Name: "apple", Display: "🍎"},
		{Name: "rock", Display: "🪨"},
		{Name: "rocket", Display: "🚀"},
		{Name: "rose", Display: "🌹"},
		{Name: "zebra", Display: "🦓"},
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.IsVisible() {
		t.Fatal("expected not visible initially")
	}
	m.Open("ro")
	if !m.IsVisible() {
		t.Fatal("expected visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Fatal("expected not visible after Close")
	}
}

func TestPrefixFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("RO")
	got := m.Filtered()
	wantNames := []string{"rock", "rocket", "rose"}
	if len(got) != len(wantNames) {
		t.Fatalf("expected %d filtered, got %d", len(wantNames), len(got))
	}
	for i, n := range wantNames {
		if got[i].Name != n {
			t.Errorf("filtered[%d] = %q, want %q", i, got[i].Name, n)
		}
	}
}

func TestEmptyQueryShowsFirstN(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("")
	if len(m.Filtered()) != len(sampleEntries()) {
		// MaxVisible=5 and we provided exactly 5 entries.
		t.Errorf("expected all entries visible, got %d", len(m.Filtered()))
	}
}

func TestMoveUpDownClamps(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro") // 3 results: rock, rocket, rose
	if m.Selected() != 0 {
		t.Errorf("initial selected = %d, want 0", m.Selected())
	}
	m.MoveDown()
	m.MoveDown()
	m.MoveDown() // clamp at 2
	if m.Selected() != 2 {
		t.Errorf("after 3 down on 3 items, selected = %d, want 2", m.Selected())
	}
	m.MoveUp()
	m.MoveUp()
	m.MoveUp() // clamp at 0
	if m.Selected() != 0 {
		t.Errorf("after 3 up, selected = %d, want 0", m.Selected())
	}
}

func TestSelectedEntry(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.MoveDown() // rocket
	got, ok := m.SelectedEntry()
	if !ok {
		t.Fatal("expected selectedEntry ok=true")
	}
	if got.Name != "rocket" {
		t.Errorf("selected = %q, want rocket", got.Name)
	}
}

func TestSelectedEntryEmpty(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("zzz") // no matches
	if got, ok := m.SelectedEntry(); ok {
		t.Errorf("expected ok=false, got %+v", got)
	}
}

func TestSetEntriesWhileVisibleClampsSelection(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.MoveDown()
	m.MoveDown() // selected=2 (rose)
	// Now restrict to a smaller list.
	m.SetEntries([]emoji.EmojiEntry{
		{Name: "rocket", Display: "🚀"},
	})
	got := m.Filtered()
	if len(got) != 1 {
		t.Fatalf("expected 1 filtered, got %d", len(got))
	}
	if m.Selected() != 0 {
		t.Errorf("expected selection clamped to 0, got %d", m.Selected())
	}
}

func TestSetQueryUpdatesFilter(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.SetQuery("ros")
	got := m.Filtered()
	if len(got) != 1 || got[0].Name != "rose" {
		t.Errorf("expected only rose, got %+v", got)
	}
	if m.Selected() != 0 {
		t.Errorf("selection should reset on SetQuery, got %d", m.Selected())
	}
}

func TestViewEmptyWhenInvisible(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.View(40) != "" {
		t.Error("expected empty view when not visible")
	}
}

func TestViewNonEmptyWhenVisibleWithMatches(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	if m.View(40) == "" {
		t.Error("expected non-empty view with matches")
	}
}

type fakeDropdownFetcher struct {
	prerender map[string]imgpkg.Render
}

func (f *fakeDropdownFetcher) Prerendered(key string, _ goimage.Point, _ imgpkg.Protocol) (imgpkg.Render, bool) {
	r, ok := f.prerender[key]
	return r, ok
}
func (f *fakeDropdownFetcher) Fetch(_ context.Context, _ imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

func TestDropdown_View_ImageMode_UsesPlacement(t *testing.T) {
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })

	thumbURL := emoji.CDNBaseURL + "1f44d.png"
	ff := &fakeDropdownFetcher{
		prerender: map[string]imgpkg.Render{
			emoji.EmojiCacheKey(thumbURL): {
				Cells: goimage.Pt(2, 1),
				Lines: []string{"\U0010EEEE\U0010EEEE"},
			},
		},
	}

	var m Model
	m.SetEntries([]emoji.EmojiEntry{
		{Name: "thumbsup", Display: "\U0001F44D"},
		{Name: "thumbsdown", Display: "\U0001F44E"},
	})
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emoji.PlaceContext{Fetcher: ff},
		Cells:    2,
	})
	m.Open("thumbs")

	out := m.View(40)
	if !strings.Contains(out, "\U0010EEEE") {
		t.Errorf("autocomplete View does not contain kitty placeholder runes\noutput=%q", out)
	}
}

// TestDropdown_ResolvesCustomEmojiURL_AfterSetEmojiCustoms guards the
// wiring fix: the dropdown must consult the customs map at View() time
// for URL resolution. Without SetEmojiCustoms, m.emojiCtx.Customs stays
// empty/nil from startup forever (the initial SetEmojiContext is called
// before CustomEmojisLoadedMsg arrives), so custom emoji rows fall back
// to the placeholder glyph instead of the image.
func TestDropdown_ResolvesCustomEmojiURL_AfterSetEmojiCustoms(t *testing.T) {
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })

	customURL := "https://emoji.slack-edge.com/T01/this-is-fine-fire/abc.gif"
	knownSGR := "\x1b[38;2;0;0;42m"
	placementLine := knownSGR + "\U0010EEEE\u0305\u0305\U0010EEEE\u0305\u030d\x1b[39m"

	ff := &fakeDropdownFetcher{
		prerender: map[string]imgpkg.Render{
			emoji.EmojiCacheKey(customURL): {
				Cells: goimage.Pt(2, 1),
				Lines: []string{placementLine},
			},
		},
	}

	var m Model
	m.SetEntries([]emoji.EmojiEntry{
		{Name: "this-is-fine-fire", Display: "\u25a1"}, // placeholder glyph for URL-backed
	})
	// Initial SetEmojiContext mirrors startup: empty Customs.
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emoji.PlaceContext{Fetcher: ff},
		Cells:    2,
		Customs:  nil, // <- the bug condition
	})
	// Now CustomEmojisLoadedMsg arrives via the new SetEmojiCustoms path.
	m.SetEmojiCustoms(map[string]string{
		"this-is-fine-fire": customURL,
	})

	m.Open("this-is-fine")

	view := m.View(40)
	if !strings.Contains(view, knownSGR) {
		t.Errorf("dropdown View does NOT contain image-ID SGR %q for the custom emoji — Customs not propagated to View()-time URL lookup", knownSGR)
		t.Logf("rendered view (first 200 chars): %q", view[:min(200, len(view))])
	}
}

func TestFrecentMatchesComeFirst(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.SetFrecency(emoji.NewFrecency([]string{"rose", "rocket"}))

	var got []string
	for _, e := range m.Filtered() {
		got = append(got, e.Name)
	}
	if want := "rose rocket rock"; strings.Join(got, " ") != want {
		t.Errorf("filtered = %v, want %s", got, want)
	}
	if e, ok := m.SelectedEntry(); !ok || e.Name != "rose" {
		t.Errorf("selected = %v, want the most frecent match rose", e)
	}
}
