# Inline Emoji Selector Design

Date: 2026-04-28

## Overview

Add an inline emoji autocomplete popup to the compose box. When the user types `:` at a word boundary followed by two or more characters, a small dropdown appears above the compose box listing matching emoji shortcodes. Selecting one replaces the in-progress `:query` with the full `:shortcode:` literal. The popup includes both built-in Unicode emojis (from the existing `kyokomi/emoji/v2` codemap) and the active workspace's custom emojis.

## Scope

- Inline autocomplete in `compose.Model`. Applies to both the channel compose and thread compose (they share the same model).
- Built-in emojis from `emoji.CodeMap()` plus the active workspace's custom emojis fetched via Slack's `emoji.list` Web API.
- Inserts the literal `:shortcode:` into the textarea on selection. Slack expands shortcodes server-side; the existing local message renderer already maps shortcodes to glyphs for display.

## Out of Scope

- Rendering custom emoji images (terminals can't display them; rows show a placeholder).
- Persisting custom emoji lists across restarts (in-memory per-workspace; refetched on reconnect).
- Smarter triggering (e.g. detecting code spans / URLs to suppress the popup). Word-boundary detection is sufficient for v1.
- Fuzzy or popularity-ranked matching. Prefix match, alphabetical sort.
- Configurable max-visible row count.

## User Experience

- Trigger: `:` at a word boundary (start of line or preceded by whitespace), followed by ≥ 2 characters of `[a-zA-Z0-9_+\-]`. The popup opens once the threshold is met.
- Rows: `🚀  :rocket:` for built-ins and alias-customs that resolve to a Unicode glyph; `□  :partyparrot:` for image-backed customs (single-cell placeholder character, kept as a named constant).
- Filter: case-insensitive prefix match on the shortcode name. Sorted alphabetically. Built-ins and customs interleaved into one list (a custom shadows a built-in of the same name).
- Visible rows: 5 (matches `mentionpicker.MaxVisible`).
- Keys when popup is open:
  - `↑` / `↓` / `Ctrl+P` / `Ctrl+N`: move selection.
  - `Enter` / `Tab`: accept — replace the substring from the trigger `:` through the cursor with `:fullname:`, then close.
  - `Esc`: dismiss without inserting.
  - `Backspace`: shrinks query; closes once the cursor crosses the trigger `:`.
  - Typing more `[a-zA-Z0-9_+\-]` characters: narrows the filter.
  - Space, closing `:`, or any other punctuation: closes the popup (the latter means the user finished the shortcode themselves).
- If the active workspace's custom emoji list is still being fetched, only built-ins appear; customs are added live once the list arrives.

## Design Decisions

1. **Inline popup, not modal overlay.** Mirrors `mentionpicker` exactly. A centered overlay would obscure the message being composed and would be inconsistent with the existing autocomplete pattern.

2. **Owned by `compose.Model`, not by `App`.** Trigger detection and substring replacement live next to the textarea state. App's only changes are (a) routing all keys to compose when the picker is active, and (b) stitching the picker's view above the compose box. This is the same pattern used for mentions.

3. **Insert `:shortcode:` literal, not Unicode.** The current send path sends the raw textarea content; Slack expands shortcodes server-side, and the local renderer already handles `:shortcode: → glyph`. Inserting Unicode would mismatch the send path and complicate cursor math for multi-cell graphemes.

4. **Custom emojis fetched per-workspace, in-memory only.** A one-shot `emoji.list` call on workspace connect (and on reconnect) populates a `map[name]targetOrURL` on the `WorkspaceManager`. No SQLite persistence in v1 — the call is cheap and the data is workspace-scoped.

5. **Alias resolution at entry-build time, capped at 4 hops.** Custom emojis with `alias:target` values are resolved against the custom map first (chained aliases), then against the built-in codemap. A successful resolution gives the row a real glyph preview; a cycle or unresolved alias falls back to the image-backed placeholder.

6. **Mention picker and emoji picker are mutually exclusive.** Compose's `Update` checks emoji-picker first, then mention-picker, then defaults. In practice the triggers (`@` vs `:`) can't both be active because typing one is not a valid query character for the other.

## Components

### Emoji Picker (`internal/ui/emojipicker/`)

A new component, structurally a near-clone of `internal/ui/mentionpicker`.

**`EmojiEntry` struct:**

```go
type EmojiEntry struct {
    Name    string // shortcode without colons, e.g. "rocket"
    Display string // single-grapheme preview cell, e.g. "🚀" or the placeholder "□"
}
```

**Model state:**
- `entries []EmojiEntry` — full sorted list (built-ins + customs)
- `filtered []EmojiEntry` — entries matching the current query
- `query string`
- `selected int` — index into `filtered`
- `visible bool`

**Public API:**

```go
func New() Model
func (m *Model) SetEntries(entries []EmojiEntry)
func (m *Model) Open(query string)
func (m *Model) Close()
func (m *Model) IsVisible() bool
func (m *Model) SetQuery(q string)
func (m *Model) MoveUp()
func (m *Model) MoveDown()
func (m *Model) Selected() (EmojiEntry, bool)
func (m Model) View(width int) string
```

**Rendering:**
- Bordered box matching `mentionpicker` style.
- Each row: `<display><pad>:<name>:` where `<pad>` is computed using `internal/emoji.Width(display)` so the name column aligns regardless of whether the glyph is one cell or two.
- Empty state: a single muted "no emojis" row, mirroring the existing pattern.

### Compose changes (`internal/ui/compose/model.go`)

New fields alongside the existing mention fields:

```go
emojiPicker     emojipicker.Model
emojiStartCol   int  // byte offset of the trigger ':' in textarea.Value()
emojiActiveLine int  // line where the trigger lives; closes if cursor leaves
```

New / changed behavior:

- `detectEmojiTrigger()` runs after every key forwarded to the textarea. It scans backward from the cursor to find a `:` preceded by whitespace or start-of-line, with all intervening characters matching `[a-zA-Z0-9_+\-]`. If query length ≥ 2 and the picker is closed, open it; if open, update the query.
- `handleEmojiKey(msg) bool` is the analog of `handleMentionKey` (compose/model.go:245). Order of checks inside compose `Update`: emoji-picker active → mention-picker active → default.
- On accept: replace bytes `[emojiStartCol, cursorPos)` in `Value()` with `:fullname:` using the same byte-offset replacement helpers used by mention insertion. Set the cursor to the position immediately after the inserted text. Close the picker.
- New exports for App: `EmojiPickerActive() bool`, `EmojiPickerView(width int) string`, `SetEmojiEntries(entries []emojipicker.EmojiEntry)`.
- The picker is closed in `Reset()` alongside the mention picker.
- Compose's `Version()` is bumped on picker state change so the panel cache invalidates.

### App changes (`internal/ui/app.go`)

- `handleInsertMode` (line 881): the existing branch that forwards every key to compose when `compose.MentionPickerActive()` is true is extended to also check `compose.EmojiPickerActive()`. Same forwarding for both. Same Escape-closes-picker-first behavior.
- Message-pane assembly (lines 1998-2002 and 2050-2054): stitch `EmojiPickerView` above compose using the same join used for `MentionPickerView`. If both are non-empty (shouldn't happen in practice), emoji wins.
- On workspace switch and on receipt of a "custom emojis updated" event for the active workspace, App rebuilds the entry list via `emoji.BuildEntries(activeCustoms)` and calls `compose.SetEmojiEntries(...)` and `threadCompose.SetEmojiEntries(...)`.

### Entry builder (`internal/emoji/entries.go`)

```go
func BuildEntries(customs map[string]string) []EmojiEntry
```

- Iterate `emoji.CodeMap()`. Strip surrounding colons and trailing space; record `Display = trimmedGlyph`.
- Iterate `customs`:
  - If value starts with `alias:`, resolve up to 4 hops: first against `customs` itself, then against the built-in codemap. On success, `Display = resolved glyph`. On cycle / dead-end, treat as image-backed.
  - Otherwise (URL), `Display = placeholder` (constant `placeholderGlyph = "□"`).
- Dedupe by name with custom-shadows-builtin precedence.
- Sort alphabetically by name.

`EmojiEntry` is defined in `internal/emoji` (it's data, not UI). The `emojipicker` package imports `internal/emoji` for both `EmojiEntry` and `Width`. The picker stores `[]emoji.EmojiEntry`. `internal/emoji` does not import `emojipicker`, so there is no import cycle.

### Slack client (`internal/slack/client.go`)

Add a thin wrapper:

```go
func (c *Client) ListCustomEmoji(ctx context.Context) (map[string]string, error)
```

Calls Slack's `emoji.list` Web API method. Returns the raw `name → value` map (where value is either `https://...` or `alias:targetname`). Errors are surfaced to the caller; the WorkspaceManager logs and continues on failure.

### Workspace manager

- New field: `customEmoji map[string]string` (per-workspace, in-memory).
- New accessor: `CustomEmoji() map[string]string` (returns a defensive copy or read-only view).
- During the connect/reconnect flow, kick off `ListCustomEmoji` alongside other startup fetches. On success, replace `customEmoji` and emit a `CustomEmojisUpdatedMsg{WorkspaceID}` so App can refresh the active compose's entry list.
- Failures are logged but non-fatal; the picker continues to work with built-ins only.

## Data Flow

```
WorkspaceManager.connect
  └─ ListCustomEmoji ──► customEmoji map ──► CustomEmojisUpdatedMsg
                                                    │
                                                    ▼
                                       App (if active workspace)
                                                    │
                                                    ▼
                                  emoji.BuildEntries(customs) ──► []EmojiEntry
                                                    │
                                                    ▼
                                  compose.SetEmojiEntries(entries)
                                  threadCompose.SetEmojiEntries(entries)

User types ':ro'
  └─ compose.Update ──► detectEmojiTrigger ──► emojiPicker.Open("ro")
       └─ App stitches EmojiPickerView above compose

User presses Enter
  └─ App.handleInsertMode sees EmojiPickerActive → forwards to compose
       └─ compose.handleEmojiKey accepts, replaces ':ro' with ':rocket:', closes picker
```

## Edge Cases

- **Cursor leaves trigger range.** Picker closes (cursor before `emojiStartCol`, different line, or selection cleared).
- **No matches after filtering.** Empty state row; Enter is a no-op close.
- **Custom list arrives while picker open.** `SetEntries` rebuilds against current query; selection clamps to new length.
- **Alias cycles.** Capped at 4 hops; cycle → image-backed placeholder.
- **Workspace switch with picker open.** App resets compose; picker closes.
- **`emoji.list` failure.** Logged; built-ins still work; retried on next reconnect.
- **Underscore / digits / `+` / `-` in shortcodes.** Allowed as query characters (e.g. `:thumbs_up:`, `:+1:`, `:-1:`, `:100:`).
- **Mention and emoji triggers in the same line.** Mutually exclusive at any given cursor position; switching contexts (e.g. user backspaces past the `:` then types `@`) closes one and opens the other naturally.
- **Shift+Enter / unknown CSI.** No special handling needed — the existing CSI rewrite in `App.Update` (lines 384-396) routes through `handleInsertMode` and forwards to compose, which sees the active picker.

## Testing

Unit tests (no terminal required):

- `internal/emoji/entries_test.go`
  - alias to built-in resolves to glyph
  - alias to another custom alias (chained) resolves
  - alias cycle falls back to placeholder
  - alias to nonexistent name falls back to placeholder
  - URL-only custom uses placeholder
  - custom shadows built-in of same name
  - alphabetical sort

- `internal/ui/emojipicker/model_test.go`
  - `Open` / `Close` / `IsVisible`
  - prefix filter, case-insensitive
  - `MoveUp` / `MoveDown` bounds
  - `SetEntries` while visible — selection clamps, filter re-applies
  - empty filter result

- `internal/ui/compose/model_test.go` (additions)
  - trigger fires at start of line after `:xy`
  - trigger fires after whitespace `\nfoo :xy`
  - trigger does NOT fire after letter `foo:xy`
  - 2-char threshold (`:` alone, `:x` do not open)
  - typing more chars updates query
  - space closes
  - second `:` closes
  - Backspace past trigger closes
  - Esc closes without inserting
  - Enter inserts `:rocket:` and closes
  - emoji picker takes precedence over mention picker

- `internal/slack/client_test.go` (additions)
  - `ListCustomEmoji` parses `name → URL` and `name → alias:target` from canned JSON
  - error path returns the error

Manual smoke checklist (in implementation plan, no automated TUI snapshots):

1. Type `:ro` in compose — popup appears with `:rocket:` selected somewhere visible.
2. Press Enter — text becomes `:rocket:`, popup closes.
3. Type `:zzz` — empty state.
4. Type `:partyparrot` (workspace must have it) — placeholder row visible.
5. Connect to a different workspace — popup updates to that workspace's customs.
6. `Esc` while popup open — popup closes, insert mode preserved.
7. Send the message — Slack renders the emoji.

## Package Boundaries

| Package | Responsibility |
|---|---|
| `internal/emoji` | `EmojiEntry` type, `BuildEntries(customs)` (alias resolution, dedupe, sort), existing `Width` |
| `internal/ui/emojipicker` | UI model: state, filter, render. Imports `internal/emoji` |
| `internal/ui/compose` | Trigger detection, key routing, substring replacement on accept |
| `internal/ui/app` | Stitches picker view; routes keys when picker active; rebuilds entries on workspace switch / customs-updated |
| `internal/slack` | `ListCustomEmoji` Web API wrapper |
| WorkspaceManager | Owns per-workspace custom emoji map; fetches on connect/reconnect; emits update message |
