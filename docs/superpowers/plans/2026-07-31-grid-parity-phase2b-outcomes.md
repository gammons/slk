# Grid Parity Phase 2b — Outcomes

## FIRST GRID EVIDENCE — 2026-08-04

A Grid user tested `v0.13.0-beta.1` (gammons/slk#5). Both halves matter.

**It no longer signs them out.** That is this project's actual success
criterion, and it now has one data point behind it rather than zero. Every
number elsewhere in this document is a proxy for that sentence.

**And conditional revalidation does not work on Grid at all.**
`channels/info could not resolve 217 ids` — every conversation they have.
The likely cause is a code-level asymmetry nobody could have caught without a
Grid account:

| | host | scoping |
|---|---|---|
| workspace API (`internal/slack/client.go:305`) | `deriveAPIBaseURL(resp.URL)` → `https://<org>.n.slack.com/api/` on Grid | grid-aware |
| edge API (`internal/slack/edge/client.go:73`) | hardcoded `https://edgeapi.slack.com` | `/cache/<single teamID>/…` |

slk models a workspace; Grid is an org containing many. If a user's
conversations span several workspaces in the org, asking one team's edge cache
about all of them fails for all of them — which is exactly the log line.

Also reported: `stars.list` returns `enterprise_is_restricted` (benign, the
Starred section hides), and **the channel list renders empty** — not yet
explained, since failed revalidation deliberately leaves channels stale rather
than removing them. Diagnostics requested on the issue.

**What this says about the method.** The top of this document and the STATE doc
both said "nobody has tested this on Grid" and treated it as the standing risk.
That risk materialised on the first contact, in the one subsystem two phases
were built around. Measurements on non-Grid workspaces could not have found it;
neither could the test suite, which fakes the edge client. Phase 2c's first task
is making the edge client grid-aware, and its first verification is a Grid user,
not a counter.

## Should a Grid tester try this yet?

**Not yet, but the blocker below is fixed.** See *Status of the cold-cache
regression* at the end of this section for the after-numbers; the remaining
reasons to wait are the four unverified manual checks, not a known burst.

The rest of this section is kept as written, because how the bug was found and
what it looked like matters more than the fact that it is now gone.

**No.** One measurement taken while writing this document says so, and it is
the most important thing here:

**On a cold cache, a 35-second boot started 40,523 `users.info` requests —
one per distinct channel member in the workspace.**

Read "started" literally: `slackhttp.Counter` records at `RoundTrip` entry, not
at completion (`internal/slackhttp/transport.go:110`). slk spawns one goroutine
and one request per unresolved user with no worker pool, so all ~40k enter the
transport within moments of each other and then queue on the connection pool.
How many actually reached Slack in those 35 seconds is unknown and is certainly
far smaller — the absence of error lines in the debug log is evidence that most
never returned at all. **Do not quote this as "40k requests hit Slack."** What
it establishes is the fan-out's *shape*: unbounded, and sized by workspace
membership rather than by anything on screen.

The count matches an independent number almost exactly:

```
counter:                                        40,523  users.info
select count(distinct user_id) from channel_members ->  40,527
```

That is one request per distinct channel member, which is the mechanism below,
confirmed arithmetically rather than argued.

It is a direct consequence of Task 8: deleting the `users.list` sweep removed
the thing that was keeping an existing per-user fan-out dormant. On a warm cache
the same boot issues **2**, which is why every other measurement in this
document looks as good as it does and why this went unnoticed for four tasks.

**Who hits it:** anyone whose cache is empty — i.e. a fresh install. That is
exactly the state a new Grid tester would be in.

Nobody should point this at an Enterprise Grid account until that is fixed. Two
contributors have already been signed out helping diagnose the original problem.

Everything below is still true; it is just not the whole picture.

---

## What landed

Tasks 1-7 landed in earlier sessions. This session did 8, 9, 10 and half of 11.

| Task | State | Commit |
|---|---|---|
| 8. Delete the `users.list` sweep | done | `35a0611` |
| 9. Delete `triggerBackfill`, bound reconnect | done | `f08011c` |
| 10. Defer boot-time `subscriptions.thread.getView` | done | `80abaf7` |
| 11a. Channel finder → `edge.ChannelsSearch`, delete `conversations.list` | done | `c60b92a` |
| 11b. Mention picker → `edge.UsersSearch` | **not done** | — |
| 12. Verification | this document | — |

## Measured call counts

Same protocol every time: `SLK_DEBUG=1`, two workspaces (105 and 39 channels),
a 25-35 second session, `grep -A40 'shutdown API request tally' slk-debug.log`.
Run under `script` for a pty, terminated with SIGINT so the shutdown tally is
written.

| Endpoint | Task 7 baseline | After 11a | Deleted by |
|---|---|---|---|
| `conversations.history` | 144 | **3** | Task 9 |
| `subscriptions.thread.getView` | 79 | **0** | Task 10 |
| `conversations.list` | 4 | **0** | Task 11a |
| `users.list` | 2 | **0** | Task 8 |
| `client.counts` | 6 | 6 | — |
| `users.conversations` | 3 | 3 | — |
| **session total** | **270** | **44** | |

**Read the totals with suspicion, the per-endpoint rows without.** The session
total is dominated by whichever background sweep was still running when the
process was killed — the same binary produced 270, 312 and 173 across three
runs of different lengths during this session. The four endpoint rows are
attributable deltas: each went to zero because the code that called it no
longer exists.

Three of the four are now **compile-time** facts rather than behavioural ones:

- `GetUsersContext` (`users.list`) and `GetConversations` (`conversations.list`)
  are gone from the `SlackAPI` interface, which is the only route from slk into
  slack-go. `TestSlackAPI_DeclaresNoWorkspaceEnumeration` fails if either
  returns.
- The reconnect path's client interface declares exactly one method.
  `TestReconnect_ClientSurfaceCannotEnumerate` fails if it grows, and
  `TestReconnect_IsO1NotOChannels` compares a 3-channel workspace against a
  300-channel one and requires the same call list from both.

### Success criteria, honestly scored

| Criterion | Result |
|---|---|
| 1. Boot ≤ 10 API calls | **Not met.** ~22 per workspace-pair boot before background work. Down from ~135, but the budget was 10. |
| 2. Reconnect is O(1) | **Met** in unit tests (3 vs 300 channels, identical call list). Not verified against a real outage — see *Not verified* below. |
| 3. Zero `users.list` | **Met**, structurally. |
| 4. Zero `conversations.list` | **Met**, structurally. |
| 5. ≤ 1 `conversations.history` at boot | **Not met**: 3. One is the `conversations.view` fallback, the others are the reconnect refresh of the active channel firing on first connect. |

## The cold-cache regression, in detail

Reproduce: copy `~/.local/share/slk/tokens` into a temp `XDG_DATA_HOME`, leave
the cache absent, boot.

```
API requests: 40604 total across 18 endpoints
  40523  users.info
     42  conversations.members
```

42 `conversations.members` responses covering 40,527 distinct members, and one
`users.info` started for each. The chain:

1. `membership.Manager.backgroundFetch` fetches `conversations.members` for a
   channel and then calls `resolver.Request(id)` for **every member**
   (`internal/slack/membership/manager.go:165`).
2. `userResolver.Request` short-circuits when the user is already in the cache
   (`cmd/slk/main.go:318`). Its own comment says exactly why that gate is there:
   *"without this, every channel-switch refetches users.info for each member,
   which is O(channel-size) API calls per switch (a 1000-member shared channel =
   1000 calls)"*.
3. On a miss it spawns **one goroutine per user**, unbounded.

The `users.list` sweep used to fill that cache in ~50 pages before the
membership manager got going, so the gate hit on nearly every call. Deleting the
sweep did not create this fan-out; it removed the thing that hid it. The gate is
still correct — it is the *population strategy behind it* that Task 8 removed
without replacing.

Two things are wrong and both need fixing:

- **Unbounded.** One goroutine and one request per user, no worker pool, no rate
  limit. Even the right number of users would arrive as a burst — and the burst
  is the fingerprint, independent of how many requests survive the queue.
- **Eager.** These are the members of every channel the manager touches, not the
  authors of messages on screen. The mention picker shows at most 7 rows.

### What the official client does instead — counted, not guessed

Across all 8 captures:

| endpoint | occurrences |
|---|---|
| `/api/users.info` | **0** |
| `/api/conversations.members` | **0** |
| `/api/users.list` | **0** |
| `edge:users/info` | 30 responses carrying **291 user records** |
| `edge:users/list` | 4 requests, each `channels:[1]`, `count: 20-30`, `present_first: true` |
| `edge:channels/membership` | 10 requests, `users` arrays of length 1-66 |

slk is wrong in two independent ways, and the second is the bigger one:

1. **Wrong endpoint.** The official client never calls `/api/users.info`. It
   revalidates in batches through `edge:users/info` — 291 records in 30
   responses, roughly ten users per request, keyed `{id: version}`.
2. **Wrong question.** slk asks `conversations.members` for *every* member id of
   a channel and then resolves each one. The official client asks
   `edge:users/list` for **one channel, `count: 30`, `present_first: true`** —
   the first page of members, present users first, with full user records
   inline and a `next_marker` for the rest. There is no resolution step at all.
   When it needs to know whether specific users are in a channel it sends
   `edge:channels/membership` with an explicit `users` array and reads back
   `members` / `non_members` (10 for 10 observed responses satisfy
   `members + non_members == users sent`).

So the fan-out is not the same work done less efficiently. It is work the
official client never does: a 40,000-member workspace costs it thirty user
records per channel view.

**The fix is wiring, not protocol work.** `internal/slack/edge` already has all
three methods, built and tested in Phase 2a and currently unused by
`membership.Manager`:

```go
func (c *Client) UsersList(ctx, channelID string, count int) (users []User, truncated bool, err error)
func (c *Client) ChannelsMembership(ctx, channelID string, userIDs []string) (members, nonMembers []string, err error)
func (c *Client) UsersInfo(ctx, updatedIDs map[string]int64) ([]User, error)
```

Order of work: point `membership.Manager` at `UsersList` so the per-member
resolution never starts; bound `userResolver.Request` with a worker pool so
whatever misses remain cannot burst; route those misses through `UsersInfo`
batches rather than one request each.

### Status of the cold-cache regression — FIXED 2026-08-03

Two changes, in `membership.Manager` and `userResolver`:

1. `backgroundFetch` no longer calls `resolver.Request` for every member. The
   member id list is still fetched and cached — one bounded call per channel,
   and the mention picker's in-channel ordering reads it — but slk no longer
   asks who each of those people *is*. A test that asserted the old behaviour
   (`TestBackgroundFetchTriggersResolverForEachID`) was replaced by its inverse;
   it was encoding the bug.
2. `userResolver.Request` acquires from an 8-slot semaphore inside its
   goroutine, so it still returns immediately to its caller (it is called from
   the render path and from WS event handlers) but cannot open an unbounded
   number of round trips. Removing the semaphore in a mutation run produced a
   measured peak of 58 concurrent requests from 60 calls; with it, 8.

Same cold-cache protocol, 35-second boot:

| | before | after |
|---|---|---|
| `users.info` | 40,523 | **200** |
| `conversations.members` | 42 | 3 |
| **total** | **40,604** | **242** |

The remaining 200 are the render path resolving message authors and DM
counterparties against an empty cache, now rate-bounded. That is proportional
to what is on screen rather than to workspace membership, which is the property
that matters. Batching them through `edge:users/info` and moving the member
list to `edge:users/list` are both still worth doing — they are optimisations
now, not a blocker.

## What contradicted the plan

- **Task 10's hook was wrong on the first attempt.** The plan says to move the
  subscription fetch to "the first open of the Threads view", and the obvious
  hook — the threads list fetcher — also runs on workspace-ready, because the
  sidebar draws a Threads unread badge before the view is ever opened. Hanging
  the network call there left `subscriptions.thread.getView` at 60 per boot. It
  took a measured run to notice; the unit tests were all green. The fix was a
  separate `ThreadService.EnsureSubscriptions` called only from the
  `ThreadsViewActivatedMsg` arm, with a test asserting workspace-ready does
  *not* trigger it.
- **The plan's Task 9 replacement said "the active channel only, via the normal
  open path with cached_latest_updates".** The normal open path
  (`fetchChannelMessages` → `GetHistory`) does not use `cached_latest_updates`;
  only `internal/bootstrap`'s fallback does. The reconnect refresh goes through
  the normal path as-is. That is a bytes optimisation left undone, not a call
  count one — it is still exactly one request.
- **`cache.BackfillCandidates` was deleted; `ChannelsWithMessages` was kept.**
  The plan left the choice open. `BackfillCandidates` existed only to enumerate
  the fan-out's candidates and means nothing without it; `ChannelsWithMessages`
  is a plain query with no fan-out semantics baked in.
- **`GetHistorySince` was kept** despite losing its only caller. The
  per-channel primitive is not the bug; the loop over channels was.

## The boot budget: why it is 18 and not 10

Criterion 1 asks for ≤ 10 API calls per boot. After removing the duplicated
`client.counts`, the duplicated first-connect catch-up and the duplicated
`emoji.list`, a two-workspace session is 37 requests — call it 18 per
workspace. The remaining calls sort into three groups, and only one of them is
ordinary work.

**Removed this session** (each was asking a question already answered):

| call | was | now | replaced by |
|---|---|---|---|
| `client.counts` | 3/ws | 1/ws | `bootstrap.Result.Counts` + `CountsOK` |
| `conversations.history` | 1.5/ws | 0.5/ws | first connect no longer repeats the catch-up |
| `emoji.list` | 1/ws | 0.5/ws | `conversations.view`'s `emojis` |

`emoji.list` is 0.5 rather than 0 because the workspace whose
`conversations.view` took the history fallback gets no emoji map with it, and
the background fetch still covers that case. That is the documented cost of the
fallback, working as intended.

**Blocked on capture evidence, not on effort:**

- `usergroups.list` (1/ws). `boot.Result.Subteams` is documented as replacing
  it, but its element type is `[]json.RawMessage` because **both captures show
  `"self": []`** — an empty list on the captured workspace. There is no evidence
  for what a populated entry looks like, so mapping it to
  `map[id]handle` means inventing a contract. Needs a capture from a workspace
  that actually has usergroups.
- `stars.list` (1/ws). Same shape of problem: `boot.Result.Starred` is
  `[]json.RawMessage` with the element shape unmodelled.

Inventing either is the Phase 1 `sec-ch-ua` mistake and the Phase 2a avatar
mistake, in that order. Do not guess these; capture them.

**Real work, no blocker:**

- `users.conversations` (1.5/ws) — `Client.GetChannels`. `userBoot` already
  returned `channels` and `ims`, and `bootstrap.Result` already exposes both.
  The work is a `boot.Channel` → `sidebar.ChannelItem` conversion, because
  `buildChannelItem` takes a `slack.Channel`. This is the sidebar's source of
  truth, so it is the riskiest single change left in this area.
- `conversations.members` (1/ws) — should become `edge:users/list`, which is
  what the official client uses and which returns user records inline.
- `dnd.info` (1/ws) — `bootstrap.Result.DND` already carries it. The call site
  is `bootstrapPresenceAndDND`, which runs from `OnConnect` on every connect and
  genuinely needs fresh data on a *re*connect, so this one needs the same
  first-connect awareness the catch-up now has.

**Ordinary work, leave alone:** `client.userBoot`, `client.counts`,
`conversations.view`, `users.channelSections.list` (2/ws is pagination, not
duplication), `auth.test`, `client.shouldReload`, `edge:channels/info`,
`edge:users/info`, `users.getPresence`.

Best case without new captures is roughly 18 → 14 per workspace. Criterion 1 is
not reachable without either the two capture-blocked calls or a decision that
some of the "ordinary" set is not worth its request.

## Not verified

Four things need a human at a terminal and are **not** claimed:

1. **Names resolve for authors you have never DMed** (Task 8). The cold-cache
   finding above suggests they resolve *aggressively*, but the rendered result
   was never looked at.
2. **The Threads view populates on first open** (Task 10). The trigger is
   tested; the view is not.
3. **The finder shows channels you have not joined, and a four-character burst
   produces one or two `edge:channels/search` rather than four** (Task 11a). The
   debounce is unit-tested against synthesised ticks; no real keystroke has gone
   through it.
4. **A real outage → reconnect issues a small constant number of calls**
   (Task 9). First-connect exercises the same handler and was measured; a
   genuine WebSocket drop was not.

Also not done: the fresh-profile cold boot the plan asks for. It needs
interactive re-auth, which cannot be driven headlessly.

## Gate

Green after every commit:

```
go build ./... && go vet ./... && go test ./... -race && golangci-lint run ./...
```

`golangci-lint`: 0 issues. Network isolation confirmed:

```
unshare -rn sh -c 'ip link set lo up && go test ./...'   # all ok
```

Every mutation listed in the plan for Tasks 8-11a was run and killed its test;
the two worth naming are (a) re-adding `ListThreadSubscriptions` to the
reconnect client interface, and (b) fanning the reconnect refresh out over every
cached channel — the exact bug Task 9 exists to prevent. Both fail loudly.

One mutation initially failed for the wrong reason: adding `GetUsersContext`
back to `SlackAPI` broke the mock's compile before the assertion could run. It
was re-run with the mock method restored so the reflection assertion was what
failed. A mutation that kills a test by breaking the build has proved nothing.

## Open items, in priority order

1. **The cold-cache `users.info` fan-out.** Blocks Grid testing.
2. **Task 11b**: the mention picker still filters the in-memory roster locally
   rather than calling `edge.UsersSearch`. Not a regression — it is what it
   always did — and less urgent than it looks, because the roster it filters is
   now much smaller.
3. **`users.conversations` (3 per boot)**: `Client.GetChannels` still enumerates
   the joined channel list, and `bootstrap`'s own regression guard lists
   `users.conversations` as forbidden. `userBoot` already returns the same
   conversations. Nothing in Phase 2b's plan deletes it.
4. **`client.counts` (6 per boot)**: `bootstrap.Run` and the first-connect
   reconnect handler both call it, per workspace. One of the two is redundant.
5. **Boot budget**: ~22 calls against a criterion of 10.
