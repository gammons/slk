package image

import "testing"

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

func TestDetect_TmuxForcesHalfBlock(t *testing.T) {
	env := Env{TMUX: "/tmp/tmux", KittyWindowID: "1", Term: "xterm-kitty"}
	if got := Detect(env, "auto"); got != ProtoHalfBlock {
		t.Errorf("expected halfblock under tmux, got %v", got)
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
