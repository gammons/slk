// internal/ui/imagepreview.go
//
// Full-screen image preview overlay state.
//
// Phase 2i of the SOLID refactor of internal/ui/app.go: extracts the
// four overlay-state fields (previewOverlay, previewChannel,
// previewTS, previewAttIdx) plus the spinner tick scheduler into a
// self-contained controller.
//
// The cmd-building helpers (openImagePreviewCmd, cycleImagePreviewCmd,
// previewFetchCmd, previewMetaForOpen) STAY on App because they couple
// to findMessageInActiveChannel which itself couples to messagepane /
// threadPanel — pulling them in here would require either passing
// several sub-model references at construction or moving the message-
// lookup helper too. Cleaner boundary: this file owns the overlay
// state; App's cmd helpers orchestrate the async fetch + dispatch.
//
// imageFetcher and imgProtocol also stay on App: they're wired once at
// startup via SetImageFetcher / SetImageProtocol and never mutate; not
// preview-specific state.
package ui

import (
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
)

// previewSpinnerTickInterval is the redraw cadence for the loading
// spinner. 100ms feels alive without being a CPU hog.
const previewSpinnerTickInterval = 100 * time.Millisecond

// previewSpinnerTickCmd schedules the next previewSpinnerTickMsg. The
// Update arm reschedules until the overlay either closes or the image
// has loaded; the chain self-terminates.
func previewSpinnerTickCmd() tea.Cmd {
	return tea.Tick(previewSpinnerTickInterval, func(time.Time) tea.Msg {
		return previewSpinnerTickMsg{}
	})
}

// imagePreviewController owns the full-screen image preview overlay
// state. A nil overlay (the zero value) means no preview is currently
// open; the channel / ts / attIdx triple identifies the message whose
// attachment is being shown (used by cycle keys to locate siblings).
type imagePreviewController struct {
	overlay *imgpkg.Preview
	channel string
	ts      string
	attIdx  int
}

func newImagePreviewController() *imagePreviewController {
	return &imagePreviewController{}
}

// Active reports whether a preview overlay is currently visible.
func (p *imagePreviewController) Active() bool {
	return p.overlay != nil && !p.overlay.IsClosed()
}

// Overlay returns the current preview overlay, or nil when no preview
// is open. Callers that only need to query Active() should prefer it.
func (p *imagePreviewController) Overlay() *imgpkg.Preview { return p.overlay }

// Channel / TS / AttIdx return the (channel, message ts, attachment
// idx) of the message whose attachment is currently displayed.
// Meaningless when !Active.
func (p *imagePreviewController) Channel() string { return p.channel }
func (p *imagePreviewController) TS() string      { return p.ts }
func (p *imagePreviewController) AttIdx() int     { return p.attIdx }

// Open installs overlay as the currently-shown preview and records the
// (channel, ts, attIdx) source so cycle keys can locate siblings.
// Any previous overlay is overwritten (caller is responsible for
// closing it first if needed).
func (p *imagePreviewController) Open(overlay *imgpkg.Preview, channel, ts string, attIdx int) {
	p.overlay = overlay
	p.channel = channel
	p.ts = ts
	p.attIdx = attIdx
}

// Close dismisses the current preview. Safe to call when no preview
// is open (no-op). Note: leaves the channel/ts/attIdx fields populated
// — they're meaningless once overlay is nil and Active() returns false.
func (p *imagePreviewController) Close() {
	if p.overlay == nil {
		return
	}
	p.overlay.Close()
	p.overlay = nil
}

// SetAttIdx updates the remembered attachment index. Called from the
// previewLoadedMsg arm after a cycle swap so the next cycle key starts
// from the new position.
func (p *imagePreviewController) SetAttIdx(idx int) { p.attIdx = idx }

// Handle is the image-preview-family reducer for App.Update (Phase 4b).
// Owns messages.OpenImagePreviewMsg (open the overlay + kick off
// fetch), previewSpinnerTickMsg (advance the loading-spinner frame),
// previewLoadedMsg (swap bytes into the overlay) and previewErrorMsg
// (dismiss on permanent fetch failure). Returns (nil, false) for any
// other message type.
//
// The arms previously lived in the monolithic Update switch at
// app.go ~lines 1073-1130 (pre-Phase-4). The cmd-building helpers
// (openImagePreviewCmd, previewFetchCmd, previewMetaForOpen,
// findMessageInActiveChannel) still live on App for the reasons noted
// in the file header; the reducer calls them via the `a` reference.
//
// Note: the modal key-trap at the top of Update (esc/q/enter/h/l
// while a.preview.Active()) is NOT part of this reducer. It's a
// pre-switch interception that returns before any reducer runs.
func (p *imagePreviewController) Handle(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case messages.OpenImagePreviewMsg:
		// Open the overlay IMMEDIATELY in a loading state so the
		// user gets visual feedback that their click / O / v
		// registered. The actual image fetch happens asynchronously;
		// previewLoadedMsg swaps the bytes in when ready. Without
		// this fast path the UI felt hung on slow-fetching previews
		// (Slack Connect channels where multi-auth retry can take
		// seconds).
		// An image the load policy holds back is not fetched for the
		// preview either: ask first, as I does.
		if msgItem, ok := a.findMessageInActiveChannel(m.Channel, m.TS); ok && previewHeld(a.imageCtx.Policy, msgItem, m.AttIdx) {
			return a.promptLoadImages(msgItem), true
		}
		cmd := a.openImagePreviewCmd(m.Channel, m.TS, m.AttIdx)
		if cmd == nil {
			return nil, true
		}
		name, sibCount, sibIndex := a.previewMetaForOpen(m.Channel, m.TS, m.AttIdx)
		loading := imgpkg.NewLoadingPreview(name, sibCount, sibIndex)
		p.Open(&loading, m.Channel, m.TS, m.AttIdx)
		return tea.Batch(cmd, previewSpinnerTickCmd()), true

	case previewSpinnerTickMsg:
		if !(p.Active() && p.Overlay().IsLoading()) {
			return nil, true
		}
		p.Overlay().AdvanceLoadingFrame()
		return previewSpinnerTickCmd(), true

	case previewLoadedMsg:
		// If the overlay is nil or already closed, the user dismissed
		// it before the bytes arrived; drop the image on the floor.
		if !p.Active() {
			return nil, true
		}
		input := imgpkg.PreviewInput{
			Name:         m.Name,
			FileID:       m.FileID,
			Img:          m.Img,
			Path:         m.Path,
			SiblingCount: m.SiblingCount,
			SiblingIndex: m.SiblingIndex,
		}
		// Swap bytes into the existing overlay (whether it's the
		// initial loading shell or an already-displayed image being
		// cycled). Preserves the overlay layout and keeps cycling
		// state coherent.
		p.Overlay().SwapImage(input)
		if !m.isCycle {
			return nil, true
		}
		// Cycling case: update the remembered attIdx so a subsequent
		// cycle key starts from the new position.
		msgItem, ok := a.findMessageInActiveChannel(p.Channel(), p.TS())
		if !ok {
			return nil, true
		}
		for i, att := range msgItem.Attachments {
			if att.FileID == m.FileID {
				p.SetAttIdx(i)
				break
			}
		}
		return nil, true

	case previewErrorMsg:
		log.Printf("preview fetch error: %v", m.Err)
		// Dismiss the loading overlay so the user isn't left staring
		// at a permanent spinner.
		if p.Overlay() != nil && p.Overlay().IsLoading() {
			p.Close()
		}
		return nil, true
	}
	return nil, false
}
