// Package blockkit image.go: rendering of full-size ImageBlock and
// the shared fetch+render pipeline used by section image accessories
// (Task 11) and legacy attachment image_url (Task 13). The flow:
//
//  1. If we lack the means to render images at all (no Fetcher, or
//     ProtoOff, or no cell metrics), the caller falls back to a plain
//     OSC-8 hyperlinked "[image] <URL>" line.
//  2. If the URL hasn't been fetched/cached yet, we return a reserved-
//     height placeholder block and spawn one background goroutine per
//     URL to fetch it; on completion the goroutine dispatches a
//     BlockImageReadyMsg via Context.SendMsg so the host can
//     invalidate the render cache for the message.
//  3. If the URL is cached, we run it through the active protocol
//     (kitty / sixel / halfblock) just like the legacy file pipeline.
package blockkit

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"image"
	"io"
	"log"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// urlCacheKey hashes a URL into a short, stable cache key suitable
// for use with the image package's fetcher and renderer. Block image
// URLs lack the file-ID + thumb-suffix structure of the legacy file
// pipeline, so we derive a content-addressable key from the URL
// itself. The "BK-" prefix isolates blockkit-derived keys from
// file-ID keys ("F0123ABCD-720") in the shared cache.
func urlCacheKey(url string) string {
	sum := sha1.Sum([]byte(url))
	return "BK-" + hex.EncodeToString(sum[:8])
}

// renderImageFallback returns a single OSC-8 hyperlinked line for an
// image we cannot render inline (no fetcher, ProtoOff, empty URL, or
// missing cell metrics). Format: "[image] <url>".
func renderImageFallback(url string) string {
	if url == "" {
		return mutedStyle().Render("[image] (no url)")
	}
	label := "[image] " + url
	// OSC-8 hyperlink: \x1b]8;;URL\x1b\\LABEL\x1b]8;;\x1b\\
	hyper := "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
	return mutedStyle().Render(hyper)
}

// inflightURL tracks per-URL fetches so each URL only has one
// goroutine in flight at a time across all blockkit Render calls.
// The image.Fetcher uses singleflight internally on its own key, but
// each blockkit caller still independently spawns a goroutine and
// dispatches its own SendMsg on completion; without this guard, a
// freshly visible message with N image blocks would fire N redundant
// invalidations.
var (
	inflightURL   = map[string]struct{}{}
	inflightURLMu sync.Mutex
)

// computeBlockImageTarget chooses (cols, rows) for an image block
// render. Width is bounded by the caller's content width (and
// Context.MaxCols if set); height is capped at Context.MaxRows
// (default 20). Aspect ratio is derived from b.Width/b.Height when
// the block declares them, otherwise a 16:9 default is assumed
// (slack-go v0.23.0's ImageBlock doesn't expose source dims).
//
// Returns a zero point when CellPixels are missing — the caller
// must fall back to the textual link in that case.
func computeBlockImageTarget(b ImageBlock, ctx Context, width int) image.Point {
	if ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		return image.Point{}
	}
	aspect := 16.0 / 9.0
	if b.Width > 0 && b.Height > 0 {
		aspect = float64(b.Width) / float64(b.Height)
	}
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)
	maxRows := ctx.MaxRows
	if maxRows <= 0 {
		maxRows = 20
	}
	maxCols := width
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	rows := maxRows
	cols := int(float64(rows) * aspect / cellRatio)
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
	}
	if rows < 1 || cols < 1 {
		return image.Point{}
	}
	return image.Pt(cols, rows)
}

// fetchOrPlaceholder is the core fetch+render flow shared by image
// blocks (Task 10), section image accessories (Task 11), and legacy
// attachment image_url (Task 13).
//
// Returns rendered lines + flushes + (currently always-nil) sixelRows
// + the hit rect for click handling; ok is true if the function did
// the work (lines returned), false if the caller should fall back —
// e.g., empty URL.
//
// rowStart is the row index within the caller's RenderResult.Lines
// where this image will land — used to populate HitRect.RowStart.
func fetchOrPlaceholder(url string, target image.Point, ctx Context, rowStart int) (
	[]string, []func(io.Writer) error, map[int]SixelEntry, HitRect, bool,
) {
	if url == "" {
		return nil, nil, nil, HitRect{}, false
	}
	key := urlCacheKey(url)
	pixelTarget := image.Pt(target.X*ctx.CellPixels.X, target.Y*ctx.CellPixels.Y)

	hit := HitRect{
		RowStart: rowStart,
		RowEnd:   rowStart + target.Y,
		ColStart: 0,
		ColEnd:   target.X,
		URL:      url,
	}

	img, cached := ctx.Fetcher.Cached(key, pixelTarget)
	if !cached {
		// Spawn at most one fetcher goroutine per URL across all
		// concurrent Render calls. The fetcher itself dedupes on
		// req.Key, but each caller would otherwise still wait on
		// Fetch and fire its own SendMsg.
		inflightURLMu.Lock()
		_, busy := inflightURL[key]
		if !busy {
			inflightURL[key] = struct{}{}
		}
		inflightURLMu.Unlock()
		if !busy {
			channel := ctx.Channel
			ts := ctx.MessageTS
			send := ctx.SendMsg
			fetcher := ctx.Fetcher
			go func() {
				_, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
					Key: key, URL: url, Target: pixelTarget,
				})
				inflightURLMu.Lock()
				delete(inflightURL, key)
				inflightURLMu.Unlock()
				if err != nil {
					log.Printf("blockkit image fetch failed: key=%s url=%s err=%v", key, url, err)
					return
				}
				if send != nil {
					send(BlockImageReadyMsg{Channel: channel, TS: ts, URL: url})
				}
			}()
		}
		return blockPlaceholder(target), nil, nil, hit, true
	}

	// Cached: render via active protocol. For kitty we go through the
	// renderer's keyed path so the upload escape is emitted exactly
	// once per (key, target) pair across the session.
	if ctx.Protocol == imgpkg.ProtoKitty && ctx.KittyRender != nil {
		ckey := "BK-" + key
		ctx.KittyRender.SetSource(ckey, img)
		out := ctx.KittyRender.RenderKey(ckey, target)
		var fl []func(io.Writer) error
		if out.OnFlush != nil {
			fl = []func(io.Writer) error{out.OnFlush}
		}
		return out.Lines, fl, nil, hit, true
	}
	out := imgpkg.RenderImage(ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	return out.Lines, fl, nil, hit, true
}

// blockPlaceholder produces a target.Y-row block of theme-surface-
// colored spaces, with a single "⏳ loading…" cell on the middle row.
// The placeholder reserves layout space so the message doesn't reflow
// when the image lands (causing visible scroll jumps).
func blockPlaceholder(target image.Point) []string {
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	pad := strings.Repeat(" ", target.X)
	row := bg.Render(pad)
	out := make([]string, target.Y)
	for i := range out {
		out[i] = row
	}
	if target.Y > 0 {
		mid := target.Y / 2
		label := "⏳ loading…"
		w := lipgloss.Width(label)
		if w <= target.X {
			leftPad := (target.X - w) / 2
			out[mid] = bg.Render(strings.Repeat(" ", leftPad)) + bg.Render(label) +
				bg.Render(strings.Repeat(" ", target.X-leftPad-w))
		}
	}
	return out
}

// BlockImageReadyMsg is dispatched by the prefetcher when a block
// image has finished downloading. The host's Update handler wires
// this to a render-cache invalidation for the matching (Channel, TS)
// message so the next render picks up the cached image.
type BlockImageReadyMsg struct {
	Channel string
	TS      string
	URL     string
}
