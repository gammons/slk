# Ephemeral action dialog — design (#247 sub-project 1, Part B)

**Issue:** [#247](https://github.com/gammons/slk/issues/247)
**Date:** 2026-10-10
**Builds on:**
- Part A, `docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md` (PR #298): parsed legacy actions, `MessageItem.Ephemeral`, display-only ingestion, and `(*slackclient.Client).AttachmentAction`.
- #302: `internal/bubbles/confirmprompt`, the first component in the RFC #236 shape.

**Supersedes:** Part A spec's "Part B" section, which recorded the agreed behaviour before the component existed. This document is the design against the component that landed.

## Goal

Make the buttons on ephemeral ("only visible to you") messages pressable.

When Slackbot says "You mentioned @x, but they're not in this channel", slk opens a dialog with **Add Them / Dismiss / Don't Show Again**. Clicking a button, or pressing its key, does what the Slack web client does. Later, `/giphy`'s Send / Shuffle / Cancel preview (sub-project 2) works through the same path.

**Done means:** in a real workspace, mention a non-member in a private channel. The dialog opens, and each of the three buttons works by mouse and by keyboard. "Add Them" first asks Slack's confirmation question.

The owner will judge the interaction by using it. The plan therefore puts a runnable build in front of a human before the polish tasks.

## Principles applied

- **RFC #236, "never add an implementation".** `confirmprompt` is generalised. No second dialog package and no new box-drawing copy.
- **RFC #236 Q2.** Pressing is intent from the dialog. On success the result lands nowhere: the WebSocket `message_deleted` cleans up. Failure is a notice. The component stays domain-agnostic: the host gives each button its action as a closure.
- **AGENTS.md.** Behaviour goes through the reducer chain, with nothing post-chain in `Update`. `internal/ui` does no I/O; pressing goes through a `core` port.

## 1. `confirmprompt` becomes a prompt with buttons

`internal/bubbles/confirmprompt`; imports stay free of Slack and slk.

### API

```go
// Button is one choice in the prompt.
type Button struct {
    Label  string
    Keys   []string       // shortcuts that press it, e.g. "y", "1"
    Action func() tea.Msg // returned as a tea.Cmd on press; nil = just close
}

// Choice is everything a prompt shows.
type Choice struct {
    Title, Body       string
    Buttons           []Button
    Default           int  // index highlighted on open; -1 = none
    CancelOnOtherKeys bool // an unbound key closes without pressing
}

func (m *Model) OpenChoice(c Choice)
func (m *Model) Open(title, body string, onConfirm ConfirmFunc) // signature unchanged
func (m Model) ButtonAt(x, y int) (int, bool)                   // box-local hit test
```

**`Open` is the yes/no preset.** It builds a Choice from two buttons:
- **Confirm:** keys `y`, `Y`; action `onConfirm`.
- **Cancel:** keys `n`, `N`; no action.

It sets `Default` to Confirm and `CancelOnOtherKeys: true`.

The resulting behaviour is identical to #302:
- `y`, `Y` or Enter confirms.
- Any other key cancels.
- A modifier on a non-printable key is ignored, so `shift+enter` confirms.
- `ctrl+y` and `alt+y` cancel.

The existing component tests, including the key grid, stay unchanged and green: they are the regression guard. The two call sites (quit, delete) are untouched.

### Keys (one model for every prompt)

Keys are checked in this order; the first match wins:

| Order | Key | Effect |
|---|---|---|
| 1 | a button's `Keys` | press that button |
| 2 | Enter | press the highlighted button; no-op when none is highlighted |
| 3 | Esc | close without pressing |
| 4 | `h`/`l`, `←`/`→`, Tab/Shift+Tab | move the highlight (wraps), **only when `CancelOnOtherKeys` is false** |
| 5 | anything else | close without pressing if `CancelOnOtherKeys`, otherwise ignored |

- Shortcut and Enter matching keep #302's modifier rule (the `printable` check on `Code`).
- A button shortcut beats movement, so a button bound to `h` or `l` gets pressed.
- **Movement is off in the yes/no preset.** In #302 every key that isn't a Confirm key cancels, `h`, `l` and Tab included. Because movement only applies when `CancelOnOtherKeys` is false, those keys still cancel there, so its behaviour is unchanged. The mouse still works on yes/no.

### Rendering

- **Box:** title, then the body, then a button row, inside the same box and width rules as today (35% of the terminal, clamped 40–60).
- **Body:**
  - It wraps instead of being flattened to one line.
  - Paragraph breaks are kept.
  - It is capped at 8 lines, the last ending in `…`.
- **Button row:**
  - It replaces the help footer: `[1 Add Them]  [2 Dismiss]  [3 Don't Show Again]`.
  - The shown key is the button's first entry in `Keys`. The bracket is omitted when a button has no keys.
  - It wraps onto further rows as needed.
  - A button wider than the row is shortened with `…`.
  - **One implementation of the row layout.** Today that logic lives in `blockkit.appendActions` (`internal/ui/messages/blockkit/render.go`), and `internal/bubbles` must not import `internal/ui`. So it **moves** into a new substrate package, `internal/bubbles/buttonrow`, and is not copied.
    - Its API: `Layout(labels []string, width, gap int) []Placed`, where each `Placed` holds a label's index, row, start column and its rendered (possibly shortened) text.
    - The Block Kit renderer's output must stay byte-identical; its existing tests are the guard.
    - The prompt also uses the column positions for `ButtonAt`.
    - `AGENTS.md`'s shared-code table gets a row for it in the same commit.
- **Styles:** `Styles` gains `Button` and `ButtonActive`. `DefaultStyles` supplies both. The host's `confirmPromptStyles()` derives them from the theme, so they follow theme changes through the existing push in `applyTheme`.
- **Quit and delete** now show `[y Confirm]  [n Cancel]` as buttons instead of the help line. No golden covers the confirm prompt.

### Mouse

- `ButtonAt(x, y)` maps a box-local cell to a button index.
- The host's `confirmPromptBox` (`internal/ui/confirm.go`) implements the click router's existing `pointClickable`. `ClickAt` highlights the button under the cursor and reports a hit.
- `reducer_modal_click.go`'s `ModeConfirm` case gains `point: confirmPromptBox{…}` and `activation: enter`. The router then sends Enter, which presses the highlighted button. This is the path the user-profile dialog already uses.
- Clicking inside the box but off a button does nothing. Clicking outside dismisses, as today.

Quit and delete become clickable as a side effect.

### Component tests

All parallel, with no `App`, stdlib `testing`.

- **Unchanged:** the existing yes/no tests.
- **N-button cases:**
  - Movement and wrap-around.
  - Enter with no highlight.
  - Shortcut press, and Esc.
  - An unbound key with and without `CancelOnOtherKeys`.
  - `Action` returned as the command, and nil `Action` closing silently.
- **Hit testing:** `ButtonAt` for each button, in gaps, on wrapped rows, and outside.
- **Rendering:**
  - Body wrapping and the 8-line cap.
  - The button row wrapping at a narrow width.
  - An over-wide label shortened.
  - The highlighted button styled differently.

## 2. Host: queue, auto-open, pressing, keys

### Port

```go
// InteractionService presses interactive message elements.
type InteractionService interface {
    PressAttachmentAction(channelID ids.ChannelID, messageTS ids.MessageTS,
        attachmentID int, callbackID string, action blocks.LegacyAction, ephemeral bool) error
}
```

- It follows `ReactionService`'s shape: it returns an error and the app turns that into a toast.
- It has a funcs-struct adapter in `internal/core/adapters.go`, nil-safe like the others.
- `cmd/slk` wires it to the **active** workspace's `Client.AttachmentAction`.
- A test-only setter in `services_helpers_test.go` follows the existing `set…ForTest` family.

### Queue and auto-open

All of this lives in a new `internal/ui/reducer_action_prompt.go` on the reducer chain. It coordinates the composer, the mode, both panes and deletes, so it is `App`'s work.

- **State:** `actionQueue []actionPromptRef`, with `{channelID, ts, threadTS}` per message, plus `shownPrompt *actionPromptRef` for the message the dialog is showing.
- **Enqueue:** `reduceNewMessage`'s ephemeral branch enqueues the message when it has at least one legacy action of type `button` and is on screen: the active channel, or a reply to the open thread. It then calls `tryOpenActionPrompt`.
- **`tryOpenActionPrompt` opens the front of the queue unless:**
  - another modal is up, or
  - the mode is `ModeInsert` and the focused composer's `Value()` is non-empty.

  In that case it waits, and the toast "<sender>: press `b` to respond" shows once per message.
- **Re-check points**, the only places it is called besides enqueue:
  1. `handleConfirmMode`, when the prompt reports hidden.
  2. Leaving insert mode (`handleInsertMode`'s `SetMode(ModeNormal)`).
  3. After a send clears the composer (`reduceSendMessage`, and the thread-reply send).

  Not from `SetMode` and not from `Update`.
- **Deletes:** `WSMessageDeletedMsg` removes the ts from the queue. If `shownPrompt` is that message, the dialog closes and the mode returns to normal.
- **Lookup:** opening, re-opening and `b` find the message in the pane models by `(channelID, ts)`. If it is gone (switched away, reloaded), the queue entry is dropped silently.

### Dialog content

- **Title:** the message's sender name.
- **Body:** `messages.FlattenMrkdwn` of the message text, then each attachment's pretext and text, separated by blank lines.
- **Buttons:**
  - One per legacy action of type `button`, across all attachments, in order. `select` actions are not shown. `Keys` are `1`–`9` by position; later buttons have none.
  - `Default: -1`, so no button is highlighted and a stray Enter does nothing (this matters for `/giphy`, whose first button is Send).
  - `CancelOnOtherKeys: false`.
- **Each button's `Action`:**
  - **`URL` set:** returns `OpenLinkMsg{URL}`.
  - **`Confirm` set:** returns a message that re-opens the prompt with `Title: Confirm.Title`, `Body: Confirm.Text` and two buttons. **[DismissText]** (default "Cancel") re-opens the original choice. **[OKText]** (default "Okay") presses. `Default: -1`.
  - **Otherwise:** presses.
- **Pressing:** the action returns a `tea.Cmd` that calls `PressAttachmentAction` with the attachment's `ID` and `CallbackID`, the action as parsed (including its `ID` from the raw frame), and `ephemeral: true`. Its result message is `actionPressedMsg{label, err}`.
  - **Success:** nothing to do, because Slack sends `message_deleted`.
  - **Failure:** `ToastMsg` "Couldn't send <label>: <err>". The message stays in the pane, so `b` retries.

### The `b` key

In normal mode, on the selected message:
- An ephemeral message with at least one button action opens its dialog immediately. The user asked, so the interruption rule does not apply.
- An ephemeral message without one shows the toast "No buttons on this message".
- A non-ephemeral message shows the same toast.

**Restricted to ephemerals.** Non-ephemeral legacy buttons from history have no action `id` (Part A §6). Sending without it is a request shape the web client never makes, which risks Enterprise Grid anomaly detection, so they stay out of scope.

`b` is added to `keys.go` with help text "respond to message buttons", and appears in the help overlay.

### Keys on a selected ephemeral

These keys would send a ts Slack does not know, so on an ephemeral they show a toast instead of acting:
- Enter (open thread)
- `r`, `R`
- `L`
- `Y`, `C` (permalink)
- `F` (forward)
- `U` (mark unread)
- `S` (save thread)

The toast is "Only visible to you — press b to respond" when the message has actions, otherwise "Only visible to you". One helper guards them all, checked at the top of each handler.

`y` (copy text) and `K` (author profile) still work. `E` and `D` already refuse messages that are not your own.

### Hint line

Under an ephemeral message with legacy actions, the interactive hint reads `b to respond` instead of `↗ open in Slack to interact`. Other interactive messages are unchanged.

One shared helper picks the text, used by both `messages.Model` (`model.go`, the hint site) and `thread.Model` (the matching site), per AGENTS.md's change-both rule.

## Error handling

| Case | Behaviour |
|---|---|
| Press fails (HTTP, `ok:false`) | toast "Couldn't send <label>: <err>"; dialog already closed; message remains |
| Message deleted while queued | removed from queue |
| Message deleted while its dialog is open | dialog closes |
| Queued message no longer in any pane | entry dropped silently when reached |
| No active workspace / nil service | press is a no-op returning an error, so a toast |
| More than 9 buttons | buttons 10+ have no shortcut; still clickable and reachable with Tab |
| Ephemeral whose actions are all `select` | not enqueued; `b` shows "No buttons on this message" |

## Testing (host)

All in `internal/ui`, white-box, on `newTestApp` and the existing helpers.

- **Auto-open:**
  - With an empty composer: the dialog opens.
  - With text in the composer: it waits and shows the toast once, then opens when insert mode is left.
  - While another modal is up: it waits, then opens when that modal closes.
- **Queue:** two ephemerals open one after another.
- **Delete:** removes a queued entry; closes an open dialog.
- **Buttons and actions:**
  - The confirm swap, Cancel returning to the list, and OK pressing.
  - A URL button.
  - A failed press giving a toast while the message stays.
  - A press passing channel, ts, attachment ID, callback ID, action (with ID) and `ephemeral=true` to a fake service.
- **`b`:** on an ephemeral with actions, one without, and a normal message.
- **Key guard:** every guarded key on an ephemeral toasts and calls no service; `y` and `K` still work.
- **Mouse:** a click on a button presses it; a click inside but off a button does nothing; a click outside dismisses.
- **Hint:** the text in both panes.
- **Goldens:** the 8 App goldens stay byte-identical.

## Out of scope

- Applying ephemeral `message_changed`. This is how `/giphy`'s Shuffle replaces its preview; it belongs with sub-project 2, which is the only way to trigger it.
- Block Kit `actions` buttons (no capture of the request).
- `select` menus. They render in the pane, but the dialog leaves them out.
- Pressing buttons on non-ephemeral messages.
- The shared modal-chrome substrate (#236's next stage).

## Delivery note

This branch builds on #298 and is rebased onto `main` once #298 merges. The implementation plan must reach a build the owner can run against a real workspace early, after the component and auto-open, before the guard, hint and polish tasks, because the interaction will be judged by feel.
