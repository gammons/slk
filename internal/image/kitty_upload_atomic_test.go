package image

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// writeRecorder records each Write call separately.
type writeRecorder struct {
	mu     sync.Mutex
	writes [][]byte
}

func (r *writeRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, append([]byte(nil), p...))
	return len(p), nil
}

// A multi-chunk upload must reach the writer as one Write. KittyOutput
// serializes Write calls, so this is what keeps a concurrent upload from
// landing between this upload's first chunk and its continuations.
func TestEmitKittyUpload_MultiChunkPayloadIsOneWrite(t *testing.T) {
	payload := strings.Repeat("A", 4096) + strings.Repeat("B", 4096) + "CC"
	rec := &writeRecorder{}
	if err := emitKittyUpload(rec, 7, payload, 4, 2); err != nil {
		t.Fatal(err)
	}
	if len(rec.writes) != 1 {
		t.Fatalf("upload took %d writes; want 1", len(rec.writes))
	}
	want := forTerminal("\x1b_Ga=T,f=100,t=d,i=7,U=1,c=4,r=2,q=2,m=1;"+strings.Repeat("A", 4096)+"\x1b\\") +
		forTerminal("\x1b_Gm=1;"+strings.Repeat("B", 4096)+"\x1b\\") +
		forTerminal("\x1b_Gm=0;CC\x1b\\")
	if got := string(rec.writes[0]); got != want {
		t.Errorf("upload bytes changed:\n got %q\nwant %q", abbreviate(got), abbreviate(want))
	}
}

// Many uploads racing through the serialized side channel must each
// arrive as an unbroken run: a start chunk naming the id, then only that
// upload's continuations until m=0.
func TestEmitKittyUpload_ConcurrentUploadsDoNotInterleave(t *testing.T) {
	var buf bytes.Buffer
	out := SerializeOutput(&buf)
	const uploads = 16
	var wg sync.WaitGroup
	for n := 1; n <= uploads; n++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			payload := strings.Repeat(fmt.Sprintf("%x", id%16), 4096*3+17)
			if err := emitKittyUpload(out, uint32(id), payload, 4, 2); err != nil {
				t.Error(err)
			}
		}(n)
	}
	wg.Wait()

	stream := buf.String()
	if inTmux() {
		t.Skip("stream parsing below assumes unwrapped sequences")
	}
	seqRe := regexp.MustCompile("\x1b_G([^;]*);([^\x1b]*)\x1b\\\\")
	current := 0
	started := 0
	for _, m := range seqRe.FindAllStringSubmatch(stream, -1) {
		hdr, data := m[1], m[2]
		if strings.HasPrefix(hdr, "a=T") {
			if current != 0 {
				t.Fatalf("upload %d started while upload %d was still in progress", idOf(hdr), current)
			}
			current = idOf(hdr)
			started++
		} else if current == 0 {
			t.Fatal("continuation chunk with no upload in progress")
		}
		if want := fmt.Sprintf("%x", current%16); strings.Trim(data, want) != "" {
			t.Fatalf("upload %d received another upload's bytes", current)
		}
		if strings.HasSuffix(hdr, "m=0") {
			current = 0
		}
	}
	if started != uploads || current != 0 {
		t.Errorf("parsed %d complete uploads (in progress: %d); want %d", started, current, uploads)
	}
}

func idOf(hdr string) int {
	var id int
	for _, kv := range strings.Split(hdr, ",") {
		if strings.HasPrefix(kv, "i=") {
			fmt.Sscanf(kv, "i=%d", &id)
		}
	}
	return id
}

func abbreviate(s string) string {
	if len(s) <= 200 {
		return s
	}
	return s[:100] + "…" + s[len(s)-100:]
}
