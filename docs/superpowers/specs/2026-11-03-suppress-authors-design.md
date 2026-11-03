# Suppress authors — design

## Problem

Some bots and people post things you never want to read: a deploy bot in
`#general`, a noisy integration everywhere, one person in one channel.
Slack has no "hide this author" for a user. slk should let you hide a
bot's or user's messages in one conversation, or everywhere in a
workspace.

## Behavior

A **rule** names an author (the message's author ID: a user `U…`/`W…`,
or a bot `B…` for a bot message with no user) and, optionally, a
conversation. With no conversation it applies to the whole workspace.
Rules belong to a workspace; IDs are not shared between workspaces.

A message is **hidden** when a rule of its workspace matches its author
ID in its conversation, or workspace-wide. The author ID is the one slk
already attributes messages to on every path (`messageAuthor` live,
the history conversion): the user, else the bot ID. `bot_id` alone is
not matched: only live events and ephemerals carry it, so matching it
would hide a message live and show it again from history. The user's own messages are never hidden.

A hidden message:

- **is not drawn** in the channel pane (any window) or the thread pane:
  history, cached history, older pages, live arrivals. It leaves no
  marker. A thread reply by a suppressed author is hidden too; a thread
  parent by one is hidden in the channel, so its thread is not reachable
  from there. A thread opened another way (Threads view, a permalink)
  still shows its parent: you asked for it;
- **does not notify**, even when it mentions you or matches a keyword;
- **does not mark its channel unread** and does not bump the mention
  badge (live arrivals; `cmd/slk` `OnMessage`);
- **is still cached** and still counts for marking read: opening a
  channel marks it read up to its newest message, hidden or not, so the
  channel does not stay unread on Slack (and come back unread at the
  next `client.counts`). That is why the panes filter, rather than
  `cmd/slk` dropping hidden messages before the UI sees them: the
  mark-read target is taken from the full list.

Not filtered (by choice): search results, the Activity view, the Threads
view list, a parent's reply count and reply avatars (Slack's numbers).

Known limitation: an unread state Slack reports at connect
(`client.counts`) for a channel whose only new messages are hidden still
shows until the channel is opened. Telling them apart would mean
fetching every unread channel's history at boot.

## Managing rules

**`:suppress`** acts on the selected message (either pane). It refuses
your own message and a message with no author. It asks:

> Hide messages from **DeployBot**?
> `[ In #deploys ]  [ Everywhere in Acme ]  [ Cancel ]`

(`App.openConfirmChoice`). The choice adds the rule, saves it, hides the
author's messages in every open pane at once, and toasts
"Hiding DeployBot in #deploys". A DM offers "In this conversation".

**`:suppressed`** opens a list of the workspace's rules (author name, or
ID when unknown; `#channel` or "everywhere"). Backspace/Delete removes
the highlighted rule; the list stays open. Removing a rule reloads the
open panes from the cache, so the author's messages come back without a
fetch. Esc closes. Modelled on the marks list (`internal/ui/marks`),
drawn through `bubbles/chrome`.

Both are in the help overlay (`TestHelpListsEveryCommand`).

## Configuration

Rules live in `config.toml`, in the workspace's block, as an array of
tables, so they can be written by hand and by slk alike:

```toml
[[workspaces.acme.suppress]]
author = "B01DEPLOYBOT"
note = "DeployBot"            # optional; for people reading the file

[[workspaces.acme.suppress]]
author = "U02ALICE"
channel = "C03RANDOM"         # omit for the whole workspace
note = "Alice in #random"
```

`author` is required; a rule without one fails `config.Load` with the
workspace's key in the error, like other workspace validation. A legacy
team-ID-keyed block works the same (`[[workspaces.T123.suppress]]`).
IDs only: names change and collide. `:suppress` is the easy way to get
the ID right; it writes `note` for you.

slk edits the file as text, like the theme and sidebar-width savers
(`configWriteMu`, `writeConfigAtomic`), so comments and layout survive:

- adding appends a `[[workspaces.<key>.suppress]]` block at the end of
  the file (`<key>` is the workspace's existing TOML key, else its team
  ID);
- removing deletes the block whose `author` and `channel` lines match.
  A rule slk cannot find as such a block (written as an inline table,
  say) is not removed; the list says to edit `config.toml`.

## Structure

- `internal/core`: `SuppressRule{AuthorID, ChannelID, Note}` and
  `Suppression`, an immutable rule set (`NewSuppression`, `Hides`,
  `HidesMessage`, `Has`, `Rules`, `With`, `Without`; nil hides nothing). Its
  maps are built once and never written, so one set may be read from any
  goroutine. The port `SuppressionService { Rules(teamID); Add(teamID,
  rule) error; Remove(teamID, rule) error }`.
- `internal/config`: `Workspace.Suppress []SuppressRule`.
- `cmd/slk/suppress.go`: `suppressStore`, the per-workspace sets behind
  a mutex, seeded from the config, replaced whole on every change after
  the file write succeeds. It implements the port, and the event
  handler asks it (`suppressed(channel, author)`) before notifying
  and before marking a channel unread.
- `internal/ui`: both panes take a hide predicate
  (`SetHide(func(MessageItem) bool)`) and apply it in every method that
  adds messages (`SetMessages`, `AppendMessage`, `PrependMessages`;
  `SetThread` replies, `AddReply`). The App installs it per model:
  the channel a model shows comes from `channelOfModel`, the same
  routing `modelsForChannel` uses (which now calls it), and the thread
  pane uses its own channel. `reapplySuppression` refilters every open
  pane after a change (from the cache after a removal).
