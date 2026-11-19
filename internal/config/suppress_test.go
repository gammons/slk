package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSuppressRules(t *testing.T) {
	cfg := loadConfigString(t, `
[workspaces.acme]
team_id = "T01ACME"

[[workspaces.acme.suppress]]
author = "B01DEPLOY"
note = "DeployBot"

[[workspaces.acme.suppress]]
author = "U02ALICE"
channel = "C03RANDOM"

[[workspaces.T09LEGACY.suppress]]
author = "U05BOB"
`)
	acme, ok := cfg.WorkspaceByTeamID("T01ACME")
	if !ok {
		t.Fatal("acme workspace missing")
	}
	s := acme.SuppressRules()
	if s.Len() != 2 || !s.Hides("C1", "B01DEPLOY") || !s.Hides("C03RANDOM", "U02ALICE") || s.Hides("C1", "U02ALICE") {
		t.Errorf("acme rules = %+v", s.Rules())
	}
	if s.Rules()[0].Note != "DeployBot" {
		t.Errorf("note = %q", s.Rules()[0].Note)
	}
	legacy, ok := cfg.WorkspaceByTeamID("T09LEGACY")
	if !ok || !legacy.SuppressRules().Hides("C1", "U05BOB") {
		t.Errorf("legacy-keyed workspace rules not loaded: %+v", legacy)
	}
}

func TestLoadSuppressRuleWithoutAuthorFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "[[workspaces.T01ACME.suppress]]\nchannel = \"C1\"\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "no author") {
		t.Fatalf("err = %v, want a missing-author error", err)
	}
}
