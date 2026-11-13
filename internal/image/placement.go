package image

import (
	"strings"
	"sync/atomic"
)

// KittyPlacement says who turns kitty unicode-placeholder cells into
// pixels once the image protocol is ProtoKitty. The renderers emit
// placeholder cells (U+10EEEE, image ID in the foreground, row and
// column in diacritics) either way; only the last step differs.
//
// It is a mode next to Protocol, not a Protocol of its own: everything
// that checks for ProtoKitty (the fetcher's prerender, the avatar
// cache, emoji image mode, the preview) behaves the same in both
// modes. Only the upload (emitKittyUpload) and the App's frame exit
// look at it.
type KittyPlacement int32

const (
	// KittyNative is the terminal resolving placeholders itself, as
	// kitty and Ghostty do. Uploads create a virtual placement (U=1).
	KittyNative KittyPlacement = iota
	// KittyEmulated is for terminals with kitty graphics but no
	// unicode placeholders (WezTerm). Uploads only transmit (a=t); the
	// App finds the placeholder cells in each finished frame
	// (ResolvePlaceholders), blanks them, and FrameOutput paints direct
	// placements (a=p) over them after the text diff. See
	// docs/superpowers/specs/2026-10-20-wezterm-emoji-images-design.md.
	KittyEmulated
)

// String returns the config spelling of m, for logs.
func (m KittyPlacement) String() string {
	if m == KittyEmulated {
		return "emulated"
	}
	return "native"
}

// kittyPlacement is process-global, like emoji.SetImageMode: it is set
// once at startup, before any upload, and read from the render and
// prerender goroutines.
var kittyPlacement atomic.Int32

// SetKittyPlacement records the placement mode. Call once during
// startup, before bubbletea starts and before any kitty upload.
func SetKittyPlacement(m KittyPlacement) {
	kittyPlacement.Store(int32(m))
}

// KittyPlacementMode returns the mode SetKittyPlacement recorded,
// KittyNative by default.
func KittyPlacementMode() KittyPlacement {
	return KittyPlacement(kittyPlacement.Load())
}

// ChooseKittyPlacement picks the placement mode for a session whose
// protocol is ProtoKitty. cfg is [appearance] kitty_placement:
// "native" and "emulated" force a mode, anything else is auto.
//
// Under tmux or zellij the answer is always native. Emulated placements
// are positioned with absolute cursor moves on the terminal slk talks
// to, and through a multiplexer those land relative to the outer
// screen, not to slk's pane.
//
// Auto picks emulated for WezTerm only. Its direct placements attach
// to cells (they scroll with their line and die with EL), which is the
// model the emulated painter's erase and repaint rules are written
// for. kitty and Ghostty keep images in a separate layer and resolve
// placeholders natively, so they must stay native. kitty started from a
// WezTerm shell inherits TERM_PROGRAM=WezTerm but sets its own TERM, so
// TERM=xterm-kitty keeps it native.
func ChooseKittyPlacement(env Env, cfg string) KittyPlacement {
	if env.TMUX != "" || env.Zellij != "" {
		return KittyNative
	}
	switch strings.ToLower(strings.TrimSpace(cfg)) {
	case "native":
		return KittyNative
	case "emulated":
		return KittyEmulated
	}
	if env.TermProgram == "WezTerm" && env.Term != "xterm-kitty" {
		return KittyEmulated
	}
	return KittyNative
}
