// internal/ui/panelcache.go
//
// Per-panel render caches.
//
// Each panel exposes Version() that increments on any state change
// that could alter its View() output. The App caches the FULLY-WRAPPED
// panel output (panel.View + border + exactSize) keyed on
// (panelVersion, width, height, layoutKey). On compose keystrokes,
// only compose's version changes so all the other panels' wrapped
// outputs are reused — saving the bulk of the per-keystroke render
// cost.
//
// Phase 2g of the SOLID refactor of internal/ui/app.go: collects the
// panelCache type + boolToInt helper here, and groups the six per-
// panel cache fields that previously hung off App into a single
// panelRenderCache aggregator. App now holds one *panelRenderCache
// instead of six panelCache values; View accesses go from
// '&a.panelCacheRail' to '&a.renderCache.rail' and friends.
package ui

import "github.com/gammons/slk/internal/ui/wintree"

// panelCache stores the fully-wrapped (border + exactSize) output of
// a single panel keyed on a tuple of inputs that affect its rendering.
// A cache hit returns the previous frame's string verbatim; a miss
// recomputes and stores.
//
// layoutKey is a free-form int64 callers can use to encode focus
// state, mode, theme version, and layout-toggle bits as a single
// comparable value.
type panelCache struct {
	output       string
	panelVersion int64
	width        int
	height       int
	layoutKey    int64
	valid        bool
}

func (c *panelCache) hit(panelVersion int64, width, height int, layoutKey int64) bool {
	return c.valid &&
		c.panelVersion == panelVersion &&
		c.width == width &&
		c.height == height &&
		c.layoutKey == layoutKey
}

func (c *panelCache) store(out string, panelVersion int64, width, height int, layoutKey int64) {
	c.output = out
	c.panelVersion = panelVersion
	c.width = width
	c.height = height
	c.layoutKey = layoutKey
	c.valid = true
}

// boolToInt is the layout-key bit-packing helper. Callers shift its
// result by some bit offset and OR it into layoutKey so an int64
// can encode multiple boolean toggles (focus, mode, etc.) in a
// single comparable value.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// panelRenderCache groups the six per-panel caches under one owner.
// Names mirror the panels they back:
//
//	rail     - the workspace rail (leftmost column).
//	sidebar  - the channel/section sidebar (when visible).
//	msgPanel - the bordered messages region in the threads-list view
//	           (no compose).
//	msgTop   - the bordered messages region in the channel view; the
//	           compose + typing line are rendered fresh below and so
//	           are not part of this cached chunk.
//	thread   - the thread side panel's bordered top region.
//	status   - the status bar (bottom row).
type panelRenderCache struct {
	rail     panelCache
	sidebar  panelCache
	msgPanel panelCache
	// activityPanel backs the bordered messages region in the Activity
	// view (no compose). Kept distinct from msgPanel (threads view) so
	// the two views' independent Version() counters can't collide on a
	// (version, dims, layoutKey) key when the user switches between them.
	activityPanel panelCache
	msgTop        panelCache
	thread        panelCache
	status        panelCache

	// winPanes caches the bordered output of unfocused live window
	// panes, one slot per window (Phase 3). Focused windows render
	// through msgTop/msgPanel as before. Evicted when windows close
	// (syncWinModels) and on workspace reset (resetWindowTree).
	winPanes map[wintree.LeafID]*panelCache
}

func newPanelRenderCache() *panelRenderCache { return &panelRenderCache{} }

// getWinPane returns the cache slot for window id, lazily creating
// both the map and the slot.
func (rc *panelRenderCache) getWinPane(id wintree.LeafID) *panelCache {
	if rc.winPanes == nil {
		rc.winPanes = make(map[wintree.LeafID]*panelCache)
	}
	c := rc.winPanes[id]
	if c == nil {
		c = &panelCache{}
		rc.winPanes[id] = c
	}
	return c
}

// dropWinPane evicts the cache slot for a closed window.
func (rc *panelRenderCache) dropWinPane(id wintree.LeafID) {
	delete(rc.winPanes, id)
}

// dropAllWinPanes evicts every per-window pane cache (workspace
// reset: all window IDs are replaced wholesale).
func (rc *panelRenderCache) dropAllWinPanes() {
	rc.winPanes = nil
}
