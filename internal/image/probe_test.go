package image

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProbeKittyGraphics_TimeoutFails(t *testing.T) {
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

type blockingReader struct{}

func (blockingReader) Read(p []byte) (int, error) {
	time.Sleep(time.Hour)
	return 0, nil
}
