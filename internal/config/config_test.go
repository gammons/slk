package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Appearance.Theme != "nord" {
		t.Errorf("expected default theme 'nord', got %q", cfg.Appearance.Theme)
	}
	if cfg.Appearance.TimestampFormat != "3:04 PM" {
		t.Errorf("expected default timestamp format '3:04 PM', got %q", cfg.Appearance.TimestampFormat)
	}
	if !cfg.Animations.Enabled {
		t.Error("expected animations enabled by default")
	}
	if !cfg.Notifications.Enabled {
		t.Error("expected notifications enabled by default")
	}
	if !cfg.Notifications.OnMention {
		t.Error("expected on_mention enabled by default")
	}
	if !cfg.Notifications.OnDM {
		t.Error("expected on_dm enabled by default")
	}
	if cfg.Cache.MessageRetentionDays != 30 {
		t.Errorf("expected 30 day retention, got %d", cfg.Cache.MessageRetentionDays)
	}
	if cfg.Cache.MaxDBSizeMB != 500 {
		t.Errorf("expected 500 MB max, got %d", cfg.Cache.MaxDBSizeMB)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	err := os.WriteFile(configPath, []byte(`
[general]
default_workspace = "myteam"

[appearance]
theme = "light"

[animations]
enabled = false

[cache]
message_retention_days = 7
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.DefaultWorkspace != "myteam" {
		t.Errorf("expected workspace 'myteam', got %q", cfg.General.DefaultWorkspace)
	}
	if cfg.Appearance.Theme != "light" {
		t.Errorf("expected theme 'light', got %q", cfg.Appearance.Theme)
	}
	if cfg.Animations.Enabled {
		t.Error("expected animations disabled")
	}
	// Defaults should fill in unset values
	if cfg.Cache.MaxDBSizeMB != 500 {
		t.Errorf("expected default max_db_size_mb 500, got %d", cfg.Cache.MaxDBSizeMB)
	}
	if cfg.Cache.MessageRetentionDays != 7 {
		t.Errorf("expected 7 day retention, got %d", cfg.Cache.MessageRetentionDays)
	}
}

func TestThemeParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[theme]
primary = "#FF0000"
accent = "#00FF00"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Primary != "#FF0000" {
		t.Errorf("expected primary #FF0000, got %q", cfg.Theme.Primary)
	}
	if cfg.Theme.Accent != "#00FF00" {
		t.Errorf("expected accent #00FF00, got %q", cfg.Theme.Accent)
	}
	if cfg.Theme.Background != "" {
		t.Errorf("expected empty background, got %q", cfg.Theme.Background)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatal("expected no error for missing file, got:", err)
	}
	// Should return defaults
	if cfg.Appearance.Theme != "nord" {
		t.Errorf("expected default theme 'nord', got %q", cfg.Appearance.Theme)
	}
}

func TestConfig_ImageDefaults(t *testing.T) {
	// Loading a missing/empty config file should yield image-related defaults.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.ImageProtocol != "auto" {
		t.Errorf("expected default image_protocol 'auto', got %q", cfg.Appearance.ImageProtocol)
	}
	if cfg.Appearance.MaxImageRows != 20 {
		t.Errorf("expected default max_image_rows 20, got %d", cfg.Appearance.MaxImageRows)
	}
	if cfg.Cache.MaxImageCacheMB != 200 {
		t.Errorf("expected default max_image_cache_mb 200, got %d", cfg.Cache.MaxImageCacheMB)
	}

	// Default() directly should also yield these values.
	d := Default()
	if d.Appearance.ImageProtocol != "auto" {
		t.Errorf("Default() image_protocol = %q, want 'auto'", d.Appearance.ImageProtocol)
	}
	if d.Appearance.MaxImageRows != 20 {
		t.Errorf("Default() max_image_rows = %d, want 20", d.Appearance.MaxImageRows)
	}
	if d.Cache.MaxImageCacheMB != 200 {
		t.Errorf("Default() max_image_cache_mb = %d, want 200", d.Cache.MaxImageCacheMB)
	}
}

func TestConfig_ImageOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[appearance]
image_protocol = "halfblock"
max_image_rows = 10

[cache]
max_image_cache_mb = 50
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Appearance.ImageProtocol != "halfblock" {
		t.Errorf("expected image_protocol 'halfblock', got %q", cfg.Appearance.ImageProtocol)
	}
	if cfg.Appearance.MaxImageRows != 10 {
		t.Errorf("expected max_image_rows 10, got %d", cfg.Appearance.MaxImageRows)
	}
	if cfg.Cache.MaxImageCacheMB != 50 {
		t.Errorf("expected max_image_cache_mb 50, got %d", cfg.Cache.MaxImageCacheMB)
	}
}

func TestResolveThemeWorkspaceWins(t *testing.T) {
	c := Config{
		Appearance: Appearance{Theme: "dark"},
		Workspaces: map[string]Workspace{
			"T01": {TeamID: "T01", Theme: "dracula"},
		},
	}
	if got := c.ResolveTheme("T01"); got != "dracula" {
		t.Errorf("ResolveTheme(T01) = %q, want dracula", got)
	}
}

func TestResolveThemeWorkspaceMissing(t *testing.T) {
	c := Config{
		Appearance: Appearance{Theme: "tokyo night"},
		Workspaces: map[string]Workspace{
			"T01": {TeamID: "T01", Theme: "dracula"},
		},
	}
	if got := c.ResolveTheme("T99"); got != "tokyo night" {
		t.Errorf("ResolveTheme(T99) = %q, want tokyo night (global)", got)
	}
}

func TestResolveThemeWorkspaceEmpty(t *testing.T) {
	// Workspace exists in map but has empty Theme.
	c := Config{
		Appearance: Appearance{Theme: "tokyo night"},
		Workspaces: map[string]Workspace{
			"T01": {TeamID: "T01", Theme: ""},
		},
	}
	if got := c.ResolveTheme("T01"); got != "tokyo night" {
		t.Errorf("ResolveTheme empty ws theme = %q, want tokyo night", got)
	}
}

func TestResolveThemeNoGlobal(t *testing.T) {
	c := Config{
		Appearance: Appearance{Theme: ""},
		Workspaces: map[string]Workspace{},
	}
	if got := c.ResolveTheme("T01"); got != "nord" {
		t.Errorf("ResolveTheme no global = %q, want nord", got)
	}
}

func TestResolveThemeNilWorkspaces(t *testing.T) {
	// A config loaded from a file that has no [workspaces] section
	// will have a nil Workspaces map. ResolveTheme must not panic.
	c := Config{
		Appearance: Appearance{Theme: "nord"},
	}
	if got := c.ResolveTheme("T01"); got != "nord" {
		t.Errorf("ResolveTheme nil workspaces = %q, want nord", got)
	}
}

func TestLoadWorkspacesLegacyTeamIDKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[workspaces.T01ABCDEF]
theme = "dracula"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ws, ok := cfg.Workspaces["T01ABCDEF"]
	if !ok {
		t.Fatalf("expected workspace key T01ABCDEF, got %v", cfg.Workspaces)
	}
	if ws.TeamID != "T01ABCDEF" {
		t.Errorf("TeamID = %q, want T01ABCDEF (synthesized from key)", ws.TeamID)
	}
}

func TestLoadWorkspacesSlugKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[workspaces.work]
team_id = "T01ABCDEF"
theme = "dracula"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ws, ok := cfg.Workspaces["work"]
	if !ok {
		t.Fatalf("expected workspace key 'work', got %v", cfg.Workspaces)
	}
	if ws.TeamID != "T01ABCDEF" {
		t.Errorf("TeamID = %q, want T01ABCDEF", ws.TeamID)
	}
}

func TestLoadWorkspacesMissingTeamIDOnSlugKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[workspaces.work]
theme = "dracula"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for non-team-ID slug key with no team_id field")
	}
}

func TestLoadWorkspacesDuplicateTeamID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[workspaces.work]
team_id = "T01ABCDEF"

[workspaces.also-work]
team_id = "T01ABCDEF"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for duplicate team_id across slugs")
	}
}

func TestLoadWorkspacesMixedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[workspaces.work]
team_id = "T01ABCDEF"
theme = "dracula"

[workspaces.T02LEGACY]
theme = "tokyo night"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Workspaces["work"].TeamID != "T01ABCDEF" {
		t.Errorf("slug-keyed TeamID = %q", cfg.Workspaces["work"].TeamID)
	}
	if cfg.Workspaces["T02LEGACY"].TeamID != "T02LEGACY" {
		t.Errorf("legacy-keyed TeamID = %q", cfg.Workspaces["T02LEGACY"].TeamID)
	}
}

func TestLoadWorkspacesSlugKeyBadTeamID(t *testing.T) {
	// A slug-keyed block whose team_id field doesn't look like a
	// real Slack team ID should fail loudly rather than silently.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	data := []byte(`
[workspaces.work]
team_id = "not-a-real-id"
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for slug-keyed block with malformed team_id")
	}
}

func TestMatchSectionWorkspaceOverride(t *testing.T) {
	c := Config{
		Sections: map[string]SectionDef{
			"GlobalEng": {Channels: []string{"eng-*"}, Order: 1},
		},
		Workspaces: map[string]Workspace{
			"work": {
				TeamID: "T01",
				Sections: map[string]SectionDef{
					"WorkAlerts": {Channels: []string{"alerts"}, Order: 1},
				},
			},
		},
	}
	// In the "work" workspace, eng-foo should NOT match GlobalEng
	// because the per-workspace sections fully replace global.
	if got := c.MatchSection("T01", "eng-foo"); got != "" {
		t.Errorf(`MatchSection("T01", "eng-foo") = %q, want "" (override hides global)`, got)
	}
	// "alerts" matches the workspace's own section.
	if got := c.MatchSection("T01", "alerts"); got != "WorkAlerts" {
		t.Errorf(`MatchSection("T01", "alerts") = %q, want "WorkAlerts"`, got)
	}
}

func TestMatchSectionWorkspaceFallsBackToGlobal(t *testing.T) {
	c := Config{
		Sections: map[string]SectionDef{
			"GlobalEng": {Channels: []string{"eng-*"}, Order: 1},
		},
		Workspaces: map[string]Workspace{
			"side": {TeamID: "T02"}, // no per-workspace sections
		},
	}
	if got := c.MatchSection("T02", "eng-foo"); got != "GlobalEng" {
		t.Errorf("expected fallback to global, got %q", got)
	}
}

func TestMatchSectionUnknownTeamID(t *testing.T) {
	c := Config{
		Sections: map[string]SectionDef{
			"GlobalEng": {Channels: []string{"eng-*"}, Order: 1},
		},
	}
	if got := c.MatchSection("Tnope", "eng-foo"); got != "GlobalEng" {
		t.Errorf("expected global match for unknown teamID, got %q", got)
	}
}

func TestMatchSectionEmptyTeamID(t *testing.T) {
	c := Config{
		Sections: map[string]SectionDef{
			"GlobalEng": {Channels: []string{"eng-*"}, Order: 1},
		},
	}
	if got := c.MatchSection("", "eng-foo"); got != "GlobalEng" {
		t.Errorf("expected global match for empty teamID, got %q", got)
	}
}

func TestWorkspaceByTeamID(t *testing.T) {
	c := Config{
		Workspaces: map[string]Workspace{
			"work":   {TeamID: "T01", Theme: "dracula"},
			"T02LEG": {TeamID: "T02LEG", Theme: "nord"},
		},
	}
	if ws, ok := c.WorkspaceByTeamID("T01"); !ok || ws.Theme != "dracula" {
		t.Errorf("WorkspaceByTeamID(T01) = %+v, %v", ws, ok)
	}
	if ws, ok := c.WorkspaceByTeamID("T02LEG"); !ok || ws.Theme != "nord" {
		t.Errorf("WorkspaceByTeamID(T02LEG) = %+v, %v", ws, ok)
	}
	if _, ok := c.WorkspaceByTeamID("nope"); ok {
		t.Error("expected WorkspaceByTeamID(nope) to be not found")
	}
}

func TestTeamIDForDefaultWorkspaceSlug(t *testing.T) {
	c := Config{
		General: General{DefaultWorkspace: "work"},
		Workspaces: map[string]Workspace{
			"work": {TeamID: "T01ABCDEF"},
		},
	}
	got, err := c.TeamIDForDefaultWorkspace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "T01ABCDEF" {
		t.Errorf("got %q, want T01ABCDEF", got)
	}
}

func TestTeamIDForDefaultWorkspaceLegacyKey(t *testing.T) {
	c := Config{
		General: General{DefaultWorkspace: "T01ABCDEF"},
		Workspaces: map[string]Workspace{
			"T01ABCDEF": {TeamID: "T01ABCDEF"},
		},
	}
	got, err := c.TeamIDForDefaultWorkspace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "T01ABCDEF" {
		t.Errorf("got %q, want T01ABCDEF", got)
	}
}

func TestTeamIDForDefaultWorkspaceEmpty(t *testing.T) {
	c := Config{} // unset
	got, err := c.TeamIDForDefaultWorkspace()
	if err != nil {
		t.Fatalf("unexpected error for unset default: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestTeamIDForDefaultWorkspaceUnknownSlug(t *testing.T) {
	c := Config{
		General:    General{DefaultWorkspace: "ghost"},
		Workspaces: map[string]Workspace{"work": {TeamID: "T01"}},
	}
	if _, err := c.TeamIDForDefaultWorkspace(); err == nil {
		t.Fatal("expected error for unknown slug")
	}
}

func TestLoadWorkspaceOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[workspaces.work]
team_id = "T01ABCDEF"
order = 1

[workspaces.side]
team_id = "T02XYZABC"
order = 2

[workspaces.oss]
team_id = "T03QQQRRR"
`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Workspaces["work"].Order; got != 1 {
		t.Errorf("work order = %d, want 1", got)
	}
	if got := cfg.Workspaces["side"].Order; got != 2 {
		t.Errorf("side order = %d, want 2", got)
	}
	if got := cfg.Workspaces["oss"].Order; got != 0 {
		t.Errorf("oss order (unset) = %d, want 0", got)
	}
}

func TestEffectiveUseSlackSections_DefaultTrue(t *testing.T) {
	cfg := Config{}
	if !cfg.EffectiveUseSlackSections("T1") {
		t.Errorf("default should be true")
	}
}

func TestEffectiveUseSlackSections_GlobalFalse(t *testing.T) {
	f := false
	cfg := Config{General: General{UseSlackSections: &f}}
	if cfg.EffectiveUseSlackSections("T1") {
		t.Errorf("global=false should disable")
	}
}

func TestEffectiveUseSlackSections_WorkspaceOverride(t *testing.T) {
	tr, fa := true, false
	// Global=true (default), workspace=false → false
	cfg := Config{
		Workspaces: map[string]Workspace{
			"work": {TeamID: "T1", UseSlackSections: &fa},
		},
	}
	if cfg.EffectiveUseSlackSections("T1") {
		t.Errorf("workspace override (false) should win over global (true)")
	}
	// Global=false, workspace=true → true
	cfg2 := Config{
		General: General{UseSlackSections: &fa},
		Workspaces: map[string]Workspace{
			"work": {TeamID: "T1", UseSlackSections: &tr},
		},
	}
	if !cfg2.EffectiveUseSlackSections("T1") {
		t.Errorf("workspace override (true) should win over global (false)")
	}
}

func TestEffectiveUseSlackSections_UnknownTeamUsesGlobal(t *testing.T) {
	f := false
	cfg := Config{General: General{UseSlackSections: &f}}
	if cfg.EffectiveUseSlackSections("T_UNKNOWN") {
		t.Errorf("unknown team should fall through to global=false")
	}
}
