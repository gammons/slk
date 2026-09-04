package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	imgpng "image/png"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gammons/slk/internal/debuglog"
	"golang.org/x/image/draw"
)

// kittyDiacritics is the 297-entry table from kitty's rowcolumn-diacritics.txt.
// Index i -> rune used to encode row-or-column index i in unicode-placeholder mode.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F, 0x0346, 0x034A,
	0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357, 0x035B, 0x0363, 0x0364, 0x0365,
	0x0366, 0x0367, 0x0368, 0x0369, 0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F,
	0x0483, 0x0484, 0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1, 0x05A8, 0x05A9,
	0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611, 0x0612, 0x0613, 0x0614, 0x0615,
	0x0616, 0x0617, 0x0657, 0x0658, 0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6,
	0x06D7, 0x06D8, 0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733, 0x0735, 0x0736,
	0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743, 0x0745, 0x0747, 0x0749, 0x074A,
	0x07EB, 0x07EC, 0x07ED, 0x07EE, 0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817,
	0x0818, 0x0819, 0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C, 0x082D, 0x0951,
	0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87, 0x135D, 0x135E, 0x135F, 0x17DD,
	0x193A, 0x1A17, 0x1A75, 0x1A76, 0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C,
	0x1B6B, 0x1B6D, 0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4, 0x1DC5, 0x1DC6,
	0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1, 0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5,
	0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9, 0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF,
	0x1DE0, 0x1DE1, 0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7, 0x20E9, 0x20F0,
	0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2, 0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6,
	0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA, 0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0,
	0x2DF1, 0x2DF2, 0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D, 0xA6F0, 0xA6F1,
	0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5, 0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9,
	0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED, 0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2,
	0xAAB3, 0xAAB7, 0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFB1E, 0xFE20, 0xFE21, 0xFE22,
	0xFE23, 0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187, 0x1D188,
	0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242, 0x1D243, 0x1D244,
}

// PlaceholderRune is the kitty graphics protocol's unicode placeholder
// (U+10EEEE). Each cell whose content is this rune carries an image
// reference in its SGR foreground color (24-bit RGB encoding the image
// ID); any downstream pass that mutates cell colors must leave these
// cells alone or the encoded ID will change and the terminal will
// resolve a different (or non-existent) image. Exported so the
// overlay/dim pipeline can detect these cells without taking a heavy
// dependency on internal/image's renderer.
const PlaceholderRune = '\U0010EEEE'

func inTmux() bool {
	return os.Getenv("TMUX") != ""
}

func wrapForTmux(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

func writeKittySequence(w io.Writer, seq string) error {
	if inTmux() {
		seq = wrapForTmux(seq)
	}
	_, err := io.WriteString(w, seq)
	return err
}

// KittyRenderer encodes images via the kitty graphics protocol with
// unicode-placeholder placement.
type KittyRenderer struct {
	registry *Registry

	mu      sync.Mutex
	sources map[string]image.Image
	// placeholders memoizes buildPlaceholderLines results so warm
	// RenderKey calls (fresh=false: image already uploaded) skip the
	// cells.Y * cells.X strings.Builder cost on every render.
	//
	// The output of buildPlaceholderLines is fully deterministic in
	// (id, target): each cell encodes the kitty image id in its SGR
	// foreground color plus row/col diacritics, none of which depend
	// on any state outside id/cells. Therefore the cached slice can
	// be returned by reference safely -- callers never mutate the
	// returned lines (verified by lipgloss / viewport / messagepane
	// flatten paths).
	//
	// Cache is never evicted: even if the registry recycles an id
	// (a different image binds to id N later), the cached lines
	// still encode "color N" which the terminal resolves to whichever
	// image currently owns id N -- producing correct output by
	// indirection. Memory cost is O(images_seen * avg_lines_per_image)
	// per session, ~12KB per typical 60x20 cached image.
	placeholders map[placeholderKey][]string

	// payloads memoizes the base64-encoded PNG payload per (id, target).
	//
	// Why this exists: Registry.Lookup keeps returning fresh=true for
	// any image whose OnFlush has never fired -- and OnFlush only fires
	// for messages whose viewEntry currently sits inside the visible
	// viewport (see internal/ui/messages/model.go:2748). For an
	// image-heavy channel with 21 images but only ~5 visible at a
	// time, 16 of the 21 images would re-pay bilinear-resize +
	// PNG-encode + base64 on EVERY buildCache because their
	// MarkUploaded was never called. That cost (~22 ms/image at
	// typical Slack thumbnail sizes) is the dominant remaining channel-
	// switch latency observed in slk-debug.log perf traces.
	//
	// The cache is keyed by (id, target). The source image bound to
	// `key` via SetSource is content-addressable upstream (BK-<sha1>
	// from URL, or F-<fileID>) so the same key always binds the same
	// pixel data -- the cached payload is safe to reuse across
	// arbitrarily many RenderKey calls within a session. Registry IDs
	// are monotonic and never reused, so (id, target) → payload is
	// stable for the session.
	//
	// Memory cost: a 480x256 RGBA → PNG → base64 typically encodes to
	// ~10-30KB. For ~1000 distinct images per long session, ~20MB.
	// Acceptable; if it ever becomes a concern, an LRU eviction can
	// be added without changing the contract.
	payloads map[placeholderKey]string
}

// placeholderKey scopes the buildPlaceholderLines memo by the inputs
// the function actually consumes: the kitty image id (encoded into
// every cell's SGR foreground) and the cell dimensions. Reused as
// the key for the payload memo -- both caches scope by the same
// (id, target) tuple.
type placeholderKey struct {
	id     uint32
	cellsX int
	cellsY int
}

// NewKittyRenderer constructs a kitty renderer backed by the given registry.
func NewKittyRenderer(reg *Registry) *KittyRenderer {
	return &KittyRenderer{
		registry:     reg,
		sources:      map[string]image.Image{},
		placeholders: map[placeholderKey][]string{},
		payloads:     map[placeholderKey]string{},
	}
}

// Render dispatches by content; convenience path for one-off renders.
// For repeated renders of the same image, prefer SetSource + RenderKey.
func (k *KittyRenderer) Render(img image.Image, target image.Point) Render {
	key := fmt.Sprintf("anon-%v-%dx%d", img.Bounds(), target.X, target.Y)
	k.SetSource(key, img)
	return k.RenderKey(key, target)
}

// SetSource binds a stable cache key to an image. Subsequent RenderKey calls
// with the same key reuse the registered image for upload bytes.
func (k *KittyRenderer) SetSource(key string, img image.Image) {
	k.mu.Lock()
	k.sources[key] = img
	k.mu.Unlock()
}

// RenderKey produces a Render for the given (key, target).
// On the first call for a (key, target) pair, OnFlush is set to upload the
// image bytes; subsequent calls return OnFlush=nil.
func (k *KittyRenderer) RenderKey(key string, target image.Point) Render {
	k.mu.Lock()
	src, ok := k.sources[key]
	k.mu.Unlock()
	if !ok || target.X <= 0 || target.Y <= 0 {
		reason := "no_source"
		if ok {
			reason = "zero_target"
		}
		debuglog.ImgRender("kitty.RenderKey: key=%s target=(%d,%d) abort reason=%s",
			key, target.X, target.Y, reason)
		return Render{Cells: target}
	}

	id, fresh := k.registry.Lookup(key, target)

	// Fast path: cached placeholder lines for (id, target). The lines
	// are deterministic in those inputs so the cache result is
	// byte-identical to a freshly-computed slice -- guarded by
	// TestKitty_RenderKeyWarmMatchesCold.
	phKey := placeholderKey{id: id, cellsX: target.X, cellsY: target.Y}
	k.mu.Lock()
	lines, phHit := k.placeholders[phKey]
	k.mu.Unlock()
	if !phHit {
		lines = buildPlaceholderLines(id, target)
		k.mu.Lock()
		k.placeholders[phKey] = lines
		k.mu.Unlock()
	}
	debuglog.ImgRender("kitty.RenderKey: key=%s target=(%d,%d) image_id=%d fresh=%v lines=%d ph_cache=%v",
		key, target.X, target.Y, id, fresh, len(lines), phHit)

	r := Render{
		Cells:    target,
		Lines:    lines,
		Fallback: lines,
		ID:       id,
	}
	if fresh {
		// Off-screen viewEntries never get their OnFlush fired, so
		// Registry.Lookup keeps returning fresh=true for them on every
		// buildCache. Without the payload cache, this re-pays bilinear
		// resize + PNG encode + base64 -- the dominant channel-switch
		// cost for image-heavy channels. The cache short-circuits the
		// expensive work; the upload bytes are STILL emitted via
		// OnFlush so the terminal eventually receives them (idempotent
		// from kitty's perspective: re-transmitting the same image id
		// just re-asserts the binding).
		payloadKey := phKey
		k.mu.Lock()
		payload, payloadHit := k.payloads[payloadKey]
		k.mu.Unlock()
		if !payloadHit {
			// Encode against the terminal's real cell size, not a
			// hardcoded 8x16. The placement is by cell either way, so
			// this does not move the image — it decides how many
			// pixels the terminal has to work with when it scales the
			// image across those cells. See measuredCellPixels.
			cw, ch := measuredCellPixels()
			pxW := target.X * cw
			pxH := target.Y * ch
			resized := image.NewRGBA(image.Rect(0, 0, pxW, pxH))
			draw.BiLinear.Scale(resized, resized.Bounds(), src, src.Bounds(), draw.Over, nil)
			var pngBuf bytes.Buffer
			if err := imgpng.Encode(&pngBuf, resized); err == nil {
				payload = base64.StdEncoding.EncodeToString(pngBuf.Bytes())
				k.mu.Lock()
				k.payloads[payloadKey] = payload
				k.mu.Unlock()
			} else {
				// Encode failed; without a payload we can't build an
				// OnFlush. Return r without one and let the caller
				// fall back to its placeholder lines (still drawn,
				// just never resolved by the terminal).
				return r
			}
		}
		imgID := id
		cellsCols := target.X
		cellsRows := target.Y
		reg := k.registry
		// fired guards against per-closure double-emission (e.g. the
		// same viewEntry being flushed twice in one frame). The
		// registry's MarkUploaded guards against double-emission
		// across DIFFERENT closures for the same (key, target) —
		// without that, a cache rebuild that discards an unfired
		// closure (e.g. SetMessages on the messages pane) would
		// leave the registry thinking the upload had landed when in
		// fact no bytes were ever sent.
		var fired atomic.Bool
		r.OnFlush = func(w io.Writer) error {
			if !fired.CompareAndSwap(false, true) {
				return nil
			}
			debuglog.ImgRender("kitty.OnFlush: image_id=%d cells=(%d,%d) payload_len=%d payload_cache=%v",
				imgID, cellsCols, cellsRows, len(payload), payloadHit)
			if err := emitKittyUpload(w, imgID, payload, cellsCols, cellsRows); err != nil {
				return err
			}
			reg.MarkUploaded(imgID)
			return nil
		}
	}
	return r
}

// emitKittyUpload writes the kitty graphics protocol APC sequence to
// transmit a PNG image and create a virtual placement of size cols×rows
// for unicode-placeholder rendering.
//
// The first chunk uses `a=T` ("transmit AND display") with `U=1`
// (unicode-placeholder mode), `c=<cols>` and `r=<rows>` to define the
// virtual placement's cell footprint. `q=2` suppresses kitty's reply.
// Continuation chunks omit those keys and only carry `m=<more>`.
//
// Per kitty protocol, payloads larger than 4096 base64 bytes must be
// chunked. The final chunk has m=0 to mark the end.
//
// Reference: https://sw.kovidgoyal.net/kitty/graphics-protocol/#unicode-placeholders
func emitKittyUpload(w io.Writer, id uint32, payload string, cols, rows int) error {
	const chunk = 4096
	for i := 0; i < len(payload); i += chunk {
		end := i + chunk
		more := 1
		if end >= len(payload) {
			end = len(payload)
			more = 0
		}
		var hdr string
		if i == 0 {
			hdr = fmt.Sprintf("a=T,f=100,t=d,i=%d,U=1,c=%d,r=%d,q=2,m=%d", id, cols, rows, more)
		} else {
			hdr = fmt.Sprintf("m=%d", more)
		}
		seq := fmt.Sprintf("\x1b_G%s;%s\x1b\\", hdr, payload[i:end])
		if err := writeKittySequence(w, seq); err != nil {
			return err
		}
	}
	return nil
}

func buildPlaceholderLines(id uint32, cells image.Point) []string {
	// Per kitty spec: image ID is encoded in the foreground color as a
	// 24-bit number. In truecolor SGR \e[38;2;R;G;Bm the natural
	// interpretation is (R << 16) | (G << 8) | B, so R = byte 2 (high),
	// G = byte 1, B = byte 0 (low). Verified against the spec's worked
	// example: ID 42 in 256-color mode is \e[38;5;42m, which means the
	// truecolor equivalent is \e[38;2;0;0;42m (low byte → B), NOT
	// \e[38;2;42;0;0m. The high byte (byte 3) of a >24-bit ID would
	// require the optional third diacritic; we don't need that since
	// our IDs are well under 2^24.
	r := byte((id >> 16) & 0xFF)
	g := byte((id >> 8) & 0xFF)
	b := byte(id & 0xFF)
	sgr := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
	reset := "\x1b[39m"

	lines := make([]string, cells.Y)
	for row := 0; row < cells.Y; row++ {
		var sb strings.Builder
		sb.WriteString(sgr)
		rowDia := diacritic(row)
		for col := 0; col < cells.X; col++ {
			sb.WriteRune(PlaceholderRune)
			sb.WriteRune(rowDia)
			sb.WriteRune(diacritic(col))
		}
		sb.WriteString(reset)
		lines[row] = sb.String()
	}
	return lines
}

func diacritic(i int) rune {
	if i < 0 || i >= len(kittyDiacritics) {
		return kittyDiacritics[0]
	}
	return kittyDiacritics[i]
}
