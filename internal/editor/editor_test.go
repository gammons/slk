package editor

import (
	"os"
	"testing"
)

func TestCommand_AppendsPathToArgv(t *testing.T) {
	cmd := command([]string{"myvisual", "--flag"}, "/tmp/draft.md")
	wantArgs := []string{"myvisual", "--flag", "/tmp/draft.md"}
	for i, w := range wantArgs {
		if cmd.Args[i] != w {
			t.Fatalf("want args %v, got %v", wantArgs, cmd.Args)
		}
	}
}

// TestCommand_UsesRealStdio pins the actual fix for garbled
// input inside the external editor: Stdin/Stdout/Stderr must be the
// real *os.File descriptors, not left for tea.ExecProcess to fall back
// to the Program's own output (a non-*os.File io.Writer, which forces
// Go's exec package to pipe the child through a copy goroutine instead
// of a real tty — breaking the editor's own terminal-capability
// negotiation and mouse parsing).
func TestCommand_UsesRealStdio(t *testing.T) {
	cmd := command([]string{"true"}, "/tmp/draft.md")
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
