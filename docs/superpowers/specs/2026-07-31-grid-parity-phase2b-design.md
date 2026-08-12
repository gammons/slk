# Grid Parity Phase 2b: Bootstrap Rewrite — Design

Layer 2 of
[`2026-07-30-enterprise-grid-bootstrap-design.md`](./2026-07-30-enterprise-grid-bootstrap-design.md),
second half. Phase 2a built the components; 2b rewires `connectWorkspace` onto
them and deletes the enumeration.

This is the phase that changes what slk puts on the wire. Phases 1 and 2a
changed request *shape* and added unused code respectively; **2b is the first
one that can plausibly stop the sign-outs, and the first that can break slk.**

## Read first

1. [Layer 2 of the bootstrap design](./2026-07-30-enterprise-grid-bootstrap-design.md) — the original design.
2. [Phase 2a outcomes](../plans/2026-07-30-grid-parity-phase2a-outcomes.md) — what shipped, and eleven places the captures overruled the plan. Its "What Phase 2b inherits" section is the direct input to this spec.
3. `internal/slack/edge`, `internal/slack/boot`, `internal/cache/versions.go` — the real interfaces this builds on.

## The original spec was right about `conversations.list` — I was wrong

An earlier revision of this document claimed slk never calls
`conversations.list`, on the grounds that its wrapper was dead code. **That was
an error, and it was propagated into the Phase 2b plan and the Phase 2a
outcomes doc before a measurement caught it.**

The mistake: the Layer 2 design cited `conversations.list` at `main.go:2177`,
which had drifted to unrelated code. Searching for a wrapper named
`GetAllChannels` — a name inferred from the design's prose, never verified —
returned nothing, and "no callers" was concluded from "no such symbol". The
real function is **`GetAllPublicChannels`** (`internal/slack/client.go:497`).

Measured on a real two-workspace boot, 2026-08-01:

```
API requests: 180 total across 18 endpoints
    106  conversations.history
     22  users.list
     16  subscriptions.thread.getView
      4  conversations.list          <- live, not dead
      ...
```

A stack dump named the caller outright:

```
internal/slack.(*Client).GetAllPublicChannels   client.go:515
main.fetchBrowseableChannels                    main.go:2238
created by main.run.func11                      main.go:1787
```

`fetchBrowseableChannels` runs in a background goroutine at boot and pages
`conversations.list` at `Limit: 1000` to populate the channel finder with
channels the user has **not** joined. Its own doc comment concedes it is
"significantly slower than GetChannels for large workspaces (potentially
thousands of channels)".

So slk enumerates all three ways the original design said, and this is the one
that most literally matches "fetch every public channel including unjoined":

| Original claim | Measured |
|---|---|
| `users.list`, ~50 pages | **Real** — 22 calls across two workspaces |
| `conversations.list`, all public channels | **Real** — 4 calls, `Limit: 1000` per page |
| per-channel `conversations.history` | **Real** — 106 calls in a 4-second boot |

`edge.ChannelsSearch` is its replacement, so deleting it belongs with the
finder work, not with the cleanup task. See *Deletions*.

## Scope decisions

Settled before design, recorded so they are not relitigated:

- **One plan, one PR.** 2b lands on `feat/grid-parity` alongside Phases 1 and 2a.
- **Users go lazy.** Delete the `users.list` sweep outright and match the official client: seed from cache, revalidate via `edge.UsersInfo`, fill from `conversations.view`'s `users`, resolve misses on demand, and back the mention picker with local-cache-first plus debounced `edge.UsersSearch`.
- **`stars.list` and `usergroups.list` stay**, as documented residual divergences. Neither appears in any capture, but `userBoot`'s `starred` and `subteams.self` were `[]` in 2/2 captures so their element shapes are unknown, and modelling an unobserved shape is the failure mode this project exists to correct. No fresh capture is being taken.
- **`conversations.view`'s `channel` param is probed at runtime**, not assumed, and falls back to `HistoryWithVersions`.

## Architecture

`connectWorkspace` (`cmd/slk/main.go:1889`) currently constructs a live client,
calls `Connect`, and then performs boot inline. Phase 1's outcomes recorded
that this makes it **untestable**: "no injectable seam without either a live
Slack connection or an interface extraction whose only consumer would be the
test."

2b puts the most security-relevant logic in the project inside that function.
So the boot sequence is extracted, and only the boot sequence:

```
internal/bootstrap        (new)
  imports: internal/slack, internal/slack/boot, internal/slack/edge, internal/cache
  imported by: cmd/slk

  func Run(ctx context.Context, deps Deps) (*Result, error)
```

`Deps` carries interfaces for the API calls plus the cache, so `Run` is
testable with fakes. `connectWorkspace` reduces to: construct client →
`Connect` → `bootstrap.Run` → wire `Result` into the UI.

This is the smallest extraction that makes the success criteria assertable. A
full `internal/workspace` extraction was considered and rejected: coupling a
large structural refactor to a behavioural rewrite in one PR makes a regression
impossible to bisect.

Note the import direction: `internal/slack/boot` imports **only stdlib** and
must keep doing so — `internal/slack` will import `boot`, so the reverse would
cycle. Mute parsing therefore stays at the caller via
`slack.ParseMutedFromAllNotificationsPrefs`, reading `boot.Result`'s raw
`all_notifications_prefs` string.

## Boot sequence

| # | Call | Replaces |
|---|---|---|
| 1 | `auth.test` | **Retained.** Grid API host discovery (`client.go:84`, `client.go:95` — `apiBaseURL` and `teamURL` are both derived from `auth.test`'s response). The captures are from a non-Grid workspace, so whether `userBoot` covers the `*.enterprise.slack.com` redirect is unverified, and removing it risks breaking exactly the accounts this targets. |
| 2 | `client.userBoot` | `users.conversations`, `users.prefs.get`, `dnd.info` |
| 3 | `client.counts` | unchanged — already the unread source of truth |
| 4 | `conversations.view` | initial `conversations.history` + per-author `users.info` fan-out + `emoji.list` |
| 5 | `edge.ChannelsInfo` + `edge.UsersInfo` | the `users.list` sweep |

Plus the two retained divergences (`stars.list`, `usergroups.list`).

**Step 4 probes.** `conversations.view` is sent with a `channel` param, which no
captured request carried. The response's `channel.id` is compared to the
requested id; on mismatch or error, the caller falls back to
`HistoryWithVersions` with `limit=28`, which is fully verified. A caller that
ignores `Channel.ID` renders the wrong channel's history with no error
anywhere, so the comparison is mandatory, not advisory.

**Step 5 is scoped, not exhaustive**, and the scope is a rule rather than a
judgement call:

- `edge.ChannelsInfo` sends the ids in `userBoot`'s `channels` + `ims` — the conversations slk will actually render in the sidebar. Not every channel in the cache.
- `edge.UsersInfo` sends: the authors returned by `conversations.view`, the counterparties of open DMs, and nothing else at boot. Users encountered later are resolved on demand.

Anything outside that set is left stale and revalidated lazily when first
needed.

This matters beyond politeness. A fixed batch size over an unbounded id set
produces a long run of identically-sized requests — on a 10k-user workspace at
`usersInfoBatchSize = 80`, 125 consecutive exactly-80 requests — which is a
*cleaner* signature than the official client's ragged, demand-driven
distribution (observed 1–80 across 30 requests). Scoping the id set is the fix.
Jitter is not: inventing a shape with no evidence behind it is the Phase 1
`sec-ch-ua` mistake.

## Deletions

- **`triggerBackfill`** — the method (`main.go:3786`) and both call sites: `OnConnect` (`main.go:3756`, which runs on first connect *and every reconnect*) and the wake-from-sleep detector (`main.go:1823`). This is the single largest fingerprint change in the project.
- **`BackfillCandidates`** as a runtime path (`reconnect_backfill.go:142`).
- **`client.GetUsers`** / the `users.list` sweep (`main.go:2077`).
- **Boot-time `subscriptions.thread.getView`** — deferred to first open of the Threads view.
- **`GetAllPublicChannels`** (`client.go:497`) and its caller `fetchBrowseableChannels` (`main.go:2238`, spawned at `main.go:1787`) — the live `conversations.list` enumeration. Deleted **with** the finder move to `edge.ChannelsSearch`, not before it, or the finder loses unjoined channels with nothing to replace them.

The existing comment at `main.go:3752` rationalises boot-time backfill as
"harmless — most `GetHistorySince` calls return zero messages quickly." True for
bytes, false for request count, and request count is what anomaly detection
scores.

### Reconnect

On WebSocket reconnect: `client.counts` (one call) plus the **active channel
only**, via the normal open path with `cached_latest_updates`. All other
channels are marked stale for lazy revalidation on next open.

This is deliberately more than the official client, which issued **zero** HTTP
calls after 90 seconds fully offline. slk cannot yet prove it receives the same
WebSocket replay, though its socket URL carries substantially the same
parameters (`sync_desync=1`, `ms_latest=true`, `flannel=3`, `lazy_channels=1` —
`client.go:345`).

The distinction that matters is **O(1) versus O(channels)**. A fixed two-to-four
calls does not read as enumeration at any workspace size; a sweep over every
channel ever visited does.

A plan task runs the cheap local check: `SLK_DEBUG=1`, drop the network for
90 s, restore, and see whether messages sent during the outage arrive over the
socket with no HTTP fetch. If they do, `client.counts` can be dropped too and
slk matches the official client exactly. Gated on that measurement, not guessed.

### Finder and mentions

Both become local-cache-first with a debounced server query on top:

- **Channel finder** → `edge.ChannelsSearch`, debounced ~300 ms, frecency list from `internal/cache/frecent.go` as `top_channels`. The capture shows two requests for a four-second typing session — roughly one per input pause, never per keystroke.
- **Mention picker** → existing per-channel membership first, then `edge.UsersSearch` debounced, with `current_channel` set.

Local matches render immediately; server results merge on arrival. A finder
that fired per keystroke would be a worse fingerprint than the enumeration it
replaces.

## Cache column mapping

**This is the part most likely to cause silent data loss, and it is written
down before it is code.**

`UpsertChannel` and `UpsertUser` use `ON CONFLICT DO UPDATE SET` over a fixed
column list. The new sources cover *different subsets* of those columns.
Feeding `edge.UsersInfo` results straight into `UpsertUser` blanks
`avatar_url` for every revalidated user; feeding `edge.ChannelsInfo` into
`UpsertChannel` blanks `is_member` and `is_starred`.

### `channels`

| Column | `boot` userBoot | `boot` view `channels[]` | `edge.ChannelsInfo` | `client.counts` |
|---|---|---|---|---|
| `id` | ✅ | ✅ | ✅ | ✅ |
| `name` | ✅ | ✅ | ✅ | — |
| `type` | ✅ derived | ✅ derived | ✅ derived | — |
| `topic` | ✅ | ✅ | ✅ | — |
| `is_member` | implied (joined) | ✅ 54/54 | ❌ **0/36** — comes from top-level `member_channels` | — |
| `is_starred` | ❌ | ❌ | ❌ | — |
| `last_read_ts` | — | ✅ 14/54 | ❌ | ✅ |
| `unread_count` / `has_unread` | — | ✅ 14/54 | ❌ | ✅ |
| `version` | ✅ `updated` | ✅ `updated` | ✅ `updated` | — |

### `users`

| Column | `boot` view `users[]` | `edge.UsersInfo` | `edge.UsersSearch` |
|---|---|---|---|
| `id`, `name` | ✅ | ✅ | ✅ |
| `display_name` | ✅ | ✅ | ✅ |
| `avatar_url` | ✅ | ⚠️ available (`image_original` 255/291) but **not currently modelled** | ⚠️ same |
| `is_bot` | ✅ | ✅ | ✅ |
| `is_external` | derived from `team_id` | derived | derived |
| `presence` | ❌ | ❌ | ❌ |
| `version` | — | ✅ `updated` | ✅ `updated` |

### Rule

A source that cannot populate a column must **preserve** it, never overwrite.
2b adds narrow partial-update methods — `UpdateChannelFromEdge`,
`UpdateUserFromEdge`, `ApplyMembership` — rather than reusing the full upserts.
Each gets a round-trip test asserting the columns it does *not* own survive
unchanged, mirroring the `SurvivesReUpsert` tests Phase 2a added for `version`.

`edge.User` should gain `ImageOriginal` — the evidence is in hand (255/291) and
it removes the `avatar_url` hazard at its source rather than working around it.

## Verification

Two mechanisms, because nobody is testing this on a real Grid account until all
three phases land and local measurement is the only feedback loop.

**1. Request counter.** A tally at the `BrowserTransport` chokepoint, grouped by
endpoint, dumped under `SLK_DEBUG=1` on shutdown. Turns success criteria 1 and
2 into a single line, gives the PR a hard before/after number, and is reusable
for Phase 3. The transport already sits beneath both slack-go and the
hand-rolled `postForm` path, so there is one place to instrument.

**2. Unit tests on `bootstrap.Run`** with fake dependencies, asserting the
exact call set and — the regression guard — **zero** `users.list`, zero
`conversations.list`, and zero per-channel `conversations.history` fan-out. No
fake Slack server needed; the extraction in *Architecture* is what makes this
possible.

Both `triggerBackfill` call sites get a test asserting a reconnect issues a
constant number of calls independent of how many channels have been visited.

## Success criteria

1. A boot on a busy workspace issues ≤ 10 API calls, with zero `users.list` and
   zero per-channel `conversations.history` fan-out — **measured by the request
   counter**, not eyeballed. Cold boot (`--add-workspace`) and warm boot land
   within a few calls of each other.
2. A WS reconnect issues a constant number of calls independent of channels
   visited — O(1), not O(channels).
3. The mention picker and channel finder remain usable on a warm cache, and
   degrade to debounced server search rather than breaking on a cold one.
4. No regression in messages displayed, unread state, or muted-channel
   behaviour.

Criterion 5 of the original spec — an Enterprise Grid tester completing
add-workspace, boot and channel switching without a sign-out — remains the only
one that settles the question, and remains gated on all three phases landing.
**Nobody should be asked to spend a sign-out before then.**

## Risks

- **Cache column mapping (above) is the most likely source of silent damage.** Avatars, membership and starred state degrade quietly and would surface as UI bugs long after the change. Mitigated by partial-update methods and preserve-column tests, but this is where to look first if something goes wrong.
- **The `conversations.view` `channel` param is unverified**, and unverified specifically on Grid, where no capture exists at all. The probe-and-fallback keeps a broken param from breaking channel open, but the fallback path will be the *common* path if the param is rejected, so it must be as well tested as the primary.
- **Cold-start UX regresses** on `--add-workspace`: the user list arrives lazily, so mention autocomplete is thin until the cache fills. Accepted deliberately; it is what the official client does.
- **Deleting `triggerBackfill` changes what users see after a sleep.** Channels other than the active one are stale until opened. If the WebSocket replay is weaker than believed, this reads as "slk lost my messages" — which is why the 90-second offline check is a plan task rather than a follow-up.
- **`main.go` is 4,320 lines** and `connectWorkspace` is a large share of it. Even the narrow extraction touches code with no existing test coverage.
- **Detection may be TLS/JA3-level.** If Grid keys on cipher ordering rather than behaviour, none of this helps. Unchanged from the original spec, and unfalsifiable until a tester tries it.

## Out of scope

Layer 3 (viewport-scoped asset fetch) is Phase 3. Phase 1's residual divergence
table — multipart bodies, alphabetical business-param order, `_x_b3_*`
over-sending, `_x_foreground`, `Accept-Encoding`, and the `Authorization:
Bearer` header on image fetches — remains open and is not addressed here.
