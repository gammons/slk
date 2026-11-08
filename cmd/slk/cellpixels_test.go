package main

import (
	"testing"

	imgpkg "github.com/gammons/slk/internal/image"
)

func TestShouldProbeCellSize(t *testing.T) {
	cases := []struct {
		name         string
		proto        imgpkg.Protocol
		tmux, zellij string
		tty          bool
		want         bool
	}{
		{"sixel", imgpkg.ProtoSixel, "", "", true, true},
		{"kitty", imgpkg.ProtoKitty, "", "", true, true},
		{"halfblock is sized in cells", imgpkg.ProtoHalfBlock, "", "", true, false},
		{"off", imgpkg.ProtoOff, "", "", true, false},
		{"tmux", imgpkg.ProtoSixel, "/tmp/tmux", "", true, false},
		{"zellij", imgpkg.ProtoSixel, "", "0", true, false},
		{"not a terminal", imgpkg.ProtoSixel, "", "", false, false},
	}
	for _, c := range cases {
		if got := shouldProbeCellSize(c.proto, c.tmux, c.zellij, c.tty); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
