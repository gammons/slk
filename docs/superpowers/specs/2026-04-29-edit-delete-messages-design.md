# Edit & Delete Own Messages

**Date:** 2026-04-29
**Status:** Design

## Overview

Add `E` (edit) and `D` (delete) keybindings that operate on the currently
selected message in the messages pane or thread pane, scoped strictly to
messages owned by the current user. Edit reuses the existing channel/thread
compose box, seeded with the message text and with submit semantics
swapped from "send" to "update". Delete shows a small new centered
confirmation overlay and, on confirm, calls Slack's `chat.delete`.

Both operations rely on the existing WebSocket echo (`message_changed`,
`message_deleted`) to update the local UI; there is no optimistic
rendering. As a precondition for the feature working correctly, this
work also fixes two pre-existing gaps:

1. The `NewMessageMsg` handler currently appends a duplicate when an
   `edited=true` event arrives. It must update in place.
2. The `OnMessageDeleted` WebSocket handler at `cmd/slk/main.go:1218` is
   a TODO. It must dispatch a UI message that removes the row.

## Goals

- Owners can edit and delete their own messages from both the channel
  message pane and the thread reply pane.
- Edits and deletes from any client (slk, web, mobile) reflect in slk
  in near real time without duplicates or stragglers.
- Confirmation overlay is reusable for future yes/no prompts.
- No regression to existing send/reply/reactions/permalink flows.

## Non-Goals

- Editing or deleting other users' messages (admin override). Owner-only.
- Editing or deleting from the thread pane's *parent* message header.
  Users navigate to the message pane to act on the parent.
- Multi-key prefix sequences (`dd`, `gg`, `yy`). The codebase does not
  yet have a prefix dispatcher; we use single uppercase keys instead.
- Undo. Slack does not provide an undelete API.
- Editing message attachments, blocks, or files.

## User-Facing Behavior

### Keybindings (Normal mode)

| Key | Pane                  | Action                              |
|-----|-----------------------|-------------------------------------|
| `E` | Messages or Thread    | Edit the selected own message       |
| `D` | Messages or Thread    | Delete the selected own message     |

The pre-existing dead `Edit` keymap entry bound to lowercase `e`
(`internal/ui/keys.go:64`) is removed; the new binding is uppercase `E`.
A new `Delete` keymap entry is added with key `D`. Help text is updated
in the keymap and the README keybinding table.

If the selected message is not owned by the current user (or no message
is selected, or `currentUserID` is empty), the key is a no-op and the
status bar shows a brief toast:

- `E` → `Can only edit your own messages`
- `D` → `Can only delete your own messages`
- No connection → `Not connected`

### Edit experience

1. Press `E` on an owned message in the messages pane or thread pane.
2. The compose box for that pane (channel compose or thread compose) is
   re-purposed:
   - Any in-progress draft text is stashed in `App.editing.stashedDraft`.
   - `compose.SetValue(msg.Text)` seeds the box with the existing text.
   - The placeholder/header is replaced with `Editing message — Enter
     to save, Esc to cancel`.
   - Mode switches to `ModeInsert` and the compose is focused.
3. The user edits with the full set of compose features (mentions,
   emoji autocomplete, multi-line via `Shift+Enter`, paste).
4. **Submit (`Enter`):** if the trimmed text is empty, refuse with toast
   `Edit must have text (use D to delete)`; the edit modal stays open.
   Otherwise emit `EditMessageMsg{ChannelID, TS, NewText}`. Edit mode
   stays active until the result message arrives, so the user is not
   "kicked out" mid-flight.
5. **Result `MessageEditedMsg{Err}`:**
   - Success: clear `App.editing`, restore stashed draft into the
     compose, return to normal mode. No toast. The WS echo will update
     the visible message text and add the `(edited)` marker.
   - Failure: clear `App.editing`, restore stashed draft, return to
     normal mode, status toast `Edit failed: <err>`. User can retry
     with `E`; the original message text is unchanged on the server.
6. **Cancel (`Esc`):** clear `App.editing`, restore stashed draft,
   return to normal mode. No API call.
7. **Implicit cancel:** channel switch, workspace switch, panel switch
   (`h`/`l`/`Tab`), mode change to non-Insert all cancel the edit and
   restore the stashed draft to its source compose.

### Delete experience

1. Press `D` on an owned message.
2. A centered confirmation overlay opens (`ModeConfirm`):

   ```
   ┌─ Delete message? ────────────────────┐
   │                                      │
   │  > <up to 80 chars of message text>… │
   │                                      │
   │  [y] confirm    [n/Esc] cancel       │
   └──────────────────────────────────────┘
   ```

3. **Confirm (`y` or `Enter`):** close overlay, return to normal mode,
   emit `DeleteMessageMsg{ChannelID, TS}`.
4. **Cancel (`n`, `Esc`, or any other key):** close overlay, return
   to normal mode. Message is untouched.
5. **Result `MessageDeletedMsg{Err}`:**
   - Success: no toast. The WS echo removes the row.
   - Failure: status toast `Delete failed: <err>`. Message stays.

## Architecture

### New tea.Msg types — `internal/ui/app.go`

Adjacent to the existing `SendMessageMsg`:

```go
type EditMessageMsg struct {
    ChannelID string
    TS        string
    NewText   string
}

type DeleteMessageMsg struct {
    ChannelID string
    TS        string
}

type MessageEditedMsg struct {
    ChannelID string
    TS        string
    Err       error
}

type MessageDeletedMsg struct {
    ChannelID string
    TS        string
    Err       error
}

// New: dispatched from the WS OnMessageDeleted callback.
type WSMessageDeletedMsg struct {
    ChannelID string
    TS        string
}
```

Plus function types and setters mirroring `SetMessageSender`:

```go
type EditMessageFunc func(channelID, ts, text string) tea.Msg
type DeleteMessageFunc func(channelID, ts string) tea.Msg

func (a *App) SetMessageEditor(fn EditMessageFunc)
func (a *App) SetMessageDeleter(fn DeleteMessageFunc)
```

### Wiring in `cmd/slk/main.go`

Next to the existing `app.SetMessageSender(...)` block (~line 349), add:

```go
app.SetMessageEditor(func(channelID, ts, text string) tea.Msg {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    err := client.EditMessage(ctx, channelID, ts, text)
    return ui.MessageEditedMsg{ChannelID: channelID, TS: ts, Err: err}
})

app.SetMessageDeleter(func(channelID, ts string) tea.Msg {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    err := client.RemoveMessage(ctx, channelID, ts)
    return ui.MessageDeletedMsg{ChannelID: channelID, TS: ts, Err: err}
})
```

`OnMessageDeleted` (currently a TODO at `cmd/slk/main.go:1218`):

```go
func (h *rtmEventHandler) OnMessageDeleted(channelID, ts string) {
    if err := h.db.DeleteMessage(channelID, ts); err != nil {
        log.Printf("cache delete: %v", err)
    }
    h.send(ui.WSMessageDeletedMsg{ChannelID: channelID, TS: ts})
}
```

### New package — `internal/ui/confirmprompt/`

Mirrors the reaction picker's shape so it composites the same way.

```go
type Result struct {
    Confirmed bool
    Cancelled bool
}

type Model struct {
    visible   bool
    title     string
    body      string
    onConfirm tea.Cmd
    // styling refs
}

func New() Model
func (m *Model) Open(title, body string, onConfirm tea.Cmd)
func (m *Model) Close()
func (m *Model) IsVisible() bool
func (m *Model) HandleKey(key string) (Result, tea.Cmd) // returns cmd from onConfirm if confirmed
func (m *Model) View(width int) string
func (m *Model) ViewOverlay(width, height int, background string) string
func (m *Model) RefreshStyles(theme ...)
```

Key handling: `y` and `enter` → confirm; `n`, `esc`, and any other key
→ cancel. Confirm and cancel both close the prompt; the caller restores
the previous mode after `HandleKey`.

A new `ModeConfirm` enum entry is added to `internal/ui/mode.go`. App
gets a `confirmPrompt confirmprompt.Model` field, a
`handleConfirmMode(msg tea.KeyMsg) tea.Cmd` dispatcher in `app.go`, and
a composition step in the final view next to the reaction picker
(`app.go:2773`).

### Edit-mode state on `App`

```go
type editState struct {
    active       bool
    channelID    string
    ts           string
    panel        Panel  // PanelMessages or PanelThread
    stashedDraft string
}

// Field on App:
editing editState
```

### Compose box reuse (no third instance)

We reuse the existing `compose.Model` for the relevant pane:
- `PanelMessages` → `App.compose`
- `PanelThread`  → `App.threadCompose`

A small extension to `compose.Model` exposes a placeholder override:

```go
func (m *Model) SetPlaceholderOverride(text string) // empty string clears override
```

Used so `Editing message — Enter to save, Esc to cancel` replaces the
normal `Message #channel… (i to insert)` placeholder. The override is
cleared when edit mode ends.

### New methods on `messages.Model` and `thread.Model`

```go
// Locate by TS, mutate text and IsEdited, invalidate render cache.
// Returns true if found.
func (m *Model) UpdateMessageInPlace(ts, newText string, isEdited bool) bool

// Remove by TS, adjust selected index, invalidate render cache.
// Returns true if found.
func (m *Model) RemoveMessageByTS(ts string) bool
```

`thread.Model` gets the same two methods, operating on its `replies`
slice. (The thread parent header is handled separately: parent edits
arrive via the channel's `messages.Model`; a parent delete in the
thread pane closes the thread panel — see Edge Cases.)

Pattern matches existing `IncrementReplyCount` and `UpdateReaction`
(`messages/model.go:421-476`).

### Wire the WS edit echo

The `OnMessage` Slack callback already carries an `edited bool`
(`internal/slack/events.go:13`). The `rtmEventHandler` in
`cmd/slk/main.go` builds a `MessageItem` with `IsEdited` set when
that flag is true and emits `NewMessageMsg`. We branch in App.Update
on `Message.IsEdited`:

```go
case NewMessageMsg:
    if msg.Message.IsEdited {
        a.messagepane.UpdateMessageInPlace(msg.Message.TS, msg.Message.Text, true)
        a.threadPanel.UpdateMessageInPlace(msg.Message.TS, msg.Message.Text, true)
        a.threadPanel.UpdateParentInPlace(msg.Message.TS, msg.Message.Text)
        // cache update for edits already happens in the writer path
        return a, nil
    }
    // existing append behavior
```

### Wire the WS delete echo

```go
case WSMessageDeletedMsg:
    a.messagepane.RemoveMessageByTS(msg.TS)
    a.threadPanel.RemoveMessageByTS(msg.TS)
    // If the deleted message was the thread parent, close the thread panel.
    if a.threadVisible && a.threadPanel.ThreadTS() == msg.TS {
        a.closeThreadPanel()
    }
    return a, nil
```

### Dispatcher additions in `app.handleNormalMode`

```go
case key.Matches(msg, a.keys.Edit):    // "E"
    return a.beginEditOfSelected()
case key.Matches(msg, a.keys.Delete):  // "D"
    return a.beginDeleteOfSelected()
```

`beginEditOfSelected` and `beginDeleteOfSelected` resolve `(channelID,
ts, text, panel)` from the focused pane (mirroring
`copyPermalinkOfSelected` at `app.go:1728`), check ownership, then
either set up `editing` state or open the confirm prompt.

### Insert-mode submit branching

In `handleInsertMode`, before the existing send/reply branches:

```go
if a.editing.active {
    text := strings.TrimSpace(activeCompose.Value())
    if text == "" {
        return a.toast("Edit must have text (use D to delete)")
    }
    return func() tea.Msg {
        return EditMessageMsg{
            ChannelID: a.editing.channelID,
            TS:        a.editing.ts,
            NewText:   activeCompose.TranslateMentionsForSend(activeCompose.Value()),
        }
    }
}
```

`Esc` in insert mode checks `a.editing.active` first and routes to a
cancel helper that restores the stashed draft.

## Data Flow

### Edit
```
press E → beginEditOfSelected → ownership check → stash draft, SetValue,
  placeholder override, ModeInsert
press Enter → emit EditMessageMsg
App.Update → invoke messageEditor → MessageEditedMsg
  success: clear editing, restore draft, ModeNormal (silent)
  failure: clear editing, restore draft, ModeNormal, toast
WS message_changed → NewMessageMsg{Edited:true} →
  UpdateMessageInPlace on both panes
```

### Delete
```
press D → beginDeleteOfSelected → ownership check →
  confirmPrompt.Open(..., onConfirm: returns DeleteMessageMsg)
press y/Enter → emit DeleteMessageMsg, ModeNormal
App.Update → invoke messageDeleter → MessageDeletedMsg
  success: silent
  failure: toast
WS message_deleted → OnMessageDeleted →
  WSMessageDeletedMsg → RemoveMessageByTS on both panes;
  close thread panel if parent deleted
```

## Error Handling & Edge Cases

- **Empty edit text:** refuse submit, toast `Edit must have text (use D
  to delete)`. Edit mode stays open.
- **Edit API failure:** exit edit mode, restore stashed draft, toast.
  User can retry.
- **Delete API failure:** toast. Message remains in pane.
- **Race: message deleted by another client during edit:** API returns
  `message_not_found`; toast standard error; pending WS delete echo
  removes row.
- **Race: WS echo arrives before HTTP response:** `UpdateMessageInPlace`
  is idempotent; `RemoveMessageByTS` returns false harmlessly if
  already gone.
- **Channel/workspace switch during edit:** cancel edit, restore draft
  to source compose, then proceed with switch.
- **Panel switch (`h`/`l`/`Tab`) during edit:** cancel edit, restore
  draft.
- **Bot/system messages:** `UserID == ""` or `UserID != currentUserID`
  → ownership check fails → toast.
- **Workspace admin deleting others' messages:** intentionally not
  supported. Owner-only.
- **`currentUserID == ""`:** toast `Not connected`.
- **Thread parent deleted via slk's main pane:** Slack deletes the
  whole thread server-side. WS `message_deleted` arrives for the
  parent; we remove it from the messages pane and, if the thread panel
  is showing this parent, close the thread panel.
- **Thread parent edited:** `message_changed` event arrives; main pane
  updates in place; thread panel header (which reads from
  `threadPanel.parentMsg`) needs the same in-place update — add a
  parallel `UpdateParentInPlace` on `thread.Model` that runs when the
  event TS matches the current parent.
- **Selected index after delete:** if the deleted message's index was
  ≤ `selected`, decrement `selected` and clamp to `[0, len-1]`. If the
  list becomes empty, `selected = 0` and `SelectedMessage()` returns
  `(_, false)` as today.

## Testing

### Unit
- `internal/ui/messages/model_test.go`:
  - `UpdateMessageInPlace`: found / not-found / `IsEdited` toggle /
    render-cache invalidation.
  - `RemoveMessageByTS`: found at top/middle/bottom / not-found /
    selected-index adjustment / empty-list result.
- `internal/ui/thread/model_test.go`: same two methods plus
  `UpdateParentInPlace`.
- `internal/ui/confirmprompt/model_test.go` (new):
  - `Open`/`Close`/`IsVisible` lifecycle.
  - `HandleKey` for `y` / `enter` / `n` / `esc` / arbitrary key.
  - Returns the configured `onConfirm` cmd only on confirm.
- `internal/ui/app_test.go`:
  - `beginEditOfSelected` rejects non-owned messages with toast.
  - `beginDeleteOfSelected` rejects non-owned messages with toast.
  - Edit submit emits `EditMessageMsg`, not `SendMessageMsg`/
    `SendThreadReplyMsg`.
  - Esc during edit restores the stashed draft.
  - Channel switch during edit cancels and restores.
  - `MessageEditedMsg{Err: nil}` exits edit silently.
  - `MessageEditedMsg{Err: x}` exits edit and toasts.
  - `WSMessageDeletedMsg` removes from both panes and closes the
    thread panel if the deleted TS matches the parent.
  - `NewMessageMsg` with `Message.IsEdited == true` calls
    `UpdateMessageInPlace` and does not append.

### Integration
- `cmd/slk/main_test.go` (or local equivalent):
  - `MessageEditor` setter wires through to `client.EditMessage`.
  - `MessageDeleter` setter wires through to `client.RemoveMessage`.
  - `OnMessageDeleted` callback persists the cache delete and emits
    `WSMessageDeletedMsg`.

### Manual smoke
- Edit own message in a channel; verify Slack web reflects the edit
  and `(edited)` marker shows in slk.
- Edit own message in a thread; verify both panes update.
- Delete own message; confirm; verify gone in Slack web.
- Try `E`/`D` on someone else's message — toast appears, no action.
- Press `D`, then `n` to cancel; confirm message remains.
- Type a draft, press `E` on previous own message, cancel — confirm
  draft restored.
- Edit from another client; verify slk updates in place rather than
  appending a duplicate.
- Delete from another client; verify slk removes the row.
- Delete a thread parent from main pane; verify thread panel closes.

## Files Touched

| File                                          | Change                          |
|-----------------------------------------------|---------------------------------|
| `internal/ui/keys.go`                         | Add `Delete`; rebind `Edit` to `E` |
| `internal/ui/mode.go`                         | Add `ModeConfirm`               |
| `internal/ui/app.go`                          | New msg types, setters, dispatchers, handlers, view composition |
| `internal/ui/messages/model.go`               | `UpdateMessageInPlace`, `RemoveMessageByTS` |
| `internal/ui/thread/model.go`                 | `UpdateMessageInPlace`, `RemoveMessageByTS`, `UpdateParentInPlace` |
| `internal/ui/compose/model.go`                | `SetPlaceholderOverride`        |
| `internal/ui/confirmprompt/model.go`          | New package                     |
| `internal/ui/confirmprompt/model_test.go`     | New tests                       |
| `cmd/slk/main.go`                             | Wire `SetMessageEditor`, `SetMessageDeleter`, fix `OnMessageDeleted` TODO |
| `README.md`                                   | Add `E`/`D` to keybinding table; move edit/delete out of roadmap |
| `docs/STATUS.md`                              | Mark edit/delete implemented    |

## Open Questions

None.

## References

- Slack client wrappers (already present): `internal/slack/client.go:531-547`
- Modal pattern template: `internal/ui/reactionpicker/model.go`,
  `internal/ui/overlay/overlay.go`
- Action dispatcher template: `app.copyPermalinkOfSelected` at
  `internal/ui/app.go:1728-1768`
- Find-by-TS mutation precedent: `messages/model.go:421-476`
- TODO this work fixes: `cmd/slk/main.go:1218-1220`
- Roadmap entries cleared: `README.md:87`, `docs/STATUS.md:105-106`
