// This file wires the scriptable CLI (internal/cli: `slk <noun> <verb>`)
// to slk's token store, config and Slack client. The CLI gets nothing
// from outside except through the cli.Env built here.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gammons/slk/internal/cli"
	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
)

// cliMain runs one CLI command line (os.Args[1:]) and returns the
// process exit code.
func cliMain(args []string) int {
	ctx, stop := signalContext()
	defer stop()
	return cli.Main(ctx, args, newCLIEnv())
}

// newCLIEnv builds the CLI's environment: the process's streams, the
// workspaces in the token store and a sign-in that refreshes a refused
// login once.
func newCLIEnv() cli.Env {
	store := slackclient.NewTokenStore(filepath.Join(xdgData(), "tokens"))
	configPath := filepath.Join(xdgConfig(), "config.toml")
	return cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Workspaces: func() ([]cli.Workspace, error) {
			tokens, err := store.List()
			if err != nil {
				return nil, fmt.Errorf("list tokens: %w", err)
			}
			// Best-effort, as for slk export and the old
			// --list-workspaces: a config that does not parse only
			// loses the rail order, the slugs and the default.
			cfg, _ := config.Load(configPath)
			return cliWorkspaces(tokens, cfg), nil
		},
		Connect: func(ctx context.Context, ws cli.Workspace) (*cli.Session, error) {
			tok, err := store.Load(ws.TeamID)
			if err != nil {
				return nil, err
			}
			// The path the TUI takes too (login.go): a login another
			// slk process saved, else the desktop app, each checked
			// with Slack before it is used.
			refresher := newLoginRefresher(store, acceptedLogin)
			client, err := connectWithRemint(ctx, tok, connectClient, refresher.refresh)
			if err != nil {
				return nil, err
			}
			return cliSession(ws, client), nil
		},
	}
}

// cliWorkspaces lists the stored workspaces in rail order
// (config.OrderTokens), with the one [general] default_workspace names
// marked Default. A default_workspace that names no workspace is
// ignored, as the TUI ignores it.
func cliWorkspaces(tokens []slackclient.Token, cfg config.Config) []cli.Workspace {
	defaultTeam, _ := cfg.TeamIDForDefaultWorkspace()
	ordered := config.OrderTokens(tokens, cfg)
	out := make([]cli.Workspace, len(ordered))
	for i, ot := range ordered {
		out[i] = cli.Workspace{
			TeamID:  ot.Token.TeamID,
			Slug:    ot.Slug,
			Name:    ot.Token.TeamName,
			Domain:  ot.Token.Domain,
			Default: defaultTeam != "" && ot.Token.TeamID == defaultTeam,
		}
	}
	return out
}

// connectClient signs in with tok (auth.test).
func connectClient(ctx context.Context, tok slackclient.Token) (*slackclient.Client, error) {
	client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// connectWithRemint signs in with the stored token and, only when Slack
// refuses the login itself (slackclient.IsAuthError), refreshes the
// token once with remint and signs in again.
//
// The TUI and slk export remint every token before they connect. The
// CLI does not: a remint reads the desktop app's cookie database and
// the OS keychain, which is slow and can show a keychain prompt, and an
// agent runs many commands. A remint that leaves the token as it was (a
// browser sign-in, which remint never touches, or a failed refresh) is
// not worth a second sign-in, so the first refusal is returned.
// newCLIEnv passes loginRefresher.refresh, the path the TUI takes too: a
// login that another slk process saved since comes before the desktop
// app, and Slack checks each before it is returned, so the one retry
// here is not spent on a login that Slack refuses.
//
// remint's error says why it has no newer login. errNoNewerLogin leaves
// the refusal as the answer (exit 3). Any other error is returned in its
// place, because it is why the refresh stopped: a cancelled command, no
// network, a timeout. Reporting the refusal then would tell the user to
// sign in again for a login that was never checked.
func connectWithRemint(
	ctx context.Context,
	tok slackclient.Token,
	connect func(context.Context, slackclient.Token) (*slackclient.Client, error),
	remint func(context.Context, slackclient.Token) (slackclient.Token, error),
) (*slackclient.Client, error) {
	client, err := connect(ctx, tok)
	if err == nil || !slackclient.IsAuthError(err) {
		return client, err
	}
	fresh, remintErr := remint(ctx, tok)
	if remintErr != nil && !errors.Is(remintErr, errNoNewerLogin) {
		return nil, remintErr
	}
	if remintErr != nil || (fresh.AccessToken == tok.AccessToken && fresh.Cookie == tok.Cookie) {
		return nil, err
	}
	return connect(ctx, fresh)
}

// cliSession is the session a signed-in client gives: the chosen
// workspace and the user. The workspace keeps the name and domain the
// token store holds, the values --workspace matches and `workspace
// list` prints, even where Slack now reports others (a renamed
// workspace; an Enterprise Grid, whose auth.test domain is
// "<org>.enterprise"). auth.test fills only what the store lacks.
func cliSession(ws cli.Workspace, client *slackclient.Client) *cli.Session {
	if ws.Name == "" {
		ws.Name = client.TeamName()
	}
	if ws.Domain == "" {
		ws.Domain = client.TeamSubdomain()
	}
	return &cli.Session{Workspace: ws, UserID: client.UserID(), UserName: client.UserName()}
}
