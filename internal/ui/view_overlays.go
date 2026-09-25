// internal/ui/view_overlays.go
//
// Overlay stack + final-screen wrapper for App.View (Phase 6c).
//
// applyOverlays stacks the seven full-screen overlays (channel
// finder, reaction picker, confirm prompt, workspace finder,
// theme switcher, presence menu, help) plus the presence
// custom-snooze numeric prompt and the bootstrap loading overlay
// onto a base screen. Each overlay's ViewOverlay knows how to
// composite itself onto an existing screen, so the order here
// matters: bootstrap wins over everything else, then the seven
// overlays stack in the order their entry-mode handlers SetMode
// to them. (Mode is exclusive so at most one of the seven can be
// open at once; the order below only matters if two slip into the
// stack via different code paths -- e.g. a bootstrap overlay
// landing while a finder is also open.)
//
// maybeWrapFinalScreen is the conservative re-wrap for the
// overlay-active case. The base screen produced by stacking the
// rail / sidebar / messages / thread panels is already
// exact-sized to (a.width, a.height), so the previously-mandatory
// full-screen lipgloss wrapper was just walking every cell to
// apply background padding that's already there (the single
// largest cost in the prior profile, ~3.4ms/frame). We skip the
// wrapper when no overlay is active; overlay compositors don't
// always produce exact-sized output, so we conservatively re-wrap
// when one is.
package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/styles"
)

// applyOverlays composes any active overlays onto screen and
// returns the result.
func (a *App) applyOverlays(screen string) string {
	if a.channelFinder.IsVisible() {
		screen = a.channelFinder.ViewOverlay(a.width, a.height, screen)
	}
	if a.searchResults.IsVisible() {
		screen = a.searchResults.ViewOverlay(a.width, a.height, screen)
	}
	if a.newMessagePicker.IsVisible() {
		screen = a.newMessagePicker.ViewOverlay(a.width, a.height, screen)
	}
	if a.reactionPicker.IsVisible() {
		screen = a.reactionPicker.ViewOverlay(a.width, a.height, screen)
	}
	if a.confirmPrompt.IsVisible() {
		screen = a.confirmPromptOverlay(screen)
	}
	if a.workspaceFinder.IsVisible() {
		screen = a.workspaceFinder.ViewOverlay(a.width, a.height, screen)
	}
	if a.themeSwitcher.IsVisible() {
		screen = a.themeSwitcher.ViewOverlay(a.width, a.height, screen)
	}
	if a.presenceMenu.IsVisible() {
		screen = a.presenceMenu.ViewOverlay(a.width, a.height, screen)
	}
	if a.help.IsVisible() {
		screen = a.help.ViewOverlay(a.width, a.height, screen)
	}
	if a.reactionsView.IsVisible() {
		screen = a.reactionsView.ViewOverlay(a.width, a.height, screen)
	}
	if a.linkPicker.IsVisible() {
		screen = a.linkPicker.ViewOverlay(a.width, a.height, screen)
	}
	if a.mode == ModePresenceCustomSnooze {
		screen = presencemenu.CustomSnoozeView(a.width, a.height, screen, a.presence.SnoozeBuf())
	}
	if a.bootstrap.IsLoading() {
		screen = a.bootstrap.Render(a.width, a.height, a.spinnerGlyph())
	}
	return screen
}

// overlayActive reports whether any of the overlay paths
// applyOverlays touches is currently composited onto the screen.
// Used by maybeWrapFinalScreen to decide whether the conservative
// re-wrap is needed.
func (a *App) overlayActive() bool {
	return a.channelFinder.IsVisible() ||
		a.searchResults.IsVisible() ||
		a.newMessagePicker.IsVisible() ||
		a.reactionPicker.IsVisible() ||
		a.confirmPrompt.IsVisible() ||
		a.workspaceFinder.IsVisible() ||
		a.themeSwitcher.IsVisible() ||
		a.presenceMenu.IsVisible() ||
		a.help.IsVisible() ||
		a.reactionsView.IsVisible() ||
		a.linkPicker.IsVisible() ||
		a.mode == ModePresenceCustomSnooze ||
		a.bootstrap.IsLoading()
}

// maybeWrapFinalScreen wraps screen in a full-canvas
// lipgloss style when an overlay is active (so the resulting
// output is guaranteed exact-sized and themed). Returns screen
// unchanged when no overlay is active -- the base-layer panels
// are already exact-sized themselves, and the full-screen wrapper
// was the single largest cost in the prior profile (~3.4ms/frame).
func (a *App) maybeWrapFinalScreen(screen string) string {
	if !a.overlayActive() {
		return screen
	}
	return lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		MaxHeight(a.height).
		Background(styles.Background).
		Render(screen)
}
