# Inline Emoji Selector Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an inline `:shortcode:` emoji autocomplete dropdown above the compose box, using built-in emojis plus per-workspace custom emojis from Slack.

**Architecture:** Mirror the existing mention-picker pattern. New `internal/emoji.EmojiEntry` data type and `BuildEntries` function. New `internal/ui/emojipicker` package modeled on `internal/ui/mentionpicker`. Picker is owned by `compose.Model`; trigger detection and substring replacement live in compose. App routes keys when picker is active and stitches the picker view above compose. Custom emojis are fetched once per workspace via `emoji.list` (slack-go's `GetEmoji`), held in `cmd/slk.WorkspaceContext`, and pushed to compose on workspace ready/switch via existing `WorkspaceReadyMsg` / `WorkspaceSwitchedMsg`.

**Tech Stack:** Go 1.22+, charm.land bubbletea/v2, charm.land bubbles/v2 (textarea), charm.land lipgloss/v2, slack-go/slack v0.23, kyokomi/emoji/v2.

**Spec:** `docs/superpowers/specs/2026-04-28-inline-emoji-selector-design.md`

**Note on spec deviation:** The spec mentions `WorkspaceManager` as the holder of custom emojis. The actual per-workspace state holder in this codebase is `WorkspaceContext` in `cmd/slk/main.go` (it already holds `UserNames`, `Channels`, etc.). The plan plumbs custom emojis through `WorkspaceContext` and the existing `WorkspaceReadyMsg`/`WorkspaceSwitchedMsg` messages — same shape as how `UserNames` flows. `service.WorkspaceManager` is left untouched.

---

## File Structure

**Create:**
- `internal/emoji/entries.go` — `EmojiEntry` type + `BuildEntries` (pure data assembly, alias resolution).
- `internal/emoji/entries_test.go`.
- `internal/ui/emojipicker/model.go` — picker UI model (state, filter, render).
- `internal/ui/emojipicker/model_test.go`.

**Modify:**
- `internal/slack/client.go` — add `ListCustomEmoji` wrapper, add `GetEmoji` to `SlackAPI` interface.
- `internal/slack/client_test.go` — add `GetEmoji` to the mock; add tests for `ListCustomEmoji`.
- `cmd/slk/main.go` — add `CustomEmoji` field to `WorkspaceContext`; fetch on connect; plumb through `WorkspaceReadyMsg` / `WorkspaceSwitchedMsg`.
- `internal/ui/app.go` — extend `WorkspaceReadyMsg` / `WorkspaceSwitchedMsg` with `CustomEmoji`; add `SetCustomEmoji` method; extend `handleInsertMode` to forward keys when emoji picker active; stitch `EmojiPickerView` above compose in two render sites; close emoji picker on Escape.
- `internal/ui/compose/model.go` — add picker field, trigger detection (`:` + 2 chars at word boundary), `handleEmojiKey`, accept-replacement, and exports `IsEmojiActive()`, `CloseEmoji()`, `EmojiPickerView()`, `SetEmojiEntries()`.
- `internal/ui/compose/model_test.go` — trigger detection, accept, dismiss, mutual-exclusion-with-mention tests.

---

## Task 1: Add `ListCustomEmoji` wrapper to Slack client

**Files:**
- Modify: `internal/slack/client.go`
- Modify: `internal/slack/client_test.go`

- [ ] **Step 1: Write the failing test for `ListCustomEmoji`**

Append to `internal/slack/client_test.go`:

```go
func TestListCustomEmoji(t *testing.T) {
	mock := &mockSlackAPI{
		getEmojiFn: func() (map[string]string, error) {
			return map[string]string{
				"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif",
				"thumbsup_alt": "alias:thumbsup",
			}, nil
		},
	}
	client := &Client{api: mock}

	got, err := client.ListCustomEmoji(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 emojis, got %d", len(got))
	}
	if got["partyparrot"] != "https://emoji.slack-edge.com/T1/partyparrot/abc.gif" {
		t.Errorf("partyparrot URL wrong: %q", got["partyparrot"])
	}
	if got["thumbsup_alt"] != "alias:thumbsup" {
		t.Errorf("thumbsup_alt alias wrong: %q", got["thumbsup_alt"])
	}
}

func TestListCustomEmoji_Error(t *testing.T) {
	apiErr := errors.New("slack API unavailable")
	mock := &mockSlackAPI{
		getEmojiFn: func() (map[string]string, error) {
			return nil, apiErr
		},
	}
	client := &Client{api: mock}

	_, err := client.ListCustomEmoji(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, apiErr) {
		t.Errorf("expected wrapped apiErr, got: %v", err)
	}
}
```

Also extend `mockSlackAPI` in the same file to support the new `getEmojiFn` field and `GetEmoji` method. Modify the mock struct definition:

```go
type mockSlackAPI struct {
	getConversationRepliesFn func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	getEmojiFn               func() (map[string]string, error)
}
```

And add the method (anywhere in the file alongside the other mock methods):

```go
func (m *mockSlackAPI) GetEmoji() (map[string]string, error) {
	if m.getEmojiFn != nil {
		return m.getEmojiFn()
	}
	return nil, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/...`
Expected: compile error — `Client` has no `ListCustomEmoji` method, and `*mockSlackAPI` does not satisfy `SlackAPI` (missing `GetEmoji`).

- [ ] **Step 3: Add `GetEmoji` to the `SlackAPI` interface**

In `internal/slack/client.go`, add `GetEmoji() (map[string]string, error)` to the `SlackAPI` interface (the slack-go `*slack.Client` already has this method, so no additional implementation is needed for production code). Updated interface:

```go
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationsForUser(params *slack.GetConversationsForUserParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(ctx context.Context, options ...slack.GetUsersOption) ([]slack.User, error)
	GetUserInfo(user string) (*slack.User, error)
	GetEmoji() (map[string]string, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	AuthTest() (*slack.AuthTestResponse, error)
	JoinConversation(channelID string) (*slack.Channel, string, []string, error)
}
```

- [ ] **Step 4: Add the `ListCustomEmoji` method**

In `internal/slack/client.go`, append:

```go
// ListCustomEmoji fetches the workspace's custom emoji list via Slack's
// emoji.list API. Returns a map of emoji name -> URL or "alias:targetname".
// The map is empty if the workspace has no custom emojis.
func (c *Client) ListCustomEmoji(ctx context.Context) (map[string]string, error) {
	emojis, err := c.api.GetEmoji()
	if err != nil {
		return nil, fmt.Errorf("listing custom emoji: %w", err)
	}
	if emojis == nil {
		emojis = map[string]string{}
	}
	return emojis, nil
}
```

(The `ctx` parameter is currently unused because slack-go's `GetEmoji` is non-context, but accepting `ctx` keeps the call site uniform with `GetUsers(ctx)`, `GetHistory(ctx, ...)`, etc. and lets us swap in `GetEmojiContext` later without changing callers.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/slack/...`
Expected: PASS, including the two new tests and all existing client tests.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/client.go internal/slack/client_test.go
git commit -m "feat(slack): add ListCustomEmoji wrapper for emoji.list"
```

---

## Task 2: Add `EmojiEntry` and `BuildEntries` (pure data assembly)

**Files:**
- Create: `internal/emoji/entries.go`
- Create: `internal/emoji/entries_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/emoji/entries_test.go`:

```go
package emoji

import (
	"strings"
	"testing"
)

func TestBuildEntries_BuiltinsHaveTrimmedDisplay(t *testing.T) {
	entries := BuildEntries(nil)
	// Find :rocket:
	var rocket *EmojiEntry
	for i := range entries {
		if entries[i].Name == "rocket" {
			rocket = &entries[i]
			break
		}
	}
	if rocket == nil {
		t.Fatal("expected :rocket: among built-in entries")
	}
	if rocket.Display == "" {
		t.Error("expected non-empty display for :rocket:")
	}
	if strings.HasSuffix(rocket.Display, " ") {
		t.Errorf("display should be trimmed, got %q", rocket.Display)
	}
}

func TestBuildEntries_AlphabeticalOrder(t *testing.T) {
	entries := BuildEntries(nil)
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Name > entries[i].Name {
			t.Fatalf("entries not sorted: %q before %q", entries[i-1].Name, entries[i].Name)
		}
	}
}

func TestBuildEntries_AliasToBuiltinResolves(t *testing.T) {
	customs := map[string]string{
		"thumbsup_alt": "alias:+1",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "thumbsup_alt")
	if got == nil {
		t.Fatal("expected thumbsup_alt entry")
	}
	// :+1: is "👍" in kyokomi codemap. We don't hard-code the glyph, just
	// require non-empty and not the placeholder.
	if got.Display == "" || got.Display == placeholderGlyph {
		t.Errorf("expected resolved glyph, got %q", got.Display)
	}
}

func TestBuildEntries_AliasChained(t *testing.T) {
	customs := map[string]string{
		"a": "alias:b",
		"b": "alias:c",
		"c": "alias:+1",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "a")
	if got == nil {
		t.Fatal("expected entry a")
	}
	if got.Display == placeholderGlyph || got.Display == "" {
		t.Errorf("expected chained alias to resolve, got %q", got.Display)
	}
}

func TestBuildEntries_AliasCycleFallsBackToPlaceholder(t *testing.T) {
	customs := map[string]string{
		"a": "alias:b",
		"b": "alias:a",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "a")
	if got == nil {
		t.Fatal("expected entry a")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected placeholder for cycle, got %q", got.Display)
	}
}

func TestBuildEntries_AliasToUnknownFallsBackToPlaceholder(t *testing.T) {
	customs := map[string]string{
		"orphan": "alias:nonexistent_emoji_name_xyz",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "orphan")
	if got == nil {
		t.Fatal("expected entry orphan")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected placeholder for unknown alias target, got %q", got.Display)
	}
}

func TestBuildEntries_URLCustomUsesPlaceholder(t *testing.T) {
	customs := map[string]string{
		"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "partyparrot")
	if got == nil {
		t.Fatal("expected entry partyparrot")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected placeholder for URL-backed custom, got %q", got.Display)
	}
}

func TestBuildEntries_CustomShadowsBuiltin(t *testing.T) {
	customs := map[string]string{
		"rocket": "https://emoji.slack-edge.com/T1/rocket/xyz.gif",
	}
	entries := BuildEntries(customs)
	got := findEntry(entries, "rocket")
	if got == nil {
		t.Fatal("expected entry rocket")
	}
	if got.Display != placeholderGlyph {
		t.Errorf("expected custom rocket to shadow built-in (placeholder), got %q", got.Display)
	}
	// And there should be exactly one :rocket: entry, not two.
	count := 0
	for _, e := range entries {
		if e.Name == "rocket" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 :rocket: entry after dedupe, got %d", count)
	}
}

func findEntry(entries []EmojiEntry, name string) *EmojiEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestBuildEntries -v`
Expected: FAIL with "undefined: EmojiEntry" / "undefined: BuildEntries" / "undefined: placeholderGlyph".

- [ ] **Step 3: Implement `entries.go`**

Create `internal/emoji/entries.go`:

```go
package emoji

import (
	"sort"
	"strings"

	kyoemoji "github.com/kyokomi/emoji/v2"
)

// placeholderGlyph is the single-cell stand-in for image-backed custom
// emojis (which have no displayable Unicode form).
const placeholderGlyph = "□"

// aliasPrefix marks an alias-style custom emoji value, e.g. "alias:thumbsup".
const aliasPrefix = "alias:"

// maxAliasHops caps recursion when resolving chained aliases.
const maxAliasHops = 4

// EmojiEntry is one row in the inline emoji selector.
//
// Name is the shortcode without surrounding colons (e.g. "rocket").
// Display is a single-grapheme preview cell rendered next to the name.
// For built-in and alias-resolved emojis this is the Unicode glyph; for
// image-backed custom emojis it is placeholderGlyph.
type EmojiEntry struct {
	Name    string
	Display string
}

// BuildEntries assembles the searchable emoji list from the kyokomi
// built-in codemap plus the workspace's custom emoji map (as returned by
// Slack's emoji.list, name -> URL-or-"alias:target"). The result is
// deduped (custom shadows built-in) and sorted alphabetically by name.
//
// Pass nil customs for built-ins only.
func BuildEntries(customs map[string]string) []EmojiEntry {
	codemap := kyoemoji.CodeMap()
	byName := make(map[string]EmojiEntry, len(codemap)+len(customs))

	// Built-ins. CodeMap keys are like ":rocket:" and values include a
	// trailing space (kyokomi convention); strip both.
	for code, glyph := range codemap {
		name := strings.Trim(code, ":")
		if name == "" {
			continue
		}
		byName[name] = EmojiEntry{
			Name:    name,
			Display: strings.TrimSpace(glyph),
		}
	}

	// Customs override built-ins of the same name.
	for name, value := range customs {
		if name == "" {
			continue
		}
		byName[name] = EmojiEntry{
			Name:    name,
			Display: resolveCustomDisplay(name, value, customs, codemap),
		}
	}

	out := make([]EmojiEntry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// resolveCustomDisplay returns the preview glyph for a custom emoji.
// Alias chains are followed up to maxAliasHops; cycles, dead ends, and
// URL-backed customs all fall back to placeholderGlyph.
func resolveCustomDisplay(name, value string, customs, codemap map[string]string) string {
	if !strings.HasPrefix(value, aliasPrefix) {
		// URL-backed (or anything else we don't understand).
		return placeholderGlyph
	}

	visited := map[string]bool{name: true}
	target := strings.TrimPrefix(value, aliasPrefix)

	for hops := 0; hops < maxAliasHops; hops++ {
		if target == "" {
			return placeholderGlyph
		}
		// Try built-in codemap first (most aliases point at built-ins).
		if glyph, ok := codemap[":"+target+":"]; ok {
			return strings.TrimSpace(glyph)
		}
		// Then chained custom alias.
		next, ok := customs[target]
		if !ok {
			return placeholderGlyph
		}
		if visited[target] {
			return placeholderGlyph // cycle
		}
		visited[target] = true
		if !strings.HasPrefix(next, aliasPrefix) {
			// Chain terminates at a URL-backed custom.
			return placeholderGlyph
		}
		target = strings.TrimPrefix(next, aliasPrefix)
	}
	return placeholderGlyph
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/emoji/ -run TestBuildEntries -v`
Expected: PASS for all 8 tests.

- [ ] **Step 5: Run the full emoji package tests to confirm no regression**

Run: `go test ./internal/emoji/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/emoji/entries.go internal/emoji/entries_test.go
git commit -m "feat(emoji): add EmojiEntry and BuildEntries with alias resolution"
```

---

## Task 3: Create `emojipicker` package

**Files:**
- Create: `internal/ui/emojipicker/model.go`
- Create: `internal/ui/emojipicker/model_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/ui/emojipicker/model_test.go`:

```go
package emojipicker

import (
	"testing"

	"github.com/gammons/slk/internal/emoji"
)

func sampleEntries() []emoji.EmojiEntry {
	return []emoji.EmojiEntry{
		{Name: "apple", Display: "🍎"},
		{Name: "rocket", Display: "🚀"},
		{Name: "rock", Display: "🪨"},
		{Name: "rose", Display: "🌹"},
		{Name: "zebra", Display: "🦓"},
	}
}

func TestOpenClose(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.IsVisible() {
		t.Fatal("expected not visible initially")
	}
	m.Open("ro")
	if !m.IsVisible() {
		t.Fatal("expected visible after Open")
	}
	m.Close()
	if m.IsVisible() {
		t.Fatal("expected not visible after Close")
	}
}

func TestPrefixFilterCaseInsensitive(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("RO")
	got := m.Filtered()
	wantNames := []string{"rock", "rocket", "rose"}
	if len(got) != len(wantNames) {
		t.Fatalf("expected %d filtered, got %d", len(wantNames), len(got))
	}
	for i, n := range wantNames {
		if got[i].Name != n {
			t.Errorf("filtered[%d] = %q, want %q", i, got[i].Name, n)
		}
	}
}

func TestEmptyQueryShowsFirstN(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("")
	if len(m.Filtered()) != len(sampleEntries()) {
		// MaxVisible=5 and we provided exactly 5 entries.
		t.Errorf("expected all entries visible, got %d", len(m.Filtered()))
	}
}

func TestMoveUpDownClamps(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro") // 3 results: rock, rocket, rose
	if m.Selected() != 0 {
		t.Errorf("initial selected = %d, want 0", m.Selected())
	}
	m.MoveDown()
	m.MoveDown()
	m.MoveDown() // clamp at 2
	if m.Selected() != 2 {
		t.Errorf("after 3 down on 3 items, selected = %d, want 2", m.Selected())
	}
	m.MoveUp()
	m.MoveUp()
	m.MoveUp() // clamp at 0
	if m.Selected() != 0 {
		t.Errorf("after 3 up, selected = %d, want 0", m.Selected())
	}
}

func TestSelectedEntry(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.MoveDown() // rocket
	got, ok := m.SelectedEntry()
	if !ok {
		t.Fatal("expected selectedEntry ok=true")
	}
	if got.Name != "rocket" {
		t.Errorf("selected = %q, want rocket", got.Name)
	}
}

func TestSelectedEntryEmpty(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("zzz") // no matches
	if got, ok := m.SelectedEntry(); ok {
		t.Errorf("expected ok=false, got %+v", got)
	}
}

func TestSetEntriesWhileVisibleClampsSelection(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.MoveDown()
	m.MoveDown() // selected=2 (rose)
	// Now restrict to a smaller list.
	m.SetEntries([]emoji.EmojiEntry{
		{Name: "rocket", Display: "🚀"},
	})
	got := m.Filtered()
	if len(got) != 1 {
		t.Fatalf("expected 1 filtered, got %d", len(got))
	}
	if m.Selected() != 0 {
		t.Errorf("expected selection clamped to 0, got %d", m.Selected())
	}
}

func TestSetQueryUpdatesFilter(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	m.SetQuery("ros")
	got := m.Filtered()
	if len(got) != 1 || got[0].Name != "rose" {
		t.Errorf("expected only rose, got %+v", got)
	}
	if m.Selected() != 0 {
		t.Errorf("selection should reset on SetQuery, got %d", m.Selected())
	}
}

func TestViewEmptyWhenInvisible(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	if m.View(40) != "" {
		t.Error("expected empty view when not visible")
	}
}

func TestViewNonEmptyWhenVisibleWithMatches(t *testing.T) {
	m := New()
	m.SetEntries(sampleEntries())
	m.Open("ro")
	if m.View(40) == "" {
		t.Error("expected non-empty view with matches")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/emojipicker/ -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Implement the picker**

Create `internal/ui/emojipicker/model.go`:

```go
package emojipicker

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/styles"
)

// MaxVisible matches mentionpicker.MaxVisible for UX symmetry.
const MaxVisible = 5

type Model struct {
	entries  []emoji.EmojiEntry
	filtered []emoji.EmojiEntry
	query    string
	selected int
	visible  bool
}

func New() Model { return Model{} }

// SetEntries replaces the full entry list. If the picker is visible, the
// filtered list and selection are recomputed against the current query.
func (m *Model) SetEntries(entries []emoji.EmojiEntry) {
	m.entries = entries
	if m.visible {
		m.filter()
	}
}

func (m *Model) Open(query string) {
	m.visible = true
	m.query = query
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

func (m *Model) IsVisible() bool { return m.visible }

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m *Model) Query() string { return m.query }

func (m *Model) Filtered() []emoji.EmojiEntry { return m.filtered }

func (m *Model) Selected() int { return m.selected }

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

// SelectedEntry returns the currently highlighted entry. ok=false if the
// filtered list is empty.
func (m *Model) SelectedEntry() (emoji.EmojiEntry, bool) {
	if len(m.filtered) == 0 {
		return emoji.EmojiEntry{}, false
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return emoji.EmojiEntry{}, false
	}
	return m.filtered[m.selected], true
}

func (m *Model) filter() {
	q := strings.ToLower(m.query)
	var results []emoji.EmojiEntry
	for _, e := range m.entries {
		if q == "" || strings.HasPrefix(strings.ToLower(e.Name), q) {
			results = append(results, e)
			if len(results) >= MaxVisible {
				break
			}
		}
	}
	m.filtered = results
	if m.selected >= len(m.filtered) {
		m.selected = 0
		if len(m.filtered) > 0 {
			m.selected = len(m.filtered) - 1
		}
	}
}

// View renders the bordered dropdown. Returns "" when not visible OR when
// there are no matches (caller already shows the textarea below).
func (m Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	// Compute the widest display preview so name columns line up.
	previewWidth := 1
	for _, e := range m.filtered {
		w := lipgloss.Width(e.Display)
		if w > previewWidth {
			previewWidth = w
		}
	}

	var rows []string
	for i, e := range m.filtered {
		indicator := "  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		if i == m.selected {
			indicator = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌ ")
			nameStyle = nameStyle.Bold(true)
		}
		// Pad preview cell so all names start at the same column.
		pad := previewWidth - lipgloss.Width(e.Display)
		if pad < 0 {
			pad = 0
		}
		preview := e.Display + strings.Repeat(" ", pad)
		row := fmt.Sprintf("%s%s  %s", indicator, preview, nameStyle.Render(":"+e.Name+":"))
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Background(styles.SurfaceDark).
		Width(width - 2).
		Render(content)
	return box
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/emojipicker/ -v`
Expected: PASS for all 11 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/emojipicker/
git commit -m "feat(emojipicker): inline dropdown UI for emoji shortcode autocomplete"
```

---

## Task 4: Wire the emoji picker into `compose.Model`

**Files:**
- Modify: `internal/ui/compose/model.go`
- Modify: `internal/ui/compose/model_test.go`

- [ ] **Step 1: Write failing tests for trigger detection and accept**

Append to `internal/ui/compose/model_test.go`:

```go
import (
	// existing imports...
	"github.com/gammons/slk/internal/emoji"
)

func sampleEmojiEntries() []emoji.EmojiEntry {
	return []emoji.EmojiEntry{
		{Name: "rocket", Display: "🚀"},
		{Name: "rock", Display: "🪨"},
		{Name: "rose", Display: "🌹"},
		{Name: "tada", Display: "🎉"},
	}
}

// typeChars feeds each rune in s through the compose Update loop as a
// character key press. This mirrors how the textarea receives input from
// bubbletea in real usage.
func typeChars(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func TestEmojiTrigger_OpensAfterColonAndTwoChars(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":")
	if m.IsEmojiActive() {
		t.Fatal("picker should NOT open with just ':'")
	}
	m = typeChars(t, m, "r")
	if m.IsEmojiActive() {
		t.Fatal("picker should NOT open with ':r' (1 char)")
	}
	m = typeChars(t, m, "o")
	if !m.IsEmojiActive() {
		t.Fatal("picker should open with ':ro'")
	}
}

func TestEmojiTrigger_RequiresWordBoundary(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, "foo:ro")
	if m.IsEmojiActive() {
		t.Errorf("picker should not open mid-word: value=%q", m.Value())
	}
}

func TestEmojiTrigger_OpensAfterSpace(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, "hi :ro")
	if !m.IsEmojiActive() {
		t.Error("picker should open after whitespace")
	}
}

func TestEmojiTrigger_ClosesOnSpace(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m = typeChars(t, m, " ")
	if m.IsEmojiActive() {
		t.Error("picker should close on space")
	}
}

func TestEmojiTrigger_ClosesOnSecondColon(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m = typeChars(t, m, ":")
	if m.IsEmojiActive() {
		t.Error("picker should close on closing ':'")
	}
}

func TestEmojiTrigger_ClosesOnEscape(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.IsEmojiActive() {
		t.Error("picker should close on escape")
	}
}

func TestEmojiTrigger_BackspacePastTriggerCloses(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	// Backspace 3 times: deletes 'o', 'r', then ':' (cursor crosses trigger).
	for i := 0; i < 3; i++ {
		m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if m.IsEmojiActive() {
		t.Error("picker should close once cursor crosses the trigger ':'")
	}
}

func TestEmojiAccept_ReplacesQueryWithFullShortcode(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, ":ro")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	// First filtered match for "ro" against sampleEmojiEntries is :rock:
	// (alphabetical), then :rocket:, then :rose:. Press Enter to accept the default.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.IsEmojiActive() {
		t.Error("picker should be closed after accept")
	}
	if got := m.Value(); got != ":rock:" {
		t.Errorf("expected value=:rock:, got %q", got)
	}
}

func TestEmojiAccept_PreservesSurroundingText(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	_ = m.Focus()

	m = typeChars(t, m, "hi :ros")
	if !m.IsEmojiActive() {
		t.Fatal("precondition: picker open")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.Value(); got != "hi :rose:" {
		t.Errorf("expected 'hi :rose:', got %q", got)
	}
}

func TestEmojiAndMentionPickersAreMutuallyExclusive(t *testing.T) {
	m := New("general")
	m.SetEmojiEntries(sampleEmojiEntries())
	m.SetUsers([]mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice"}})
	_ = m.Focus()

	m = typeChars(t, m, "@a")
	if !m.IsMentionActive() {
		t.Fatal("precondition: mention picker open")
	}
	if m.IsEmojiActive() {
		t.Error("emoji picker should not be active when mention is")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/compose/ -run Emoji -v`
Expected: FAIL — `SetEmojiEntries`, `IsEmojiActive` undefined.

- [ ] **Step 3: Add picker fields to `compose.Model`**

In `internal/ui/compose/model.go`, add to the imports:

```go
"github.com/gammons/slk/internal/emoji"
"github.com/gammons/slk/internal/ui/emojipicker"
```

Modify the `Model` struct to add new fields after the mention fields (around line 26):

```go
type Model struct {
	input       textarea.Model
	channelName string
	width       int

	// Mention picker state
	mentionPicker   mentionpicker.Model
	mentionActive   bool
	mentionStartCol int
	users           []mentionpicker.User
	reverseNames    map[string]string

	// Emoji picker state. emojiStartCol is the byte offset of the trigger ':'
	// within input.Value(). emojiActiveLine is the logical line index where
	// the trigger lives; if the cursor moves to a different line the picker
	// closes.
	emojiPicker     emojipicker.Model
	emojiActive     bool
	emojiStartCol   int
	emojiActiveLine int

	version int64
}
```

- [ ] **Step 4: Add public API for the picker**

Append to `internal/ui/compose/model.go`:

```go
// SetEmojiEntries provides the searchable emoji list (built-ins + workspace
// customs). Safe to call any time, including while the picker is visible.
func (m *Model) SetEmojiEntries(entries []emoji.EmojiEntry) {
	m.emojiPicker.SetEntries(entries)
	m.dirty()
}

// IsEmojiActive returns whether the emoji picker is currently showing.
func (m Model) IsEmojiActive() bool { return m.emojiActive }

// CloseEmoji dismisses the emoji picker without selecting.
func (m *Model) CloseEmoji() {
	m.emojiActive = false
	m.emojiPicker.Close()
	m.dirty()
}

// EmojiPickerView returns the rendered emoji picker dropdown, or "" if not active.
func (m Model) EmojiPickerView(width int) string {
	if !m.emojiActive {
		return ""
	}
	return m.emojiPicker.View(width)
}
```

- [ ] **Step 5: Update `Reset` to also close the emoji picker**

Modify `Reset` (around line 122):

```go
func (m *Model) Reset() {
	m.input.Reset()
	m.input.SetHeight(1)
	m.mentionActive = false
	m.mentionPicker.Close()
	m.emojiActive = false
	m.emojiPicker.Close()
	m.dirty()
}
```

- [ ] **Step 6: Wire trigger detection and key routing into `Update`**

Replace the existing `Update` method (around line 167) with:

```go
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)

	// If emoji picker is active, intercept keys (takes precedence over mention).
	if m.emojiActive && isKey {
		return m.handleEmojiKey(keyMsg)
	}
	// If mention picker is active, intercept keys.
	if m.mentionActive && isKey {
		return m.handleMentionKey(keyMsg)
	}

	// Normal textarea update
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Mention trigger: '@' at word boundary.
	if isKey && keyMsg.Key().Text == "@" {
		val := m.input.Value()
		cursorAbsPos := m.cursorPosition()
		atPos := cursorAbsPos - 1
		if atPos >= 0 && atPos < len(val) && val[atPos] == '@' {
			if atPos == 0 || val[atPos-1] == ' ' || val[atPos-1] == '\n' {
				m.mentionActive = true
				m.mentionStartCol = cursorAbsPos
				m.mentionPicker.Open()
			}
		}
	}

	// Emoji trigger: ':' at word boundary, plus 2 query chars before the
	// popup opens. We re-check on every keystroke (cheap) so the popup
	// appears the moment the threshold is hit.
	m.maybeOpenEmojiPicker()

	m.autoGrow()
	m.dirty()
	return m, cmd
}
```

- [ ] **Step 7: Implement `maybeOpenEmojiPicker` and `handleEmojiKey`**

Append to `internal/ui/compose/model.go`:

```go
// emojiQueryChar reports whether r is a valid character inside an emoji
// shortcode query (the run of chars after ':' the user is currently typing).
// Mirrors the character set kyokomi recognizes in shortcodes.
func emojiQueryChar(r byte) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '_' || r == '+' || r == '-':
		return true
	}
	return false
}

// maybeOpenEmojiPicker scans backward from the cursor to find an emoji
// trigger of the form `:xy` (at start-of-line or after whitespace, with
// at least 2 valid query characters and no closing ':' yet). Opens the
// picker if the threshold is met; updates the query if already open.
func (m *Model) maybeOpenEmojiPicker() {
	val := m.input.Value()
	pos := m.cursorPosition()
	if pos > len(val) {
		pos = len(val)
	}

	// Walk backward from the cursor over query chars to find the trigger ':'.
	i := pos
	for i > 0 && emojiQueryChar(val[i-1]) {
		i--
	}
	// Now val[i:pos] is the candidate query. We need val[i-1] == ':' and
	// either i-1 == 0 or val[i-2] is whitespace.
	if i == 0 || val[i-1] != ':' {
		// No trigger; if we had one open, close it (cursor moved off).
		if m.emojiActive {
			m.emojiActive = false
			m.emojiPicker.Close()
		}
		return
	}
	if i-1 != 0 {
		prev := val[i-2]
		if prev != ' ' && prev != '\t' && prev != '\n' {
			if m.emojiActive {
				m.emojiActive = false
				m.emojiPicker.Close()
			}
			return
		}
	}
	query := val[i:pos]
	if len(query) < 2 {
		// Below threshold; close if open.
		if m.emojiActive {
			m.emojiActive = false
			m.emojiPicker.Close()
		}
		return
	}

	if !m.emojiActive {
		m.emojiActive = true
		m.emojiStartCol = i // byte offset of ':' is i-1; we store the col AFTER the colon (matches mentionStartCol semantics)
		m.emojiActiveLine = m.input.Line()
		m.emojiPicker.Open(query)
	} else {
		m.emojiPicker.SetQuery(query)
	}
}

// handleEmojiKey processes key events when the emoji picker is active.
// Mirrors handleMentionKey.
func (m Model) handleEmojiKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.Key()
	switch {
	case k.Code == tea.KeyUp || (k.Code == 'p' && k.Mod == tea.ModCtrl):
		m.emojiPicker.MoveUp()
		return m, nil

	case k.Code == tea.KeyDown || (k.Code == 'n' && k.Mod == tea.ModCtrl):
		m.emojiPicker.MoveDown()
		return m, nil

	case k.Code == tea.KeyEnter || k.Code == tea.KeyTab:
		if entry, ok := m.emojiPicker.SelectedEntry(); ok {
			m.insertEmoji(entry.Name)
		}
		m.emojiActive = false
		m.emojiPicker.Close()
		m.autoGrow()
		return m, nil

	case k.Code == tea.KeyEscape:
		m.emojiActive = false
		m.emojiPicker.Close()
		return m, nil

	case k.Code == tea.KeyBackspace:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.maybeOpenEmojiPicker()
		m.autoGrow()
		return m, cmd

	case len(k.Text) > 0:
		// If the user types a non-query char (space, ':', punctuation), let
		// the textarea record it, then close the picker.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// A single rune in k.Text — check the first byte for ASCII set.
		ch := k.Text[0]
		if !emojiQueryChar(ch) {
			m.emojiActive = false
			m.emojiPicker.Close()
		} else {
			m.maybeOpenEmojiPicker()
		}
		m.autoGrow()
		return m, cmd

	default:
		m.emojiActive = false
		m.emojiPicker.Close()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.autoGrow()
		return m, cmd
	}
}

// insertEmoji replaces the in-progress :query (the bytes from the trigger
// ':' through the cursor) with `:name:`.
func (m *Model) insertEmoji(name string) {
	val := m.input.Value()
	pos := m.cursorPosition()
	colonPos := m.emojiStartCol - 1 // byte offset of the trigger ':'
	if colonPos < 0 {
		colonPos = 0
	}
	if pos > len(val) {
		pos = len(val)
	}
	before := val[:colonPos]
	after := ""
	if pos < len(val) {
		after = val[pos:]
	}
	newText := before + ":" + name + ":" + after
	m.input.SetValue(newText)
}
```

Note on `emojiStartCol` semantics: it stores the byte offset of the first character AFTER the trigger `:` (i.e. the start of the query), matching `mentionStartCol`. The trigger `:` itself sits at `emojiStartCol - 1`. `insertEmoji` uses `colonPos = emojiStartCol - 1` to splice from the colon through the cursor.

- [ ] **Step 8: Run the new tests**

Run: `go test ./internal/ui/compose/ -run Emoji -v`
Expected: PASS for all 9 emoji tests.

- [ ] **Step 9: Run the full compose tests to confirm no regression**

Run: `go test ./internal/ui/compose/...`
Expected: PASS for all tests (existing mention/translate tests untouched).

- [ ] **Step 10: Build the whole module to confirm no other package broke**

Run: `go build ./...`
Expected: success.

- [ ] **Step 11: Commit**

```bash
git add internal/ui/compose/model.go internal/ui/compose/model_test.go
git commit -m "feat(compose): inline emoji autocomplete with :shortcode: trigger"
```

---

## Task 5: Wire compose's emoji picker into `App`

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add `SetCustomEmoji` method on App**

The plumbing flow: cmd/slk fetches custom emoji → puts on `WorkspaceContext` → sends through `WorkspaceReadyMsg` / `WorkspaceSwitchedMsg` → app handler calls `SetCustomEmoji`.

In `internal/ui/app.go`, find the `WorkspaceSwitchedMsg` and `WorkspaceReadyMsg` struct definitions (lines 104 and 116) and add a `CustomEmoji` field to each:

```go
WorkspaceSwitchedMsg struct {
	TeamID      string
	TeamName    string
	Channels    []sidebar.ChannelItem
	FinderItems []channelfinder.Item
	UserNames   map[string]string
	UserID      string
	CustomEmoji map[string]string
}
```

```go
WorkspaceReadyMsg struct {
	TeamID      string
	TeamName    string
	Channels    []sidebar.ChannelItem
	FinderItems []channelfinder.Item
	UserNames   map[string]string
	UserID      string
	CustomEmoji map[string]string
}
```

Also add the imports for the new packages near the top of the file (find the existing import block; add):

```go
"github.com/gammons/slk/internal/emoji"
```

Add a new method (placed near the existing `SetUserNames` method around line 1675):

```go
// SetCustomEmoji rebuilds the emoji entry list (built-ins + the active
// workspace's customs) and pushes it into both compose boxes.
func (a *App) SetCustomEmoji(customs map[string]string) {
	entries := emoji.BuildEntries(customs)
	a.compose.SetEmojiEntries(entries)
	a.threadCompose.SetEmojiEntries(entries)
}
```

Call it from the two existing handlers. In the `WorkspaceReadyMsg` case (around line 647), inside the `if a.activeChannelID == ""` block, after `a.SetUserNames(msg.UserNames)`:

```go
a.SetCustomEmoji(msg.CustomEmoji)
```

In the `WorkspaceSwitchedMsg` case (around line 606), after `a.SetUserNames(msg.UserNames)`:

```go
a.SetCustomEmoji(msg.CustomEmoji)
```

Also call it once at startup so the picker has built-ins available even before any workspace ready msg arrives. Find the `New` function (search for `func New(` in app.go) — at the end of `New`, before the return, add:

```go
// Seed the picker with built-in emojis so the autocomplete works even
// before the first workspace finishes loading customs.
app.compose.SetEmojiEntries(emoji.BuildEntries(nil))
app.threadCompose.SetEmojiEntries(emoji.BuildEntries(nil))
```

(If `app` is named differently in `New` — e.g. `a := &App{...}; return a` — adjust the variable name. Check first by reading the function.)

- [ ] **Step 2: Update `handleInsertMode` to forward keys when emoji picker is active**

In `internal/ui/app.go`, modify `handleInsertMode` (line 881). The Escape branch must also close the emoji picker first:

Replace lines 882-895 (the Escape block) with:

```go
	if key.Matches(msg, a.keys.Escape) {
		// If a picker is active, close it instead of exiting insert mode.
		if a.focusedPanel == PanelThread && a.threadVisible {
			if a.threadCompose.IsEmojiActive() {
				a.threadCompose.CloseEmoji()
				return nil
			}
			if a.threadCompose.IsMentionActive() {
				a.threadCompose.CloseMention()
				return nil
			}
		} else {
			if a.compose.IsEmojiActive() {
				a.compose.CloseEmoji()
				return nil
			}
			if a.compose.IsMentionActive() {
				a.compose.CloseMention()
				return nil
			}
		}
		a.SetMode(ModeNormal)
		a.compose.Blur()
		a.threadCompose.Blur()
		return nil
	}
```

Then extend the two key-forwarding branches. Find the thread-compose branch (around line 904):

```go
		// If mention picker is active, forward all keys to compose (including Enter)
		if a.threadCompose.IsMentionActive() {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			return cmd
		}
```

Replace with:

```go
		// If a picker is active, forward all keys to compose (including Enter).
		if a.threadCompose.IsEmojiActive() || a.threadCompose.IsMentionActive() {
			var cmd tea.Cmd
			a.threadCompose, cmd = a.threadCompose.Update(msg)
			return cmd
		}
```

Find the channel-compose branch (around line 941) and apply the same change:

```go
	// If a picker is active, forward all keys to compose (including Enter).
	if a.compose.IsEmojiActive() || a.compose.IsMentionActive() {
		var cmd tea.Cmd
		a.compose, cmd = a.compose.Update(msg)
		return cmd
	}
```

- [ ] **Step 3: Stitch the emoji picker view above the compose box**

Find the channel-message render block (around line 1998). Replace:

```go
		composeView := a.compose.View(msgWidth-2, composeFocused)
		mentionView := a.compose.MentionPickerView(msgWidth - 2)
		if mentionView != "" {
			composeView = mentionView + "\n" + composeView
		}
```

With:

```go
		composeView := a.compose.View(msgWidth-2, composeFocused)
		// Inline pickers stack above the compose box. Both should never be
		// visible simultaneously (mutually exclusive in compose.Update);
		// emoji wins if somehow both are.
		if pickerView := a.compose.EmojiPickerView(msgWidth - 2); pickerView != "" {
			composeView = pickerView + "\n" + composeView
		} else if mentionView := a.compose.MentionPickerView(msgWidth - 2); mentionView != "" {
			composeView = mentionView + "\n" + composeView
		}
```

Find the thread render block (around line 2050) and apply the same change:

```go
			threadComposeView := a.threadCompose.View(threadWidth-2, threadComposeFocused)
			if pickerView := a.threadCompose.EmojiPickerView(threadWidth - 2); pickerView != "" {
				threadComposeView = pickerView + "\n" + threadComposeView
			} else if threadMentionView := a.threadCompose.MentionPickerView(threadWidth - 2); threadMentionView != "" {
				threadComposeView = threadMentionView + "\n" + threadMentionView
			}
```

Wait — that last line had a typo in the original style. Use this corrected version:

```go
			threadComposeView := a.threadCompose.View(threadWidth-2, threadComposeFocused)
			if pickerView := a.threadCompose.EmojiPickerView(threadWidth - 2); pickerView != "" {
				threadComposeView = pickerView + "\n" + threadComposeView
			} else if mentionView := a.threadCompose.MentionPickerView(threadWidth - 2); mentionView != "" {
				threadComposeView = mentionView + "\n" + threadComposeView
			}
```

- [ ] **Step 4: Build to confirm everything compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS. The app_test.go test that calls `SetUserNames` is unaffected since `SetCustomEmoji` is a separate call.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(app): route emoji picker keys and stitch dropdown above compose"
```

---

## Task 6: Fetch custom emojis on workspace connect

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Add `CustomEmoji` field to `WorkspaceContext`**

In `cmd/slk/main.go`, modify the `WorkspaceContext` struct (around line 40):

```go
type WorkspaceContext struct {
	Client      *slackclient.Client
	ConnMgr     *slackclient.ConnectionManager
	RTMHandler  *rtmEventHandler
	UserNames   map[string]string
	LastReadMap map[string]string
	Channels    []sidebar.ChannelItem
	FinderItems   []channelfinder.Item
	TeamID        string
	TeamName      string
	UserID        string
	UnresolvedDMs []UnresolvedDM
	CustomEmoji   map[string]string // emoji name -> URL or "alias:target"
}
```

- [ ] **Step 2: Initialize `CustomEmoji` and kick off background fetch in `connectWorkspace`**

In `cmd/slk/main.go`, in `connectWorkspace` (around line 495), modify the `wctx := &WorkspaceContext{...}` block to initialize the new map:

```go
	wctx := &WorkspaceContext{
		Client:      client,
		TeamID:      client.TeamID(),
		TeamName:    token.TeamName,
		UserID:      client.UserID(),
		UserNames:   make(map[string]string),
		LastReadMap: make(map[string]string),
		CustomEmoji: make(map[string]string),
	}
```

Then after the existing background-user-fetch goroutine (it ends around line 545), add a parallel goroutine for emojis. The fetch is best-effort: if it fails or returns late, the picker simply uses built-ins until it succeeds. Insert the following after the user-fetch goroutine:

```go
	// Background custom-emoji fetch. Best-effort: failure leaves the picker
	// using built-ins only.
	go func() {
		emojis, err := client.ListCustomEmoji(ctx)
		if err != nil {
			return
		}
		wctx.CustomEmoji = emojis
	}()
```

Note: there's no notification message for "customs arrived later" in v1; if the user is fast enough to open the picker before the goroutine completes, they get built-ins only on first open. Subsequent workspace switches and the next workspace-ready message will pick up the populated map. This matches the spec's "fetched on connect, in-memory" decision.

- [ ] **Step 3: Pass `CustomEmoji` through `WorkspaceReadyMsg` and `WorkspaceSwitchedMsg`**

In `cmd/slk/main.go`, find the `p.Send(ui.WorkspaceReadyMsg{...})` site (around line 452). Add the field:

```go
			p.Send(ui.WorkspaceReadyMsg{
				TeamID:      wctx.TeamID,
				TeamName:    wctx.TeamName,
				Channels:    wctx.Channels,
				FinderItems: wctx.FinderItems,
				UserNames:   wctx.UserNames,
				UserID:      wctx.UserID,
				CustomEmoji: wctx.CustomEmoji,
			})
```

Then find the `return ui.WorkspaceSwitchedMsg{...}` site (around line 391) and add the field there too:

```go
		return ui.WorkspaceSwitchedMsg{
			TeamID:      wctx.TeamID,
			TeamName:    wctx.TeamName,
			Channels:    wctx.Channels,
			FinderItems: wctx.FinderItems,
			UserNames:   wctx.UserNames,
			UserID:      wctx.UserID,
			CustomEmoji: wctx.CustomEmoji,
		}
```

- [ ] **Step 4: Build the whole module**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(slk): fetch workspace custom emojis on connect for picker"
```

---

## Task 7: Manual smoke test

This step is verification only; no code changes. Run from the repo root.

- [ ] **Step 1: Build a fresh binary**

Run: `make build`
Expected: `bin/slk` produced without errors.

- [ ] **Step 2: Open the TUI against an existing workspace**

Run: `./bin/slk`

- [ ] **Step 3: Verify built-in trigger**

In any channel, press `i` to enter insert mode, type `:ro` — the popup appears above the compose box with `:rock:`, `:rocket:`, `:rose:` (depending on what's in kyokomi's codemap). Press `↓` once, then Enter — the textarea now contains `:rocket:`. Press Enter again to send. Verify the message appears with the rocket emoji rendered.

- [ ] **Step 4: Verify dismiss paths**

Type `:ro`, then:
- Press space — popup closes, textarea contains `:ro `.
- Type `:ro` again, press Esc — popup closes, textarea contains `:ro`. Press Esc again to leave insert mode.
- Type `:ro:` — popup closes when the closing `:` is typed.
- Type `:rocketx`, then Backspace until past the `:` — popup is gone.

- [ ] **Step 5: Verify word-boundary suppression**

Type `foo:ro` — popup must NOT appear (the trigger `:` is mid-word).

- [ ] **Step 6: Verify custom emoji (if your workspace has any)**

Wait a second after launch for the background fetch, then type `:` followed by 2 letters of a known custom (e.g. `:pa` for `:partyparrot:`). The matching custom rows should appear with the placeholder glyph `□`. Selecting one inserts the literal `:partyparrot:`; sending it shows the emoji rendered in Slack.

- [ ] **Step 7: Verify thread compose**

Open a thread (e.g. select a message and press whatever key opens the thread panel — refer to the README key bindings). Press `i` and type `:ro`. The picker appears above the thread compose box. Same accept/dismiss behavior.

- [ ] **Step 8: Verify mention/emoji mutual exclusion**

Type `@a` — mention picker opens. Type `:ro` after — verify both don't show simultaneously (the trigger detector should not fire mid-word; emoji picker stays closed).

- [ ] **Step 9: Verify workspace switch (if you have multiple workspaces configured)**

Open the workspace finder, switch to another workspace, type `:ro` — picker appears with built-ins immediately, plus that workspace's customs once fetched.

- [ ] **Step 10: Final commit (if any tweaks were needed)**

If the smoke test surfaced issues that needed code changes, fix them and commit. Otherwise this task is complete.

---

## Self-Review Checklist (filled in during plan-write)

**Spec coverage:**
- "Trigger after `:` + 2 chars at word boundary" → Task 4 step 7 (`maybeOpenEmojiPicker`).
- "Close on space / `:` / Esc / cursor crosses trigger" → Task 4 steps 7 (close branches) + tests in step 1.
- "Inserts `:shortcode:` literal" → Task 4 step 7 `insertEmoji`.
- "Built-ins + customs interleaved alphabetically with custom shadowing built-in" → Task 2 `BuildEntries` + tests.
- "Alias to built-in resolves to glyph; URL/cycle uses placeholder" → Task 2 `resolveCustomDisplay` + tests.
- "Picker visible 5 rows max, ↑/↓/Ctrl+P/Ctrl+N/Enter/Tab/Esc" → Task 3 `MaxVisible` + Task 4 `handleEmojiKey`.
- "Mention and emoji pickers mutually exclusive" → Task 4 step 6 ordering + tests.
- "Custom emojis fetched on connect, in-memory per-workspace, refreshed on reconnect" → Task 6. Reconnect refresh: the existing `ConnectionManager` reuses the same `Client` instance and does not re-call `connectWorkspace`, so custom emojis are NOT re-fetched on each WS reconnect today. The spec says "refresh on reconnect" — for v1 we accept the simpler "fetched once on initial connect" behavior. (If reconnect-driven refresh is required, add a refresh hook in `rtmEventHandler.OnConnect()` later; out of scope for this plan to keep TDD scope tight.)
- "App pushes entries on workspace switch" → Task 5 `SetCustomEmoji` called from `WorkspaceSwitchedMsg`.
- "Picker view stitched above compose, both compose boxes" → Task 5 step 3.
- "Insert-mode key forwarding when picker active" → Task 5 step 2.
- "Slack `emoji.list` wrapper" → Task 1.

**Placeholder scan:** No "TBD"/"TODO"/"add appropriate ..." patterns. Each step has the actual code or command.

**Type consistency check:**
- `EmojiEntry{Name, Display}` defined in Task 2; used identically in Tasks 3, 4, 5.
- `BuildEntries(map[string]string) []EmojiEntry` signature used in Task 5 `SetCustomEmoji`.
- `ListCustomEmoji(ctx) (map[string]string, error)` from Task 1 matches caller in Task 6.
- `IsEmojiActive() bool`, `CloseEmoji()`, `EmojiPickerView(width int) string`, `SetEmojiEntries([]emoji.EmojiEntry)` defined in Task 4, called in Task 5.
- `emojiStartCol` documented as "byte offset of first char AFTER trigger `:`" — `insertEmoji` uses `emojiStartCol - 1` consistently.

**Spec deviation noted:** Custom emojis live on `cmd/slk.WorkspaceContext` (not `service.WorkspaceManager`). Documented at top of plan.

**Spec scope reduction:** "Refresh on reconnect" deferred — see spec coverage notes above. Documented inline.
