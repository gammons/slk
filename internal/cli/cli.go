// Package cli is slk's scriptable command line: `slk <noun> <verb>`
// commands for scripts and AI agents (gammons/slk#107). Each prints
// text for people or, with --json, exactly one JSON document. A
// command never asks for input and never starts the TUI. The one wait
// it cannot rule out is the OS asking the user to allow keychain
// access while an expired login is refreshed (connectWithRemint in
// cmd/slk).
//
// The command table (registry.go) drives dispatch and help. Everything
// from outside (the process's streams, the configured workspaces,
// Slack) arrives through Env, which cmd/slk/cli.go builds; tests build
// it from fakes.
package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"slices"
	"strconv"
	"strings"
)

// globalFlags are the flags every command accepts, before the command
// words or after them.
type globalFlags struct {
	json      bool
	workspace string
}

// jsonFlag is the name of the flag that asks for JSON output;
// jsonRequested looks for it before the flags are parsed.
const jsonFlag = "json"

// register declares the global flags on fs.
func (g *globalFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&g.json, jsonFlag, false, "write one JSON document to stdout (and a failure as one to stderr)")
	fs.StringVar(&g.workspace, "workspace", "", "the workspace's slug, team ID, name or domain\n(default: $SLK_WORKSPACE, else [general] default_workspace, else the only one)")
}

// newGlobalFlagSet returns a flag set with the global flags, the start
// of every command's flag set and, alone, the one for the parts of a
// command line no command owns: the flags before the command words,
// and help's arguments. Parse errors are reported by the caller, so
// the flag package writes nothing itself.
func newGlobalFlagSet(name string) (*flag.FlagSet, *globalFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g := &globalFlags{}
	g.register(fs)
	return fs, g
}

// Invocation is what a running command gets: the environment, the
// printer for its output and, on demand, a signed-in Session.
type Invocation struct {
	env     Env
	out     *printer
	global  *globalFlags
	session *Session
}

// Handles reports whether args (os.Args[1:]) are a CLI command line:
// the first argument is a word rather than a flag, one of the global
// flags, or a "--" with arguments after it (they are all words then,
// and the TUI takes none). Everything else stays with main(): its own
// flags, and no arguments at all, which starts the TUI.
func Handles(args []string) bool {
	switch {
	case len(args) == 0:
		return false
	case args[0] == "--":
		return len(args) > 1
	}
	return !strings.HasPrefix(args[0], "-") || isGlobalFlag(args[0])
}

// isGlobalFlag reports whether arg is one of the global flags, in any
// of the flag package's spellings (-json, --json, --workspace=acme).
func isGlobalFlag(arg string) bool {
	if !strings.HasPrefix(arg, "-") {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	fs, _ := newGlobalFlagSet("")
	return name != "" && fs.Lookup(name) != nil
}

// Main runs one command line (os.Args[1:]) and returns the process
// exit code: 0 success, 1 failure, 2 usage, 3 authentication, 4 not
// found (errors.go).
func Main(ctx context.Context, args []string, env Env) int {
	// Only the global flags are known until the command is found; run
	// decides again with the command's own flags.
	globals, _ := newGlobalFlagSet("slk")
	out := &printer{json: jsonRequested(globals, args), stdout: env.Stdout, stderr: env.Stderr}
	lead, words, err := splitLeadingFlags(args)
	if errors.Is(err, flag.ErrHelp) {
		return out.help(Overview())
	}
	if err != nil {
		return out.fail(err)
	}
	if len(words) == 0 {
		return out.fail(usageError("missing command; run 'slk help' for the commands"))
	}
	// Every route gets the leading flags back in front of the arguments
	// after its command words. lead can end in the "--" that ended the
	// flags, and only in front of the rest does it keep doing that.
	if words[0] == "help" {
		return help(out, slices.Concat(lead, words[1:]))
	}
	cmd := lookup(words)
	if cmd == nil {
		return unknownCommand(out, words[0], slices.Concat(lead, words[1:]))
	}
	return run(ctx, env, out, cmd, slices.Concat(lead, words[len(cmd.Path):]))
}

// splitLeadingFlags splits off the global flags that come before the
// command words (slk --workspace acme workspace whoami). They are
// parsed again with the command's own flags, so a flag means the same
// before the words as after them. words starts at the first argument
// that is not a flag. A "--" that ends the flags is the last token of
// lead, so the caller can keep its effect on the words after it.
func splitLeadingFlags(args []string) (lead, words []string, err error) {
	fs, _ := newGlobalFlagSet("slk")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil, err
		}
		return nil, nil, usageError("%v; run 'slk help' for the commands", err)
	}
	words = fs.Args()
	return args[:len(args)-len(words)], words, nil
}

// run parses a command's flags and runs it.
func run(ctx context.Context, env Env, out *printer, cmd *Command, args []string) int {
	fs, g, runCmd := newFlagSet(cmd)
	// The command's own flags are known now. One of them can take a
	// value that looks like --json, so the error format is decided again.
	out.json = jsonRequested(fs, args)
	path := strings.Join(cmd.Path, " ")
	// parseInterspersed reads the arguments as jsonRequested just did,
	// so the format chosen above matches what the parse sees.
	positionals, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return out.help(commandHelp(cmd, fs))
		}
		return out.fail(usageError("%v; run 'slk help %s'", err, path))
	}
	if len(positionals) > 0 {
		return out.fail(usageError("unexpected argument %q; run 'slk help %s'", positionals[0], path))
	}
	out.json = g.json
	if err := runCmd(ctx, &Invocation{env: env, out: out, global: g}); err != nil {
		return out.fail(err)
	}
	return exitOK
}

// newFlagSet builds cmd's flag set: the global flags, then the
// command's own.
func newFlagSet(cmd *Command) (*flag.FlagSet, *globalFlags, runFunc) {
	fs, g := newGlobalFlagSet("slk " + strings.Join(cmd.Path, " "))
	return fs, g, cmd.Setup(fs)
}

// help runs `slk help [noun [verb]]`. args are the arguments after the
// help word, with the leading flags (and a "--" that ended them) in
// front. They are parsed the way a command's are: the global flags may
// come before, between or after the topic words, a flag's value is not
// a topic word, an unknown flag is a usage error, and after "--" every
// argument is a topic word. The global flags change nothing in the help.
func help(out *printer, args []string) int {
	fs, _ := newGlobalFlagSet("slk help")
	// slk help already asks for help, so -h and --help are accepted
	// anywhere and change nothing. Declared, they are ordinary flags:
	// as the value of --workspace they are that value, like any word.
	fs.Bool("h", false, "")
	fs.Bool("help", false, "")
	topic, err := parseInterspersed(fs, args)
	if err != nil {
		return out.fail(usageError("%v; run 'slk help' for the commands", err))
	}
	text, err := helpText(topic)
	if err != nil {
		return out.fail(err)
	}
	return out.help(text)
}

// unknownCommand handles a first word, noun, that starts no command: a
// noun with a help flag gets the noun's help; anything else is a usage
// error. args are the arguments after noun, with the leading flags (and
// a "--" that ended them) in front. It never falls through to the TUI:
// an agent that mistypes a command must not end up in a full-screen
// program that waits forever.
func unknownCommand(out *printer, noun string, args []string) int {
	if _, ok := nouns[noun]; !ok {
		return out.fail(usageError("unknown command %q; run 'slk help' for the commands", noun))
	}
	// What follows the noun is parsed like a command's arguments: a
	// help flag asks for help only where it is a flag (not a flag's
	// value, and not after "--"), and an unknown flag before it is a
	// usage error.
	fs, _ := newGlobalFlagSet("slk " + noun)
	rest, err := parseInterspersed(fs, args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return out.help(nounHelp(noun))
	case err != nil:
		return out.fail(usageError("%v; run 'slk help %s'", err, noun))
	case len(rest) == 0:
		return out.fail(usageError("missing command after 'slk %s'; run 'slk help %s'", noun, noun))
	case lookup([]string{noun, rest[0]}) != nil:
		// The verb exists, but a flag came between it and the noun.
		return out.fail(usageError("flags go before or after the command words, not between them: slk %s %s [flags]", noun, rest[0]))
	}
	return out.fail(usageError("unknown command \"slk %s %s\"; run 'slk help %s'", noun, rest[0], noun))
}

// jsonRequested reports whether args set --json. It runs before the
// flags are parsed, so that an error found while parsing comes out in
// the format the caller asked for, and it reads args as the parse does
// (splitArgs):
//
//   - A flag that takes a value consumes the next argument: in
//     `--workspace --json=false` the workspace is "--json=false", and
//     --json is not touched.
//   - "--" ends the flags: a --json after it is a positional.
//   - It reads on past a flag fs does not declare, where fs.Parse stops
//     with an error, so `--nope --json` reports --nope as JSON.
//   - The last valid setting wins. An invalid one (--json=maybe) changes
//     nothing, so the error it causes comes out in the format asked for
//     before it.
func jsonRequested(fs *flag.FlagSet, args []string) bool {
	flags, _ := splitArgs(fs, args)
	on := false
	for _, f := range flags {
		if f.name != jsonFlag {
			continue
		}
		if !f.hasValue {
			on = true
		} else if b, err := strconv.ParseBool(f.value); err == nil {
			on = b
		}
	}
	return on
}
