package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
)

// updateGolden re-blesses the golden JSON files from current output:
//
//	go test ./internal/cli -run TestGolden -update
//
// It is package-local, like internal/ui's, so `go test ./... -update`
// fails.
var updateGolden = flag.Bool("update", false, "rewrite testdata/json golden files from current output")

// fakeEnv is the environment runCLI hands to Main.
type fakeEnv struct {
	workspaces []Workspace
	listErr    error
	vars       map[string]string
	connect    func(ctx context.Context, ws Workspace) (*Session, error)
	stdout     io.Writer // nil: runCLI captures stdout
}

// envOpt configures a fakeEnv.
type envOpt func(*fakeEnv)

// withWorkspaces sets the configured workspaces, in rail order.
func withWorkspaces(ws ...Workspace) envOpt {
	return func(e *fakeEnv) { e.workspaces = ws }
}

// withEnv sets an environment variable for Getenv.
func withEnv(key, value string) envOpt {
	return func(e *fakeEnv) { e.vars[key] = value }
}

// withConnect replaces the sign-in. The default signs in to any
// workspace as testUserID / testUserName.
func withConnect(fn func(ctx context.Context, ws Workspace) (*Session, error)) envOpt {
	return func(e *fakeEnv) { e.connect = fn }
}

// withListError makes Workspaces fail with err.
func withListError(err error) envOpt {
	return func(e *fakeEnv) { e.listErr = err }
}

// withStdout sends stdout to w instead of capturing it; runCLI then
// returns "" for stdout.
func withStdout(w io.Writer) envOpt {
	return func(e *fakeEnv) { e.stdout = w }
}

// failingWriter is a stdout that cannot be written, like a descriptor
// opened read-only.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write /dev/stdout: bad file descriptor")
}

const (
	testUserID   = "U0SELF01"
	testUserName = "alice"
)

// acme and side are the standard two-workspace fixture; acme is the
// default.
var (
	acme = Workspace{TeamID: "T0ACME01", Slug: "acme", Name: "Acme Inc", Domain: "acme", Default: true}
	side = Workspace{TeamID: "T0SIDE02", Slug: "", Name: "Side Project", Domain: "side-project"}
)

// runCLI runs args through Main with a fake environment and returns
// what the user would see. When the arguments hold a literal --json or
// -json it also checks the JSON contract: a success writes one JSON
// object to stdout (help, which is text, excepted), a failure one
// {"error": …} object to stderr and nothing to stdout, and neither
// holds a null.
func runCLI(t *testing.T, args []string, opts ...envOpt) (stdout, stderr string, exit int) {
	t.Helper()
	e := &fakeEnv{vars: map[string]string{}}
	e.connect = func(ctx context.Context, ws Workspace) (*Session, error) {
		return &Session{Workspace: ws, UserID: testUserID, UserName: testUserName}, nil
	}
	for _, opt := range opts {
		opt(e)
	}
	var out, errOut bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: &errOut,
		Getenv: func(k string) string { return e.vars[k] },
		Workspaces: func() ([]Workspace, error) {
			return e.workspaces, e.listErr
		},
		Connect: e.connect,
	}
	if e.stdout != nil {
		env.Stdout = e.stdout
	}
	exit = Main(context.Background(), args, env)
	stdout, stderr = out.String(), errOut.String()
	if jsonAsked(args) && (exit != exitOK || !helpAsked(args)) {
		if exit == exitOK {
			checkJSONDocument(t, "stdout", stdout)
		} else {
			if stdout != "" {
				t.Errorf("a failure wrote to stdout under --json: %q", stdout)
			}
			doc := checkJSONDocument(t, "stderr", stderr)
			if _, ok := doc["error"].(map[string]any); !ok {
				t.Errorf("stderr document has no \"error\" object: %s", stderr)
			}
		}
	}
	return stdout, stderr, exit
}

// jsonAsked is runCLI's own, deliberately literal reading of args: a
// test that wants the JSON contract checked passes --json or -json as
// such, before any "--" (after which every argument is positional). It
// does not call jsonRequested, so a bug there cannot switch off the
// check that would catch it; tests of other spellings assert the output
// themselves.
func jsonAsked(args []string) bool {
	if i := slices.Index(args, "--"); i >= 0 {
		args = args[:i]
	}
	return slices.Contains(args, "--json") || slices.Contains(args, "-json")
}

// helpAsked reports, as literally, whether args ask for help, which
// prints text even under --json.
func helpAsked(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		switch a {
		case "help", "-h", "--h", "-help", "--help":
			return true
		}
		return false
	})
}

// checkJSONDocument fails the test unless s is exactly one JSON object
// followed by a newline, with no null anywhere in it.
func checkJSONDocument(t *testing.T, stream, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewBufferString(s))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("%s is not a JSON object: %v\n%s", stream, err, s)
	}
	if _, err := dec.Token(); err != io.EOF {
		t.Errorf("%s holds more than one JSON document:\n%s", stream, s)
	}
	if len(s) == 0 || s[len(s)-1] != '\n' {
		t.Errorf("%s does not end in a newline", stream)
	}
	if path := findNull(doc, "$"); path != "" {
		t.Errorf("%s has a null at %s; empty lists are [] and missing strings \"\":\n%s", stream, path, s)
	}
	return doc
}

// findNull returns the path of the first null in v, or "".
func findNull(v any, path string) string {
	switch v := v.(type) {
	case nil:
		return path
	case map[string]any:
		for k, child := range v {
			if p := findNull(child, path+"."+k); p != "" {
				return p
			}
		}
	case []any:
		for i, child := range v {
			if p := findNull(child, path+"["+strconv.Itoa(i)+"]"); p != "" {
				return p
			}
		}
	}
	return ""
}

// compareGoldenJSON compares got with testdata/json/<name>.json, byte
// for byte, or rewrites the file under -update. The files pin every
// JSON shape, so a change to a field shows in review (D5: fields are
// added, never renamed or removed).
func compareGoldenJSON(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "json", name+".json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing or unreadable: %v\n"+
			"bless it with: go test ./internal/cli -run TestGolden -update", path, err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the output.\n--- want\n%s--- got\n%s"+
			"If the change is intended (additive only), bless it with: go test ./internal/cli -run TestGolden -update",
			path, want, got)
	}
}
