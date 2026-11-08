package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// setupWorkspaceList is `slk workspace list`: the configured
// workspaces, in the order of the TUI's workspace rail. It reads only
// the local token store and config; it never signs in.
func setupWorkspaceList(*flag.FlagSet) runFunc {
	return func(ctx context.Context, inv *Invocation) error {
		list, err := inv.env.Workspaces()
		if err != nil {
			return err
		}
		if list == nil {
			list = []Workspace{}
		}
		doc := struct {
			Workspaces []Workspace `json:"workspaces"`
		}{list}
		return inv.out.result(doc, func(w io.Writer) error { return writeWorkspaceTable(w, list) })
	}
}

// writeWorkspaceTable writes the table `slk --list-workspaces` has
// always printed, which now runs `slk workspace list`: TEAM ID, SLUG
// and NAME, each column padded to its widest value (in bytes).
func writeWorkspaceTable(w io.Writer, list []Workspace) error {
	if len(list) == 0 {
		_, err := fmt.Fprintln(w, "No workspaces configured. Run 'slk --add-workspace' first.")
		return err
	}
	idW, slugW, nameW := len("TEAM ID"), len("SLUG"), len("NAME")
	for _, ws := range list {
		idW = max(idW, len(ws.TeamID))
		slugW = max(slugW, len(ws.Slug))
		nameW = max(nameW, len(ws.Name))
	}
	fmt.Fprintf(w, "%-*s  %-*s  %s\n", idW, "TEAM ID", slugW, "SLUG", "NAME")
	fmt.Fprintf(w, "%s  %s  %s\n", strings.Repeat("-", idW), strings.Repeat("-", slugW), strings.Repeat("-", nameW))
	for _, ws := range list {
		if _, err := fmt.Fprintf(w, "%-*s  %-*s  %s\n", idW, ws.TeamID, slugW, ws.Slug, ws.Name); err != nil {
			return err
		}
	}
	return nil
}

// setupWorkspaceWhoami is `slk workspace whoami`: it signs in
// (auth.test) and shows the workspace and the user that commands act
// as. It is the check an agent runs first: exit 3 means the login
// failed.
func setupWorkspaceWhoami(*flag.FlagSet) runFunc {
	return func(ctx context.Context, inv *Invocation) error {
		s, err := inv.Session(ctx)
		if err != nil {
			return err
		}
		type user struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		doc := struct {
			Workspace Workspace `json:"workspace"`
			User      user      `json:"user"`
		}{s.Workspace, user{ID: s.UserID, Name: s.UserName}}
		return inv.out.result(doc, func(w io.Writer) error {
			ws := s.Workspace
			if ws.Domain != "" {
				fmt.Fprintf(w, "Workspace: %s (%s), %s.slack.com\n", ws.Name, ws.TeamID, ws.Domain)
			} else {
				fmt.Fprintf(w, "Workspace: %s (%s)\n", ws.Name, ws.TeamID)
			}
			_, err := fmt.Fprintf(w, "User:      %s (%s)\n", s.UserName, s.UserID)
			return err
		})
	}
}
