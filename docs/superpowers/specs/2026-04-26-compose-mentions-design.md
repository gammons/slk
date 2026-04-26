# Compose Mentions Design

Date: 2026-04-26

## Overview

Add `@mention` autocomplete to the compose box for both channel messages and thread replies. When the user types `@` at a word boundary, an inline dropdown appears above the compose box showing matching workspace users. Selecting a user inserts `@DisplayName` into the text. On send, display names are translated back to Slack's `<@UserID>` wire format.

## Scope

- User mentions only (`@user`). Channel mentions (`#channel`) out of scope for now.
- Applies to both channel compose and thread reply compose (both use `compose.Model`).
- Special mentions: `@here`, `@channel`, `@everyone`.

## Design Decisions

1. **Inline popup, not centered overlay.** The mention picker appears as a small dropdown anchored above the compose box, similar to Slack's native autocomplete. This is more natural for mid-sentence mentions than the centered modal pattern used by channel finder and reaction picker.

2. **Reverse lookup on send, not position tracking.** After the user selects a mention, `@DisplayName` appears in the compose text as plain text. On send, the text is scanned for `@DisplayName` patterns and translated to `<@UserID>`. This avoids the complexity of tracking mention character positions through edits.

3. **Word boundary trigger.** The `@` character only triggers the mention picker when preceded by a space, newline, or at position 0. This avoids false triggers in email addresses or other contexts.

## Components

### Mention Picker (`internal/ui/mentionpicker/`)

A new component that renders an inline dropdown for selecting users.

**Model state:**
- `visible bool` -- whether the picker is showing
- `query string` -- characters typed after `@`
- `users []User` -- all workspace users
- `filtered []User` -- users matching the current query
- `selected int` -- index into filtered list
- `maxVisible int` -- max items shown (5)

**User struct:**
```go
type User struct {
    ID          string
    DisplayName string
    Username    string
}
```

**Filtering:**
- Case-insensitive prefix match on both DisplayName and Username
- Empty query shows first 5 users alphabetically
- Special entries (`@here`, `@channel`, `@everyone`) included at the top when they match the query

**Key handling (`HandleKey(msg tea.KeyMsg) (bool, *MentionResult)`):**
- Returns `(consumed bool, result *MentionResult)`
- Up/Down or Ctrl+P/Ctrl+N: navigate selection
- Enter or Tab: select current user, return result
- Esc: close picker
- Backspace: remove last query char, or close if query is empty and backspace deletes the `@`
- Printable chars: NOT handled by picker -- compose forwards textarea content changes as query updates

**Rendering (`View(width int) string`):**
- Lipgloss-styled box with rounded border (blue, matching compose focus style)
- Each row: `▌ DisplayName (username)` with green selection indicator
- Max 5 visible rows
- Width matches compose box width

### Compose Model Changes (`internal/ui/compose/`)

**New fields:**
```go
mentionPicker   mentionpicker.Model
mentionActive   bool
mentionStartCol int  // cursor column where @ was typed
users           []mentionpicker.User  // all workspace users
reverseNames    map[string]string     // displayName -> userID
```

**Trigger logic (in `Update()`):**
1. Forward key to textarea as usual
2. After forwarding, check if the character just inserted is `@` at a word boundary
3. If so: set `mentionActive = true`, record `mentionStartCol`, open the picker

**While mention picker is active:**
1. Intercept keys before they reach the textarea:
   - Up/Down/Ctrl+P/Ctrl+N: forward to picker only (navigation)
   - Enter/Tab: forward to picker; if it returns a result, replace text from `mentionStartCol-1` (the `@`) to cursor with `@DisplayName ` (trailing space), close picker
   - Esc: close picker, resume normal typing
   - Backspace: forward to textarea AND update picker query; if cursor moves before `mentionStartCol`, close picker
   - Printable chars: forward to textarea, then update picker query from textarea content between `mentionStartCol` and cursor
2. Picker query is derived from textarea content, not maintained separately, keeping them naturally in sync.

**Dismissal conditions:**
- Esc pressed
- Cursor moves before `mentionStartCol` (e.g., via backspace deleting the `@`)
- Space typed with no matching results

**`TranslateMentionsForSend(text string) string`:**
- Scans text for `@DisplayName` patterns using the `reverseNames` map
- Replaces with `<@UserID>` format
- Sorts display names by length (longest first) to avoid partial matches
- Handles special mentions: `@here` -> `<!here>`, `@channel` -> `<!channel>`, `@everyone` -> `<!everyone>`

**`SetUsers(users []mentionpicker.User)`:**
- Stores users for the mention picker
- Builds the `reverseNames` map (displayName -> userID)
- Forwards users to the mention picker model

**`IsMentionActive() bool`:**
- Returns whether the mention picker is currently visible
- Used by app.go to know whether Enter should send or select a mention

**View changes:**
- When `mentionActive`: render picker above compose box (picker `View()` + newline + compose `View()`)
- When not active: render as before

### App-level Changes (`internal/ui/app.go`)

**Key routing in `handleInsertMode()`:**
- Before the existing Enter-to-send logic, check `activeCompose.IsMentionActive()`
- If mention is active, forward key to compose (which handles picker interaction) instead of sending
- All other keys: forward to compose as before (compose handles picker state internally)

**User data flow:**
- Where `app.SetUserNames()` is called today, also call `compose.SetUsers()` and `threadCompose.SetUsers()` with the user list
- Build `[]mentionpicker.User` from the existing `UserNames` map plus the cache's user data (to get usernames)

**No new mode.** The mention picker lives inside INSERT mode. The compose model manages the state internally, keeping app.go changes minimal.

## Data Flow

```
1. User types `@ali` in compose box
2. Compose detects `@` at word boundary -> opens mention picker
3. Subsequent chars `l`, `i` update picker query via textarea content
4. Picker filters users: shows "Alice (alice)", "Alicia (alicia.j)"
5. User presses Enter -> picker returns {ID: "U1234", DisplayName: "Alice"}
6. Compose replaces `@ali` with `@Alice ` in textarea, closes picker
7. User finishes message: "hey @Alice can you review this?"
8. User presses Enter to send
9. app.go reads compose value, calls TranslateMentionsForSend()
10. Text becomes: "hey <@U1234> can you review this?"
11. Sent to Slack API
```

## Edge Cases

- **Multiple mentions in one message:** Each `@` at a word boundary triggers a fresh picker session. Previous mentions remain as `@DisplayName` in the text.
- **Display name collisions:** If two users share a display name, reverse lookup picks the first match. This is rare in practice.
- **Editing existing mentions:** If the user edits a previously inserted `@DisplayName` (e.g., deletes part of it), the broken mention will be sent as plain text. This matches Slack's desktop behavior.
- **Empty user list:** If users haven't loaded yet, the picker shows no results. The `@` character is still inserted normally.
- **Thread compose:** Works identically to channel compose since both use `compose.Model`.

## Testing

- **mentionpicker:** Unit tests for filtering (prefix match, case insensitive, special mentions), key handling (navigation, selection, dismissal), and rendering.
- **compose:** Unit tests for trigger detection (word boundary check), mention insertion (text replacement), `TranslateMentionsForSend()` (name-to-ID replacement, longest-first ordering, special mentions), and `IsMentionActive()` state.
- **Integration:** Manual testing for the visual popup behavior and end-to-end mention flow.
