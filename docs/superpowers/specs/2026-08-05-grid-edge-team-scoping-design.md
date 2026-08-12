# Grid-aware edge revalidation — team-scoped channels/info

Date: 2026-08-05
Status: approved (design), unverifiable on Grid until raff runs it

## Problem

On Enterprise Grid, slk's edge revalidation resolves nothing. Raff's
log (gammons/slk#5): `channels/info could not resolve 217 ids` — every
conversation he has. That message is the `FailedIDs` branch of
`revalidateChannels` (internal/bootstrap/revalidate.go), which only
prints after an HTTP 200 + `ok:true` response. So
`edgeapi.slack.com` accepts a Grid user's request; what fails is the
`/cache/<teamID>/` path scoping. The edge client
(internal/slack/edge/client.go:73) builds that path from the single
team id `auth.test` returned, but on Grid a user's conversations are
owned by many teams within the org, and the edge cache keys them under
their owning team.

Conditional revalidation — the mechanism Phase 2 was built around — is
therefore non-functional on Grid, and every boot leaves the whole
cache stale there.

## Evidence and non-evidence

- The 8 HAR captures behind the edge package are all from Rands
  (T04T4TH8W), a non-Grid workspace. There is **no capture evidence**
  of what the official client does for edgeapi on Grid. (The comment
  at internal/slack/edge/cache.go claiming "a live Grid workspace" is
  wrong and is corrected as part of this work.)
- userBoot already decodes `context_team_id` for every channel
  (internal/slack/boot/boot.go:117) and IM (boot.go:143), and
  conversations.view users carry `team_id`
  (internal/slack/boot/view.go:67). None of it is consumed today.
- Failure here is already non-fatal: unresolved ids are left stale and
  logged. A wrong guess degrades to today's behavior, not worse.

## Decision

**Team-scoping only.** The host is not changed: raff's log shows
edgeapi.slack.com serving his requests, and switching hosts would risk
breaking a path that currently degrades gracefully. (The handoff's
"derive its host as the workspace client does" framing predates this
reading of his log.)

**channels/info only.** users/info may fail the same way on Grid, but
there is no evidence it does, and regrouping users could break a path
that works for him today. Search, UsersList, UsersCounts and
ChannelsMembership are likewise unchanged. Raff's next log is the
evidence-gathering step for all of them.

## Design

1. `edge.Client.call` takes the team id per request instead of reading
   `c.teamID`; the URL becomes `/cache/<team>/<endpoint>` with the
   team chosen by the caller. The struct field stays as the default
   for the endpoints that keep their current scoping (search,
   members, users/info).

2. `ChannelsInfo` gains an explicit team parameter:
   `ChannelsInfo(ctx, teamID string, updatedIDs map[string]int64)`.
   A signature change, not a new `ChannelsInfoForTeam` method: one
   production caller plus mocks, and a mandatory parameter forces every
   future caller to decide the scoping instead of inheriting a default
   that is wrong on Grid.

3. `bootstrap.revalidateChannels` partitions `updated_ids` by each
   conversation's `context_team_id` from userBoot, then calls
   `ChannelsInfo` once per team, batching within each group at 60 as
   today. **An empty or absent `context_team_id` groups under the
   workspace's own team id**, so any entry lacking the field behaves
   exactly as it does now.

4. Failure logging names the team
   (`channels/info for team T… could not resolve N ids`), so the next
   Grid log says which scoping failed, not just that one did.

5. Result handling across teams: each team is processed fully inside
   its own iteration — call, write-through, membership, failed-ids —
   rather than merging per-team results into one aggregate first.
   (Amended during implementation: the aggregate design below was
   superseded. Per-team processing gives failure independence for
   free, and the MemberChannels/MembershipQueried pairing invariant
   holds trivially because a queried set never crosses the partition.)

## Why this is safe to ship unverifiable-on-Grid

On a non-Grid workspace every `context_team_id` is expected to equal
the auth.test team id, which makes the partition a single group and
the request shape byte-identical to today. Verified locally before
merge: run a 25-second session and confirm the `edge:channels/info`
tally and count are unchanged, and that the debug log shows exactly
one team group. If any shared channel on a non-Grid workspace carries
a foreign context team, the change produces additional, correctly
scoped requests — more traffic, but semantically right, and bounded by
the same batching.

## Explicitly out of scope

- Deriving the edge host from auth.test's URL (rejected: no evidence
  the host is wrong; adds risk to the working degradation path).
- users/info, users/search, users/list, channels/membership,
  users/counts team scoping (no evidence of breakage).
- Persisting `context_team_id` into the cache (nothing downstream
  needs it yet; adding a column for a hypothetical reader is the
  shape-guessing failure mode).

## Testing

- Partition logic: unit tests over `revalidateChannels`' grouping —
  mixed teams produce one call per team with the right ids; empty
  context team lands in the default group; non-Grid fixtures produce
  exactly one call identical to today's.
- edge client: `ChannelsInfo` hits `/cache/<given team>/`, and the
  existing contract tests keep passing with the default team.
- Regression: the 25-second local session tally for
  `edge:channels/info` must be unchanged.
