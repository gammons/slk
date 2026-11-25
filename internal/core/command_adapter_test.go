package core

import (
	"errors"
	"testing"

	"github.com/gammons/slk/internal/ids"
)

func TestCommandServiceNilFuncIsUnsupported(t *testing.T) {
	err := NewCommandService(CommandServiceFuncs{}).Run("T1", "C1", "", "/giphy", "cats")
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestCommandServicePassesArguments(t *testing.T) {
	var got []any
	svc := NewCommandService(CommandServiceFuncs{
		Run: func(team string, ch ids.ChannelID, th ids.ThreadTS, cmd, args string) error {
			got = []any{team, ch, th, cmd, args}
			return nil
		},
	})
	if err := svc.Run("T1", "C1", "10.0", "/giphy", "cats"); err != nil {
		t.Fatal(err)
	}
	want := []any{"T1", ids.ChannelID("C1"), ids.ThreadTS("10.0"), "/giphy", "cats"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestCommandServiceNilListIsEmpty(t *testing.T) {
	if got := NewCommandService(CommandServiceFuncs{}).List(); got != nil {
		t.Errorf("List() = %#v, want nil", got)
	}
}

func TestCommandServiceListPassesThrough(t *testing.T) {
	want := []SlashCommand{{Name: "/giphy", Usage: "[text]", Desc: "Post a gif", AppName: "Giphy"}}
	svc := NewCommandService(CommandServiceFuncs{
		List: func() []SlashCommand { return want },
	})
	got := svc.List()
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("List() = %#v, want %#v", got, want)
	}
}

func TestCommandErrorMessageIsTheCode(t *testing.T) {
	if got := (&CommandError{Code: "unknown_command"}).Error(); got != "unknown_command" {
		t.Errorf("Error() = %q, want unknown_command", got)
	}
}
