# Send link chips

Status: implemented (SLK-272, second half; the first half, PR #375, renders
chips that other clients sent).

## Problem

Paste a Linear issue URL into Slack's composer and it turns into a chip:
the issue's title in place of the URL. The same URL sent from slk stays a
URL, for everyone who reads it.

## What Slack does

From three captures of the official web client sending a Linear link
(scrubbed and anonymized in `internal/slack/testdata/`):

1. On paste the composer calls `chat.unfurlLink` (`url`, `channel`,
   `client_msg_id`, `_x_reason=fetch-unfurl-preview`). The reply holds no
   title, only `is_app_unfurl: true, is_preview_pending: true` and the
   app's id (`chat_unfurl_link.json`).
2. About 2.5s later the WebSocket delivers `unfurl_preview_updated` with
   the same `client_msg_id`. Its `attachments[<url>].work_object_entity`
   is the app's Work Object: `entity_id`, `app_id`, `product_name`,
   `slug_icon_name`, `app_icons.image_36.image_url`, and the title in
   `layouts.expanded.title.text` (`ws_unfurl_preview_updated.json`).
3. `chat.postMessage` (`_x_reason=webapp_message_send`) sends the link as
   `{"type":"attachment_mention", url, text: <title>, entity_id, app_id,
   icon_url, icon_name, product_name}` in the rich_text block, with
   `unfurl=[{"url":…}]` and a `client_msg_id`
   (`chat_post_message_chip.json`).

Not explained by any capture: GitHub links reportedly become chips too,
but the GitHub message captured is a plain `<url|label>` link with no
`attachment_mention`. Only apps that publish Work Objects are covered.

## Behaviour

At send time (`SendMessage`, `SendReply`; edits are unchanged):

- Every distinct bare http(s) link (no label of its own, no style, not
  code; at most 5) is looked up with `chat.unfurlLink`, concurrently, each
  with its own `client_msg_id`, registered before the call because the
  frame can beat the reply.
- The send waits at most 3s (`linkChipWait`) for the answers. A reply that
  is not a pending app unfurl ends that link's wait at once; so does the
  first `unfurl_preview_updated` for it, chip or not.
- Links with a Work Object (id, app, title) go out as chips; the message
  is posted by hand (`postForm`) with `channel`, `text`, `blocks`,
  `unfurl` (the chip URLs), `client_msg_id`, and `thread_ts` /
  `reply_broadcast` for replies. Fields only the web client's drafts use
  (`ts`, `type`, `xArgs`, `draft_id`, `client_context_team_id`,
  `include_channel_perm_error`) are not sent. `text`, the mrkdwn
  fallback, is: slk sends it with every message, with each chip written as
  `<url|title>`.
- Everything else goes out exactly as before, through slack-go: messages
  without links (no lookup, no wait), links without a Work Object, no
  answer within 3s, or no live WebSocket (no lookup). If Slack refuses the
  chips as `invalid_blocks`, nothing was posted, so the message is sent
  again with plain links. Any other refusal is the send's error.

The WebSocket event owner takes `unfurl_preview_updated` frames off the
socket before dispatch (`unfurlWaiters.deliver`, non-blocking) and hands
each to the lookup waiting for its `client_msg_id`. `EventHandler` does not
change.

## Consequences

- A message with links can take up to 3s longer to post. The web client
  avoids that by looking up on paste; doing so in slk needs the composer
  to ask through the app layer and show the chip while typing, a much
  larger change.
- slk drops the WebSocket echo of its own sends (`internal/ui/selfsend.go`),
  and the optimistic row shows the returned mrkdwn. A send now keeps its
  channel's self-user echoes suppressed until its answer lands
  (`BeginSend` / `EndSend`, at most `selfSendPendingLimit`), not just for
  `selfSendWindow` (3s, unchanged), so a delayed send's echo is still
  dropped while the user's messages from another client show as soon as
  it has answered. The returned mrkdwn carries `<url|title>`, so the
  sender sees the title too.
- A title with `*`, `_`, `~` or backticks is formatted in slk's rows, the
  sender's and (through #375) every reader's: slk applies mrkdwn
  formatting inside link labels, for every labelled link. Slack mrkdwn
  has no escape for those characters; the fix belongs in the renderer.
- A `<url>` written literally inside a code span is rewritten in the
  mrkdwn fallback if the same URL is also a chip. Only the fallback is
  affected; the blocks are exact.
