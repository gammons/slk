# Grid Parity — Manual QA Checklist

Everything in Phase 2b was verified by unit tests and by counting API requests.
Neither of those can tell you whether slk still *works*. This checklist covers
what a human at a terminal has to confirm, and it is the gate on any Enterprise
Grid test.

**The order matters.** Section 1 sets up measurement, Section 2 is the cold-cache
run (do it early — it is the case four tasks' worth of measurement was blind
to), and the later sections assume a warm cache.

For each check: what to do, what should happen, and **what to suspect if it
does not**. That last column is the point. A failure here is almost always one
specific deletion, and knowing which one saves an afternoon.

---

## 1. Setup

### 1.0 Build to a distinct name, and run it by absolute path

**There is a stale `slk` binary in the main checkout** (`~/local_code/slk/slk`),
predating all of this work. A `./slk` typed from the wrong directory silently
runs it, and the first QA attempt of this checklist did exactly that — the whole
session measured code from July, and the "bug" it found was the original one.

So: build under a name that cannot collide, and always invoke it by full path.

```bash
cd ~/local_code/slk/.worktrees/grid-parity-phase1
git log --oneline -1                    # expect a05f9d4 or later on feat/grid-parity
go build -o slk-qa ./cmd/slk
export SLKQA=$PWD/slk-qa
```

### 1.1 Prove you are running the right binary — do this EVERY session

Run `$SLKQA` for a few seconds, quit normally, then:

```bash
grep -c 'shutdown API request tally' slk-debug.log   # must be 1
grep -c 'channel-phase\|trigger=reconnect'  slk-debug.log   # must be 0
```

The tally exists only in the new code; `channel-phase` and `trigger=reconnect`
exist only in the deleted backfiller. **If those numbers come out the other way
around, you are running the old binary and nothing you observe is about this
work.** No other check in this document means anything until this one passes.

Run everything with the counter on:

```bash
SLK_DEBUG=1 $SLKQA
```

`slk-debug.log` is written to the **current directory** and **truncated on every
start** — copy it between runs if you want to compare:

```bash
grep -A30 'shutdown API request tally' slk-debug.log
```

The tally is written on a clean shutdown, so quit slk normally (not `kill -9`).

Reference numbers from this session, two workspaces (105 and 39 channels),
~25 second warm session:

```
API requests: 37 total across 18 endpoints
      4  users.channelSections.list      2  conversations.view
      3  edge:channels/info              2  dnd.info
      3  users.conversations             2  edge:users/info
      2  auth.test                       2  stars.list
      2  client.counts                   2  usergroups.list
      2  client.shouldReload             2  users.getPresence
      2  client.userBoot                 2  users.info
      2  conversations.members           1  conversations.history
                                         1  emoji.list
```

Zero of: `users.list`, `conversations.list`, `subscriptions.thread.getView`.

---

## 2. Cold cache — the fresh-install path

This is the scenario that hid a 40,000-request fan-out for four tasks. It is
also exactly what a new Grid tester's first run looks like. Do it before
anything else.

```bash
rm -rf /tmp/slkcold                      # always start from nothing
mkdir -p /tmp/slkcold/data/slk /tmp/slkcold/cache
cp -r ~/.local/share/slk/tokens /tmp/slkcold/data/slk/tokens
SLK_DEBUG=1 XDG_DATA_HOME=/tmp/slkcold/data XDG_CACHE_HOME=/tmp/slkcold/cache $SLKQA
# ... use it for a minute, then quit normally
grep -A30 'shutdown API request tally' slk-debug.log
rm -rf /tmp/slkcold                      # DELETE THE TOKEN COPY WHEN DONE
```

**If the machine bogs down — load climbing past ~10, UI unresponsive — that is
the symptom of the pre-fix fan-out.** Before debugging anything, re-run the 1.1
provenance check on that session's log. The first QA attempt hit exactly this
and it was a stale binary.

For reference, what the OLD binary does on a cold cache with this workspace, and
what the fix is measured against:

| | old (July binary) | fixed |
|---|---|---|
| `users.info` requests | 37,573 (one per distinct channel member) | ~200 |
| rate-limit errors | 30,713, ignored, no backoff | — |
| load average | ~30, UI unresponsive | normal |

- [ ] **2.1 It boots at all.** Sidebar populates, a channel opens.
- [ ] **2.2 `users.info` is in the low hundreds, not thousands.** Reference after
      the fix: 200. Anything above ~1,000 means the fan-out is back.
      → Suspect: `membership.Manager.backgroundFetch` (should not call the
      resolver at all) or `userResolver.sem` (8-slot semaphore).
- [ ] **2.3 Names render.** Messages show display names, not `U01ABCDEF`. Some
      may briefly show as IDs and then resolve — that is expected now; they
      should not *stay* as IDs.
      → Suspect: Task 8 (`users.list` deletion) left a reader that assumed the
      sweep had finished.
- [ ] **2.4 Second boot is cheaper than the first.** Run it again against the
      same temp dirs. Convergence takes two boots by design (the partial cache
      writers are UPDATE-only, so first-sight rows land at version 0).
- [ ] **2.5 Avatars appear** for people whose messages are on screen.

---

## 3. Unread state — highest risk of silent damage

`connectWorkspace` no longer makes its own `client.counts` call; it uses the one
`bootstrap.Run` already made. Unread state is applied as a **full snapshot**
(reset everything to read, then mark what came back), so a mistake here does not
show up as an error — it shows up as dots quietly disappearing.

- [ ] **3.1 Unread dots at boot match reality.** Compare the sidebar against the
      official Slack client or the mobile app, side by side. Channels with
      genuinely unread messages have dots; channels you have read do not.
      → Suspect: `bootstrap.Result.CountsOK` handling in `connectWorkspace`.
- [ ] **3.2 Mention badges are right** (the numbered ones), not just the dots.
- [ ] **3.3 Read one channel, quit, reboot.** It stays read.
- [ ] **3.4 Mark a channel unread in the official client, then boot slk.** slk
      shows it unread.

## 4. Muted, starred, membership — the cache-mapping risk

The Phase 2a partial writers (`internal/cache/edge_sync.go`) update rows from
edge responses that cannot populate every column. If they ever write a full
upsert by mistake, these three go blank — silently, and days later.

- [ ] **4.1 Muted channels are still muted.** There is no visual treatment for
      mute in the sidebar, so check the log instead — slk records what it
      loaded at boot:

      ```bash
      grep 'mute store bootstrap\|marked IsMuted after build' slk-debug.log
      ```

      Compare the count against the number of muted channels the official
      client shows you. A count of 0 when you have muted channels means the
      mute state was lost; a plausible non-zero count means it survived.
      → Suspect: `bootMutedChannels` (mute now comes from `userBoot`'s prefs
      rather than a `users.prefs.get` round trip).
- [ ] **4.2 Starred channels still appear in the Starred section.**
- [ ] **4.3 Your channels are still yours** — nothing you are a member of has
      dropped out of the sidebar.
      → Suspect any of these three: `UpdateChannelFromEdge`, `ApplyMembership`,
      `UpdateUserFromEdge`.
- [ ] **4.4 Boot twice more and re-check 4.1-4.3.** This damage accumulates
      across revalidation passes rather than appearing on the first one.

## 5. Names, DMs and the mention picker

Task 8 deleted the `users.list` sweep; the blocker fix deleted the per-member
resolution. The mention picker's candidate list is now "users slk has seen"
rather than "everyone in the workspace" — a known, deliberate reduction that
Task 11b is meant to restore via server-side search.

- [ ] **5.1 DM list shows names**, not user IDs.
- [ ] **5.2 App/bot DMs are in the Apps section**, not mixed into DMs.
      → Suspect: `BotUserIDs` population (cache seed + `applyBootUsers`).
- [ ] **5.3 Open a busy channel with people you have never DMed.** Names
      resolve within a second or two.
- [ ] **5.4 Type `@` in a channel.** The picker opens and lists people.
- [ ] **5.5 Type `@` plus a few letters of someone in that channel.** They
      appear. **Record what happens if they do not** — that is the expected
      gap Task 11b closes, and knowing how bad it is decides that task's
      priority.
- [ ] **5.6 Sending a mention still works** — pick someone, send, and confirm
      it renders as a real mention (highlighted) rather than literal text.

## 6. Reconnect — the one that needs a real outage

`triggerBackfill` is gone. Reconnect is now `client.counts` + the active channel
+ marking everything else stale. The first connect after launch deliberately
skips this, because `bootstrap.Run` just did it.

- [ ] **6.1 Note the message counts in two busy channels.** Leave slk running.
- [ ] **6.2 Drop the network for ~90 seconds** (`nmcli networking off`, or turn
      wifi off).
- [ ] **6.3 Have someone post in both channels** — one you have open, one you
      do not. Also mark a third channel unread from another client.
- [ ] **6.4 Restore the network.** Wait for the status bar to show connected.
- [ ] **6.5 The open channel shows the new message** without you doing anything.
- [ ] **6.6 The channel you did *not* have open shows an unread dot.**
      → This is the bug Task 9 was supposed to fix (`client.counts` never ran on
      reconnect before). If the dot is missing, `reconnectSync.refreshUnreadState`
      is not working.
- [ ] **6.7 Switch to that channel — the missed message is there.** It may take
      a moment: it is fetched on open now, not pre-fetched.
      → Suspect: `MarkChannelsStale` (the channel should have `synced_at = 0`,
      which forces a refetch on open).
- [ ] **6.8 Check the tally after reconnect.** `conversations.history` should
      have gone up by roughly 1-2, **not by the number of channels you have
      ever visited**. This is success criterion 2.
- [ ] **6.9 Sleep the laptop for a few minutes and wake it.** Same expectations
      — the wake detector uses the same bounded path.
- [ ] **6.10 Flap the network on and off quickly three times.** The 30-second
      dedupe gate should mean roughly one catch-up, not three.

## 7. Threads

`subscriptions.thread.getView` no longer runs at boot. It runs once per
workspace, on the first open of the Threads view.

- [ ] **7.1 Boot and check the tally *before* opening Threads.**
      `subscriptions.thread.getView` should be **0**.
- [ ] **7.2 Open the Threads view.** It populates. There may be a brief moment
      where it shows cached threads before the fetch lands.
      → Suspect: `ensureThreadSubscriptions` wiring in the `EnsureSubscriptions`
      service closure.
- [ ] **7.3 Check the tally again.** `getView` is now non-zero (expect a lot —
      it paginates to a 1000-item cap, ~60 requests per workspace; that is
      pre-existing behaviour, just deferred).
- [ ] **7.4 Leave Threads and reopen it several times.** `getView` does **not**
      climb further — it is once per workspace per session.
- [ ] **7.5 Thread unread badges are correct**, and opening a thread clears its
      badge.
- [ ] **7.6 No "Threads list unavailable" banner** when things worked.
- [ ] **7.7 Switch to your other workspace and open Threads there.** It fetches
      for that workspace (the `sync.Once` is per workspace, not global).

## 8. Channel finder

The `conversations.list` walk is gone. Unjoined channels now come from a
debounced `edge:channels/search`.

- [ ] **8.1 Ctrl+T.** Your joined channels appear immediately.
- [ ] **8.2 Type a few letters.** Local matches filter instantly — there should
      be **no lag on the keystroke**. Server results merge in shortly after.
      → If typing feels laggy, the debounce is blocking the filter rather than
      just the request.
- [ ] **8.3 Search for a public channel you have NOT joined.** It appears.
      → Suspect: `searchChannelsRemote` / `wctx.Edge`. This is the capability
      `conversations.list` used to provide, and its replacement is the least
      proven thing in the phase.
- [ ] **8.4 Select an unjoined channel and press enter.** It joins and opens.
- [ ] **8.5 Type "test" briskly, then check the tally.** `edge:channels/search`
      should be **1-2, not 4**. This is the debounce contract.
- [ ] **8.6 Type, pause a second, type more.** Two requests, not one — the
      debounce must not collapse a whole session into a single query.
- [ ] **8.7 Type quickly and hit enter fast.** The right channel opens; a late
      result for an earlier query does not swap the list under you.
- [ ] **8.8 Private channels and DMs still appear** in the finder with the right
      sigils.
      → Suspect: `finderChannelType` mapping (edge results carry flags, not a
      type string).
- [ ] **8.9 Archived channels do NOT appear.**

## 9. Emoji

Custom emoji now come from `conversations.view` where possible.

- [ ] **9.1 Custom emoji render in messages** in both workspaces.
- [ ] **9.2 The emoji picker offers custom emoji** in both workspaces.
      → Note: one workspace legitimately still calls `emoji.list` (the one whose
      `conversations.view` took the history fallback). Both should end up with
      emoji either way; if one workspace has none, that is a real bug.
- [ ] **9.3 Reactions with custom emoji still work.**

## 10. General regression sweep

Things no task touched, but the diff was large.

- [ ] **10.1 Send a message.** It appears, and appears in the official client.
- [ ] **10.2 Reply in a thread.**
- [ ] **10.3 Add and remove a reaction.**
- [ ] **10.4 Edit and delete a message.**
- [ ] **10.5 Scroll back through history** past the initial page.
- [ ] **10.6 Switch workspaces** (Ctrl+N) a few times. Sidebar, unread state and
      the finder all follow.
- [ ] **10.7 Upload a file.**
- [ ] **10.8 Open a permalink.**
- [ ] **10.9 Leave slk running for an hour.** Come back: no runaway request
      count in the tally, no memory blowup, messages still arriving.

---

## 11. Unblock the two capture-dependent items (optional, ~10 minutes)

`usergroups.list` and `stars.list` could both be dropped — `client.userBoot`
already returns the data — but `boot.Result.Subteams` and `boot.Result.Starred`
are `[]json.RawMessage` because **both existing captures show empty lists**.
There is no evidence for what a populated entry looks like, and guessing is how
this project has been burned twice.

To unblock, capture a `client.userBoot` response from a workspace that actually
has usergroups and starred channels:

1. Open Slack in a browser on a workspace where you have starred channels and
   which has usergroups (`@team-handles`).
2. DevTools → Network → filter `userBoot` → hard reload.
3. Save the **response** for `client.userBoot`.
4. Check the `subteams.self` and `starred` arrays are non-empty.

**That file contains your session token and real content — treat it like the
existing HAR captures: worktree root, gitignored, never committed, never
pasted into a document.**

---

## Results so far

| section | date | result |
|---|---|---|
| 1. Provenance | 2026-08-04 | pass, on the second attempt — the first ran a stale binary |
| 2. Cold cache | 2026-08-04 | **pass.** 235 `users.info` (was 37,573 on the old binary), 330 total, responsive throughout, no load spike. No `conversations.view` and two `emoji.list` calls, which is correct for a profile with no visit history: nothing to restore, so no view response, so the emoji fallback covers both workspaces. |
| 3. Unread state | 2026-08-04 | pass, provisionally — every item checked out by eye against the official client. Real confidence needs users. |
| 4. Muted / starred / membership | 2026-08-04 | starred present, no channels missing. Mute unverified: 4.1 originally assumed a visual treatment that does not exist; rewritten as a log check, not yet run. |

Two things manual QA found that the automated work did not:

- The counter tallied an asset URL whose path contained `/api/` as though it
  were an API call. Fixed; Slack method names never contain a slash.
- `conversations.members` reached 54 in a real session (one per channel
  visited) against 3 in a 35-second scripted run. It is bounded by user
  action rather than workspace size, so it is not a scraper signature, but it
  is the call `edge:users/list` would remove and this workspace has 40,504
  distinct channel members.

## What this checklist cannot tell you

It cannot tell you whether Enterprise Grid's anomaly detection is satisfied.
Every number in Phase 2b was measured on non-Grid workspaces, and the
`conversations.view` `channel` parameter — which slk depends on and falls back
from — has never been exercised against Grid at all.

Passing this list means slk works and its call pattern is no longer a scraper's.
It does not mean the original problem is solved. That needs a Grid account and
someone willing to risk it.
