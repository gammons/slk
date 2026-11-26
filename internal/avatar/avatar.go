// Package avatar downloads Slack user avatars and renders them at a
// fixed 4×2 cell footprint. Storage delegates to internal/image's
// shared cache; rendering uses kitty graphics on capable terminals
// (sharper), sixel on sixel terminals (SetSixel), and half-block (▀)
// elsewhere. A sixel avatar is encoded once per user; the content panes
// place it like an inline image, and the sixel painter re-sends it only
// when its slot or the text under it changes, not on every redraw.
package avatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gammons/slk/internal/debuglog"
	imgpkg "github.com/gammons/slk/internal/image"
)

const (
	// AvatarCols is the width of the rendered avatar in terminal columns.
	AvatarCols = 4
	// AvatarRows is the height in terminal rows. Half-block uses 2 pixel
	// rows per cell row; kitty fits the source image to AvatarCols×AvatarRows
	// cells and the terminal scales pixels appropriately.
	AvatarRows = 2
	// SmallCols×SmallRows is the reply-row avatar (kitty only).
	SmallCols = 2
	SmallRows = 1
)

// avatarPreloadWorkers caps the number of avatar Preload jobs that
// can be downloading + rendering concurrently. Bounding this matters
// because the lazy AvatarFunc path can call Preload for many distinct
// userIDs in a single frame when a scrollback first paints; without
// a bound, each unique user spawned a goroutine that could race to
// write its kitty graphics APC upload to os.Stdout, and a burst of
// hundreds of those is enough to make a kitty terminal visibly stall
// while it decodes them. 8 keeps the disk/network subsystems busy
// without saturating the kitty graphics queue.
const avatarPreloadWorkers = 8

// avatarPreloadQueueSize caps the worker pool's pending-job queue. In
// the lazy-load model this is bounded by visible-history size in
// practice (an extreme channel with thousands of unique authors in a
// scrollback can still be drained linearly). When the queue is full,
// Preload drops the job AND removes the userID from inflight so a
// subsequent retry can re-enqueue. 256 covers ordinary workloads with
// plenty of headroom.
const avatarPreloadQueueSize = 256

// sizedAvatarPx is the CDN variant fetched in place of a Slack original
// avatar (see SizedURL). 72px is ~2x the 4x2-cell slot at a typical
// 9x17 cell, and a few KB where originals run to 3000x3000 and MBs.
const sizedAvatarPx = 72

// Cache wraps an image.Fetcher and memoizes rendered ANSI strings per user.
//
// When the active rendering protocol is kitty, the avatar's "render"
// is a small block of unicode-placeholder cells; the actual image
// upload happens via the kitty side channel (image.KittyOutput) on
// first render of a given user, deduped by the kitty registry's
// per-(key,target) tracking. When the protocol is not kitty (sixel,
// half-block, off, ...), the avatar renders as half-block ANSI text.
type Cache struct {
	fetcher  *imgpkg.Fetcher
	kitty    *imgpkg.KittyRenderer // nil when not using kitty
	useKitty bool
	useSixel bool // also encode a sixel image per avatar; see SetSixel

	mu      sync.RWMutex
	renders map[string]string      // userID -> rendered ANSI string
	small   map[string]string      // userID -> SmallCols×SmallRows render
	sixel   map[string]sixelAvatar // userID -> sixel image, when useSixel

	// gen counts Invalidate calls. A preload started under one
	// generation drops its result when the generation has moved on by
	// the time it finishes: renders and sixel encodings are sized
	// against the cell pixels measured at render time, so a late
	// worker must not re-store renders encoded at the old size after
	// the caches were cleared.
	gen atomic.Uint64

	// inflight tracks userIDs whose Preload is currently in-flight (or
	// already rendered). Acts as a dedup gate so a hot render path
	// calling Preload on every redraw doesn't stampede the fetcher:
	// the first call enters the map and starts work; subsequent calls
	// short-circuit. Entries are NOT removed on completion — once a
	// user has been rendered (or attempted-and-failed) we don't need
	// to retry on every subsequent miss. Callers that need to force a
	// refetch (theme change, avatar URL change) should construct a new
	// Cache.
	inflight sync.Map // userID -> struct{}

	// onReady is invoked from the worker goroutine after a render is
	// stored in c.renders. Hosts use it to dispatch a bubbletea
	// invalidation message (AvatarReadyMsg). May be nil; nil-safe.
	onReady func(userID string)

	// preloadCh feeds the bounded worker pool. Preload enqueues jobs
	// here; workers drain. Closed jobs are not supported (Cache lives
	// for the program's lifetime). nil disables async pool dispatch
	// (PreloadSync still works directly), which the kitty/parity tests
	// rely on.
	preloadCh chan preloadJob

	// sizeURL maps the URL handed to Preload to the URL actually
	// fetched: SizedURL in production. Tests replace it so httptest
	// URLs can stand in for Slack's. nil fetches the URL as given.
	sizeURL func(string) string
}

// sixelAvatar is one user's avatar encoded as sixel at the avatar's
// cell footprint, with its placement identity: the user and a hash of
// the bytes, so a changed avatar is a different image to the painter.
type sixelAvatar struct {
	key   string
	bytes []byte
}

type preloadJob struct {
	userID    string
	avatarURL string
}

// NewCache creates an avatar cache backed by the shared image.Fetcher.
// kitty may be nil; in that case (or when useKitty is false) the cache
// renders avatars via half-block regardless of any kitty support
// elsewhere in the app.
func NewCache(fetcher *imgpkg.Fetcher, kitty *imgpkg.KittyRenderer, useKitty bool) *Cache {
	return newCacheForTest(fetcher, kitty, useKitty, avatarPreloadWorkers, avatarPreloadQueueSize)
}

// newCacheForTest is the underlying constructor; production code uses
// NewCache, tests use this to dial worker count and queue size to
// produce deterministic backpressure behavior.
func newCacheForTest(fetcher *imgpkg.Fetcher, kitty *imgpkg.KittyRenderer, useKitty bool, workers, queueSize int) *Cache {
	c := &Cache{
		fetcher:   fetcher,
		kitty:     kitty,
		useKitty:  useKitty && kitty != nil,
		renders:   make(map[string]string),
		small:     make(map[string]string),
		sixel:     make(map[string]sixelAvatar),
		preloadCh: make(chan preloadJob, queueSize),
		sizeURL:   func(u string) string { return SizedURL(u, sizedAvatarPx) },
	}
	for i := 0; i < workers; i++ {
		go c.preloadWorker()
	}
	return c
}

func (c *Cache) preloadWorker() {
	for job := range c.preloadCh {
		c.preloadInner(job.userID, job.avatarURL)
	}
}

// SetSixel makes the cache encode every avatar as sixel too, for a
// terminal that draws sixel: GetSixel then returns it, beside the
// half-block render Get keeps returning as its fallback. Ignored with
// kitty, which draws avatars itself. Call once at startup, before any
// Preload.
func (c *Cache) SetSixel(on bool) {
	c.useSixel = on && !c.useKitty
}

// SetOnReady registers a callback invoked once per userID after a
// successful Preload completes. Not called for fetch failures. Safe to
// call once at startup before any Preload; concurrent reassignment is
// not supported.
func (c *Cache) SetOnReady(fn func(userID string)) {
	c.onReady = fn
}

// Invalidate drops every rendered avatar and releases the in-flight
// dedup set, so the lazy Preload path re-fetches exactly as it would
// for a user's first render. Call it when the terminal's cell size in
// pixels changed: renders and sixel encodings are sized against that
// measurement (the encoded pixel dimensions are the on-screen size),
// so after it they must be rebuilt from the fetcher's sources, which
// the disk cache serves without the network. Preloads already running
// when Invalidate lands drop their results instead of re-storing
// renders encoded at the old size.
func (c *Cache) Invalidate() {
	c.gen.Add(1)
	c.mu.Lock()
	c.renders = make(map[string]string)
	c.small = make(map[string]string)
	c.sixel = make(map[string]sixelAvatar)
	c.mu.Unlock()
	c.inflight.Range(func(key, _ any) bool {
		c.inflight.Delete(key)
		return true
	})
}

// Preload enqueues a background download+render for an avatar. Bounded
// by the worker pool (see avatarPreloadWorkers). Idempotent: repeated
// calls for the same userID short-circuit via the inflight set. If the
// worker queue is full, the job is dropped AND the inflight slot is
// released so a later retry can re-enqueue (otherwise a dropped userID
// would be stuck "in flight" with no work pending and its avatar would
// never appear). avatarURL of subsequent calls for the same userID is
// ignored — the first call wins.
func (c *Cache) Preload(userID, avatarURL string) {
	if avatarURL == "" {
		return
	}
	if _, loaded := c.inflight.LoadOrStore(userID, struct{}{}); loaded {
		return
	}
	if c.preloadCh == nil {
		// No pool wired (e.g. zero-value Cache in a test). Fall back
		// to a one-shot goroutine so callers still see eventual work.
		go c.preloadInner(userID, avatarURL)
		return
	}
	select {
	case c.preloadCh <- preloadJob{userID: userID, avatarURL: avatarURL}:
	default:
		// Queue full. Release the inflight slot so the next caller can
		// retry; we'd rather re-attempt later than wedge this user.
		c.inflight.Delete(userID)
	}
}

// PreloadSync downloads and renders synchronously. Unlike Preload, this
// does NOT participate in the inflight dedup set — it's the worker
// entry point and tests' deterministic path. Callers that want dedup
// should use Preload.
func (c *Cache) PreloadSync(userID, avatarURL string) {
	c.preloadInner(userID, avatarURL)
}

func (c *Cache) preloadInner(userID, avatarURL string) {
	if avatarURL == "" {
		return
	}
	gen := c.gen.Load()
	// Source size differs by protocol:
	//   - half-block: (AvatarCols, AvatarRows*2) gives the renderer
	//     exactly the pixel grid it samples, matching the original
	//     pre-kitty pipeline byte-for-byte (parity test relies on this).
	//   - kitty: skip the fetcher's downscale (Target = zero point) so
	//     the renderer's own pixel-target resize starts from the highest
	//     available source resolution. With a 32×32 source (Slack's
	//     image_32) and kitty's internal target of ~32×32, this is
	//     effectively identity scaling — sharp pixels.
	//   - sixel: as kitty, since the sixel encoder scales to the cell
	//     size in pixels; the half-block fallback scales down itself.
	target := image.Pt(AvatarCols, AvatarRows*2)
	if c.useKitty || c.useSixel {
		target = image.Point{}
	}
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
	rendered := c.renderAvatar(userID, res.Img)
	var sx sixelAvatar
	if c.useSixel {
		sx = encodeSixel(userID, res.Img)
	}
	c.mu.Lock()
	if gen != c.gen.Load() {
		// The cell metrics changed while this preload ran: the render
		// and sixel encoding above were built against the old pixel
		// size. Drop them; a fresh preload (the lazy path re-runs on
		// the next miss) encodes against the new size.
		c.mu.Unlock()
		return
	}
	c.renders[userID] = rendered
	if sx.bytes != nil {
		c.sixel[userID] = sx
	}
	c.mu.Unlock()
	if c.onReady != nil {
		c.onReady(userID)
	}
}

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
	return avatarKey(userID) + "-" + hex.EncodeToString(sum[:])[:12]
}

// avatarKey is the "avatar-<userID>" prefix of every key an avatar is
// cached under. The ID is path-escaped: a bot author ID carries the
// sender's username after a "/" (core.BotAuthorID), and the disk cache
// uses the key as a file name. Slack user and bot IDs are unchanged.
func avatarKey(userID string) string {
	return "avatar-" + url.PathEscape(userID)
}

// Get returns the rendered avatar string, or empty if not cached.
func (c *Cache) Get(userID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.renders[userID]
}

// GetSixel returns userID's avatar as a sixel image and its placement
// key, or ok false when the cache is not in sixel mode (SetSixel) or the
// avatar is not loaded yet. Get returns the same avatar as half-block,
// which a pane draws while the sixel image cannot be (a pane edge cuts
// it).
func (c *Cache) GetSixel(userID string) (key string, bytes []byte, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sx, ok := c.sixel[userID]
	return sx.key, sx.bytes, ok
}

// encodeSixel encodes img as sixel at the avatar's footprint. The zero
// value when the encoder fails; the caller then has only the half-block
// render, which is what a pane draws without a sixel image.
func encodeSixel(userID string, img image.Image) sixelAvatar {
	out := imgpkg.NewSixelRenderer().Render(img, image.Pt(AvatarCols, AvatarRows))
	if out.OnFlush == nil {
		return sixelAvatar{} // the encoder fell back to half-block
	}
	var buf bytes.Buffer
	if err := out.OnFlush(&buf); err != nil || buf.Len() == 0 {
		return sixelAvatar{}
	}
	sum := sha256.Sum256(buf.Bytes())
	return sixelAvatar{key: avatarKey(userID) + "-" + hex.EncodeToString(sum[:8]), bytes: buf.Bytes()}
}

// GetSmall returns the reply-row avatar, rendering it on first use, or
// empty if the avatar is not loaded yet or not on kitty (a 2×1
// half-block avatar is unreadable).
func (c *Cache) GetSmall(userID string) string {
	c.mu.RLock()
	small, loaded := c.small[userID], c.renders[userID] != ""
	c.mu.RUnlock()
	if small != "" || !loaded {
		return small
	}
	if small = c.renderSmall(userID); small != "" {
		c.mu.Lock()
		c.small[userID] = small
		c.mu.Unlock()
	}
	return small
}

// renderSmall renders the source renderAvatar registered at the reply-row
// size and uploads it, as renderAvatar does. "" off kitty.
func (c *Cache) renderSmall(userID string) string {
	if !c.useKitty {
		return ""
	}
	out := c.kitty.RenderKey(avatarKey(userID), image.Pt(SmallCols, SmallRows))
	if out.OnFlush != nil {
		_ = out.OnFlush(imgpkg.KittyOutput)
	}
	return joinLines(out.Lines)
}

// renderAvatar produces the avatar's rendered string for the active
// protocol. Kitty path: SetSource + RenderKey, immediately drain the
// upload escape to the kitty side channel (the registry's fresh-flag
// guarantees this fires only once per user), and return the
// placeholder cells. Half-block path: encode and return.
func (c *Cache) renderAvatar(userID string, img image.Image) string {
	target := image.Pt(AvatarCols, AvatarRows)
	if c.useKitty {
		key := avatarKey(userID)
		c.kitty.SetSource(key, img)
		out := c.kitty.RenderKey(key, target)
		// Fire the upload escape NOW (single-threaded from
		// PreloadSync's perspective; the side-channel writer handles
		// concurrency). After this, the kitty registry returns
		// fresh=false for subsequent renders, so OnFlush is nil.
		if out.OnFlush != nil {
			_ = out.OnFlush(imgpkg.KittyOutput)
		}
		return joinLines(out.Lines)
	}
	out := imgpkg.HalfBlockRenderer{}.Render(img, target)
	return joinLines(out.Lines)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
