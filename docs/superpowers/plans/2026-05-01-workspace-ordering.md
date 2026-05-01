# Workspace Ordering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users specify a stable order for workspaces in `config.toml` via an `order` field, so that `1`–`9` keys jump to predictable workspaces every run.

**Architecture:** Add an `Order int` field to the `[workspaces.<slug>]` config block. At startup, sort the tokens slice by config-defined order before the rail is populated. The rail is already pre-populated up front in `cmd/slk/main.go`, so no rail-population timing changes are needed — just a deterministic sort.

**Tech Stack:** Go 1.22+, pelletier/go-toml/v2, charmbracelet/bubbletea.

---

## Implementation note

After writing the spec we discovered that the workspace rail is already
populated up front at startup (`cmd/slk/main.go:400-416` iterates
`tokens` and calls `app.SetWorkspaces(wsItems)` once before any
goroutine connects). The `1`–`9` key handler reads
`a.workspaceItems` (set by `SetWorkspaces`), not `wsMgr`. So:

- **No** `SeedWorkspace`/`UpdateWorkspace` split is needed.
- **No** new `WorkspacesSeededMsg` is needed.
- We do **not** need to change how `wsMgr.AddWorkspace` is called.

The fix is: sort `tokens` by config-defined order **before** the
existing `wsItems` / `wsNames` loops. The rest of the codebase needs
zero changes beyond the new config field, the sort helper, and the
README.

---

## File Structure

**Create:**
- `internal/config/ordering.go` — pure helper that sorts tokens by config order.
- `internal/config/ordering_test.go` — table-driven tests for the helper.

**Modify:**
- `internal/config/config.go` — add `Order int` to `Workspace` struct.
- `internal/config/config_test.go` — round-trip test for the new field.
- `cmd/slk/main.go` — call the new sort helper before building `wsItems`.
- `README.md` — document `order` in the example config block.

No changes needed in `internal/service/workspace.go`, `internal/ui/app.go`, or any rail/UI code.

---

## Task 1: Add `Order` field to config schema

**Files:**
- Modify: `internal/config/config.go:91-95` (the `Workspace` struct)
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Append this to `internal/config/config_test.go` (after the existing tests):

```go
func TestLoadWorkspaceOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	contents := `
[workspaces.work]
team_id = "T01ABCDEF"
order = 1

[workspaces.side]
team_id = "T02XYZ"
order = 2

[workspaces.oss]
team_id = "T03QQQ"
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
```

(`os` and `filepath` are already imported in that test file; if not, add them.)

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/config/ -run TestLoadWorkspaceOrder -v
```

Expected: FAIL — `cfg.Workspaces["work"].Order` is undefined (compile error: `Order undefined`).

- [ ] **Step 3: Add the field to the `Workspace` struct**

In `internal/config/config.go`, change the `Workspace` struct from:

```go
type Workspace struct {
	TeamID   string                `toml:"team_id"`
	Theme    string                `toml:"theme"`
	Sections map[string]SectionDef `toml:"sections"`
}
```

to:

```go
type Workspace struct {
	TeamID string `toml:"team_id"`
	Theme  string `toml:"theme"`
	// Order controls the workspace's position in the rail and the
	// digit-key mapping (1-9). Positive values are explicit positions
	// ascending; 0 or unset means "unordered" (sorts after ordered
	// workspaces, alphabetically by slug). Ties in Order break by slug.
	Order    int                   `toml:"order"`
	Sections map[string]SectionDef `toml:"sections"`
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/config/ -run TestLoadWorkspaceOrder -v
```

Expected: PASS.

- [ ] **Step 5: Run the full config package tests to verify no regression**

```bash
go test ./internal/config/ -v
```

Expected: all PASS, including legacy round-trip tests (zero `Order` value preserves existing behavior).

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: add Order field to per-workspace block"
```

---

## Task 2: Implement the ordering helper

**Files:**
- Create: `internal/config/ordering.go`
- Create: `internal/config/ordering_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/ordering_test.go`:

```go
package config

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/slack"
)

func TestOrderTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens []slack.Token
		cfg    Config
		want   []string // expected TeamIDs, in order
	}{
		{
			name: "all ordered, distinct values",
			tokens: []slack.Token{
				{TeamID: "T1", TeamName: "Work"},
				{TeamID: "T2", TeamName: "Side"},
				{TeamID: "T3", TeamName: "Oss"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"work": {TeamID: "T1", Order: 3},
				"side": {TeamID: "T2", Order: 1},
				"oss":  {TeamID: "T3", Order: 2},
			}},
			want: []string{"T2", "T3", "T1"},
		},
		{
			name: "ties broken by slug alphabetically",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"zebra": {TeamID: "T1", Order: 1},
				"apple": {TeamID: "T2", Order: 1},
			}},
			want: []string{"T2", "T1"}, // apple before zebra
		},
		{
			name: "ordered, then unordered configured (by slug), then unconfigured (by team id)",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
				{TeamID: "T3"},
				{TeamID: "T4"},
				{TeamID: "T5"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"work":  {TeamID: "T1", Order: 1},
				"side":  {TeamID: "T2", Order: 2},
				"zebra": {TeamID: "T3"}, // configured but no order
				"apple": {TeamID: "T4"}, // configured but no order
				// T5: not in config at all
			}},
			want: []string{"T1", "T2", "T4", "T3", "T5"},
		},
		{
			name: "explicit order = 0 treated as unordered",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"first":  {TeamID: "T1", Order: 0},
				"second": {TeamID: "T2", Order: 1},
			}},
			want: []string{"T2", "T1"}, // T2 ordered first, T1 falls into unordered bucket
		},
		{
			name: "negative order treated as unordered",
			tokens: []slack.Token{
				{TeamID: "T1"},
				{TeamID: "T2"},
			},
			cfg: Config{Workspaces: map[string]Workspace{
				"a": {TeamID: "T1", Order: -5},
				"b": {TeamID: "T2", Order: 1},
			}},
			want: []string{"T2", "T1"},
		},
		{
			name:   "empty token list returns empty slice",
			tokens: nil,
			cfg:    Config{},
			want:   nil,
		},
		{
			name: "no config block at all",
			tokens: []slack.Token{
				{TeamID: "T2"},
				{TeamID: "T1"},
			},
			cfg:  Config{},
			want: []string{"T1", "T2"}, // alphabetical by team ID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OrderTokens(tt.tokens, tt.cfg)
			gotIDs := make([]string, len(got))
			for i, ot := range got {
				gotIDs[i] = ot.Token.TeamID
			}
			if !reflect.DeepEqual(gotIDs, tt.want) {
				t.Errorf("OrderTokens = %v, want %v", gotIDs, tt.want)
			}
		})
	}
}

func TestOrderTokensPreservesSlug(t *testing.T) {
	tokens := []slack.Token{{TeamID: "T1"}}
	cfg := Config{Workspaces: map[string]Workspace{
		"work": {TeamID: "T1", Order: 1},
	}}
	got := OrderTokens(tokens, cfg)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Slug != "work" {
		t.Errorf("Slug = %q, want %q", got[0].Slug, "work")
	}
	if got[0].Order != 1 {
		t.Errorf("Order = %d, want 1", got[0].Order)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test ./internal/config/ -run TestOrderTokens -v
```

Expected: FAIL — `OrderTokens` is undefined.

- [ ] **Step 3: Implement the helper**

Create `internal/config/ordering.go`:

```go
package config

import (
	"sort"

	"github.com/gammons/slk/internal/slack"
)

// OrderedToken is a token paired with its config-derived ordering metadata.
// Slug is the [workspaces.<slug>] key from config, or "" if the workspace
// has no config block. Order is the value of the `order` field, or 0
// when unset/non-positive.
type OrderedToken struct {
	Token slack.Token
	Slug  string
	Order int
}

// OrderTokens returns the tokens sorted into a stable, user-configurable
// order:
//
//  1. Configured with Order > 0 — sorted ascending by Order, ties
//     broken alphabetically by slug.
//  2. Configured but Order <= 0 — sorted alphabetically by slug.
//  3. Not in config at all — sorted alphabetically by TeamID.
//
// Bucket boundaries are stable: any bucket-1 entry precedes every
// bucket-2 entry, which precedes every bucket-3 entry.
func OrderTokens(tokens []slack.Token, cfg Config) []OrderedToken {
	if len(tokens) == 0 {
		return nil
	}

	// Build a TeamID -> (slug, Workspace) lookup.
	type cfgEntry struct {
		slug string
		ws   Workspace
	}
	byTeam := make(map[string]cfgEntry, len(cfg.Workspaces))
	for slug, ws := range cfg.Workspaces {
		if ws.TeamID != "" {
			byTeam[ws.TeamID] = cfgEntry{slug: slug, ws: ws}
		}
	}

	out := make([]OrderedToken, 0, len(tokens))
	for _, tok := range tokens {
		ot := OrderedToken{Token: tok}
		if entry, ok := byTeam[tok.TeamID]; ok {
			ot.Slug = entry.slug
			if entry.ws.Order > 0 {
				ot.Order = entry.ws.Order
			}
		}
		out = append(out, ot)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		ba, bb := bucket(a), bucket(b)
		if ba != bb {
			return ba < bb
		}
		switch ba {
		case 1:
			if a.Order != b.Order {
				return a.Order < b.Order
			}
			return a.Slug < b.Slug
		case 2:
			return a.Slug < b.Slug
		default: // 3
			return a.Token.TeamID < b.Token.TeamID
		}
	})
	return out
}

// bucket returns 1, 2, or 3 per the rules in OrderTokens.
func bucket(o OrderedToken) int {
	if o.Slug != "" && o.Order > 0 {
		return 1
	}
	if o.Slug != "" {
		return 2
	}
	return 3
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
go test ./internal/config/ -run TestOrderTokens -v
```

Expected: PASS for all sub-tests, including `TestOrderTokensPreservesSlug`.

- [ ] **Step 5: Run the full config package tests**

```bash
go test ./internal/config/ -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/ordering.go internal/config/ordering_test.go
git commit -m "config: add OrderTokens helper for deterministic workspace ordering"
```

---

## Task 3: Wire OrderTokens into startup

**Files:**
- Modify: `cmd/slk/main.go:400-416` (rail item / loading-workspaces construction)

- [ ] **Step 1: Write the failing test**

There's no existing unit test for the `cmd/slk/main.go` startup glue, and refactoring it for testability is out of scope here. The behavior is verified by:

1. Task 2's tests confirm `OrderTokens` itself is correct.
2. The full test suite (`go test ./...`) confirms nothing else breaks.
3. Manual verification — see Step 4 below.

Skip directly to the implementation step.

- [ ] **Step 2: Modify the startup code**

Open `cmd/slk/main.go` and find the block at lines 400–416:

```go
	// Build workspace rail items for all tokens
	var wsItems []workspace.WorkspaceItem
	for _, token := range tokens {
		wsItems = append(wsItems, workspace.WorkspaceItem{
			ID:       token.TeamID,
			Name:     token.TeamName,
			Initials: workspace.WorkspaceInitials(token.TeamName),
		})
	}

	// Set up loading overlay with workspace names
	var wsNames []string
	for _, t := range tokens {
		wsNames = append(wsNames, t.TeamName)
	}
	app.SetLoadingWorkspaces(wsNames)
	app.SetWorkspaces(wsItems)
```

Replace it with:

```go
	// Apply user-configured workspace ordering to tokens before
	// building the rail. The rail and digit-key (1-9) mapping both
	// follow this order, so a stable sort here is what makes
	// `1` always go to the same workspace across runs.
	orderedTokens := config.OrderTokens(tokens, cfg)

	// Build workspace rail items for all tokens, in configured order.
	var wsItems []workspace.WorkspaceItem
	for _, ot := range orderedTokens {
		wsItems = append(wsItems, workspace.WorkspaceItem{
			ID:       ot.Token.TeamID,
			Name:     ot.Token.TeamName,
			Initials: workspace.WorkspaceInitials(ot.Token.TeamName),
		})
	}

	// Set up loading overlay with workspace names, in the same order
	// so the loading list visually matches the rail.
	var wsNames []string
	for _, ot := range orderedTokens {
		wsNames = append(wsNames, ot.Token.TeamName)
	}
	app.SetLoadingWorkspaces(wsNames)
	app.SetWorkspaces(wsItems)
```

Also update the connection-launch loop near line 814 to iterate
`orderedTokens` so connections kick off in the same order (the
goroutines still run in parallel, but starting them in rail order
is a tiny consistency win and avoids an unused-variable warning):

Find the block at line 814:

```go
	for _, token := range tokens {
		go func(tok slackclient.Token) {
```

and change it to:

```go
	for _, ot := range orderedTokens {
		go func(tok slackclient.Token) {
```

The closure body is unchanged; only the loop variable source differs.
The variable `tokens` is still used earlier (e.g., the
`default_workspace` validation loop at lines 790–795 and any other
references), so leave that intact.

- [ ] **Step 3: Verify it builds**

```bash
go build ./...
```

Expected: no errors. If the linter flags `tokens` as unused in any
remaining context, audit those references — but per the grep above,
`tokens` is still used for `default_workspace` validation, so it
should remain.

- [ ] **Step 4: Manual verification**

Spot-check the change works end-to-end:

```bash
go build -o /tmp/slk-ordering ./cmd/slk
```

If you have a working `~/.local/share/slk/tokens/` with multiple
workspaces, edit `~/.config/slk/config.toml` to add `order = N`
fields and run `/tmp/slk-ordering`. Verify the rail matches the
configured order and `1`–`9` jump as expected. (This step is
optional if no multi-workspace test setup is handy; the unit tests
plus the full suite are the authoritative verification.)

- [ ] **Step 5: Run the full test suite**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "main: sort workspaces by configured order at startup"
```

---

## Task 4: Document the new field in the README

**Files:**
- Modify: `README.md` (the configuration example around lines 318–376)

- [ ] **Step 1: Update the example config block**

In `README.md`, find the `[workspaces.work]` example (around line 353):

```toml
[workspaces.work]
team_id = "T01ABCDEF"
theme   = "dracula"          # overrides [appearance].theme
```

Change it to:

```toml
[workspaces.work]
team_id = "T01ABCDEF"
order   = 1                  # rail position; 1-based, used by 1-9 keys
theme   = "dracula"          # overrides [appearance].theme
```

And update the `side` example (around line 368) so the demo shows two
ordered workspaces:

```toml
# A second workspace with no per-workspace sections — falls back to
# the global [sections.*] above.
[workspaces.side]
team_id = "T02XYZ"
order   = 2
```

- [ ] **Step 2: Add a short prose note immediately after that block**

Find the existing paragraph (around lines 378–383):

```markdown
Per-workspace `[workspaces.<slug>.sections.*]` blocks fully replace the
global `[sections.*]` for that workspace. Workspaces that define no
sections of their own fall back to the global table.

Legacy configs that key the block by raw team ID
(`[workspaces.T01ABCDEF]`) keep working unchanged.
```

Insert a new paragraph between these two:

```markdown
The `order` field controls workspace position in the rail and the
mapping for the `1`–`9` digit keys. Positive values sort ascending
(lowest first); workspaces without an `order` (or with `order = 0`)
sort after explicitly ordered ones, alphabetically by slug. Tokens
on disk that have no `[workspaces.<slug>]` block at all sort last,
alphabetically by team ID. The order is stable across runs.
```

- [ ] **Step 3: Verify**

```bash
git diff README.md
```

Confirm the diff matches the changes above.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document workspaces.<slug>.order config field"
```

---

## Final verification

- [ ] **Step 1: Run the full test suite one more time**

```bash
go test ./...
```

Expected: all PASS.

- [ ] **Step 2: Build the binary**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Inspect commit history**

```bash
git log --oneline feature/workspace-ordering ^main
```

Expected: four commits, one per task.
