# Handoff — 2026-08-05

Read this first. It is the state of play, not a design document. The
forward-looking documents are listed at the bottom.

## Where things are

| | |
|---|---|
| `main` | `0cfdcc7` — grid-parity merged (PR #121), release config updated, connect errors now logged |
| `fix/sidebar-from-userboot` | `96596e1` — **WIP, DO NOT MERGE OR TAG** |
| Released | `v0.13.0-beta.1` — GitHub pre-release, tap untouched, `v0.12.0` still "Latest" |
| Live bug report | [#5](https://github.com/gammons/slk/issues/5), an Enterprise Grid user (`@raff`) |

Phase 2b is done and measured: a 25-second two-workspace session went from
270 API requests to 37, with `users.list`, `conversations.list`, the
per-channel history backfill and the boot-time thread-subscription sweep all
deleted — three of them removed from the interfaces that could reach them, so
they fail a test rather than a review if reintroduced.

## THE BLOCKER — do this first, it needs nobody else

**A binary built from `fix/sidebar-from-userboot` makes 42
`conversations.members` calls per session. `main` makes 1-2.**

Fully reproducible locally. No Grid account, no volunteer, no waiting.

```bash
cd ~/local_code/slk
git checkout main               && go build -o /tmp/slk-before ./cmd/slk
git checkout fix/sidebar-from-userboot && go build -o /tmp/slk-after ./cmd/slk

cd .worktrees/grid-parity-phase1        # any dir; the log lands in $PWD
SLK_DEBUG=1 script -qec 'timeout -s INT 25 /tmp/slk-before' /dev/null >/dev/null 2>&1
grep -A20 'shutdown API request tally' slk-debug.log | grep conversations.members
# repeat with /tmp/slk-after
```

Measured, alternating runs: before **2**, after **42**, after **42**, before **1**.
So it is the binary, not cache state.

**What is ruled out:** only 3 `ChannelSelectedMsg` events occurred, so the
`MembershipFetch`-on-channel-switch path cannot account for it. The current
(inverted) version does not even call `bootConversations` when
`users.conversations` succeeds — and it succeeds on these workspaces — so the
mechanism is genuinely not understood.

**Where to look:** `conversations.members` is only issued by
`membership.Manager.backgroundFetch` (`internal/slack/membership/manager.go`),
reached via `EnsureFresh`. Callers are the `MembershipFetch` service closure and
`OnConnect` for the active channel. Add a caller-identifying log line to
`backgroundFetch` and diff the two binaries' logs. The diff between them is
small: `bootConversations` in `cmd/slk/bootstrap_adapters.go` plus a rewritten
error branch in `connectWorkspace`.

**Until this is explained, do not tag a beta.** 42 membership fetches per
session is the same shape of problem this entire phase exists to remove, and it
would be shipped to a user who has already been signed out of Slack once.

## What the WIP branch does, and the trap it already fell into

It makes `users.conversations` non-fatal, falling back to the conversations
`client.userBoot` returned. That fixes the Grid failure below.

**The first version had it the other way round — `userBoot` primary — and it
was wrong.** Measured on real workspaces:

| workspace | `users.conversations` | `userBoot` |
|---|---|---|
| Rands | **218** channels | 67 |
| Truelist | 71 | 60 |

`userBoot`'s `channels[]` is *not* the complete joined-conversation list.
Preferring it silently drops channels from the sidebar — worse than the bug it
fixes, because an absent channel is not noticeable. With the order inverted,
channel counts and `users.conversations` call counts match `main` exactly.

Do not re-derive this. It cost an A/B run to find and it is not documented
anywhere else.

## The Grid situation (#5)

`@raff` ran `v0.13.0-beta.1` on a large Grid org. **It no longer signs him out**
— that is this project's actual success criterion and it now has one data point
rather than zero. Two failures remain:

1. **No channels, no threads, "no active workspaces."** Diagnosed from his full
   log: `connectWorkspace` called `users.conversations`, it failed, the function
   returned an error, and the caller discarded it — so nothing was logged and a
   hard failure looked identical to an empty workspace. `stars.list` on the same
   org returns `enterprise_is_restricted`, so `users.conversations` almost
   certainly does too. **`0cfdcc7` on `main` now logs the reason**; the WIP
   branch is the actual fix.

2. **`channels/info could not resolve 217 ids`** — every conversation he has.
   Separate bug, not what breaks him. The workspace client derives its host from
   `auth.test` and correctly targets `<org>.n.slack.com` on Grid
   (`internal/slack/client.go:305`); the edge client hardcodes
   `edgeapi.slack.com` and scopes every request to a single team id
   (`internal/slack/edge/client.go:73`). slk models a workspace; Grid is an org
   of many. **Conditional revalidation — the mechanism two phases were built
   around — is therefore non-functional on Grid.** Fixable, unverifiable without
   a Grid account.

He has been asked to re-run once the logging fix is out, to confirm the
restricted-endpoint theory rather than leave it as an inference.

## Work available now, in priority order

Nothing here waits on raff.

1. **Explain the 42 `conversations.members`** (above). Blocks beta 2.
2. **Make the edge client Grid-aware** — derive its host as the workspace client
   does, and reckon with `/cache/<teamID>/` assuming one team when a Grid user's
   conversations span many. Cannot be *verified* without Grid, but can be
   written and reviewed.
3. **Task 11b** — mention picker on `edge.UsersSearch`, debounced. The last
   unfinished task in the Phase 2b plan. The picker's candidate list shrank when
   the per-member resolution was removed; this restores it. Entirely local.
4. **`edge.UsersList` for channel members** — one request per channel returning
   user records inline, which is what the official client does. Would take
   `conversations.members` to zero and is a strict improvement regardless of
   what (1) turns out to be.
5. **Batch resolver misses through `edge.UsersInfo`** — the ~200 `users.info`
   on a cold boot become ~4.
6. **QA sections 5-10** (`2026-08-03-grid-parity-qa-checklist.md`) — needs a
   human at a terminal, not a Grid account. §6, a real 90-second outage, is the
   most valuable: it is new code that runs on every wifi flap and has never seen
   one.
7. **Boot budget** — ~18 requests per workspace against a criterion of 10.
   `dnd.info` and `users.conversations` are dedupable against data already in
   hand. `usergroups.list` and `stars.list` are **blocked on capture evidence**:
   `boot.Result.Subteams` and `.Starred` are `[]json.RawMessage` because both
   existing captures show empty arrays. Guessing the shape is the mistake this
   project has made twice. §11 of the QA checklist describes the 10-minute
   browser capture that would unblock them.

## Traps this session hit — all of them cost real time

- **A stale binary.** `~/local_code/slk/slk` dates from July and predates
  everything. The first QA attempt ran it and "found" the original bug at full
  scale. Always build to a distinct name and run by absolute path; §1.1 of the
  QA checklist is a provenance check keyed on log strings that exist in exactly
  one of the two binaries. Run it every session.
- **Warm-cache measurements say nothing about a cold cache.** A 40,000-request
  fan-out hid behind a warm cache for four tasks. Measure cold first: copy
  `~/.local/share/slk/tokens` into a temp `XDG_DATA_HOME`, boot, then delete the
  token copy.
- **`slackhttp.Counter` records at `RoundTrip` entry** (`transport.go`), so its
  numbers are requests *started*, not delivered. With an unbounded fan-out most
  never reach Slack. Say "started", never quote it as delivered traffic.
- **A test can encode the bug.** `TestBackgroundFetchTriggersResolverForEachID`
  pinned one `users.info` per channel member — the exact behaviour that had to
  be deleted. Replaced by its inverse. If a test blocks a fix, question the test.
- **Assert before scripted edits.** A `python .replace()` against text copied
  from a *plan document* rather than the file silently did nothing, and the test
  kept failing for a reason that looked unrelated. Assert the pattern matched.
- **A mutation that fails to compile has proved nothing.** Re-adding a method to
  an interface broke the mock before the assertion could run; it had to be
  re-run with the mock restored.

## The documents that matter

| | |
|---|---|
| `2026-08-01-grid-parity-STATE.md` | State of the three phases; symbol table; standing risks |
| `2026-07-31-grid-parity-phase2b-outcomes.md` | Measured before/after, criteria scored honestly, first Grid evidence |
| `2026-08-03-grid-parity-qa-checklist.md` | The manual QA gate, with what to suspect when each check fails |
| `2026-07-31-grid-parity-phase2b-bootstrap-rewrite.md` | The 12-task plan; task 11b is the only one unfinished |
