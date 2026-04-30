package image

import (
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/sync/singleflight"
)

// FetchRequest describes one image fetch.
type FetchRequest struct {
	Key    string      // cache key (e.g. "F0123ABCD-720" or "avatar-U123")
	URL    string      // remote URL
	Target image.Point // target downscale size in pixels (0 = no downscale)
}

// FetchResult is the decoded, downscaled image plus on-disk metadata.
type FetchResult struct {
	Img    image.Image
	Source string // path on disk
	Mime   string
}

// TeamAuth pairs a Slack workspace's xoxc token with its 'd' cookie.
// Both are required to authenticate fetches on files.slack.com.
type TeamAuth struct {
	TeamID  string
	Token   string // xoxc-...
	DCookie string
}

// Fetcher downloads images, stores raw bytes in Cache, decodes, and
// downscales. Concurrent fetches for the same Key are deduplicated;
// across-key concurrency is bounded by a semaphore so a channel full
// of images doesn't trigger Slack's CDN rate limiter (HTTP 429).
//
// For files.slack.com URLs the fetcher attaches per-team auth (xoxc
// Bearer + 'd' cookie). When the URL's team isn't in our token map
// (Slack Connect / shared channels), the fetcher tries each registered
// team's auth in order until one succeeds, then caches the result so
// subsequent fetches for that foreign team skip the search.
type Fetcher struct {
	cache *Cache
	http  *http.Client
	sf    singleflight.Group
	sem   chan struct{} // bounded concurrency for cross-key fetches

	// decoded caches the result of Cached() per (key, target) so
	// repeated calls don't re-decode the on-disk PNG and re-bilinear
	// downscale on every cache rebuild. Without this, a burst of
	// ImageReadyMsg events (e.g., older-history load completing for
	// 25 attachments) triggers N cache rebuilds × N decodes each
	// = N² synchronous decode work on the UI thread, making the app
	// appear hung for many seconds.
	decoded sync.Map // string("<key>|<wxh>") -> image.Image

	// auth state. authsByTeam holds an entry per registered workspace;
	// fallbacks is the same set as a slice (ordered) for sequential
	// retry on Slack Connect URLs. learnedAuths caches the foreign-team
	// -> auth mapping after a successful retry.
	authsByTeam   map[string]TeamAuth
	fallbacks     []TeamAuth
	learnedAuths  sync.Map // string -> TeamAuth
}

// fetchConcurrencyLimit caps the number of in-flight HTTP fetches.
// Slack's files.slack.com CDN rate-limits aggressive scraping (HTTP 429).
// 4 is the empirical sweet spot: large enough to keep the messages-pane
// responsive on a fresh channel switch, small enough to stay under the
// rate limit even when a busy channel has 20+ image attachments visible.
const fetchConcurrencyLimit = 4

// rateLimitMaxRetries / rateLimitInitialBackoff / rateLimitMaxBackoff
// govern automatic retry on HTTP 429 from files.slack.com. Slack's
// rate-limit window is short (a few seconds); exponential backoff
// recovers without user intervention. Cap at 3 attempts so a stuck
// 429 doesn't block forever.
const (
	rateLimitMaxRetries     = 3
	rateLimitInitialBackoff = 500 * time.Millisecond
	rateLimitMaxBackoff     = 4 * time.Second
)

// NewFetcher constructs a Fetcher. If client is nil, a default with a
// 10-second timeout is used. Auth is empty until SetAuths is called.
func NewFetcher(cache *Cache, client *http.Client) *Fetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Fetcher{
		cache:       cache,
		http:        client,
		sem:         make(chan struct{}, fetchConcurrencyLimit),
		authsByTeam: map[string]TeamAuth{},
	}
}

// SetAuths configures the per-workspace credentials used to
// authenticate files.slack.com fetches. Each TeamAuth must have a
// non-empty TeamID. The slice's order determines the order in which
// fallback auths are tried for foreign-team URLs (Slack Connect).
// Safe to call once at startup; not safe to mutate the input slice
// afterward.
func (f *Fetcher) SetAuths(auths []TeamAuth) {
	byTeam := make(map[string]TeamAuth, len(auths))
	fallbacks := make([]TeamAuth, 0, len(auths))
	for _, a := range auths {
		if a.TeamID == "" || a.Token == "" {
			continue
		}
		byTeam[a.TeamID] = a
		fallbacks = append(fallbacks, a)
	}
	f.authsByTeam = byTeam
	f.fallbacks = fallbacks
	f.learnedAuths = sync.Map{} // reset learned mappings on reconfig
}

// Fetch returns the decoded image, downloading and caching if needed.
// Cross-key concurrency is bounded; if the limit is full, additional
// callers wait until a slot frees up or ctx is canceled.
func (f *Fetcher) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	v, err, _ := f.sf.Do(req.Key, func() (any, error) {
		// Cache-only path doesn't need an HTTP slot; check first.
		if _, hit := f.cache.Get(req.Key); !hit {
			select {
			case f.sem <- struct{}{}:
				defer func() { <-f.sem }()
			case <-ctx.Done():
				return FetchResult{}, ctx.Err()
			}
		}
		return f.fetchInner(ctx, req)
	})
	if err != nil {
		return FetchResult{}, err
	}
	return v.(FetchResult), nil
}

func (f *Fetcher) fetchInner(ctx context.Context, req FetchRequest) (FetchResult, error) {
	path, hit := f.cache.Get(req.Key)
	if !hit {
		body, ct, err := f.download(ctx, req.URL)
		if err != nil {
			return FetchResult{}, err
		}
		ext := extFromMime(ct, req.URL)
		path, err = f.cache.Put(req.Key, ext, body)
		if err != nil {
			return FetchResult{}, err
		}
	}

	file, err := os.Open(path)
	if err != nil {
		return FetchResult{}, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		// Cache poisoning recovery: a previously persisted file isn't
		// decodable as an image (e.g., an HTML auth-failure response from
		// before this auth path was wired up). Evict so the next Fetch
		// re-downloads with the now-correct credentials.
		file.Close()
		f.cache.Delete(req.Key)
		return FetchResult{}, fmt.Errorf("decode %s: %w (cache evicted)", path, err)
	}

	if req.Target.X > 0 && req.Target.Y > 0 {
		img = downscale(img, req.Target)
	}

	mime := mimeFromExt(filepath.Ext(path))
	return FetchResult{Img: img, Source: path, Mime: mime}, nil
}

// download fetches url and returns its body bytes + Content-Type.
//
// For files.slack.com URLs the fetcher attaches per-team auth. If the
// URL's team isn't in our token map (Slack Connect), each registered
// team's auth is tried in order; the winning auth is cached for that
// foreign team. Auth failures are detected by either a 401/403 status
// or a 200 with a text/html body (Slack's login page).
//
// HTTP 429 responses are retried with exponential backoff up to
// rateLimitMaxRetries times before giving up.
func (f *Fetcher) download(ctx context.Context, url string) (body []byte, contentType string, err error) {
	authsToTry := f.authsForURL(url)
	// Always try at least one attempt; if we have no auths it's still
	// fine to fetch unauthenticated URLs (avatars on slack-edge.com).
	if len(authsToTry) == 0 {
		authsToTry = []TeamAuth{{}}
	}

	teamID := teamIDFromFilesURL(url)
	var lastErr error
	for _, auth := range authsToTry {
		body, ct, status, err := f.tryDownloadWithBackoff(ctx, url, auth)
		if err != nil {
			lastErr = err
			continue
		}
		if status == http.StatusOK && strings.HasPrefix(strings.ToLower(ct), "image/") {
			// Success. Remember which auth worked for foreign-team URLs.
			if teamID != "" {
				if _, known := f.authsByTeam[teamID]; !known && auth.TeamID != "" {
					f.learnedAuths.Store(teamID, auth)
					log.Printf("file auth: learned team %q is reachable via team %q's auth", teamID, auth.TeamID)
				}
			}
			return body, ct, nil
		}
		// Auth failure (HTML response or 401/403) — try next auth.
		lastErr = fmt.Errorf("fetch %s: HTTP %d ct=%q (auth failure?)", url, status, ct)
		log.Printf("file auth: attempt with team %q failed for %s (status=%d ct=%q); trying next",
			auth.TeamID, url, status, ct)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("fetch %s: no auth succeeded", url)
	}
	return nil, "", lastErr
}

// authsForURL returns the ordered list of auths to try for url.
//
// For non-Slack URLs (avatars on gravatar / slack-edge), returns an
// empty list so the request goes out unauthenticated.
//
// For files.slack.com URLs:
//   - If the URL's team is in our token map, return that auth alone.
//   - If we've previously learned an auth for this foreign team, return
//     that auth alone.
//   - Otherwise return the full ordered fallback list (Slack Connect:
//     try every workspace until one succeeds).
func (f *Fetcher) authsForURL(url string) []TeamAuth {
	teamID := teamIDFromFilesURL(url)
	if teamID == "" {
		return nil
	}
	if a, ok := f.authsByTeam[teamID]; ok {
		return []TeamAuth{a}
	}
	if v, ok := f.learnedAuths.Load(teamID); ok {
		if a, ok := v.(TeamAuth); ok {
			return []TeamAuth{a}
		}
	}
	return f.fallbacks
}

// tryDownloadWithBackoff issues one logical fetch with rate-limit
// retry. Returns (body, content-type, status, err). On HTTP 429 the
// request is retried with exponential backoff up to rateLimitMaxRetries
// times. On any other terminal status (success or non-429 failure) it
// returns immediately.
func (f *Fetcher) tryDownloadWithBackoff(ctx context.Context, url string, auth TeamAuth) ([]byte, string, int, error) {
	backoff := rateLimitInitialBackoff
	for attempt := 0; attempt <= rateLimitMaxRetries; attempt++ {
		body, ct, status, err := f.tryDownload(ctx, url, auth)
		if status != http.StatusTooManyRequests {
			return body, ct, status, err
		}
		if attempt == rateLimitMaxRetries {
			return body, ct, status, fmt.Errorf("fetch %s: HTTP 429 after %d retries", url, attempt+1)
		}
		log.Printf("file auth: HTTP 429 for %s (attempt %d/%d); backing off %s",
			url, attempt+1, rateLimitMaxRetries+1, backoff)
		select {
		case <-ctx.Done():
			return nil, "", 0, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > rateLimitMaxBackoff {
			backoff = rateLimitMaxBackoff
		}
	}
	return nil, "", 0, fmt.Errorf("fetch %s: unreachable", url)
}

// tryDownload issues a single HTTP GET with the given auth attached
// (if non-empty). Returns (body, content-type, status-code, err).
// Body is nil for non-200 responses; caller decides whether to treat
// them as terminal or retry.
func (f *Fetcher) tryDownload(ctx context.Context, url string, auth TeamAuth) ([]byte, string, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", 0, err
	}
	httpReq.Header.Set("User-Agent", "slk/inline-image-fetcher")
	if auth.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+auth.Token)
	}
	if auth.DCookie != "" {
		// Inline cookie header: a shared cookie jar can hold only one
		// 'd' value at a time but workspaces may have different ones.
		httpReq.Header.Set("Cookie", "d="+auth.DCookie)
	}
	resp, err := f.http.Do(httpReq)
	if err != nil {
		return nil, "", 0, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode != http.StatusOK {
		// Drain body for connection reuse, but we don't return it.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, ct, resp.StatusCode, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ct, resp.StatusCode, err
	}
	return body, ct, resp.StatusCode, nil
}

// teamIDFromFilesURL extracts the team ID embedded in a Slack file URL.
// Returns "" for URLs that aren't on files.slack.com or don't match a
// recognized path pattern.
func teamIDFromFilesURL(rawURL string) string {
	// Cheap host check before fully parsing.
	if !strings.Contains(rawURL, "files.slack.com") {
		return ""
	}
	// Find the path portion. We don't need full url.Parse for this.
	i := strings.Index(rawURL, "files.slack.com")
	if i < 0 {
		return ""
	}
	rest := rawURL[i+len("files.slack.com"):]
	// Strip any query string for cleanliness.
	if q := strings.IndexByte(rest, '?'); q >= 0 {
		rest = rest[:q]
	}
	for _, prefix := range []string{"/files-tmb/", "/files-pri/", "/files/"} {
		if !strings.HasPrefix(rest, prefix) {
			continue
		}
		seg := rest[len(prefix):]
		if j := strings.IndexByte(seg, '/'); j >= 0 {
			seg = seg[:j]
		}
		if prefix == "/files/" {
			return seg
		}
		if j := strings.IndexByte(seg, '-'); j >= 0 {
			return seg[:j]
		}
		return seg
	}
	return ""
}

// downscale fits img within target preserving the renderer's expectation;
// the renderer always wants exactly target pixels — so we always scale.
// (Avoids an extra branch and image-copy path.)
func downscale(img image.Image, target image.Point) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, target.X, target.Y))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

func extFromMime(contentType, url string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "image/png"):
		return "png"
	case strings.HasPrefix(ct, "image/jpeg"), strings.HasPrefix(ct, "image/jpg"):
		return "jpg"
	case strings.HasPrefix(ct, "image/gif"):
		return "gif"
	}
	// Fall back to URL extension.
	if i := strings.LastIndex(url, "."); i >= 0 {
		ext := strings.ToLower(strings.TrimPrefix(url[i:], "."))
		if ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "gif" {
			if ext == "jpeg" {
				return "jpg"
			}
			return ext
		}
	}
	return "png"
}

func mimeFromExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	}
	return "application/octet-stream"
}

// Bytes reads the cached file's raw bytes.
func (f *Fetcher) Bytes(key string) ([]byte, error) {
	path, ok := f.cache.Get(key)
	if !ok {
		return nil, fmt.Errorf("not cached: %s", key)
	}
	return os.ReadFile(path)
}

// Cached returns the decoded image and true if it's already in the on-disk
// cache. Never starts a network download. When target is positive on both
// axes, the returned image is downscaled to those pixel dimensions; pass
// image.Point{} (zero) to skip downscale.
//
// Decoded results are memoized per (key, target) so repeated calls
// don't re-decode the on-disk PNG and re-bilinear downscale on every
// hit. The memo is invalidated when the cache file is evicted (via
// Cache.Delete or LRU eviction); a future cache rebuild that finds
// the file gone falls back through the disk path and re-populates.
//
// If a file is found but fails to decode (e.g., a stale auth-failure HTML
// response was persisted under an image extension), the entry is evicted
// from the cache and the caller is told it's not present, so a subsequent
// Fetch re-downloads.
func (f *Fetcher) Cached(key string, target image.Point) (image.Image, bool) {
	memoKey := decodedMemoKey(key, target)
	if v, ok := f.decoded.Load(memoKey); ok {
		if img, ok := v.(image.Image); ok {
			return img, true
		}
	}
	path, ok := f.cache.Get(key)
	if !ok {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		file.Close()
		f.cache.Delete(key)
		f.decoded.Delete(memoKey)
		return nil, false
	}
	if target.X > 0 && target.Y > 0 {
		img = downscale(img, target)
	}
	f.decoded.Store(memoKey, img)
	return img, true
}

func decodedMemoKey(key string, target image.Point) string {
	return fmt.Sprintf("%s|%dx%d", key, target.X, target.Y)
}

// ThumbSpec is one Slack thumbnail variant.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}

// PickThumb selects the smallest thumb whose dimensions are >= target on
// both axes. Falls back to the largest available if none satisfy.
// suffix is a short string usable in cache keys (e.g. "720").
func PickThumb(thumbs []ThumbSpec, target image.Point) (url, suffix string) {
	if len(thumbs) == 0 {
		return "", ""
	}
	// Sort ascending by max(W, H).
	sorted := make([]ThumbSpec, len(thumbs))
	copy(sorted, thumbs)
	sort.Slice(sorted, func(i, j int) bool {
		return max(sorted[i].W, sorted[i].H) < max(sorted[j].W, sorted[j].H)
	})
	for _, t := range sorted {
		if t.W >= target.X && t.H >= target.Y {
			return t.URL, fmt.Sprintf("%d", max(t.W, t.H))
		}
	}
	last := sorted[len(sorted)-1]
	return last.URL, fmt.Sprintf("%d", max(last.W, last.H))
}
