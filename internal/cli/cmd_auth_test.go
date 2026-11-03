package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

func TestGolden_AuthExport(t *testing.T) {
	stdout, stderr, exit := runCLI(t, []string{"auth", "export", "--output", "./acme-auth.json", "--json"}, withWorkspaces(acme))
	if exit != exitOK || stderr != "" {
		t.Fatalf("exit %d, stderr %q", exit, stderr)
	}
	compareGoldenJSON(t, "auth_export", stdout)
}

func TestGolden_AuthImport(t *testing.T) {
	for _, replace := range []bool{false, true} {
		args := []string{"auth", "import", "--input", "./acme-auth.json", "--json"}
		name := "auth_import"
		if replace {
			args = append(args, "--replace")
			name += "_replaced"
		}
		stdout, stderr, exit := runCLI(t, args)
		if exit != exitOK || stderr != "" {
			t.Fatalf("exit %d, stderr %q", exit, stderr)
		}
		compareGoldenJSON(t, name, stdout)
	}
}

func TestGolden_AuthErrors(t *testing.T) {
	for _, tc := range []struct {
		code    string
		message string
		exit    int
	}{
		{"invalid_auth_bundle", "invalid auth bundle", exitUsage},
		{"auth_conflict", "workspace already exists; use --replace for the same account", exitFailure},
		{"unsafe_auth_file", "auth file permissions must be 0600", exitFailure},
	} {
		t.Run(tc.code, func(t *testing.T) {
			_, stderr, exit := runCLI(t, []string{"auth", "import", "--input", "./acme-auth.json", "--json"},
				withImportAuth(func(context.Context, string, bool) (AuthImportResult, error) {
					return AuthImportResult{}, &Error{Code: tc.code, Message: tc.message}
				}))
			if exit != tc.exit {
				t.Fatalf("exit %d, want %d: %s", exit, tc.exit, stderr)
			}
			compareGoldenJSON(t, "error_"+tc.code, stderr)
		})
	}
}

func TestAuth_RequiredFileFlags(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "export"},
		{"auth", "export", "--output"},
		{"auth", "export", "--output="},
		{"auth", "export", "--output", "-"},
		{"auth", "export", "--output=ok.json", "--replace"},
		{"auth", "export", "--input=in.json"},
		{"auth", "export", "--output=ok.json", "extra"},
		{"auth", "import"},
		{"auth", "import", "--input"},
		{"auth", "import", "--input="},
		{"auth", "import", "--input", "-"},
		{"auth", "import", "--output=out.json"},
		{"auth", "import", "--input=in.json", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, stderr, exit := runCLI(t, append([]string{"--json"}, args...),
				withListError(errors.New("must not list workspaces")), withExportAuth(nil), withImportAuth(nil))
			var doc struct{ Error Error }
			if err := json.Unmarshal([]byte(stderr), &doc); err != nil || exit != exitUsage || doc.Error.Code != codeUsage {
				t.Errorf("exit %d, stderr %q; want usage", exit, stderr)
			}
			if len(args) > 2 && (args[2] == "--output" || args[2] == "--input") {
				if !strings.Contains(stderr, strings.TrimPrefix(args[2], "-")) {
					t.Errorf("error does not name the required flag: %s", stderr)
				}
			}
		})
	}
}

func TestAuthImport_RejectsExplicitWorkspaceEvenEmpty(t *testing.T) {
	for _, args := range [][]string{
		{"--workspace=acme", "auth", "import", "--input=in.json"},
		{"--workspace=", "auth", "import", "--input=in.json"},
		{"--workspace", "", "auth", "import", "--input=in.json"},
		{"auth", "import", "--workspace", "acme", "--input=in.json"},
		{"auth", "import", "--input=in.json", "--workspace="},
		{"auth", "import", "--input=in.json", "-workspace", ""},
		{"--workspace=acme", "auth", "import", "--workspace=", "--input=in.json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, stderr, exit := runCLI(t, append(args, "--json"), withImportAuth(nil), withConnect(nil),
				withListError(errors.New("must not list workspaces")))
			var doc struct{ Error Error }
			if err := json.Unmarshal([]byte(stderr), &doc); err != nil || exit != exitUsage || doc.Error.Code != codeUsage ||
				!strings.Contains(doc.Error.Message, "--workspace") {
				t.Errorf("exit %d, stderr %q; want usage naming --workspace", exit, stderr)
			}
		})
	}
}

func TestAuthImport_IgnoresWorkspaceDefaultsAndPassesFlags(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		input   string
		replace bool
	}{
		{[]string{"auth", "import", "--input", "./in.json"}, "./in.json", false},
		{[]string{"--json", "auth", "import", "--replace", "--input", "./in.json"}, "./in.json", true},
		{[]string{"auth", "import", "--input=./in.json", "--json", "--replace=false"}, "./in.json", false},
		{[]string{"auth", "import", "--input=--workspace", "--replace=true", "--json"}, "--workspace", true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			stdout, stderr, exit := runCLI(t, tc.args, withWorkspaces(side, acme), withEnv("SLK_WORKSPACE", "does-not-exist"),
				withListError(errors.New("import must not read local workspace metadata")), withConnect(nil), withExportAuth(nil),
				withImportAuth(func(ctx context.Context, input string, replace bool) (AuthImportResult, error) {
					calls++
					if ctx == nil || ctx.Err() != nil || input != tc.input || replace != tc.replace {
						t.Errorf("import args: input %q, replace %v, context %v", input, replace, ctx)
					}
					return AuthImportResult{Workspace: AuthWorkspace{TeamID: acme.TeamID, Name: acme.Name}, User: AuthUser{ID: testUserID}}, nil
				}))
			if exit != exitOK || stderr != "" || calls != 1 || !strings.Contains(stdout, acme.TeamID) {
				t.Errorf("exit %d, calls %d, stdout %q, stderr %q", exit, calls, stdout, stderr)
			}
		})
	}
}

func TestAuthExport_LocalWorkspaceSelection(t *testing.T) {
	plain := acme
	plain.Default = false
	for _, tc := range []struct {
		name   string
		args   []string
		env    string
		list   []Workspace
		chosen Workspace
	}{
		{"flag beats environment", []string{"--workspace=acme", "auth", "export", "--output=./out.json", "--json"}, side.Domain, []Workspace{acme, side}, acme},
		{"environment beats default", []string{"auth", "export", "--json", "--output=./out.json"}, side.Domain, []Workspace{acme, side}, side},
		{"default", []string{"auth", "export", "--output=./out.json", "--json"}, "", []Workspace{acme, side}, acme},
		{"only workspace", []string{"auth", "export", "--output=./out.json", "--json"}, "", []Workspace{side}, side},
		{"case insensitive name", []string{"auth", "export", "--output=./out.json", "--workspace=SIDE PROJECT", "--json"}, "", []Workspace{plain, side}, side},
		{"team id", []string{"auth", "export", "--workspace=" + side.TeamID, "--json", "--output=./out.json"}, "", []Workspace{plain, side}, side},
		{"empty flag preserves selection rules", []string{"--workspace=", "auth", "export", "--output=./out.json", "--json"}, side.Domain, []Workspace{acme, side}, side},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, stderr, exit := runCLI(t, tc.args, withWorkspaces(tc.list...), withEnv("SLK_WORKSPACE", tc.env), withConnect(nil), withImportAuth(nil),
				withExportAuth(func(ctx context.Context, ws Workspace, output string) (AuthExportResult, error) {
					calls++
					if ctx == nil || ctx.Err() != nil || ws != tc.chosen || output != "./out.json" {
						t.Errorf("export args: workspace %+v, output %q, context %v", ws, output, ctx)
					}
					return AuthExportResult{Workspace: AuthWorkspace{TeamID: ws.TeamID}, Output: output, Plaintext: true}, nil
				}))
			if exit != exitOK || stderr != "" || calls != 1 {
				t.Errorf("exit %d, calls %d, stderr %q", exit, calls, stderr)
			}
		})
	}
}

func TestAuthExport_SelectionFailures(t *testing.T) {
	plain := acme
	plain.Default = false
	for _, tc := range []struct {
		name       string
		list       []Workspace
		ref        string
		listErr    error
		code       string
		exit       int
		candidates int
	}{
		{"none", nil, "", nil, codeNoWorkspace, exitAuth, 0},
		{"ambiguous", []Workspace{plain, side}, "", nil, codeWorkspaceRequired, exitUsage, 2},
		{"unknown", []Workspace{acme, side}, "nope", nil, codeNotFound, exitNotFound, 2},
		{"store failure", nil, "", errors.New("list tokens: permission denied"), codeFailed, exitFailure, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, exit := runCLI(t, []string{"auth", "export", "--output=out.json", "--workspace=" + tc.ref, "--json"},
				withWorkspaces(tc.list...), withListError(tc.listErr), withExportAuth(nil), withConnect(nil))
			var doc struct{ Error Error }
			if err := json.Unmarshal([]byte(stderr), &doc); err != nil || doc.Error.Code != tc.code || exit != tc.exit || len(doc.Error.Candidates) != tc.candidates {
				t.Errorf("exit %d, stderr %q", exit, stderr)
			}
			if tc.code == codeWorkspaceRequired {
				for _, want := range []string{"--workspace", "SLK_WORKSPACE", "default_workspace"} {
					if !strings.Contains(doc.Error.Message, want) {
						t.Errorf("ambiguity message misses %q: %s", want, stderr)
					}
				}
			}
		})
	}
}

func TestAuth_TextOutput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"auth", "export", "--output=./acme-auth.json"}, []string{"WARNING", "plaintext", "./acme-auth.json", acme.Name, acme.TeamID}},
		{[]string{"auth", "import", "--input=in.json"}, []string{acme.Name, acme.TeamID, testUserID, "Restart"}},
		{[]string{"auth", "import", "--input=in.json", "--replace"}, []string{acme.Name, acme.TeamID, testUserID, "Restart"}},
	} {
		stdout, stderr, exit := runCLI(t, tc.args, withWorkspaces(acme))
		if exit != exitOK || stderr != "" {
			t.Fatalf("%q: exit %d, stderr %q", tc.args, exit, stderr)
		}
		for _, want := range tc.want {
			if !strings.Contains(stdout, want) {
				t.Errorf("%q: output misses %q: %s", tc.args, want, stdout)
			}
		}
	}
}

func TestAuth_TransferErrors(t *testing.T) {
	for _, tc := range []struct {
		code string
		err  error
		exit int
	}{
		{"invalid_auth_bundle", &Error{Code: "invalid_auth_bundle", Message: "invalid document"}, exitUsage},
		{"auth_conflict", fmt.Errorf("import: %w", &Error{Code: "auth_conflict", Message: "workspace already exists"}), exitFailure},
		{"unsafe_auth_file", &Error{Code: "unsafe_auth_file", Message: "unsafe file permissions"}, exitFailure},
		{codeAuthFailed, slack.SlackErrorResponse{Err: "invalid_auth"}, exitAuth},
		{codeCancelled, context.Canceled, exitFailure},
		{codeFailed, errors.New("read auth file: permission denied"), exitFailure},
		{codeFailed, errors.New("dial tcp: no route to host"), exitFailure},
	} {
		for _, verb := range []string{"export", "import"} {
			for _, jsonOutput := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/json=%v/%s", verb, tc.code, jsonOutput, tc.err), func(t *testing.T) {
					fileFlag := "--input=in.json"
					if verb == "export" {
						fileFlag = "--output=out.json"
					}
					args := []string{"auth", verb, fileFlag}
					if jsonOutput {
						args = append(args, "--json")
					}
					stdout, stderr, exit := runCLI(t, args, withWorkspaces(acme),
						withExportAuth(func(context.Context, Workspace, string) (AuthExportResult, error) { return AuthExportResult{}, tc.err }),
						withImportAuth(func(context.Context, string, bool) (AuthImportResult, error) { return AuthImportResult{}, tc.err }))
					if exit != tc.exit || stdout != "" {
						t.Errorf("exit %d, stdout %q, stderr %q", exit, stdout, stderr)
					}
					if jsonOutput {
						var doc struct{ Error Error }
						if err := json.Unmarshal([]byte(stderr), &doc); err != nil || doc.Error.Code != tc.code {
							t.Errorf("stderr %q; want code %s", stderr, tc.code)
						}
					} else if !strings.HasPrefix(stderr, "Error: ") {
						t.Errorf("stderr %q; want text error", stderr)
					}
				})
			}
		}
	}
}

func TestAuth_OutputFailureDoesNotRepeatCompletedTransfer(t *testing.T) {
	for _, verb := range []string{"export", "import"} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%v", verb, jsonOutput), func(t *testing.T) {
				calls := 0
				args := []string{"auth", verb}
				if verb == "export" {
					args = append(args, "--output=out.json")
				} else {
					args = append(args, "--input=in.json")
				}
				if jsonOutput {
					args = append(args, "--json")
				}
				_, stderr, exit := runCLI(t, args, withWorkspaces(acme), withStdout(failingWriter{}),
					withExportAuth(func(context.Context, Workspace, string) (AuthExportResult, error) {
						calls++
						return AuthExportResult{Plaintext: true}, nil
					}), withImportAuth(func(context.Context, string, bool) (AuthImportResult, error) {
						calls++
						return AuthImportResult{}, nil
					}))
				if exit != exitFailure || calls != 1 || !strings.Contains(stderr, "bad file descriptor") {
					t.Errorf("exit %d, completed operations %d, stderr %q", exit, calls, stderr)
				}
			})
		}
	}
}

func TestAuth_RegistryAndHelp(t *testing.T) {
	for _, verb := range []string{"export", "import"} {
		if !Handles([]string{"auth", verb}) || !Handles([]string{"--json", "auth", verb}) {
			t.Errorf("auth %s is not routed to CLI", verb)
		}
		if lookup([]string{"auth", verb}) == nil {
			t.Errorf("auth %s is missing from registry", verb)
		}
	}
	for _, args := range [][]string{{"help"}, {"help", "auth"}, {"auth", "--help"}} {
		stdout, stderr, exit := runCLI(t, args)
		if exit != exitOK || stderr != "" || !strings.Contains(stdout, "auth") ||
			!strings.Contains(stdout, "export") || !strings.Contains(stdout, "import") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, exit, stdout, stderr)
		}
	}
	for _, verb := range []string{"export", "import"} {
		for _, args := range [][]string{{"help", "auth", verb}, {"--json", "auth", verb, "--help"}} {
			stdout, stderr, exit := runCLI(t, args, withConnect(nil), withExportAuth(nil), withImportAuth(nil))
			if exit != exitOK || stderr != "" {
				t.Fatalf("%q: exit %d, stderr %q", args, exit, stderr)
			}
			want := []string{"slk auth " + verb, "-json", "plaintext", "required", "'-'"}
			if verb == "export" {
				want = append(want, "-output", "existing")
			} else {
				want = append(want, "-input", "-replace", "--workspace", "restart")
			}
			for _, s := range want {
				if !strings.Contains(strings.ToLower(stdout), strings.ToLower(s)) {
					t.Errorf("%q: help misses %q: %s", args, s, stdout)
				}
			}
		}
	}
}

func TestAuth_SkillSafetyAndReruns(t *testing.T) {
	for _, want := range []string{"explicit user authorization", "plaintext", "--output", "--input", "--replace", "SLK_WORKSPACE", "restart", "existing file", "output fails", "imported"} {
		if !strings.Contains(strings.ToLower(skillMarkdown), strings.ToLower(want)) {
			t.Errorf("SKILL.md misses auth guidance %q", want)
		}
	}
}
