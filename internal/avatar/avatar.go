// Package avatar downloads Slack user avatars and renders them at a
// fixed 4×2 cell footprint. Storage delegates to internal/image's
// shared cache; rendering uses kitty graphics on capable terminals
// (sharper) and falls back to half-block (▀) elsewhere. Sixel is
// intentionally NOT used for avatars: re-emitting sixel byte streams
// per visible avatar per redraw would dominate the bandwidth budget.
package avatar

import (
	"context"
	"image"
	"strings"
	"sync"

	imgpkg "github.com/gammons/slk/internal/image"
)

const (
	// AvatarCols is the width of the rendered avatar in terminal columns.
	AvatarCols = 4
	// AvatarRows is the height in terminal rows. Half-block uses 2 pixel
	// rows per cell row; kitty fits the source image to AvatarCols×AvatarRows
	// cells and the terminal scales pixels appropriately.
	AvatarRows = 2
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

	mu      sync.RWMutex
	renders map[string]string // userID -> rendered ANSI string

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
		preloadCh: make(chan preloadJob, queueSize),
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

// SetOnReady registers a callback invoked once per userID after a
// successful Preload completes. Not called for fetch failures. Safe to
// call once at startup before any Preload; concurrent reassignment is
// not supported.
func (c *Cache) SetOnReady(fn func(userID string)) {
	c.onReady = fn
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
	// Source size differs by protocol:
	//   - half-block: (AvatarCols, AvatarRows*2) gives the renderer
	//     exactly the pixel grid it samples, matching the original
	//     pre-kitty pipeline byte-for-byte (parity test relies on this).
	//   - kitty: skip the fetcher's downscale (Target = zero point) so
	//     the renderer's own pixel-target resize starts from the highest
	//     available source resolution. With a 32×32 source (Slack's
	//     image_32) and kitty's internal target of ~32×32, this is
	//     effectively identity scaling — sharp pixels.
	target := image.Pt(AvatarCols, AvatarRows*2)
	if c.useKitty {
		target = image.Point{}
	}
	res, err := c.fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
		Key:    "avatar-" + userID,
		URL:    avatarURL,
		Target: target,
	})
	if err != nil {
		return
	}
	rendered := c.renderAvatar(userID, res.Img)
	c.mu.Lock()
	c.renders[userID] = rendered
	c.mu.Unlock()
	if c.onReady != nil {
		c.onReady(userID)
	}
}

// Get returns the rendered avatar string, or empty if not cached.
func (c *Cache) Get(userID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.renders[userID]
}

// renderAvatar produces the avatar's rendered string for the active
// protocol. Kitty path: SetSource + RenderKey, immediately drain the
// upload escape to the kitty side channel (the registry's fresh-flag
// guarantees this fires only once per user), and return the
// placeholder cells. Half-block path: encode and return.
func (c *Cache) renderAvatar(userID string, img image.Image) string {
	target := image.Pt(AvatarCols, AvatarRows)
	if c.useKitty {
		key := "avatar-" + userID
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
