package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Env is everything the CLI gets from outside: the process's streams
// and environment, configured workspaces, sign-in and credential transfer.
// cmd/slk/cli.go builds the real one from the token store, the
// config and the Slack client; tests build it from fakes. Nothing in
// this package reads a file or opens a connection except through Env.
type Env struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	// Workspaces lists the configured workspaces in the order of the
	// TUI's workspace rail (config.OrderTokens), with the one that
	// [general] default_workspace names marked Default.
	Workspaces func() ([]Workspace, error)
	// Connect signs in to ws (auth.test) and returns the session. A
	// login Slack refuses is refreshed once before Connect gives up
	// (connectWithRemint in cmd/slk).
	Connect    func(ctx context.Context, ws Workspace) (*Session, error)
	ExportAuth func(ctx context.Context, ws Workspace, output string) (AuthExportResult, error)
	ImportAuth func(ctx context.Context, input string, replace bool) (AuthImportResult, error)
}

// Workspace is one configured workspace. It is also the JSON shape
// `slk workspace list` and `slk workspace whoami` print.
type Workspace struct {
	TeamID string `json:"team_id"`
	// Slug is the [workspaces.<slug>] key in config.toml; "" for a
	// workspace with no block there.
	Slug   string `json:"slug"`
	Name   string `json:"name"`
	Domain string `json:"domain"` // the <domain>.slack.com subdomain
	// Default is true for the workspace [general] default_workspace
	// names: the one a command uses when none is chosen.
	Default bool `json:"default"`
}

// Session is a signed-in workspace: who commands act as.
type Session struct {
	// Workspace is the chosen workspace, as `slk workspace list` shows
	// it, so its name and domain work as --workspace references.
	Workspace Workspace
	UserID    string
	UserName  string // the handle, not the display name
}

// SelectWorkspace picks the workspace ref names, matched
// case-insensitively against the slug, team ID, team name and domain;
// the first match in list order wins. An empty ref picks the Default
// workspace, else the only one. `slk export` shares this rule through
// selectExportToken, so the two cannot drift apart.
//
// The errors are *Error: no_workspace when list is empty,
// workspace_required when ref is empty and nothing decides, and
// not_found when ref matches nothing. The last two list every
// workspace as candidates.
func SelectWorkspace(list []Workspace, ref string) (Workspace, error) {
	if len(list) == 0 {
		return Workspace{}, &Error{Code: codeNoWorkspace, Message: "no workspaces configured; run 'slk --add-workspace' first"}
	}
	if ref == "" {
		for _, ws := range list {
			if ws.Default {
				return ws, nil
			}
		}
		if len(list) == 1 {
			return list[0], nil
		}
		return Workspace{}, &Error{
			Code:       codeWorkspaceRequired,
			Message:    "--workspace is required when more than one workspace is configured; see 'slk --list-workspaces'",
			Candidates: workspaceCandidates(list),
		}
	}
	for _, ws := range list {
		for _, name := range []string{ws.Slug, ws.TeamID, ws.Name, ws.Domain} {
			if name != "" && strings.EqualFold(name, ref) {
				return ws, nil
			}
		}
	}
	return Workspace{}, &Error{
		Code:       codeNotFound,
		Message:    fmt.Sprintf("workspace %q not found; see 'slk --list-workspaces'", ref),
		Candidates: workspaceCandidates(list),
	}
}

// workspaceCandidates lists every workspace for an error's candidates.
func workspaceCandidates(list []Workspace) []Candidate {
	out := make([]Candidate, len(list))
	for i, ws := range list {
		out[i] = Candidate{ID: ws.TeamID, Name: ws.Name}
	}
	return out
}

func (inv *Invocation) workspace() (Workspace, error) {
	list, err := inv.env.Workspaces()
	if err != nil {
		return Workspace{}, err
	}
	ref := inv.global.workspace
	if ref == "" {
		ref = inv.env.Getenv("SLK_WORKSPACE")
	}
	ws, err := SelectWorkspace(list, ref)
	if err != nil {
		// SelectWorkspace words this one for slk export, which has
		// only --workspace; the CLI has two more ways to choose.
		var e *Error
		if errors.As(err, &e) && e.Code == codeWorkspaceRequired {
			e.Message = "more than one workspace is configured; choose one with --workspace or SLK_WORKSPACE, or set default_workspace under [general] in config.toml"
		}
		return Workspace{}, err
	}
	return ws, nil
}

// Session signs in to the workspace the command line chose, once per
// invocation. Commands that need no Slack call never call it, so they
// work without a network.
func (inv *Invocation) Session(ctx context.Context) (*Session, error) {
	if inv.session != nil {
		return inv.session, nil
	}
	ws, err := inv.workspace()
	if err != nil {
		return nil, err
	}
	s, err := inv.env.Connect(ctx, ws)
	if err != nil {
		if e := errorFrom(err); e.Code == codeAuthFailed {
			e.Message = fmt.Sprintf("Slack refused the saved login for %s (%s); sign in again with 'slk --add-workspace'", ws.Name, e.SlackError)
			return nil, e
		}
		return nil, err
	}
	inv.session = s
	return s, nil
}
