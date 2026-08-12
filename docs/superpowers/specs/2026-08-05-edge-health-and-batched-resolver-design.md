# Edge health + batched user resolution

Date: 2026-08-05
Status: approved (direction), pending spec review

## Problem

Two request-amplification measurements from the first working
Enterprise Grid session (raff, gammons/slk#5, 2026-08-05):

1. **23 `edge:channels/info` calls at boot, every one wasted.** His
   conversations partition into 17 context-team groups: one enterprise
   id group (218 ids) that edgeapi accepts but resolves none of, and
   16 foreign-team groups (1–42 ids) whose calls all fail
   `Unauthenticated`. The groups are processed independently and to
   completion, so a workspace where edge resolution is wholesale
   broken pays the full cost of discovering that on every boot.

2. **282 `users.info` calls in one session.** `userResolver.Request`
   fires one goroutine and one Web API call per cache miss, with no
   coalescing. `edge.UsersInfo` accepts 80 ids per request and returns
   full user records inline; the same misses batched through it are
   ~4 requests.

Both are the same underlying question — *is edge working for this
workspace right now?* — asked in two places, so both are answered by
one mechanism.

## Design

### 1. Per-workspace edge-health signal (session-scoped)

A tiny thread-safe tracker (`internal/slack/edge`, one field on
`WorkspaceContext`) with two states: **unknown** and **degraded**.
Session-scoped only — nothing is persisted. Each cold boot
re-discovers degradation at a cost of a handful of calls; persisting
pessimism in SQLite risks suppressing the real Grid-scoping fix when
it lands, and is out of scope.

`bootstrap.revalidateChannels` evaluates the partition outcome:

- Groups are processed in **descending size order** (largest first),
  replacing the current alphabetical sort. On raff's org the
  enterprise-id group (79% of ids) is therefore judged first; on a
  non-Grid workspace the single workspace-team group is.
- **Wholesale failure** of a group means: the call errored, OR every
  non-IM id in the group came back in `failed_ids`. IMs are excluded
  because they ALWAYS land in `failed_ids` — 22 of 22 across the
  captures, on healthy workspaces — so including them would trip the
  rule everywhere. (revalidate.go already knows which ids are IMs.)
- If the largest group fails wholesale **and holds at least half of
  all ids being revalidated**, edge is marked degraded for the
  workspace, the remaining groups are aborted, and one log line says
  so. Raff's boot: 23 edge calls become ~4 (the largest group's own
  batches). A healthy workspace never trips: its largest group
  resolves. A workspace whose largest group is merely ratelimited
  degrades for the session — consequence is falling back to behaviour
  identical to today's (per-user resolution, foreign teams left
  stale), and it self-heals next boot.

### 2. Batched resolver misses

`userResolver.Request` keeps its contract (non-blocking, deduped,
cache-checked) but queues misses instead of firing immediately:

- A pending set (mutex-guarded) collects ids; the first enqueue arms
  a **200 ms timer**. This coalesces the render-path burst — a
  channel open resolves its unknown authors as one batch.
- On flush, if the workspace's edge health is NOT degraded and an
  edge client is wired: one `UsersInfo` call with `{id: 0}` for every
  pending id (0 is the protocol's "never seen, send full record";
  `fetchInfo` batches at 80 internally). Returned users are written
  via the existing `cache.UpdateUserFromEdge` path and each emits the
  same `UserResolvedMsg` the per-user path emits today (display-name
  fallback chain: display → real → handle, mirroring bootstrap's
  `userDisplayName`).
- **Fallback preserves correctness everywhere:** ids the edge call
  did not return, and ALL pending ids when the edge call errors or
  the workspace is degraded or no edge client is wired, go through
  today's per-user `users.info` goroutine path unchanged. On Grid
  today that means the batch attempt costs one failed edge call and
  then behaves exactly as slk does now; once boot marks the workspace
  degraded it costs nothing.

### 3. Foreign-team noise

The abort in (1) skips the foreign-team groups whose calls fail
`Unauthenticated` on Grid. Teams that were processed before the abort
keeps their current per-team log lines; no new mechanism.

## Explicitly out of scope

- Persisting edge health across sessions (SQLite + TTL). Session
  rediscovery costs ≤4 calls; revisit only if the Grid capture never
  arrives.
- The correct Grid edge scoping itself (enterprise-id groups,
  `Unauthenticated` teams). Blocked on raff's browser capture; this
  change deliberately makes no guess at it.
- Batching `RequestBot` (bots.info has no edge analogue).
- users/info revalidation at boot (`revalidateUsers`) — already one
  batched conditional call; unchanged.

## Testing

- Health: largest-group wholesale failure marks degraded and aborts
  remaining groups (bootstrap fake, per-team error injection
  exists); a healthy run stays unknown; IM-only failure does not
  trip; majority threshold respected (largest group < 50% of ids →
  no abort).
- Resolver: N requests → 1 edge call with all ids at version 0;
  returned users cached + one UserResolvedMsg each; unreturned ids
  fall back to per-user; edge error → all fall back; degraded or
  nil edge → zero edge calls; duplicate Request dedupes end-to-end.
- No-persistence guard: no new cache columns, no new files under
  XDG paths.
