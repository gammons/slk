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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
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

	path, err := a.editor.WriteDraft(target.Value())
	if err != nil {
		return toastWithClear(a, "Could not open editor: "+err.Error(), 3*time.Second)
	}

	target.SetEditingExternally(true)
	target.SetPlaceholderOverride("Editing in $EDITOR — waiting for it to exit...")
	debuglog.General("editor: opening %v for %s", a.composeEditor, path)

	return teaCmd(a.editor.Edit(a.composeEditor, path, func(err error) core.Msg {
		return EditorFinishedMsg{Panel: panel, Path: path, Err: err}
	}))
}

func reduceEditorFinished(a *App, m EditorFinishedMsg) tea.Cmd {
	target := &a.compose
	if m.Panel == PanelThread {
		target = &a.threadCompose
	}
	target.SetEditingExternally(false)
	target.SetPlaceholderOverride("")

	// An error with an exit code (*exec.ExitError) means the editor ran
	// and exited non-zero — trust the file over that (some editors exit
	// non-zero on things that still leave a saved draft). Any other error
	// means it never ran at all (e.g. not found), which the user should
	// hear about.
	var exitErr interface{ ExitCode() int }
	launchFailed := m.Err != nil && !errors.As(m.Err, &exitErr)
	debuglog.General("editor: finished path=%s err=%v launchFailed=%v", m.Path, m.Err, launchFailed)

	content, readErr := a.editor.TakeDraft(m.Path)
	if readErr != nil {
		return toastWithClear(a, "Editor: could not read draft back: "+readErr.Error(), 3*time.Second)
	}
	target.SetValue(strings.TrimRight(content, "\n"))

	if launchFailed {
		return toastWithClear(a, "Could not open editor: "+m.Err.Error(), 4*time.Second)
	}
	return nil
}
