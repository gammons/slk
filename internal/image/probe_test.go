package image

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProbeKittyGraphics_TimeoutFails(t *testing.T) {
	t.Setenv("TMUX", "")
	r := blockingReader{}
	var w bytes.Buffer
	ok := ProbeKittyGraphics(&w, r, 50*time.Millisecond)
	if ok {
		t.Error("expected probe to fail on timeout")
	}
	if !strings.Contains(w.String(), "\x1b_G") {
		t.Errorf("expected \\e_G in probe output, got %q", w.String())
	}
}

func TestProbeKittyGraphics_WrapsProbeInTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux")
	r := blockingReader{}
	var w bytes.Buffer
	ok := ProbeKittyGraphics(&w, r, 50*time.Millisecond)
	if ok {
		t.Error("expected probe to fail on timeout")
	}
	if !strings.HasPrefix(w.String(), "\x1bPtmux;\x1b\x1b_G") {
		t.Errorf("expected tmux-wrapped kitty probe, got %q", w.String())
	}
}

// TestProbeKittyGraphics_NoStdinTheftAfterTimeout is the regression
// guard for issue #50. The historical implementation leaked a goroutine
// that kept reading from r forever after the probe timed out; that
// goroutine then stole bytes from whatever reader the host installed
// next (bubbletea), making slk unresponsive in zellij / tmux without
// allow-passthrough.
//
// We use os.Pipe to get a real pollable *os.File pair. We expect that:
//  1. ProbeKittyGraphics returns false on timeout.
//  2. A byte written to the pipe AFTER the probe returns is delivered
//     intact to a subsequent reader (i.e. no leaked goroutine
//     intercepts it).
func TestProbeKittyGraphics_NoStdinTheftAfterTimeout(t *testing.T) {
	t.Setenv("TMUX", "")
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	var wbuf bytes.Buffer
	ok := ProbeKittyGraphics(&wbuf, pr, 50*time.Millisecond)
	if ok {
		t.Fatal("expected probe to fail on timeout (pipe never replies)")
	}

	// Write a byte AFTER the probe has timed out. If a leaked
	// goroutine is still reading from pr, it will consume this byte
	// and the Read below will block until the test's deadline.
	if _, err := pw.Write([]byte{'X'}); err != nil {
		t.Fatalf("pipe write: %v", err)
	}

	// Bound the read so the test fails fast on regression instead of
	// hanging.
	if err := pr.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1)
	n, err := pr.Read(buf)
	if err != nil || n != 1 || buf[0] != 'X' {
		t.Fatalf("expected to read 'X' from pipe after probe; got n=%d err=%v buf=%q (probe goroutine likely leaked and stole the byte)",
			n, err, buf[:n])
	}
}

func TestScanForSixelDA(t *testing.T) {
	cases := []struct {
		name        string
		buf         string
		wantMatched bool
		wantOK      bool
	}{
		{"xterm vt340", "\x1b[?63;1;2;4;6;9;15;22c", true, true},
		{"foot", "\x1b[?62;4;22c", true, true},
		{"sixel only", "\x1b[?4c", true, true},
		{"no sixel", "\x1b[?62;1;6;22c", true, false},
		// Attributes are semicolon-separated values, not substrings:
		// 14 / 24 / 41 are unrelated capabilities.
		{"substring not attribute", "\x1b[?14;24;41c", true, false},
		{"incomplete: no terminator", "\x1b[?62;4;22", false, false},
		{"incomplete: no csi", "62;4;22c", false, false},
		{"empty", "", false, false},
		// Terminals emit the reply amid whatever else is in flight.
		{"leading noise", "junk\x1b[?62;4;22c", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, ok := scanForSixelDA([]byte(tc.buf))
			if matched != tc.wantMatched || ok != tc.wantOK {
				t.Errorf("scanForSixelDA(%q) = (%v, %v), want (%v, %v)",
					tc.buf, matched, ok, tc.wantMatched, tc.wantOK)
			}
		})
	}
}

func TestProbeSixel_SendsDA1AndFailsOnTimeout(t *testing.T) {
	var w bytes.Buffer
	if ok := ProbeSixel(&w, blockingReader{}, 50*time.Millisecond); ok {
		t.Error("expected probe to fail on timeout")
	}
	if w.String() != "\x1b[c" {
		t.Errorf("expected DA1 query, got %q", w.String())
	}
}

func TestProbeSixel_ReadsReply(t *testing.T) {
	cases := []struct {
		reply string
		want  bool
	}{
		{"\x1b[?63;1;2;4;6;9;15;22c", true},
		{"\x1b[?62;1;6;22c", false},
	}
	for _, tc := range cases {
		t.Run(tc.reply, func(t *testing.T) {
			var w bytes.Buffer
			r := strings.NewReader(tc.reply)
			if got := ProbeSixel(&w, r, time.Second); got != tc.want {
				t.Errorf("ProbeSixel with reply %q = %v, want %v", tc.reply, got, tc.want)
			}
		})
	}
}

// The poll-based production path (real *os.File) must reach the same
// answer as the goroutine fallback, and must not leave a reader behind
// that steals bytes from bubbletea afterwards (the issue #50 hazard).
func TestProbeSixel_PollPathOnRealPipe(t *testing.T) {
	skipPipeProbeOnWindows(t)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer pr.Close()
	defer pw.Close()

	if _, err := pw.Write([]byte("\x1b[?62;4;22c")); err != nil {
		t.Fatalf("pipe write: %v", err)
	}
	var w bytes.Buffer
	if ok := ProbeSixel(&w, pr, time.Second); !ok {
		t.Fatal("expected sixel-capable DA1 reply to be recognized")
	}

	if _, err := pw.Write([]byte{'X'}); err != nil {
		t.Fatalf("pipe write: %v", err)
	}
	if err := pr.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 1)
	n, err := pr.Read(buf)
	if err != nil || n != 1 || buf[0] != 'X' {
		t.Fatalf("expected to read 'X' from pipe after probe; got n=%d err=%v buf=%q",
			n, err, buf[:n])
	}
}

type blockingReader struct{}

func (blockingReader) Read(p []byte) (int, error) {
	time.Sleep(time.Hour)
	return 0, nil
}

// The emulated-placement probe is a query: WezTerm answers it, and it
// stores and draws nothing, unlike ProbeKittyGraphics' a=T upload.
func TestProbeKittyQuery_SendsQuery(t *testing.T) {
	t.Setenv("TMUX", "")
	var w bytes.Buffer
	if ProbeKittyQuery(&w, blockingReader{}, 50*time.Millisecond) {
		t.Error("expected probe to fail on timeout")
	}
	got := w.String()
	if !strings.HasPrefix(got, "\x1b_Ga=q,") || strings.Contains(got, "a=T") {
		t.Errorf("want an a=q query, got %q", got)
	}
}

func TestProbeKittyQuery_AcceptsOK(t *testing.T) {
	t.Setenv("TMUX", "")
	var w bytes.Buffer
	reply := strings.NewReader("\x1b_Gi=9999;OK\x1b\\")
	if !ProbeKittyQuery(&w, reply, time.Second) {
		t.Error("expected the OK reply to pass the probe")
	}
}

// skipPipeProbeOnWindows skips a test that hands a probe an os.Pipe. On
// Windows the poll path reads console input records (ReadConsoleInputW
// in probe_windows.go), which a pipe handle does not have, so the probe
// can only fall back to "no answer" there.
func skipPipeProbeOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Windows poll path reads console input, which a pipe is not")
	}
}
