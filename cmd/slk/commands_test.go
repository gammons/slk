package main

import (
	"context"
	"errors"
	"testing"

	"github.com/gammons/slk/internal/core"
	slackclient "github.com/gammons/slk/internal/slack"
)

type fakeCommandRunner struct {
	got  slackclient.CommandRequest
	err  error
	team string
}

func (f *fakeCommandRunner) RunCommand(_ context.Context, req slackclient.CommandRequest) error {
	f.got = req
	return f.err
}

func (f *fakeCommandRunner) TeamID() string { return f.team }

func TestRunCommandMapsFields(t *testing.T) {
	f := &fakeCommandRunner{team: "T0EXAMPLE01"}
	listed := []core.SlashCommand{{Name: "/giphy", Desc: "gif"}}
	if err := runCommand(context.Background(), f, "C0EXAMPLE01", "10.0", "/giphy", "cats", listed); err != nil {
		t.Fatal(err)
	}
	want := slackclient.CommandRequest{ChannelID: "C0EXAMPLE01", ThreadTS: "10.0", Command: "/giphy", Args: "cats", TeamID: "T0EXAMPLE01"}
	if f.got != want {
		t.Errorf("request = %+v, want %+v", f.got, want)
	}
}

func TestRunCommandOmitsTeamIDWhenUnlisted(t *testing.T) {
	f := &fakeCommandRunner{team: "T0EXAMPLE01"}
	listed := []core.SlashCommand{{Name: "/giphy"}, {Name: "/kick", AliasOf: "/remove"}}
	if err := runCommand(context.Background(), f, "C1", "", "/nope", "", listed); err != nil {
		t.Fatal(err)
	}
	if f.got.TeamID != "" {
		t.Errorf("TeamID = %q, want it omitted for a command that is not in the list", f.got.TeamID)
	}
	if f.got.Command != "/nope" {
		t.Errorf("Command = %q", f.got.Command)
	}
}

func TestRunCommandSendsTeamIDForAListedCommand(t *testing.T) {
	f := &fakeCommandRunner{team: "T0EXAMPLE01"}
	listed := []core.SlashCommand{{Name: "/giphy"}, {Name: "/kick", AliasOf: "/remove"}}
	for _, name := range []string{"/giphy", "/kick", "/KICK"} {
		f.got = slackclient.CommandRequest{}
		if err := runCommand(context.Background(), f, "C1", "", name, "", listed); err != nil {
			t.Fatal(err)
		}
		if f.got.TeamID != "T0EXAMPLE01" {
			t.Errorf("%s TeamID = %q, want T0EXAMPLE01", name, f.got.TeamID)
		}
	}
	f.got = slackclient.CommandRequest{}
	if err := runCommand(context.Background(), f, "C1", "", "/giphy", "", nil); err != nil {
		t.Fatal(err)
	}
	if f.got.TeamID != "" {
		t.Errorf("empty list TeamID = %q, want it omitted", f.got.TeamID)
	}
}

func TestRunCommandTranslatesRejection(t *testing.T) {
	f := &fakeCommandRunner{err: &slackclient.CommandRejectedError{Code: "unknown_command"}}
	err := runCommand(context.Background(), f, "C1", "", "/nope", "", nil)
	var ce *core.CommandError
	if !errors.As(err, &ce) || ce.Code != "unknown_command" {
		t.Fatalf("err = %v, want *core.CommandError{unknown_command}", err)
	}
}

func TestRunCommandPassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("boom")
	f := &fakeCommandRunner{err: boom}
	if err := runCommand(context.Background(), f, "C1", "", "/giphy", "", nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
