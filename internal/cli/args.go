package cli

import (
	"flag"
	"strings"
)

// flagArg is one flag on a command line, as fs.Parse reads it.
type flagArg struct {
	name string // without its dashes and its =value
	// value is the =value, or the next argument for a flag that takes
	// one; hasValue says whether there is either.
	value    string
	hasValue bool
	tokens   []string // the flag's argument, and the next one when that is its value
}

// splitArgs separates args into flags and positionals the way fs.Parse
// reads them, except that it does not stop at the first positional, so
// flags may come before, between and after the positionals. It is the
// one reading of a command line that parsing (parseInterspersed) and
// the choice of error format (jsonRequested) share, so the two cannot
// disagree:
//
//   - A flag fs declares that takes a value consumes the next argument,
//     whatever it looks like: in `--workspace --help` the workspace is
//     "--help", and in `--workspace --` it is "--".
//   - A flag fs does not declare is taken to have no value. fs.Parse
//     stops at it with an error anyway.
//   - "--" where a flag could be ends the flags: every argument after it
//     is positional.
func splitArgs(fs *flag.FlagSet, args []string) (flags []flagArg, positionals []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return flags, append(positionals, args[i+1:]...)
		}
		if len(a) < 2 || a[0] != '-' {
			positionals = append(positionals, a) // a word, or "-"
			continue
		}
		// -name or --name, as the flag package accepts them. Bad syntax
		// (---name, -=x) yields a name fs does not declare, and fs.Parse
		// reports it.
		name, value, hasValue := strings.Cut(strings.TrimPrefix(a[1:], "-"), "=")
		fa := flagArg{name: name, value: value, hasValue: hasValue, tokens: []string{a}}
		if f := fs.Lookup(name); f != nil && !isBoolFlag(f) && !hasValue && i+1 < len(args) {
			i++
			fa.value, fa.hasValue = args[i], true
			fa.tokens = append(fa.tokens, args[i])
		}
		flags = append(flags, fa)
	}
	return flags, positionals
}

// isBoolFlag reports whether f takes no value, the test the flag
// package itself applies (its unexported boolFlag interface).
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// parseInterspersed parses args with fs and returns the positional
// arguments, in order. Flags may come before, between and after them,
// and "--" makes every later argument positional (splitArgs).
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	flags, positionals := splitArgs(fs, args)
	var tokens []string
	for _, f := range flags {
		tokens = append(tokens, f.tokens...)
	}
	// tokens holds only flags and their values, so fs.Parse reads all of
	// them or stops at the first error; it leaves no arguments over.
	if err := fs.Parse(tokens); err != nil {
		return nil, err
	}
	return positionals, nil
}
