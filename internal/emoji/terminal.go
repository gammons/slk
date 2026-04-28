package emoji

import "os"

// IdentifyTerminal returns a stable cache key for the current terminal
// based on environment variables.
func IdentifyTerminal() string {
	env := map[string]string{
		"TERM_PROGRAM":         os.Getenv("TERM_PROGRAM"),
		"TERM_PROGRAM_VERSION": os.Getenv("TERM_PROGRAM_VERSION"),
		"KITTY_WINDOW_ID":      os.Getenv("KITTY_WINDOW_ID"),
		"KITTY_VERSION":        os.Getenv("KITTY_VERSION"),
		"ALACRITTY_LOG":        os.Getenv("ALACRITTY_LOG"),
		"WEZTERM_PANE":         os.Getenv("WEZTERM_PANE"),
		"WEZTERM_VERSION":      os.Getenv("WEZTERM_VERSION"),
		"TERM":                 os.Getenv("TERM"),
	}
	return identifyTerminalFromEnv(env)
}

func identifyTerminalFromEnv(env map[string]string) string {
	if prog := env["TERM_PROGRAM"]; prog != "" {
		if ver := env["TERM_PROGRAM_VERSION"]; ver != "" {
			return prog + "_" + ver
		}
		return prog
	}
	if env["KITTY_WINDOW_ID"] != "" {
		if ver := env["KITTY_VERSION"]; ver != "" {
			return "kitty_" + ver
		}
		return "kitty"
	}
	if env["ALACRITTY_LOG"] != "" {
		return "alacritty"
	}
	if env["WEZTERM_PANE"] != "" {
		if ver := env["WEZTERM_VERSION"]; ver != "" {
			return "wezterm_" + ver
		}
		return "wezterm"
	}
	if term := env["TERM"]; term != "" {
		return term
	}
	return "unknown"
}
