package avatar

import (
	"bytes"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

// TestRender_ParityGolden asserts that the refactored avatar package
// produces byte-identical output to the pre-refactor implementation, for
// a deterministic 16x16 PNG fed through the fetcher+half-block pipeline.
//
// The `want` constant below was captured from the OLD avatar code (commit
// e6cde11's avatar.go) via `t.Logf("GOLDEN: %q", got)`.
func TestRender_ParityGolden(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			src.Set(x, y, imgcolor.RGBA{uint8(x * 16), uint8(y * 16), 128, 255})
		}
	}
	var buf bytes.Buffer
	imgpng.Encode(&buf, src)
	pngBytes := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	cache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := imgpkg.NewFetcher(cache, http.DefaultClient)
	// Halfblock path explicitly: parity test verifies this rendering
	// stays byte-identical to the original pre-kitty pipeline. Kitty
	// avatars are tested separately.
	c := NewCache(fetcher, nil, false)
	c.PreloadSync("U_GOLDEN", srv.URL)
	got := c.Get("U_GOLDEN")
	if got == "" {
		t.Fatal("avatar render is empty")
	}

	const want = "\x1b[38;2;30;30;128m\x1b[48;2;30;88;128m▀\x1b[38;2;88;30;128m\x1b[48;2;88;88;128m▀\x1b[38;2;152;30;128m\x1b[48;2;152;88;128m▀\x1b[38;2;210;30;128m\x1b[48;2;210;88;128m▀\x1b[0m\n\x1b[38;2;30;152;128m\x1b[48;2;30;210;128m▀\x1b[38;2;88;152;128m\x1b[48;2;88;210;128m▀\x1b[38;2;152;152;128m\x1b[48;2;152;210;128m▀\x1b[38;2;210;152;128m\x1b[48;2;210;210;128m▀\x1b[0m"
	if got != want {
		t.Errorf("parity mismatch:\n got: %q\nwant: %q", got, want)
	}
}
