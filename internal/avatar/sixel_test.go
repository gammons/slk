package avatar

import (
	"bytes"
	"image"
	imgcolor "image/color"
	"strings"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

// In sixel mode a loaded avatar has a sixel image at the avatar's
// footprint, keyed by user and content, and still a half-block render
// for the frames that cannot paint the image.
func TestPreload_SixelEncodesAvatar(t *testing.T) {
	c, url, done := testCache(t)
	defer done()
	c.SetSixel(true)
	c.PreloadSync("U1", url)

	key, b, ok := c.GetSixel("U1")
	if !ok || len(b) == 0 {
		t.Fatalf("GetSixel(U1) = (%q, %d bytes, %v), want a sixel image", key, len(b), ok)
	}
	if !bytes.HasPrefix(b, []byte("\x1bP")) {
		t.Errorf("sixel bytes do not start with DCS: %q", b[:min(len(b), 8)])
	}
	if !strings.HasPrefix(key, "avatar-U1-") {
		t.Errorf("key = %q, want the user's avatar identity", key)
	}
	if hb := c.Get("U1"); strings.Count(hb, "\n") != AvatarRows-1 {
		t.Errorf("Get(U1) = %q, want the %d-row half-block fallback", hb, AvatarRows)
	}
}

// Off sixel mode, and before an avatar loads, there is no sixel image.
func TestGetSixel_NoneOffSixelOrUnloaded(t *testing.T) {
	c, url, done := testCache(t)
	defer done()
	c.PreloadSync("U1", url)
	if _, _, ok := c.GetSixel("U1"); ok {
		t.Error("GetSixel answered without SetSixel")
	}

	s, _, done2 := testCache(t)
	defer done2()
	s.SetSixel(true)
	if _, _, ok := s.GetSixel("U2"); ok {
		t.Error("GetSixel answered for an avatar that never loaded")
	}
}

// Kitty draws avatars itself; SetSixel does not switch it to sixel.
func TestSetSixel_IgnoredWithKitty(t *testing.T) {
	c := newCacheForTest(nil, imgpkg.NewKittyRenderer(imgpkg.NewRegistry()), true, 0, 1)
	c.SetSixel(true)
	if c.useSixel {
		t.Error("SetSixel(true) turned sixel on for a kitty cache")
	}
}

// A changed avatar is a different image to the painter: the key carries
// the content, not just the user.
func TestEncodeSixel_KeyFollowsContent(t *testing.T) {
	a := encodeSixel("U1", solid(10, 200, 30))
	b := encodeSixel("U1", solid(200, 10, 30))
	if a.key == "" || b.key == "" || a.key == b.key {
		t.Errorf("keys %q and %q, want two distinct non-empty keys", a.key, b.key)
	}
}

// solid is a 16x16 image of one colour.
func solid(r, g, b uint8) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, imgcolor.RGBA{r, g, b, 255})
		}
	}
	return img
}
