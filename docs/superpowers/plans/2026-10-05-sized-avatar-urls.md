# Sized Avatar URLs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fetch Slack avatars as ~72px CDN thumbnails instead of original uploads (up to 3000×3000 / 4.3 MB), under a URL-derived cache key.

**Architecture:** A pure URL-rewrite helper, `avatar.SizedURL`, turns Slack `_original` avatar URLs into the sized `avatars.slack-edge.com/…_72.<ext>` form. `avatar.Cache.preloadInner`, the single choke point every avatar fetch passes through, applies it, falls back to the original URL on any fetch error, and keys the disk cache on `avatar-<userID>-<12 hex of sha256(url)>` so the old oversized files are never read again.

**Tech Stack:** Go, stdlib `testing` + `net/http/httptest`, `regexp`, `crypto/sha256`.

**Spec:** `docs/superpowers/specs/2026-10-05-sized-avatar-urls-design.md`

## Global Constraints

- Tests: plain `testing.T`, stdlib only, white-box (`package avatar`). No testify/gomock.
- Sized pixel size in production: package constant `sizedAvatarPx = 72`.
- Rewritten URL form: `https://avatars.slack-edge.com/<date>/<name>_<px>.<ext>`, `<ext>` kept verbatim (a `.png` rewritten to `.jpg` is a 403).
- Inputs rewritten: exactly `https://s3-<region>.amazonaws.com/slack-files2/avatars/<YYYY-MM-DD>/<name>_original.<ext>` and `https://avatars.slack-edge.com/<YYYY-MM-DD>/<name>_original.<ext>`. Everything else is returned unchanged.
- Cache key: `avatar-<userID>-<first 12 hex chars of sha256(fetched URL)>`.
- The kitty source key in `renderAvatar` stays `avatar-<userID>`.
- No changes to callers in `cmd/slk` or `internal/bootstrap`; `users.avatar_url` keeps storing what Slack sent.
- No cleanup of old cache files; the LRU evicts them.
- A new reusable helper is added to the AGENTS.md "Shared code" tables in the same commit.
- Before the PR: `go build ./...`, `go vet ./...`, `go test ./... -race`, `gofmt -l .` empty, `golangci-lint run`.

## Review Focus

- A sized URL that answers `200` with a non-image body (an HTML error page): this must fall back to the original, not leave the avatar blank. Pinned in Task 2.
- An uppercase extension (`_original.JPG`): the extension must be kept verbatim. Pinned in Task 1.
- An `http://` (not `https://`) or query-string original: this must be left unchanged, never half-rewritten. Pinned in Task 1.
- A URL with no rewrite (Gravatar, a bot icon, an already-sized `_32`) that fails: exactly one request, with no fallback loop. Pinned in Task 2.
- The same user arriving later with a different avatar URL, against a warm disk cache: the new URL must be fetched, not the stale file served. Pinned in Task 2.

---

### Task 1: `SizedURL` helper

**Files:**
- Create: `internal/avatar/sizedurl.go`
- Create: `internal/avatar/sizedurl_test.go`
- Modify: `AGENTS.md` (the "Text and rendering" table in "Shared code")

**Interfaces:**
- Consumes: nothing.
- Produces: `func SizedURL(orig string, px int) string` in package `avatar`. It returns `orig` unchanged when `px <= 0` or when `orig` is not one of the two original-avatar shapes.

- [ ] **Step 1: Write the failing test**

Create `internal/avatar/sizedurl_test.go`:

```go
package avatar

import "testing"

func TestSizedURL(t *testing.T) {
	const s3 = "https://s3-us-west-2.amazonaws.com/slack-files2/avatars/"
	const edge = "https://avatars.slack-edge.com/"
	cases := []struct {
		name string
		in   string
		px   int
		want string
	}{
		// Rewritten.
		{"s3 jpg", s3 + "2023-05-31/5349743656757_da76a902a76dda11cdbb_original.jpg", 72,
			edge + "2023-05-31/5349743656757_da76a902a76dda11cdbb_72.jpg"},
		{"s3 png keeps png", s3 + "2026-04-05/10849023461394_23b3ca254f8d12a7cc34_original.png", 72,
			edge + "2026-04-05/10849023461394_23b3ca254f8d12a7cc34_72.png"},
		{"s3 other region", "https://s3-eu-west-1.amazonaws.com/slack-files2/avatars/2024-01-02/1_ab_original.jpg", 72,
			edge + "2024-01-02/1_ab_72.jpg"},
		{"edge gif", edge + "2024-01-02/111_abc_original.gif", 72,
			edge + "2024-01-02/111_abc_72.gif"},
		{"uppercase ext kept verbatim", s3 + "2024-01-02/1_ab_original.JPG", 72,
			edge + "2024-01-02/1_ab_72.JPG"},
		{"other px", s3 + "2024-01-02/1_ab_original.png", 48,
			edge + "2024-01-02/1_ab_48.png"},

		// Unchanged.
		{"already sized", edge + "2026-04-08/10866830453622_8c4c7dfa336e8ea7c80b_32.png", 72,
			edge + "2026-04-08/10866830453622_8c4c7dfa336e8ea7c80b_32.png"},
		{"gravatar", "https://secure.gravatar.com/avatar/abc123?s=32&d=https%3A%2F%2Fa.slack-edge.com%2Fdf10d%2Fimg%2Favatars%2Fava_0001-32.png", 72,
			"https://secure.gravatar.com/avatar/abc123?s=32&d=https%3A%2F%2Fa.slack-edge.com%2Fdf10d%2Fimg%2Favatars%2Fava_0001-32.png"},
		{"query string", s3 + "2024-01-02/1_ab_original.jpg?x=1", 72,
			s3 + "2024-01-02/1_ab_original.jpg?x=1"},
		{"fragment", s3 + "2024-01-02/1_ab_original.jpg#f", 72,
			s3 + "2024-01-02/1_ab_original.jpg#f"},
		{"lookalike host", "https://avatars.slack-edge.com.evil.example/2024-01-02/1_ab_original.png", 72,
			"https://avatars.slack-edge.com.evil.example/2024-01-02/1_ab_original.png"},
		{"lookalike s3 host", "https://s3-us-west-2.amazonaws.com.evil.example/slack-files2/avatars/2024-01-02/1_ab_original.png", 72,
			"https://s3-us-west-2.amazonaws.com.evil.example/slack-files2/avatars/2024-01-02/1_ab_original.png"},
		{"missing date", edge + "1_ab_original.png", 72,
			edge + "1_ab_original.png"},
		{"http scheme", "http://avatars.slack-edge.com/2024-01-02/1_ab_original.png", 72,
			"http://avatars.slack-edge.com/2024-01-02/1_ab_original.png"},
		{"empty", "", 72, ""},
		{"zero px", s3 + "2024-01-02/1_ab_original.png", 0,
			s3 + "2024-01-02/1_ab_original.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SizedURL(tc.in, tc.px); got != tc.want {
				t.Errorf("SizedURL(%q, %d)\n got %q\nwant %q", tc.in, tc.px, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/avatar -run TestSizedURL -count=1`
Expected: build failure, `undefined: SizedURL`.

- [ ] **Step 3: Write the implementation**

Create `internal/avatar/sizedurl.go`:

```go
package avatar

import (
	"fmt"
	"regexp"
)

// originalAvatarRE matches the two forms Slack uses for a user's
// original avatar upload: the S3 bucket URL that boot, edge users/info
// and bootstrap revalidation carry as image_original, and the same file
// on the avatar CDN. Anchored at both ends so a query string, a
// fragment, or a lookalike host never matches.
var originalAvatarRE = regexp.MustCompile(
	`^https://(?:s3-[a-z0-9-]+\.amazonaws\.com/slack-files2/avatars|avatars\.slack-edge\.com)` +
		`/(\d{4}-\d{2}-\d{2})/([^/?#]+)_original\.([A-Za-z0-9]+)$`)

// SizedURL rewrites a Slack original-avatar URL to the avatar CDN's
// px-sized variant of the same file, keeping the extension (the CDN
// 403s on a mismatched one). Any other URL, including Gravatar,
// already-sized and bot-icon URLs, is returned unchanged, so it is safe
// to call on every avatar URL.
func SizedURL(orig string, px int) string {
	if px <= 0 {
		return orig
	}
	m := originalAvatarRE.FindStringSubmatch(orig)
	if m == nil {
		return orig
	}
	return fmt.Sprintf("https://avatars.slack-edge.com/%s/%s_%d.%s", m[1], m[2], px, m[3])
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/avatar -run TestSizedURL -count=1 -v`
Expected: PASS, all subtests.

- [ ] **Step 5: Add the helper to AGENTS.md**

In `AGENTS.md`, in the "Text and rendering" table under "Shared code", add this row directly after the `| User IDs mentioned in message text | ... |` row:

```markdown
| Slack original-avatar URL → sized CDN URL (e.g. 72px) | `avatar.SizedURL(orig, px)`; returns non-Slack-original URLs unchanged |
```

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/avatar
git add internal/avatar/sizedurl.go internal/avatar/sizedurl_test.go AGENTS.md
git commit -m "feat(avatar): add SizedURL for Slack original-avatar URLs"
```

Expected: `gofmt -l` prints nothing.

---

### Task 2: Fetch the sized URL, fall back, key on the URL

**Files:**
- Modify: `internal/avatar/avatar.go` (imports; the `Cache` struct; `newCacheForTest`; `preloadInner`; plus a new `fetch` method and `fetchKey` func)
- Create: `internal/avatar/sized_test.go`

**Interfaces:**
- Consumes: `SizedURL(orig string, px int) string` from Task 1.
- Produces (all unexported, package `avatar`):
  - `const sizedAvatarPx = 72`
  - field `Cache.sizeURL func(string) string`, defaulted in `newCacheForTest` to `func(u string) string { return SizedURL(u, sizedAvatarPx) }`. A nil value means "fetch the URL as given".
  - `func (c *Cache) fetch(userID, url string, target image.Point) (imgpkg.FetchResult, error)`
  - `func fetchKey(userID, url string) string`, which returns `"avatar-" + userID + "-" + hex(sha256(url))[:12]`

Background for the implementer:
- Every avatar fetch goes through `preloadInner`. `PreloadSync` calls it directly and synchronously, which is what these tests use, so they need no draining or sentinels.
- `imgpkg.Fetcher.Fetch` returns an error for a non-200 status, and also for a 200 whose `Content-Type` is not `image/*` (`download` in `internal/image/fetcher.go`). With no auths configured, it makes exactly one HTTP request per URL.
- The fetcher's disk cache is consulted by key before any HTTP request. That is why the key has to change for the old full-size files to stop being served.

- [ ] **Step 1: Write the failing tests**

Create `internal/avatar/sized_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/avatar -run 'TestPreload_|TestFetchKey' -count=1`
Expected: build failure, with `c.sizeURL undefined (type *Cache has no field or method sizeURL)` and `undefined: fetchKey`.

- [ ] **Step 3: Add the imports, constant and field**

In `internal/avatar/avatar.go`, replace the import block with:

```go
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"strings"
	"sync"

	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
)
```

Directly after the `avatarPreloadQueueSize` constant, add:

```go
// sizedAvatarPx is the CDN variant fetched in place of a Slack original
// avatar (see SizedURL). 72px is ~2x the 4x2-cell slot at a typical
// 9x17 cell, and a few KB where originals run to 3000x3000 and MBs.
const sizedAvatarPx = 72
```

In the `Cache` struct, after the `preloadCh chan preloadJob` field, add:

```go

	// sizeURL maps the URL handed to Preload to the URL actually
	// fetched: SizedURL in production. Tests replace it so httptest
	// URLs can stand in for Slack's. nil fetches the URL as given.
	sizeURL func(string) string
```

In `newCacheForTest`, add the field to the `&Cache{...}` literal, after `preloadCh`:

```go
		sizeURL:   func(u string) string { return SizedURL(u, sizedAvatarPx) },
```

- [ ] **Step 4: Fetch sized, fall back, key on the URL**

In `preloadInner`, replace:

```go
	res, err := c.fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    "avatar-" + userID,
		URL:    avatarURL,
		Target: target,
	})
	if err != nil {
		return
	}
```

with:

```go
	fetchURL := avatarURL
	if c.sizeURL != nil {
		fetchURL = c.sizeURL(avatarURL)
	}
	res, err := c.fetch(userID, fetchURL, target)
	if err != nil && fetchURL != avatarURL {
		debuglog.ImgFetch("avatar: sized fetch failed user=%s url=%s err=%v; falling back to original",
			userID, fetchURL, err)
		res, err = c.fetch(userID, avatarURL, target)
	}
	if err != nil {
		return
	}
```

Directly after `preloadInner`, add:

```go
// fetch downloads one avatar URL, or reads it from the disk cache,
// under that URL's key.
func (c *Cache) fetch(userID, url string, target image.Point) (imgpkg.FetchResult, error) {
	return c.fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    fetchKey(userID, url),
		URL:    url,
		Target: target,
	})
}

// fetchKey is the disk-cache key for one user's avatar at one URL. The
// URL hash means a changed avatar (a new URL) is fetched fresh, and the
// older `avatar-<userID>` entries, which held full-size originals, are
// never read again; the image cache's LRU evicts them.
func fetchKey(userID, url string) string {
	sum := sha256.Sum256([]byte(url))
	return "avatar-" + userID + "-" + hex.EncodeToString(sum[:])[:12]
}
```

Leave `renderAvatar`'s kitty key (`"avatar-" + userID`) unchanged.

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/avatar -race -count=1 -v 2>&1 | grep -E '^(=== RUN|--- FAIL|FAIL|ok|PASS)' | grep -v '=== RUN'`
Expected: `ok  	github.com/gammons/slk/internal/avatar`, with no `--- FAIL`. This includes the pre-existing kitty, parity, dedup and backpressure tests: their `httptest` URLs never match `SizedURL`, so they take the no-rewrite path.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/avatar
go vet ./internal/avatar
git add internal/avatar/avatar.go internal/avatar/sized_test.go
git commit -m "fix(avatar): fetch 72px CDN avatars instead of Slack originals"
```

Expected: `gofmt -l` and `go vet` print nothing.

---

### Task 3: Whole-branch verification

No code changes. These are the AGENTS.md pre-PR checks plus a live check against the spec's success criteria.

**Files:** none.

**Interfaces:** consumes Tasks 1 and 2; produces nothing.

- [ ] **Step 1: Build, vet, format**

Run: `go build ./... && go vet ./... && gofmt -l .`
Expected: no output.

- [ ] **Step 2: Full test suite under the race detector**

Run: `go test ./... -race -count=1 2>&1 | grep -v -E '^(ok|\?)'`
Expected: no output. This takes about 47s.

- [ ] **Step 3: Lint**

Run: `golangci-lint run`
Expected: `0 issues.` (the repo pins v2.13.1; config in `.golangci.yml`).

- [ ] **Step 4: Live check (done by the human, who has the Slack session)**

Ask your human partner to run slk with `SLK_DEBUG=1`, open a channel with many human authors, quit, and then run:

```bash
grep -E 'decode: key=avatar' /tmp/slk-debug.log | sed -E 's/.*key=([^ ]+).*dur_ms=([0-9]+) dims=\(([0-9,]+)\).*/\2ms \3 \1/' | sort -rn | head
grep -c 'avatar: sized fetch failed' /tmp/slk-debug.log
ls -laS ~/.cache/slk/images/avatar-*-* | head
```

Expected:
- Decode lines for Slack-hosted avatars show `72,72` dims and `0ms` or `1ms`. Any remaining large ones should be the unrewritten Gravatar or bot shapes, which are already small.
- The fallback count is 0, or small and explained.
- New `avatar-<userID>-<hash>.*` files are a few KB each.

This live check is not a merge gate for subagents. Report the results back to your human partner.

