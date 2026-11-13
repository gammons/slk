package image

import "testing"

// withKittyPlacement sets the process-global placement mode for one
// test and restores native afterwards.
func withKittyPlacement(t *testing.T, m KittyPlacement) {
	t.Helper()
	SetKittyPlacement(m)
	t.Cleanup(func() { SetKittyPlacement(KittyNative) })
}

func TestChooseKittyPlacement(t *testing.T) {
	cases := []struct {
		name string
		env  Env
		cfg  string
		want KittyPlacement
	}{
		{"WezTerm auto", Env{TermProgram: "WezTerm"}, "auto", KittyEmulated},
		{"WezTerm unset config", Env{TermProgram: "WezTerm"}, "", KittyEmulated},
		{"WezTerm forced native", Env{TermProgram: "WezTerm"}, "native", KittyNative},
		{"kitty", Env{KittyWindowID: "1", Term: "xterm-kitty"}, "auto", KittyNative},
		{"Ghostty", Env{TermProgram: "ghostty"}, "auto", KittyNative},
		// A terminal started from a WezTerm shell inherits
		// TERM_PROGRAM=WezTerm. kitty sets its own TERM; Ghostty its own
		// TERM_PROGRAM.
		{"kitty started from WezTerm", Env{TermProgram: "WezTerm", KittyWindowID: "1", Term: "xterm-kitty"}, "auto", KittyNative},
		{"Ghostty started from WezTerm", Env{TermProgram: "ghostty", Term: "xterm-ghostty"}, "auto", KittyNative},
		// WezTerm started from a kitty shell inherits KITTY_WINDOW_ID.
		{"WezTerm started from kitty", Env{TermProgram: "WezTerm", KittyWindowID: "1", Term: "xterm-256color"}, "auto", KittyEmulated},
		{"unknown terminal forced emulated", Env{TermProgram: "Nash"}, "emulated", KittyEmulated},
		{"config is case and space insensitive", Env{}, " Emulated ", KittyEmulated},
		// Emulated placements are positioned on the terminal slk talks
		// to, so they never run inside a multiplexer, even when forced.
		{"tmux WezTerm", Env{TMUX: "/tmp/tmux", TermProgram: "WezTerm"}, "auto", KittyNative},
		{"tmux forced emulated", Env{TMUX: "/tmp/tmux"}, "emulated", KittyNative},
		{"zellij WezTerm", Env{Zellij: "0", TermProgram: "WezTerm"}, "auto", KittyNative},
	}
	for _, tc := range cases {
		if got := ChooseKittyPlacement(tc.env, tc.cfg); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestKittyPlacementMode_DefaultsToNative(t *testing.T) {
	if got := KittyPlacementMode(); got != KittyNative {
		t.Fatalf("default mode = %v, want native", got)
	}
	withKittyPlacement(t, KittyEmulated)
	if got := KittyPlacementMode(); got != KittyEmulated {
		t.Fatalf("after SetKittyPlacement(emulated) = %v", got)
	}
}
