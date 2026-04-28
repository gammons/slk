package emoji

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseDSRResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantCol int
		wantErr bool
	}{
		{"valid response", []byte("\x1b[1;3R"), 3, false},
		{"valid with extra row digits", []byte("\x1b[42;5R"), 5, false},
		{"valid col=1", []byte("\x1b[1;1R"), 1, false},
		{"valid wide", []byte("\x1b[1;200R"), 200, false},
		{"empty", []byte(""), 0, true},
		{"missing terminator", []byte("\x1b[1;3"), 0, true},
		{"missing semicolon", []byte("\x1b[13R"), 0, true},
		{"missing CSI", []byte("1;3R"), 0, true},
		{"col not a number", []byte("\x1b[1;xR"), 0, true},
		{"col zero", []byte("\x1b[1;0R"), 0, true},
		{"col negative", []byte("\x1b[1;-1R"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := parseDSRResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDSRResponse(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if col != tt.wantCol {
				t.Errorf("parseDSRResponse(%q) col = %d, want %d", tt.input, col, tt.wantCol)
			}
		})
	}
}

// fakeTerminal simulates a terminal that responds to CSI 6n queries
// with a configurable column number.
type fakeTerminal struct {
	mu      sync.Mutex
	out     *bytes.Buffer  // stdin from probe's perspective (we read from this)
	in      *bytes.Buffer  // stdout from probe's perspective (we write to this)
	respCol map[string]int // emoji string → column to report after rendering
	current string         // current emoji being measured
	timeout bool           // if true, never respond to DSR
}

func newFakeTerminal(widths map[string]int) *fakeTerminal {
	return &fakeTerminal{
		out:     &bytes.Buffer{},
		in:      &bytes.Buffer{},
		respCol: widths,
	}
}

// Write captures probe output. When CSI 6n is seen, queue a response.
func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in.Write(p)
	s := string(p)
	// Detect emoji by capturing what's written between \r and CSI 6n
	if strings.Contains(s, "\x1b[6n") {
		// Find the most recent \r in our buffer; everything between it and \x1b[6n is the emoji
		buf := f.in.String()
		lastCR := strings.LastIndex(buf[:strings.LastIndex(buf, "\x1b[6n")], "\r")
		if lastCR >= 0 {
			f.current = buf[lastCR+1 : strings.LastIndex(buf, "\x1b[6n")]
		}
		if f.timeout {
			return len(p), nil
		}
		col, ok := f.respCol[f.current]
		if !ok {
			col = 1 // unknown emoji → no advance
		}
		// Column = 1 + width (we start at column 1, render emoji, cursor is at 1+width)
		fmt.Fprintf(f.out, "\x1b[1;%dR", 1+col)
	}
	return len(p), nil
}

func (f *fakeTerminal) Read(p []byte) (int, error) {
	if f.timeout {
		// Block briefly so the probe's deadline triggers
		time.Sleep(500 * time.Millisecond)
		return 0, io.EOF
	}
	// Spin briefly until data is available, since Write may happen
	// concurrently (probe writes, then reads).
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		if f.out.Len() > 0 {
			n, err := f.out.Read(p)
			f.mu.Unlock()
			return n, err
		}
		f.mu.Unlock()
		if time.Now().After(deadline) {
			return 0, io.EOF
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProbeOne(t *testing.T) {
	widths := map[string]int{
		"a": 1,
		"中": 2,
		"👍": 2,
		"❤": 1,
	}
	ft := newFakeTerminal(widths)

	for emoji, want := range widths {
		got, err := probeOne(ft, ft, emoji, 200*time.Millisecond)
		if err != nil {
			t.Errorf("probeOne(%q) error: %v", emoji, err)
			continue
		}
		if got != want {
			t.Errorf("probeOne(%q) = %d, want %d", emoji, got, want)
		}
	}
}

func TestProbeOneTimeout(t *testing.T) {
	ft := newFakeTerminal(nil)
	ft.timeout = true

	_, err := probeOne(ft, ft, "👍", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, errProbeTimeout) {
		t.Errorf("expected errProbeTimeout, got %v", err)
	}
}

func TestProbeAll(t *testing.T) {
	codemap := map[string]string{
		":a:":  "a",
		":cn:": "中",
		":up:": "👍",
	}
	widths := map[string]int{
		"a":  1,
		"中": 2,
		"👍": 2,
	}
	ft := newFakeTerminal(widths)

	result, err := probeAll(ft, ft, codemap, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("probeAll: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}
	for _, emoji := range []string{"a", "中", "👍"} {
		if got, ok := result[emoji]; !ok {
			t.Errorf("missing entry for %q", emoji)
		} else if got != widths[emoji] {
			t.Errorf("Width(%q) = %d, want %d", emoji, got, widths[emoji])
		}
	}
}

// TestProbeAllContinuesAfterUnknown verifies that probeAll does not abort
// when an emoji isn't present in the fake terminal's width map. The fake
// returns column=1 (width 0) for unknown emoji, so "中" is recorded with
// width 0 rather than skipped — but importantly the loop continues and
// "a" still ends up in the result. The actual timeout-skip behavior is
// covered by TestProbeOneTimeout (errProbeTimeout sentinel).
func TestProbeAllContinuesAfterUnknown(t *testing.T) {
	codemap := map[string]string{
		":a:":  "a",
		":cn:": "中",
	}
	ft := newFakeTerminal(map[string]int{"a": 1})
	result, err := probeAll(ft, ft, codemap, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("probeAll: %v", err)
	}
	if _, ok := result["a"]; !ok {
		t.Error("expected 'a' in result; loop did not continue past unknown emoji")
	}
}
