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

Every 2 seconds slk compares each composer slot (team, channel, thread) with
the text Slack last had from it. The composer's text changes through many
paths (keys, paste, the external editor, emoji insertion, `/msg` pre-fill),
so comparing catches them all where hooking each would miss one.

- The visible slot is saved once its text has stayed the same for a whole
  interval, 2 to 4 seconds after the last keystroke.
- A slot the composer has moved away from (another conversation, another
  workspace) is saved at the next check; it stays in memory until then.
- On quit, every slot that differs from what Slack last had is saved with a
  2-second deadline -- and so is a slot whose current text equals that
  (nothing to re-save, on the face of it) but has a save in flight for
  *different* text: save A, start saving B, change back to A, quit would
  otherwise see text == synced ("A") and flush nothing, leaving Slack's
  draft at whatever the in-flight save for B lands as. The flush visits
  such a key with its current text (here, A).

Non-empty text is saved; empty or whitespace-only text discards the slot's
draft. slk sends the time it first saw the slot's current text as
`client_last_updated_ts`. A slot is not compared before its workspace's
drafts have been listed once, so slk never creates a draft Slack already has.

Not drafts:

- the text edit mode (`E`) puts in the composer — that is a sent message; the
  draft held aside during the edit is what is compared;
- text a remote restore writes (it is recorded as Slack's text);
- the empty composer a send leaves behind (the send marks the draft sent);
- pending local attachments (a slot with attachments and no text is empty
  for sync).

A save that fails for any reason other than a refusal is retried with
backoff (4 s doubling to at most a minute).

Mentions are translated with the slot's OWN workspace's names, never the
active workspace's: a slot left in workspace A and saved after switching to
B must still read `<@U…>` back from A's mention data, not B's (two workspaces
can give the same user ID different handles). For the active workspace, the
translation runs now and is remembered; for an inactive one, the save (or
quit flush) reuses that remembered translation when the text still matches,
and is skipped otherwise — it is saved once that workspace is active again.
A workspace switch records every outgoing slot's translation before the
composers' mention data changes underneath it.

### Restoring

At connect and on every reconnect slk lists the workspace's drafts. Each
writable draft is restored into its slot (the thread composer when it has a
`thread_ts`) only when the slot still holds the text Slack last had from slk,
i.e. the user has typed nothing there since. The visible composer is
additionally written only when it is **empty**: text on screen is never
replaced. (The composer is usually bound to its first conversation before the
list arrives, so without this its draft would never appear.) A slot whose
draft disappeared from Slack (sent or deleted elsewhere) is cleared only if it
is not visible and still holds exactly the text slk last synced.

Restoring and vanish-clearing act only on the active workspace's keys: the
composers hold the active workspace's mention data, so applying another
workspace's list there would restore or clear text translated with the wrong
names. A list for a workspace that is not active only updates that
workspace's `loaded`/rows bookkeeping; the restore happens once that
workspace becomes active, which re-issues the list load.

A list older than a local change made since it was issued does not override
that change: restoring a sent draft's text back, clearing a slot saved
during the request, or bringing a sent/deleted draft's row back. Each local
change (a save answer, a send, a list delete, a vanish-clear) is stamped
with a sequence number; a list is stamped with the sequence in effect when
it was issued, and skips restore/vanish/clear for any key touched since.

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
| `draft_not_found`, `draft_already_deleted` or `draft_already_sent` on update | forget the id and create a new draft |
| the destination's attached draft is hands-off (scheduled or has files), or `draft_update_invalid` / `attached_draft_exists` naming a deleted draft | keep the text in slk only and stop syncing that conversation until the next list shows a writable draft there; one toast: "Slack already has a draft here that slk won't overwrite" |

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
internal/ui/compose          slot access: Slots, SlotText, ApplyRemoteDraft, ClearSlot
internal/ui/drafts.go        draftSync: the 2 s comparison, restore, results
internal/ui/reducer_drafts.go  routes the draft messages
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
- `SendDraft(ctx, channelID, threadTS, text, broadcast, DraftRef)` posts as
  `SendMessage` / `SendReply` do, adding `draft_id` and
  `current_draft_last_updated_ts` (`DraftRef{ID, LastUpdatedTS}`). Both the
  chips path and a plain hand-rolled `chat.postMessage` form carry them;
  `SendMessage` and `SendReply` are unchanged.
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
    Saved       bool      // is_from_composer: "Save as draft" or "New message"
}

func (d Draft) Writable() bool // attached, addressed, unscheduled, no files

type DraftSaveResult struct{ Draft Draft; Replaced bool }
type DraftChange struct{ Key DraftKey; Text string; Edited time.Time } // Text "" discards
var ErrDraftHandsOff error // the destination's draft is one slk won't overwrite
var ErrDraftStale error    // current reported the save no longer wanted; no API call made

// Blocking, like CommandService.Run: the UI runs them from tea.Cmds and
// wraps the answers in its own messages (core cannot import the UI).
type DraftService interface {
    Load(teamID string) ([]Draft, error)
    // current, checked under the engine's per-conversation lock before
    // any API call, answers whether this save is still wanted; nil
    // means always current (the quit flush passes nil). A false answer
    // makes no call and returns ErrDraftStale -- the escape hatch for a
    // save built before a later send or delete for the same
    // conversation, which would otherwise take the lock only after
    // that send/delete forgot the draft and recreate it.
    Save(key DraftKey, text string, edited time.Time, current func() bool) (DraftSaveResult, error)
    Discard(key DraftKey) error
    Delete(teamID, draftID string) error
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
  `Save`'s `current` is checked right after the key's lock is taken, before
  any call, so a save that only reaches the lock after a send or delete for
  the same key forgot the draft is refused (`ErrDraftStale`) instead of
  recreating it;
- the create/update choice and the conflict table above;
- `beginSend(dest)` for the send adapters (`Send` / `SendReply` in
  `main.go`, through `sendWithDraft`), which waits for that key's in-flight
  save, returns the `DraftRef`, and forgets the key once the post succeeds.

On every connect (first and reconnect) the workspace's `rtmEventHandler`
sends `ui.DraftsStaleMsg{TeamID}`; the App answers by calling `Load`.
`shutdown` saves the App's `UnsyncedDrafts()` through the engines with a
2-second deadline.

### UI

- `compose.Model` exposes its slots: `Slots()` (the visible one, even
  empty, and every stored one), `SlotText(key)`, `ApplyRemoteDraft(key,
  text)` (inactive slots, or the visible one when it is empty) and
  `ClearSlot(key)` (any slot; list delete). It knows nothing of sync.
- `draftSync` (`internal/ui/drafts.go`) holds, per slot, `synced` (the
  composer-form text Slack last had from slk), `observed` (the text seen at
  the last check and when), `inflight`, a generation (an `*atomic.Uint64`,
  via `genPtr`) bumped by a send or a delete, retry backoff and the
  hands-off set; per workspace, whether drafts were listed, and the list
  rows. A `draftTickMsg` every 2 s runs the comparison; it starts with the
  first list. A send calls `noteDraftSent(key)` at the moment the composer
  is reset, before the save command for that key exists, so any save that
  takes the engine's lock afterward sees the bump. `saveDraftCmd` reads the
  pointer once to build a `current` closure it passes to `Save`; a stale
  answer (gen mismatch, or `ErrDraftStale` from the service itself) is
  ignored by `reduceDraftSaved`. A separate `seq`/`touched` pair tracks
  freshness against a list in flight: `touch(key)` bumps `seq` and records
  it against `key` on every save answer (success or hands-off), a send, a
  list delete and a vanish-clear. `loadDraftsCmd` captures `issued :=
  d.seq`; `reduceDraftsLoaded` skips restore, vanish-clear and the
  hands-off clear for any key touched since (`touched[key] > issued`), and
  `mergeDraftRows` keeps the current rows for such a key (present or
  absent) instead of the list's.
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
  `beginSend` waits for an in-flight save and forgets after the post; a
  draft refusal on send falls back to a plain post and deletes the draft.
- **`internal/ui/compose`**: `Slots`, `SlotText`; `ApplyRemoteDraft` writes
  inactive slots and only an empty visible one; `ClearSlot`.
- **`internal/ui`**: the comparison driven tick by tick on a pinned clock
  (debounce of the visible slot, immediate save of a left slot, discard,
  nothing before the first list, edit mode, send, backoff, hands-off);
  `UnsyncedDrafts` for quit; restore into inactive and empty visible slots
  only; list golden via `dialogGoldens`; jump and
  delete-with-confirm via `pressKey`; `:drafts` refuses when the option is
  off; `TestHelpListsEveryCommand` and `boundary_test.go` pass.
- AGENTS.md tables gain the new helpers in the commits that add them.
