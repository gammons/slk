# Hide deactivated users

Status: implemented. Issue #345.

## Problem

The Slack desktop client hides deactivated ("deleted") users: a DM with a
deactivated person leaves the sidebar, and they are not offered in people
pickers. slk showed them everywhere. Long-gone colleagues sat in the DM
section, in the channel finder, in the new-message picker and in `@`
autocomplete.

The flag never reached the UI:

- The `users` table had no `deleted` column. Every writer receives
  `deleted` from Slack (`slack.User.Deleted`, `edge.User.Deleted`,
  `boot.User.Deleted`) and dropped it.
- `buildChannelItem` (`cmd/slk/channelitem.go`) ignored
  `slack.Channel.IsUserDeleted`, which `users.conversations`,
  `conversations.info` and WS `im_created` / `im_open` already decode.
- The only reader of the flag was the profile dialog's "deactivated" badge,
  from a fresh `users.info`.

Some DMs with deactivated people were not in slk at all: `users.conversations`
leaves out some open DMs that `client.userBoot` has (see "Open DMs
users.conversations leaves out"), so there was nothing to hide or find.

## Behaviour

New option, on by default to match the desktop client:

```toml
[general]
hide_deactivated_users = true   # false shows deactivated people again
```

It lives in `[general]`, not `[sidebar]`, because it covers pickers too.
`Load` decodes over `Default()`, so a plain `bool` defaulting to `true`
reads an explicit `false` correctly.

When on, a deactivated user is hidden from:

| Surface | Where |
|---|---|
| Sidebar DM and app rows | `sidebar.Model.rebuildFilter`, `revealUnreadHidden` |
| Channel finder before you type, and the forward picker | `channelfinder` `filter()` |
| `#` autocomplete in compose (it lists sidebar DM rows) | `App.SetChannels` |
| New-message picker (Ctrl+N) | `App.seedNewMessagePicker` |
| `@` mention autocomplete (both composers) | `App.SetUserNames` roster |

Sidebar rules:

- The active conversation is never hidden (same rule as staleness), so
  opening one by permalink, mark or history still works and the cursor
  does not vanish from under the user.
- Nothing else is exempt. Starred or section-pinned DMs are hidden (unlike
  `IsStale`), and so are DMs with unreads, as in Slack. The reveal of
  hidden rows that turned unread (`revealUnreadHidden`) skips them: it is
  for rows hidden as stale, not on purpose. The Active only filter runs
  after the deactivated check in `rebuildFilter`, so its refilter on every
  read-state change keeps them hidden too.
- The window title's unread count (`UnreadChannelCount`) skips them as
  well, so it matches the dots on screen. A stale row is different: it is
  never hidden while unread, so it always counts. This count,
  `rebuildFilter` and `revealUnreadHidden` share one check, `hidesPeer`.

Channel finder, like the Slack desktop quick switcher: the person stays out
of sight until you look for them on purpose.

- **Empty query (or only spaces): hidden.** The recent list is a browse
  surface like the sidebar.
- **With a query: offered and marked.** A deactivated DM matches by name
  like any row. It is drawn in the dim grey used for not-joined channels,
  with a dim `○`, no presence or status glyph, and a `deactivated` tag
  after the name. The tag is taken out of the name budget, so a long name
  truncates before the tag is lost. It sorts after every active match
  (including not-joined channels), so it never pushes a live conversation
  down. An exact name match can sit below a weak fuzzy match on a channel;
  that is on purpose.
- **Forwarding: not offered.** A message cannot be sent there.
- **Opening it** works as any DM: history loads. The sidebar shows the row
  while it is active and drops it when you leave. `openChannel` makes the
  row active before it selects it, so the sidebar cursor lands on it (this
  also helps a stale row opened from the finder).

The finder has no people search: it only reaches DMs slk already has. A
former colleague you never had a DM with is not offered.

Composer: the desktop client offers no composer for a deactivated account.
slk disables the channel composer of a DM whose peer is deactivated, the
same way it does for read-only conversations: the composer area reads
"You can't send messages to a deactivated account.", `i` shows it as a
toast, and a thread reply there is refused with the same toast on Enter.
Editing an old message is not blocked, as it is not for read-only
conversations. This does not depend on the option: hiding is a display
preference, the account is deactivated either way.

With the option off nothing is hidden or marked, and the finder shows the
DM as a normal row. The composer stays disabled, and forwarding still does
not offer the DM, for the same reason.

Out of scope:

- Group DMs with a deactivated member. Slack keeps them, and no per-member
  flag reaches `FormatMPDMName`.
- Message authors, reactions, threads and activity. Slack still shows
  deactivated authors on old messages.
- Pruning presence subscriptions for hidden DMs (`workspacePresenceIDs`).
- Refreshing profiles on reconnect. A deactivation or reactivation that
  happens while slk is disconnected is seen at the next start, or when the
  person's profile is next fetched.
- Refiltering an `@` / `#` dropdown that is open at the moment the set
  changes. The next keystroke's list is filtered, and a deactivated name
  picked from the old list still becomes a mention.

## Design

One global set of deactivated user IDs, pushed to the App the way
`ExternalUsers` is, and read by every surface. Global, not per workspace:
Slack deactivates an account, not a membership, and the users cache is
keyed by user ID with `workspace_id` meaning only "the workspace that first
cached the row" (`UpsertUser`). A per-workspace query would miss users
another workspace cached first, and one live update reaches every
workspace's view.

### Sources of the flag

1. **Conversation objects.** `IsUserDeleted` on IM objects from
   `users.conversations`, `conversations.info` (starred recovery, message
   discovery) and WS `im_created` / `im_open`. `buildChannelItem` stays
   pure; its callers run `recordDeactivatedPeer(ch, db)`. A conversation
   only ever adds: an absent or false flag does not undo a deletion (only
   a profile can). `MarkUserDeleted(id)` is an UPDATE that never creates a
   row; a peer with no row is kept in the process-wide `rememberedPeers`
   for this run.
2. **Users cache.** Column `users.deleted INTEGER NOT NULL DEFAULT 0`.
   Every full-profile writer sets it and overwrites it on conflict like
   `is_bot`: both `resolveUser` branches, `resolveOneContext`,
   `UpsertUserFromEdge`, `hydrateFirstSight` (insert only) and bootstrap
   revalidation (`UpdateUserFromEdge`). The bot-ID writer (`GetBotInfo`)
   writes `false`, correct for a `B…` ID. Profiles that bypass those
   writers (the `users.info` fallback in `peer_status.go`, the avatar
   backfill, the profile dialog's fetch) go through
   `recordUserDeleted(db, send, userID, deleted)`.
3. **Migration and coverage.** The one-time `UPDATE users SET version = 0`
   earlier columns used. Conditional revalidation then refetches the users
   bootstrap asks about: the open DMs' peers and the authors of the
   channel restored at startup. That is every sidebar DM. It is not every cached user: the `@`
   roster, seeded from the whole cache, learns that someone is deactivated
   only when they are next resolved. A whole-cache sweep would cost about
   `ceil(N/80)` calls per workspace for tens of thousands of rows, so it
   is not done.
4. `user_change` carries only the self user in practice and is not used.
   A userBoot IM has no `is_user_deleted`; the cache covers that path.

### Open DMs users.conversations leaves out

Checked against a real workspace (read-only calls, flags only): for a DM
with a deactivated person, `users.conversations` did not return it, while
`client.userBoot` had it open and not archived, in `ims` and in the
top-level `is_open` list. `conversations.info` on it said
`is_user_deleted=false`, so the deactivation is known from the person's
profile, not the conversation. Of 20 open DMs userBoot had, 2 were missing
from `users.conversations`. slk used userBoot's list only when
`users.conversations` failed, and recovered only starred DMs from it.

So `connectWorkspace` now adds every open, non-archived userBoot DM that
`users.conversations` left out (`missingOpenIMs`, with the same open rule
as `bootConversations`, `bootIMOpen`). It runs before the starred
recovery, which then sees those IDs as known. The DMs go through the usual
pipeline (`buildChannelItem`, cache, name seeding). No new requests: the
userBoot result is already there.

Their deactivated flag comes from the person's profile: a cached
`users.deleted`, or name resolution after connect (`resolveDMNames`, which
sends `UserDeactivatedMsg`). With a cold cache the row can show for a
moment before that answer.

Side effect: an open DM with an active person that `users.conversations`
leaves out is now a row too, as in the desktop client. In the sidebar it is
still subject to the staleness filter.

### Engine to UI

- `(*cache.DB).DeactivatedUsers() map[string]bool`, caller-owned.
  `deactivatedUsers(db)` is that plus the remembered peers the cache has
  no row for, and the `deactivatedSeq` value read before both.
- `WorkspaceReadyMsg` and `WorkspaceSwitchedMsg` carry `DeactivatedUsers`
  and that `DeactivatedSeq`. `App.SetDeactivatedUsers(map, seq)` only adds
  to the App's set.
- Revalidation writes `users.deleted` with no sender of its own, and a
  non-initial `WorkspaceReadyMsg` does not apply its set. So
  `connectWorkspace` reads `DeactivatedUsers()` before and after
  `bootstrap.Run` and announces the difference (`announceRevalidated`),
  both ways. Without it, a user reactivated while slk was off stays hidden
  when another workspace's message hid them first.
- Live change: `UserDeactivatedMsg{UserID, Deactivated, Seq}`, sent for
  every deactivation a profile shows, and for a reactivation only of a
  user known as deleted (`knownDeleted` before the write,
  `announceUserDeleted` after). Once a cache row exists the cache decides,
  so a remembered flag does not undo a reactivation on the next switch.
- Ordering. Workspaces connect in parallel, so a snapshot read before
  another workspace's reactivation can reach the App after it, and adding
  the user back would last until restart (the cache says active, so no
  later profile reports a change). `deactivatedSeq` is one process-wide
  counter: each `UserDeactivatedMsg` takes the next value after its cache
  write, and a snapshot reads it before reading the cache. A snapshot can
  have an old number with fresh data, never a fresh number with old data.
  The App keeps the `Seq` of the last live update per user and skips a
  user in a snapshot with a lower number; the rest of the snapshot still
  applies. A live update older than the last one applied for the user is
  dropped too.
- The App replaces its set whole on every change and never writes a
  published map, because the sidebar and finder keep the map they are
  given. An unchanged set is not republished. `App.hiddenUsers()` is nil
  when the option is off, so the surfaces know nothing about the option.
  The finder also gets the full set (`SetUnsendableUsers`), only for the
  forwarding check.
- The option reaches the App through `uiSettings` and
  `App.SetHideDeactivatedUsers(bool)`. `NewApp` starts with it on, like
  `config.Default()`.

### Filtering

- One rule for the sidebar, the finder and the `#` autocomplete:
  `core.IsHiddenPeerConversation(type, dmUserID, hidden)`, a dm or app row
  with a hidden peer. `core.ChannelFinderItem` gains `DMUserID`, set in
  `buildChannelItem` and `refreshDMPeerFromCache`, and kept by
  `upsertFinderItem`.
- Sidebar setters keep the cursor on its row (`refilter`).
- Finder `filter()`: the check runs after the query check. With an empty
  query the row is skipped, and in forwarding mode a DM of anyone
  deactivated is skipped whatever the option says; with a query it goes
  into matching, and the sort puts deactivated last, ahead of `Joined`.
  `channels/search` returns channels only, so the server search cannot
  bring back an unmarked DM.
- `#` autocomplete: `App.channelPickItems` keeps the last `SetChannels`
  list so it can be rebuilt when the set changes.
- New-message picker: seeded on open, so it needs no live refilter.
- `@` roster: users are marked (`mentionpicker.User.Deactivated`), not
  dropped. `compose.rebuildMentionUsers` leaves them out of suggestions,
  while `SetUsers` keeps them for the send-time `@Name` to `<@ID>`
  translation, so a name typed by hand still becomes a mention.
- `/open` matches only channels, so it is unaffected.

### Composer

`(*App).sendBlockedReason(channelID)` returns why a conversation takes no
messages, or "": read-only mark, then deactivated DM peer, then the
app-home probe. The peer (`(*App).peerDeactivated`) comes from the
finder's rows, else from the sidebar's items (a DM opened with Ctrl+N
reaches the sidebar first), and is looked up in the deactivated set
whatever the option. It replaces the read-only checks in
`retargetActiveChannel`, `ChannelMessagingCapabilityMsg` and the thread
reply check. A change of the set recomputes `composeDisabled` for the
active channel (a reactivation keeps a read-only mark), and the
`UserDeactivatedMsg` reducer leaves insert mode when the composer is now
disabled; the draft is kept. `ConversationOpenedMsg` recomputes it too:
`addConversation` reports a deactivated peer before the row that names it.

## Tests

- cache: migration adds the column and resets versions once; every writer
  persists and overwrites `deleted`; `MarkUserDeleted` never creates a
  row; `DeactivatedUsers` returns exactly the deleted users.
- config: default `true`, explicit `false` and `true` read back, also with
  a partial `[general]` block.
- cmd/slk: `buildChannelItem` carries `DMUserID`; an IM with
  `IsUserDeleted` marks the user; profiles override remembered peers in
  both directions; `missingOpenIMs` adds only open, non-archived DMs the
  list left out.
- sidebar: a deactivated DM is hidden, shown again with a nil set; the
  active one stays; starred and unread ones are hidden, also after the
  unread reveal and with Slack style and Active only on; the cursor stays
  on the same item; Slack sections leave it out.
- finder: absent on an empty or blank query, present, marked and last on a
  matching one, absent in forwarding mode, a normal row with a nil set; a
  long name keeps the tag.
- new-message picker, mention roster: deactivated users are not offered
  when on, offered when off; a typed `@Name` still becomes a mention.
- App: `UserDeactivatedMsg` hides the row live and a reactivation shows
  it, in whichever workspace is on screen; opening a deactivated DM
  disables the composer, also with the option off; `i`, Enter and a
  thread reply are refused; a reactivation keeps a read-only mark.
