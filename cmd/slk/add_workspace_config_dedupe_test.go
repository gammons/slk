package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Re-running --add-workspace with an already-configured workspace
// selected used to write a second [workspaces.<slug>-2] block for the
// same team_id. config.Load rejects that, so slk stopped starting until
// the config was hand-edited — a picker default turned into a broken
// install.
func TestAppendWorkspaceConfigBlock_SkipsTeamIDAlreadyConfigured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := appendWorkspaceConfigBlock(path, "pucek-com", "T011B427MLP", "pucek.com"); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendWorkspaceConfigBlock(path, "pucek-com-2", "T011B427MLP", "pucek.com"); err != nil {
		t.Fatalf("second append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	got := string(data)
	if n := strings.Count(got, "T011B427MLP"); n != 1 {
		t.Errorf("team id written %d times, want 1:\n%s", n, got)
	}
	if strings.Contains(got, "pucek-com-2") {
		t.Errorf("duplicate slug block was written:\n%s", got)
	}
}

func TestAppendWorkspaceConfigBlock_DifferentTeamsStillAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	appendWorkspaceConfigBlock(path, "one", "T111", "One")
	appendWorkspaceConfigBlock(path, "two", "T222", "Two")

	data, _ := os.ReadFile(path)
	got := string(data)
	for _, want := range []string{"T111", "T222", "[workspaces.one]", "[workspaces.two]"} {
		if !strings.Contains(got, want) {
			t.Errorf("config missing %q:\n%s", want, got)
		}
	}
}

func TestConfiguredTeamIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if got := configuredTeamIDs(path); len(got) != 0 {
		t.Errorf("absent config reported %d team ids, want 0", len(got))
	}

	appendWorkspaceConfigBlock(path, "one", "T111", "One")
	appendWorkspaceConfigBlock(path, "two", "T222", "Two")

	got := configuredTeamIDs(path)
	if !got["T111"] || !got["T222"] || len(got) != 2 {
		t.Errorf("configuredTeamIDs = %v, want exactly T111 and T222", got)
	}
}

// Writing to a config that already carries unrelated settings must not
// disturb them — the writer appends text rather than re-marshalling.
func TestAppendWorkspaceConfigBlock_PreservesUnrelatedSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[appearance]\nimage_protocol = \"halfblock\"\n"), 0644)

	if err := appendWorkspaceConfigBlock(path, "one", "T111", "One"); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `image_protocol = "halfblock"`) {
		t.Errorf("unrelated setting lost:\n%s", data)
	}
}

// The guard must work on a config that is ALREADY broken by a
// duplicate — that is the state a second --add-workspace run leaves
// behind, and the state in which a third run would otherwise make
// things worse. Going through config.Load would return an error here
// and report "no team ids configured".
func TestConfiguredTeamIDs_WorksOnAConfigThatFailsValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte(`
[workspaces.pucek-com]
team_id = "T011B427MLP"

[workspaces.pucek-com-2]
team_id = "T011B427MLP"
`), 0644)

	got := configuredTeamIDs(path)
	if !got["T011B427MLP"] {
		t.Fatalf("team id not found in a duplicate-broken config: %v", got)
	}

	// And appending must therefore be a no-op rather than a third block.
	if err := appendWorkspaceConfigBlock(path, "pucek-com-3", "T011B427MLP", "pucek.com"); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "pucek-com-3") {
		t.Errorf("a third duplicate block was appended:\n%s", data)
	}
}

// A hand-written config using single-quoted TOML strings must be read
// too — slk writes double quotes, but the file is a user's to edit.
func TestConfiguredTeamIDs_ReadsSingleQuotedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("[workspaces.x]\nteam_id = 'T011B427MLP'\n"), 0644)

	if !configuredTeamIDs(path)["T011B427MLP"] {
		t.Error("single-quoted team_id not recognised")
	}
}
