package image

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/gammons/slk/internal/debuglog"
)

// SixelPlacement is one image painted at an absolute screen position.
//
// Row and Col are 1-based absolute terminal coordinates (the coordinate
// system of CUP, ESC[row;colH). Rows and Cols are the cell footprint,
// used to erase the region when the placement goes away.
//
// Key identifies the image content at this position. Two placements with
// equal Key, Row, Col, Rows and Cols are considered identical and the
// painter will not re-emit — that is what stops the per-frame flood.
type SixelPlacement struct {
	Key        string
	Row, Col   int
	Rows, Cols int
	Bytes      []byte

	// Guard fingerprints the frame text underneath this placement.
	//
	// Painting out-of-band assumes the cells beneath an image stay
	// untouched, which holds only while their content is identical frame
	// to frame. Anything that restyles those rows — moving focus recolors
	// the pane border, selection tints a row — changes the line, so
	// bubbletea rewrites it and the rewrite paints over the pixels. The
	// placement itself has not changed, so without this the painter would
	// consider the image current and never repaint it: the image
	// disappears permanently.
	Guard string
}

// PlacementKey derives the content identity for a placement. identity is
// the stable cache key (file ID + thumb suffix) when one exists; without
// one the freshly encoded payload is hashed once here, at construction
// time — never during painter comparison. The cell footprint is always
// appended, so a key collision is still broken by a geometry change.
//
// The encoded byte LENGTH is deliberately not part of the identity: two
// different images of the same size and palette depth encode to the same
// length, so a length-based key would treat a slot swap as unchanged and
// never repaint. Shared by the messages pane and the preview overlay.
func PlacementKey(identity string, payload []byte, cols, rows int) string {
	if identity == "" {
		sum := sha256.Sum256(payload)
		identity = hex.EncodeToString(sum[:16])
	}
	return fmt.Sprintf("%s/%dx%d", identity, cols, rows)
}

// sameSlot reports whether p occupies the same screen slot with the same
// content as q. Bytes are deliberately not compared: Key is the content
// identity (a cache key), and the byte slices are large.
func (p SixelPlacement) sameSlot(q SixelPlacement) bool {
	return p.Key == q.Key && p.Row == q.Row && p.Col == q.Col &&
		p.Rows == q.Rows && p.Cols == q.Cols && p.Guard == q.Guard
}

// sixelSlot is the screen position that keys the painted set. A plain
// struct is used instead of a "row,col" string so frame reconciliation
// never allocates a formatted key per placement.
type sixelSlot struct {
	row int
	col int
}

// SixelPainter tracks which sixel images are currently painted on the
// terminal and reconciles that against what each frame wants.
//
// Sixel has no cell-aware placement and no addressable image identity:
// the terminal paints pixels at the cursor and forgets about them. Unlike
// kitty — where a unicode placeholder in the text stream lets the terminal
// re-render the image itself — nothing repaints or removes a sixel image
// for us. So slk must own the screen region: paint when it appears, erase
// when it leaves, and otherwise leave it alone.
//
// The painter is transactional: Plan computes the operation bytes and the
// resulting state without touching the painter; Commit installs that
// state. A caller that fails to write the plan's bytes to the terminal
// simply skips Commit, so a failed write never leaves the painter
// believing an image is on screen that is not.
//
// Not safe for concurrent use; call from the serialized output path only.
type SixelPainter struct {
	painted map[sixelSlot]SixelPlacement
}

// SixelPlan is the output of Plan: the exact bytes to append to the
// terminal write for the reconciliation, the change counters, and the
// state to install when the write succeeds. next is nil when nothing
// changed, which makes Commit a no-op.
//
// Erase and Bytes are separate because they land on opposite sides of
// the text diff: an erase clears old sixel pixels, and ECH also blanks
// the cells it touches. If erases ran AFTER the text diff (like paints
// must), they would wipe the freshly written text inside a vacated
// image region when the image moves on scroll. FrameOutput therefore
// emits Erase before the text diff and Bytes after it.
type SixelPlan struct {
	Erase   []byte
	Bytes   []byte
	Painted int
	Erased  int
	next    map[sixelSlot]SixelPlacement
}

// NewSixelPainter returns an empty painter. The terminal write side is
// owned by the caller (FrameOutput), not the painter.
func NewSixelPainter() *SixelPainter {
	return &SixelPainter{painted: make(map[sixelSlot]SixelPlacement)}
}

// Plan computes the erase/paint operations needed to display want,
// without mutating p. eraseSGR is the SGR sequence emitted before each
// ECH so vacated cells take the pane's background rather than the
// terminal default; empty disables it. force=true erases and repaints
// every valid wanted placement even when key, geometry, and guard are
// unchanged — used for renderer full redraws such as terminal resize.
//
// Returned plan bytes are owned by the plan and safe to append to a
// write; Commit installs the planned state.
func (p *SixelPainter) Plan(want []SixelPlacement, eraseSGR string, force bool) SixelPlan {
	wanted := make(map[sixelSlot]SixelPlacement, len(want))
	for _, pl := range want {
		if pl.Rows <= 0 || pl.Cols <= 0 || pl.Row < 1 || pl.Col < 1 {
			continue // nonsensical placement; never touch the screen for it
		}
		wanted[sixelSlot{row: pl.Row, col: pl.Col}] = pl
	}

	next := make(map[sixelSlot]SixelPlacement, len(wanted))
	var eraseBuf bytes.Buffer
	var buf bytes.Buffer
	painted, erased := 0, 0

	// Erase slots that are gone or whose content/footprint changed (or
	// all of them under force). Sorted for deterministic output (maps
	// randomize iteration order, which would make tests flaky and diffs
	// unreadable).
	//
	// Erase bytes are collected separately from paint bytes: FrameOutput
	// emits them BEFORE the text diff so they never blank freshly
	// written text, while paints must stay after the text diff.
	for _, k := range sortedSlots(p.painted) {
		old := p.painted[k]
		if cur, ok := wanted[k]; ok && cur.sameSlot(old) && !force {
			next[k] = old
			continue
		}
		writeErase(&eraseBuf, old, eraseSGR)
		erased++
	}

	// Paint anything not already on screen.
	for _, k := range sortedSlots(wanted) {
		pl := wanted[k]
		if cur, ok := p.painted[k]; ok && cur.sameSlot(pl) && !force {
			next[k] = cur
			continue
		}
		if len(pl.Bytes) == 0 {
			continue
		}
		writeSixelAt(&buf, pl)
		next[k] = pl
		painted++
	}

	if painted > 0 || erased > 0 {
		debuglog.ImgRender("sixel painter: painted=%d erased=%d live=%d bytes=%d",
			painted, erased, len(next), buf.Len())
	}
	return SixelPlan{Erase: eraseBuf.Bytes(), Bytes: buf.Bytes(), Painted: painted, Erased: erased, next: next}
}

// Commit installs the state produced by Plan. Call it only after the
// plan's bytes were written to the terminal in full; a short or failed
// write must leave the painter untouched.
func (p *SixelPainter) Commit(plan SixelPlan) {
	if plan.next != nil {
		p.painted = plan.next
	}
}

// Live returns the number of placements currently painted. For tests and
// diagnostics.
func (p *SixelPainter) Live() int { return len(p.painted) }

// writeSixelAt emits one image at its absolute position, leaving the
// cursor where it found it. DECSC/DECRC (ESC 7 / ESC 8) rather than
// CSI s/u: the DEC pair is what terminals with sixel support implement.
func writeSixelAt(w io.Writer, pl SixelPlacement) {
	fmt.Fprint(w, "\x1b7")                        // save cursor
	fmt.Fprintf(w, "\x1b[%d;%dH", pl.Row, pl.Col) // absolute position
	w.Write(pl.Bytes)
	fmt.Fprint(w, "\x1b8") // restore cursor
}

// writeErase clears the cells a placement occupied, one row at a time
// with ECH (CSI n X). Erasing by cell is what removes sixel pixels;
// simply not re-emitting leaves them on screen forever, which is the bug
// this whole type exists to fix.
//
// eraseSGR is applied first and reset afterwards: ECH clears to the
// current background, so without it the cleared region shows the
// terminal default instead of the pane colour.
func writeErase(w io.Writer, pl SixelPlacement, eraseSGR string) {
	fmt.Fprint(w, "\x1b7")
	if eraseSGR != "" {
		fmt.Fprint(w, eraseSGR)
	}
	for r := 0; r < pl.Rows; r++ {
		fmt.Fprintf(w, "\x1b[%d;%dH", pl.Row+r, pl.Col)
		fmt.Fprintf(w, "\x1b[%dX", pl.Cols)
	}
	if eraseSGR != "" {
		fmt.Fprint(w, "\x1b[0m")
	}
	fmt.Fprint(w, "\x1b8")
}

// sortedSlots returns the painter's slot keys in deterministic (row,
// col) order.
func sortedSlots(m map[sixelSlot]SixelPlacement) []sixelSlot {
	out := make([]sixelSlot, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].row != out[j].row {
			return out[i].row < out[j].row
		}
		return out[i].col < out[j].col
	})
	return out
}
