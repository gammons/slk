package cli

import (
	"context"
	"errors"
	"testing"
)

func TestGolden_WorkspaceList(t *testing.T) {
	stdout, stderr, exit := runCLI(t, []string{"workspace", "list", "--json"}, withWorkspaces(acme, side))
	if exit != exitOK {
		t.Fatalf("exit %d, stderr %q", exit, stderr)
	}
	compareGoldenJSON(t, "workspace_list", stdout)
}

func TestGolden_WorkspaceListEmpty(t *testing.T) {
	stdout, _, exit := runCLI(t, []string{"workspace", "list", "--json"})
	if exit != exitOK {
		t.Fatalf("exit %d", exit)
	}
	compareGoldenJSON(t, "workspace_list_empty", stdout)
}

func TestGolden_WorkspaceWhoami(t *testing.T) {
	stdout, stderr, exit := runCLI(t, []string{"workspace", "whoami", "--json"}, withWorkspaces(acme, side))
	if exit != exitOK {
		t.Fatalf("exit %d, stderr %q", exit, stderr)
	}
	compareGoldenJSON(t, "workspace_whoami", stdout)
}

func TestGolden_ErrorNotFound(t *testing.T) {
	_, stderr, exit := runCLI(t, []string{"workspace", "whoami", "--json", "--workspace", "nope"}, withWorkspaces(acme, side))
	if exit != exitNotFound {
		t.Fatalf("exit %d, want %d", exit, exitNotFound)
	}
	compareGoldenJSON(t, "error_not_found", stderr)
}

// TestWorkspaceList_TextIsTheListWorkspacesTable pins the text form,
// which `slk --list-workspaces` prints too: the same bytes it printed
// before it ran through the CLI.
func TestWorkspaceList_TextIsTheListWorkspacesTable(t *testing.T) {
	stdout, _, exit := runCLI(t, []string{"workspace", "list"}, withWorkspaces(acme, side))
	if exit != exitOK {
		t.Fatalf("exit %d", exit)
	}
	want := "" +
		"TEAM ID   SLUG  NAME\n" +
		"--------  ----  ------------\n" +
		"T0ACME01  acme  Acme Inc\n" +
		"T0SIDE02        Side Project\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}

	stdout, _, exit = runCLI(t, []string{"workspace", "list"})
	if exit != exitOK || stdout != "No workspaces configured. Run 'slk --add-workspace' first.\n" {
		t.Errorf("no workspaces: exit %d, stdout %q", exit, stdout)
	}
}

// TestWorkspaceList_NeverSignsIn: listing reads local files only, so
// it works offline and with an expired login.
func TestWorkspaceList_NeverSignsIn(t *testing.T) {
	noConnect := withConnect(func(context.Context, Workspace) (*Session, error) {
		t.Error("workspace list signed in")
		return nil, errors.New("unexpected Connect")
	})
	if _, _, exit := runCLI(t, []string{"workspace", "list"}, withWorkspaces(acme), noConnect); exit != exitOK {
		t.Errorf("exit = %d", exit)
	}
}

func TestWorkspaceList_StoreErrorExits1(t *testing.T) {
	_, stderr, exit := runCLI(t, []string{"workspace", "list"}, withListError(errors.New("list tokens: permission denied")))
	if exit != exitFailure || stderr != "Error: list tokens: permission denied\n" {
		t.Errorf("exit %d, stderr %q", exit, stderr)
	}
}

func TestWorkspaceWhoami_Text(t *testing.T) {
	stdout, _, exit := runCLI(t, []string{"workspace", "whoami"}, withWorkspaces(acme))
	want := "Workspace: Acme Inc (T0ACME01), acme.slack.com\nUser:      alice (U0SELF01)\n"
	if exit != exitOK || stdout != want {
		t.Errorf("exit %d, stdout\n%s\nwant\n%s", exit, stdout, want)
	}
}
