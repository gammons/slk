# Find a DM

## Goal

Slack's web client keeps a "Find a DM..." box above Direct Messages. It
searches people, not the sidebar's current 30-day view, and shows presence.
slk has `Ctrl+N` for a new message, but no always-visible entry point and no
presence in its result rows.

## Scope for v1

The box searches the active workspace's people. It is the default scope Slack
uses, and it needs no cross-workspace directory. The other scopes discussed
(People across the grid, DMs, Channel, Unreads) are later additions; the box
and picker are built so a scope chip can be added without changing the result
model.

Searching ignores `[sidebar] hide_inactive_after_days`: the picker's input is
the workspace user map, not the filtered sidebar.

## UI

- A synthetic row is drawn above the Direct Messages section header:
  `🔍 Find a DM...` in muted text. It stays visible when the section is
  collapsed. Slack mode always has a Direct Messages section; in config
  mode the row exists whenever the workspace has a DM.
- Clicking the row opens the picker. It does not select a conversation.
- The picker is the existing new-message modal with a finder mode: title
  "Find a DM", a single result (Enter opens it), and no recipient pills.
- Each result row shows the person's initial as a small coloured avatar,
  a presence glyph when known (`●` online, `○` away, the DND glyph), the
  display name, and `@handle` when it differs.
- `Ctrl+N` keeps its current new-message behaviour: multi-select, title
  "New message", no finder-only row decoration.
- The picker filters on display name and handle as before. In finder mode a
  space is a query character, not a selection toggle.

## Data

`seedNewMessagePicker` already builds the workspace user list from
`a.userNames`, excluding self and deactivated users. The finder adds:

- `Presence` from `(*sidebar.Model).PresenceByUser` when a DM row has it.
- the avatar initial is drawn from the user's display name with
  `styles.UserColor(userID)`, so every row has a stable visual identity even
  before real avatars are wired.

Selecting a person uses the existing `openConversation` path. If a DM already
exists, Slack's `conversations.open` returns it, so no special case is needed.

## Tests

- Sidebar renders the row above the Direct Messages header and hit-tests it.
- Clicking the row opens the finder, not a conversation.
- Finder mode submits the highlighted person on Enter, including when the
  query contains spaces.
- Rows render presence and the coloured initial.
- Ctrl+N still opens the multi-select new-message picker.
