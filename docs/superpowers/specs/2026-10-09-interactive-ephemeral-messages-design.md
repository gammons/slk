# Interactive ephemeral messages — design

**Issue:** [#247](https://github.com/gammons/slk/issues/247) (slash commands / `/giphy`)
**Date:** 2026-10-09
**Status:** Part A ready for planning. Part B blocked on
[#237](https://github.com/gammons/slk/pull/237).

## Why this comes before slash commands

#247 asks for slash commands, motivated by `/giphy`. Running a command is the
easy half: slk authenticates with browser credentials (xoxc + `d` cookie), so
it can call `chat.command` exactly as Slack's web client does. The hard half is
what comes back. `/giphy` replies with an **ephemeral** message — "only visible
to you" — carrying **Send / Shuffle / Cancel** buttons, and nothing is posted
until one is pressed.

slk cannot show or press those buttons today, and the same gap is already
visible without slash commands: mentioning someone who is not in the channel
makes Slackbot post an ephemeral "You mentioned @x, but they're not in this
channel" with **Add Them / Dismiss / Don't Show Again** buttons, and slk drops
the buttons entirely.

So #247 decomposes into independently shippable sub-projects:

1. **Interactive ephemeral messages** — this spec.
2. **Slash commands** via `chat.command`. With (1) in place, `/giphy` needs no
   special handling.
3. *(Later, optional)* a `/` autocomplete picker.
4. *(Later, optional)* animated GIFs. Received GIFs already render as a still
   first frame (`image/gif` decodes frame 0); animation is cheap on kitty and
   expensive on sixel/half-block, and is out of scope here.

This spec is split again by the architecture work in RFC
[#236](https://github.com/gammons/slk/issues/236):

- **Part A — data, ingestion, rendering, wire call.** Touches `internal/slack`,
  `internal/core`, `cmd/slk`, the Block Kit renderer and `reduceNewMessage`.
  None of these are restructured by #236. Fixes a live bug on its own.
- **Part B — the action dialog.** A modal. #237 rewrites `confirmprompt`, the
  component Part B extends, into the #236 component shape. Building the dialog
  before #237 lands would either conflict with it or add exactly the debt #236
  exists to remove. Part B's requirements are recorded here so the design is
  not lost; it gets its own plan once #237 merges.

## Evidence

Both endpoints involved are undocumented. The design rests on captures from the
Slack web client, not on guesses.

**Inbound** — WebSocket frame for Slackbot's mention ephemeral (IDs replaced with placeholders):

```json
{"type":"message","subtype":"bot_message","channel":"C0EXAMPLE01",
 "text":"You mentioned <@U0EXAMPLE01>, but they’re not in this private channel.",
 "blocks":[{"type":"rich_text", "...": "..."}],
 "username":"slackbot","user":"USLACKBOT","bot_id":"B01",
 "ts":"1791541429.559220",
 "attachments":[{
   "callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0",
   "fallback":"You may want to invite them.","id":1,
   "actions":[
     {"id":"1","name":"invite","text":"Add Them","type":"button","value":"invite","style":"",
      "confirm":{"text":"New members will be able to see all of the channel's history, including any files that have been shared in the channel.",
                 "title":"Are you sure you want to add them?","ok_text":"Add","dismiss_text":"Cancel"}},
     {"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""},
     {"id":"3","name":"dont-show-again","text":"Don't Show Again","type":"button","value":"dont-show-again","style":""}]}],
 "is_ephemeral":true,"event_ts":"1791541429.004800"}
```

**Outbound** — the web client's request when "Dismiss" is pressed:

```
POST https://<workspace>.slack.com/api/chat.attachmentAction?_x_id=…&_x_csid=…&slack_route=T0EXAMPLE01&_x_version_ts=…&…
payload={"actions":[{"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""}],
         "attachment_id":"1",
         "callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541587716039_0",
         "channel_id":"C0EXAMPLE01","is_ephemeral":true,
         "message_ts":"1791541587.716040","prompt_app_install":false}
```

**Result** — Slack then removes the ephemeral over the WebSocket:

```json
{"type":"message","subtype":"message_deleted","channel":"C0EXAMPLE01","hidden":true,
 "deleted_ts":"1791541429.559220","event_ts":"1791541531.005300"}
```

What the captures establish:

- The buttons are **legacy attachment actions** (`attachments[].actions`), not a
  Block Kit `actions` block. slk parses the latter (as inert labels) and does
  not parse the former at all, which is why the buttons vanish.
- The message arrives as `subtype: bot_message`, so it passes
  `dispatchWebSocketEvent`'s subtype filter and reaches `OnMessage` — which
  caches it. `is_ephemeral` is not a field `wsMessageEvent` declares, so nothing
  downstream can tell it apart from a real message.
- The press echoes the action object **as received**, including `"id"`.
  slack-go's `AttachmentAction` has **no `id` field**, so decoding through
  slack-go drops it. Whether Slack requires it is unverified (see Part A §6).
- `attachment_id` is the attachment's numeric `id`, sent as a **string**.
- `slack_route` is the team ID. `Client.postForm` already adds it, with the
  rest of the `_x_*` envelope.
- After a press, cleanup arrives as an ordinary `message_deleted`, which
  `dispatchWebSocketEvent` already routes to `OnMessageDeleted` and the UI
  already handles (`WSMessageDeletedMsg`). No new code is needed for it.

## Part A — data, ingestion, rendering, wire call

### 1. Decoding (`internal/slack`)

- `wsMessageEvent` gains `IsEphemeral bool \`json:"is_ephemeral"\``.
- `EventHandler` gains a method:

  ```go
  OnEphemeralMessage(m EphemeralMessage)

  type EphemeralMessage struct {
      ChannelID, UserID, BotID, Username string
      TS, ThreadTS, Subtype, Text        string
      Blocks      slack.Blocks
      Attachments []slack.Attachment
      ActionIDs   [][]string // ActionIDs[i][j] = Attachments[i].Actions[j]'s "id"; see §6
  }
  ```

  A struct rather than positional parameters: `OnMessage`'s twelve positional
  strings and slices are easy to transpose, and this method is new.

  `dispatchWebSocketEvent` routes a `message` event with `is_ephemeral: true`
  to it instead of `OnMessage`, in the same subtype arm. Files are omitted —
  ephemerals do not carry them.

  **Why a separate method rather than an `ephemeral bool` on `OnMessage`:**
  `OnMessage` already takes 12 positional parameters across 17 call sites, and
  its body is ~270 lines of side effects an ephemeral must skip — the SQLite
  upsert, `SetChannelSyncedAt`, `AdvanceChannelLatestSyncedTS`, conversation
  discovery, desktop notification, `has_unread`, and the mention-badge
  increment. A flag means a guard on every one of those, and the next side
  effect added to `OnMessage` would have to remember it. A separate method skips
  them by construction.

  This supersedes the plan recorded in `ee2ae71` ("filter ephemerals at
  ingestion"): ephemerals are identified at ingestion, as that commit intended,
  but routed to the UI rather than dropped.

### 2. Model (`internal/core`)

- `core.MessageItem` gains `Ephemeral bool`.
- `blocks.LegacyAttachment` gains:

  ```go
  ID         int    // attachment id; sent back as attachment_id
  CallbackID string
  Actions    []LegacyAction
  ```

- New types in `internal/core/blocks`:

  ```go
  // LegacyAction is one button in a legacy attachment's `actions`.
  type LegacyAction struct {
      ID      string // echoed back verbatim on press; see §6
      Name    string
      Text    string // button label
      Type    string // "button"; "select" is parsed but not actionable
      Value   string
      Style   string // "", "default", "primary", "danger"
      URL     string // set → the button is a link, not a Slack call
      Confirm *ActionConfirm
  }

  type ActionConfirm struct {
      Title, Text, OKText, DismissText string
  }
  ```

- `blockkit.parseAttachment` (`internal/ui/messages/blockkit/parse.go`) copies
  these from `slack.Attachment`. This is the single conversion choke point:
  `cmd/slk/extractLegacyAttachments` calls it from the WebSocket path, all three
  history paths and export, so non-ephemeral bot messages with legacy buttons
  render them too.

### 3. Ingestion (`cmd/slk/rtm_handler.go`)

`rtmEventHandler.OnEphemeralMessage`:

- **Inactive workspace → drop.** There is nowhere to show it, and it is not
  recoverable later (Slack never returns ephemerals in history).
- Resolves the author's name and builds the `MessageItem` exactly as
  `OnMessage`'s tail does today. **That tail is extracted into one helper used
  by both methods** — not copied. It is the code from `resolveUserCached`
  through the `messages.MessageItem` literal.
- Sends `ui.NewMessageMsg` with `Message.Ephemeral = true`.
- Touches nothing else: no SQLite, no watermarks, no discovery, no
  notification, no unread or mention-badge write.

### 4. UI ingestion (`internal/ui/reducer_send.go`)

`reduceNewMessage` handles an ephemeral like any new message — appended to
every pane viewing the channel, and to the thread panel when it is a reply in
the open thread — except it **must not**:

- call `recordChannelMark` or `recordThreadMark`. These are the only two
  read-marking entry points (both called only from here); staging an ephemeral
  ts would advance the read cursor to a message Slack does not know about. This
  is the live-message counterpart of the bug `3aa044b` fixed for cached
  replies.
- call `IncrementReplyCount` — an ephemeral reply is not a reply.
- call `notifyReadStateChanged` — it changed no read state.

### 5. Rendering

- **Buttons.** `blockkit/attachments.go` renders `Actions` as a row inside the
  attachment's bar, after fields and nested blocks and before the footer. It
  **reuses `appendActions`** by mapping each `LegacyAction` to an
  `ActionElement{Kind: "button", Label: Text}` — no second button renderer.
  `select` actions render as `appendActions` already renders selects. `Style`
  is not rendered in Part A.
- **`fallback` is not rendered**, matching Slack's web client: it is
  notification text, and the message body already says the same thing.
- **Ephemeral marker.** A muted `Only visible to you` line above the author
  row — the slot the `↳ replied to a thread` label already uses, so the
  messages pane's row math (`preAttachmentRows`) absorbs it the same way — in
  **both** `messages.Model` and `thread.Model` (AGENTS.md: change one, change
  both). One shared `messages.EphemeralLabel()` renders it for both.
- **The existing `↗ open in Slack to interact` hint** still follows any
  interactive render, including these buttons. It stays in Part A; Part B
  replaces it for messages slk can press.
- **Lifetime.** Ephemerals live only in pane memory. Switching away and back
  reloads the pane from cache and history, neither of which contains them, so
  they disappear — as they do on reload in Slack's web client. Accepted.

### 6. The wire call (`internal/slack`)

```go
// AttachmentAction presses a legacy attachment button, as Slack's web
// client does (chat.attachmentAction, undocumented).
func (c *Client) AttachmentAction(ctx context.Context, req AttachmentActionRequest) error
```

`AttachmentActionRequest` carries `ChannelID`, `MessageTS`, `AttachmentID int`,
`CallbackID`, `Ephemeral bool` and the `LegacyAction`. The method marshals the
`payload` field in exactly the captured shape — `actions` as a one-element
array holding the action as received, `attachment_id` as a decimal string,
`prompt_app_install: false` — and sends it with
`c.postForm(ctx, "chat.attachmentAction", url.Values{"payload": {…}})`.
`ok: false` is returned as an error carrying Slack's `error` string.

The payload is marshalled from slk's own structs, not slack-go's, so the shape
is under test control: top-level keys in the captured (alphabetical) order;
the action object as `id, name, text, type, value, style` with `style` kept
even when empty, as captured. A pressed action that carried a `confirm` is
echoed with its `confirm` object, "as received" — the capture only covers
"Dismiss", which has none, so this is the one unverified detail of the shape
(see below).

**The action `id` is always sent.** Whether Slack strictly requires it is
unknown, and it does not matter: slk mimics the web client's requests
deliberately, because a request shape the web client never produces is the
kind of contradictory signature that trips Enterprise Grid anomaly detection
(`postForm`'s comment, issue #5). So `dispatchWebSocketEvent` captures
`attachments[].actions[].id` with a second, narrow decode of the same frame —
slack-go's `AttachmentAction` does not declare the field — and carries it to
`LegacyAction.ID`.

That capture is on the WebSocket path only. History paths decode through
slack-go and leave `ID` empty; nothing in Part A presses a button. Part B's `b`
key would press buttons on non-ephemeral messages loaded from history, so Part
B either extends the capture to history or restricts `b` to ephemerals — a Part
B planning decision.

Verification is by construction rather than a live call: a unit test asserts
the marshalled `payload` is byte-equal to the captured web-client payload,
which Slack accepted. The first real press — and the one unverified detail,
`confirm` echo — is checked by hand in Part B, the first time a button is
pressed from slk.

The `core` port and the UI wiring that call this method land with Part B.

### 7. Error handling

- Malformed or absent `actions` → no button row; the message still renders.
- `is_ephemeral` absent → today's path, unchanged.
- `AttachmentAction` returns transport errors, HTTP status errors and `ok:false`
  as `error`; it never retries. Retry is a user decision (press again).

### 8. Testing

All stdlib `testing`, white-box, per AGENTS.md.

- **`internal/slack`:** the captured inbound frame (verbatim apart from the placeholder IDs)
  routes to `OnEphemeralMessage`, not `OnMessage`, with `ActionIDs`
  `[["1","2","3"]]`; the same frame without
  `is_ephemeral` still routes to `OnMessage`. `AttachmentAction` against an
  `httptest` server: method path `chat.attachmentAction`, the `payload` field
  byte-equal to the captured payload, `ok:false` surfaced as an error.
- **`blockkit`:** `parseAttachment` maps `ID`, `CallbackID`, every action field
  and `Confirm`; render shows all three labels inside the bar, wraps at narrow
  width, and does not render `fallback`.
- **`cmd/slk`:** `OnEphemeralMessage` with a real test DB writes no message row
  and leaves `latest_synced_ts` and `has_unread` unchanged; sends a
  `NewMessageMsg` with `Ephemeral` set; drops when the workspace is inactive.
  The extracted `MessageItem` helper keeps `OnMessage`'s existing tests green
  unchanged.
- **`internal/ui`:** an ephemeral `NewMessageMsg` for the active, focused
  channel is appended but stages no channel mark; an ephemeral reply in the open
  thread stages no thread mark and does not bump the parent's reply count. The
  marker renders in both panes.
- The 8 App-level goldens stay byte-identical (no golden fixture is ephemeral
  or carries legacy actions).

### 9. Documentation

- `AGENTS.md` shared-code table, same commit as the code: the extracted
  WS-message → `MessageItem` helper, and `Client.AttachmentAction`.

## Part B — the action dialog (blocked on #237)

> **Superseded** by `docs/superpowers/specs/2026-10-10-ephemeral-action-dialog-design.md`,
> the design against the `confirmprompt` component #302 landed. The section
> below is kept as the record of what was agreed before that component existed.

Recorded so the agreed behavior survives until #237 lands. Part B gets its own
plan then; nothing below is built in Part A.

### Shape

- **No new modal package.** Part B extends #237's `confirmprompt` from a yes/no
  prompt to *a message plus N buttons*, per the principle on #236: every stage
  reduces the number of implementations, never adds one. The existing yes/no
  `Open` keeps its keys and gains a visible, clickable button row as a side
  effect — the delete, quit and forward confirms become clickable too.
- **The dialog presses buttons itself.** Per #236 question 2, pressing is
  intent from the dialog whose success lands nowhere (the WebSocket
  `message_deleted` does the cleanup) and whose failure is a notice. So the
  dialog receives a `PressFunc` at `New`, backed by a `core` port wrapping
  `Client.AttachmentAction`. Failures go through the shared notice message, not
  a new `*FailedMsg`.

### Behavior

- **Auto-open.** An ephemeral with actions, in the channel or open thread on
  screen, opens as a dialog: sender as title, flattened message text as body
  (`messages.FlattenMrkdwn`), one button per action.
- **Interruption rule.** It opens immediately **unless** another modal is up or
  the user is in insert mode with unsent composer text. Otherwise it waits, and
  a toast says "<sender>: press `b` to respond". Pending dialogs are re-checked
  when a modal closes, when insert mode is left, and after a send clears the
  composer.
- **Queue.** Several arriving together open one at a time. A `message_deleted`
  for a queued or open message removes it, or closes the dialog.
- **Keys.** `h`/`l`, ←/→ and Tab move; `1`–`9` press directly; Enter presses the
  highlighted button; Esc closes without pressing. **No button starts
  highlighted** — a stray Enter does nothing, which matters for `/giphy`, where
  the first button is "Send".
- **Mouse.** Clicking a button presses it, via `reduceModalClick`'s
  point-hit-test path (`ClickAt` selects, the router sends the activation).
  Clicking outside dismisses.
- **Confirm.** A button with `confirm` swaps the dialog content in place to the
  confirm title and text with `[DismissText] [OKText]` (Slack's labels,
  defaulting to Cancel / Okay); Cancel returns to the button list.
- **URL buttons** open the link through the existing `OpenLinkMsg`.
- **`b`** (normal mode, selected message) opens the dialog for any message with
  legacy actions; a toast otherwise. Esc'd dialogs are reopened this way.
- **After pressing**, the dialog closes immediately; on failure a toast reads
  "Couldn't send <label>: <error>" and the message remains for a retry.

### Explicitly out of scope for both parts

- Block Kit `actions` buttons (`blocks.actions` — no capture yet).
- `select` menus.
- Slash commands themselves (sub-project 2).
- GIF animation.
