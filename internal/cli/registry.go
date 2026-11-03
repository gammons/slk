package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
)

// Command is one row of the command table. The table drives dispatch
// and every help text, and TestSkillListsEveryCommand checks SKILL.md
// against it, so a command added here cannot be missing from either.
type Command struct {
	// Path is the words that name the command: {"workspace", "list"}.
	// A one-word path ({"skill"}) is a command on its own; a longer
	// one belongs to the noun its first word names (see nouns).
	Path []string
	// Summary is the command's line in help.
	Summary string
	// Setup declares the command's own flags on fs, where the global
	// flags (--json, --workspace) already are, and returns the function
	// that runs the command. That function reads the parsed flag values
	// through the closure.
	Setup func(fs *flag.FlagSet) runFunc
}

// runFunc runs one command. A returned error becomes the exit code and
// the error output (errorFrom).
type runFunc func(ctx context.Context, inv *Invocation) error

// commands is the command table, in the order help lists it.
var commands = []Command{
	{
		Path:    []string{"skill"},
		Summary: "Print the agent skill: how an AI agent uses these commands",
		Setup:   setupSkill,
	},
	{
		Path:    []string{"workspace", "list"},
		Summary: "List the configured workspaces, in the order of the workspace rail",
		Setup:   setupWorkspaceList,
	},
	{
		Path:    []string{"workspace", "whoami"},
		Summary: "Sign in, and show the workspace and the user that commands act as",
		Setup:   setupWorkspaceWhoami,
	},
	{
		Path:    []string{"auth", "export"},
		Summary: "Export a saved login offline to a new plaintext file",
		Setup:   setupAuthExport,
	},
	{
		Path:    []string{"auth", "import"},
		Summary: "Validate and import a plaintext login file; restart running slk afterward",
		Setup:   setupAuthImport,
	},
}

// nouns gives every noun (the first word of a multi-word path) its line
// in `slk help`.
var nouns = map[string]string{
	"auth":      "Transfer a saved login between machines using a plaintext file",
	"workspace": "Configured workspaces and the signed-in user",
}

// lookup returns the command whose path is the longest prefix of
// words, or nil.
func lookup(words []string) *Command {
	var best *Command
	for i := range commands {
		c := &commands[i]
		if len(c.Path) <= len(words) && slices.Equal(c.Path, words[:len(c.Path)]) &&
			(best == nil || len(c.Path) > len(best.Path)) {
			best = c
		}
	}
	return best
}

// Overview is the command section of `slk --help`: one line per noun or
// stand-alone command. It is generated from the table, so it cannot
// leave one out.
func Overview() string {
	var b strings.Builder
	b.WriteString("Commands for scripts and AI agents (add --json for JSON output):\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	var seen []string
	for _, c := range commands {
		name := c.Path[0]
		if slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		if len(c.Path) == 1 {
			fmt.Fprintf(tw, "  slk %s\t%s\n", name, c.Summary)
			continue
		}
		fmt.Fprintf(tw, "  slk %s\t%s (%s)\n", name, nouns[name], strings.Join(verbsOf(name), ", "))
	}
	tw.Flush()
	b.WriteString("  Run 'slk help <command>' for details.\n")
	return b.String()
}

// verbsOf lists the second words of noun's commands, in table order.
func verbsOf(noun string) []string {
	var verbs []string
	for _, c := range commands {
		if len(c.Path) > 1 && c.Path[0] == noun {
			verbs = append(verbs, c.Path[1])
		}
	}
	return verbs
}

// helpText is the help for topic, the words after `slk help`: the
// overview, a noun's commands, or one command's flags. Every help text
// is built in memory and written once (printer.help), so a failed
// write is reported; fs.PrintDefaults would drop the error.
func helpText(topic []string) (string, error) {
	if len(topic) == 0 {
		return Overview(), nil
	}
	if cmd := lookup(topic); cmd != nil && len(cmd.Path) == len(topic) {
		fs, _, _ := newFlagSet(cmd)
		return commandHelp(cmd, fs), nil
	}
	if _, ok := nouns[topic[0]]; ok && len(topic) == 1 {
		return nounHelp(topic[0]), nil
	}
	return "", usageError("unknown command %q; run 'slk help' for the commands", strings.Join(topic, " "))
}

// nounHelp is `slk help <noun>`: the noun's commands.
func nounHelp(noun string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "slk %s: %s\n\nCommands:\n", noun, nouns[noun])
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		if len(c.Path) > 1 && c.Path[0] == noun {
			fmt.Fprintf(tw, "  slk %s\t%s\n", strings.Join(c.Path, " "), c.Summary)
		}
	}
	tw.Flush()
	fmt.Fprintf(&b, "\nRun 'slk help %s <verb>' for a command's flags.\n", noun)
	return b.String()
}

// commandHelp is `slk help <command>`: its summary, usage and flags.
// fs is the command's flag set (newFlagSet).
func commandHelp(c *Command, fs *flag.FlagSet) string {
	var b strings.Builder
	path := strings.Join(c.Path, " ")
	fmt.Fprintf(&b, "slk %s: %s\n\nUsage:\n  slk %s [flags]\n\nFlags:\n", path, c.Summary, path)
	fs.SetOutput(&b)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
	return b.String()
}
