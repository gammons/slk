package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Terminals quote a pasted or dropped file path so it is safe in a
// shell; each of these forms must resolve to the path on disk.
func TestResolveFilePath_UndoesTerminalQuoting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	cases := []struct {
		name, in, want string
	}{
		{"plain", "/a/My File.pdf", "/a/My File.pdf"},
		{"backslash-escaped spaces (Ghostty, iTerm2)", `/a/My\ File.pdf`, "/a/My File.pdf"},
		{"backslash-escaped with trailing space", `/a/My\ File.pdf `, "/a/My File.pdf"},
		{"escaped apostrophe and parens", `/a/Bob\'s\ \(1\).pdf`, "/a/Bob's (1).pdf"},
		{"single-quoted", "'/a/My File.pdf'", "/a/My File.pdf"},
		{"single-quoted with apostrophe", `'/a/Bob'\''s File.pdf'`, "/a/Bob's File.pdf"},
		{"double-quoted", `"/a/My File.pdf"`, "/a/My File.pdf"},
		{"file URL", "file:///a/My%20File.pdf", "/a/My File.pdf"},
		{"file URL on localhost", "file://localhost/a/x.pdf", "/a/x.pdf"},
		{"quoted home path", "'~/My File.pdf'", ""}, // expanded below
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cases[len(cases)-1].want = filepath.Join(home, "My File.pdf")

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := resolveFilePath(c.in)
			if !ok || got != c.want {
				t.Errorf("resolveFilePath(%q) = %q, %v; want %q, true", c.in, got, ok, c.want)
			}
		})
	}
}

func TestResolveFilePath_RejectsTextThatIsNotAPath(t *testing.T) {
	for _, in := range []string{
		"'just some quoted text'",
		`"hello world"`,
		`hello\ world`,
		"file://server/share/x.pdf",
		"'",
	} {
		if got, ok := resolveFilePath(in); ok {
			t.Errorf("resolveFilePath(%q) = %q, true; want not a path", in, got)
		}
	}
}

// End to end: a bracketed paste of a quoted or escaped path attaches
// the file instead of inserting the quoted text.
func TestPasteMsg_QuotedOrEscapedPath_AttachesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "Q2 report - ENG.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"single-quoted": "'" + path + "'",
		"escaped":       strings.ReplaceAll(path, " ", `\ `),
	} {
		t.Run(name, func(t *testing.T) {
			app := NewApp()
			app.SetClipboardAvailable(true)
			app.activeChannelID = "C1"
			app.focusedPanel = PanelMessages
			app.SetMode(ModeInsert)
			app.setClipboardReaderForTest(fakeClipboard(nil, nil))
			app.setFilesystemForTest()

			app.Update(tea.PasteMsg{Content: payload})

			atts := app.compose.Attachments()
			if len(atts) != 1 {
				t.Fatalf("expected 1 attachment, got %d (compose=%q)", len(atts), app.compose.Value())
			}
			if atts[0].Path != path || atts[0].Filename != "Q2 report - ENG.pdf" {
				t.Errorf("got Path=%q Filename=%q; want %q", atts[0].Path, atts[0].Filename, path)
			}
			if app.compose.Value() != "" {
				t.Errorf("quoted path leaked into compose: %q", app.compose.Value())
			}
		})
	}
}
