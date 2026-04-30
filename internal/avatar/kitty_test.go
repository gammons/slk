package avatar

import (
	"bytes"
	"image"
	imgcolor "image/color"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

// TestRender_KittyAvatarUploadsAndPlaceholders asserts that, when the
// cache is configured for kitty rendering, PreloadSync writes a kitty
// graphics upload escape (\x1b_G ... \x1b\\) to the side-channel
// writer and stores a render string composed of unicode-placeholder
// cells (U+10EEEE) at the avatar's 4x2 cell footprint.
//
// We don't assert on byte-exact contents — kitty rendering depends on
// the registry's auto-assigned image ID, which varies between test
// runs — but we do verify the structural shape:
//   - the upload escape appears on the kitty side channel,
//   - the rendered string contains the placeholder rune,
//   - the rendered string spans exactly AvatarRows lines.
func TestRender_KittyAvatarUploadsAndPlaceholders(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.Set(x, y, imgcolor.RGBA{uint8(x * 8), uint8(y * 8), 64, 255})
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

	// Capture the kitty side channel for assertions. The renderer
	// writes the upload escape directly to imgpkg.KittyOutput
	// because lipgloss/bubbletea strip APC sequences embedded in
	// rendered strings.
	saved := imgpkg.KittyOutput
	defer func() { imgpkg.KittyOutput = saved }()
	var sideCh bytes.Buffer
	imgpkg.KittyOutput = &sideCh

	kitty := imgpkg.NewKittyRenderer(imgpkg.NewRegistry())
	c := NewCache(fetcher, kitty, true)
	c.PreloadSync("U_KITTY", srv.URL)
	got := c.Get("U_KITTY")

	if got == "" {
		t.Fatal("expected non-empty kitty avatar render")
	}
	// Placeholder rune present on every row.
	if !strings.ContainsRune(got, '\U0010EEEE') {
		t.Errorf("expected kitty placeholder rune in render, got %q", got)
	}
	// AvatarRows lines (one newline separator).
	if nl := strings.Count(got, "\n"); nl != AvatarRows-1 {
		t.Errorf("expected %d newlines, got %d (render: %q)", AvatarRows-1, nl, got)
	}
	// Upload escape on side channel.
	if !strings.HasPrefix(sideCh.String(), "\x1b_G") {
		t.Errorf("expected kitty graphics upload (\\e_G) on side channel, got %d bytes starting with %q",
			sideCh.Len(), sideCh.String()[:min(20, sideCh.Len())])
	}
	if !strings.Contains(sideCh.String(), "U=1") {
		t.Error("expected U=1 (unicode placeholder) in upload escape")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
