# Channel marks: mark a whole DM or channel

Issue: https://github.com/gammons/slk/issues/473
Status: approved design (small change)

## Why

Marks (`m{letter}` / `'{letter}`) always name a message. Using them as quick
shortcuts to frequent conversations (`'a` → teammate DM, `'b` → #team) is
awkward: the jump lands on the old marked message instead of the latest one,
a busy channel loads history just to reach it, and setting the mark needs a
message selected first. Channel marks turn marks into a vim-like way to hop
between a small set of conversations without `Ctrl+t` or scrolling the sidebar.

## Goals

- Pressing `m{letter}` while a conversation (channel, private channel, DM,
  group DM, app) is highlighted in the **sidebar** records a channel mark for
  that conversation. The conversation does not need to be open.
- `'{letter}` on a channel mark opens the conversation at its latest message,
  exactly like opening it from the sidebar. No history window is loaded.
- `:marks` and the jump overlay list channel marks next to message marks,
  labelled `#name` for channels and `@name` for DMs, with no preview.
- Persistence is unchanged: lowercase letters are session-only (or persisted
  under `persist_all`), uppercase letters survive a restart.

## Non-goals

- Marking the current conversation from inside it (no `M` key, no
  `:markchan` command). Reach the conversation in the sidebar instead.
- Cross-workspace channel marks (still refused, as for message marks).

## Anti-goals

- A channel mark must never trigger a `FetchAround` / history load.
- Message marks set from the messages pane, thread panel, Threads view or
  Drafts view must behave exactly as today.

## Approaches considered

1. **Sidebar `m` (chosen).** Reuses the existing `m` chord and both letter
   cases; lets the user mark without opening the conversation. Cost: today
   `m` with the sidebar focused marks the messages pane's selection — that
   behaviour is dropped (focus the messages pane to mark a message).
2. **`M{letter}` in normal mode.** Additive, marks the open conversation from
   any pane. Rejected: adds a key and a second chord for the same concept.
3. **`:markchan {letter}`.** Cheap but slow and hard to discover. Rejected as
   the primary way.

## Design

### Setting (`internal/ui/set_mark.go`)

`m` is handled in the shared normal-mode switch (`mode_normal.go`), so it
already reaches `setMark` with the sidebar focused; no routing change.

In `setMark`, before `currentLocation()`: when `a.focusedPanel ==
PanelSidebar`, read `a.sidebar.SelectedItem()`.
- `ok` → record `Mark{Location{TeamID: activeTeamID, ChannelID: item.ID},
  Letter, ChannelName: item.Name, ChannelType: item.Type}`. No author, no
  excerpt, empty `MessageTS`/`ThreadTS`.
- `!ok` (section header, Threads/Drafts rows) → toast
  "No conversation selected to mark".

All other panes keep the current message-mark path.

### Storage

- `ui.Mark` and `cache.Mark` gain `ChannelType string`.
- `marks` table gains `channel_type TEXT NOT NULL DEFAULT ''`, added for
  existing databases via `addColumnIfMissing` in `internal/cache/db.go`
  (and in the `CREATE TABLE`). `UpsertMark` / `ListMarks` read and write it;
  the adapter in `cmd/slk` maps it.
- A mark is a channel mark when `MessageTS == "" && ThreadTS == ""`. No
  separate kind flag.
- Message marks also record `ChannelType` (from `a.channels.Lookup`), so a
  message mark in a DM renders `@name` too. Old rows with an empty type
  render as `#`, as today.

### Jumping (`jump_mark.go` / `reducer_links.go`)

`jumpToMark` is unchanged: it hands the channel-only `Location` to
`applyLocation`. Opening a different conversation already works —
`completePendingLinkNav` drops a channel-only pending nav without
`FetchAround`, and the normal channel open lands on the latest message.

Fix: when the target is the **already active** conversation, the in-place path
currently leaves the selection where it was. For a channel-only location,
select the newest message (the same effect as `G`, `handleGoToBottom`), so a
channel mark always lands at the latest message. The back-jump (`''`) still
records the departure as today.

### Overlay (`internal/ui/marks/marks.go`, `marks_overlay.go`)

`Row` gains `ChannelType`. The prefix is `@` for `dm`, `group_dm` and
`app`, `#` otherwise. With no author and no excerpt, the row is just the
letter and the label.

### Help / docs

Update the `m` help text in `keys.go` ("set mark; on a sidebar item, marks
the conversation") and wherever marks are documented (README / wiki).

## Tests

- Sidebar-focused `m{a}` on a channel records a channel-only mark with name
  and type; on a section header it toasts and records nothing.
- Uppercase sidebar mark persists through `UpsertMark` / `ListMarks`
  including `channel_type`; the migration adds the column to an old table.
- `'a` on a channel mark for another conversation opens it with no
  `FetchAround`; for the active conversation, selects the newest message.
- Overlay renders `@name` for a DM mark, `#name` for a channel mark, no
  preview text.
- Existing message-mark tests stay green.

## Left to the builder

- How group DM names display (use whatever `item.Name` the sidebar shows).
- Exact toast and help wording.
