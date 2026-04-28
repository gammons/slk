package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveGlobalThemeNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveGlobalTheme(path, "dracula"); err != nil {
		t.Fatalf("saveGlobalTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `theme = "dracula"`) {
		t.Errorf("expected theme = \"dracula\", got:\n%s", data)
	}
}

func TestSaveGlobalThemeAddsAppearanceWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[general]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveGlobalTheme(path, "dracula"); err != nil {
		t.Fatalf("saveGlobalTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "[appearance]") || !strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("expected appended [appearance] section, got:\n%s", got)
	}
}

func TestSaveWorkspaceThemeNewSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveWorkspaceTheme(path, "T01ABCDEF", "ACME Corp", "dracula"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "# ACME Corp") {
		t.Errorf("expected '# ACME Corp' comment in:\n%s", got)
	}
	if !strings.Contains(got, "[workspaces.T01ABCDEF]") {
		t.Errorf("expected [workspaces.T01ABCDEF] section in:\n%s", got)
	}
	if !strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("expected theme = \"dracula\" in:\n%s", got)
	}
	// Global theme should be untouched.
	if !strings.Contains(got, `[appearance]`) {
		t.Errorf("global [appearance] section was lost:\n%s", got)
	}
}

func TestSaveWorkspaceThemeUpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[appearance]
theme = "dark"

# ACME Corp
[workspaces.T01ABCDEF]
theme = "dracula"
`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveWorkspaceTheme(path, "T01ABCDEF", "ACME Corp", "tokyo night"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, `theme = "tokyo night"`) {
		t.Errorf("expected updated theme, got:\n%s", got)
	}
	// The old "dracula" should be gone.
	if strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("old theme still present:\n%s", got)
	}
	// Comment should remain.
	if !strings.Contains(got, "# ACME Corp") {
		t.Errorf("comment was lost:\n%s", got)
	}
}

func TestSaveWorkspaceThemeMultipleWorkspaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveWorkspaceTheme(path, "T01", "ACME", "dracula"); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkspaceTheme(path, "T02", "Personal", "tokyo night"); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "[workspaces.T01]") || !strings.Contains(got, "[workspaces.T02]") {
		t.Errorf("expected both workspace sections, got:\n%s", got)
	}
	if !strings.Contains(got, `theme = "dracula"`) || !strings.Contains(got, `theme = "tokyo night"`) {
		t.Errorf("expected both themes, got:\n%s", got)
	}
}

func TestSaveGlobalThemeIgnoresWorkspaceSection(t *testing.T) {
	// If [workspaces.X] appears BEFORE [appearance], saveGlobalTheme must
	// not clobber the workspace's theme line.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `# ACME
[workspaces.T01]
theme = "dracula"

[appearance]
theme = "dark"
`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := saveGlobalTheme(path, "tokyo night"); err != nil {
		t.Fatalf("saveGlobalTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, `theme = "tokyo night"`) {
		t.Errorf("expected global theme updated, got:\n%s", got)
	}
	// Workspace theme must still be "dracula".
	if !strings.Contains(got, `theme = "dracula"`) {
		t.Errorf("workspace theme was clobbered, got:\n%s", got)
	}
}

func TestSaveThemeEscapesQuotes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Theme with embedded quote (pathological but should not corrupt TOML).
	if err := saveGlobalTheme(path, `bad"name`); err != nil {
		t.Fatalf("saveGlobalTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	// Embedded " must be escaped as \"
	if !strings.Contains(got, `theme = "bad\"name"`) {
		t.Errorf("expected escaped quote, got:\n%s", got)
	}
}

func TestSaveWorkspaceThemeSanitizesTeamName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[appearance]\ntheme = \"dark\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Team name with newline + control character.
	badName := "ACME\nCorp\x07"
	if err := saveWorkspaceTheme(path, "T01", badName, "dracula"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	// Comment must be a single line.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "#") {
			if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
				t.Errorf("comment line still contains newline: %q", line)
			}
		}
	}
	// The sanitized name should still be recognizable.
	if !strings.Contains(got, "ACME") {
		t.Errorf("expected sanitized comment to still contain ACME, got:\n%s", got)
	}
}
