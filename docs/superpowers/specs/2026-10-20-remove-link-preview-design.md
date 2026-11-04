# Remove a link preview

Status: implemented.

## Problem

When you post a link, Slack unfurls it into a preview (a legacy attachment
on your message). The desktop client lets the poster remove it with the
"x" on the preview. slk cannot, so the user has to open Slack to do it.

## What Slack does

The web client calls an internal method, `chat.deleteAttachment`. It is
not in the public API docs. What is known, and from where:

- Observed in emacs-slack's `slack-remove-preview` (a working
  implementation): the method, the form fields `channel`, `ts`,
  `attachment`, and `_x_reason=delete-single-attachment`. It sends
  multipart form data and always `attachment=1`.
- Stated by the C++ client "make-slack-great-again" (source comment, not
  verified here): `attachment` is the attachment's 1-based `id`, Slack
  renumbers the rest after a removal, own messages only, session (`xoxc`)
  token only. slk uses a session token.
- Assumed: Slack then sends `message_changed` with the attachment gone.

slk sends the fields url-encoded, like every other `postForm` call; the
web client's encoding does not matter to Slack's API. The request test pins
the shape above and names its sources. A scrubbed HAR of one "Remove
preview" from the Slack web client would replace that, and is worth asking
for before an upstream PR.

## Behaviour

New normal-mode key `x` ("remove link preview"), matching Slack's "x":

- Selected message is not the user's own: toast "You can only remove
  previews from your own messages".
- No removable preview: toast "No link preview on this message".
- One preview: confirm prompt "Remove preview?" showing its title (or URL),
  buttons Remove / Cancel.
- Several: the same prompt via `OpenChoice`, one button per preview
  (truncated title) plus Cancel.
- On failure: toast with the reason. On success nothing is done locally;
  the `message_changed` echo updates the message (see below).

A removable preview is a legacy attachment with a URL it unfurls:
`FromURL != "" || OriginalURL != ""`. Bot cards without either are left
alone. `OriginalURL` is added to `blocks.LegacyAttachment` and parsed in
`blockkit.parseAttachment`; slack-go declares it, so it survives the
re-marshalled cache. The id sent is `ID`, or the 1-based position when
`ID == 0`.

This includes the card of a message the user shared (forwarded), which
also carries `from_url`; slk cannot tell the two apart (`is_share` and
`is_msg_unfurl` are not decoded by slack-go and do not survive the cache).
The confirm prompt names the card, so the user sees what goes, and Slack
refuses whatever it does not allow.

`x` joins `isMessageOpKey`, so it is refused on an ephemeral like the other
message operations.

### Stale ids

Ids are renumbered after a removal, so an id is only good against the
attachment list it was read from:

- **Pending removal per message.** While a removal for (channel, ts) is in
  flight, `x` on that message toasts "Removing preview…" and does
  nothing. It clears when the `message_changed` echo replaces the
  attachments, or when the call fails. A call that times out stays pending
  until the echo, a reload of the message, or 30 seconds, whichever comes
  first; the failure toast says to check the message.
- **Re-check on confirm.** The prompt's button carries the attachment's id
  and its URL. When pressed, the App looks the message up again in the pane
  and sends only if the attachment with that id still has that URL;
  otherwise it toasts "That preview changed, try again".
- **Workspace pinned.** The request carries the team ID the key was
  pressed in, and `cmd/slk` uses that workspace's client (`router.ByID`,
  as `/` commands do), not the active one.

## Design

- `internal/slack`: `(*Client).DeleteAttachment(ctx, channelID, ts string,
  attachmentID int) error`, mirroring `AttachmentAction`: `postForm`,
  `parseOKResponse`, `slackhttp.WithReason(ctx, "delete-single-attachment")`.
  slk adds no retry, including on 429 (`postForm` makes one `Do`, and
  `DeleteAttachment` returns a 429 as `*slack.RateLimitedError`; nothing above
  it may wrap this call in `WaitOutRateLimit`): a lost answer is ambiguous
  and a resend can hit a renumbered id.
- `core.MessageService.RemoveAttachment(teamID, channelID, ts string,
  attachmentID int) tea.Msg` returning `AttachmentRemovedMsg{ChannelID, TS,
  Err}`, nil-safe in `MessageServiceFuncs` like the others. Wired in
  `cmd/slk` to that workspace's client with the same 10s timeout as
  `Delete`.
- UI: `(*App).removePreviewOfSelected()`, using `selectedMessageInFocus`
  and `isOwnMessage`, then `openConfirmPrompt` / `OpenChoice`. Reducer arm
  for `AttachmentRemovedMsg` toasts only on error.

## Prerequisite: apply `message_changed` fully (existing bug)

Today a `message_changed` echo only replaces the text:
`UpdateMessageInPlace` on `messages.Model` and `thread.Model` (and
`thread.Model.UpdateParentInPlace`) swap `Text` and force `IsEdited =
true`. Blocks, legacy attachments and files on the echo are dropped. So:

- a removed preview would stay on screen until the channel reloads from
  cache;
- the preview Slack adds a moment after you post a link never appears live;
- that same unfurl stamps "(edited)" on a message nobody edited, because
  `wsSubMsg` does not decode `edited` and every change is treated as an
  edit.

Fix, as its own commit before the feature:

- Decode `edited` in `wsSubMsg`. Split the two meanings the one `edited`
  bool carries today through the **whole** path, not just the UI:
  - "this event is a change of an existing message" (`changed`): the
    `EventHandler.OnMessage` parameter that `rtm_handler.OnMessage` uses to
    skip unread marking and mention badges, and `NewMessageMsg.Changed`
    for the routing in `reduceNewMessage` (which must still run before
    the self-sent dedup, reply-count increments and read marking);
  - "Slack says it was edited" (`edited`): only `MessageItem.IsEdited`,
    for the "(edited)" mark.
  Repurposing the existing bool would make an unfurl on someone else's
  message mark the channel unread again.
- A change echo no longer fires a desktop notification
  (`rtm_handler.OnMessage` has no guard today, so an edit or an unfurl on
  a message that mentions you notifies again).
- The in-place updates take the changed `MessageItem` and copy its content
  fields: `Text`, `IsEdited`, `Blocks`, `LegacyAttachments`,
  `Attachments`. Reactions, reply counts and the rest are kept. The
  method is added to the `contentPane` interface so both panes have it,
  with one test that drives the same echo through both and the thread
  parent. The thread pane does not draw "(edited)" (`EditedMark` is off
  there), which stays as is.

Known limits, not changed here:

- A change echo that arrives before the send's HTTP answer finds only the
  optimistic `local:` row and is dropped, as text edits are today. Slack
  usually unfurls after the answer; when it does not, the preview shows on
  the next load.
- `rtm_handler.OnMessage` caches a change echo as a partial message, which
  resets the cached reply count and subtype of an edited thread parent or
  broadcast. Pre-existing for edits; recorded for a separate fix.
- The 30-second end of a removal whose outcome is unknown is a trade-off,
  not a guarantee: if Slack did remove the preview and both its answer and
  the echo were lost, the pane still shows the old list after 30 seconds, and
  a second removal could name an id Slack has renumbered. Waiting for fresh
  server state instead would leave `x` stuck on such a message until a
  reload.
- A thread fetch does not refresh a parent the thread pane already shows, so
  a confirmed removal of a thread parent whose echo was lost lasts until the
  thread is opened again from a reloaded channel.
- The parent of a thread opened from the Threads view is built from a text
  summary, so `x` there finds no preview. Open it from the channel instead.
- The Threads and Activity views keep their own copies of message bodies
  and do not take change echoes.
- The history path never sets `IsEdited` (messages loaded from history
  show no "(edited)"). Separate gap.

## Tests

- slack: form fields and reason match the documented shape; an error
  response comes back as an error; no retry on 429. (As built, see the
  implementation notes: a refusal and a 429 come back typed, and an
  unreadable answer is not a refusal.)
- parse: `OriginalURL` carried; removable-preview selection (from_url,
  original_url, neither, `ID == 0`).
- echo: a `message_changed` without `edited` replaces attachments and does
  not mark "(edited)"; with `edited` it does; both panes and the thread
  parent; a change from someone else does not mark the channel unread or
  notify.
- keys: `x` on own message with one / several / no previews, on someone
  else's message, on an ephemeral; the confirm button sends the right id
  and team; a second `x` while pending is refused; a confirm whose
  attachment changed underneath is refused; a failed removal toasts and
  clears the pending state. (As built: only a definite refusal clears it,
  see the implementation notes.)
- help golden re-blessed.

## Docs

`wiki/Keybindings.md`, `wiki/Features.md`, `docs/STATUS.md` (link
previews), and `wiki/Tradeoffs-and-Non-Goals.md`, which lists "link
unfurls" as a non-goal and needs rewording. AGENTS.md lists
`Client.DeleteAttachment` and the preview-selection helper.

## Implementation notes

Where the code differs from, or fills in, the design above:

- `core.MessageService.RemoveAttachment` takes `ids.ChannelID` and
  `ids.MessageTS` like its neighbours (`Delete`, `Forward`), not plain
  strings; `teamID` stays a string.
- The prerequisite carries `changed` and `edited` as two adjacent bools on
  `EventHandler.OnMessage` (`..., subtype string, changed, edited bool,
  ...`). The shared field copy is `core.MessageItem.ApplyChange`; both panes
  and the thread parent call it. `UpdateMessageInPlace` and
  `UpdateParentInPlace` now take the changed `MessageItem`.
- The removable-preview selection is `messages.RemovablePreviews`
  (`internal/ui/messages/previews.go`); its `Label()` is the title, else the
  URL.
- The pending state names the targeted preview's URL and how many previews
  of that URL the message showed, not the whole attachment list. It ends when
  the message shows fewer previews of that URL (counting, not looking for the
  (id, URL) pair, because Slack renumbers a twin of the same URL into the
  removed one's id):
  the `message_changed` echo (an echo that still shows it, such as a text edit
  from another client or a second unfurl landing, leaves it pending), or a
  reload (checked at the next `x`, there is no hook on the load path). It also
  ends on a definite refusal, and, for a call whose outcome is unknown, after
  30 seconds.
- Each removal has a sequence number from a counter that never resets. The
  UI stamps it on the `AttachmentRemovedMsg` its call returns, and the
  reducer ignores an answer whose number is not the pending removal's: the
  echo can end removal A before A's HTTP answer, and a removal B on the same
  message can start in between. A's late answer must not confirm or end B.
- A removal Slack answered ok for is marked confirmed and does not expire: if
  the echo never arrives, `x` toasts "Preview removed, reload the channel to
  see it" and sends nothing. The pane is not changed optimistically, since
  Slack's renumbering is not verified.
- The prompt's confirm answers with `RemovePreviewMsg`; the re-check, the
  pending state and the call live in `internal/ui/remove_preview.go`, the
  reducer for both results in `internal/ui/reducer_remove_preview.go`.
  Only a definite refusal clears the pending state:
  `core.AttachmentRefusedError`, which `cmd/slk/removeAttachment` makes of
  Slack's `ok:false` (`slackclient.AttachmentRejectedError`), of an HTTP 429
  (`*slack.RateLimitedError`) and of a call never sent (no client for the
  team). It toasts "Could not remove the preview: <reason>", the reason being
  Slack's bare code, never the response body. Two `ok:false` codes are not
  definite: Slack documents `internal_error` and `fatal_error` as possibly
  following a partial success. Those, and every other error (a 5xx, a
  dropped connection, an unreadable answer or one without an `ok` field),
  leave it unknown whether Slack acted: the removal stays pending, like a
  call that hits the 10s timeout. The timeout toasts "Removing the preview
  timed out, check the message", the others "No clear answer from Slack,
  check the message".
- The confirm re-checks for a removal in flight too, not only `x`: the
  prompt closes before its answer is reduced, so a second `x` can open
  another prompt for the same message in between.
- The confirm re-check finds the message with `findActionMessage`, which
  looks a thread parent selected in the thread pane up in that pane first
  (`ParentMsg`), then in the channel windows: the thread pane can be the
  only one holding it.
- A message can be held twice: as the open thread's parent (or a broadcast
  reply) and in the channel windows. One copy can be reloaded while the
  other is not (Greptile on #349: remove A of A and B, lose the echo, reload
  the channel; the thread still shows A with its old id, which Slack gave
  to B). So `x` and the confirm both check `previewCopiesAgree` first: every
  copy (the thread pane's, and every channel window's in any view, since a
  window Activity covers can be acted on again when the view returns) must
  offer the same previews, ids and URLs in order. If not, they toast "This
  message changed, close and reopen the thread" (a channel reload
  refreshes the channel windows, not the thread pane) and send nothing. The check runs
  before the pending state is read, so disagreeing copies neither end a
  removal (the lazy reload check in `previewRemovalState`) nor start one.
  `previewRemovalState` itself does not compare copies: its only other
  caller is the `message_changed` echo, which is newer than any copy. When
  the copies agree, `findActionMessage` still picks the one used.
- The chooser offers at most nine previews, one per digit key, so a prompt
  of long titles (a button row each) keeps its Cancel button on a short
  terminal. With more, its body says it shows the first nine; the rest can
  be removed once some of these are gone.
- The help golden did not change (the new binding is below the first page
  the golden captures), so it was not re-blessed.
- Not done, as designed: the `message_changed` echo that arrives before the
  send's HTTP answer is still dropped (see the known limits).
