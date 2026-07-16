package image

import "testing"

// withTmuxClientTerm overrides the tmux query for the duration of the test.
func withTmuxClientTerm(t *testing.T, term string) {
	t.Helper()
	orig := tmuxClientTerm
	tmuxClientTerm = func() string { return term }
	t.Cleanup(func() { tmuxClientTerm = orig })
}

func TestDetect_ConfigOverrides(t *testing.T) {
	cases := []struct {
		cfg  string
		want Protocol
	}{
		{"off", ProtoOff},
		{"halfblock", ProtoHalfBlock},
		{"sixel", ProtoSixel},
		{"kitty", ProtoKitty},
	}
	for _, tc := range cases {
		got := Detect(Env{}, tc.cfg)
		if got != tc.want {
			t.Errorf("cfg=%q: got %v, want %v", tc.cfg, got, tc.want)
		}
	}
}

// Pre-tmux env signals still work if they happened to propagate (e.g.
// user launched tmux from inside kitty, KITTY_WINDOW_ID survived).
func TestDetect_TmuxKittyHostFromInheritedEnv(t *testing.T) {
	withTmuxClientTerm(t, "")
	cases := []Env{
		{TMUX: "/tmp/tmux", KittyWindowID: "1"},
		{TMUX: "/tmp/tmux", Term: "xterm-kitty"},
		{TMUX: "/tmp/tmux", TermProgram: "ghostty"},
		{TMUX: "/tmp/tmux", TermProgram: "WezTerm"},
	}
	for i, env := range cases {
		if got := Detect(env, "auto"); got != ProtoKitty {
			t.Errorf("case %d (%+v): want kitty, got %v", i, env, got)
		}
	}
}

// The real-world case: tmux has stripped TERM and TERM_PROGRAM, but
// asking tmux for the client's TERM reveals a kitty-capable host.
func TestDetect_TmuxRecoversKittyViaClientTerm(t *testing.T) {
	cases := []struct {
		clientTerm string
		desc       string
	}{
		{"xterm-kitty", "kitty"},
		{"xterm-ghostty", "ghostty"},
		{"wezterm", "wezterm"},
		{"WezTerm", "wezterm capitalized"},
	}
	tmuxEnv := Env{TMUX: "/tmp/tmux", Term: "tmux-256color", TermProgram: "tmux"}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			withTmuxClientTerm(t, tc.clientTerm)
			if got := Detect(tmuxEnv, "auto"); got != ProtoKitty {
				t.Errorf("tmux client=%q: want kitty, got %v", tc.clientTerm, got)
			}
		})
	}
}

// Tmux + non-kitty client → halfblock. tmux query is consulted but
// returns a non-kitty terminal.
func TestDetect_TmuxNonKittyClientIsHalfBlock(t *testing.T) {
	withTmuxClientTerm(t, "xterm-256color")
	env := Env{TMUX: "/tmp/tmux", Term: "tmux-256color", TermProgram: "tmux"}
	if got := Detect(env, "auto"); got != ProtoHalfBlock {
		t.Errorf("tmux + xterm client should halfblock, got %v", got)
	}
}

// Tmux + tmux command unavailable/empty → halfblock fallback.
func TestDetect_TmuxQueryFailsFallsThrough(t *testing.T) {
	withTmuxClientTerm(t, "")
	env := Env{TMUX: "/tmp/tmux", Term: "tmux-256color", TermProgram: "tmux"}
	if got := Detect(env, "auto"); got != ProtoHalfBlock {
		t.Errorf("tmux query failure should halfblock, got %v", got)
	}
}

func TestDetect_KittyByEnvVar(t *testing.T) {
	cases := []Env{
		{KittyWindowID: "1"},
		{Term: "xterm-kitty"},
		{TermProgram: "ghostty"},
		{TermProgram: "WezTerm"},
	}
	for i, env := range cases {
		if got := Detect(env, "auto"); got != ProtoKitty {
			t.Errorf("case %d (%+v): want kitty, got %v", i, env, got)
		}
	}
}

func TestDetect_Sixel(t *testing.T) {
	cases := []Env{
		{Term: "foot"},
		{Term: "mlterm"},
		// iTerm2 has no working kitty graphics implementation (the
		// startup probe would time out and downgrade to halfblock),
		// but it does support sixel — real pixels instead of the
		// halfblock mosaic.
		{TermProgram: "iTerm.app"},
	}
	for _, env := range cases {
		if got := Detect(env, "auto"); got != ProtoSixel {
			t.Errorf("env=%+v: want sixel, got %v", env, got)
		}
	}
}

func TestDetect_FallbackHalfBlock(t *testing.T) {
	env := Env{Term: "xterm-256color", Colorterm: "truecolor"}
	if got := Detect(env, "auto"); got != ProtoHalfBlock {
		t.Errorf("want halfblock fallback, got %v", got)
	}
}

func TestDetect_AutoUnknownConfigDefaultsToAuto(t *testing.T) {
	if got := Detect(Env{Term: "xterm-kitty"}, ""); got != ProtoKitty {
		t.Errorf("empty cfg should be auto, got %v", got)
	}
	if got := Detect(Env{Term: "xterm-kitty"}, "bogus"); got != ProtoKitty {
		t.Errorf("unknown cfg should be auto, got %v", got)
	}
}
