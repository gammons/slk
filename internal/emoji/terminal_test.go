package emoji

import (
	"testing"
)

func TestIdentifyTerminal(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "iTerm with version",
			env:  map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.0"},
			want: "iTerm.app_3.5.0",
		},
		{
			name: "ghostty",
			env:  map[string]string{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.0.0"},
			want: "ghostty_1.0.0",
		},
		{
			name: "TERM_PROGRAM without version",
			env:  map[string]string{"TERM_PROGRAM": "vscode"},
			want: "vscode",
		},
		{
			name: "kitty via env",
			env:  map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"},
			want: "kitty",
		},
		{
			name: "alacritty via env",
			env:  map[string]string{"ALACRITTY_LOG": "/tmp/alacritty.log", "TERM": "alacritty"},
			want: "alacritty",
		},
		{
			name: "wezterm with version",
			env:  map[string]string{"WEZTERM_PANE": "0", "WEZTERM_VERSION": "20240127"},
			want: "wezterm_20240127",
		},
		{
			name: "wezterm no version",
			env:  map[string]string{"WEZTERM_PANE": "0"},
			want: "wezterm",
		},
		{
			name: "fallback to TERM",
			env:  map[string]string{"TERM": "xterm-256color"},
			want: "xterm-256color",
		},
		{
			name: "no env vars",
			env:  map[string]string{},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := identifyTerminalFromEnv(tt.env)
			if got != tt.want {
				t.Errorf("identifyTerminalFromEnv(%v) = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}
