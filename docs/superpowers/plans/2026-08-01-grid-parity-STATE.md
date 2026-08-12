# Grid Parity — Where This Is, 2026-08-03

**Read this first if you are picking the work up.** It records *state* and the
findings that live nowhere else. The design and the plan are the forward-looking
documents and are still accurate — this one says how far through them we got and
what changed underneath them.

## The three documents, and what each is for

| Document | Purpose |
|---|---|
| [`specs/2026-07-30-enterprise-grid-bootstrap-design.md`](../specs/2026-07-30-enterprise-grid-bootstrap-design.md) | The original three-layer design. **Partly superseded** — see *Corrections* below. |
| [`specs/2026-07-31-grid-parity-phase2b-design.md`](../specs/2026-07-31-grid-parity-phase2b-design.md) | Phase 2b design. Current. Its opening section records a wrong correction I made and then reversed; read it. |
| [`plans/2026-07-31-grid-parity-phase2b-bootstrap-rewrite.md`](./2026-07-31-grid-parity-phase2b-bootstrap-rewrite.md) | The 12-task plan. Current, **but its `- [ ]` checkboxes were never ticked** — use the table below instead. Tasks 9/10/11 were re-scoped mid-flight; the file reflects that. |
| [`plans/2026-07-30-grid-parity-phase1-outcomes.md`](./2026-07-30-grid-parity-phase1-outcomes.md) | Phase 1 retrospective. |
| [`plans/2026-07-30-grid-parity-phase2a-outcomes.md`](./2026-07-30-grid-parity-phase2a-outcomes.md) | Phase 2a retrospective. Its *Test-integrity findings* section is the most useful thing to read before writing any test here. |
| [`plans/2026-08-03-grid-parity-qa-checklist.md`](./2026-08-03-grid-parity-qa-checklist.md) | **The manual QA gate.** Nothing in Phase 2b was confirmed to still *work* — only that it stopped enumerating. Run this before any Grid test. |
| [`plans/2026-07-31-grid-parity-phase2b-outcomes.md`](./2026-07-31-grid-parity-phase2b-outcomes.md) | **Phase 2b retrospective, 2026-08-03. Read its first section before anything else** — it opens with a cold-cache regression that blocks Grid testing. |

## Where we are

Branch `feat/grid-parity`, worktree `.worktrees/grid-parity-phase1` (the
directory name is a leftover; it holds all three phases). Draft PR
[gammons/slk#121](https://github.com/gammons/slk/pull/121). HEAD `c60b92a`,
tree clean.

Phases 1 and 2a are complete. Phase 2b is **11 of 12 tasks done, one half-done**.

| Task | State | Key commits |
|---|---|---|
| 1. Request counter | done | `372cd0f`, `91de452`, `7d53bcb`, `bec5ab3` |
| 2. Cache partial writers | done | `23dd441`, `9b0d52b` |
| 3. `edge.User.ImageOriginal` + membership batch presence | done | `2c2c619`, `2350c4c` |
| 4. `internal/bootstrap` skeleton | done | `cbede9d` |
| 5. `conversations.view` + fallback | done | `5b210fd`, `26adbc0` |
| 6. Scoped revalidation | done | `1997284`, `263464c` |
| 7. Wire `connectWorkspace` | done | `12b5a7d`, `26adbc0` |
| 8. Delete the `users.list` sweep | done | `35a0611` |
| 9. Delete `triggerBackfill`, bound reconnect | done | `f08011c` |
| 10. Defer boot-time `subscriptions.thread.getView` | done | `80abaf7` |
| 11a. Finder → `edge.ChannelsSearch`, delete `conversations.list` | done | `c60b92a` |
| 11b. Mention picker → `edge.UsersSearch` | **not started** | |
| 12. Verification + outcomes doc | done (this session) | |

**slk no longer runs the old enumeration paths.** `users.list`,
`conversations.list`, the per-channel `conversations.history` backfill and the
boot-time thread-subscription sweep are all gone, three of them removed from the
interfaces that could reach them so they cannot come back without failing a
test. A 25-second two-workspace session went from 270 API requests to 44.

**The cold-cache `users.info` fan-out is FIXED (2026-08-03).** It is recorded
here because the way it was found is the most useful thing in this document.

On a cold cache a 35-second boot used to start **40,523 `users.info` requests**,
one per distinct channel member (`select count(distinct user_id) from
channel_members` returned 40,527 on the same workspace). `membership.Manager`
asked the user resolver about every member of every channel it fetched, and the
resolver spawned one goroutine and one request per cache miss. The `users.list`
sweep used to fill that cache before the manager got going, so deleting the
sweep in Task 8 did not create the fan-out — it removed what was hiding it.
Warm cache: 2 requests, which is why four tasks went by without noticing.

Fixed by deleting the per-member resolution (`membership.Manager.backgroundFetch`
still fetches and caches the id list, which is one bounded call per channel) and
bounding `userResolver.Request` with an 8-slot semaphore acquired inside its
goroutine. Same protocol after: `users.info` **200**, total **242**.

**Two lessons worth carrying forward:**

- **Measure cold first, not last.** Every measurement across four tasks was
  taken against a warm cache holding 42,992 users that the deleted sweep had put
  there. The instrument was blind to the only case that matters — a fresh
  install — and it was Task 12's checklist that finally pointed it there.
- **`slackhttp.Counter` records at `RoundTrip` entry** (`transport.go:110`), so
  its numbers are requests *initiated*. With an unbounded fan-out most of those
  40,523 never reached Slack; they queued. Say "started", not "issued", and
  never quote such a figure as delivered traffic.

**The captures say the old work was unnecessary, not merely inefficient.**
Counted across all 8: `/api/users.info` **0**, `/api/conversations.members`
**0**, `/api/users.list` **0**. The official client uses `edge:users/list` for
one channel at a time with `count: 30, present_first: true` (full user records
inline, no resolution step), `edge:channels/membership` to test a specific set
of users, and batched `edge:users/info` to revalidate — 291 records in 30
responses. `internal/slack/edge` already implements all three, tested, and
`membership.Manager` still uses none of them. Moving to `edge:users/list` and
batching the remaining resolver misses are now optimisations rather than
blockers; see the Phase 2b outcomes doc.

## Corrections to the original design, made during the work

The Layer 2 design predates any measurement. Four of its claims are wrong.

1. ~~**`conversations.list` IS called.**~~ *Resolved 2026-08-03: Task 11a
   deleted `GetAllPublicChannels` and `fetchBrowseableChannels`, and removed
   `GetConversations` from `SlackAPI`, so it is now structurally impossible.
   The paragraph below is kept because the way this was got wrong is worth
   remembering.* The design said so, I "corrected" it to dead code, and I was wrong — that reversal is documented at the top of the 2b design. The live path is `Client.GetAllPublicChannels` ← `fetchBrowseableChannels` ← the `go` spawn in `run`. **Find these by symbol, not by line** — see *A note on line citations* below. It pages at `Limit: 1000` to populate the finder with unjoined channels. **Deleting it belongs with Task 11**, next to the `edge.ChannelsSearch` move that replaces it — deleting it earlier drops unjoined channels from the finder with nothing in their place.

2. **The WebSocket does not replay missed messages.** The design inferred from slk's socket params (`sync_desync=1`, `ms_latest=true`, `flannel=3`, `lazy_channels=1`) that slk probably receives the same replay as the official client. Measured 2026-08-01: after a 90-second outage the socket delivered ~160 `presence_change` events and nothing else. **`client.counts` stays in the reconnect path.**

3. **slk never refreshes `client.counts` on reconnect today.** `rtmEventHandler.OnConnect` does presence/DND, a section rebootstrap, backfill and a membership refresh — no counts. That is why a message posted during an outage never appeared. Task 9's bounded handler is a **user-visible bug fix**, not only a fingerprint change.

4. **The reconnect cost is not where the design put it.** Measured on a 105-channel workspace:

   ```
   channel-phase       total_msgs=0   dur_ms=2711     <- 2.7s, found nothing
   subscription-phase  subs=1000      dur_ms=132248   <- 132s
   ListThreadSubscriptions: hit hard cap 1000, stopping   (x4 per session)
   ```

   The thread-subscription sweep is **50x** the channel backfill and runs on
   every reconnect. It moved from a Task 10 cleanup into Task 9.

   Also measured: 288 per-channel `conversations.history` calls in one
   3-minute session, **250 of them (86%) returning zero messages**. The
   design's "most calls return zero messages quickly, so this is harmless" is
   confirmed on the first half and refuted on the second.

5. **`conversations.view`'s `channel` param works.** The design's one flagged
   unknown. Verified 2026-08-01 on two non-Grid workspaces — honoured both
   times, no fallback. **Still unverified on Grid**, so the probe-and-compare
   stays.

## Measurement: read this before quoting any number

**"Boot slk and quit" is not a repeatable protocol.** Totals are dominated by
background sweeps racing process shutdown. Same binary, same ~5-second session:
`users.list` came back **12, 34, and 97**; `users.info` 2 and 60. An early
"180 calls" baseline in this session's history is noise and should not be
quoted.

What works: build both commits (`git archive` the parent), run them at matched
durations, and compare **per-endpoint attributable deltas**, not totals. Task 12
must do this. The Task 7 delta below was derived that way and is stable across
runs:

```
+2 client.userBoot   +2 conversations.view   +3 edge:channels/info
+2 edge:users/info   +2 client.counts        -2 users.prefs.get   = +9 net
```

The instrument is `slackhttp.DefaultCounter`, dumped at shutdown under
`SLK_DEBUG=1` (`grep -A40 'shutdown API request tally' slk-debug.log`).

## A note on line citations

**Do not trust a `file.go:NNN` citation in any of these documents, including
this one.** Line numbers drift with every task, and that drift caused the single
worst error in this project: the Layer 2 design cited `conversations.list` at
`main.go:2177`, the line had moved, I searched for a wrapper under a name I had
inferred from the prose rather than verified, found nothing, and concluded the
call was dead code. It was not. I then wrote that conclusion into two documents
and stated it confidently before a measured boot disproved it.

Cite and search by **symbol**. The symbols that matter, current at `26adbc0`:

| Symbol | File | Line at `26adbc0` |
|---|---|---|
| `userResolver.Request` (the cold-cache fan-out) | `cmd/slk/main.go` | 296 |
| `membership.Manager.backgroundFetch` (its caller) | `internal/slack/membership/manager.go` | 165 |
| `rtmEventHandler.OnConnect` | `cmd/slk/main.go` | 3782 |
| `rtmEventHandler.syncOnReconnect` | `cmd/slk/main.go` | 3846 |
| `reconnectSync.run` | `cmd/slk/reconnect_sync.go` | 89 |
| `searchChannelsRemote` | `cmd/slk/channel_search.go` | 44 |
| `ensureThreadSubscriptions` | `cmd/slk/thread_subscriptions.go` | 147 |

Deleted in this session, so that citations to them elsewhere are known-dead:
`Client.GetAllPublicChannels`, `Client.GetUsers`, `fetchBrowseableChannels` and
its `go` spawn, `rtmEventHandler.triggerBackfill`, the whole `backfiller` type
(`cmd/slk/reconnect_backfill.go`), `cache.BackfillCandidates`, and the
`GetConversations` / `GetUsersContext` methods on `SlackAPI`.

Re-derive with `rg -n 'func searchChannelsRemote'` before acting on any of
them.

## Operational notes

- **The 8 HAR captures live in the worktree root** and hold live `xoxc`/`xoxd` credentials and real message content. They are gitignored via `.gitignore` **and** `.git/info/exclude`. Never `git add -A`. Never paste raw capture content into a file or a commit message. Sanitized aggregates go in `internal/slack/testdata/phase2-api-contracts.json`; `/tmp/opencode/phase2_fixtures.py` shows the pattern and has an assert-no-token-leak check.
- **The fixture extractor truncates.** It keeps `samples[:3]` and summarises `results[0]`, so any per-field claim about an array element is a single-element generalisation unless it has a denominator. That bug produced a wrong avatar contract in Phase 2a. The `measured` blocks added later cover all observations; prefer those.
- **Very long subagent prompts get cancelled.** Several dispatches in this session died mid-flight — one left a live `// MUTANT` marker in `counter.go`. If a task ends unexpectedly, `grep -rn MUTANT internal/ cmd/` and check `git status` before trusting anything.
- **`go test ... | tail` reports tail's exit status.** Five implementers on this project recorded false mutation results that way. Redirect to a file and read `$?` on the next line.
- **Removing a struct tag is usually not a valid Go mutation** — `encoding/json` falls back to case-insensitive field-name matching. Use `json:"-"` or a mis-tag.
- Gate: `go build ./... && go vet ./... && go test ./... -race && golangci-lint run ./...`
- Network isolation check needs loopback up: `unshare -rn sh -c 'ip link set lo up && go test ./...'`. Bare `unshare -rn` leaves `lo` DOWN and every `httptest` test fails for the wrong reason.
- `gofmt -l` reports ~30 unformatted files repo-wide, all predating this branch. Do not reformat them; only files you touch must be clean.

## Standing risks

- **The cache column mapping** is the most likely source of silent damage. `edge` results cover different column subsets than `UpsertChannel`/`UpsertUser` write, so revalidation goes through the partial writers in `internal/cache/edge_sync.go`. If avatars, membership or starred state start disappearing, look there first.
- **`Result.Messages` is fetched and discarded.** `conversations.view` is currently pure cost — the channel still renders through the old cache + `GetHistory` path. Tasks 8-11 wire it. The `[]slack.Message` → `[]json.RawMessage` conversion in `cmd/slk/bootstrap_adapters.go` is lossy and unvalidated against a real render.
- **Cold-cache convergence takes two boots.** The partial writers are UPDATE-only, so on an empty cache they find no rows; first-sight hydration inserts at version 0 and the next boot re-requests in full. Bytes, not correctness.
- **Warm-cache measurements say nothing about a cold cache.** That is how the
  40k fan-out survived four tasks. Any claim about slk's call pattern needs a
  run against an empty cache: copy `~/.local/share/slk/tokens` into a temp
  `XDG_DATA_HOME`, leave the rest absent, boot, and delete the token copy
  afterwards.
- **Nobody has tested any of this on Enterprise Grid**, and nobody should until all three phases land. Two contributors have already been signed out helping diagnose the original problem. The Phase 2a outcomes doc leads with this and so should any summary.
