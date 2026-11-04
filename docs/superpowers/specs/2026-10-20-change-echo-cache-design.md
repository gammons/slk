# A change echo updates the cached row, it does not replace it

## The bug

`rtmEventHandler.OnMessage` (`cmd/slk/rtm_handler.go`) caches every
message it gets, including a `message_changed` echo (an edit, an unfurl
landing, a link preview removed). It builds a `slack.Message` from the
event's fields, marshals it into `raw_json` and calls
`cache.DB.UpsertMessage`, whose `ON CONFLICT` branch overwrites every
column.

What reaches `OnMessage` of the echo is partial. The dispatcher
(`internal/slack/events.go`, the `message_changed` case) decodes no
reply count (`wsSubMsg` has no such field) and passes an empty subtype.
So a change echo rewrites the cached row with:

- `reply_count = 0`: a thread parent reopened from cache shows no replies.
- `subtype = ''`: a `thread_broadcast` reopened from cache loses its
  broadcast marker, and `GetMessages` (which keeps a reply in the
  channel feed only when `subtype = 'thread_broadcast'`) drops it from
  the channel feed altogether.
- a `raw_json` with only what the echo had, so every other key a
  history fetch stored (`reply_count`, `subtype`, `reply_users`,
  `latest_reply`, `bot_id`, `username`, ...) is gone too.

The live panes are fine since the `message_changed` fix (the previous
commit): they apply the echo with `MessageItem.ApplyChange`, which
replaces only the content.
The cache never got the same treatment. This makes it match.

## The fix

`OnMessage` already gets `changed` (this is a `message_changed`
re-delivery). When it is set, the cache write is a content update of
the existing row instead of an upsert:

```go
// internal/cache/messages.go
type MessageChange struct {
	Text        string
	ThreadTS    string          // fills thread_ts only on a row that has none
	Files       json.RawMessage // nil or empty: the message has none
	Blocks      json.RawMessage
	Attachments json.RawMessage
}

// ApplyMessageChange replaces the content of a cached message and keeps
// everything else. Reports whether a row was there to change.
func (db *DB) ApplyMessageChange(channelID, ts string, c MessageChange) (bool, error)
```

One `UPDATE ... WHERE channel_id = ? AND ts = ?`:

- `text` is set to the echo's text.
- `raw_json` is merged with SQLite's `json_patch` (RFC 7396): the patch
  sets `text`, and sets `files`, `blocks` and `attachments` to the
  echo's arrays, or to `null` (which removes the key) when the echo
  has none. Removing the last preview must remove `attachments`, not
  keep the old one. Every other key is left as the history fetch wrote
  it. A row with an empty or invalid `raw_json` (rows cached before
  raw_json existed) is patched onto `{}`.
- `reply_count`, `subtype`, `user_id`, `is_deleted`, `created_at` and
  `edited_at` are not touched.
- `thread_ts` is kept, unless the row has none: then it takes the
  echo's. A thread parent first cached from the WebSocket has
  `thread_ts = ''` (its first delivery had none) until a history fetch,
  and `GetThreadReplies` leaves such a parent out, so the cached thread
  open would drop the first reply in its place. The old upsert filled
  it by accident; this keeps that, and never blanks or moves a
  `thread_ts` the row already has.

The merge is a single statement, so nothing written to the row between
a read and a write can be lost, as it could be with a read, a merge in
Go and a write back. It does not order the echo against a history
fetch: a fetch whose response predates the edit and is written after
it still overwrites the new content with the old (until the next
fetch). That ordering problem exists for every cached message today
and is not this fix.

Updating `text` fires the existing FTS update trigger
(`internal/cache/search.go`), so search sees the edit with no extra
work. The `is_deleted = 0` guard keeps a late echo from changing a
soft-deleted row.

The reload path (`enrichCachedRow` in `cmd/slk/history.go`) reads
reply count, subtype and thread_ts from the columns and files, blocks
and attachments from `raw_json`, so keeping the columns and patching
those three keys is what it needs. No change there.

### A change for a message that is not cached

Not written. `ApplyMessageChange` reports false and `OnMessage` logs it.
Inserting the echo as a new row would create exactly the misleading
partial row this fix removes (no reply count, no subtype, no bot
identity), possibly far back in a channel's history where the cache has
no neighbours, and the next history or thread fetch that reaches the
message writes the full row anyway. The live pane still gets the echo:
the UI dispatch does not depend on the cache write.

### Watermarks (separate commit)

A change echo also stops calling `SetChannelSyncedAt` and
`AdvanceChannelLatestSyncedTS`. This is a behaviour change of its own,
so it is its own commit with its own test.

`synced_at` drives the channel-open freshness tiers
(`reduceChannelSelected` in `internal/ui/reducer_channels.go`): a
channel synced under 30s ago renders from cache with no fetch.
Reconnect zeroes it for every channel but the active one
(`MarkChannelsStale`) so the next open refetches what was missed. A
change echo for an old message in such a channel set it back to now,
so opening that channel within 30s showed the cache without the
messages missed while offline. A change is not a fetch and not a new
arrival, so it no longer touches `synced_at`.

`latest_synced_ts` has no reader outside `internal/cache` today (the
reconnect backfill that used it is gone). Advancing it from an echo
claimed "nothing missing at or below this ts" on the strength of a
re-delivery, which proves nothing about the messages around it. New
messages keep both writes as before.

### Payload completeness

The fix treats the echo's files, blocks and attachments as the
message's current content: an absent array means none. That is what
Slack documents for `message_changed` (the inner `message` is the
updated message, which clients replace), and it is what the live
panes already do (`MessageItem.ApplyChange`). There is no captured
`message_changed` frame in the repo for a file message or a bot
update; if one ever shows an unchanged array omitted, the panes and
the cache are wrong together and both need presence-aware decoding.

`message_changed` also covers more than content edits (a deleted
thread parent becomes a `tombstone` with Slackbot as author). The
dispatcher drops the inner subtype, so that is not handled here, as
before.

### edited_at

Left alone. Nothing in the repo writes or reads `edited_at` (every
writer passes `""`), and the dispatcher passes only an `edited` bool,
not the edit's timestamp. The "(edited)" mark is live-only today: the
history fetch and the cache reload never set `MessageItem.IsEdited`.
Persisting it is a separate change (pass `Edited.Timestamp` through
`OnMessage`, write `edited_at`, read it back in `enrichCachedRow` and
the history fetch).

## Tests

White-box, plain `testing.T`.

`internal/cache` (`message_change_test.go`):

- a cached thread parent (reply_count 3, raw_json from a full
  `slack.Message` with `reply_count`, `thread_ts`, attachments) gets
  a change: text and attachments change, reply_count / subtype /
  thread_ts columns and the raw_json `reply_count` key are unchanged.
- an empty change (`nil`, `null` or `[]`) removes the key (the last
  preview removed).
- a broadcast keeps its subtype and stays in `GetMessages`.
- `thread_ts` is filled when empty and kept otherwise.
- a change for a missing row, or a deleted one, reports false and
  writes nothing.
- a row with empty or malformed raw_json gets a valid raw_json with
  the content; `bot_id` / `username` in raw_json survive a change.
- the same ts in another channel is not touched.

`cmd/slk` (`change_echo_cache_test.go`), through
`OnMessage(..., changed=true, ...)` with the dispatcher's empty subtype:

- thread parent with replies, edit echo: reload via
  `loadCachedMessages` shows the new text and `ReplyCount` 3, and
  `loadCachedThreadReplies` still opens with the parent.
- thread parent, unfurl echo (attachments added, not edited): reload
  shows the attachment and `ReplyCount` 3.
- `thread_broadcast` reply, edit echo and unfurl echo: reload via
  `loadCachedMessages` still includes it with `Subtype ==
  "thread_broadcast"`, and `loadCachedThreadReplies` still has it.
- a change echo for an uncached message leaves the cache empty.
- a parent cached only from the WebSocket (no history fetch) gets an
  echo with `thread_ts = ts`: `loadCachedThreadReplies` opens with it.
- a new message (`changed=false`) still upserts as before.
- (watermark commit) a change echo leaves `synced_at` and
  `latest_synced_ts` as they were; a new message still sets them.
