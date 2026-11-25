# Slack drafts: composer sync and a Drafts list

## Goal

slk's unsent composer text becomes Slack's own draft for that conversation,
the same draft the desktop, web and mobile clients show. A draft typed in slk
appears on the user's phone; a draft started on the phone is in slk's composer
when the user opens that conversation; sending from slk marks the draft sent.
A `:drafts` list shows every active draft in the workspace, jumps to its
conversation and deletes it.

Today slk's drafts live only in memory (`compose.Model.drafts`, keyed by team,
channel and thread), are lost on quit and are invisible to Slack.

## Evidence

Three web-client captures (Chrome, 2026-11-25), scrubbed into
`2026-11-25-slack-drafts-captures/` in the `{method, note, form, response}`
shape `capturedForm` reads:

| File | What it shows |
|---|---|
| `drafts_list.json` | Boot: `drafts.list` with `is_active=true`, `limit=25`, paged by `next_ts` (the oldest `last_updated_ts` so far) while `has_more`. Trimmed to one draft of each kind |
| `drafts_list_active.json` | Boot: `drafts.listActive`, ids only. slk does not need it |
| `drafts_create_autosave.json` | Typing in a composer: `drafts.create` with `_x_reason=MessageInput:updateDraft`, `client_msg_id` (fresh UUID), `blocks`, `destinations`, `attachments=[]`, `file_ids=[]`, `is_from_composer=false` |
| `drafts_update_autosave.json` | Later typing: `drafts.update` with `draft_id`, `client_last_updated_ts`, and the create fields minus `client_msg_id` |
| `drafts_update_stash.json`, `drafts_create_stash.json` | Slack's "Save as draft" button (`_x_reason=draft-management-save-as-draft`, `is_from_composer=true`, `date_stashed`). Reference only |

What the captures establish:

- **`client_last_updated_ts` is the time of the last local edit, in unix
  milliseconds**, not the request time (the update went out at 14:44:09.020
  carrying 14:43:58.196). Slack echoes it as `"<ms>.000000"`.
- **Two kinds of draft.** An *attached* draft (`is_from_composer=false`) is a
  conversation's in-progress composer text, one per destination. A draft with
  `is_from_composer=true` is either stashed by "Save as draft" (several may
  share a destination) or an unaddressed "New message" draft
  (`destinations: []`).
- A DM destination reads back with `user_ids` (Slackbot's is `["USLACK"]`).
  The web client writes `[{"channel_id": …}]` only.
- A thread destination is `{"channel_id", "thread_ts", "broadcast": false}`.
- The response to create and update is `{ok, draft: {…}, files: []}`; the
  draft carries `id`, `last_updated_ts`, `blocks`, `destinations`, `file_ids`,
  `date_scheduled`, `is_from_composer`, `is_deleted`, `is_sent`.
- No WebSocket frames were captured, so it is not known whether Slack pushes
  draft changes to other clients.

Not captured, taken from documentation:

- **Sending a draft:** Slack's public `chat.postMessage` docs list
  `draft_id` and `current_draft_last_updated_ts`, and the errors
  `draft_already_sent`, `draft_already_deleted`, `draft_has_conflict`,
  `draft_not_found`.
- **`drafts.delete`:** `draft_id` and `client_last_updated_ts` (the current
  time), from three open-source clients that call it against live
  workspaces. Their notes also give `attached_draft_exists` (create into a
  destination that already has an attached draft), `draft_has_conflict`,
  `draft_update_invalid` / `draft_delete_invalid` (a deleted draft still
  holding its slot) and `both_user_ids_and_channel_id_provided_in_destination`.

Tests that rely on these say in their comment that the shape is uncaptured,
so a later capture can replace the assumption.

## Scope

In scope:

1. Composer sync for attached drafts, channel and thread composers, every
   connected workspace.
2. Sending marks the draft sent.
3. A `:drafts` list: browse, jump, delete.
4. `[compose] sync_drafts` option, on by default.

Out of scope (possible follow-ups):

- A slk "Save as draft" action (its request is already captured).
- Sending from the list, a "Sent" tab, scheduling.
- Reacting to WebSocket draft events (none observed).
- Syncing slk's local pending attachments.

## Behaviour

### Which drafts slk writes

slk writes only attached drafts it can represent: `is_from_composer=false`,
exactly one destination, `date_scheduled == 0`, no `file_ids`. Every other
active draft is **hands-off**: listed, deletable from the list, never restored
into a composer, never updated. The hands-off kinds are:

| Kind | Test | List marker |
|---|---|---|
| Scheduled | `date_scheduled != 0` | `scheduled <date time>` |
| Has files | `len(file_ids) > 0` | `N files` |
| Stashed | `is_from_composer` and one destination | `saved` |
| Unaddressed | no destination | `no recipient` |

### Saving

A composer slot (team, channel, thread) whose text changed is *dirty*. About
2 seconds after the last edit, and immediately when the composer moves to
another conversation, the workspace switches, or slk quits, every dirty slot
is flushed: non-empty text is saved, empty text discards the slot's draft.
slk sends the slot's last-edit time as `client_last_updated_ts`.

Not dirtied:

- text placed in the composer by edit mode (`E`) — that is a sent message;
- text written into a slot by a remote restore;
- pending local attachments (a slot with attachments and no text is empty
  for sync).

### Restoring

At connect and on every reconnect slk lists the workspace's drafts. Each
writable draft is restored into its slot (the thread composer when it has a
`thread_ts`) only when the slot is **not visible and not dirty**. The visible
composer is never written by a remote draft. A slot whose draft disappeared
from Slack (sent or deleted elsewhere) is cleared only if it still holds
exactly the text slk last synced.

### Sending

Sending from a composer whose conversation has a draft posts with `draft_id`
and `current_draft_last_updated_ts`, so Slack marks the draft sent with the
post. Uploads with a caption post without them; the composer's empty text
then discards the draft as usual.

### Conflicts

The text in slk's composer wins, because a save only ever carries text the
user typed in slk:

| Slack answers | slk does |
|---|---|
| `draft_has_conflict` on update | retry once with the current time as `client_last_updated_ts`; toast "Replaced a newer draft from another device" |
| `attached_draft_exists` on create | list, adopt the attached draft's id, update it; same toast |
| the destination's attached draft is hands-off (scheduled or has files), or `draft_update_invalid` / `attached_draft_exists` naming a deleted draft | keep the text in slk only; one toast per conversation per session: "Slack already has a draft here that slk won't overwrite" |

Stashed drafts never block a save: several may share a destination with the
attached draft.

### Drafts list

`:drafts` (keyless, like `:marks`; listed in the help overlay) opens a modal
for the active workspace, newest first. Each row: destination (`#channel`,
`#channel › thread`, DM name), age, a one-line preview, and the hands-off
marker. Opening the list also reloads it in the background, since Slack is
not known to push draft changes.

- `enter` jumps to the destination through `applyLocation` (channel, or
  thread). An unaddressed draft does not jump; a toast says so.
- `d` asks for confirmation (`confirmprompt`), then deletes the draft. The
  deleted draft's slot is cleared, including the visible composer, because the
  user asked.
- `esc` closes.

### Option

```toml
[compose]
sync_drafts = true   # default
```

Off: no draft calls at all, drafts stay in memory as today, `:drafts` refuses
with a toast.

## Architecture

```
internal/slack/drafts.go     wire: list, create, update, delete; postMessage draft fields
internal/core                DraftService port, DraftKey, Draft, messages
cmd/slk/drafts.go            engine: per-workspace index, create/update choice,
                             one call in flight per key, conflicts, list on connect
internal/ui/compose          dirty set + last-edit time, ApplyRemoteDraft
internal/ui/reducer_drafts.go  debounce, flush, apply loaded/saved/deleted drafts
internal/ui/draftlist        the list widget (chrome); glue in internal/ui/drafts_list.go
```

`internal/ui` stays free of I/O: everything Slack-facing goes through the
`DraftService` port, wired in `cmd/slk`.

### Wire layer: `internal/slack/drafts.go`

- `(*Client).ListDrafts(ctx) ([]Draft, error)`: `drafts.list` with
  `is_active=true`, `limit=25`, following `next_ts` while `has_more`, with a
  page cap so a server that never says `has_more=false` cannot loop.
  `_x_reason` as captured (`client-v2-boot-team` at connect).
- `CreateDraft(ctx, DraftWrite) (Draft, error)`,
  `UpdateDraft(ctx, draftID string, DraftWrite) (Draft, error)`,
  `DeleteDraft(ctx, draftID string) error`. `DraftWrite` holds the
  destination (channel, thread), the composer text and the last-edit time.
  Forms match the captures field for field: `blocks` from `mrkdwn.Convert`
  (as `commandForm` builds them), `destinations` with `channel_id` only (plus
  `thread_ts` and `broadcast:false` for a thread), `attachments=[]`,
  `file_ids=[]`, `is_from_composer=false`, `client_msg_id` from a UUID
  generator on create, `_x_reason=MessageInput:updateDraft` via
  `slackhttp.WithReason`. Sent urlencoded through `postForm` like every other
  hand-rolled endpoint.
- Slack's `{"ok":false,"error":code}` is `*DraftRejectedError{Code}`.
- `postMessage` (and so `SendMessage` / `SendReply`) take an optional
  `*DraftRef{ID, LastUpdatedTS}`, added as `draft_id` and
  `current_draft_last_updated_ts`. The chips path (`sendWithChips`) carries
  it too.
- `Draft` decodes `id`, destinations, `blocks`, `file_ids`, `date_scheduled`,
  `is_from_composer`, `last_updated_ts`, `date_created`.

### Port: `internal/core`

```go
type DraftKey struct{ TeamID, ChannelID, ThreadTS string }

type Draft struct {
    ID          string
    Key         DraftKey // ChannelID "" when unaddressed
    Text        string   // composer markdown (see Formatting)
    Updated     time.Time
    Scheduled   time.Time // zero unless scheduled
    FileCount   int
    Stashed     bool
}

func (d Draft) Writable() bool // attached, addressed, unscheduled, no files

type DraftService interface {
    Load(teamID string) Msg                         // DraftsLoadedMsg
    Save(key DraftKey, text string, edited time.Time) Msg // DraftSavedMsg
    Discard(key DraftKey) Msg                       // DraftSavedMsg (gone)
    Delete(teamID, draftID string) Msg              // DraftDeletedMsg
}
```

An adapter in `adapters.go`, nil-safe like the others; `App.SetDraftService`;
a `setDraftServiceForTest` setter in `services_helpers_test.go`.

### Engine: `cmd/slk/drafts.go`

One `draftEngine` per workspace (on `WorkspaceContext`), owning:

- the index: `DraftKey → {id, lastUpdatedTS, writable}`, rebuilt from every
  list;
- per-key serialization: at most one Slack call in flight per key; a newer
  save for a busy key replaces any queued one, so saves never reorder;
- the create/update choice and the conflict table above;
- `DraftFor(key)` for the send adapters (`SendMessage` / `SendReply` in
  `main.go`), which waits for that key's in-flight save, returns the
  `DraftRef`, and forgets the key once the post succeeds.

At connect and on reconnect `connectWorkspace` calls `Load` and the result
reaches the UI as `DraftsLoadedMsg`, the same way other boot data does.

### UI

- `compose.Model` records, per slot, whether it is dirty and its last-edit
  time. `TakeDirty()` returns the dirty slots (key, text, edited) and clears
  the marks. `ApplyRemoteDraft(key, text)` writes an inactive, clean slot and
  reports whether it did. `ClearSlot(key)` empties a slot, visible or not
  (used by list delete). Edit mode and restores do not mark dirty.
- `reducer_drafts.go`: each composer edit bumps a sequence and schedules
  `draftFlushMsg{seq}` after the debounce; only the latest sequence flushes.
  `SetDraftContext` changes, workspace switches and quit flush immediately.
  It reduces `DraftsLoadedMsg` (restore, refresh list rows), `DraftSavedMsg`
  (list row, conflict toast) and `DraftDeletedMsg` (row, slot). It keeps
  `syncedText[DraftKey]`, the text last saved to or restored from Slack per
  slot, which decides whether a draft that vanished from Slack may clear its
  slot.
- The App keeps the active workspace's list rows; `draftlist` draws them with
  `bubbles/chrome` and is composited in `view_overlays.go` like the marks
  overlay. `:drafts` is an entry in `commands` and a keyless help entry in
  `DefaultKeyMap`.

### Formatting

Saving: composer text → `TranslateMentionsForSend` → `mrkdwn.Convert`, the
same path a send takes. Restoring: blocks → `blockkit.RichTextToMarkdown` →
`MentionsForEdit`, the path `E` uses (`messages.EditSource`). Text that cannot
round-trip keeps its wire form, as in edit.

## Testing

stdlib `testing`, white-box, existing helpers.

- **`internal/slack`**: the captures copied to `testdata/`;
  `assertFormMatchesCapture` for create and update (`client_msg_id` and
  `client_last_updated_ts` checked for shape); list paging and the page cap
  against a fake server; decoding every kind in `drafts_list.json` into the
  right hands-off flags; each `DraftRejectedError` code; `postMessage` with a
  `DraftRef` sends both fields (named as uncaptured).
- **`cmd/slk`**: the engine against a fake client: create then update; one
  call in flight per key with the newest text winning; `draft_has_conflict`
  retry; `attached_draft_exists` adoption; hands-off never written;
  `DraftFor` waits for an in-flight save and forgets after the post.
- **`internal/ui/compose`** (`draft_test.go`): dirty and last-edit time;
  `ApplyRemoteDraft` refuses the visible slot and dirty slots; no dirty marks
  from edit mode or restores; `ClearSlot`.
- **`internal/ui`**: debounce via `deliveredMsgs` (real timers); flush on
  conversation switch, workspace switch and quit; `DraftsLoadedMsg` restores
  into inactive slots only; list golden via `dialogGoldens`; jump and
  delete-with-confirm via `pressKey`; `:drafts` refuses when the option is
  off; `TestHelpListsEveryCommand` and `boundary_test.go` pass.
- AGENTS.md tables gain the new helpers in the commits that add them.
