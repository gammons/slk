package blockkit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	imgpkg "github.com/gammons/slk/internal/image"
)

// narrowWidths are the content widths the fallback-line tests sweep:
// around and well below the length of the longest label, which is 51
// cells for a short host.
var narrowWidths = []int{1, 2, 3, 5, 10, 16, 20, 30, 41, 50, 51, 52, 60, 80}

// legacyMinWidth is the narrowest width a legacy attachment can render
// in. It reserves a 2-cell colour stripe and floors its content width
// at 1 (contentW in attachments.go), so below 3 cells the stripe alone
// overflows. That predates the load policy and is not what these tests
// are about.
const legacyMinWidth = 3

// assertLinesFit fails when any rendered line is wider than the width
// it was rendered for, or when a line carries an embedded newline.
//
// Both are the same defect seen from two sides. RenderMessage counts
// Block Kit output as one row per entry in RenderResult.Lines
// (preAttachmentRows += len(bkLines), render_message.go), but the pane
// re-wraps the finished message through a lipgloss width in WrapRows.
// An entry wider than the pane therefore becomes two visual rows that
// nothing counted, and every later file image, click target and sixel
// placement in the message is shifted up a row.
func assertLinesFit(t *testing.T, what string, lines []string, width int) {
	t.Helper()
	for i, l := range lines {
		if strings.Contains(l, "\n") {
			t.Errorf("%s w=%d: line %d holds a newline, so it is one entry but several rows: %q",
				what, width, i, ansi.Strip(l))
			continue
		}
		if got := lipgloss.Width(ansi.Strip(l)); got > width {
			t.Errorf("%s w=%d: line %d is %d cells wide, want <= %d: %q",
				what, width, i, got, width, ansi.Strip(l))
		}
	}
}

// heldPolicyCtx is a context that renders inline images but holds back
// anything off a Slack host.
func heldPolicyCtx(proto imgpkg.Protocol) Context {
	ctx := imageCtx(proto)
	ctx.Policy = imgpkg.NewLoadPolicy(imgpkg.LoadTrusted, nil)
	return ctx
}

// TestNotLoadedImageLineFitsWidth covers the third-round review finding
// on PR #467. The held-back image line is "[image from <host> not
// loaded · I to load]" — 51 cells for a short host — and it was emitted
// at full length however narrow the pane was, while its caller counted
// it as a single row.
func TestNotLoadedImageLineFitsWidth(t *testing.T) {
	const held = "https://tracker.example/pixel.png"
	for _, w := range narrowWidths {
		r := Render([]Block{ImageBlock{URL: held}}, heldPolicyCtx(imgpkg.ProtoHalfBlock), w)
		assertLinesFit(t, "image block", r.Lines, w)

		if w >= legacyMinWidth {
			l := RenderLegacy([]LegacyAttachment{{ImageURL: held}}, heldPolicyCtx(imgpkg.ProtoHalfBlock), w)
			assertLinesFit(t, "legacy image_url", l.Lines, w)
		}
	}
}

// TestNotLoadedAccessoryLineFitsWidth is the same for a section's image
// accessory, whose label gains a " · not loaded" suffix under the
// policy. A stacked accessory (one too wide to sit beside the body) is
// appended as its own counted row. The body text is a single cell so
// that a width narrower than one word — which cannot wrap, and predates
// this change — does not confound the result.
func TestNotLoadedAccessoryLineFitsWidth(t *testing.T) {
	const held = "https://tracker.example/pixel.png"
	for _, w := range narrowWidths {
		r := Render([]Block{SectionBlock{
			Text:      "x",
			Accessory: ImageAccessory{URL: held, AltText: "a company logo in full colour"},
		}}, heldPolicyCtx(imgpkg.ProtoHalfBlock), w)
		assertLinesFit(t, "section accessory", r.Lines, w)
	}
}

// TestImageLinkFallbackFitsWidth covers the plain-link fallback, which
// has the same shape: "[image] <url>" at full length. This one predates
// the image_loading policy — a long URL overflowed a narrow pane on
// main — but it is the same helper and the same miscount.
func TestImageLinkFallbackFitsWidth(t *testing.T) {
	const long = "https://example.test/a/very/long/path/that/will/not/fit/in/a/narrow/pane/image.png"
	for _, w := range narrowWidths {
		// ProtoOff: no inline rendering, so always the link.
		r := Render([]Block{ImageBlock{URL: long}}, heldPolicyCtx(imgpkg.ProtoOff), w)
		assertLinesFit(t, "link fallback", r.Lines, w)

		if w >= legacyMinWidth {
			l := RenderLegacy([]LegacyAttachment{{ImageURL: long}}, heldPolicyCtx(imgpkg.ProtoOff), w)
			assertLinesFit(t, "legacy link fallback", l.Lines, w)
		}
	}
}

// TestNotLoadedLinesKeepTheirHintWhenTheyFit pins the other side: a
// pane wide enough must still show the whole hint rather than a
// shortened or truncated one.
func TestNotLoadedLinesKeepTheirHintWhenTheyFit(t *testing.T) {
	const held = "https://tracker.example/pixel.png"
	ctx := heldPolicyCtx(imgpkg.ProtoHalfBlock)

	r := Render([]Block{ImageBlock{URL: held}}, ctx, 80)
	got := ansi.Strip(strings.Join(r.Lines, "\n"))
	if want := NotLoadedLabel("tracker.example"); !strings.Contains(got, want) {
		t.Errorf("image block at width 80 should keep the full hint:\nwant %q\ngot  %q", want, got)
	}

	s := Render([]Block{SectionBlock{
		Text:      "body",
		Accessory: ImageAccessory{URL: held, AltText: "logo"},
	}}, ctx, 80)
	got = ansi.Strip(strings.Join(s.Lines, "\n"))
	if want := "[image: logo · not loaded]"; !strings.Contains(got, want) {
		t.Errorf("accessory at width 80 should keep the full label:\nwant %q\ngot  %q", want, got)
	}
}

// TestFittedFallbackKeepsItsLink asserts the OSC 8 target survives
// fitting. The label is cut before the hyperlink is wrapped around it,
// so a narrow pane still offers the whole URL as the click target —
// which is why these lines truncate rather than wrap (ScanOsc8Spans
// only registers a span that starts and ends on one row).
func TestFittedFallbackKeepsItsLink(t *testing.T) {
	const long = "https://example.test/a/very/long/path/that/will/not/fit/image.png"
	line := renderImageFallback(long, 12)

	if got := lipgloss.Width(ansi.Strip(line)); got > 12 {
		t.Fatalf("fitted line is %d cells, want <= 12: %q", got, ansi.Strip(line))
	}
	if !strings.Contains(line, "\x1b]8;;"+long+"\x1b\\") {
		t.Errorf("fitted line lost its OSC 8 target %q: %q", long, line)
	}
	// Contains, not HasSuffix: the muted style closes with its own SGR
	// reset after the terminator.
	if !strings.Contains(line, "[image]\x1b]8;;\x1b\\") {
		t.Errorf("fitted line lost its OSC 8 terminator: %q", line)
	}
}
