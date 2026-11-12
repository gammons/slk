package imgrender

import (
	"context"
	"image"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/config"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// emptyFetcher has nothing cached and fetches nothing.
type emptyFetcher struct{}

func (emptyFetcher) Fetch(context.Context, imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, imgpkg.ErrNotAllowed
}
func (emptyFetcher) Cached(string, image.Point) (image.Image, bool) { return nil, false }
func (emptyFetcher) Prerendered(string, image.Point, imgpkg.Protocol) (imgpkg.Render, bool) {
	return imgpkg.Render{}, false
}
func (emptyFetcher) ConfigurePrerender(imgpkg.Protocol)            {}
func (emptyFetcher) ConfigurePrerenderKitty(*imgpkg.KittyRenderer) {}

// A file image the policy holds back draws one "not loaded" line and
// starts no fetch; with image_loading = "off" it is the plain [Image]
// line; a trusted one starts its fetch as before.
func TestRenderBlockFollowsLoadPolicy(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	block := Block{
		Kind: "image", FileID: "F1", Name: "cat.png",
		URL:    "https://myteam.slack.com/files/U1/F1/cat.png",
		Thumbs: []ThumbSpec{{URL: "https://files.slack.com/files-tmb/T1-F1-x/cat_360.png", W: 360, H: 240}},
	}
	render := func(mode imgpkg.LoadMode) (string, bool) {
		r := NewRenderer()
		r.SetContext(ImageContext{
			Protocol: imgpkg.ProtoHalfBlock, Fetcher: emptyFetcher{}, CellPixels: image.Pt(10, 20), MaxRows: 10,
			Policy: imgpkg.NewLoadPolicy(mode, nil),
		})
		res := r.RenderBlock(block, "C1", "1.0", 80, 0, 0, 0)
		// The renderer marks a fetch in flight before spawning it.
		return ansi.Strip(strings.Join(res.Lines, "\n")), r.ClearFetching("F1-360")
	}

	got, inFlight := render(imgpkg.LoadConfirm)
	if inFlight || !strings.Contains(got, "[Image not loaded · I to load] cat.png") {
		t.Errorf("confirm: inFlight=%v lines=%q", inFlight, got)
	}
	got, inFlight = render(imgpkg.LoadOff)
	if inFlight || !strings.HasPrefix(got, "[Image] cat.png") {
		t.Errorf("off: inFlight=%v lines=%q", inFlight, got)
	}
	_, inFlight = render(imgpkg.LoadTrusted)
	if !inFlight {
		t.Errorf("trusted: Slack thumbnail fetch not started")
	}
}

// A held-back image's line must fit the width the caller wrapped bodies
// to, at every width. RenderBlock reports Height 1 for it, so a line
// wider than availWidth wraps and shifts every later attachment's rows
// in the entry, and its own click span ends up spread over two rows,
// which ScanOsc8Spans cannot register.
//
// markedLine used to truncate only the label, and only when the marker
// left a positive budget. The "not loaded" marker is 30 cells, so on any
// pane narrower than about 32 the label was left at full length and the
// 30-cell marker already overflowed on its own.
func TestNotLoadedLineFitsAvailWidth(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	block := Block{
		Kind: "image", FileID: "F1",
		Name:   "a-rather-long-screenshot-name.png",
		URL:    "https://myteam.slack.com/files/U1/F1/a-rather-long-screenshot-name.png",
		Thumbs: []ThumbSpec{{URL: "https://images.example.com/x_360.png", W: 360, H: 240}},
	}

	for _, availWidth := range []int{1, 2, 3, 5, 10, 16, 20, 29, 30, 31, 32, 40, 80} {
		r := NewRenderer()
		r.SetContext(ImageContext{
			Protocol: imgpkg.ProtoHalfBlock, Fetcher: emptyFetcher{}, CellPixels: image.Pt(10, 20), MaxRows: 10,
			// confirm holds back every image, so this is the not-loaded line.
			Policy: imgpkg.NewLoadPolicy(imgpkg.LoadConfirm, nil),
		})
		res := r.RenderBlock(block, "C1", "1.0", availWidth, 0, 0, 0)

		if len(res.Lines) != 1 || res.Height != 1 {
			t.Fatalf("availWidth=%d: got %d lines / Height %d, want 1/1",
				availWidth, len(res.Lines), res.Height)
		}
		plain := ansi.Strip(res.Lines[0])
		if got := lipgloss.Width(plain); got > availWidth {
			t.Errorf("availWidth=%d: line is %d cells wide and will wrap: %q",
				availWidth, got, plain)
		}
	}
}

// Fitting must not cost the full hint where there is room for it: at a
// normal width the line still names the key and the file.
func TestNotLoadedLineKeepsFullMarkerWhenItFits(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	block := Block{
		Kind: "image", FileID: "F1", Name: "cat.png",
		URL:    "https://myteam.slack.com/files/U1/F1/cat.png",
		Thumbs: []ThumbSpec{{URL: "https://images.example.com/cat_360.png", W: 360, H: 240}},
	}
	r := NewRenderer()
	r.SetContext(ImageContext{
		Protocol: imgpkg.ProtoHalfBlock, Fetcher: emptyFetcher{}, CellPixels: image.Pt(10, 20), MaxRows: 10,
		Policy: imgpkg.NewLoadPolicy(imgpkg.LoadConfirm, nil),
	})
	res := r.RenderBlock(block, "C1", "1.0", 80, 0, 0, 0)
	if got := ansi.Strip(strings.Join(res.Lines, "\n")); !strings.Contains(got, "[Image not loaded · I to load] cat.png") {
		t.Errorf("wide pane lost the full marker: %q", got)
	}
}
