# Per-workspace Config Sections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow each Slack workspace in `config.toml` to define its own `[sections.*]` table, switching the `[workspaces.<key>]` block from raw team-ID keys to user-friendly slugs (`[workspaces.work] team_id = "T01..."`) while keeping every existing config working unchanged.

**Architecture:** Add `team_id` and `sections` fields to the per-workspace TOML block. After unmarshal, the loader (a) detects whether each block's TOML key is a slug (has `team_id` field) or a legacy team ID (key matches `^[TE][A-Z0-9]{6,}$` and no `team_id`), (b) builds in-memory indices keyed by team ID, and (c) exposes `MatchSection(teamID, channel)` and a slug-aware `WorkspaceByTeamID` lookup. Per-workspace sections fully replace global sections for that workspace; global remains the fallback for workspaces that define no sections of their own.

**Tech Stack:** Go 1.22+, `github.com/pelletier/go-toml/v2`, existing `huh` forms for the slug prompt.

**Spec:** [`docs/superpowers/specs/2026-04-30-per-workspace-config-sections-design.md`](../specs/2026-04-30-per-workspace-config-sections-design.md)

---

## File Structure

| File | Role |
|---|---|
| `internal/config/config.go` | Schema (`Workspace` struct), `Default`, `Load`. Key parsing/validation extracted to `workspaces.go`. |
| `internal/config/workspaces.go` | **New.** Slug regex, team-ID regex, slug derivation, index building, lookup methods (`WorkspaceByTeamID`, `TeamIDForSlug`, `MatchSection`). |
| `internal/config/config_test.go` | Updated for new API; new tests for slug/legacy/mixed loads + section overrides. |
| `internal/config/workspaces_test.go` | **New.** Slug derivation, key detection, conflict detection, `MatchSection` workspace-vs-global fallback. |
| `cmd/slk/main.go` | Sidebar call site passes active team ID into `MatchSection`. Theme-saver resolves team ID → slug/legacy key before writing. `listWorkspaces` prints slug column. |
| `cmd/slk/save_theme.go` | `saveWorkspaceTheme` takes the resolved TOML key (slug or team ID); new helper `appendNewWorkspaceBlock` writes `team_id = "..."` line for slug-keyed blocks. |
| `cmd/slk/save_theme_test.go` | Updated to exercise both slug-keyed and legacy team-ID-keyed write paths. |
| `cmd/slk/onboarding.go` | `addWorkspace` prompts for a slug (default = sluggified team name), writes `[workspaces.<slug>] team_id = "..."` to config alongside the existing token save. |
| `cmd/slk/add_workspace_config.go` | **New.** `appendWorkspaceConfigBlock(configPath, slug, teamID, teamName)` — small helper used by `addWorkspace`. |
| `cmd/slk/add_workspace_config_test.go` | **New.** Slug derivation + collision suffixing + config-file append. |

Each task is fully self-contained: write the failing test, watch it fail, implement, watch it pass, commit.

---

## Task 1: Add `Workspace` struct and team-ID/slug regexes

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/config/workspaces.go`

The current `WorkspaceSettings` only holds `Theme`. We rename it to `Workspace` and add `TeamID` plus `Sections`. We add the team-ID detection regex and slug derivation here so subsequent tasks can use them; we don't yet wire them into `Load` (that's Task 2).

- [ ] **Step 1.1: Write the failing tests**

Create `internal/config/workspaces_test.go` with:

```go
package config

import "testing"

func TestIsTeamIDKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"T01ABCDEF", true},
		{"E0123456", true},
		{"work", false},
		{"acme-corp", false},
		{"T01", true},      // 6+ alphanumerics after T/E
		{"T0", false},      // too short
		{"t01abcdef", false}, // lowercase not a Slack team ID
		{"", false},
	}
	for _, tc := range cases {
		if got := isTeamIDKey(tc.key); got != tc.want {
			t.Errorf("isTeamIDKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Acme Inc.", "acme-inc"},
		{"ACME", "acme"},
		{"  hello  world  ", "hello-world"},
		{"foo/bar_baz", "foo-bar-baz"},
		{"---trim---", "trim"},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 1.2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run "TestIsTeamIDKey|TestSlugify" -v`
Expected: FAIL with "undefined: isTeamIDKey" / "undefined: Slugify".

- [ ] **Step 1.3: Create `internal/config/workspaces.go`**

```go
package config

import (
	"regexp"
	"strings"
)

// teamIDKeyRe matches the shape of a raw Slack team ID
// (workspace IDs start with T, enterprise IDs with E). Used to
// recognize legacy [workspaces.T01ABCDEF] blocks that predate
// slug-keyed entries with explicit team_id fields.
var teamIDKeyRe = regexp.MustCompile(`^[TE][A-Z0-9]{6,}$`)

// isTeamIDKey reports whether s looks like a raw Slack team or
// enterprise ID. Used by Load to decide whether a [workspaces.<key>]
// TOML key whose block has no team_id field should be treated as a
// legacy team-ID key.
func isTeamIDKey(s string) bool {
	return teamIDKeyRe.MatchString(s)
}

// Slugify produces a lowercase, hyphen-separated slug from a
// human-readable name. Non-alphanumeric runes become hyphens; runs
// of hyphens are collapsed; leading/trailing hyphens are trimmed.
// Returns an empty string if the input has no alphanumeric content.
func Slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevDash := true // suppress leading hyphens
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
```

- [ ] **Step 1.4: Update `internal/config/config.go` — replace `WorkspaceSettings` with `Workspace`**

Find the existing struct (around line 77):

```go
// WorkspaceSettings holds per-workspace user preferences. Currently
// only Theme is configurable; future per-workspace settings (notification
// rules, default channel, etc.) belong here.
type WorkspaceSettings struct {
	Theme string `toml:"theme"`
}
```

Replace with:

```go
// Workspace holds per-workspace user preferences. The TOML key for
// the surrounding map can be either a user-chosen slug (with TeamID
// set explicitly via team_id) or — for backward compatibility —
// a raw Slack team ID (with TeamID left empty; Load fills it in
// from the key).
type Workspace struct {
	TeamID   string                `toml:"team_id"`
	Theme    string                `toml:"theme"`
	Sections map[string]SectionDef `toml:"sections"`
}
```

Then update the `Config` struct field around line 21:

```go
	Workspaces    map[string]WorkspaceSettings `toml:"workspaces"`
```

becomes

```go
	Workspaces    map[string]Workspace `toml:"workspaces"`
```

`ResolveTheme` is unchanged in this task — the field-level rename leaves the body type-correct because `Workspace` still has a `Theme` field. Task 3 replaces it with an index-based lookup.

- [ ] **Step 1.5: Run unit tests for the package**

Run: `go test ./internal/config/ -v`
Expected: PASS for new tests; existing `TestResolveTheme*` tests still PASS (the rename is API-only at the field level since `ws.Theme` access is unchanged in tests).

- [ ] **Step 1.6: Build the rest of the tree to find call sites that broke**

Run: `go build ./...`
Expected: failures referencing `config.WorkspaceSettings`. Grep them:

Run: `grep -rn "config.WorkspaceSettings\|WorkspaceSettings{" --include='*.go'`

Update each occurrence to `config.Workspace`. As of writing, only one call site exists in `cmd/slk/main.go` (around line 350):

```go
			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]config.WorkspaceSettings)
			}
```

becomes

```go
			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]config.Workspace)
			}
```

- [ ] **Step 1.7: Verify the whole tree builds and tests still pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 1.8: Commit**

```bash
git add internal/config/config.go internal/config/workspaces.go internal/config/workspaces_test.go cmd/slk/main.go
git commit -m "config: rename WorkspaceSettings to Workspace, add team_id/sections fields"
```

---

## Task 2: Resolve workspace keys at load time (slug + legacy)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/workspaces.go`
- Modify: `internal/config/config_test.go`

After unmarshalling the TOML, walk `cfg.Workspaces` and:
1. If a block has `team_id` set, the key is a slug.
2. Else if the key matches `isTeamIDKey`, treat the key itself as the team ID.
3. Else return an error.
4. Detect duplicate `team_id` across two slugs and error.

We mutate each `Workspace` in place to fill in `TeamID`. We do not build a separate `byTeamID` index yet — Task 3 introduces it together with `WorkspaceByTeamID`.

- [ ] **Step 2.1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 2.2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestLoadWorkspaces -v`
Expected: tests for legacy/missing-team_id/duplicate FAIL (TeamID empty / no error returned).

- [ ] **Step 2.3: Add the resolution function in `workspaces.go`**

Append to `internal/config/workspaces.go`:

```go
import "fmt"

// resolveWorkspaceKeys walks ws and, for each block, fills in TeamID
// when it can be inferred from the TOML key (legacy team-ID-keyed
// blocks). Returns an error if a slug-keyed block lacks team_id, if
// two slugs map to the same team_id, or if a slug-keyed block's
// team_id field is itself not a Slack-team-ID-shaped string.
func resolveWorkspaceKeys(ws map[string]Workspace) (map[string]Workspace, error) {
	if len(ws) == 0 {
		return ws, nil
	}
	out := make(map[string]Workspace, len(ws))
	seenTeamID := make(map[string]string, len(ws)) // teamID -> first slug we saw
	for key, w := range ws {
		switch {
		case w.TeamID != "":
			// Slug-keyed block. team_id must look like a real ID.
			if !isTeamIDKey(w.TeamID) {
				return nil, fmt.Errorf(
					"workspace %q: team_id %q does not look like a Slack team ID",
					key, w.TeamID)
			}
		case isTeamIDKey(key):
			// Legacy team-ID-keyed block; synthesize TeamID from key.
			w.TeamID = key
		default:
			return nil, fmt.Errorf(
				"workspace %q is missing team_id (the TOML key is a slug, "+
					"so the block must set team_id explicitly)", key)
		}
		if first, dup := seenTeamID[w.TeamID]; dup {
			return nil, fmt.Errorf(
				"workspaces %q and %q both reference team_id %q",
				first, key, w.TeamID)
		}
		seenTeamID[w.TeamID] = key
		out[key] = w
	}
	return out, nil
}
```

(Move the existing `import "regexp"` line into a multi-import block to add `"fmt"`.)

- [ ] **Step 2.4: Call it from `Load`**

In `internal/config/config.go`, find `Load`:

```go
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
```

Replace with:

```go
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	resolved, err := resolveWorkspaceKeys(cfg.Workspaces)
	if err != nil {
		return cfg, err
	}
	cfg.Workspaces = resolved

	return cfg, nil
}
```

- [ ] **Step 2.5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: all PASS, including pre-existing `TestResolveTheme*`.

- [ ] **Step 2.6: Commit**

```bash
git add internal/config/config.go internal/config/workspaces.go internal/config/config_test.go
git commit -m "config: resolve slug/legacy workspace keys at load, error on conflicts"
```

---

## Task 3: `WorkspaceByTeamID` and section-aware `MatchSection`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/workspaces.go`
- Modify: `internal/config/config_test.go`

Existing `(Config).MatchSection(name string)` becomes `(Config).MatchSection(teamID, name string)`. Per-workspace `Sections` maps fully replace the global one when non-empty. Also add `(Config).WorkspaceByTeamID(teamID) (Workspace, bool)` for callers (theme-saver, future overrides) that have a team ID and need the matching block.

We do not pre-build an index — there are at most a handful of workspaces; an O(N) scan is fine and keeps `Config` a value type with no hidden state.

- [ ] **Step 3.1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
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
			"work":    {TeamID: "T01", Theme: "dracula"},
			"T02LEG":  {TeamID: "T02LEG", Theme: "nord"},
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
```

- [ ] **Step 3.2: Update existing `TestResolveTheme*` tests for the new field**

The pre-existing tests pass team IDs like `"T01"` as map keys. With the new `Workspace` type carrying `TeamID`, they need to set it. Update each test (around lines 118–172):

```go
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
```

Apply the same `{TeamID: "T01", Theme: ...}` shape to each `TestResolveTheme*` test.

- [ ] **Step 3.3: Run tests to verify they fail**

Run: `go test ./internal/config/ -run "TestMatchSection|TestWorkspaceByTeamID" -v`
Expected: FAIL. The current `MatchSection(name)` signature accepts one arg; new tests pass two — compile error.

- [ ] **Step 3.4: Update `MatchSection` and add `WorkspaceByTeamID` and `ResolveTheme`**

In `internal/config/config.go`, replace the existing `MatchSection` (around line 142) with the team-ID-aware version, and add `WorkspaceByTeamID`. Update `ResolveTheme` to use the new helper.

```go
// WorkspaceByTeamID returns the configured Workspace for the given
// team ID, scanning c.Workspaces (which is keyed by either slug or
// legacy team ID). Returns false if no workspace matches.
func (c Config) WorkspaceByTeamID(teamID string) (Workspace, bool) {
	if teamID == "" {
		return Workspace{}, false
	}
	for _, ws := range c.Workspaces {
		if ws.TeamID == teamID {
			return ws, true
		}
	}
	return Workspace{}, false
}

// MatchSection returns the section name for a given channel name in
// the context of the given workspace. If the workspace has its own
// non-empty Sections map, that fully replaces the global Sections;
// otherwise the global Sections apply. Returns "" if no pattern
// matches.
func (c Config) MatchSection(teamID, channelName string) string {
	sections := c.Sections
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && len(ws.Sections) > 0 {
		sections = ws.Sections
	}
	return matchSectionIn(sections, channelName)
}

// matchSectionIn walks sections in Order-ascending order and returns
// the first section name whose patterns match channelName.
func matchSectionIn(sections map[string]SectionDef, channelName string) string {
	type entry struct {
		name     string
		order    int
		patterns []string
	}
	var entries []entry
	for name, def := range sections {
		entries = append(entries, entry{name: name, order: def.Order, patterns: def.Channels})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].order < entries[j].order
	})
	for _, e := range entries {
		for _, pattern := range e.patterns {
			if matched, _ := filepath.Match(pattern, channelName); matched {
				return e.name
			}
		}
	}
	return ""
}
```

Replace `ResolveTheme`:

```go
func (c Config) ResolveTheme(teamID string) string {
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && ws.Theme != "" {
		return ws.Theme
	}
	if c.Appearance.Theme != "" {
		return c.Appearance.Theme
	}
	return "nord"
}
```

Delete the original body of `MatchSection` (the function from line 140 onward you replaced).

- [ ] **Step 3.5: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: all PASS.

- [ ] **Step 3.6: Update the sole `MatchSection` caller**

Find it (`cmd/slk/main.go:835`):

```go
		section := cfg.MatchSection(ch.Name)
```

Becomes:

```go
		section := cfg.MatchSection(client.TeamID(), ch.Name)
```

In the same loop, the `cfg.Sections[section]` lookup (around line 838) must also resolve the right section table — global vs. per-workspace. Replace the block:

```go
		section := cfg.MatchSection(ch.Name)
		var sectionOrder int
		if section != "" {
			if def, ok := cfg.Sections[section]; ok {
				sectionOrder = def.Order
			}
		}
```

With:

```go
		section := cfg.MatchSection(client.TeamID(), ch.Name)
		var sectionOrder int
		if section != "" {
			sectionOrder = cfg.SectionOrder(client.TeamID(), section)
		}
```

Add a small helper next to `MatchSection` in `internal/config/config.go`:

```go
// SectionOrder returns the Order field for the named section,
// resolved through the same workspace-vs-global precedence as
// MatchSection. Returns 0 if the section is not defined.
func (c Config) SectionOrder(teamID, sectionName string) int {
	sections := c.Sections
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && len(ws.Sections) > 0 {
		sections = ws.Sections
	}
	if def, ok := sections[sectionName]; ok {
		return def.Order
	}
	return 0
}
```

- [ ] **Step 3.7: Verify whole tree builds and tests pass**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3.8: Commit**

```bash
git add internal/config/config.go cmd/slk/main.go internal/config/config_test.go
git commit -m "config: per-workspace MatchSection with global fallback"
```

---

## Task 4: Slug-aware `saveWorkspaceTheme`

**Files:**
- Modify: `cmd/slk/save_theme.go`
- Modify: `cmd/slk/main.go`
- Modify: `cmd/slk/save_theme_test.go`

Today the theme-switcher writes `[workspaces.<teamID>]` straight from the active team ID. Now the loader may have mapped `<teamID>` to a slug key. The theme-saver needs to:

- If a workspace block already exists in the config matching `teamID` (via slug or legacy key), update that existing block.
- If no block exists, create one keyed by the team ID (the legacy form). This preserves the old behavior for users who haven't opted into slugs; users who *have* slugs already have a block to update.

Design: change `saveWorkspaceTheme` to accept the resolved `tomlKey` (the actual `[workspaces.<key>]` string) plus `teamID`. The caller in `main.go` looks up the right key from the loaded config.

- [ ] **Step 4.1: Write the failing tests**

Replace `TestSaveWorkspaceThemeUpdatesExisting` (currently lines 73–104 in `cmd/slk/save_theme_test.go`) and add coverage for the slug-keyed update path.

```go
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

	if err := saveWorkspaceTheme(path, "T01ABCDEF", "T01ABCDEF", "ACME Corp", "tokyo night"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `theme = "tokyo night"`) {
		t.Errorf("expected updated theme, got:\n%s", got)
	}
	if strings.Contains(string(got), `theme = "dracula"`) {
		t.Errorf("old theme still present:\n%s", got)
	}
	if !strings.Contains(string(got), "# ACME Corp") {
		t.Errorf("comment was lost:\n%s", got)
	}
}

func TestSaveWorkspaceThemeUpdatesExistingSlugBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := `[appearance]
theme = "dark"

# ACME Corp
[workspaces.work]
team_id = "T01ABCDEF"
theme = "dracula"
`
	if err := os.WriteFile(path, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	// tomlKey = "work" (the slug); teamID is the underlying ID.
	if err := saveWorkspaceTheme(path, "work", "T01ABCDEF", "ACME Corp", "tokyo night"); err != nil {
		t.Fatalf("saveWorkspaceTheme: %v", err)
	}

	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, `theme = "tokyo night"`) {
		t.Errorf("expected slug block updated, got:\n%s", s)
	}
	// team_id line must still be present and unchanged.
	if !strings.Contains(s, `team_id = "T01ABCDEF"`) {
		t.Errorf("team_id line was clobbered, got:\n%s", s)
	}
	// Header should still be the slug.
	if !strings.Contains(s, "[workspaces.work]") {
		t.Errorf("slug header was lost, got:\n%s", s)
	}
}
```

Update the other `TestSaveWorkspaceTheme*` tests so the call sites match the new signature (`saveWorkspaceTheme(path, tomlKey, teamID, teamName, themeName)`). For all of them, `tomlKey == teamID` mirrors the legacy default:

```go
	if err := saveWorkspaceTheme(path, "T01ABCDEF", "T01ABCDEF", "ACME Corp", "dracula"); err != nil {
```

```go
	if err := saveWorkspaceTheme(path, "T01", "T01", "ACME", "dracula"); err != nil {
```

```go
	if err := saveWorkspaceTheme(path, "T02", "T02", "Personal", "tokyo night"); err != nil {
```

```go
	if err := saveWorkspaceTheme(path, "T01", "T01", badName, "dracula"); err != nil {
```

(Leave assertions about file contents unchanged — the legacy-key behavior is preserved when slug == teamID.)

- [ ] **Step 4.2: Run tests to verify they fail**

Run: `go test ./cmd/slk/ -run TestSaveWorkspaceTheme -v`
Expected: FAIL on signature mismatch / new slug-block test.

- [ ] **Step 4.3: Update `saveWorkspaceTheme` signature and behavior**

Replace `saveWorkspaceTheme` in `cmd/slk/save_theme.go` (line 89 onward):

```go
// saveWorkspaceTheme rewrites or appends a [workspaces.<tomlKey>]
// theme entry. tomlKey is the literal TOML key in the config — for
// slug-keyed blocks that's the slug, for legacy blocks it's the team
// ID. teamID is the underlying Slack team ID; when we are creating a
// brand-new slug-keyed block, teamID is written as the team_id =
// "..." line (currently we only create legacy-keyed blocks here, but
// slug callers update an existing block).
func saveWorkspaceTheme(configPath, tomlKey, teamID, teamName, themeName string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	header := fmt.Sprintf("[workspaces.%s]", tomlKey)

	sectionStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			sectionStart = i
			break
		}
	}

	if sectionStart >= 0 {
		end := len(lines)
		for j := sectionStart + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || strings.HasPrefix(t, "[") {
				end = j
				break
			}
		}
		updated := false
		for j := sectionStart + 1; j < end; j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "theme") && strings.Contains(t, "=") {
				lines[j] = "theme = " + tomlString(themeName)
				updated = true
				break
			}
		}
		if !updated {
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, "theme = "+tomlString(themeName))
			newLines = append(newLines, lines[sectionStart+1:]...)
			lines = newLines
		}
		return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
	}

	// No existing section — append at end. We only get here when no
	// block exists for either the slug or the team ID, which means we
	// fall back to a legacy-keyed [workspaces.<teamID>] block.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	commentLine := "# " + safeName
	legacyHeader := fmt.Sprintf("[workspaces.%s]", teamID)
	lines = append(lines, commentLine, legacyHeader, "theme = "+tomlString(themeName))
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}
```

- [ ] **Step 4.4: Update the call site in `main.go`**

Find the theme-saver block (around line 337–365). Replace:

```go
		case themeswitcher.ScopeWorkspace:
			if activeTeamID == "" {
				return // shouldn't happen, but guard against it
			}
			// Find the team name for the comment.
			teamName := activeTeamID
			if wctx, ok := workspaces[activeTeamID]; ok && wctx.TeamName != "" {
				teamName = wctx.TeamName
			}
			// Update in-memory config.
			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]config.Workspace)
			}
			ws := cfg.Workspaces[activeTeamID]
			ws.Theme = name
			cfg.Workspaces[activeTeamID] = ws
			// Persist.
			if err := saveWorkspaceTheme(configPath, activeTeamID, teamName, name); err != nil {
				log.Printf("save workspace theme: %v", err)
			}
```

With:

```go
		case themeswitcher.ScopeWorkspace:
			if activeTeamID == "" {
				return // shouldn't happen, but guard against it
			}
			teamName := activeTeamID
			if wctx, ok := workspaces[activeTeamID]; ok && wctx.TeamName != "" {
				teamName = wctx.TeamName
			}
			// Find the existing TOML key for this workspace, if any.
			// If no block exists yet we use the team ID as the key
			// (legacy default); a future --add-workspace may have
			// already written a slug-keyed block.
			tomlKey := activeTeamID
			for k, w := range cfg.Workspaces {
				if w.TeamID == activeTeamID {
					tomlKey = k
					break
				}
			}
			// Update in-memory config.
			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]config.Workspace)
			}
			ws := cfg.Workspaces[tomlKey]
			ws.TeamID = activeTeamID
			ws.Theme = name
			cfg.Workspaces[tomlKey] = ws
			// Persist.
			if err := saveWorkspaceTheme(configPath, tomlKey, activeTeamID, teamName, name); err != nil {
				log.Printf("save workspace theme: %v", err)
			}
```

- [ ] **Step 4.5: Run tests + build**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 4.6: Commit**

```bash
git add cmd/slk/save_theme.go cmd/slk/save_theme_test.go cmd/slk/main.go
git commit -m "saveWorkspaceTheme: take tomlKey, update existing slug blocks in place"
```

---

## Task 5: Slug prompt + config write in `--add-workspace`

**Files:**
- Create: `cmd/slk/add_workspace_config.go`
- Create: `cmd/slk/add_workspace_config_test.go`
- Modify: `cmd/slk/onboarding.go`

`addWorkspace` currently saves a token and exits. It does *not* touch `config.toml`. We add a step that:

1. Suggests a default slug = `Slugify(teamName)`. If the slug is already used in `config.toml` (or empty), append `-2`, `-3`, ... until unique.
2. Lets the user accept or edit the slug via a `huh` input.
3. Appends `[workspaces.<slug>] team_id = "<TID>"` to `config.toml`. If the file does not exist, create it.

If the user enters an invalid slug (matches `isTeamIDKey`, contains a `[` or `]`, or is empty), the form re-prompts.

- [ ] **Step 5.1: Write the failing tests**

Create `cmd/slk/add_workspace_config_test.go`:

```go
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
```

- [ ] **Step 5.2: Run tests to verify they fail**

Run: `go test ./cmd/slk/ -run "TestUniqueSlug|TestAppendWorkspaceConfigBlock" -v`
Expected: FAIL with "undefined: uniqueSlug" / "undefined: appendWorkspaceConfigBlock".

- [ ] **Step 5.3: Implement the helpers**

Create `cmd/slk/add_workspace_config.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gammons/slk/internal/config"
)

// uniqueSlug returns base if it is non-empty and not in existing,
// otherwise appends -2, -3, ... until it finds an unused slug.
// An empty base falls back to "workspace".
func uniqueSlug(base string, existing map[string]bool) string {
	if base == "" {
		base = "workspace"
	}
	if !existing[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !existing[candidate] {
			return candidate
		}
	}
}

// appendWorkspaceConfigBlock appends a [workspaces.<slug>] block with
// team_id set, prefixed by a "# <teamName>" comment line. The file
// is created if it does not exist. Existing content is preserved
// verbatim (textual append, not TOML re-marshal).
func appendWorkspaceConfigBlock(configPath, slug, teamID, teamName string) error {
	var existing []byte
	if data, err := os.ReadFile(configPath); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	fmt.Fprintf(&b, "# %s\n", safeName)
	fmt.Fprintf(&b, "[workspaces.%s]\n", slug)
	fmt.Fprintf(&b, "team_id = %s\n", tomlString(teamID))

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(b.String()), 0644)
}

// existingSlugs reads configPath (if present) and returns the set of
// already-used [workspaces.<key>] keys. Used by addWorkspace to
// avoid colliding with existing slug or legacy entries.
func existingSlugs(configPath string) map[string]bool {
	cfg, err := config.Load(configPath)
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(cfg.Workspaces))
	for k := range cfg.Workspaces {
		out[k] = true
	}
	return out
}
```

- [ ] **Step 5.4: Run helper tests**

Run: `go test ./cmd/slk/ -run "TestUniqueSlug|TestAppendWorkspaceConfigBlock" -v`
Expected: PASS.

- [ ] **Step 5.5: Wire the slug prompt into `addWorkspace`**

In `cmd/slk/onboarding.go`, after the `tokenStore.Save(token)` block (around line 175–177), but before the final success message, insert:

```go
	// Pick a slug for the new workspace and append a [workspaces.<slug>]
	// block to config.toml so the user can extend per-workspace settings
	// (sections, theme, etc.) later by hand.
	configPath := filepath.Join(xdgConfig(), "config.toml")
	defaultSlug := uniqueSlug(config.Slugify(wsName), existingSlugs(configPath))

	var slug string
	slugForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Workspace slug").
				Description("Short identifier used in config.toml (e.g. [workspaces.<slug>])").
				Placeholder(defaultSlug).
				Value(&slug).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return nil // empty -> placeholder default
					}
					if config.Slugify(s) != s {
						return fmt.Errorf("must be lowercase letters, digits, and hyphens")
					}
					if existingSlugs(configPath)[s] {
						return fmt.Errorf("slug %q is already used", s)
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeFunc(huh.ThemeDracula))

	if err := slugForm.Run(); err != nil {
		slug = defaultSlug
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = defaultSlug
	}

	if err := appendWorkspaceConfigBlock(configPath, slug, teamID, wsName); err != nil {
		// Don't fail the whole onboarding: the token saved, the user
		// can hand-edit config later. Just warn.
		fmt.Println(errorStyle.Render("  Note: could not write config.toml: " + err.Error()))
	}
```

Add `"github.com/gammons/slk/internal/config"` to the imports of `onboarding.go`.

- [ ] **Step 5.6: Build and run all tests**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 5.7: Commit**

```bash
git add cmd/slk/add_workspace_config.go cmd/slk/add_workspace_config_test.go cmd/slk/onboarding.go
git commit -m "add-workspace: prompt for slug and write [workspaces.<slug>] block"
```

---

## Task 6: `--list-workspaces` shows the slug column

**Files:**
- Modify: `cmd/slk/main.go`

Add a SLUG column derived from the loaded config. Workspaces present in tokens but absent from config show an empty slug.

- [ ] **Step 6.1: Update `listWorkspaces`**

Replace the body of `listWorkspaces` (lines 1577–1603 of `cmd/slk/main.go`):

```go
func listWorkspaces() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}
	configPath := filepath.Join(xdgConfig(), "config.toml")
	cfg, _ := config.Load(configPath) // best-effort

	slugByTeamID := make(map[string]string, len(cfg.Workspaces))
	for k, w := range cfg.Workspaces {
		slugByTeamID[w.TeamID] = k
	}

	idW, slugW, nameW := len("TEAM ID"), len("SLUG"), len("NAME")
	for _, t := range tokens {
		if len(t.TeamID) > idW {
			idW = len(t.TeamID)
		}
		if s := slugByTeamID[t.TeamID]; len(s) > slugW {
			slugW = len(s)
		}
		if len(t.TeamName) > nameW {
			nameW = len(t.TeamName)
		}
	}
	fmt.Printf("%-*s  %-*s  %s\n", idW, "TEAM ID", slugW, "SLUG", "NAME")
	fmt.Printf("%s  %s  %s\n",
		strings.Repeat("-", idW),
		strings.Repeat("-", slugW),
		strings.Repeat("-", nameW))
	for _, t := range tokens {
		fmt.Printf("%-*s  %-*s  %s\n", idW, t.TeamID, slugW, slugByTeamID[t.TeamID], t.TeamName)
	}
	return nil
}
```

- [ ] **Step 6.2: Verify build + tests**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 6.3: Manual smoke (optional)**

Run: `go run ./cmd/slk --list-workspaces`
Expected: three columns, with SLUG populated for any workspaces written via the new `--add-workspace` flow.

- [ ] **Step 6.4: Commit**

```bash
git add cmd/slk/main.go
git commit -m "list-workspaces: print SLUG column from config"
```

---

## Task 7: README + sample config update

**Files:**
- Modify: `README.md`

Update the `## Configuration` section so the sample reflects the new shape. Replace the existing config block (around lines 266–305) with:

```toml
[general]
default_workspace = "work"   # the slug, not the team ID

[appearance]
theme = "dracula"
timestamp_format = "3:04 PM"

[animations]
enabled = true
smooth_scrolling = true
typing_indicators = true

[notifications]
enabled = true
on_mention = true
on_dm = true
on_keyword = ["deploy", "incident"]
quiet_hours = "22:00-08:00"   # planned

[cache]
message_retention_days = 30
max_db_size_mb = 500

# Global channel sections (used as a fallback for workspaces that
# don't define their own).
[sections.Alerts]
channels = ["alerts", "ops", "*-alerts"]
order = 1

# Per-workspace settings: keyed by a slug you choose at --add-workspace
# time. team_id ties the slug to the underlying Slack workspace.
[workspaces.work]
team_id = "T01ABCDEF"
theme   = "dracula"          # overrides [appearance].theme

[workspaces.work.sections.Alerts]
channels = ["alerts", "*-alerts"]
order = 1

[workspaces.work.sections.Engineering]
channels = ["eng-*", "deploys"]
order = 2

# A second workspace with no per-workspace sections — falls back to
# the global [sections.*] above.
[workspaces.side]
team_id = "T02XYZ"
```

Add a short note immediately after the block:

```
Per-workspace `[workspaces.<slug>.sections.*]` blocks fully replace the
global `[sections.*]` for that workspace. Workspaces that define no
sections of their own fall back to the global table.

Legacy configs that key the block by raw team ID
(`[workspaces.T01ABCDEF]`) keep working unchanged.
```

- [ ] **Step 7.1: Edit README.md**

Apply the replacement above with `Edit`.

- [ ] **Step 7.2: Commit**

```bash
git add README.md
git commit -m "docs: document per-workspace sections and slug-keyed workspace blocks"
```

---

## Final verification

Before declaring the work done:

- [ ] `go build ./...` — PASS
- [ ] `go test ./...` — PASS
- [ ] `go run ./cmd/slk --list-workspaces` against an existing legacy config still works.
- [ ] A hand-edited `[workspaces.<slug>] team_id = "..."` block loads without error.
- [ ] Removing the `team_id` line from a slug-keyed block produces a clear error mentioning the workspace name.
