package image

import (
	"os"
	"strings"
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

// Detect picks the rendering protocol for the current terminal.
// cfg is the user's config value (e.g. "auto", "kitty", "sixel", "halfblock", "off").
// Anything other than the four explicit values is treated as "auto".
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
	if env.TMUX != "" {
		return ProtoHalfBlock
	}
	if env.KittyWindowID != "" || env.Term == "xterm-kitty" {
		return ProtoKitty
	}
	switch env.TermProgram {
	case "ghostty", "WezTerm":
		return ProtoKitty
	}
	if env.Term == "foot" || env.Term == "mlterm" {
		return ProtoSixel
	}
	return ProtoHalfBlock
}
