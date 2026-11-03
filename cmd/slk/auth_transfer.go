package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/gammons/slk/internal/cli"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

func exportAuth(ctx context.Context, store *slackclient.TokenStore, ws cli.Workspace, output string) (cli.AuthExportResult, error) {
	if err := ctx.Err(); err != nil {
		return cli.AuthExportResult{}, err
	}
	tok, err := store.SnapshotForImport(ws.TeamID)
	if err != nil {
		return cli.AuthExportResult{}, authTransferError(err)
	}
	if tok == nil {
		return cli.AuthExportResult{}, &cli.Error{Code: "failed", Message: "The saved workspace login no longer exists; select the workspace again."}
	}
	data, err := slackclient.EncodeAuthBundle(*tok)
	if err != nil {
		return cli.AuthExportResult{}, authTransferError(err)
	}
	if err := writeAuthExport(ctx, output, data, func(path string) (authExportFile, error) {
		return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}); err != nil {
		return cli.AuthExportResult{}, err
	}
	return cli.AuthExportResult{Workspace: authWorkspace(*tok), Output: output, Plaintext: true}, nil
}

type authExportFile interface {
	io.WriteCloser
	Sync() error
}

func writeAuthExport(ctx context.Context, path string, data []byte, open func(string) (authExportFile, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return authTransferError(err)
	}
	parent := filepath.Dir(path)
	if _, err := os.Stat(parent); err != nil {
		return authTransferError(err)
	}
	if err := slackclient.ValidateAuthDirectory(parent); err != nil {
		return authTransferError(err)
	}
	f, err := open(path)
	if err != nil {
		return authTransferError(err)
	}
	closed, complete := false, false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if n, err := f.Write(data); err != nil {
		return authTransferError(err)
	} else if n != len(data) {
		return authTransferError(io.ErrShortWrite)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return authTransferError(err)
	}
	err = f.Close()
	closed = true
	if err != nil {
		return authTransferError(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	complete = true
	return nil
}

func importAuth(ctx context.Context, store *slackclient.TokenStore, input string, replace bool, identify identifyFunc) (cli.AuthImportResult, error) {
	if err := ctx.Err(); err != nil {
		return cli.AuthImportResult{}, err
	}
	data, err := slackclient.ReadAuthFile(input)
	if err != nil {
		return cli.AuthImportResult{}, authTransferError(err)
	}
	candidate, err := slackclient.DecodeAuthBundle(data)
	if err != nil {
		return cli.AuthImportResult{}, authTransferError(err)
	}
	expected, err := store.SnapshotForImport(candidate.TeamID)
	if err != nil {
		return cli.AuthImportResult{}, authTransferError(err)
	}
	if expected != nil && !replace {
		return cli.AuthImportResult{}, &cli.Error{Code: "auth_conflict", Message: "This workspace already has a login; use --replace only to replace the same account."}
	}
	if err := ctx.Err(); err != nil {
		return cli.AuthImportResult{}, err
	}
	team, user, err := identify(ctx, candidate)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return cli.AuthImportResult{}, ctxErr
	}
	if err != nil {
		return cli.AuthImportResult{}, authTransferError(err)
	}
	if team != candidate.TeamID || !authUserID(user) {
		return cli.AuthImportResult{}, authTransferError(slackclient.ErrInvalidAuthBundle)
	}
	if expected != nil {
		if err := checkAuthReplacement(ctx, *expected, user, identify); err != nil {
			return cli.AuthImportResult{}, err
		}
	}
	candidate.Source, candidate.UserID = slackclient.TokenSourceImported, user
	if err := ctx.Err(); err != nil {
		return cli.AuthImportResult{}, err
	}
	saved, err := store.ImportIf(ctx, candidate, expected)
	if err != nil {
		return cli.AuthImportResult{}, authTransferError(err)
	}
	if !saved {
		return cli.AuthImportResult{}, &cli.Error{Code: "auth_conflict", Message: "The saved login changed during validation; nothing was imported. Review the workspace and try again."}
	}
	return cli.AuthImportResult{Workspace: authWorkspace(candidate), User: cli.AuthUser{ID: user}, Replaced: expected != nil}, nil
}

func checkAuthReplacement(ctx context.Context, existing slackclient.Token, user string, identify identifyFunc) error {
	previousUser := existing.UserID
	if previousUser == "" {
		team, verifiedUser, err := identify(ctx, existing)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return authTransferError(err)
		}
		if err != nil || team != existing.TeamID || !authUserID(verifiedUser) {
			return &cli.Error{Code: "auth_conflict", Message: "The existing account could not be verified. Restore a verifiable login for that account before replacing it, or use a separate XDG_DATA_HOME profile; nothing was imported."}
		}
		previousUser = verifiedUser
	}
	if previousUser != user {
		return &cli.Error{Code: "auth_conflict", Message: "The imported login belongs to a different account. Use a separate XDG_DATA_HOME profile to keep cached private data isolated; --replace cannot switch accounts."}
	}
	return nil
}

func authWorkspace(tok slackclient.Token) cli.AuthWorkspace {
	return cli.AuthWorkspace{TeamID: tok.TeamID, Name: tok.TeamName, Domain: tok.Domain}
}

func authUserID(user string) bool {
	if len(user) < 2 || len(user) > 128 || (user[0] != 'U' && user[0] != 'W') {
		return false
	}
	for i := 1; i < len(user); i++ {
		if !(user[i] >= 'A' && user[i] <= 'Z') && !(user[i] >= '0' && user[i] <= '9') {
			return false
		}
	}
	return true
}

func authTransferError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, slackclient.ErrInvalidAuthBundle):
		return &cli.Error{Code: "invalid_auth_bundle", Message: "Invalid authentication bundle or Slack identity mismatch; export a fresh login and try again."}
	case errors.Is(err, slackclient.ErrUnsafeAuthFile):
		return &cli.Error{Code: "unsafe_auth_file", Message: "Unsafe authentication file or directory. Use a regular file owned by you with chmod 600, and trusted directories without symlinks or group/world write access. On Windows, use a private user directory with restricted ACLs."}
	case errors.Is(err, os.ErrExist):
		return &cli.Error{Code: "failed", Message: "The export destination already exists; choose a new file. Existing files and symlinks are never overwritten."}
	}
	var se slack.SlackErrorResponse
	if errors.As(err, &se) {
		if slackclient.IsAuthError(err) {
			return &cli.Error{Code: "auth_failed", Message: "Slack refused the supplied login; export a fresh session and try again.", SlackError: se.Err}
		}
		switch se.Err {
		case "internal_error", "fatal_error", "ratelimited", "request_timeout", "service_unavailable":
			return &cli.Error{Code: "slack_error", Message: "Slack could not validate the login; try again later.", SlackError: se.Err}
		}
	}
	return &cli.Error{Code: "failed", Message: "Authentication transfer failed. Check file access, available disk space and network connectivity; no credential details are included in this diagnostic."}
}
