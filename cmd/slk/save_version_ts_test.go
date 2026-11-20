package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/slackhttp"
)

func TestSaveWorkspaceVersionTS_UpdatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := "# My Team\n[workspaces.acme]\nteam_id = \"T04T4TH8W\"\nversion_ts = \"1111111111\"\ntheme = \"nord\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "My Team", "1785403654"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	if !strings.Contains(s, `version_ts = "1785403654"`) {
		t.Errorf("version_ts not updated:\n%s", s)
	}
	if strings.Contains(s, "1111111111") {
		t.Errorf("old version_ts still present:\n%s", s)
	}
	// Comments and sibling fields must survive — this is why slk
	// rewrites textually instead of re-marshalling TOML.
	if !strings.Contains(s, "# My Team") {
		t.Errorf("comment was lost:\n%s", s)
	}
	if !strings.Contains(s, `team_id = "T04T4TH8W"`) || !strings.Contains(s, `theme = "nord"`) {
		t.Errorf("sibling fields clobbered:\n%s", s)
	}
}

func TestSaveWorkspaceVersionTS_AddsToExistingSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[workspaces.acme]\nteam_id = \"T04T4TH8W\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "My Team", "1785403654"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), `version_ts = "1785403654"`) {
		t.Errorf("version_ts not added:\n%s", out)
	}
	if !strings.Contains(string(out), `team_id = "T04T4TH8W"`) {
		t.Errorf("team_id lost:\n%s", out)
	}
}

func TestSaveWorkspaceVersionTS_DoesNotTouchOtherWorkspaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := "[workspaces.acme]\nteam_id = \"T111\"\nversion_ts = \"1111111111\"\n\n[workspaces.other]\nteam_id = \"T222\"\nversion_ts = \"2222222222\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := saveWorkspaceVersionTS(path, "acme", "T111", "Acme", "9999999999"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out := string(mustRead(t, path))
	if !strings.Contains(out, `version_ts = "9999999999"`) {
		t.Errorf("target not updated:\n%s", out)
	}
	if !strings.Contains(out, `version_ts = "2222222222"`) {
		t.Errorf("other workspace's version_ts was clobbered:\n%s", out)
	}
}

// First setup appends [workspaces.<slug>] while run() is still holding
// the config it loaded before onboarding. The background version_ts
// refresh then resolves the key from that snapshot, which falls back
// to the team id, and a second block is what makes the next launch
// refuse to start (#499). The refresh must land in the named block.
func TestFirstSetupVersionTSReloadsAsOneWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := appendWorkspaceConfigBlock(path, "my-company", "T01ABCDEF", "My Company"); err != nil {
		t.Fatalf("append workspace: %v", err)
	}
	// The snapshot connectWorkspace saves with: loaded before the
	// named block existed.
	stale := config.Config{}
	tomlKey := workspaceTOMLKey(stale, "T01ABCDEF")
	if tomlKey != "T01ABCDEF" {
		t.Fatalf("stale snapshot key = %q, want the team-id fallback", tomlKey)
	}
	if err := saveWorkspaceVersionTS(path, tomlKey, "T01ABCDEF", "My Company", "1795162663"); err != nil {
		t.Fatalf("save: %v", err)
	}

	body := string(mustRead(t, path))
	if n := strings.Count(body, "[workspaces."); n != 1 {
		t.Fatalf("%d workspace blocks, want 1:\n%s", n, body)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, body)
	}
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1:\n%s", len(cfg.Workspaces), body)
	}
	ws, ok := cfg.Workspaces["my-company"]
	if !ok {
		t.Fatalf("named workspace missing:\n%s", mustRead(t, path))
	}
	if ws.TeamID != "T01ABCDEF" || ws.VersionTS != "1795162663" {
		t.Fatalf("workspace = %+v, want team T01ABCDEF and version_ts 1795162663", ws)
	}
}

func TestSaveWorkspaceVersionTS_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.toml")
	if err := saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "My Team", "1785403654"); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(out), `version_ts = "1785403654"`) {
		t.Errorf("version_ts missing from new file:\n%s", out)
	}
}

// Workspaces connect in parallel, so the background version_ts refresh
// fires from several goroutines at once against one config file. Each
// save is a read-modify-write; without serialization one workspace's
// value is silently lost (and a reader can observe a truncated file
// mid-write and write the truncation back).
func TestSaveWorkspaceVersionTS_ConcurrentSavesDoNotLoseUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := "[workspaces.acme]\nteam_id = \"T111\"\n\n[workspaces.other]\nteam_id = \"T222\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, w := range []struct{ key, teamID, ts string }{
		{"acme", "T111", "1111111111"},
		{"other", "T222", "2222222222"},
	} {
		wg.Add(1)
		go func(key, teamID, ts string) {
			defer wg.Done()
			if err := saveWorkspaceVersionTS(path, key, teamID, key, ts); err != nil {
				errs <- err
			}
		}(w.key, w.teamID, w.ts)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("save: %v", err)
	}

	out := string(mustRead(t, path))
	if !strings.Contains(out, `version_ts = "1111111111"`) {
		t.Errorf("acme's version_ts was lost:\n%s", out)
	}
	if !strings.Contains(out, `version_ts = "2222222222"`) {
		t.Errorf("other's version_ts was lost:\n%s", out)
	}
}

func TestSeedVersionTS_UsesCachedValue(t *testing.T) {
	env := slackhttp.NewEnvelope()
	cfg := config.Config{Workspaces: map[string]config.Workspace{
		"acme": {TeamID: "T111", VersionTS: "1799999999"},
	}}
	seedVersionTS(env, cfg, "T111")
	if got := env.VersionTS(); got != "1799999999" {
		t.Errorf("VersionTS = %q; want the cached 1799999999", got)
	}
}

func TestSeedVersionTS_UnknownWorkspaceKeepsFallback(t *testing.T) {
	env := slackhttp.NewEnvelope()
	cfg := config.Config{Workspaces: map[string]config.Workspace{
		"acme": {TeamID: "T111", VersionTS: "1799999999"},
	}}
	seedVersionTS(env, cfg, "T999")
	if got := env.VersionTS(); got != slackhttp.DefaultVersionTS {
		t.Errorf("VersionTS = %q; want the compiled-in fallback %q", got, slackhttp.DefaultVersionTS)
	}
}

func TestSeedVersionTS_EmptyCachedValueKeepsFallback(t *testing.T) {
	env := slackhttp.NewEnvelope()
	cfg := config.Config{Workspaces: map[string]config.Workspace{
		"acme": {TeamID: "T111"},
	}}
	seedVersionTS(env, cfg, "T111")
	if got := env.VersionTS(); got != slackhttp.DefaultVersionTS {
		t.Errorf("VersionTS = %q; want the compiled-in fallback %q", got, slackhttp.DefaultVersionTS)
	}
}

func TestSeedVersionTS_NilEnvelopeIsNoOp(t *testing.T) {
	cfg := config.Config{Workspaces: map[string]config.Workspace{
		"acme": {TeamID: "T111", VersionTS: "1799999999"},
	}}
	seedVersionTS(nil, cfg, "T111") // must not panic
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return b
}
