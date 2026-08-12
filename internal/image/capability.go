package image

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gammons/slk/internal/debuglog"
)

// Env is a snapshot of terminal-related environment variables.
// Captured separately so tests can inject values without touching os.Getenv.
type Env struct {
	TMUX          string
	KittyWindowID string
	Term          string
	TermProgram   string
	Colorterm     string
}

// CaptureEnv reads the relevant environment variables from the OS.
func CaptureEnv() Env {
	return Env{
		TMUX:          getenv("TMUX"),
		KittyWindowID: getenv("KITTY_WINDOW_ID"),
		Term:          getenv("TERM"),
		TermProgram:   getenv("TERM_PROGRAM"),
		Colorterm:     getenv("COLORTERM"),
	}
}

// getenv is overridable in tests.
var getenv = os.Getenv

// tmuxClientTerm asks the running tmux server for the TERM value of the
// currently attached client. tmux strips the outer terminal's identity
// from a pane's env (sets TERM=tmux-*, TERM_PROGRAM=tmux, etc.), so this
// is how we recover it. Returns "" on any failure or if tmux is missing.
//
// Overridable for tests.
var tmuxClientTerm = func() string {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{client_termname}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Detect picks the rendering protocol for the current terminal.
// cfg is the user's config value (e.g. "auto", "kitty", "sixel", "halfblock", "off").
// Anything other than the four explicit values is treated as "auto".
//
// Under tmux, env vars that identify the outer terminal are stripped:
// TERM becomes tmux-256color, TERM_PROGRAM becomes "tmux", and
// KITTY_WINDOW_ID is only present if the user happens to have started
// tmux from inside kitty (often false after a reattach). To recover
// the outer terminal's identity, we ask tmux via display-message
// #{client_termname}. Kitty graphics escapes are wrapped in the tmux
// DCS passthrough envelope at emit time (see writeKittySequence) and
// the startup probe in cmd/slk confirms the terminal actually
// acknowledges the upload — if allow-passthrough is off, the probe
// times out and the caller downgrades to halfblock.
//
// Sixel under tmux is treated more conservatively: sixel has no
// equivalent of the kitty graphics ;OK reply, so we can't probe-verify
// it, and re-emitting sixel per redraw under tmux passthrough is
// bandwidth-hostile.
func Detect(env Env, cfg string) Protocol {
	switch strings.ToLower(strings.TrimSpace(cfg)) {
	case "off":
		return ProtoOff
	case "halfblock":
		return ProtoHalfBlock
	case "sixel":
		return ProtoSixel
	case "kitty":
		return ProtoKitty
	}
	// auto
	if env.KittyWindowID != "" || env.Term == "xterm-kitty" {
		return ProtoKitty
	}
	if env.TermProgram == "ghostty" {
		return ProtoKitty
	}
	if env.TMUX != "" {
		// tmux has stripped TERM and TERM_PROGRAM. Ask tmux what the
		// attached client's terminal actually is.
		raw := tmuxClientTerm()
		outer := strings.ToLower(raw)
		debuglog.ImgRender("tmux client_termname=%q", raw)
		if strings.Contains(outer, "kitty") ||
			strings.Contains(outer, "ghostty") ||
			strings.Contains(outer, "wezterm") {
			return ProtoKitty
		}
		// No kitty-capable outer terminal detected; stay conservative.
		// Sixel-under-tmux is also halfblock — see function doc.
		return ProtoHalfBlock
	}
	if env.Term == "foot" || env.Term == "mlterm" {
		return ProtoSixel
	}
	// iTerm2 and WezTerm both support kitty *graphics*, but not the
	// unicode-placeholder extension slk's kitty renderer depends on for
	// placement (see the U=1 note on writeKittySequence / kitty.go) —
	// iTerm2 because its kitty implementation is partial and fails the
	// startup probe, WezTerm because it answers the probe but then
	// prints the placeholder codepoints as literal glyphs. Both have
	// solid sixel support, so auto picks sixel for real pixels instead
	// of the halfblock mosaic the failed kitty probe would otherwise
	// fall back to.
	switch env.TermProgram {
	case "iTerm.app", "WezTerm":
		return ProtoSixel
	}
	return ProtoHalfBlock
}
