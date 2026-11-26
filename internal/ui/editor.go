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
	"github.com/gammons/slk/internal/ui/messages"
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

// ComposeEditorFinishedMsg reports an external-editor session ending. Path is
// read back and removed regardless of Err — the file's contents are
// trusted over the editor's exit code.
type ComposeEditorFinishedMsg struct {
	Panel Panel
	Path  string
	Err   error
}

// MessageEditorFinishedMsg reports an external-editor session ending. Path is
// read back and removed regardless of Err — the file's contents are
// trusted over the editor's exit code.
type MessageEditorFinishedMsg struct {
	OriginalText string
	Path         string
	Err          error
}

// Opens text in editor, calling a callback when the editor has finished.
//
// It also calls the callback with empty arguments, and discards the result,
// whenever a failure has occured. This allows the caller to clean up.
func (a *App) openInEditor(text string, callback func(path string, err error) core.Msg) tea.Cmd {
	if len(a.composeEditor) == 0 {
		err := errors.New("no editor configured")
		callback("", err)
		return toastWithClear(a,
			"No editor configured — set $VISUAL, $EDITOR, or compose.editor in config.toml",
			4*time.Second)
	}

	path, err := a.editor.WriteDraft(text)

	if err != nil {
		callback("", err)
		return toastWithClear(a, "Could not write draft file: "+err.Error(), 3*time.Second)
	}

	debuglog.General("editor: opening %v for %s", a.composeEditor, path)

	return teaCmd(a.editor.Edit(a.composeEditor, path, func(err error) core.Msg {
		return callback(path, err)
	}))
}

func (a *App) openComposeInEditor() tea.Cmd {
	target := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		target = &a.threadCompose
	}

	panel := PanelMessages
	if a.focusedPanel == PanelThread && a.threadVisible {
		panel = PanelThread
	}
	target.SetEditingExternally(true)
	target.SetPlaceholderOverride("Editing in $EDITOR — waiting for it to exit...")

	return a.openInEditor(
		target.Value(),
		func(path string, err error) core.Msg {
			target.SetEditingExternally(false)
			target.SetPlaceholderOverride("")

			return ComposeEditorFinishedMsg{Panel: panel, Path: path, Err: err}
		},
	)
}

func (a *App) openMessageInEditor() tea.Cmd {
	target, _, hasMessage := a.selectedMessageInFocus()
	if hasMessage {
		text := messages.MessageCopyText(target)
		return a.openInEditor(text,
			func(path string, err error) core.Msg {
				return MessageEditorFinishedMsg{OriginalText: text, Path: path, Err: err}
			})
	} else {
		return toastWithClear(a,
			"No message selected for viewing in editor",
			4*time.Second)
	}
}

// An error with an exit code (*exec.ExitError) means the editor ran
// and exited non-zero — trust the file over that (some editors exit
// non-zero on things that still leave a saved draft). Any other error
// means it never ran at all (e.g. not found), which the user should
// hear about.
func didLaunchFail(e error) bool {
	var exitErr interface{ ExitCode() int }
	return e != nil && !errors.As(e, &exitErr)
}

func reduceComposeEditorFinished(a *App, m ComposeEditorFinishedMsg) tea.Cmd {
	target := &a.compose
	if m.Panel == PanelThread {
		target = &a.threadCompose
	}
	launchFailed := didLaunchFail(m.Err)
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

func reduceMessageEditorFinished(a *App, m MessageEditorFinishedMsg) tea.Cmd {
	content, readErr := a.editor.TakeDraft(m.Path)
	if readErr == nil && content != m.OriginalText {
		return toastWithClear(a, "Message not edited — you need to be in Insert mode (press "+a.keys.Edit.Help().Key+" first)", 4*time.Second)
	}
	if didLaunchFail(m.Err) {
		return toastWithClear(a, "Could not open editor: "+m.Err.Error(), 4*time.Second)
	}
	return nil
}
