package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Terminals quote a pasted or dropped file path so it is safe in a
// shell; each of these forms must offer the path on disk.
func TestFilePathCandidates_UndoesTerminalQuoting(t *testing.T) {
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
			got := filePathCandidates(c.in)
			if len(got) == 0 || got[len(got)-1] != c.want {
				t.Errorf("filePathCandidates(%q) = %q; want it to end with %q", c.in, got, c.want)
			}
		})
	}
}

// The text as pasted is always tried first: a file whose name really
// contains a backslash must not be rewritten into another file's.
func TestFilePathCandidates_LiteralFirst(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	if got := filePathCandidates(`/tmp/a\b.pdf`); !slices.Equal(got, []string{`/tmp/a\b.pdf`, "/tmp/ab.pdf"}) {
		t.Errorf("escaped path: got %q", got)
	}
	if got := filePathCandidates("/tmp/plain.pdf"); !slices.Equal(got, []string{"/tmp/plain.pdf"}) {
		t.Errorf("plain path is offered once: got %q", got)
	}
}

func TestFilePathCandidates_RejectsTextThatIsNotAPath(t *testing.T) {
	for _, in := range []string{
		"'just some quoted text'",
		`"hello world"`,
		`hello\ world`,
		"file://server/share/x.pdf",
		"'",
		"/a\n/b",
	} {
		if got := filePathCandidates(in); len(got) != 0 {
			t.Errorf("filePathCandidates(%q) = %q; want not a path", in, got)
		}
	}
}

// On Windows a backslash separates directories: quotes come off, the
// backslashes stay.
func TestUnshellPathSep_Windows(t *testing.T) {
	for in, want := range map[string]string{
		`"C:\Users\me\report.pdf"`:   `C:\Users\me\report.pdf`,
		`C:\Users\me\My File.pdf`:    `C:\Users\me\My File.pdf`,
		`'C:\Users\me\My File.pdf'`:  `C:\Users\me\My File.pdf`,
		"file:///C:/Users/me/My%20F": "/C:/Users/me/My F",
	} {
		if got := unshellPathSep(in, '\\'); got != want {
			t.Errorf("unshellPathSep(%q, '\\\\') = %q; want %q", in, got, want)
		}
	}
	if got := unshellPathSep(`"/a/My\ File.pdf"`, '/'); got != "/a/My File.pdf" {
		t.Errorf("unix double-quoted: got %q", got)
	}
}

func TestUnescapeBackslashes(t *testing.T) {
	for in, want := range map[string]string{
		`My\ File`: "My File",
		`a\\b`:     `a\b`,
		`trail\`:   `trail\`,
		`plain`:    "plain",
	} {
		if got := unescapeBackslashes(in); got != want {
			t.Errorf("unescapeBackslashes(%q) = %q; want %q", in, got, want)
		}
	}
}

// pasteIntoCompose pastes payload as a bracketed paste into the channel
// composer and returns the app.
func pasteIntoCompose(t *testing.T, payload string) *App {
	t.Helper()
	app := newTestApp(t, withActiveChannel("C1"), withMode(ModeInsert))
	app.focusedPanel = PanelMessages
	app.SetClipboardAvailable(true)
	app.setClipboardReaderForTest(fakeClipboard(nil, nil))
	app.setFilesystemForTest()
	app.Update(tea.PasteMsg{Content: payload})
	return app
}

func assertAttached(t *testing.T, app *App, path string) {
	t.Helper()
	atts := app.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d (compose=%q)", len(atts), app.compose.Value())
	}
	if atts[0].Path != path || atts[0].Filename != filepath.Base(path) {
		t.Errorf("got Path=%q Filename=%q; want %q", atts[0].Path, atts[0].Filename, path)
	}
	if app.compose.Value() != "" {
		t.Errorf("pasted path leaked into compose: %q", app.compose.Value())
	}
}

// End to end: a bracketed paste of a quoted or escaped path attaches
// the file instead of inserting the quoted text.
func TestPasteMsg_QuotedOrEscapedPath_AttachesFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	path := filepath.Join(t.TempDir(), "Q2 report - ENG.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"single-quoted": "'" + path + "'",
		"escaped":       strings.ReplaceAll(path, " ", `\ `),
	} {
		t.Run(name, func(t *testing.T) {
			assertAttached(t, pasteIntoCompose(t, payload), path)
		})
	}
}

// A file whose name really contains a backslash attaches as itself,
// even when the unescaped name exists too.
func TestPasteMsg_LiteralBackslashPath_AttachesThatFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("backslash is a path separator on Windows")
	}
	dir := t.TempDir()
	literal, unescaped := filepath.Join(dir, `a\b.pdf`), filepath.Join(dir, "ab.pdf")
	for _, p := range []string{literal, unescaped} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	assertAttached(t, pasteIntoCompose(t, literal), literal)

	// Without the literal file, the escaped reading still attaches.
	if err := os.Remove(literal); err != nil {
		t.Fatal(err)
	}
	assertAttached(t, pasteIntoCompose(t, literal), unescaped)
}
