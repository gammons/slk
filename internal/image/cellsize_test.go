package image

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestParseCellSize(t *testing.T) {
	cases := []struct {
		name         string
		buf          string
		matched      bool
		wantW, wantH int
		ok           bool
	}{
		// Windows Terminal's reply with Cascadia Mono at its default size.
		{"windows terminal", "\x1b[6;20;10t", true, 10, 20, true},
		{"hidpi", "\x1b[6;34;14t", true, 14, 34, true},
		{"after other replies", "\x1b[?61;4;6c\x1b[4;860;1560t\x1b[6;20;10t", true, 10, 20, true},
		{"zero", "\x1b[6;0;10t", true, 0, 0, false},
		{"garbage", "\x1b[6;x;10t", true, 0, 0, false},
		{"three fields", "\x1b[6;20;10;1t", true, 0, 0, false},
		{"incomplete", "\x1b[6;20;10", false, 0, 0, false},
		// The text area reply (CSI 14 t) is not a cell size.
		{"text area only", "\x1b[4;860;1560t", false, 0, 0, false},
		{"empty", "", false, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			matched, ok := scanForCellSize([]byte(c.buf))
			if matched != c.matched || ok != c.ok {
				t.Errorf("scanForCellSize = (%v, %v), want (%v, %v)", matched, ok, c.matched, c.ok)
			}
			w, h, ok := parseCellSize([]byte(c.buf))
			if w != c.wantW || h != c.wantH || ok != c.ok {
				t.Errorf("parseCellSize = (%d, %d, %v), want (%d, %d, %v)", w, h, ok, c.wantW, c.wantH, c.ok)
			}
		})
	}
}

func TestProbeCellSize_ReadsReply(t *testing.T) {
	var w bytes.Buffer
	pxW, pxH, ok := ProbeCellSize(&w, strings.NewReader("\x1b[6;20;10t"), time.Second)
	if !ok || pxW != 10 || pxH != 20 {
		t.Errorf("ProbeCellSize = (%d, %d, %v), want (10, 20, true)", pxW, pxH, ok)
	}
	if w.String() != "\x1b[16t" {
		t.Errorf("query = %q, want CSI 16 t", w.String())
	}
}

func TestProbeCellSize_NoReply(t *testing.T) {
	var w bytes.Buffer
	if _, _, ok := ProbeCellSize(&w, blockingReader{}, 50*time.Millisecond); ok {
		t.Error("expected no size from a terminal that does not answer")
	}
}

// The production path on a real *os.File: the answer, and no reader left
// behind to take bytes meant for bubbletea.
func TestProbeCellSize_PollPathOnRealPipe(t *testing.T) {
	skipPipeProbeOnWindows(t)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()
	if _, err := pw.Write([]byte("\x1b[6;20;10t")); err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	if pxW, pxH, ok := ProbeCellSize(&w, pr, time.Second); !ok || pxW != 10 || pxH != 20 {
		t.Fatalf("ProbeCellSize = (%d, %d, %v), want (10, 20, true)", pxW, pxH, ok)
	}
	if _, err := pw.Write([]byte{'X'}); err != nil {
		t.Fatal(err)
	}
	if err := pr.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if n, err := pr.Read(buf); err != nil || n != 1 || buf[0] != 'X' {
		t.Fatalf("read after probe: n=%d err=%v", n, err)
	}
}

func TestMeasureCellPixels_EnvOverride(t *testing.T) {
	t.Setenv("COLORTERM_CELL_WIDTH", "10")
	t.Setenv("COLORTERM_CELL_HEIGHT", "20")
	if w, h, ok := MeasureCellPixels(-1); !ok || w != 10 || h != 20 {
		t.Errorf("MeasureCellPixels = (%d, %d, %v), want (10, 20, true)", w, h, ok)
	}
	t.Setenv("COLORTERM_CELL_WIDTH", "")
	if _, _, ok := MeasureCellPixels(-1); ok {
		t.Error("expected no measurement without the override or an fd")
	}
	if w, h := CellPixels(-1); w != 8 || h != 16 {
		t.Errorf("CellPixels fallback = %dx%d, want 8x16", w, h)
	}
}

// consoleInputRecord must match the Win32 INPUT_RECORD layout that
// ReadConsoleInputW fills, or every character read is garbage.
func TestConsoleInputRecordLayout(t *testing.T) {
	var r consoleInputRecord
	if got := unsafe.Sizeof(r); got != 20 {
		t.Errorf("sizeof = %d, want 20", got)
	}
	offsets := map[string]uintptr{
		"KeyDown":     unsafe.Offsetof(r.KeyDown),
		"RepeatCount": unsafe.Offsetof(r.RepeatCount),
		"UnicodeChar": unsafe.Offsetof(r.UnicodeChar),
		"ControlKeys": unsafe.Offsetof(r.ControlKeys),
	}
	want := map[string]uintptr{"KeyDown": 4, "RepeatCount": 8, "UnicodeChar": 14, "ControlKeys": 16}
	for k, w := range want {
		if offsets[k] != w {
			t.Errorf("offset of %s = %d, want %d", k, offsets[k], w)
		}
	}
}

func keyRec(c uint16, down bool) consoleInputRecord {
	r := consoleInputRecord{EventType: consoleKeyEvent, UnicodeChar: c, RepeatCount: 1}
	if down {
		r.KeyDown = 1
	}
	return r
}

func TestAppendConsoleChars(t *testing.T) {
	var recs []consoleInputRecord
	for _, c := range "\x1b[6;20;10t" {
		recs = append(recs, keyRec(uint16(c), true), keyRec(uint16(c), false))
	}
	// A focus event (EventType 0x10) and a key with no character (Shift).
	recs = append([]consoleInputRecord{{EventType: 0x10}, keyRec(0, true)}, recs...)
	got, pending := appendConsoleChars(nil, recs, 0)
	if string(got) != "\x1b[6;20;10t" || pending != 0 {
		t.Errorf("got %q pending %#x", got, pending)
	}

	// A repeat count, and a surrogate pair split across two reads.
	rep := keyRec('a', true)
	rep.RepeatCount = 3
	got, pending = appendConsoleChars(nil, []consoleInputRecord{rep, keyRec(0xD83D, true)}, 0)
	if string(got) != "aaa" || pending != 0xD83D {
		t.Fatalf("got %q pending %#x", got, pending)
	}
	got, pending = appendConsoleChars(got, []consoleInputRecord{keyRec(0xDE00, true)}, pending)
	if string(got) != "aaa\U0001F600" || pending != 0 {
		t.Errorf("got %q pending %#x", got, pending)
	}
}
