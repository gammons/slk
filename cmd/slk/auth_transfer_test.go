package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/cli"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

func authTransferFixture(t *testing.T) (*slackclient.TokenStore, slackclient.Token, string, string) {
	t.Helper()
	dir := t.TempDir()
	tok := testLogin("xoxc-FAKE-TRANSFER-SECRET", "FAKE-TRANSFER-COOKIE%2F")
	tok.Domain = "acme"
	data, err := slackclient.EncodeAuthBundle(tok)
	if err != nil {
		t.Fatal("encode fixture failed")
	}
	input := filepath.Join(dir, "input.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal("write fixture failed")
	}
	return slackclient.NewTokenStore(filepath.Join(dir, "tokens")), tok, input, dir
}

func saveAuthTransferToken(t *testing.T, store *slackclient.TokenStore, tok slackclient.Token) {
	t.Helper()
	if err := store.Save(tok); err != nil {
		t.Fatal("save fixture failed")
	}
}

func assertAuthTransferCode(t *testing.T, err error, code string) {
	t.Helper()
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("error classification = %T; want %s", err, code)
	}
}

func assertAuthTransferAbsent(t *testing.T, store *slackclient.TokenStore) {
	t.Helper()
	if got, err := store.SnapshotForImport("T1"); err != nil || got != nil {
		t.Fatal("operation unexpectedly saved a login")
	}
}

func TestAuthTransferExportOfflineSnapshot(t *testing.T) {
	store, tok, _, dir := authTransferFixture(t)
	tok.Source = slackclient.TokenSourceImported
	tok.UserID = "U1"
	saveAuthTransferToken(t, store, tok)
	output := filepath.Join(dir, "export.json")
	doc, err := exportAuth(context.Background(), store, cli.Workspace{TeamID: tok.TeamID, Name: "stale label"}, output)
	if err != nil {
		t.Fatal("export failed")
	}
	if doc.Output != output || !doc.Plaintext || doc.Workspace.Name != tok.TeamName || doc.Workspace.Domain != tok.Domain {
		t.Fatal("export result did not use stored metadata")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal("export missing")
	}
	got, err := slackclient.DecodeAuthBundle(data)
	if err != nil || got.AccessToken != tok.AccessToken || got.Cookie != tok.Cookie || got.Source != "" || got.UserID != "" {
		t.Fatal("export changed credentials or included internal policy/identity")
	}
	info, err := os.Stat(output)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatal("export permissions are not owner-only")
	}
	stored, err := store.Load(tok.TeamID)
	if err != nil || stored != tok {
		t.Fatal("offline export changed the stored login")
	}
	encoded, _ := json.Marshal(doc)
	if bytes.Contains(encoded, []byte(tok.AccessToken)) || bytes.Contains(encoded, []byte(tok.Cookie)) {
		t.Fatal("export DTO exposed credentials")
	}
}

func TestAuthTransferExportNoClobber(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "dangling symlink", "directory", "missing parent", "unsafe parent", "symlink parent"} {
		t.Run(kind, func(t *testing.T) {
			store, tok, _, dir := authTransferFixture(t)
			saveAuthTransferToken(t, store, tok)
			output := filepath.Join(dir, "export.json")
			protected := filepath.Join(dir, "protected")
			if err := os.WriteFile(protected, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				output = protected
			case "symlink", "dangling symlink":
				target := protected
				if kind == "dangling symlink" {
					target += "-absent"
				}
				if err := os.Symlink(target, output); err != nil {
					t.Skip("symlinks unavailable")
				}
			case "directory":
				if err := os.Mkdir(output, 0o700); err != nil {
					t.Fatal(err)
				}
			case "missing parent":
				output = filepath.Join(dir, "absent", "export.json")
			case "unsafe parent", "symlink parent":
				parent := filepath.Join(dir, "parent")
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
				if kind == "unsafe parent" {
					if runtime.GOOS == "windows" {
						t.Skip("POSIX permissions")
					}
					if err := os.Chmod(parent, 0o777); err != nil {
						t.Fatal(err)
					}
				} else {
					link := filepath.Join(dir, "parent-link")
					if err := os.Symlink(parent, link); err != nil {
						t.Skip("symlinks unavailable")
					}
					parent = link
				}
				output = filepath.Join(parent, "export.json")
			}
			if _, err := exportAuth(context.Background(), store, cli.Workspace{TeamID: tok.TeamID}, output); err == nil {
				t.Fatal("unsafe or existing destination accepted")
			}
			data, err := os.ReadFile(protected)
			if err != nil || string(data) != "keep" {
				t.Fatal("preexisting file was changed")
			}
			if kind == "symlink" || kind == "dangling symlink" {
				if info, err := os.Lstat(output); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("preexisting symlink was removed")
				}
			}
		})
	}
}

func TestAuthTransferExportSymlinkDotDot(t *testing.T) {
	for _, form := range []string{"absolute", "relative"} {
		for _, stage := range []string{"success", "sync"} {
			t.Run(form+"/"+stage, func(t *testing.T) {
				store, tok, _, dir := authTransferFixture(t)
				saveAuthTransferToken(t, store, tok)
				safe := filepath.Join(dir, "safe")
				outside := filepath.Join(dir, "outside")
				child := filepath.Join(outside, "child")
				if err := os.Mkdir(safe, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(child, 0o700); err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" {
					if err := os.Chmod(outside, 0o777); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(child, filepath.Join(safe, "link")); err != nil {
					t.Skip("symlinks unavailable")
				}
				output := safe + "/link/../export.json"
				if form == "relative" {
					t.Chdir(dir)
					output = "safe/link/../export.json"
				}
				target := filepath.Join(safe, "export.json")
				if stage == "success" {
					doc, err := exportAuth(context.Background(), store, cli.Workspace{TeamID: tok.TeamID}, output)
					if err != nil {
						t.Fatal("export to normalized safe destination failed")
					}
					if doc.Output != output {
						t.Error("export result changed the requested output path")
					}
					data, err := os.ReadFile(target)
					if err != nil {
						t.Error("export missing from validated parent")
					} else if got, err := slackclient.DecodeAuthBundle(data); err != nil || got.AccessToken != tok.AccessToken || got.Cookie != tok.Cookie {
						t.Error("normalized export changed credentials")
					}
				} else {
					var opened string
					err := writeAuthExport(context.Background(), output, []byte("fake credentials"), func(path string) (authExportFile, error) {
						opened = path
						f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
						if err != nil {
							return nil, err
						}
						return failingAuthExportFile{File: f, stage: stage}, nil
					})
					assertAuthTransferCode(t, err, "failed")
					normalized, err := filepath.Abs(output)
					if err != nil {
						t.Fatal(err)
					}
					if opened != normalized {
						t.Error("open did not receive the normalized absolute destination")
					}
					if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
						t.Error("partial export remained in validated parent")
					}
				}
				if _, err := os.Lstat(filepath.Join(outside, "export.json")); !errors.Is(err, os.ErrNotExist) {
					t.Error("export created a file outside validated parent")
				}
			})
		}
	}
}

type failingAuthExportFile struct {
	*os.File
	stage string
}

func (f failingAuthExportFile) Write(data []byte) (int, error) {
	if f.stage == "write" {
		return 0, errors.New("xoxc-FAKE-ADVERSARIAL-WRITE")
	}
	if f.stage == "short write" {
		return len(data) - 1, nil
	}
	return f.File.Write(data)
}

func (f failingAuthExportFile) Sync() error {
	if f.stage == "sync" {
		return errors.New("FAKE-ADVERSARIAL-SYNC-COOKIE")
	}
	return f.File.Sync()
}

func (f failingAuthExportFile) Close() error {
	err := f.File.Close()
	if f.stage == "close" {
		return errors.New("FAKE-ADVERSARIAL-CLOSE-COOKIE")
	}
	return err
}

func TestAuthTransferExportFailureCleanup(t *testing.T) {
	for _, stage := range []string{"write", "short write", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "export.json")
			err := writeAuthExport(context.Background(), output, []byte("fake credentials"), func(path string) (authExportFile, error) {
				f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err != nil {
					return nil, err
				}
				return failingAuthExportFile{File: f, stage: stage}, nil
			})
			assertAuthTransferCode(t, err, "failed")
			if strings.Contains(err.Error(), "ADVERSARIAL") {
				t.Fatal("filesystem error leaked raw diagnostic")
			}
			if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("partial export not removed")
			}
		})
	}
}

func TestAuthTransferExportCancellationRemovesPartialFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := filepath.Join(t.TempDir(), "export.json")
	err := writeAuthExport(ctx, output, []byte("fake credentials"), func(path string) (authExportFile, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		cancel()
		return f, err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("export lost cancellation")
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled export left a partial file")
	}
}

func TestAuthTransferExportUntrustedStoredRecord(t *testing.T) {
	store, tok, _, dir := authTransferFixture(t)
	saveAuthTransferToken(t, store, tok)
	if err := os.WriteFile(filepath.Join(dir, "tokens", "T1.json"), []byte(`{"access_token":"xoxc-FAKE-STORED-SECRET"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "export.json")
	_, err := exportAuth(context.Background(), store, cli.Workspace{TeamID: "T1"}, output)
	assertAuthTransferCode(t, err, "invalid_auth_bundle")
	if strings.Contains(err.Error(), "FAKE-STORED-SECRET") {
		t.Fatal("invalid stored credentials leaked")
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid stored record created an export")
	}
}

func TestAuthTransferImportExactPair(t *testing.T) {
	store, tok, input, _ := authTransferFixture(t)
	calls := 0
	doc, err := importAuth(context.Background(), store, input, false, func(_ context.Context, got slackclient.Token) (string, string, error) {
		calls++
		if got != tok {
			t.Fatal("check did not receive exactly the file's login")
		}
		assertAuthTransferAbsent(t, store)
		return "T1", "U1", nil
	})
	if err != nil || calls != 1 || doc.Replaced || doc.User.ID != "U1" || doc.Workspace.TeamID != "T1" {
		t.Fatal("fresh import failed or returned incorrect metadata")
	}
	got, err := store.Load("T1")
	tok.UserID, tok.Source = "U1", slackclient.TokenSourceImported
	if err != nil || got != tok {
		t.Fatal("verified login or imported policy not saved")
	}
	encoded, _ := json.Marshal(doc)
	if bytes.Contains(encoded, []byte(tok.AccessToken)) || bytes.Contains(encoded, []byte(tok.Cookie)) {
		t.Fatal("import DTO exposed credentials")
	}
}

func TestAuthTransferImportValidationFailures(t *testing.T) {
	for _, tc := range []struct {
		name, team, user, code string
		err                    error
	}{
		{name: "team mismatch", team: "T2", user: "U1", code: "invalid_auth_bundle"},
		{name: "empty user", team: "T1", code: "invalid_auth_bundle"},
		{name: "unsafe user", team: "T1", user: "xoxc-FAKE-SECRET", code: "invalid_auth_bundle"},
		{name: "invalid", err: slack.SlackErrorResponse{Err: "invalid_auth"}, code: "auth_failed"},
		{name: "not authed", err: slack.SlackErrorResponse{Err: "not_authed"}, code: "auth_failed"},
		{name: "inactive", err: slack.SlackErrorResponse{Err: "account_inactive"}, code: "auth_failed"},
		{name: "revoked", err: slack.SlackErrorResponse{Err: "token_revoked"}, code: "auth_failed"},
		{name: "expired", err: slack.SlackErrorResponse{Err: "token_expired"}, code: "auth_failed"},
		{name: "network", err: errors.New("FAKE-SECRET-NETWORK"), code: "failed"},
		{name: "unknown slack", err: slack.SlackErrorResponse{Err: "FAKE-SECRET-ERROR"}, code: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _, input, _ := authTransferFixture(t)
			_, err := importAuth(context.Background(), store, input, false, func(context.Context, slackclient.Token) (string, string, error) {
				return tc.team, tc.user, tc.err
			})
			assertAuthTransferCode(t, err, tc.code)
			assertAuthTransferAbsent(t, store)
		})
	}
}

func TestAuthTransferImportUnsafeOrInvalidInput(t *testing.T) {
	for _, kind := range []string{"oversize", "invalid", "unknown secret field", "directory", "symlink", "permissive", "unsafe store", "corrupt store"} {
		t.Run(kind, func(t *testing.T) {
			store, tok, input, dir := authTransferFixture(t)
			code := "unsafe_auth_file"
			switch kind {
			case "oversize", "invalid", "unknown secret field":
				code = "invalid_auth_bundle"
				data := "FAKE-SECRET-INVALID"
				if kind == "oversize" {
					data = strings.Repeat(" ", slackclient.AuthBundleLimit+1)
				}
				if kind == "unknown secret field" {
					data = `{"FAKE-SECRET-FIELD":"FAKE-SECRET-VALUE"}`
				}
				if err := os.WriteFile(input, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				input = dir
			case "symlink":
				link := filepath.Join(dir, "link.json")
				if err := os.Symlink(input, link); err != nil {
					t.Skip("symlinks unavailable")
				}
				input = link
			case "permissive":
				if runtime.GOOS == "windows" {
					t.Skip("POSIX permissions")
				}
				if err := os.Chmod(input, 0o644); err != nil {
					t.Fatal(err)
				}
			case "unsafe store", "corrupt store":
				saveAuthTransferToken(t, store, tok)
				target := filepath.Join(dir, "tokens", "T1.json")
				if kind == "corrupt store" {
					code = "invalid_auth_bundle"
					if err := os.WriteFile(target, []byte("FAKE-SECRET-CORRUPT"), 0o600); err != nil {
						t.Fatal(err)
					}
				} else {
					if runtime.GOOS == "windows" {
						t.Skip("POSIX permissions")
					}
					if err := os.Chmod(target, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			_, err := importAuth(context.Background(), store, input, true, func(context.Context, slackclient.Token) (string, string, error) {
				t.Fatal("unsafe input or store reached network check")
				return "", "", nil
			})
			assertAuthTransferCode(t, err, code)
			if strings.Contains(err.Error(), "FAKE-SECRET") {
				t.Fatal("input leaked into diagnostics")
			}
			if kind == "permissive" && !strings.Contains(err.Error(), "chmod 600") {
				t.Fatal("permission refusal lacks recovery instruction")
			}
		})
	}
}

func TestAuthTransferImportReplacement(t *testing.T) {
	for _, kind := range []string{"no replace", "known same", "known other", "legacy same", "legacy other", "legacy expired", "legacy network", "legacy wrong team", "legacy empty user", "legacy cancelled", "legacy timeout"} {
		t.Run(kind, func(t *testing.T) {
			store, candidate, input, _ := authTransferFixture(t)
			old := candidate
			old.AccessToken, old.Cookie = "xoxc-FAKE-OLD-SECRET", "FAKE-OLD-COOKIE"
			if strings.HasPrefix(kind, "known") {
				old.UserID = "U1"
				if kind == "known other" {
					old.UserID = "U2"
				}
			}
			saveAuthTransferToken(t, store, old)
			calls := 0
			doc, err := importAuth(context.Background(), store, input, kind != "no replace", func(_ context.Context, got slackclient.Token) (string, string, error) {
				calls++
				if got == candidate {
					return "T1", "U1", nil
				}
				if got != old {
					t.Fatal("legacy check did not use exact stored pair")
				}
				switch kind {
				case "legacy other":
					return "T1", "U2", nil
				case "legacy expired":
					return "", "", slack.SlackErrorResponse{Err: "invalid_auth"}
				case "legacy network":
					return "", "", errors.New("FAKE-SECRET-FAILURE")
				case "legacy wrong team":
					return "T2", "U1", nil
				case "legacy empty user":
					return "T1", "", nil
				case "legacy cancelled":
					return "", "", fmt.Errorf("FAKE-SECRET: %w", context.Canceled)
				case "legacy timeout":
					return "", "", fmt.Errorf("FAKE-SECRET: %w", context.DeadlineExceeded)
				}
				return "T1", "U1", nil
			})
			wantCalls := 1
			if kind == "no replace" {
				wantCalls = 0
			} else if strings.HasPrefix(kind, "legacy") {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("checks = %d; want %d", calls, wantCalls)
			}
			got, loadErr := store.Load("T1")
			if kind == "known same" || kind == "legacy same" {
				if err != nil || !doc.Replaced || loadErr != nil || got.AccessToken != candidate.AccessToken || got.UserID != "U1" || got.Source != slackclient.TokenSourceImported {
					t.Fatal("same-account replacement failed")
				}
			} else {
				switch kind {
				case "legacy cancelled":
					if !errors.Is(err, context.Canceled) {
						t.Fatal("legacy check cancellation was lost")
					}
				case "legacy timeout":
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal("legacy check timeout was lost")
					}
				default:
					assertAuthTransferCode(t, err, "auth_conflict")
				}
				if loadErr != nil || got != old {
					t.Fatal("refused replacement changed existing login")
				}
				if strings.Contains(err.Error(), "FAKE-SECRET") {
					t.Fatal("legacy verification error leaked")
				}
				if kind != "no replace" && kind != "legacy cancelled" && kind != "legacy timeout" && !strings.Contains(err.Error(), "XDG_DATA_HOME") {
					t.Fatal("account conflict lacks isolation/recovery guidance")
				}
			}
		})
	}
}

func TestAuthTransferImportConcurrentChanges(t *testing.T) {
	for _, kind := range []string{"create", "replace", "delete", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			store, candidate, input, _ := authTransferFixture(t)
			old := candidate
			old.AccessToken, old.UserID = "xoxc-FAKE-OLD", "U1"
			if kind != "create" {
				saveAuthTransferToken(t, store, old)
			}
			newer := old
			newer.AccessToken = "xoxc-FAKE-NEWER"
			if kind == "metadata" {
				newer.AccessToken, newer.TeamName = old.AccessToken, "Renamed"
			}
			_, err := importAuth(context.Background(), store, input, true, func(context.Context, slackclient.Token) (string, string, error) {
				if kind == "delete" {
					if err := store.Delete("T1"); err != nil {
						t.Fatal("concurrent delete failed")
					}
				} else {
					saveAuthTransferToken(t, store, newer)
				}
				return "T1", "U1", nil
			})
			assertAuthTransferCode(t, err, "auth_conflict")
			if kind == "delete" {
				assertAuthTransferAbsent(t, store)
			} else if got, err := store.Load("T1"); err != nil || got != newer {
				t.Fatal("import overwrote a concurrent store change")
			}
		})
	}
}

func TestAuthTransferImportConcurrentImports(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			store, tok, input, dir := authTransferFixture(t)
			if replacement {
				old := tok
				old.AccessToken, old.UserID = "xoxc-FAKE-OLD", "U1"
				saveAuthTransferToken(t, store, old)
			}
			var ready sync.WaitGroup
			ready.Add(2)
			gate := make(chan struct{})
			results := make(chan error, 2)
			for range 2 {
				go func() {
					_, err := importAuth(context.Background(), slackclient.NewTokenStore(filepath.Join(dir, "tokens")), input, replacement, func(context.Context, slackclient.Token) (string, string, error) {
						ready.Done()
						<-gate
						return "T1", "U1", nil
					})
					results <- err
				}()
			}
			ready.Wait()
			close(gate)
			successes := 0
			for range 2 {
				if err := <-results; err == nil {
					successes++
				} else {
					assertAuthTransferCode(t, err, "auth_conflict")
				}
			}
			if successes != 1 {
				t.Fatalf("successful guarded imports = %d; want 1", successes)
			}
		})
	}
}

func TestAuthTransferCancellation(t *testing.T) {
	for _, stage := range []string{"before import", "candidate check", "legacy check", "deadline", "before export"} {
		t.Run(stage, func(t *testing.T) {
			store, tok, input, dir := authTransferFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "before import" || stage == "before export" {
				cancel()
			}
			var err error
			if stage == "before export" {
				saveAuthTransferToken(t, store, tok)
				output := filepath.Join(dir, "export.json")
				_, err = exportAuth(ctx, store, cli.Workspace{TeamID: "T1"}, output)
				if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatal("cancelled export created a file")
				}
			} else {
				old := tok
				old.AccessToken = "xoxc-FAKE-OLD"
				if stage == "legacy check" {
					saveAuthTransferToken(t, store, old)
				}
				_, err = importAuth(ctx, store, input, true, func(_ context.Context, got slackclient.Token) (string, string, error) {
					if stage == "deadline" {
						return "", "", fmt.Errorf("FAKE-SECRET: %w", context.DeadlineExceeded)
					}
					if stage != "legacy check" || got == old {
						cancel()
					}
					return "T1", "U1", nil
				})
				if stage == "legacy check" {
					if got, err := store.Load("T1"); err != nil || got != old {
						t.Fatal("cancelled legacy check changed login")
					}
				} else {
					assertAuthTransferAbsent(t, store)
				}
			}
			want := context.Canceled
			if stage == "deadline" {
				want = context.DeadlineExceeded
			}
			if !errors.Is(err, want) {
				t.Fatalf("cancellation classification = %T", err)
			}
			if strings.Contains(err.Error(), "FAKE-SECRET") {
				t.Fatal("wrapped cancellation exposed raw error")
			}
		})
	}
}

func TestAuthTransferBoundaryErrorsNeverLeak(t *testing.T) {
	secret := "xoxc-FAKE-ADVERSARIAL-ERROR-COOKIE"
	for _, tc := range []struct {
		name, code, slackCode string
		err                   error
	}{
		{"raw", "failed", "", errors.New(secret)},
		{"cli", "failed", "", &cli.Error{Code: "failed", Message: secret, SlackError: secret}},
		{"unknown slack", "failed", "", slack.SlackErrorResponse{Err: secret}},
		{"wrapped refusal", "auth_failed", "invalid_auth", fmt.Errorf("%s: %w", secret, slack.SlackErrorResponse{Err: "invalid_auth"})},
		{"known slack", "slack_error", "internal_error", fmt.Errorf("%s: %w", secret, slack.SlackErrorResponse{Err: "internal_error"})},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%v", tc.name, jsonOutput), func(t *testing.T) {
				store, _, input, _ := authTransferFixture(t)
				var stdout, stderr, logs bytes.Buffer
				oldLog := log.Writer()
				log.SetOutput(&logs)
				t.Cleanup(func() { log.SetOutput(oldLog) })
				args := []string{"auth", "import", "--input", input}
				if jsonOutput {
					args = append(args, "--json")
				}
				env := cli.Env{Stdout: &stdout, Stderr: &stderr, ImportAuth: func(ctx context.Context, path string, replace bool) (cli.AuthImportResult, error) {
					return importAuth(ctx, store, path, replace, func(context.Context, slackclient.Token) (string, string, error) { return "", "", tc.err })
				}}
				if exit := cli.Main(context.Background(), args, env); exit == 0 || stdout.Len() != 0 {
					t.Fatal("failed import reported success")
				}
				combined := stdout.String() + stderr.String() + logs.String()
				if strings.Contains(combined, secret) {
					t.Fatal("secret leaked in command output or logs")
				}
				if jsonOutput {
					var doc struct{ Error cli.Error }
					if err := json.Unmarshal(stderr.Bytes(), &doc); err != nil || doc.Error.Code != tc.code || doc.Error.SlackError != tc.slackCode {
						t.Fatal("sanitized boundary lost stable error classification")
					}
				}
				assertAuthTransferAbsent(t, store)
			})
		}
	}
}

type authTransferTransport func(*http.Request) (*http.Response, error)

func (f authTransferTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAuthTransferCLIRealClientWiring(t *testing.T) {
	_, candidate, input, dir := authTransferFixture(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/auth.test" || r.ParseForm() != nil || r.Form.Get("token") != candidate.AccessToken {
			t.Error("real client did not check supplied token at auth.test")
		}
		cookie, err := r.Cookie("d")
		if err != nil || cookie.Value != candidate.Cookie {
			t.Error("real client did not use the supplied cookie")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"team_id":"T1","user_id":"U1"}`)
	}))
	defer srv.Close()
	previous := http.DefaultTransport
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	http.DefaultTransport = authTransferTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		u := *r.URL
		clone.URL = &u
		clone.URL.Scheme = "http"
		clone.URL.Host = srv.Listener.Addr().String()
		return base.RoundTrip(clone)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	env := newCLIEnv()
	if env.ExportAuth == nil || env.ImportAuth == nil {
		t.Fatal("CLI transfer operations not wired")
	}
	var stdout, stderr bytes.Buffer
	env.Stdout, env.Stderr = &stdout, &stderr
	if exit := cli.Main(context.Background(), []string{"auth", "import", "--input", input, "--json"}, env); exit != 0 {
		t.Fatalf("real-client import exit = %d", exit)
	}
	if calls != 1 {
		t.Fatalf("auth.test calls = %d; want 1", calls)
	}
	store := slackclient.NewTokenStore(filepath.Join(xdgData(), "tokens"))
	got, err := store.Load("T1")
	if err != nil || got.AccessToken != candidate.AccessToken || got.Cookie != candidate.Cookie || got.UserID != "U1" || got.Source != slackclient.TokenSourceImported {
		t.Fatal("real CLI wiring did not save the validated pair")
	}
	stdout.Reset()
	output := filepath.Join(dir, "export.json")
	if exit := cli.Main(context.Background(), []string{"auth", "export", "--output", output, "--json"}, env); exit != 0 || calls != 1 {
		t.Fatal("CLI export failed or performed network I/O")
	}
	if strings.Contains(stdout.String()+stderr.String(), candidate.AccessToken) || strings.Contains(stdout.String()+stderr.String(), candidate.Cookie) {
		t.Fatal("real CLI wiring leaked credentials")
	}
}
