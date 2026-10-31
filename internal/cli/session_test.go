package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestSelectWorkspace_MatchesSlugTeamNameAndDomain(t *testing.T) {
	list := []Workspace{acme, side}
	for ref, want := range map[string]string{
		"acme":         acme.TeamID, // slug
		"ACME":         acme.TeamID, // case-insensitive
		"T0SIDE02":     side.TeamID, // team ID
		"side project": side.TeamID, // team name
		"side-project": side.TeamID, // domain
	} {
		got, err := SelectWorkspace(list, ref)
		if err != nil || got.TeamID != want {
			t.Errorf("SelectWorkspace(%q) = %q, %v; want %q", ref, got.TeamID, err, want)
		}
	}
}

func TestSelectWorkspace_EmptyRefUsesDefaultThenTheOnlyOne(t *testing.T) {
	sideDefault := side
	sideDefault.Default = true
	acmePlain := acme
	acmePlain.Default = false

	if got, _ := SelectWorkspace([]Workspace{acmePlain, sideDefault}, ""); got.TeamID != side.TeamID {
		t.Errorf("with a default: got %q, want the default %q", got.TeamID, side.TeamID)
	}
	if got, _ := SelectWorkspace([]Workspace{acmePlain}, ""); got.TeamID != acme.TeamID {
		t.Errorf("one workspace: got %q, want it", got.TeamID)
	}
	_, err := SelectWorkspace([]Workspace{acmePlain, side}, "")
	assertCode(t, err, codeWorkspaceRequired, 2)
}

func TestSelectWorkspace_Errors(t *testing.T) {
	_, err := SelectWorkspace(nil, "acme")
	assertCode(t, err, codeNoWorkspace, 0)

	_, err = SelectWorkspace([]Workspace{acme, side}, "nope")
	assertCode(t, err, codeNotFound, 2)

	// A workspace without a slug must not match a blank-looking ref.
	_, err = SelectWorkspace([]Workspace{side}, " ")
	assertCode(t, err, codeNotFound, 1)
}

// assertCode checks that err is an *Error with code and that many
// candidates.
func assertCode(t *testing.T, err error, code string, candidates int) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an *Error with code %q", err, code)
	}
	if e.Code != code || len(e.Candidates) != candidates {
		t.Errorf("err = %q (code %q, %d candidates), want code %q with %d candidates",
			e.Message, e.Code, len(e.Candidates), code, candidates)
	}
}

// TestSession_WorkspaceOrder walks the D4 order through a real command:
// --workspace, then $SLK_WORKSPACE, then the default.
func TestSession_WorkspaceOrder(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
		want string
	}{
		{"flag beats environment", []string{"--workspace", "acme"}, "side-project", acme.TeamID},
		{"environment beats default", nil, "side-project", side.TeamID},
		{"default", nil, "", acme.TeamID},
	}
	for _, tc := range cases {
		args := append([]string{"workspace", "whoami", "--json"}, tc.args...)
		stdout, stderr, exit := runCLI(t, args, withWorkspaces(acme, side), withEnv("SLK_WORKSPACE", tc.env))
		if exit != exitOK {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, exit, stderr)
		}
		if !strings.Contains(stdout, `"team_id": "`+tc.want+`"`) {
			t.Errorf("%s: signed in to the wrong workspace:\n%s", tc.name, stdout)
		}
	}
}

func TestSession_NoChoiceAmongSeveralSaysHowToChoose(t *testing.T) {
	plain := acme
	plain.Default = false
	_, stderr, exit := runCLI(t, []string{"workspace", "whoami"}, withWorkspaces(plain, side))
	if exit != exitUsage {
		t.Errorf("exit = %d, want %d", exit, exitUsage)
	}
	for _, want := range []string{"--workspace", "SLK_WORKSPACE", "default_workspace", acme.TeamID, side.TeamID} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr misses %q:\n%s", want, stderr)
		}
	}
}

func TestSession_RefusedLoginExits3(t *testing.T) {
	refuse := withConnect(func(context.Context, Workspace) (*Session, error) {
		return nil, fmt.Errorf("auth test failed: %w", slack.SlackErrorResponse{Err: "invalid_auth"})
	})
	_, stderr, exit := runCLI(t, []string{"workspace", "whoami"}, withWorkspaces(acme), refuse)
	if exit != exitAuth {
		t.Errorf("exit = %d, want %d", exit, exitAuth)
	}
	for _, want := range []string{"Acme Inc", "invalid_auth", "slk --add-workspace"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr misses %q: %q", want, stderr)
		}
	}

	_, stderr, _ = runCLI(t, []string{"workspace", "whoami", "--json"}, withWorkspaces(acme), refuse)
	if !strings.Contains(stderr, `"code": "auth_failed"`) || !strings.Contains(stderr, `"slack_error": "invalid_auth"`) {
		t.Errorf("JSON error = %s", stderr)
	}
}

func TestSession_NoWorkspaceExits3(t *testing.T) {
	_, stderr, exit := runCLI(t, []string{"workspace", "whoami"})
	if exit != exitAuth || !strings.Contains(stderr, "slk --add-workspace") {
		t.Errorf("exit = %d, stderr %q; want 3 and the way to add one", exit, stderr)
	}
}

func TestSession_NetworkErrorExits1(t *testing.T) {
	fail := withConnect(func(context.Context, Workspace) (*Session, error) {
		return nil, errors.New("dial tcp: no route to host")
	})
	_, stderr, exit := runCLI(t, []string{"workspace", "whoami"}, withWorkspaces(acme), fail)
	if exit != exitFailure || !strings.Contains(stderr, "no route to host") {
		t.Errorf("exit = %d, stderr %q; want 1 and the cause", exit, stderr)
	}
}
