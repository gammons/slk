package ansi

import "testing"

func TestReapplyAfterResets(t *testing.T) {
	t.Parallel()
	const bg = "\x1b[48;2;26;26;46m"
	const bgfg = bg + "\x1b[38;2;224;224;224m"
	cases := []struct {
		name, text, style, want string
	}{
		{"empty style is a no-op", "a\x1b[mb\x1b[0mc", "", "a\x1b[mb\x1b[0mc"},
		{"no resets", "plain", bg, "plain"},
		{"short form", "x\x1b[1mbold\x1b[m tail", bg, "x\x1b[1mbold\x1b[m" + bg + " tail"},
		{"explicit zero form", "x\x1b[0m tail", bg, "x\x1b[0m" + bg + " tail"},
		{"both forms, each once", "a\x1b[0mb\x1b[mc", bgfg, "a\x1b[0m" + bgfg + "b\x1b[m" + bgfg + "c"},
		{"trailing reset", "span\x1b[m", bg, "span\x1b[m" + bg},
		{"other SGR left alone", "\x1b[1m\x1b[22m", bg, "\x1b[1m\x1b[22m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := ReapplyAfterResets(c.text, c.style); got != c.want {
				t.Errorf("ReapplyAfterResets(%q, %q) = %q, want %q", c.text, c.style, got, c.want)
			}
		})
	}
}
