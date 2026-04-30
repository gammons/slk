package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
)

func TestUniqueSlug(t *testing.T) {
	existing := map[string]bool{"acme": true, "acme-2": true}
	if got := uniqueSlug("acme", existing); got != "acme-3" {
		t.Errorf("uniqueSlug = %q, want acme-3", got)
	}
	if got := uniqueSlug("fresh", existing); got != "fresh" {
		t.Errorf("uniqueSlug = %q, want fresh", got)
	}
}

func TestUniqueSlugEmptyInputUsesFallback(t *testing.T) {
	existing := map[string]bool{}
	if got := uniqueSlug("", existing); got != "workspace" {
		t.Errorf("uniqueSlug(\"\") = %q, want workspace", got)
	}
}

func TestAppendWorkspaceConfigBlockNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := appendWorkspaceConfigBlock(path, "work", "T01ABCDEF", "ACME Corp"); err != nil {
		t.Fatalf("append: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ws, ok := cfg.Workspaces["work"]
	if !ok || ws.TeamID != "T01ABCDEF" {
		t.Errorf("workspace not loadable: %+v %v", ws, ok)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# ACME Corp") {
		t.Errorf("expected '# ACME Corp' comment, got:\n%s", got)
	}
}

func TestAppendWorkspaceConfigBlockAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[appearance]
theme = "dracula"
`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	if err := appendWorkspaceConfigBlock(path, "work", "T01ABCDEF", "ACME"); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "[appearance]") {
		t.Errorf("existing config clobbered, got:\n%s", s)
	}
	if !strings.Contains(s, "[workspaces.work]") || !strings.Contains(s, `team_id = "T01ABCDEF"`) {
		t.Errorf("workspace block not appended, got:\n%s", s)
	}
}
