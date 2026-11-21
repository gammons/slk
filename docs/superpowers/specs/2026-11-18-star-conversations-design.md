# Star conversations - design

Date: 2026-11-18
Status: implemented - /star, /unstar, the star echo, and the header
click target.

## Problem

Slack stars existed in slk as a read-only sync: the Starred sidebar
section rendered what `stars.list` and the section endpoints reported,
but the only way to set a star was the web client. A user who lives in
slk has to leave it to pin a conversation.

## What exists (implemented)

- `/star` and `/unstar` slash built-ins call `stars.add` /
  `stars.remove` on the conversation the composer belongs to (a thread
  composer stars its parent channel; Slack stars are
  conversation-level).
- The sidebar row moves into the Starred section optimistically; a
  refusal (`ChannelStarSetMsg` with `Err`) reverts it with a toast.
- Slack echoes every star change to every client of the user as a
  `star_added` / `star_removed` WebSocket frame. The dispatcher
  forwards every item kind (`StarChange{ItemType, Channel}`); the
  handler acts on conversation stars (im/channel/group/mpim) and
  ignores message and file stars until slk grows a saved-items view.
- Each session applies the echo to its own workspace: the section
  store's stars membership, the channel row, the cache flag, and the
  active workspace's sidebar refresh. `SectionStore.SetChannelStarred`
  synthesizes the stars section on a workspace's first star (Slack
  serves the section only once it is non-empty), so the first star
  shows without waiting for a sections bootstrap.

## Mouse: a star in the channel header

### Decision

A star glyph at the right end of the channel header - the `# virt`
line at the top of the messages pane. Click toggles the star for the
channel on screen.

Rejected alternative: a per-row glyph in the sidebar. It would need
the row renderer's trailer-cell budget (unread dot vs mention badge)
reworked, widen `ClickAt` to take an x coordinate, and draw a glyph on
every row for a rarely-used action. The header is one click target,
always visible, and doubles as state display for the current channel:
filled when starred, hollow when not.

Limitation accepted: only the channel on screen can be starred by
click. Starring several channels means visiting them; the sidebar rows
show membership through the Starred section anyway.

### Rendering

- The header line is composed with the star at its right end, two
  cells of padding from the border, so the pane width fixes its
  screen column. Filled for starred, hollow for not.
- The glyph is styled like the other header affordances (muted when
  the pane is unfocused) and must survive the header's render cache:
  the cache key already covers the active channel, and the star state
  rides the same invalidation the sidebar's move uses, so no new key
  component is expected. If measurement shows otherwise, the star
  state joins the key rather than the header re-rendering per frame.
- The thread pane's breadcrumb header is out of scope: starring from a
  thread uses `/star`, which stars the parent channel.

### Hit-testing

- The messages-pane region router already resolves clicks in the
  content area; the header is one line above it. A hit test resolves
  (x, y) against the glyph's column range on the header line, the way
  `HitTestLink` and `HitTestReaction` resolve against rows the pane
  published for the frame.
- A hit dispatches the same path `/star` uses: optimistic
  `sidebar.SetItemStarred`, then `ChannelService.SetStarred`; the
  `ChannelStarSetMsg` arm reverts on refusal. No new message types.
- The click must not also select/scroll: the header consumes the event
  before the content pane sees it.

### Tests

- Unit: the composed header contains the glyph, filled or hollow per
  the active channel's starred state, at the expected column.
- Unit: a click on the glyph column toggles the star through the same
  service call `/star` uses; a click one column outside does nothing.
- Golden: a dialog-golden scenario for the header with each glyph
  state, so the two-cell budget from the border is pinned.
