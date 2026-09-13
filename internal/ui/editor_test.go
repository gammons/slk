package ui

import (
	"os"
	"os/exec"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestResolveEditor_PrefersVisualOverEditorOverConfig(t *testing.T) {
	t.Setenv("VISUAL", "myvisual --flag")
	t.Setenv("EDITOR", "myeditor")

	parts, ok := ResolveEditor("configeditor")
	if !ok {
		t.Fatal("want ok=true")
	}
	want := []string{"myvisual", "--flag"}
	if len(parts) != len(want) || parts[0] != want[0] || parts[1] != want[1] {
		t.Fatalf("want VISUAL to win with %v, got %v", want, parts)
	}

	cmd := editorCommand(parts, "/tmp/draft.md")
	wantArgs := []string{"myvisual", "--flag", "/tmp/draft.md"}
	for i, w := range wantArgs {
		if cmd.Args[i] != w {
			t.Fatalf("want args %v, got %v", wantArgs, cmd.Args)
		}
	}
}

func TestResolveEditor_EditorBeatsConfigWhenVisualUnset(t *testing.T) {
	t.Setenv("VISUAL", "")
	os.Unsetenv("VISUAL")
	t.Setenv("EDITOR", "nano")

	parts, ok := ResolveEditor("configeditor")
	if !ok || len(parts) != 1 || parts[0] != "nano" {
		t.Fatalf("want [nano] beating config, got %v ok=%v", parts, ok)
	}
}

func TestResolveEditor_FallsBackToConfigWhenNeitherEnvVarSet(t *testing.T) {
	os.Unsetenv("VISUAL")
	os.Unsetenv("EDITOR")

	parts, ok := ResolveEditor("myconfigeditor --wait")
	if !ok {
		t.Fatal("want ok=true from the config value")
	}
	want := []string{"myconfigeditor", "--wait"}
	if len(parts) != len(want) || parts[0] != want[0] || parts[1] != want[1] {
		t.Fatalf("want %v, got %v", want, parts)
	}
}

func TestResolveEditor_NoneConfiguredReturnsNotOk(t *testing.T) {
	os.Unsetenv("VISUAL")
	os.Unsetenv("EDITOR")

	if parts, ok := ResolveEditor(""); ok {
		t.Fatalf("want ok=false when nothing is configured, got %v", parts)
	}
}

func TestReduceEditorFinished_LoadsContentIntoChannelCompose(t *testing.T) {
	a := newTestAppWithMessages(t)
	f, err := os.CreateTemp(t.TempDir(), "slk-compose-*.md")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if _, err := f.WriteString("edited in vim\n\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	reduceEditorFinished(a, EditorFinishedMsg{Panel: PanelMessages, Path: path})

	if got := a.compose.Value(); got != "edited in vim" {
		t.Fatalf("want channel compose %q, got %q", "edited in vim", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("want temp file removed, stat err = %v", err)
	}
}

func TestReduceEditorFinished_RoutesToThreadCompose(t *testing.T) {
	a := newTestAppWithMessages(t)
	f, err := os.CreateTemp(t.TempDir(), "slk-compose-*.md")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if _, err := f.WriteString("thread reply text"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	reduceEditorFinished(a, EditorFinishedMsg{Panel: PanelThread, Path: path})

	if got := a.threadCompose.Value(); got != "thread reply text" {
		t.Fatalf("want thread compose %q, got %q", "thread reply text", got)
	}
	if a.compose.Value() != "" {
		t.Fatalf("editor result leaked into channel compose: %q", a.compose.Value())
	}
}

func TestReduceEditorFinished_MissingFileLeavesComposeUntouched(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.compose.SetValue("original draft")

	reduceEditorFinished(a, EditorFinishedMsg{Panel: PanelMessages, Path: "/nonexistent/slk-compose-does-not-exist.md"})

	if got := a.compose.Value(); got != "original draft" {
		t.Fatalf("want original draft preserved on read failure, got %q", got)
	}
}

func TestApp_CtrlEOpensEditorFromInsertMode(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.composeEditor = []string{"true"} // Cmd is never invoked, so this never actually runs
	_ = a.compose.Focus()
	a.compose.SetValue("draft before editor")

	cmd := a.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("want a non-nil Cmd from Ctrl+E in insert mode")
	}
	if !a.compose.EditingExternally() {
		t.Fatal("want compose locked while the editor is open")
	}
}

func TestComposeModel_UpdateIgnoresKeysWhileEditingExternally(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	_ = a.compose.Focus()
	a.compose.SetValue("draft")
	a.compose.SetEditingExternally(true)

	_, _ = a.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})

	if a.compose.Value() != "draft" {
		t.Fatalf("want compose locked, got %q", a.compose.Value())
	}
}

func TestReduceEditorFinished_ClearsLockAndPlaceholder(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.compose.SetEditingExternally(true)
	a.compose.SetPlaceholderOverride("Editing in $EDITOR — waiting for it to exit...")
	f, err := os.CreateTemp(t.TempDir(), "slk-compose-*.md")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	reduceEditorFinished(a, EditorFinishedMsg{Panel: PanelMessages, Path: path})

	if a.compose.EditingExternally() {
		t.Fatal("want compose unlocked after the editor exits")
	}
}

func TestReduceEditorFinished_LaunchFailureShowsToast(t *testing.T) {
	a := newTestAppWithMessages(t)
	f, err := os.CreateTemp(t.TempDir(), "slk-compose-*.md")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	launchErr := &exec.Error{Name: "no-such-editor", Err: exec.ErrNotFound}
	cmd := reduceEditorFinished(a, EditorFinishedMsg{Panel: PanelMessages, Path: path, Err: launchErr})
	if cmd == nil {
		t.Fatal("want a toast Cmd when the editor could not be launched")
	}
}

func TestReduceEditorFinished_NonZeroExitDoesNotToast(t *testing.T) {
	a := newTestAppWithMessages(t)
	f, err := os.CreateTemp(t.TempDir(), "slk-compose-*.md")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	exitErr := exec.Command("false").Run() // a real *exec.ExitError
	if exitErr == nil {
		t.Skip("\"false\" did not produce a non-zero exit on this system")
	}
	cmd := reduceEditorFinished(a, EditorFinishedMsg{Panel: PanelMessages, Path: path, Err: exitErr})
	if cmd != nil {
		t.Fatal("want no toast for a plain non-zero exit — trust the file over the exit code")
	}
}

func TestApp_CtrlEWithNoEditorConfiguredShowsToast(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.composeEditor = nil
	_ = a.compose.Focus()
	a.compose.SetValue("draft before editor")

	cmd := a.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("want a non-nil Cmd (the toast) even with no editor configured")
	}
	if a.compose.Value() != "draft before editor" {
		t.Fatalf("draft must be untouched when no editor is configured, got %q", a.compose.Value())
	}
}

// TestApp_UpdateRoutesCtrlEThroughFullDispatch exercises the real
// tea.Program entry point (Update, not handleKey directly) — the
// reducer chain in Update runs before handleKey and could swallow a
// KeyPressMsg before it ever reaches the Ctrl+E check.
func TestApp_UpdateRoutesCtrlEThroughFullDispatch(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.composeEditor = []string{"true"}
	_ = a.compose.Focus()

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("want a non-nil Cmd from a.Update for Ctrl+E in insert mode")
	}
	if !a.compose.EditingExternally() {
		t.Fatal("want compose locked — something upstream of handleKey may be swallowing the key")
	}
}

// TestApp_CtrlEWorksWithNumLockModifierBit guards the actual bug:
// some terminals (e.g. Ghostty) report a NumLock/CapsLock bit
// alongside Mod even when irrelevant to the binding, which broke a
// bare `mod == tea.ModCtrl` comparison.
func TestApp_CtrlEWorksWithNumLockModifierBit(t *testing.T) {
	a := newTestAppWithMessages(t)
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	a.composeEditor = []string{"true"}
	_ = a.compose.Focus()

	cmd := a.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl | tea.ModNumLock})
	if cmd == nil {
		t.Fatal("want Ctrl+E to still register with NumLock's Mod bit set")
	}
	if !a.compose.EditingExternally() {
		t.Fatal("want compose locked — the NumLock bit must not defeat the ctrl+e check")
	}
}

// TestEditorCommand_UsesRealStdio pins the actual fix for garbled
// input inside the external editor: Stdin/Stdout/Stderr must be the
// real *os.File descriptors, not left for tea.ExecProcess to fall back
// to the Program's own output (a non-*os.File io.Writer, which forces
// Go's exec package to pipe the child through a copy goroutine instead
// of a real tty — breaking the editor's own terminal-capability
// negotiation and mouse parsing).
func TestEditorCommand_UsesRealStdio(t *testing.T) {
	cmd := editorCommand([]string{"true"}, "/tmp/draft.md")
	if cmd.Stdin != os.Stdin {
		t.Error("want cmd.Stdin == os.Stdin")
	}
	if cmd.Stdout != os.Stdout {
		t.Error("want cmd.Stdout == os.Stdout")
	}
	if cmd.Stderr != os.Stderr {
		t.Error("want cmd.Stderr == os.Stderr")
	}
}
