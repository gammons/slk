package image

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// TestSerializeOutput_NoInterleave asserts that concurrent Write calls
// to a SerializeOutput-wrapped writer land as contiguous runs — never
// interleaved at the byte level. This is the invariant the kitty
// graphics path depends on: a single image upload is hundreds of KB
// of chunked APC escape data and ANY byte-level interleave from a
// competing goroutine corrupts the protocol stream.
func TestSerializeOutput_NoInterleave(t *testing.T) {
	var buf bytes.Buffer
	w := SerializeOutput(&buf)

	const size = 10000
	a := bytes.Repeat([]byte("A"), size)
	b := bytes.Repeat([]byte("B"), size)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); w.Write(a) }()
	go func() { defer wg.Done(); w.Write(b) }()
	wg.Wait()

	got := buf.String()
	wantAB := strings.Repeat("A", size) + strings.Repeat("B", size)
	wantBA := strings.Repeat("B", size) + strings.Repeat("A", size)
	if got != wantAB && got != wantBA {
		// Find the first non-A non-B run boundary to give a useful
		// diagnostic. The full strings are too large to print.
		for i := 1; i < len(got); i++ {
			if got[i] != got[i-1] {
				if i != size {
					t.Fatalf("write interleave detected: first boundary at byte %d, want %d", i, size)
				}
				break
			}
		}
		t.Fatalf("unexpected output: len=%d", len(got))
	}
}

// TestSerializeOutput_ManyWritersForwardsAllBytes asserts that all
// bytes from many concurrent writers reach the underlying writer.
func TestSerializeOutput_ManyWritersForwardsAllBytes(t *testing.T) {
	var buf bytes.Buffer
	w := SerializeOutput(&buf)

	const writers = 32
	const perWriter = 1024
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func() {
			defer wg.Done()
			w.Write(bytes.Repeat([]byte("x"), perWriter))
		}()
	}
	wg.Wait()

	if got, want := buf.Len(), writers*perWriter; got != want {
		t.Fatalf("byte count: got %d, want %d", got, want)
	}
}
