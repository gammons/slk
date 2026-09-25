// Package editor runs the user's external editor for the compose box's
// Ctrl+E. cmd/slk wires it into the TUI as its core.EditorService.
package editor

import (
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

// WriteDraft saves text to a new temp file and returns its path.
func WriteDraft(text string) (string, error) {
	f, err := os.CreateTemp("", "slk-compose-*.md")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// Edit returns a Cmd that suspends the TUI while argv edits path.
func Edit(argv []string, path string, done func(err error) core.Msg) core.Cmd {
	c := tea.ExecProcess(command(argv, path), func(err error) tea.Msg { return done(err) })
	return func() core.Msg { return c() }
}

// TakeDraft reads the edited draft back and removes the file, even when
// the read fails.
func TakeDraft(path string) (string, error) {
	defer os.Remove(path)
	content, err := os.ReadFile(path)
	return string(content), err
}

func command(parts []string, path string) *exec.Cmd {
	args := append(append([]string{}, parts[1:]...), path)
	cmd := exec.Command(parts[0], args...)
	// tea.ExecProcess only fills in Stdin/Stdout/Stderr when unset, and
	// otherwise falls back to the Program's own output (a non-*os.File
	// io.Writer wrapping sixel frame correlation) — which forces Go's
	// exec package to pipe the child through a copy goroutine instead
	// of a real fd. The editor's own terminal-capability negotiation
	// (and mouse parsing) breaks without a genuine tty here, same as it
	// would for any program handed a pipe instead of its real stdout.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
