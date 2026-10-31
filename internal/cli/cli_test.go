package cli

import (
	"encoding/json"
	"flag"
	"slices"
	"strings"
	"testing"
)

// TestHandles pins what main() hands to the CLI: a first argument that
// is a word, or a global flag that comes before the command words.
// main()'s own flags, and no arguments (the TUI), stay with main().
func TestHandles(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"workspace", "list"}, true},
		{[]string{"nonsense"}, true},
		{[]string{"--json", "workspace", "list"}, true},
		{[]string{"-json"}, true},
		{[]string{"--workspace=acme", "workspace", "whoami"}, true},
		{[]string{"--workspace", "acme", "workspace", "whoami"}, true},
		{[]string{"--add-workspace"}, false},
		{[]string{"--demo"}, false},
		{[]string{"-"}, false},
		{[]string{"--"}, false},
		// After "--" every argument is a word, so the line is the CLI's.
		{[]string{"--", "workspace", "list"}, true},
		{[]string{"--", "help", "workspace", "--help"}, true},
		{[]string{"--", "--json"}, true},
	}
	for _, tc := range cases {
		if got := Handles(tc.args); got != tc.want {
			t.Errorf("Handles(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestMain_UnknownCommandExits2 is the guard behind the behavior
// change: a mistyped command is a usage error, never the TUI.
func TestMain_UnknownCommandExits2(t *testing.T) {
	_, stderr, exit := runCLI(t, []string{"wrokspace", "list"}, withWorkspaces(acme))
	if exit != exitUsage {
		t.Errorf("exit = %d, want %d", exit, exitUsage)
	}
	if !strings.Contains(stderr, `unknown command "wrokspace"`) {
		t.Errorf("stderr = %q, want the unknown word named", stderr)
	}

	_, stderr, exit = runCLI(t, []string{"workspace", "lsit", "--json"}, withWorkspaces(acme))
	if exit != exitUsage {
		t.Errorf("unknown verb: exit = %d, want %d", exit, exitUsage)
	}
	var doc struct{ Error Error }
	if err := json.Unmarshal([]byte(stderr), &doc); err != nil || doc.Error.Code != codeUsage {
		t.Errorf("unknown verb under --json: stderr = %q, want a usage error document", stderr)
	}
}

func TestMain_MissingCommandIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"workspace"}, {"workspace", "--json"}} {
		if _, _, exit := runCLI(t, args); exit != exitUsage {
			t.Errorf("%q: exit = %d, want %d", args, exit, exitUsage)
		}
	}
}

// TestMain_HelpListsNouns checks `slk help`, which is also the command
// block of `slk --help` (Overview), against the table.
func TestMain_HelpListsNouns(t *testing.T) {
	stdout, _, exit := runCLI(t, []string{"help"})
	if exit != exitOK {
		t.Fatalf("exit = %d", exit)
	}
	if stdout != Overview() {
		t.Errorf("slk help = %q, want Overview()", stdout)
	}
	for _, c := range commands {
		if !strings.Contains(stdout, "slk "+c.Path[0]) {
			t.Errorf("overview misses %q:\n%s", "slk "+c.Path[0], stdout)
		}
		if len(c.Path) > 1 && !strings.Contains(stdout, c.Path[1]) {
			t.Errorf("overview misses verb %q:\n%s", c.Path[1], stdout)
		}
	}
}

func TestMain_NounHelpListsVerbs(t *testing.T) {
	for _, args := range [][]string{{"help", "workspace"}, {"workspace", "--help"}, {"workspace", "-h"}} {
		stdout, _, exit := runCLI(t, args)
		if exit != exitOK {
			t.Errorf("%q: exit = %d, want 0", args, exit)
		}
		for _, verb := range []string{"slk workspace list", "slk workspace whoami"} {
			if !strings.Contains(stdout, verb) {
				t.Errorf("%q: help misses %q:\n%s", args, verb, stdout)
			}
		}
	}
}

func TestMain_VerbHelpShowsFlags(t *testing.T) {
	for _, args := range [][]string{{"help", "workspace", "whoami"}, {"workspace", "whoami", "--help"}, {"--json", "workspace", "whoami", "-h"}} {
		stdout, _, exit := runCLI(t, args, withConnect(nil))
		if exit != exitOK {
			t.Errorf("%q: exit = %d, want 0", args, exit)
		}
		for _, want := range []string{"slk workspace whoami [flags]", "-json", "-workspace"} {
			if !strings.Contains(stdout, want) {
				t.Errorf("%q: help misses %q:\n%s", args, want, stdout)
			}
		}
	}
	if _, _, exit := runCLI(t, []string{"help", "workspace", "nope"}); exit != exitUsage {
		t.Errorf("help for an unknown command: exit = %d, want %d", exit, exitUsage)
	}
}

// TestMain_GlobalFlagsBeforeOrAfterTheCommand: an agent can put --json
// and --workspace on either side of the command words.
func TestMain_GlobalFlagsBeforeOrAfterTheCommand(t *testing.T) {
	var outputs []string
	for _, args := range [][]string{
		{"workspace", "whoami", "--json", "--workspace", "side-project"},
		{"--json", "--workspace", "side-project", "workspace", "whoami"},
		{"--workspace=side-project", "workspace", "whoami", "-json"},
	} {
		stdout, stderr, exit := runCLI(t, args, withWorkspaces(acme, side))
		if exit != exitOK {
			t.Fatalf("%q: exit = %d, stderr %q", args, exit, stderr)
		}
		outputs = append(outputs, stdout)
	}
	if !strings.Contains(outputs[0], side.TeamID) {
		t.Errorf("--workspace side-project was not used:\n%s", outputs[0])
	}
	for i, out := range outputs[1:] {
		if out != outputs[0] {
			t.Errorf("flag order %d changed the output:\n%s\nwant\n%s", i+1, out, outputs[0])
		}
	}
}

func TestMain_BadFlagsAndArgumentsExit2(t *testing.T) {
	for _, args := range [][]string{
		{"workspace", "list", "--nope"},
		{"--nope", "workspace", "list"},
		{"workspace", "list", "extra"},
		{"workspace", "whoami", "--workspace"},
		{"skill", "--json=maybe"},
	} {
		_, stderr, exit := runCLI(t, args, withWorkspaces(acme))
		if exit != exitUsage {
			t.Errorf("%q: exit = %d, want %d (stderr %q)", args, exit, exitUsage, stderr)
		}
		if !strings.HasPrefix(stderr, "Error: ") {
			t.Errorf("%q: stderr = %q, want an Error: line", args, stderr)
		}
	}
}

func TestJSONRequested(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"workspace", "list"}, false},
		{[]string{"workspace", "list", "--json"}, true},
		{[]string{"-json", "workspace", "list"}, true},
		{[]string{"--json=true"}, true},
		{[]string{"--json", "--json=false"}, false},
		// An invalid value changes nothing: the last valid setting wins.
		{[]string{"--json", "--json=maybe"}, true},
		{[]string{"--json=maybe"}, false},
		// A flag that takes a value consumes the next argument.
		{[]string{"--json", "--workspace", "--json=false"}, true},
		{[]string{"--workspace", "--json"}, false},
		{[]string{"--workspace=acme", "--json"}, true},
		// Read on past an unknown flag, which fs.Parse would stop at.
		{[]string{"--nope", "--json"}, true},
		// "--" ends the flags, as in the parse.
		{[]string{"--", "--json"}, false},
		{[]string{"--", "--json", "x"}, false},
		{[]string{"--json", "--", "--json=false", "--nope"}, true},
		{[]string{"--workspace", "--", "--json"}, true}, // "--" is the workspace
		{[]string{"--jsonx"}, false},
		{[]string{"---json"}, false},
	}
	fs, _ := newGlobalFlagSet("slk")
	for _, tc := range cases {
		if got := jsonRequested(fs, tc.args); got != tc.want {
			t.Errorf("jsonRequested(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// TestMain_ParseErrorsKeepTheRequestedJSONFormat: a script that asked
// for JSON gets its error as JSON, also when the error is in the flags,
// a flag's value looks like --json, or the bad flag comes first.
func TestMain_ParseErrorsKeepTheRequestedJSONFormat(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "skill", "--json=maybe"},
		{"skill", "--json", "--workspace", "--json=false", "--nope"},
		{"skill", "--nope", "--json"},
		{"workspace", "list", "--json", "--workspace"},
		{"--json", "--nope", "workspace", "list"},
	} {
		stdout, stderr, exit := runCLI(t, args, withWorkspaces(acme))
		if exit != exitUsage || stdout != "" {
			t.Errorf("%q: exit %d, stdout %q; want 2 and nothing", args, exit, stdout)
		}
		var doc struct{ Error Error }
		if err := json.Unmarshal([]byte(stderr), &doc); err != nil || doc.Error.Code != codeUsage {
			t.Errorf("%q: stderr is not a usage error document (%v):\n%s", args, err, stderr)
		}
	}

	// The last valid setting wins, so this one asked for text.
	_, stderr, exit := runCLI(t, []string{"skill", "--json=true", "--json=false", "--nope"})
	if exit != exitUsage || !strings.HasPrefix(stderr, "Error: ") {
		t.Errorf("--json=false: exit %d, stderr %q; want 2 and a text error", exit, stderr)
	}
}

func TestParseInterspersed(t *testing.T) {
	cases := []struct {
		args []string
		want []string
		ws   string
		json bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, "", false},
		{[]string{"a", "--workspace", "acme", "b"}, []string{"a", "b"}, "acme", false},
		{[]string{"--workspace=acme", "a", "b", "--json"}, []string{"a", "b"}, "acme", true},
		{[]string{"a", "b", "--workspace", "acme"}, []string{"a", "b"}, "acme", false},
		// "--" ends the flags: everything after it is positional, also
		// what looks like a flag, however many arguments follow.
		{[]string{"--", "-x"}, []string{"-x"}, "", false},
		{[]string{"a", "--", "b", "--json", "-x"}, []string{"a", "b", "--json", "-x"}, "", false},
		{[]string{"--json", "--", "--json=false", "c"}, []string{"--json=false", "c"}, "", true},
		// A value is a value, whatever it looks like: "--" too.
		{[]string{"--workspace", "--", "a", "--json"}, []string{"a"}, "--", true},
		{[]string{"--workspace", "--help", "a"}, []string{"a"}, "--help", false},
		{nil, nil, "", false},
	}
	for _, tc := range cases {
		fs, g := newGlobalFlagSet("test")
		got, err := parseInterspersed(fs, tc.args)
		if err != nil || !slices.Equal(got, tc.want) || g.workspace != tc.ws || g.json != tc.json {
			t.Errorf("parseInterspersed(%q) = %q, workspace %q, json %v, %v; want %q, %q, %v",
				tc.args, got, g.workspace, g.json, err, tc.want, tc.ws, tc.json)
		}
		// The parse and the choice of error format read alike.
		if req := jsonRequested(fs, tc.args); req != tc.json {
			t.Errorf("jsonRequested(%q) = %v, but the parse set json %v", tc.args, req, tc.json)
		}
	}
	for _, args := range [][]string{{"a", "--nope"}, {"a", "--workspace"}, {"---json"}} {
		fs, _ := newGlobalFlagSet("test")
		if _, err := parseInterspersed(fs, args); err == nil {
			t.Errorf("parseInterspersed(%q) accepted a bad flag", args)
		}
	}
}

// TestMain_DoubleDashEndsTheFlagsEverywhere: after "--" every argument
// is positional, for the parse and for the error format alike, so a
// --json or --json=false after it changes neither.
func TestMain_DoubleDashEndsTheFlagsEverywhere(t *testing.T) {
	// The --json after "--" is a topic word: the error is text.
	_, stderr, exit := runCLI(t, []string{"help", "--", "workspace", "--json", "--nope"})
	if exit != exitUsage || !strings.HasPrefix(stderr, `Error: unknown command "workspace --json --nope"`) {
		t.Errorf("help -- … --json: exit %d, stderr %q; want 2 and a text error", exit, stderr)
	}

	// The --json=false after "--" is a topic word too: the error stays JSON.
	_, stderr, exit = runCLI(t, []string{"help", "--json", "--", "workspace", "--json=false", "--nope"})
	var doc struct{ Error Error }
	if exit != exitUsage || json.Unmarshal([]byte(stderr), &doc) != nil || doc.Error.Code != codeUsage {
		t.Errorf("help --json -- … --json=false: exit %d, stderr %q; want 2 and a JSON error", exit, stderr)
	}

	// The same for a command: after "--", --json is an argument.
	_, stderr, exit = runCLI(t, []string{"workspace", "list", "--", "--json", "x"}, withWorkspaces(acme))
	if exit != exitUsage || !strings.HasPrefix(stderr, `Error: unexpected argument "--json"`) {
		t.Errorf("workspace list -- --json: exit %d, stderr %q; want 2 and a text error", exit, stderr)
	}
}

// TestMain_LeadingDoubleDashEndsTheFlagsForEveryRoute: a "--" before the
// command words ends the flags for the whole line, whichever route the
// words take (help, a bare noun, a command). What follows is
// positional, so a --help there asks for no help, and a --json or
// --json=false there does not change the error format, which only the
// flags before "--" decide.
func TestMain_LeadingDoubleDashEndsTheFlagsForEveryRoute(t *testing.T) {
	routes := []struct {
		words []string
		want  func(tail string) string // how the usage error's message starts
	}{
		{[]string{"help", "workspace"}, func(tail string) string { return `unknown command "workspace ` + tail + `"` }},
		{[]string{"workspace"}, func(tail string) string { return `unknown command "slk workspace ` + tail + `"` }},
		{[]string{"workspace", "list"}, func(tail string) string { return `unexpected argument "` + tail + `"` }},
	}
	for _, route := range routes {
		for _, tail := range []string{"--help", "--json", "--json=false"} {
			for _, lead := range [][]string{{"--json"}, nil} {
				args := slices.Concat(lead, []string{"--"}, route.words, []string{tail})
				stdout, stderr, exit := runCLI(t, args, withWorkspaces(acme))
				if exit != exitUsage || stdout != "" {
					t.Errorf("%q: exit %d, stdout %q; want 2 and nothing", args, exit, stdout)
					continue
				}
				msg := strings.TrimPrefix(stderr, "Error: ")
				if lead != nil {
					var doc struct{ Error Error }
					if err := json.Unmarshal([]byte(stderr), &doc); err != nil {
						t.Errorf("%q: stderr is not the JSON error asked for before \"--\": %q", args, stderr)
						continue
					}
					msg = doc.Error.Message
				} else if !strings.HasPrefix(stderr, "Error: ") {
					t.Errorf("%q: stderr %q; want a text error", args, stderr)
					continue
				}
				if want := route.want(tail); !strings.HasPrefix(msg, want) {
					t.Errorf("%q: message %q; want it to start with %q", args, msg, want)
				}
			}
		}
	}
}

// TestMain_HelpTakesHelpFlagsAsFlagValues: in `--workspace --help` the
// help flag is the workspace's value, as in `--workspace=--help`, for
// every spelling of the help flag.
func TestMain_HelpTakesHelpFlagsAsFlagValues(t *testing.T) {
	want, _, _ := runCLI(t, []string{"help", "workspace", "whoami"})
	for _, alias := range []string{"-h", "--h", "-help", "--help"} {
		for _, args := range [][]string{
			{"help", "workspace", "whoami", "--workspace", alias},
			{"help", "workspace", "whoami", "--workspace=" + alias},
			{"help", "workspace", "whoami", "--workspace", alias, "--json"},
			{"help", "--workspace", alias, "workspace", "whoami"},
		} {
			stdout, stderr, exit := runCLI(t, args)
			if exit != exitOK || stdout != want {
				t.Errorf("%q: exit %d, stderr %q; want the command's help", args, exit, stderr)
			}
		}
		args := []string{"help", "workspace", "whoami", "--workspace", alias, "--nope"}
		if _, _, exit := runCLI(t, args); exit != exitUsage {
			t.Errorf("%q: exit %d, want %d for the unknown flag after the value", args, exit, exitUsage)
		}
	}
}

// TestMain_NounHelpOnlyForARealHelpFlag: after a noun, a help flag asks
// for the noun's help only where it is a flag; a flag's value is not
// one, and an unknown flag before it is a usage error.
func TestMain_NounHelpOnlyForARealHelpFlag(t *testing.T) {
	noun := nounHelp("workspace")
	for _, args := range [][]string{
		{"workspace", "--help"},
		{"workspace", "--workspace", "acme", "-h"},
		{"workspace", "--help", "--nope"}, // the help flag comes first
	} {
		if stdout, stderr, exit := runCLI(t, args); exit != exitOK || stdout != noun {
			t.Errorf("%q: exit %d, stderr %q; want the noun's help", args, exit, stderr)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"workspace", "--workspace", "--help"}, "missing command after 'slk workspace'"},
		{[]string{"workspace", "--nope", "--help"}, "flag provided but not defined: -nope"},
		{[]string{"workspace", "--json", "list"}, "flags go before or after the command words"},
		{[]string{"workspace", "--workspace", "acme", "lsit"}, `unknown command "slk workspace lsit"`},
	} {
		stdout, stderr, exit := runCLI(t, tc.args)
		if exit != exitUsage || stdout != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("%q: exit %d, stdout %q, stderr %q; want 2 and %q", tc.args, exit, stdout, stderr, tc.want)
		}
	}
}

// TestMain_HelpAcceptsGlobalFlagsAnywhere: help reads the global flags
// the way every command does, whatever their spelling and place, so a
// flag's value never becomes a topic word.
func TestMain_HelpAcceptsGlobalFlagsAnywhere(t *testing.T) {
	cases := []struct {
		want string
		args [][]string
	}{
		{Overview(), [][]string{
			{"help", "--workspace", "acme"},
			{"--workspace", "acme", "help"},
			{"help", "--json"},
			{"help", "-h"},
		}},
		{nounHelp("workspace"), [][]string{
			{"help", "workspace", "--workspace", "acme"},
			{"help", "--workspace=acme", "workspace"},
			{"--json", "help", "workspace"},
		}},
		{commandHelp(lookup([]string{"workspace", "whoami"}), mustFlagSet(t, "workspace", "whoami")), [][]string{
			{"help", "workspace", "whoami", "--workspace", "acme"},
			{"help", "workspace", "whoami", "--workspace=acme"},
			{"--workspace", "acme", "help", "workspace", "whoami"},
			{"help", "workspace", "--json", "whoami"},
			{"help", "--workspace", "acme", "workspace", "whoami", "--json"},
		}},
	}
	for _, tc := range cases {
		for _, args := range tc.args {
			stdout, stderr, exit := runCLI(t, args)
			if exit != exitOK || stdout != tc.want {
				t.Errorf("%q: exit %d, stderr %q; stdout is not the expected help:\n%s", args, exit, stderr, stdout)
			}
		}
	}
}

// mustFlagSet is the flag set of the command at path.
func mustFlagSet(t *testing.T, path ...string) *flag.FlagSet {
	t.Helper()
	cmd := lookup(path)
	if cmd == nil {
		t.Fatalf("no command %q", path)
	}
	fs, _, _ := newFlagSet(cmd)
	return fs
}

func TestMain_HelpRejectsUnknownFlags(t *testing.T) {
	for _, args := range [][]string{
		{"help", "--nope"},
		{"help", "workspace", "--nope"},
		{"help", "workspace", "whoami", "--nope"},
		{"help", "workspace", "--workspace"},
	} {
		stdout, stderr, exit := runCLI(t, args)
		if exit != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "Error: ") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q; want a usage error", args, exit, stdout, stderr)
		}
	}
	_, stderr, _ := runCLI(t, []string{"help", "--nope", "--json"})
	var doc struct{ Error Error }
	if err := json.Unmarshal([]byte(stderr), &doc); err != nil || doc.Error.Code != codeUsage {
		t.Errorf("help --nope --json: stderr is not a usage error document:\n%s", stderr)
	}
}

// TestMain_HelpReportsAFailedWrite: help that cannot be written is a
// failure, as for every other command, not a silent exit 0.
func TestMain_HelpReportsAFailedWrite(t *testing.T) {
	for _, args := range [][]string{
		{"help"},                            // overview
		{"--json", "-h"},                    // overview, before any command
		{"help", "workspace"},               // noun help
		{"workspace", "--help"},             // noun help, without the help word
		{"help", "workspace", "whoami"},     // command help
		{"workspace", "whoami", "--help"},   // command help, from the command
		{"skill", "-h", "--workspace=acme"}, // a stand-alone command's help
	} {
		_, stderr, exit := runCLI(t, args, withStdout(failingWriter{}))
		if exit != exitFailure || !strings.Contains(stderr, "bad file descriptor") {
			t.Errorf("%q: exit %d, stderr %q; want 1 and the write error", args, exit, stderr)
		}
	}
}

// TestRegistry_EveryCommandIsComplete is the table's contract: every
// row can be dispatched and shown in help, and no path hides another.
func TestRegistry_EveryCommandIsComplete(t *testing.T) {
	var paths []string
	for _, c := range commands {
		path := strings.Join(c.Path, " ")
		if len(c.Path) == 0 || c.Summary == "" || c.Setup == nil {
			t.Errorf("command %q needs a path, a summary and a setup", path)
			continue
		}
		if slices.Contains(paths, path) {
			t.Errorf("command %q is in the table twice", path)
		}
		paths = append(paths, path)
		if len(c.Path) > 1 && nouns[c.Path[0]] == "" {
			t.Errorf("command %q: noun %q has no summary in nouns", path, c.Path[0])
		}
		if len(c.Path) == 1 && nouns[c.Path[0]] != "" {
			t.Errorf("command %q is also a noun, so its verbs would hide it", path)
		}
		if got := lookup(c.Path); got == nil || !slices.Equal(got.Path, c.Path) {
			t.Errorf("lookup(%q) does not find the command", path)
		}
	}
	for noun := range nouns {
		if len(verbsOf(noun)) == 0 {
			t.Errorf("noun %q has no commands", noun)
		}
	}
}
