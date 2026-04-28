# Copy message permalink

## Goal

Let the user copy a Slack permalink to the currently-selected message (or
thread reply) to the system clipboard, with a brief confirmation toast in
the status bar.

## User-facing behavior

In normal mode, with a message selected in the messages pane or a reply
selected in the thread pane:

- `yy` (vim-style double-y) or `C` triggers the action.
- The action calls Slack's `chat.getPermalink` for the selected message,
  writes the returned URL to the system clipboard via OSC 52, and shows
  `Copied permalink` for 2 seconds in the status bar's right-side toast
  slot, immediately to the left of the connection indicator.
- If nothing is selected, the keypress is a no-op.
- If the API call fails, the toast shows `Failed to copy link` for the
  same 2 seconds. No clipboard write happens.

`chat.getPermalink` returns a thread-aware URL when given a reply's `ts`,
so no client-side URL construction is needed for either pane.

## Implementation

### 1. Slack client — `internal/slack/client.go`

Add to the `SlackAPI` interface:

```go
GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error)
```

Add a wrapper on `Client`, mirroring `AddReaction`:

```go
func (c *Client) GetPermalink(ctx context.Context, channelID, ts string) (string, error) {
    return c.api.GetPermalinkContext(ctx, &slack.PermalinkParameters{
        Channel: channelID,
        Ts:      ts,
    })
}
```

### 2. Callback wiring (mirrors `SetReactionSender`)

`internal/ui/app.go`:

```go
type PermalinkFetchFunc func(ctx context.Context, channelID, ts string) (string, error)
```

- New field on `App` (`permalinkFetchFn`).
- New setter `SetPermalinkFetcher`.

`cmd/slk/main.go`: wire it next to `SetReactionSender`, calling
`client.GetPermalink`.

### 3. Keybindings — `internal/ui/keys.go`

- Keep the existing `Yank` binding on `y` (help text becomes
  `yy / copy permalink`).
- Add `CopyPermalink` on `C` (uppercase) with help text
  `copy permalink`.

### 4. Key handling — `internal/ui/app.go:handleNormalMode`

Treat `y` as a prefix the same way `g` is treated for `gg`. Inspect the
existing `gg` implementation and reuse the same prefix mechanism (a
`pendingPrefix` rune field with a short timeout, or whatever is in
place) by extending it to accept `y`. The second `y` and a single `C`
both call the same action helper.

### 5. Action helper — `internal/ui/app.go`

New helper `copyPermalinkOfSelected()` that mirrors
`openPickerFromMessage` / `openPickerFromThread`:

1. Resolve `(channelID, ts)` from the focused panel:
   - `PanelMessages`: `(a.activeChannelID, msg.TS)` from
     `a.messagepane.SelectedMessage()`.
   - `PanelThread`: `(a.threadPanel.ChannelID(), reply.TS)` from
     `a.threadPanel.SelectedReply()`.
   - If nothing is selected, return `nil`.
2. Capture `a.permalinkFetchFn`. If nil, return `nil`.
3. Return a `tea.Cmd` that calls the fetcher with a context. On
   success, return
   `tea.Batch(tea.SetClipboard(url), func() tea.Msg { return statusbar.PermalinkCopiedMsg{} })`.
   On error, return `statusbar.PermalinkCopyFailedMsg{}` and log the
   error at debug level.

### 6. Status bar — `internal/ui/statusbar/model.go`

Generalize the existing transient toast:

- Replace `copiedChars int` (line 27) with `toast string`.
- `View()` (lines 140–147): render `toast` verbatim if non-empty,
  styled the same as today's `Copied N chars`. Same position
  (`rightParts`, immediately before the connection indicator).
- Keep `CopiedMsg{N int}` and have its handler in `app.go` set the
  toast to `fmt.Sprintf("Copied %d chars", n)` so drag-to-copy is
  byte-for-byte unchanged in the UI.
- Add new message types:
  ```go
  type PermalinkCopiedMsg struct{}
  type PermalinkCopyFailedMsg struct{}
  ```
- Add internal setter `SetToast(s string)` and keep `ClearCopied()`
  (rename optional; keep for backwards compatibility) that zeroes the
  toast field.
- `CopiedClearMsg{}` continues to clear the slot. Reuse it for the
  permalink toasts.

In `app.go`, the existing `CopiedMsg` handler (lines 660–667) gains
two siblings that set the toast to `"Copied permalink"` /
`"Failed to copy link"` and schedule the same 2-second
`CopiedClearMsg` tick. Factor the tick-and-clear out into a small
helper if duplication is awkward.

### 7. README — `README.md`

Add to the keybindings table:

```
| `yy` / `C` | Normal (message) | Copy message permalink |
```

Remove `OSC 52 clipboard yank (yy)` from the roadmap section if it
implied a separate generic-yank feature; if generic message-text yank
is still intended as a separate roadmap item, leave it but reword to
disambiguate.

## Testing

- `internal/slack/client_test.go`: unit-test `GetPermalink` against a
  fake `SlackAPI` that records the `PermalinkParameters` and returns a
  canned URL/error.
- `internal/ui/statusbar/model_test.go`: assert the generalized toast
  renders the literal string passed in and clears on
  `CopiedClearMsg`. Keep the existing `CopiedMsg` test passing.
- `internal/ui/app_test.go` (or the closest existing key-handler
  test): with a stub `PermalinkFetchFunc`, drive `C` and `yy` against
  a populated messages pane and a populated thread pane. Assert:
  - Fetcher was called with the right `(channelID, ts)`.
  - A `tea.SetClipboard` and `PermalinkCopiedMsg` were emitted.
  - On fetcher error, `PermalinkCopyFailedMsg` was emitted and no
    clipboard write happened.
  - With nothing selected, the keypress is a no-op.

## Out of scope

- A generic message-text yank (`yy` to copy the rendered message
  body). Tracked separately on the roadmap.
- Copying permalinks for messages not currently selected (e.g. via
  `:` command).
- A toast queue. The single-slot 2-second toast is reused as-is; a
  second copy within 2 seconds simply replaces the previous toast and
  resets the timer.

## Files touched

- `internal/slack/client.go` (+ test)
- `internal/ui/keys.go`
- `internal/ui/app.go` (+ test)
- `internal/ui/statusbar/model.go` (+ test)
- `cmd/slk/main.go`
- `README.md`
