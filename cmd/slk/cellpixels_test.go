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

func TestEmulatedPlacementIfMeasured(t *testing.T) {
	cases := []struct {
		name          string
		measured      bool
		auto          bool
		wantProto     imgpkg.Protocol
		wantPlacement imgpkg.KittyPlacement
	}{
		{"measured", true, true, imgpkg.ProtoKitty, imgpkg.KittyEmulated},
		{"measured, kitty forced", true, false, imgpkg.ProtoKitty, imgpkg.KittyEmulated},
		{"unmeasured falls back to sixel", false, true, imgpkg.ProtoSixel, imgpkg.KittyNative},
		{"unmeasured, kitty forced", false, false, imgpkg.ProtoHalfBlock, imgpkg.KittyNative},
	}
	for _, c := range cases {
		proto, placement := emulatedPlacementIfMeasured(c.measured, c.auto)
		if proto != c.wantProto || placement != c.wantPlacement {
			t.Errorf("%s: got (%s, %s), want (%s, %s)", c.name, proto, placement, c.wantProto, c.wantPlacement)
		}
	}
}
