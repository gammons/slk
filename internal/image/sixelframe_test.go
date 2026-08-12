package image

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/term"
)

func oscTitle(title string) []byte {
	return []byte("\x1b]2;" + title + "\a")
}

func markedFlush(real string, id uint64, body string) []byte {
	return append(oscTitle(FrameTitle(real, id)), []byte(body)...)
}

// TestSixelFrameStore_TakeExactFrameAndPruneSkipped pins the monotonic
// flush contract: flushing a later frame must prune every earlier frame,
// because skipped intermediate views will never flush later.
func TestSixelFrameStore_TakeExactFrameAndPruneSkipped(t *testing.T) {
	s := NewSixelFrameStore()
	id1 := s.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 1, 1, 1, 1)}})
	id2 := s.Publish(SixelFrame{Placements: []SixelPlacement{pl("b", 2, 2, 1, 1)}})

	got, ok := s.Take(id2)
	if !ok || len(got.Placements) != 1 || got.Placements[0].Key != "b" {
		t.Fatalf("Take(%d) = %+v, %v", id2, got, ok)
	}
	if _, ok := s.Take(id1); ok {
		t.Fatal("skipped older frame survived pruning")
	}
}

// Publish must clone the placement slice so the caller can reuse its
// buffer, while the large payload byte slices stay shared by reference.
func TestSixelFrameStore_PublishClonesPlacementSlice(t *testing.T) {
	s := NewSixelFrameStore()
	placements := []SixelPlacement{pl("a", 1, 1, 1, 1)}
	id := s.Publish(SixelFrame{Placements: placements})
	placements[0].Key = "mutated"
	got, _ := s.Take(id)
	if got.Placements[0].Key != "a" {
		t.Fatalf("published frame aliased caller slice: %+v", got.Placements[0])
	}
	// Payload backing data must remain shared (no deep copy of Bytes).
	if &got.Placements[0].Bytes[0] != &placements[0].Bytes[0] {
		t.Fatal("publish deep-copied the payload bytes; must keep them by reference")
	}
}

// TestFrameOutput_WritesTextBeforeExactFrameSixel proves the ordering
// contract: the text diff for the exact flushed frame precedes the sixel
// DCS payload, and a frame that was never flushed contributes nothing.
func TestFrameOutput_WritesTextBeforeExactFrameSixel(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)

	// Publish frame A and frame B, then flush only the marker for B —
	// as if the renderer skipped A.
	frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("A", 2, 3, 1, 1)}})
	idB := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("B", 4, 5, 1, 1)}})

	n, err := out.Write(markedFlush("real-title", idB, "body-text"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := raw.String()
	if n != len(markedFlush("real-title", idB, "body-text")) {
		t.Fatalf("Write returned %d, want input length %d", n, len(markedFlush("real-title", idB, "body-text")))
	}
	textAt := strings.Index(got, "body-text")
	paintAt := strings.Index(got, "\x1bPqB\x1b\\")
	if textAt < 0 || paintAt < 0 {
		t.Fatalf("output missing text or DCS; got %q", got)
	}
	if textAt > paintAt {
		t.Fatalf("text must precede the sixel DCS for the same frame: %q", got)
	}
	if strings.Contains(got, "\x1bPqA\x1b\\") {
		t.Fatalf("skipped frame A leaked into output: %q", got)
	}
}

// The internal marker must never reach the terminal or a captured
// underlying writer.
func TestFrameOutput_StripsInternalMarker(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)
	id := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 2, 2, 1, 1)}})

	if _, err := out.Write(markedFlush("slk", id, "text")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if strings.Contains(raw.String(), "slk-sixel-frame=") {
		t.Fatalf("internal marker leaked to the underlying writer: %q", raw.String())
	}
	if strings.Contains(raw.String(), "\x1f") {
		t.Fatalf("US separator leaked to the underlying writer: %q", raw.String())
	}
}

// A real title that does not change must not be re-emitted; only the
// first flush forwards it, and it is never suffixed.
func TestFrameOutput_SuppressesDuplicateRealTitle(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)

	id1 := frames.Publish(SixelFrame{})
	if _, err := out.Write(markedFlush("slk", id1, "text-1")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	id2 := frames.Publish(SixelFrame{})
	if _, err := out.Write(markedFlush("slk", id2, "text-2")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}

	if got := strings.Count(raw.String(), "\x1b]2;slk\a"); got != 1 {
		t.Fatalf("title forwarded %d times, want 1; raw %q", got, raw.String())
	}
	if strings.Contains(raw.String(), "slk-sixel-frame=") {
		t.Fatalf("suffix leaked: %q", raw.String())
	}
}

// A changed real title is forwarded once.
func TestFrameOutput_ForwardsChangedRealTitle(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)

	id1 := frames.Publish(SixelFrame{})
	if _, err := out.Write(markedFlush("first", id1, "text-1")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	id2 := frames.Publish(SixelFrame{})
	if _, err := out.Write(markedFlush("second", id2, "text-2")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	for _, want := range []string{"\x1b]2;first\a", "\x1b]2;second\a"} {
		if !strings.Contains(raw.String(), want) {
			t.Fatalf("missing forwarded title %q; raw %q", want, raw.String())
		}
	}
	if strings.Contains(raw.String(), "slk-sixel-frame=") {
		t.Fatalf("suffix leaked: %q", raw.String())
	}
}

// A missing frame ID passes the text through untouched and must not
// mutate painter state (the previously painted image stays live).
func TestFrameOutput_MissingFramePassesTextWithoutPainterMutation(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)

	// Paint frame 1.
	id1 := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 2, 2, 1, 1)}})
	if _, err := out.Write(markedFlush("slk", id1, "text-1")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if out.painter.Live() != 1 {
		t.Fatalf("Live = %d after paint, want 1", out.painter.Live())
	}

	// Flush a marker for a frame that was never published.
	if _, err := out.Write(markedFlush("slk", 999, "text-2")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if out.painter.Live() != 1 {
		t.Fatalf("missing frame must not mutate painter state; Live = %d, want 1", out.painter.Live())
	}
	if !strings.Contains(raw.String(), "text-2") {
		t.Fatalf("text for the missing frame did not pass through: %q", raw.String())
	}

	// Re-publish the same placement: still live, so no re-emit.
	raw.Reset()
	id3 := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 2, 2, 1, 1)}})
	if _, err := out.Write(markedFlush("slk", id3, "text-3")); err != nil {
		t.Fatalf("Write 3: %v", err)
	}
	if strings.Contains(raw.String(), "\x1bPqa\x1b\\") {
		t.Fatalf("painter state lost; repainted a placement that should still be live: %q", raw.String())
	}
}

// The sixel plan belongs inside synchronized output: immediately before
// the final CSI ?2026l reset, never after it.
func TestFrameOutput_InsertsSixelBeforeSynchronizedOutputReset(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)
	id := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 2, 2, 1, 1)}})

	flush := append(markedFlush("slk", id, "diff-text"), []byte("\x1b[?2026l")...)
	if _, err := out.Write(flush); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := raw.String()
	diffAt := strings.Index(got, "diff-text")
	paintAt := strings.Index(got, "\x1bPqa\x1b\\")
	resetAt := strings.Index(got, "\x1b[?2026l")
	if diffAt < 0 || paintAt < 0 || resetAt < 0 {
		t.Fatalf("output missing components; got %q", got)
	}
	if !(diffAt < paintAt && paintAt < resetAt) {
		t.Fatalf("expected diff < DCS < ?2026l; got %q", got)
	}
}

// When a placement moves on scroll, its erase must land BEFORE the text
// diff (ECH blanks the cells it clears — after the diff it would wipe
// the text that scrolled into the vacated region) while the paint lands
// after the diff. Both stay inside the synchronized-output window.
func TestFrameOutput_ErasePrecedesTextDiffPaintFollows(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)

	id1 := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 10, 5, 2, 20)}})
	if _, err := out.Write(markedFlush("slk", id1, "first")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	raw.Reset()

	id2 := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 8, 5, 2, 20)}})
	flush := append(markedFlush("slk", id2, "diff-text"), []byte("\x1b[?2026l")...)
	if _, err := out.Write(flush); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	got := raw.String()
	eraseAt := strings.Index(got, "\x1b[10;5H")
	paintAt := strings.Index(got, "\x1bPqa\x1b\\")
	textAt := strings.Index(got, "diff-text")
	resetAt := strings.Index(got, "\x1b[?2026l")
	if eraseAt < 0 || paintAt < 0 || textAt < 0 || resetAt < 0 {
		t.Fatalf("output missing components; got %q", got)
	}
	if !(eraseAt < textAt && textAt < paintAt && paintAt < resetAt) {
		t.Fatalf("expected erase < diff-text < DCS < ?2026l; got %q", got)
	}
}

// A failed underlying write must not commit painter state, and the next
// successful write of the same frame must emit the paint again.
func TestFrameOutput_WriteFailureDoesNotCommitPainter(t *testing.T) {
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&failWriter{err: errors.New("boom")}, frames)
	id := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 5, 12, 4, 20)}})

	if _, err := out.Write(markedFlush("real", id, "body")); err == nil {
		t.Fatal("expected the underlying write error")
	}
	if out.painter.Live() != 0 {
		t.Fatalf("painter Live = %d after failed write, want 0", out.painter.Live())
	}

	// Recover: successful buffer, same frame content published again.
	buf := new(bytes.Buffer)
	out.out = buf
	id2 := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 5, 12, 4, 20)}})
	if _, err := out.Write(markedFlush("real", id2, "body")); err != nil {
		t.Fatalf("Write after recovery: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1bPqa\x1b\\")) {
		t.Fatalf("paint not emitted after failure recovery; got %q", buf.String())
	}
}

// A short write (n < len, nil error) is an io.ErrShortWrite and must not
// commit painter state or record the title as forwarded.
func TestFrameOutput_ShortWriteDoesNotCommitPainterOrTitle(t *testing.T) {
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&shortWriter{}, frames)
	id := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 5, 12, 4, 20)}})

	_, err := out.Write(markedFlush("real", id, "body"))
	if err != io.ErrShortWrite {
		t.Fatalf("err = %v, want io.ErrShortWrite", err)
	}
	if out.painter.Live() != 0 {
		t.Fatalf("painter Live = %d after short write, want 0", out.painter.Live())
	}

	// The title was not recorded as forwarded: a same-title flush on a
	// good writer still emits the OSC 2, and the paint lands.
	buf := new(bytes.Buffer)
	out.out = buf
	id2 := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 5, 12, 4, 20)}})
	if _, err := out.Write(markedFlush("real", id2, "body")); err != nil {
		t.Fatalf("Write after recovery: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1b]2;real\a")) {
		t.Fatalf("title not forwarded after short write; got %q", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1bPqa\x1b\\")) {
		t.Fatalf("paint not emitted after short write; got %q", buf.String())
	}
}

// io.Writer contract: Write returns the length of its input on success,
// not the length of the expanded output.
func TestFrameOutput_ReturnsInputLengthOnSuccess(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)
	id := frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 2, 2, 1, 1)}})

	input := markedFlush("slk", id, "text")
	n, err := out.Write(input)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(input) {
		t.Fatalf("Write returned %d, want %d", n, len(input))
	}
}

// Both OSC 2 terminators — BEL and ST — must be sanitized and framed.
func TestFrameOutput_SupportsBELAndSTTerminatedOSC2(t *testing.T) {
	for name, flush := range map[string][]byte{
		"BEL": append([]byte("\x1b]2;"+FrameTitle("slk", 1)+"\a"), []byte("body-bel")...),
		"ST":  append([]byte("\x1b]2;"+FrameTitle("slk", 1)+"\x1b\\"), []byte("body-st")...),
	} {
		t.Run(name, func(t *testing.T) {
			var raw bytes.Buffer
			frames := NewSixelFrameStore()
			out := NewFrameOutput(&raw, frames)
			frames.Publish(SixelFrame{Placements: []SixelPlacement{pl("a", 2, 2, 1, 1)}})
			if _, err := out.Write(flush); err != nil {
				t.Fatalf("Write: %v", err)
			}
			got := raw.String()
			if strings.Contains(got, "slk-sixel-frame=") {
				t.Fatalf("marker leaked: %q", got)
			}
			if !strings.Contains(got, "body-"+strings.ToLower(name)) {
				t.Fatalf("text missing: %q", got)
			}
		})
	}
}

// A frame whose OSC 2 is marked must still forward the real title once,
// regardless of which terminator Bubble Tea used.
func TestFrameOutput_ForwardsRealTitleWithBothTerminators(t *testing.T) {
	for name, term := range map[string][]byte{"BEL": {'\a'}, "ST": {'\x1b', '\\'}} {
		t.Run(name, func(t *testing.T) {
			var raw bytes.Buffer
			frames := NewSixelFrameStore()
			out := NewFrameOutput(&raw, frames)
			id := frames.Publish(SixelFrame{})
			osc := append([]byte("\x1b]2;"+FrameTitle("my-title", id)), term...)
			if _, err := out.Write(append(osc, []byte("text")...)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if !strings.Contains(raw.String(), "\x1b]2;my-title\a") {
				t.Fatalf("real title not forwarded; got %q", raw.String())
			}
		})
	}
}

// Writes without a marker pass through unchanged and mutate nothing.
func TestFrameOutput_UnmarkedFlushPassesThrough(t *testing.T) {
	var raw bytes.Buffer
	frames := NewSixelFrameStore()
	out := NewFrameOutput(&raw, frames)
	input := []byte("plain \x1b]2;my-title\a renderer bytes")
	if _, err := out.Write(input); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !bytes.Equal(raw.Bytes(), input) {
		t.Fatalf("unmarked flush mutated; got %q want %q", raw.Bytes(), input)
	}
	if out.painter.Live() != 0 {
		t.Fatalf("unmarked flush mutated painter; Live = %d", out.painter.Live())
	}
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (w shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

// FrameOutput must satisfy term.File so Bubble Tea and colorprofile can
// type-assert the program output and detect the terminal: when the
// wrapper hides the underlying fd, the renderer initializes at 0x0 and
// draws nothing. The delegated fd must be the real terminal's.
var _ term.File = (*FrameOutput)(nil)

func TestFrameOutput_ExposesUnderlyingFileDescriptor(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "fd")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	out := NewFrameOutput(f, NewSixelFrameStore())
	if out.Fd() != f.Fd() {
		t.Fatalf("Fd() = %d, want %d", out.Fd(), f.Fd())
	}
	var asFile term.File = out
	if asFile.Fd() != f.Fd() {
		t.Fatalf("term.File Fd() = %d, want %d", asFile.Fd(), f.Fd())
	}
}
