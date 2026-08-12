package image

import (
	"bytes"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/gammons/slk/internal/debuglog"
)

// frameTitleMarker is the private suffix slk appends to Bubble Tea's
// window title to carry the exact frame ID through the renderer. The US
// byte (0x1f) is chosen because no terminal displays it and the real
// window title never contains it; FrameOutput strips the suffix before
// any byte reaches the terminal.
const frameTitleMarker = "\x1fslk-sixel-frame="

// SixelFrame is one published snapshot of the placements the App's
// View() produced for a single screen. The frame store correlates it
// with the Bubble Tea frame that actually flushes by embedding its ID
// in the view's window title.
type SixelFrame struct {
	// Placements is the immutable set of absolute placements the frame
	// wants displayed. The store clones the slice header, never the
	// payload bytes.
	Placements []SixelPlacement
	// EraseSGR is the background sequence to set before each ECH so
	// vacated cells take the pane's theme background.
	EraseSGR string
	// Force repaints every valid wanted placement even when unchanged
	// (renderer full redraws such as terminal resize).
	Force bool
}

// SixelFrameStore publishes View-time placement snapshots and hands
// them back to FrameOutput in monotonic flush order. Publish clones
// only the placement slice; the large payload bytes stay shared by
// reference.
type SixelFrameStore struct {
	mu     sync.Mutex
	nextID uint64
	frames map[uint64]SixelFrame
}

// NewSixelFrameStore returns an empty store with IDs starting at 1.
func NewSixelFrameStore() *SixelFrameStore {
	return &SixelFrameStore{frames: make(map[uint64]SixelFrame)}
}

// Publish stores frame and returns its monotonic ID. The placement
// slice is cloned so the caller can reuse its buffer; payload bytes are
// not copied.
func (s *SixelFrameStore) Publish(frame SixelFrame) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	frame.Placements = slices.Clone(frame.Placements)
	s.frames[s.nextID] = frame
	return s.nextID
}

// Take returns the exact frame with id and prunes every pending frame
// with an ID <= id. Bubble Tea flushes views in monotonic order, so a
// skipped intermediate view will never flush later and its frame can be
// discarded. There is no latest-fallback: a missing id yields ok=false.
func (s *SixelFrameStore) Take(id uint64) (SixelFrame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	frame, ok := s.frames[id]
	for k := range s.frames {
		if k <= id {
			delete(s.frames, k)
		}
	}
	return frame, ok
}

// FrameTitle encodes id into the Bubble Tea window title so the flushed
// bytes identify the exact frame, using a private marker that the real
// title never contains.
func FrameTitle(realTitle string, id uint64) string {
	return realTitle + frameTitleMarker + strconv.FormatUint(id, 10)
}

// parseFrameTitle splits a FrameTitle back into the real title and the
// frame ID. Returns ok=false when the title carries no marker.
func parseFrameTitle(title string) (real string, id uint64, ok bool) {
	idx := strings.Index(title, frameTitleMarker)
	if idx < 0 {
		return "", 0, false
	}
	real = title[:idx]
	n, err := strconv.ParseUint(title[idx+len(frameTitleMarker):], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return real, n, true
}

// FrameOutput is the serialized writer that sits between Bubble Tea's
// renderer and the terminal. One mutex serializes every renderer frame
// and every side-channel write (kitty uploads), so byte streams never
// interleave.
//
// For a marked frame it:
//   - extracts the frame ID from the internal title suffix and removes
//     the suffix before any byte reaches the terminal;
//   - forwards the real OSC 2 title only when it actually changed;
//   - takes the exact frame from the store and asks the painter for a
//     non-mutating reconciliation plan;
//   - appends the plan after the text diff (or immediately before a
//     final synchronized-output reset CSI ?2026l);
//   - writes sanitized text + plan in one serialized call;
//   - treats a short write as io.ErrShortWrite and commits painter and
//     title state only after a full successful write.
//
// Writes without a valid slk frame marker pass through unchanged and
// do not mutate painter state.
type FrameOutput struct {
	mu        sync.Mutex
	out       io.Writer
	frames    *SixelFrameStore
	painter   *SixelPainter
	lastTitle string
	haveTitle bool
}

// sideChannelWriter is the SideChannel's writer: it shares FrameOutput's
// mutex so out-of-band writes (kitty APC uploads) cannot interleave with
// renderer frames.
type sideChannelWriter struct{ parent *FrameOutput }

// NewFrameOutput wraps w. frames feeds the exact-frame lookups; the
// painter is owned here and only commits after a full successful write.
func NewFrameOutput(w io.Writer, frames *SixelFrameStore) *FrameOutput {
	return &FrameOutput{out: w, frames: frames, painter: NewSixelPainter()}
}

// SideChannel returns the serialized side-channel writer for
// out-of-band protocol writes (kitty uploads). It shares the same mutex
// as renderer frames.
func (w *FrameOutput) SideChannel() io.Writer {
	return sideChannelWriter{parent: w}
}

// Fd, Read, and Close delegate to the wrapped writer so FrameOutput
// satisfies term.File. Bubble Tea and colorprofile type-assert the
// program output to term.File to detect the TTY and query the terminal
// size; without these methods the renderer initializes at 0x0 and draws
// a blank screen. The terminal never reads from or closes the output
// during normal operation, so the delegates are only exercised through
// that interface check.
func (w *FrameOutput) Fd() uintptr {
	if f, ok := w.out.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

func (w *FrameOutput) Read(p []byte) (int, error) {
	if r, ok := w.out.(io.Reader); ok {
		return r.Read(p)
	}
	return 0, io.EOF
}

func (w *FrameOutput) Close() error {
	if c, ok := w.out.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (s sideChannelWriter) Write(p []byte) (int, error) {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	return s.parent.out.Write(p)
}

// Write implements io.Writer for a renderer flush. It returns len(p) on
// success (the input length, not the expanded output length).
func (w *FrameOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	sanitized, realTitle, id, marked := sanitizeFrameTitle(p)
	var plan SixelPlan
	if marked {
		if frame, ok := w.frames.Take(id); ok {
			plan = w.painter.Plan(frame.Placements, frame.EraseSGR, frame.Force)
		} else {
			debuglog.ImgRender("sixel frame: missing id=%d", id)
		}
	}

	// Forward the real title only when it changed; otherwise the marked
	// OSC 2 was already dropped by the sanitizer and stays dropped.
	var title []byte
	if marked && (!w.haveTitle || realTitle != w.lastTitle) {
		title = osc2(realTitle)
	}

	// Erase bytes land BEFORE the text diff and paint bytes AFTER it:
	// ECH also blanks the cells it clears, so erases must run before
	// the diff rewrites a vacated image region with scrolled-in text,
	// while paints must overlay the text that was just written.
	withPaint := insertBeforeSyncReset(sanitized, plan.Bytes)
	withErase := insertEraseAfterSyncStart(withPaint, plan.Erase)
	combined := append(title, withErase...)
	n, err := w.out.Write(combined)
	if err != nil {
		return 0, err
	}
	if n != len(combined) {
		return 0, io.ErrShortWrite
	}
	if marked && plan.next != nil {
		w.painter.Commit(plan)
	}
	if marked {
		w.lastTitle = realTitle
		w.haveTitle = true
	}
	return len(p), nil
}

// osc2 renders an OSC 2 window-title sequence terminated by BEL,
// matching Bubble Tea's own title emission.
func osc2(title string) []byte {
	return []byte("\x1b]2;" + title + "\a")
}

// sanitizeFrameTitle scans a renderer flush for OSC 2 sequences.
//
//   - A marked OSC 2 (title carrying the internal frame suffix) is
//     extracted and dropped from the output; the real title and frame
//     ID are returned. FrameOutput re-emits the real title only when it
//     changed.
//   - A plain OSC 2 is preserved byte-for-byte.
//   - All other bytes (including unrelated OSC sequences) are preserved
//     byte-for-byte.
func sanitizeFrameTitle(p []byte) (sanitized []byte, realTitle string, id uint64, marked bool) {
	sanitized = make([]byte, 0, len(p))
	i := 0
	for i < len(p) {
		if p[i] != 0x1b || i+1 >= len(p) || p[i+1] != ']' {
			sanitized = append(sanitized, p[i])
			i++
			continue
		}
		// OSC sequence: ESC ] ... terminated by BEL (0x07) or ST (ESC \).
		j := i + 2
		end := -1
		for j < len(p) {
			if p[j] == 0x07 {
				end = j + 1
				break
			}
			if p[j] == 0x1b && j+1 < len(p) && p[j+1] == '\\' {
				end = j + 2
				break
			}
			j++
		}
		if end < 0 {
			// Unterminated OSC; preserve the remainder verbatim.
			sanitized = append(sanitized, p[i:]...)
			break
		}
		body := p[i+2 : j]
		if len(body) >= 2 && body[0] == '2' && body[1] == ';' {
			if real, idv, ok := parseFrameTitle(string(body[2:])); ok {
				// Marked frame: drop the OSC entirely; the caller
				// re-emits the real title on change.
				marked = true
				realTitle = real
				id = idv
			} else {
				// Plain title: preserve unchanged.
				sanitized = append(sanitized, p[i:end]...)
			}
		} else {
			// Unrelated OSC sequence: preserve byte-for-byte.
			sanitized = append(sanitized, p[i:end]...)
		}
		i = end
	}
	return sanitized, realTitle, id, marked
}

// insertBeforeSyncReset appends plan to the text diff. When the diff
// ends with the synchronized-output reset (CSI ?2026l), the plan is
// inserted immediately before that reset so sixel operations run inside
// the synchronized output window, not after it.
func insertBeforeSyncReset(sanitized, plan []byte) []byte {
	if len(plan) == 0 {
		return sanitized
	}
	idx := bytes.LastIndex(sanitized, []byte("\x1b[?2026l"))
	if idx < 0 {
		out := make([]byte, 0, len(sanitized)+len(plan))
		out = append(out, sanitized...)
		out = append(out, plan...)
		return out
	}
	out := make([]byte, 0, len(sanitized)+len(plan))
	out = append(out, sanitized[:idx]...)
	out = append(out, plan...)
	out = append(out, sanitized[idx:]...)
	return out
}

// insertEraseAfterSyncStart inserts the erase operations immediately
// after the synchronized-output start (CSI ?2026h), so they run before
// the text diff that follows and stay inside the sync window. Without
// a sync start (a cursor-only flush), the erases are prepended.
func insertEraseAfterSyncStart(sanitized, erase []byte) []byte {
	if len(erase) == 0 {
		return sanitized
	}
	const syncStart = "\x1b[?2026h"
	idx := bytes.Index(sanitized, []byte(syncStart))
	if idx < 0 {
		out := make([]byte, 0, len(sanitized)+len(erase))
		out = append(out, erase...)
		out = append(out, sanitized...)
		return out
	}
	after := idx + len(syncStart)
	out := make([]byte, 0, len(sanitized)+len(erase))
	out = append(out, sanitized[:after]...)
	out = append(out, erase...)
	out = append(out, sanitized[after:]...)
	return out
}
