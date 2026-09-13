// internal/ui/editor.go
//
// Ctrl+E edits the compose box in an external editor ($VISUAL /
// $EDITOR / compose.editor), resolved once at startup by
// ResolveEditor (see cmd/slk/main.go) and stored on App — never
// re-read per keypress.
package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/debuglog"
)

// getenv returns os.Getenv(key), or def if unset/empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ResolveEditor picks $VISUAL, then $EDITOR, then configEditor. No
// fallback beyond that (e.g. "vi") — it isn't installed by default on
// Windows.
func ResolveEditor(configEditor string) (parts []string, ok bool) {
	parts = strings.Fields(getenv("VISUAL", getenv("EDITOR", configEditor)))
	return parts, len(parts) > 0
}

func editorCommand(parts []string, path string) *exec.Cmd {
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

// EditorFinishedMsg reports an external-editor session ending. Path is
// read back and removed regardless of Err — the file's contents are
// trusted over the editor's exit code.
type EditorFinishedMsg struct {
	Panel Panel
	Path  string
	Err   error
}

func (a *App) openComposeInEditor() tea.Cmd {
	if len(a.composeEditor) == 0 {
		return toastWithClear(a,
			"No editor configured — set $VISUAL, $EDITOR, or compose.editor in config.toml",
			4*time.Second)
	}

	panel := PanelMessages
	target := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		panel = PanelThread
		target = &a.threadCompose
	}

	f, err := os.CreateTemp("", "slk-compose-*.md")
	if err != nil {
		return toastWithClear(a, "Could not open editor: "+err.Error(), 3*time.Second)
	}
	path := f.Name()
	if _, err := f.WriteString(target.Value()); err != nil {
		f.Close()
		os.Remove(path)
		return toastWithClear(a, "Could not open editor: "+err.Error(), 3*time.Second)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return toastWithClear(a, "Could not open editor: "+err.Error(), 3*time.Second)
	}

	target.SetEditingExternally(true)
	target.SetPlaceholderOverride("Editing in $EDITOR — waiting for it to exit...")
	debuglog.General("editor: opening %v for %s", a.composeEditor, path)

	cmd := editorCommand(a.composeEditor, path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return EditorFinishedMsg{Panel: panel, Path: path, Err: err}
	})
}

func reduceEditorFinished(a *App, m EditorFinishedMsg) tea.Cmd {
	defer os.Remove(m.Path)

	target := &a.compose
	if m.Panel == PanelThread {
		target = &a.threadCompose
	}
	target.SetEditingExternally(false)
	target.SetPlaceholderOverride("")

	// A non-nil *exec.ExitError means the editor ran and exited
	// non-zero — trust the file over that (some editors exit non-zero
	// on things that still leave a saved draft). Any other error means
	// it never ran at all (e.g. not found), which the user should hear
	// about.
	var exitErr *exec.ExitError
	launchFailed := m.Err != nil && !errors.As(m.Err, &exitErr)
	debuglog.General("editor: finished path=%s err=%v launchFailed=%v", m.Path, m.Err, launchFailed)

	content, readErr := os.ReadFile(m.Path)
	if readErr != nil {
		return toastWithClear(a, "Editor: could not read draft back: "+readErr.Error(), 3*time.Second)
	}
	target.SetValue(strings.TrimRight(string(content), "\n"))

	if launchFailed {
		return toastWithClear(a, "Could not open editor: "+m.Err.Error(), 4*time.Second)
	}
	return nil
}
