package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type AuthWorkspace struct {
	TeamID string `json:"team_id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

type AuthUser struct {
	ID string `json:"id"`
}

type AuthExportResult struct {
	Workspace AuthWorkspace `json:"workspace"`
	Output    string        `json:"output"`
	Plaintext bool          `json:"plaintext"`
}

type AuthImportResult struct {
	Workspace AuthWorkspace `json:"workspace"`
	User      AuthUser      `json:"user"`
	Replaced  bool          `json:"replaced"`
}

func setupAuthExport(fs *flag.FlagSet) runFunc {
	output := fs.String("output", "", "write a plaintext login to a new `file` (required; '-' and existing files are refused)")
	return func(ctx context.Context, inv *Invocation) error {
		if *output == "" || *output == "-" {
			return usageError("auth export requires --output <file>; '-' is not supported")
		}
		ws, err := inv.workspace()
		if err != nil {
			return err
		}
		doc, err := inv.env.ExportAuth(ctx, ws, *output)
		if err != nil {
			return err
		}
		return inv.out.result(doc, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "Exported login for %s (%s) to %s.\nWARNING: This file contains plaintext credentials. Transfer it securely and delete it when finished.\n",
				doc.Workspace.Name, doc.Workspace.TeamID, doc.Output)
			return err
		})
	}
}

func setupAuthImport(fs *flag.FlagSet) runFunc {
	input := fs.String("input", "", "read the plaintext login from this `file` (required; '-' is not supported)\nworkspace comes from the file; --workspace is not accepted\nrestart running slk after import")
	replace := fs.Bool("replace", false, "replace an existing login only for the same workspace and user")
	return func(ctx context.Context, inv *Invocation) error {
		if inv.global.workspaceExplicit {
			return usageError("auth import does not accept --workspace; the workspace comes from the input file")
		}
		if *input == "" || *input == "-" {
			return usageError("auth import requires --input <file>; '-' is not supported")
		}
		doc, err := inv.env.ImportAuth(ctx, *input, *replace)
		if err != nil {
			return err
		}
		return inv.out.result(doc, func(w io.Writer) error {
			_, err := fmt.Fprintf(w, "Imported login for %s (%s), user %s.\nRestart any running slk process to use this login.\n",
				doc.Workspace.Name, doc.Workspace.TeamID, doc.User.ID)
			return err
		})
	}
}
