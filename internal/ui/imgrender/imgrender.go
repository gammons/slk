// Package imgrender renders inline image attachments for any UI panel
// (messages pane, thread side panel) using the kitty / sixel /
// halfblock pipelines. Two callers — internal/ui/messages and
// internal/ui/thread — embed a Renderer (added in a follow-up task)
// to share the fetch-tracking and per-block encode logic.
//
// This file holds the standalone types and pure helpers. The Renderer
// itself comes in the next task.
package imgrender

import (
	"bytes"
	"context"
	"image"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/styles"
)

// ImageContext bundles the dependencies a Renderer needs. Configured
// at startup via Renderer.SetContext (added in the next task). SendMsg
// is optional; when nil, fetches still complete but no re-render is
// triggered when bytes arrive.
type ImageContext struct {
	Protocol    imgpkg.Protocol
	Fetcher     *imgpkg.Fetcher
	KittyRender *imgpkg.KittyRenderer
	CellPixels  image.Point
	// MaxRows caps the height of an inline image in terminal rows.
	MaxRows int
	// MaxCols caps the width of an inline image in terminal columns.
	// 0 disables the column cap (width-only bounded by the message
	// pane).
	MaxCols int
	SendMsg func(tea.Msg)
}

// ImageReadyMsg is dispatched by the prefetcher when an image
// attachment has finished downloading and decoding. The host panel
// uses Channel + TS to identify the affected message; Key clears the
// in-flight bit on the renderer that was tracking the fetch. ReqID
// is the debuglog correlator threaded from the enqueue site.
type ImageReadyMsg struct {
	Channel string
	TS      string
	Key     string
	ReqID   uint64
}

// ImageFailedMsg is dispatched when all auth attempts for an image
// have failed. Carries the cache key only; hosts use it to mark the
// key as permanently failed so RenderBlock won't re-spawn a fetch
// goroutine until the channel is switched. ReqID is the debuglog
// correlator threaded from the enqueue site.
type ImageFailedMsg struct {
	Key   string
	ReqID uint64
}

// ThumbSpec describes a single available thumbnail for an image.
// imgrender keeps its own copy (rather than importing messages.ThumbSpec)
// so messages can later import imgrender without creating an import
// cycle. messages converts at the call site.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}

// Hit is one inline-image hit-rect, expressed in coordinates relative
// to a single rendered block. The host panel translates these to its
// own per-entry / viewport coordinate system.
type Hit struct {
	RowStartInEntry int
	RowEndInEntry   int // exclusive
	ColStart        int
	ColEnd          int // exclusive
	FileID          string
	AttIdx          int
}

// SixelEntry holds the pre-computed sixel bytes for one inline image,
// plus the halfblock fallback used when the image is only partially
// visible (sixel cannot emit a half-image).
type SixelEntry struct {
	// Key is the stable image cache key (file ID + thumb suffix), the
	// content identity that lets the painter detect a byte-for-byte
	// different image in the same slot. Empty when no stable key exists
	// (blockkit-derived entries); the messages pane then hashes the
	// payload once at construction.
	Key      string
	Bytes    []byte
	Fallback []string
	Height   int
	Width    int
}

// computeImageTarget chooses (cols, rows) for an inline image render.
// Aspect ratio is preserved. rows is capped at ctx.MaxRows; cols is
// capped at min(availWidth, ctx.MaxCols). Returns image.Point{} when
// the attachment has no usable thumbnail or the cell metrics are zero.
//
// The largest thumb in the slice is used as the source aspect ratio
// (matching the existing messages-pane behavior).
func computeImageTarget(thumbs []ThumbSpec, ctx ImageContext, availWidth int) image.Point {
	if len(thumbs) == 0 || ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		debuglog.ImgRender("computeImageTarget: thumbs=%d cell_px=(%d,%d) → zero target",
			len(thumbs), ctx.CellPixels.X, ctx.CellPixels.Y)
		return image.Point{}
	}
	largest := thumbs[len(thumbs)-1]
	if largest.W <= 0 || largest.H <= 0 {
		debuglog.ImgRender("computeImageTarget: largest_thumb_dims=(%d,%d) → zero target",
			largest.W, largest.H)
		return image.Point{}
	}
	aspect := float64(largest.W) / float64(largest.H)
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)

	rows := ctx.MaxRows
	if rows <= 0 {
		rows = 20
	}
	maxCols := availWidth
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	cols := int(float64(rows) * aspect / cellRatio)
	if cols < 1 {
		cols = 1
	}
	clamped := false
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
		clamped = true
	}
	if rows < 1 {
		rows = 1
	}
	debuglog.ImgRender("computeImageTarget: natural=(%d,%d) avail_cols=%d MaxCols=%d MaxRows=%d cell_px=(%d,%d) target=(%d,%d) clamped_to_cols=%v",
		largest.W, largest.H, availWidth, ctx.MaxCols, ctx.MaxRows,
		ctx.CellPixels.X, ctx.CellPixels.Y, cols, rows, clamped)
	return image.Pt(cols, rows)
}

// buildPlaceholder produces a target.Y-row block with theme-surface
// background and a centered "⏳ Loading <name>..." indicator on the
// middle row. Used while image bytes are being fetched.
func buildPlaceholder(name string, target image.Point) []string {
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	pad := strings.Repeat(" ", target.X)
	emptyRow := bg.Render(pad)

	lines := make([]string, target.Y)
	for i := range lines {
		lines[i] = emptyRow
	}

	label := "⏳ Loading " + name + "..."
	labelW := lipgloss.Width(label)
	if labelW > target.X {
		// Truncate to fit. Use rune-safe slicing via a runes round-trip
		// — image labels are user-controlled file names and can contain
		// multi-byte UTF-8.
		if target.X > 1 {
			runes := []rune(label)
			// Conservatively trim to target.X-1 runes plus an ellipsis;
			// some runes are wide so the result may still be slightly
			// over, in which case we re-trim.
			for len(runes) > 0 {
				candidate := string(runes[:len(runes)-1]) + "…"
				if lipgloss.Width(candidate) <= target.X {
					label = candidate
					labelW = lipgloss.Width(label)
					break
				}
				runes = runes[:len(runes)-1]
			}
			if labelW > target.X {
				return lines
			}
		} else {
			return lines
		}
	}
	leftPad := (target.X - labelW) / 2
	rightPad := target.X - labelW - leftPad
	mid := target.Y / 2
	lines[mid] = bg.Render(strings.Repeat(" ", leftPad)) + bg.Render(label) + bg.Render(strings.Repeat(" ", rightPad))
	return lines
}

// Block is the per-attachment input to RenderBlock. Hosts construct it
// from their own attachment representation (e.g. messages.Attachment).
// Defining a local struct here avoids a circular import on the messages
// package.
type Block struct {
	Kind   string // "image" or anything else (non-image falls back to text)
	FileID string // Slack file ID; required for cache keys
	Name   string // user-visible filename (for placeholder label)
	URL    string // canonical URL for OSC 8 hyperlink in the legacy text fallback
	Thumbs []ThumbSpec
}

// BlockResult bundles RenderBlock's return values.
type BlockResult struct {
	Lines     []string
	Flushes   []func(io.Writer) error
	SixelRows map[int]SixelEntry
	Height    int
	Hit       Hit
}

// Renderer owns the per-panel inline-image state: fetch-in-flight set,
// permanent-failure set, and the active ImageContext. One instance per
// host panel (one for messages, one for thread).
type Renderer struct {
	ctx      ImageContext
	fetching map[string]struct{}
	failed   map[string]struct{}
}

// NewRenderer returns an empty Renderer. Hosts must call SetContext
// before RenderBlock can produce inline output (a zero-valued context
// falls through to the legacy text rendering, which is also the safe
// default during early app startup).
func NewRenderer() *Renderer {
	return &Renderer{
		fetching: map[string]struct{}{},
		failed:   map[string]struct{}{},
	}
}

// SetContext configures the inline-image rendering pipeline. May be
// called multiple times (e.g. when the prefetcher's tea.Program send
// fn becomes available). Resets the fetch-tracking state.
func (r *Renderer) SetContext(ctx ImageContext) {
	r.ctx = ctx
	for k := range r.fetching {
		delete(r.fetching, k)
	}
	for k := range r.failed {
		delete(r.failed, k)
	}
}

// Context returns the current ImageContext (read-only).
func (r *Renderer) Context() ImageContext { return r.ctx }

// ClearFetching removes a key from the in-flight set. Returns true if
// the key was present (i.e. this Renderer was tracking that fetch).
func (r *Renderer) ClearFetching(key string) bool {
	if _, ok := r.fetching[key]; !ok {
		return false
	}
	delete(r.fetching, key)
	return true
}

// MarkFailed clears the in-flight bit for key and adds it to the
// failed set so RenderBlock won't re-spawn a fetch goroutine. Returns
// true if the key was being tracked here.
func (r *Renderer) MarkFailed(key string) bool {
	tracked := false
	if _, ok := r.fetching[key]; ok {
		delete(r.fetching, key)
		tracked = true
	}
	r.failed[key] = struct{}{}
	return tracked
}

// ResetFailed clears the failure and in-flight sets. Hosts call this
// on channel / thread switch so the user can retry.
func (r *Renderer) ResetFailed() {
	for k := range r.failed {
		delete(r.failed, k)
	}
	for k := range r.fetching {
		delete(r.fetching, k)
	}
}

// RenderBlock returns the rendered rows + per-frame flushes + sixel
// sentinel rows + the image-hit footprint for one attachment. channel
// + ts identify the originating message for ImageReadyMsg routing
// when a fetch completes. baseRow is the absolute row index where this
// block's first row will land within the containing entry. attIdx is
// the index of this attachment within its parent message. contentColBase
// is the display column at which the message-content area begins
// within the entry's lines.
//
// Behavior matrix (matches the legacy renderAttachmentBlock):
//   - Non-image, ProtoOff, missing fetcher, missing FileID, or no
//     usable thumb -> single-line legacy "[Image|File] <url>" form.
//   - Cached bytes -> render via active protocol.
//   - Not cached -> reserved-height placeholder + async prefetch.
func (r *Renderer) RenderBlock(att Block, channel, ts string, availWidth, baseRow, attIdx, contentColBase int) BlockResult {
	// Fall through to the legacy text line for any attachment we can't
	// or shouldn't render inline.
	if att.Kind != "image" || r.ctx.Protocol == imgpkg.ProtoOff || r.ctx.Fetcher == nil {
		debuglog.ImgRender("RenderBlock: file_id=%s decision=legacy_text reason=non_image_or_off", att.FileID)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
	if att.FileID == "" {
		debuglog.ImgRender("RenderBlock: decision=legacy_text reason=no_file_id name=%q", att.Name)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}

	target := computeImageTarget(att.Thumbs, r.ctx, availWidth)
	if target.X <= 0 || target.Y <= 0 {
		debuglog.ImgRender("RenderBlock: file_id=%s decision=legacy_text reason=zero_target", att.FileID)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}

	pixelTarget := image.Pt(target.X*r.ctx.CellPixels.X, target.Y*r.ctx.CellPixels.Y)
	imgThumbs := make([]imgpkg.ThumbSpec, len(att.Thumbs))
	for i, t := range att.Thumbs {
		imgThumbs[i] = imgpkg.ThumbSpec{URL: t.URL, W: t.W, H: t.H}
	}
	url, suffix := imgpkg.PickThumb(imgThumbs, pixelTarget)
	if url == "" {
		debuglog.ImgRender("RenderBlock: file_id=%s decision=legacy_text reason=no_thumb_url", att.FileID)
		return BlockResult{Lines: []string{renderLegacyLine(att)}, Height: 1}
	}
	key := att.FileID + "-" + suffix

	hit := Hit{
		RowStartInEntry: baseRow,
		RowEndInEntry:   baseRow + target.Y,
		ColStart:        contentColBase,
		ColEnd:          contentColBase + target.X,
		FileID:          att.FileID,
		AttIdx:          attIdx,
	}

	img, cached := r.ctx.Fetcher.Cached(key, pixelTarget)
	if !cached {
		if _, failed := r.failed[key]; failed {
			debuglog.ImgRender("RenderBlock: key=%s decision=placeholder_failed", key)
			return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
		}
		if _, inFlight := r.fetching[key]; inFlight {
			debuglog.ImgRender("RenderBlock: key=%s decision=placeholder_in_flight", key)
			return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
		}
		r.fetching[key] = struct{}{}
		reqID := debuglog.NextReqID()
		debuglog.ImgFetch("enqueue: key=%s url=%s panel=msgs channel=%s ts=%s req_id=%d fetching_set_size=%d",
			key, url, channel, ts, reqID, len(r.fetching))
		ctx := r.ctx // capture for the goroutine
		go func() {
			fetchStart := time.Now()
			_, err := ctx.Fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
				Key:        key,
				URL:        url,
				Target:     pixelTarget,
				CellTarget: target,
				ReqID:      reqID,
			})
			if ctx.SendMsg == nil {
				debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=skipped (SendMsg=nil)",
					key, reqID, time.Since(fetchStart).Milliseconds())
				return
			}
			if err != nil {
				debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=failed err=%v",
					key, reqID, time.Since(fetchStart).Milliseconds(), err)
				ctx.SendMsg(ImageFailedMsg{Key: key, ReqID: reqID})
				return
			}
			debuglog.ImgFetch("dispatch: key=%s req_id=%d total_ms=%d kind=ready",
				key, reqID, time.Since(fetchStart).Milliseconds())
			ctx.SendMsg(ImageReadyMsg{Channel: channel, TS: ts, Key: key, ReqID: reqID})
		}()
		debuglog.ImgRender("RenderBlock: key=%s decision=placeholder_spawned_fetch req_id=%d", key, reqID)
		return BlockResult{Lines: buildPlaceholder(att.Name, target), Height: target.Y, Hit: hit}
	}

	// Fast path: prerendered output baked off the UI thread.
	if pr, ok := r.ctx.Fetcher.Prerendered(key, target, r.ctx.Protocol); ok {
		var fl []func(io.Writer) error
		var sxlMap map[int]SixelEntry
		if r.ctx.Protocol == imgpkg.ProtoSixel && pr.OnFlush != nil {
			var bb bytes.Buffer
			if err := pr.OnFlush(&bb); err == nil {
				sxlMap = map[int]SixelEntry{
					baseRow: {Key: key, Bytes: bb.Bytes(), Fallback: pr.Fallback, Height: target.Y, Width: target.X},
				}
			}
		} else if pr.OnFlush != nil {
			fl = []func(io.Writer) error{pr.OnFlush}
		}
		debuglog.ImgRender("RenderBlock: key=%s decision=prerendered proto=%v target=(%d,%d)",
			key, r.ctx.Protocol, target.X, target.Y)
		return BlockResult{Lines: pr.Lines, Flushes: fl, SixelRows: sxlMap, Height: target.Y, Hit: hit}
	}

	// Slow path: prerender wasn't populated. Encode on this goroutine.
	if r.ctx.Protocol == imgpkg.ProtoKitty && r.ctx.KittyRender != nil {
		ckey := "F-" + att.FileID
		r.ctx.KittyRender.SetSource(ckey, img)
		out := r.ctx.KittyRender.RenderKey(ckey, target)
		var fl []func(io.Writer) error
		if out.OnFlush != nil {
			fl = []func(io.Writer) error{out.OnFlush}
		}
		debuglog.ImgRender("RenderBlock: key=%s decision=cached_kitty_slow target=(%d,%d)",
			key, target.X, target.Y)
		return BlockResult{Lines: out.Lines, Flushes: fl, Height: target.Y, Hit: hit}
	}

	out := imgpkg.RenderImage(r.ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	var sxlMap map[int]SixelEntry
	if r.ctx.Protocol == imgpkg.ProtoSixel {
		if out.OnFlush != nil {
			var bb bytes.Buffer
			if err := out.OnFlush(&bb); err == nil {
				sxlMap = map[int]SixelEntry{
					baseRow: {Key: key, Bytes: bb.Bytes(), Fallback: out.Fallback, Height: target.Y, Width: target.X},
				}
			}
		}
	} else if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	debuglog.ImgRender("RenderBlock: key=%s decision=cached_slow proto=%v target=(%d,%d)",
		key, r.ctx.Protocol, target.X, target.Y)
	return BlockResult{Lines: out.Lines, Flushes: fl, SixelRows: sxlMap, Height: target.Y, Hit: hit}
}

// renderLegacyLine returns the single-line "[Image] <url>" or
// "[File] <url>" fallback used when inline rendering is unavailable
// for an attachment. Mirrors the existing internal/ui/messages
// renderSingleAttachment helper byte-for-byte (Bold marker style,
// underlined link style, OSC 8 wrapping the entire body), but takes
// an imgrender.Block to keep imgrender independent of the messages
// package.
func renderLegacyLine(att Block) string {
	markerStyle := lipgloss.NewStyle().Foreground(styles.TextMuted).Bold(true)
	urlStyle := lipgloss.NewStyle().Foreground(styles.Primary).Underline(true)
	marker := "[File]"
	if att.Kind == "image" {
		marker = "[Image]"
	}
	body := markerStyle.Render(marker) + " " + urlStyle.Render(att.URL)
	// OSC 8 hyperlink: ESC ] 8 ;; URL ESC \ LABEL ESC ] 8 ;; ESC \
	return "\x1b]8;;" + att.URL + "\x1b\\" + body + "\x1b]8;;\x1b\\"
}
