package channelfinder

import (
	"bytes"
	"context"
	goimage "image"
	"io"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// statusEmojiFetcher serves one prerendered placement per emoji URL.
type statusEmojiFetcher map[string]imgpkg.Render

func (f statusEmojiFetcher) Prerendered(key string, _ goimage.Point, _ imgpkg.Protocol) (imgpkg.Render, bool) {
	r, ok := f[key]
	return r, ok
}

func (f statusEmojiFetcher) Fetch(context.Context, imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

// serve makes the emoji name names warm and returns its placement, a
// realistic one ending in the "\x1b[39m" a real placement ends with.
func (f statusEmojiFetcher) serve(t *testing.T, name string, customs map[string]string, id string) string {
	t.Helper()
	url, ok := emoji.URLForShortcode(name, customs)
	if !ok {
		t.Fatalf("precondition: :%s: has no URL", name)
	}
	placement := "\x1b[38;2;0;0;" + id + "m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
	f[emoji.EmojiCacheKey(url)] = imgpkg.Render{
		Cells: goimage.Pt(2, 1),
		Lines: []string{placement},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, "<upload "+name+">")
			return err
		},
	}
	return placement
}

func statusEmojiImages(t *testing.T) *bytes.Buffer {
	t.Helper()
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })
	var buf bytes.Buffer
	prev := imgpkg.KittyOutput
	imgpkg.KittyOutput = &buf
	t.Cleanup(func() { imgpkg.KittyOutput = prev })
	return &buf
}

// imageAfter reports whether placement is drawn after name, separated
// only by escape sequences and one space.
func imageAfter(box, name, placement string) bool {
	i := strings.Index(box, name)
	j := strings.Index(box, placement)
	if i < 0 || j < i {
		return false
	}
	return ansi.Strip(box[i:j]) == name+" "
}

// assertEvenRows fails when the box's rows are not all the same width,
// i.e. a row's measured width and what it draws disagree.
func assertEvenRows(t *testing.T, box string) {
	t.Helper()
	lines := strings.Split(box, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) != lipgloss.Width(lines[0]) {
			t.Errorf("row %d is %d cells, row 0 is %d:\n%s", i, lipgloss.Width(l), lipgloss.Width(lines[0]), box)
		}
	}
}

func TestStatusEmojiImage_OnTheDMRow(t *testing.T) {
	kitty := statusEmojiImages(t)
	f := statusEmojiFetcher{}
	placement := f.serve(t, "calendar", nil, "71")

	m := New()
	m.SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: f}, Cells: 2})
	m.SetItems([]core.ChannelFinderItem{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
		{ID: "D1", Name: "Alice", Type: "dm", Joined: true, Presence: "active"},
	})
	m.SetStatus("D1", peerstatus.Status{Emoji: ":calendar:"})
	m.Open()

	got := m.View(80)
	if !imageAfter(got, "Alice", placement) {
		t.Errorf("DM row does not draw the status emoji as an image after the name:\n%q", got)
	}
	if strings.Contains(got, emoji.CodeMap()[":calendar:"]) {
		t.Errorf("DM row still draws the glyph:\n%q", got)
	}
	if n := strings.Count(kitty.String(), "<upload calendar>"); n != 1 {
		t.Errorf("status image uploaded %d times from View, want 1", n)
	}
	assertEvenRows(t, got)

	// The same on the selected row.
	m.HandleKey("down")
	sel := m.View(80)
	if !strings.Contains(sel, placement) {
		t.Errorf("selected DM row lost the image:\n%q", sel)
	}
	assertEvenRows(t, sel)
}

// A long name truncates; the image after it is never cut.
func TestStatusEmojiImage_LongNameTruncatesNotTheImage(t *testing.T) {
	statusEmojiImages(t)
	f := statusEmojiFetcher{}
	placement := f.serve(t, "headphones", nil, "72")

	m := New()
	m.SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: f}, Cells: 2})
	m.SetItems([]core.ChannelFinderItem{{ID: "D1", Name: strings.Repeat("averylongdisplayname", 5), Type: "dm", Joined: true}})
	m.SetStatus("D1", peerstatus.Status{Huddle: peerstatus.HuddleActive})
	m.Open()

	got := m.View(40)
	if !imageAfter(got, "…", placement) {
		t.Errorf("expected the name truncated and the image whole after it:\n%q", got)
	}
	assertEvenRows(t, got)
}

// A custom status emoji shows the fallback glyph until the workspace's
// custom emoji arrive, then its artwork; with image mode off the row is
// what it always was.
func TestStatusEmojiImage_CustomAndFallback(t *testing.T) {
	customs := map[string]string{"company-logo": "https://emoji.slack-edge.com/T1/company-logo/abc.png"}
	f := statusEmojiFetcher{}
	placement := f.serve(t, "company-logo", customs, "73")
	newFinder := func(ctx messages.EmojiContext) Model {
		m := New()
		m.SetEmojiContext(ctx)
		m.SetItems([]core.ChannelFinderItem{{ID: "D1", Name: "Alice", Type: "dm", Joined: true}})
		m.SetStatus("D1", peerstatus.Status{Emoji: ":company-logo:", Text: "Shipping"})
		m.Open()
		return m
	}

	plain := newFinder(messages.EmojiContext{})
	want := plain.View(80)

	off := newFinder(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: f}, Cells: 2, Customs: customs})
	if got := off.View(80); got != want {
		t.Errorf("image mode off changed the box:\n got %q\nwant %q", got, want)
	}

	statusEmojiImages(t)
	m := newFinder(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: f}, Cells: 2})
	if got := m.View(80); !strings.Contains(got, "Alice "+peerstatus.FallbackGlyph) {
		t.Fatalf("before customs load the row should draw the fallback glyph:\n%q", got)
	}
	m.SetEmojiCustoms(customs)
	if got := m.View(80); !imageAfter(got, "Alice", placement) {
		t.Errorf("custom status emoji not drawn as an image after SetEmojiCustoms:\n%q", got)
	}
}
