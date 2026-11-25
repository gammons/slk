package main

import (
	"context"
	"errors"
	"testing"

	"github.com/gammons/slk/internal/core"
)

type fakeAppCommands struct {
	cmds []core.SlashCommand
	err  error
}

func (f fakeAppCommands) AppCommands(context.Context) ([]core.SlashCommand, error) {
	return f.cmds, f.err
}

func TestFetchAppCommandsStoresTheList(t *testing.T) {
	wctx := &WorkspaceContext{TeamName: "Acme"}
	want := []core.SlashCommand{{Name: "/giphy", Usage: "[text]", Desc: "Post a gif", AppName: "Giphy"}}
	fetchAppCommands(context.Background(), wctx, fakeAppCommands{cmds: want})
	got := wctx.AppCommands()
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("AppCommands = %#v, want %#v", got, want)
	}
	// The stored slice is a copy.
	want[0].Desc = "changed"
	if wctx.AppCommands()[0].Desc != "Post a gif" {
		t.Fatal("SetAppCommands kept the caller's slice")
	}
}

func TestFetchAppCommandsFailureLeavesTheListEmpty(t *testing.T) {
	wctx := &WorkspaceContext{TeamName: "Acme"}
	fetchAppCommands(context.Background(), wctx, fakeAppCommands{err: errors.New("nope")})
	if got := wctx.AppCommands(); got != nil {
		t.Fatalf("AppCommands = %#v, want nil after a failed fetch", got)
	}
}
