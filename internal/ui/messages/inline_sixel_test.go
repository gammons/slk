package messages

import (
	goimage "image"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
)

// warmInlineFetcher serves one sixel payload for url at the emoji
// footprint, using the shared emoji-placement fake.
func warmInlineFetcher(url string) *fakePlaceFetcher {
	f := newFakePlaceFetcher()
	f.setPrerendered(emojiutil.EmojiCacheKey(url), goimage.Pt(2, 1), inlineTestRender())
	return f
}

func inlineTestRender() imgpkg.Render {
	return imgpkg.Render{
		Cells:    goimage.Pt(2, 1),
		Fallback: []string{"  "},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, "\x1bPqsixel")
			return err
		},
	}
}

func enableInlineSixel(t *testing.T) {
	t.Helper()
	emojiutil.SetSixelInlineMode(true, 2)
	t.Cleanup(func() { emojiutil.SetSixelInlineMode(false, 2) })
}

// containsInlineMarker reports whether s still carries an emoji marker
// rune (a lead or an index).
func containsInlineMarker(s string) bool {
	for _, r := range s {
		if r == emojiutil.InlineMarkerLead() {
			return true
		}
		if _, ok := emojiutil.InlineMarkerIndex(r); ok {
			return true
		}
	}
	return false
}

func TestResolveInlineEmojiSixel(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	f := warmInlineFetcher(url)
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: f}, 2, imgpkg.ProtoSixel)
	m1, _ := sink.Place(url)
	m2, _ := sink.Place(url)

	content := "hi " + m1 + " and " + m2 + "!"
	out, images := resolveInlineEmojiSixel(content, sink, nil)
	if containsInlineMarker(out) {
		t.Fatalf("markers leaked into the content: %q", out)
	}
	if want := "hi    and   !"; out != want {
		t.Errorf("resolved content = %q, want %q", out, want)
	}
	if len(images) != 2 {
		t.Fatalf("images = %d, want two", len(images))
	}
	if images[0].row != 0 || images[0].col != 3 {
		t.Errorf("first image row/col = %d/%d, want 0/3", images[0].row, images[0].col)
	}
	if images[1].row != 0 || images[1].col != 10 {
		t.Errorf("second image row/col = %d/%d, want 0/10", images[1].row, images[1].col)
	}
}

func TestResolveInlineEmojiSixel_ColdReservesOnly(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/cold/abc.png"
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: newFakePlaceFetcher()}, 2, imgpkg.ProtoSixel)
	marker, _ := sink.Place(url)

	out, images := resolveInlineEmojiSixel("a "+marker+" b", sink, nil)
	if out != "a    b" {
		t.Errorf("resolved content = %q, want the marker as spaces", out)
	}
	if len(images) != 0 {
		t.Errorf("images = %d, want none until the payload is warm", len(images))
	}
}

// Standard emoji keep their Unicode glyphs: only workspace custom emoji
// (which have no glyph) become sixel images.
func TestRenderMessage_InlineEmojiLeavesStandardEmojiAsGlyphs(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	f := warmInlineFetcher(url)
	renderer := imgrender.NewRenderer()
	renderer.SetContext(imgrender.ImageContext{Protocol: imgpkg.ProtoSixel})
	rc := &RenderContext{
		Emoji: EmojiContext{
			PlaceCtx: emojiutil.PlaceContext{Fetcher: f},
			Cells:    2,
			Customs:  map[string]string{"thanks-ty": url},
		},
		Images: renderer,
	}
	msg := MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: ":smile: :thanks-ty:"}
	out := RenderMessage(msg, 60, rc, RenderOptions{SixelRows: true})

	if len(out.SixelRows) != 1 {
		t.Fatalf("SixelRows = %d, want only the custom emoji", len(out.SixelRows))
	}
	plain := ansi.Strip(out.Content)
	if !strings.Contains(plain, "😄") {
		t.Errorf("standard emoji did not render as its glyph:\n%s", plain)
	}
}

// A message body with a custom emoji gets a sixel placement at the
// emoji's cell, and no sentinel reaches the content or plain text.
func TestRenderMessage_InlineEmojiPlacement(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	f := warmInlineFetcher(url)
	renderer := imgrender.NewRenderer()
	renderer.SetContext(imgrender.ImageContext{Protocol: imgpkg.ProtoSixel})

	rc := &RenderContext{
		Emoji: EmojiContext{
			PlaceCtx: emojiutil.PlaceContext{Fetcher: f},
			Cells:    2,
			Customs:  map[string]string{"thanks-ty": url},
		},
		Images: renderer,
	}
	msg := MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: ":thanks-ty: thanks"}
	out := RenderMessage(msg, 60, rc, RenderOptions{SixelRows: true})

	if containsInlineMarker(out.Content) {
		t.Fatalf("sentinel leaked into the content: %q", out.Content)
	}
	if len(out.SixelRows) != 1 {
		t.Fatalf("SixelRows = %d, want the emoji image", len(out.SixelRows))
	}
	sx := out.SixelRows[0]
	if sx.row != 1 {
		t.Errorf("image row = %d, want 1 (below the author line)", sx.row)
	}
	// The body starts at content column 0; the pane's border shifts
	// every image right by contentColBase (1 without an avatar).
	if sx.col != 1 {
		t.Errorf("image col = %d, want 1", sx.col)
	}
	if sx.width != 2 || sx.height != 1 {
		t.Errorf("image footprint = %dx%d, want 2x1", sx.width, sx.height)
	}
	if len(sx.bytes) == 0 {
		t.Error("image carries no sixel payload")
	}
	plain := strings.Split(ansi.Strip(out.Content), "\n")
	if len(plain) < 2 || !strings.Contains(plain[1], "  thanks") {
		t.Errorf("body row = %q, want two spaces where the image sits", plain)
	}
}

// A standard emoji with a Slack tone suffix keeps the old glyph
// fallback: strip the tone before resolving the glyph.
func TestRenderEmojiWith_SixelPathStripsStandardTone(t *testing.T) {
	enableInlineSixel(t)
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: newFakePlaceFetcher()}, 2, imgpkg.ProtoSixel)
	got := renderEmojiWith(":+1::skin-tone-5:", RenderSlackMarkdownOpts{
		InlineSixel: sink,
		Customs:     map[string]string{"thanks-ty": "https://e/t.gif"},
	})
	if strings.Contains(got, "skin-tone") || strings.Contains(got, ":+1:") {
		t.Errorf("tone shortcode left visible: %q", got)
	}
	if !strings.Contains(got, "👍") {
		t.Errorf("standard emoji did not resolve to its glyph: %q", got)
	}
}

// Emoji resolution must not rewrite the hidden target of an OSC 8 link;
// a shortcode-looking path segment stays a path segment.
func TestRenderEmojiWith_SixelKeepsOsc8Targets(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: warmInlineFetcher(url)}, 2, imgpkg.ProtoSixel)
	target := "https://example.com/:thanks-ty:"
	got := renderEmojiWith(osc8Hyperlink(target, "open"), RenderSlackMarkdownOpts{
		InlineSixel: sink,
		Customs:     map[string]string{"thanks-ty": url},
	})
	if !strings.Contains(got, target) {
		t.Errorf("link target was rewritten: %q", got)
	}
	if containsInlineMarker(got) {
		t.Errorf("a marker appeared for an emoji only present in the target: %q", got)
	}
	if len(sink.Images()) != 0 {
		t.Errorf("images = %d, want none", len(sink.Images()))
	}
}

// An emoji in the visible label still resolves; only the target is
// protected.
func TestRenderEmojiWith_SixelKeepsLinkLabelEmoji(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: warmInlineFetcher(url)}, 2, imgpkg.ProtoSixel)
	target := "https://example.com/x"
	got := renderEmojiWith(osc8Hyperlink(target, ":thanks-ty:"), RenderSlackMarkdownOpts{
		InlineSixel: sink,
		Customs:     map[string]string{"thanks-ty": url},
	})
	if !strings.Contains(got, target) {
		t.Errorf("link target was rewritten: %q", got)
	}
	if len(sink.Images()) != 1 {
		t.Errorf("images = %d, want the label's custom emoji", len(sink.Images()))
	}
}

// A literal U+E000 in the message is text, not a marker: it must not
// consume an image or shift the real one.
func TestResolveInlineEmojiSixel_LiteralLeadRune(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: warmInlineFetcher(url)}, 2, imgpkg.ProtoSixel)
	marker, _ := sink.Place(url)
	literal := string(emojiutil.InlineMarkerLead())
	out, images := resolveInlineEmojiSixel("a"+literal+"b "+marker, sink, nil)

	if !strings.ContainsRune(out, emojiutil.InlineMarkerLead()) {
		t.Errorf("the literal rune was removed: %q", out)
	}
	if containsInlineMarker(strings.ReplaceAll(out, literal, "")) {
		t.Errorf("marker runes leaked into the content besides the literal: %q", out)
	}
	if len(images) != 1 || images[0].col != 4 {
		t.Fatalf("images = %+v, want one at column 4", images)
	}
}

// A marker whose two runes a wrap put on different rows is dropped, not
// allowed to consume a later image.
func TestResolveInlineEmojiSixel_SplitMarker(t *testing.T) {
	enableInlineSixel(t)
	const url0 = "https://emoji.slack-edge.com/T1/one/a.png"
	const url1 = "https://emoji.slack-edge.com/T1/two/b.png"
	f := newFakePlaceFetcher()
	f.setPrerendered(emojiutil.EmojiCacheKey(url0), goimage.Pt(2, 1), inlineTestRender())
	f.setPrerendered(emojiutil.EmojiCacheKey(url1), goimage.Pt(2, 1), inlineTestRender())
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: f}, 2, imgpkg.ProtoSixel)
	m0, _ := sink.Place(url0)
	m1, _ := sink.Place(url1)
	rs := []rune(m0)
	content := "a " + string(rs[0]) + "\n" + string(rs[1]) + " b " + m1

	out, images := resolveInlineEmojiSixel(content, sink, nil)
	if containsInlineMarker(out) {
		t.Errorf("marker runes leaked into the content: %q", out)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want only the unsplit one", len(images))
	}
	if images[0].row != 1 || images[0].col != 4 {
		t.Errorf("image row/col = %d/%d, want 1/4", images[0].row, images[0].col)
	}
}

// A split marker whose second rune the blockquote border and padding
// pushed off rune zero is still found and dropped.
func TestResolveInlineEmojiSixel_SplitMarkerInQuote(t *testing.T) {
	enableInlineSixel(t)
	const url0 = "https://emoji.slack-edge.com/T1/one/a.png"
	const url1 = "https://emoji.slack-edge.com/T1/two/b.png"
	f := newFakePlaceFetcher()
	f.setPrerendered(emojiutil.EmojiCacheKey(url0), goimage.Pt(2, 1), inlineTestRender())
	f.setPrerendered(emojiutil.EmojiCacheKey(url1), goimage.Pt(2, 1), inlineTestRender())
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: f}, 2, imgpkg.ProtoSixel)
	m0, _ := sink.Place(url0)
	m1, _ := sink.Place(url1)
	rs := []rune(m0)
	// The next row starts with an SGR reset, the quote border, its
	// padding, and only then the marker's index rune.
	content := "a " + string(rs[0]) + "\n" +
		"\x1b[38;5;245m┃\x1b[0m " + string(rs[1]) + " b " + m1

	out, images := resolveInlineEmojiSixel(content, sink, nil)
	if containsInlineMarker(out) {
		t.Errorf("marker runes leaked into the quoted content: %q", out)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want only the unsplit one", len(images))
	}
	if images[0].row != 1 {
		t.Errorf("image row = %d, want 1", images[0].row)
	}
}

// Two literal lead runes are text and must not consume the marker that
// follows them.
func TestResolveInlineEmojiSixel_TwoLiteralLeadRunes(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: warmInlineFetcher(url)}, 2, imgpkg.ProtoSixel)
	marker, _ := sink.Place(url)
	literal := string(emojiutil.InlineMarkerLead())
	out, images := resolveInlineEmojiSixel("x"+literal+literal+" "+marker, sink, nil)

	if got := strings.Count(out, literal); got != 2 {
		t.Errorf("literal runes kept = %d, want 2: %q", got, out)
	}
	if containsInlineMarker(strings.ReplaceAll(out, literal, "")) {
		t.Errorf("marker runes leaked besides the literals: %q", out)
	}
	if len(images) != 1 || images[0].col != 4 {
		t.Fatalf("images = %+v, want one at column 4", images)
	}
}

// The avatar gutter is added once, after resolution: an emoji at the
// start of the body sits at contentColBase, not twice that.
func TestRenderMessage_InlineEmojiBesideAvatar(t *testing.T) {
	enableInlineSixel(t)
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	renderer := imgrender.NewRenderer()
	renderer.SetContext(imgrender.ImageContext{Protocol: imgpkg.ProtoSixel})
	rc := &RenderContext{
		Emoji: EmojiContext{
			PlaceCtx: emojiutil.PlaceContext{Fetcher: warmInlineFetcher(url)},
			Cells:    2,
			Customs:  map[string]string{"thanks-ty": url},
		},
		Images: renderer,
	}
	msg := MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: ":thanks-ty: hi"}
	out := RenderMessage(msg, 80, rc, RenderOptions{SixelRows: true, Avatar: "ab\ncd"})
	if len(out.SixelRows) != 1 {
		t.Fatalf("SixelRows = %d, want the emoji image", len(out.SixelRows))
	}
	// 1 border column + 5 avatar gutter columns.
	if got := out.SixelRows[0].col; got != 6 {
		t.Errorf("image col = %d, want 6 (contentColBase once)", got)
	}
}

// With one-cell emoji the marker is the index rune alone.
func TestResolveInlineEmojiSixel_OneCell(t *testing.T) {
	emojiutil.SetSixelInlineMode(true, 1)
	t.Cleanup(func() { emojiutil.SetSixelInlineMode(false, 2) })
	const url = "https://emoji.slack-edge.com/T1/thanks/abc.png"
	f := newFakePlaceFetcher()
	f.setPrerendered(emojiutil.EmojiCacheKey(url), goimage.Pt(1, 1), inlineTestRender())
	sink := emojiutil.NewInlineSixel(emojiutil.PlaceContext{Fetcher: f}, 1, imgpkg.ProtoSixel)
	marker, _ := sink.Place(url)
	out, images := resolveInlineEmojiSixel("x"+marker+"y", sink, nil)
	if out != "x y" {
		t.Errorf("resolved content = %q, want one blank cell", out)
	}
	if len(images) != 1 || images[0].width != 1 || images[0].col != 1 {
		t.Fatalf("images = %+v, want one of width 1 at column 1", images)
	}
}
