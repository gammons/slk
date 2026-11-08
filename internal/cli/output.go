package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// printer writes what a command produces. Under --json, stdout gets
// exactly one JSON document and a failure goes to stderr as one JSON
// document too; otherwise both are text for people, whose format is not
// part of the contract.
type printer struct {
	json   bool
	stdout io.Writer
	stderr io.Writer
}

// result writes a command's result: doc as one indented JSON document
// and a newline under --json, or whatever text writes otherwise. doc is
// always an object with a named field ({"workspaces": [...]}), so a
// field can be added beside it later.
func (p *printer) result(doc any, text func(w io.Writer) error) error {
	if !p.json {
		return text(p.stdout)
	}
	return writeJSON(p.stdout, doc)
}

// help writes help text to stdout and returns the exit code. Help is
// for people, so it is text even under --json; a write that fails is
// still a failure (exit 1), not a silent success.
func (p *printer) help(text string) int {
	if _, err := io.WriteString(p.stdout, text); err != nil {
		return p.fail(err)
	}
	return exitOK
}

// fail reports err on stderr and returns the process exit code for it.
func (p *printer) fail(err error) int {
	e := errorFrom(err)
	if p.json {
		writeJSON(p.stderr, struct {
			Error *Error `json:"error"`
		}{e})
		return e.exit()
	}
	fmt.Fprintf(p.stderr, "Error: %s\n", e.Message)
	for _, c := range e.Candidates {
		fmt.Fprintf(p.stderr, "  %s  %s\n", c.ID, c.Name)
	}
	return e.exit()
}

// writeJSON writes v as one indented JSON document and a newline.
func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
