package avatar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	imgpng "image/png"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

// pathLog records the request paths a test server saw, in order.
type pathLog struct {
	mu    sync.Mutex
	paths []string
}

func (l *pathLog) add(p string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paths = append(l.paths, p)
}

func (l *pathLog) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.paths)
}

// sizedTestServer serves a 16x16 PNG at every path in ok, a 200
// text/html login page at every path in html (what Slack's CDN sends
// on an auth failure), and a 404 everywhere else.
func sizedTestServer(t *testing.T, ok, html []string) (*httptest.Server, *pathLog) {
	t.Helper()
	var buf bytes.Buffer
	if err := imgpng.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	png := buf.Bytes()
	log := &pathLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.Path)
		switch {
		case slices.Contains(ok, r.URL.Path):
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case slices.Contains(html, r.URL.Path):
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>login</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

// testSizeURL stands in for SizedURL, which only recognizes Slack hosts,
// so httptest URLs can exercise the rewrite.
func testSizeURL(u string) string { return strings.Replace(u, "_original.", "_72.", 1) }

func newImgCache(t *testing.T) *imgpkg.Cache {
	t.Helper()
	ic, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	return ic
}

// sizedTestCache builds an avatar Cache over imgCache with a fresh
// fetcher, as a new slk launch would against the same disk cache.
func sizedTestCache(imgCache *imgpkg.Cache) *Cache {
	c := NewCache(imgpkg.NewFetcher(imgCache, http.DefaultClient), nil, false)
	c.sizeURL = testSizeURL
	return c
}

func TestPreload_FetchesSizedURL(t *testing.T) {
	srv, log := sizedTestServer(t, []string{"/a_72.png", "/a_original.png"}, nil)
	c := sizedTestCache(newImgCache(t))

	c.PreloadSync("U1", srv.URL+"/a_original.png")

	if got, want := log.get(), []string{"/a_72.png"}; !slices.Equal(got, want) {
		t.Errorf("requested %v; want %v (the sized URL only)", got, want)
	}
	if c.Get("U1") == "" {
		t.Error("avatar not rendered")
	}
}

func TestPreload_FallsBackToOriginal(t *testing.T) {
	cases := []struct {
		name     string
		ok, html []string
	}{
		{"sized 404", []string{"/a_original.png"}, nil},
		{"sized answers 200 with HTML", []string{"/a_original.png"}, []string{"/a_72.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, log := sizedTestServer(t, tc.ok, tc.html)
			c := sizedTestCache(newImgCache(t))
			var ready atomic.Int32
			c.SetOnReady(func(string) { ready.Add(1) })

			c.PreloadSync("U1", srv.URL+"/a_original.png")

			if got, want := log.get(), []string{"/a_72.png", "/a_original.png"}; !slices.Equal(got, want) {
				t.Errorf("requested %v; want %v", got, want)
			}
			if c.Get("U1") == "" {
				t.Error("avatar not rendered after falling back to the original")
			}
			if n := ready.Load(); n != 1 {
				t.Errorf("onReady fired %d times; want 1", n)
			}
		})
	}
}

// A URL the rewrite leaves alone (Gravatar, bot icons, already-sized
// _32 URLs) must cost exactly one request, success or failure: the
// fallback must not refetch the same URL.
func TestPreload_NoRewriteMakesOneRequest(t *testing.T) {
	srv, log := sizedTestServer(t, []string{"/plain.png"}, nil)
	c := sizedTestCache(newImgCache(t))

	c.PreloadSync("U1", srv.URL+"/plain.png")
	c.PreloadSync("U2", srv.URL+"/gone.png")

	if got, want := log.get(), []string{"/plain.png", "/gone.png"}; !slices.Equal(got, want) {
		t.Errorf("requested %v; want %v (one request each)", got, want)
	}
	if c.Get("U1") == "" {
		t.Error("U1 not rendered")
	}
	if c.Get("U2") != "" {
		t.Error("U2 rendered despite a 404")
	}
}

func TestFetchKey(t *testing.T) {
	const u = "https://avatars.slack-edge.com/2024-01-02/1_ab_72.png"
	sum := sha256.Sum256([]byte(u))
	if got, want := fetchKey("U1", u), "avatar-U1-"+hex.EncodeToString(sum[:])[:12]; got != want {
		t.Errorf("fetchKey = %q; want %q", got, want)
	}
	if fetchKey("U1", u) == fetchKey("U1", u+"x") {
		t.Error("different URLs for one user share a key")
	}
	if fetchKey("U1", u) == fetchKey("U2", u) {
		t.Error("different users with one URL share a key")
	}
}

// A user's avatar URL changes between launches: the new URL must be
// fetched rather than the old file served from the shared disk cache,
// and a repeat of the same URL must still be a disk hit.
func TestPreload_NewURLForSameUserIsFetchedFresh(t *testing.T) {
	srv, log := sizedTestServer(t, []string{"/old.png", "/new.png"}, nil)
	imgCache := newImgCache(t)

	sizedTestCache(imgCache).PreloadSync("U1", srv.URL+"/old.png")
	sizedTestCache(imgCache).PreloadSync("U1", srv.URL+"/new.png")
	third := sizedTestCache(imgCache)
	third.PreloadSync("U1", srv.URL+"/new.png")

	if got, want := log.get(), []string{"/old.png", "/new.png"}; !slices.Equal(got, want) {
		t.Errorf("requested %v; want %v", got, want)
	}
	if third.Get("U1") == "" {
		t.Error("avatar not rendered from the disk cache")
	}
}
