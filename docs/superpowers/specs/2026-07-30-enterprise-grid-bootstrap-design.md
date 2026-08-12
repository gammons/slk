# Enterprise Grid Bootstrap Parity Design

## Problem

Enterprise Grid orgs sign slk users out and email them a security notice
naming *"data scraping, excessive file downloads, connecting to Slack from a
Tor exit node, or using a third-party client."* This has survived three prior
mitigation attempts (issues #5, #111): browser-like headers (#22), network
token minting (#106), and `localConfig_v2` token reads (#115). In #111 a
tester was signed out **twice** while using the desktop app's own token and
cookie — proving the detection is not credential-related.

Separately, on busy workspaces slk saturates the user's network and the UI
crawls.

Both problems trace to what slk *does* on the wire, not how it authenticates.

## Evidence

**Eight** HAR captures of the official Slack web client on a busy workspace
(`rands-leadership.slack.com`, 10k+ users), taken 2026-07-30.

The **API requests** column is the committed, reconcilable figure: it comes
from `internal/slackhttp/testdata/capture-evidence.json`, counts requests to
`*.slack.com/api/*` **and** `edgeapi.slack.com`, and sums to the 279 every
other claim in this document is measured against. The remaining columns come
from the original manual pass over the HARs and are **not** in the digest —
the raw captures are not in the repo, so nothing can re-derive them. Treat
them as indicative.

| Capture | API requests | of which workspace API (original pass) | Total requests | Assets | Max concurrent |
|---|---|---|---|---|---|
| `initial-load` (warm cache) | 77 | 53 (+24 edgeapi) | 521 | 337 CDN / 68 MB | 80 |
| `coldboot` (fresh profile) | 70 | 49 (+20 edgeapi) † | 746 | 469 bundles / 60 MB | 55 |
| `channel-switch` | 24 | 12 | 84 | 5 images | 39 |
| `channel-switch-2` | 19 | 9 | 146 | 57 images / 3.8 MB | 43 |
| `scroll` (1 page) | 12 | 5 | 246 | 124 images / 12 MB | 56 |
| `quickswitch` (finder) | 15 | — | — | — | — |
| `quickswitch2` (finder) | 37 | 7 (+12 edgeapi) † | 62 | 4 emoji | — |
| `reconnect` (90 s offline) | 25 | **3** (+12 edgeapi) † | 109 | 62 avatars | — |
| **Total** | **279** | **163 workspace + 116 edgeapi** | | | |

† The original pass's workspace/edgeapi split does not add up to the digest
figure for these three rows (69 vs 70, 19 vs 37, 15 vs 25), and the third
column does not sum to the total beneath it. The digest is authoritative; the
split is not. The total row is digest-derived too, not a column sum: 163 is
`x_id_total` (workspace-API only — `_x_id` is never sent to edgeapi), and 116
is the remainder of the 279. What the split is reliably good for is the
*shape* of the finding — a handful of workspace-API calls per interaction,
never an enumeration — and that survives either reading. Anything in Layer 2
that depends on an exact per-capture split needs a fresh measurement.

`quickswitch` was missing from this table entirely in an earlier revision,
which is where the "seven captures" figure came from. There are eight, and
they carry 279 API requests between them.

Four findings reframe the problem:

1. **The official client is not conservative about assets.** 68 MB and 80
   concurrent connections at boot. slk fetches thumbnails only, capped at 4
   (`internal/image/fetcher.go:117`). On assets slk is already an order of
   magnitude politer than the real client. *Asset volume is not the detection
   trigger.*

2. **The official client never enumerates.** Zero `users.list` and zero
   `conversations.list` across all eight captures, warm *or* cold. It
   maintains a local cache and revalidates it conditionally. A cold boot on a
   fresh browser profile issues 49 API calls — four *fewer* than a warm boot,
   because cold and warm follow the same path.

3. **The official client does no HTTP catch-up on reconnect.** After 90
   seconds fully offline, it issued **zero** recovery calls — no
   `client.counts`, no history sweep. Missed state arrives over the
   WebSocket. The only three `conversations.history` calls in the capture are
   the ordinary channel-open triple for the one channel the user then
   clicked.

4. **slk enumerates on every start.** `users.list` (~50 pages),
   `conversations.list` (all public channels), then
   `conversations.history` for *every channel ever visited* plus
   `conversations.replies` for every thread found. That is a textbook scraper
   signature, and it is the one thing the official client provably never
   emits.

The captures live outside the repo (they contain live `xoxc` tokens and
message content). Sanitized request/response pairs are extracted into
`testdata/` fixtures — see Testing Strategy.

## Key Insight

slk's problem is not authentication and not bandwidth. It is that slk's
traffic is **separable** from official-client traffic at three independent
layers, and the middle layer reads as data scraping. Prior fixes addressed
credentials; this design addresses request shape, call pattern, and fetch
scope.

## Scope

Three layers, phased. Layer 1 is independently shippable and may alone
resolve the sign-outs. Layer 3 is a performance fix with no detection
payoff, included because it shares the same code paths.

---

## Layer 1 — Request Envelope Parity

### Divergence

| | Official | slk |
|---|---|---|
| Query params | `_x_id`, `_x_csid`, `_x_version_ts`, `_x_app_name=client`, `_x_frontend_build_type=current`, `_x_desktop_ia=4`, `_x_gantry=true`, `slack_route`, `fp=6e`, `_x_num_retries=0`, `_x_b3_traceid`, `_x_b3_spanid`, `_x_b3_sampled=1` | none |
| POST body extras | `_x_sonic=true`, `_x_app_name=client`, `_x_reason=<ui-trigger>`, `_x_mode=online` | none |
| Content-Type (`/api/`) | `multipart/form-data` | `application/x-www-form-urlencoded` |
| `sec-ch-ua`, `sec-ch-ua-mobile`, `sec-ch-ua-platform` | always present | **absent** |
| `cache-control`, `pragma`, `priority` | `no-cache`, `no-cache`, `u=1, i` | absent |
| `referer` | **absent** on 279/279 API requests, all 8 captures | **sent** (`transport.go:45`) |
| User-Agent | `Chrome/150.0.0.0` | `Chrome/120.0.0.0` (`transport.go:94`) |

The "Official" column above is the union across host classes, not what any
single request carries: the workspace API and `edgeapi` take **different**
envelopes, and `_x_app_name` is an `edgeapi` query param but a workspace-API
*body* field. See the per-host breakdown under *Approach* below.

Two of these are self-defeating: a Chrome/120 UA in 2026 is 30 major versions
stale, and a Chrome UA with no `sec-ch-ua` client hints is a combination real
Chrome never produces. Adding a `referer` the real client omits makes slk
separable with a single log predicate, before any behavioral analysis.

### Verified impersonation values

Captured 2026-07-30 from the Slack web client on Linux/Chrome 150. Both the
`sec-ch-ua` and the User-Agent value were byte-identical on **all 279 API
requests across all eight captures** — a single distinct value each, per
`capture-evidence.json`'s `sec_ch_ua_values` and `user_agent_values`.

(An earlier revision cited 1032 and 1516. Those counted header occurrences
over *every* request including assets and bundles, across a five-capture
subset, and named neither the population nor the subset. The digest does not
record that population, so the figures are not reproducible from anything in
the repo; the 279 above is.)

```
user-agent: Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36
sec-ch-ua: "Not;A=Brand";v="8", "Chromium";v="150", "Google Chrome";v="150"
sec-ch-ua-mobile: ?0
sec-ch-ua-platform: "Linux"
```

**Chrome permutes both the GREASE brand token and the brand ordering between
major versions** — an earlier capture in this repo shows Chrome 147 sending
`"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`, a
different token *and* a different order. Bumping the impersonated version
therefore requires a fresh capture, not just incrementing a number. A
plausible-but-wrong `sec-ch-ua` is worse than none: it is a stable,
slk-specific signature no real Chrome emits.

**The WebSocket upgrade uses a different, smaller header set.** Verified
against the status-101 upgrade in `initial-load.har` and `coldboot.har`:

```
user-agent: Mozilla/5.0 (X11; Linux x86_64) ... Chrome/150.0.0.0 Safari/537.36
accept-language: en-US,en;q=0.9
cache-control: no-cache
pragma: no-cache
origin: https://app.slack.com
```

Chrome sends **no** `Accept`, **no** `Sec-Fetch-*`, **no** `sec-ch-ua*`, and
**no** `Priority` on a WS handshake. slk previously sent
`Sec-Fetch-Dest: websocket` believing browsers do — they do not. A long-lived
socket carrying headers no real Chrome emits is a stable slk-specific
signature, so `WebSocketHeaders()` is deliberately separate from
`browserHeaderPairs()`, the unexported XHR set inside
`internal/slackhttp`. (`BrowserHeaders()` was its exported predecessor and
no longer exists — `RoundTrip` is the only consumer, so there was nothing
for an exported accessor to serve.)

### Approach

Inject at the transport, not the call sites. slk issues API calls two ways —
slack-go (`internal/slack/client.go:101`) and hand-rolled `postForm`
(`client.go:1320`). `BrowserTransport.RoundTrip`
(`internal/slackhttp/transport.go:29`) already sits beneath both, so it is the
single chokepoint. Patching call sites would mean touching ~50 methods and
forking slack-go's URL construction.

**Changes to `internal/slackhttp`:**

1. **Headers.** Add `sec-ch-ua`, `sec-ch-ua-mobile`, `sec-ch-ua-platform`,
   `cache-control: no-cache`, `pragma: no-cache`, `priority: u=1, i`. Remove
   the `Referer` set at `transport.go:45`. Bump the UA to Chrome/150 and
   derive the client-hint version from the same constant so they cannot
   drift.

2. **Envelope params** — all **query** params, including the `_x_b3_*` trio
   (the captures show them in the query string on `conversations.history`,
   not as headers).

   **The two host classes take different envelopes.** An earlier draft of
   this section listed one set for both; that is wrong, and sending the
   workspace set to `edgeapi` would itself be an slk-specific signature.
   Measured, not assumed:

   | | Workspace API (`*.slack.com/api/*`) | edgeapi (`edgeapi.slack.com/*`) |
   |---|---|---|
   | Requests measured | 163 | 116 |
   | `_x_app_name` | — (body only) | `client` |
   | `_x_id` | yes | **never** (0/116) |
   | `_x_csid` | post-boot | **never** |
   | `slack_route` | post-boot | **never** |
   | `_x_version_ts` | yes | **never** |
   | `_x_foreground` | yes | **never** |
   | `_x_frontend_build_type` / `_x_desktop_ia` / `_x_gantry` | `current` / `4` / `true` | **never** |
   | `_x_b3_traceid` / `_x_b3_spanid` / `_x_b3_sampled` | post-boot | yes |
   | `fp` / `_x_num_retries` | `6e` / `0` | `6e` / `0` |

   **Param order is part of the contract, not an implementation detail.**
   0 of the 163 workspace-API requests carried alphabetically sorted params.
   The client emits one canonical sequence, with optional members omitted
   *in place* and `fp` / `_x_num_retries` always last. `url.Values.Encode()`
   sorts keys, so using it would give every slk request a perfectly
   alphabetized query string — a stable distributional signature, which is
   exactly what this layer exists to remove. The transport therefore
   assembles queries and bodies by hand.

   Canonical workspace-API order (13 params, post-boot):

   ```
   _x_id, _x_csid, slack_route, _x_version_ts, _x_foreground,
   _x_frontend_build_type, _x_desktop_ia, _x_gantry,
   _x_b3_traceid, _x_b3_spanid, _x_b3_sampled, fp, _x_num_retries
   ```

   Canonical edgeapi order (6 params, 116/116 matching):

   ```
   _x_app_name, _x_b3_traceid, _x_b3_spanid, _x_b3_sampled, fp, _x_num_retries
   ```

   Both orders, and the pre-boot subset, are pinned in
   `internal/slackhttp/testdata/official-request-shape.json` and asserted
   against live `BrowserTransport` output by `golden_test.go`.

   **Identity has two phases**, and replicating the transition matters:
   before the team id is known, `_x_id` uses the literal prefix `noversion-`
   and *neither* `slack_route` nor `_x_csid` nor the `_x_b3_*` trio is sent;
   afterwards `_x_id` uses an 8-hex client id and all of them appear. Verified
   in `initial-load.har`: `experiments.getByUser` at t+3.0 s has
   `_x_id=noversion-…` with no `slack_route`; `sfdc.integration.listOrgs` at
   t+4.6 s has `_x_id=741e4b14-…&slack_route=T04T4TH8W`. Note `_x_id`'s prefix
   and `_x_csid` are *different* values (`741e4b14` vs `U4129EELrMo`).

   `_x_id` is **not** unique per request. In `initial-load.har`, 53 requests
   produced 52 distinct values — `741e4b14-1785407067.503` appears twice, sent
   68 ms apart. The client timestamps at call-composition time with no
   uniqueness clamp, so slk must not add one: an always-unique sequence where
   Chrome shows occasional collisions is itself a distributional signal.

3. **Body extras:** `_x_sonic=true`, `_x_app_name=client`, `_x_mode=online`,
   and `_x_reason`. `_x_reason` encodes caller intent, so it rides a context
   value — `slackhttp.WithReason(ctx, "message-pane/requestHistory")` — that
   the transport reads. Endpoints without an explicit reason get a
   per-endpoint default.

   Verified body-only: `_x_reason` occurs 153 times as a form field across the
   captures and **never** in a query string (48 distinct values observed).

   **The body envelope is workspace-API-only.** edgeapi bodies are JSON sent
   as `content-type: text/plain;charset=UTF-8` and carry **zero** `_x_*`
   fields — 116/116. The transport must pass them through byte-for-byte;
   injecting form fields would both corrupt the JSON and produce a body
   shape no real client emits.

   **Body field order is a contract too.** The captured trailing sequence is
   `_x_reason, _x_mode, _x_sonic, _x_app_name` on 149/163 requests, with
   business params (`token`, `channel`, …) first. `url.Values.Encode()`
   would sort alphabetically, putting `_x_app_name` first and `token` last —
   an order no real client produces.

### `_x_version_ts` sourcing

`_x_version_ts` is a real Slack build timestamp (`1785403052` and `1785403654`
observed in two captures hours apart, so it moves). A hardcoded value that
never changes is itself an anomaly signal.

**Source it from `client.shouldReload`**, whose response carries
`recommended_build_version` — exactly the value in use as `_x_version_ts`:

```
POST /api/client.shouldReload   → 292 bytes
{"ok":true,"should_reload":false,"recommended_build_version":1785403654, …}
```

An earlier draft of this spec said to scrape the workspace page. That is
wrong and would regress a shipped fix: commit `da6a7e1` deliberately removed
slk's workspace-page fetch, and #111 showed corporate proxies reject that
navigation with 403. `client.shouldReload` is a plain `/api/` POST with no
navigation surface, and it appears in **both** boot captures — so calling it
is also *more* faithful, not less.

slk seeds from the per-workspace cached value, refreshes in the background
after connect, and persists the result. A failed lookup leaves the previous
value intact.

### Deliberately deferred

**Multipart bodies.** The real client posts `multipart/form-data`; slk posts
`x-www-form-urlencoded`. Re-encoding at the transport means parsing and
rebuilding every body across ~50 endpoints — real regression risk for a
signal weaker than the header and param gaps. Recorded as a known residual
difference, revisited after Layers 1–3 land.

---

## Layer 2 — Bootstrap Rewrite

Replace *enumeration* with *conditional revalidation*, and *fan-out* with
*lazy per-view fetch*.

### Boot sequence: 6 calls instead of ~400

Five phases below plus the retained `auth.test` (see Phase A), for six calls
in steady state.

| Phase | Call | Replaces |
|---|---|---|
| A | `client.userBoot` | `users.conversations`, `users.prefs.get`, `stars.list`, `usergroups.list`, `dnd.info` — 5 → 1 |
| B | `client.counts` | unchanged; already the unread source of truth |
| C | `conversations.view` | initial `conversations.history` + per-author `users.info` fan-out + `emoji.list` |
| D | `edgeapi/cache/{team}/channels/info` + `users/info` | `conversations.list` + `users.list` |
| E | — | **deleted:** startup backfill, `conversations.list`, boot-time `subscriptions.thread.getView` |

**Phase A — `client.userBoot`.** POST with `_x_reason=initial-data`,
`version_all_channels=false`, `return_all_relevant_mpdms=true`,
`omit_extras=feature_usage_data,plan_info,salesforce_features`. Response
carries `self`, `team`, `channels` (joined), `ims`, `is_open`, `prefs` (702
keys, including muted channels), `starred`, `subteams` (usergroups), `dnd`,
`channels_priority`, `read_only_channels`, `emoji_cache_ts`, `workspaces`.

`auth.test` is **retained** for Grid API host discovery
(`client.go:172`). The captures are from a non-Grid workspace, so whether
`userBoot` covers the `*.enterprise.slack.com` redirect seen in #111's
diagnostic is unverified. It is one low-signal call, and removing it risks
breaking exactly the accounts this design targets.

**Phase C — `conversations.view`.** Returns `history.messages` (`count=28`),
`users`, `bots`, `channels`, and `emojis` in one response.

**Phase D — conditional revalidation.** The mechanism that replaces
enumeration:

```
POST edgeapi.slack.com/cache/{team}/channels/info
{"check_membership":true,"updated_ids":{"CL0AET1L0":1783337533019, …}}
→ 290 bytes, results=0            (nothing changed)

{"updated_ids":{"C6M7U8DFF":0,"C092E63RUUC":0}}
→ 14.8 KB, results=2              (unknown IDs, fully hydrated)
```

Hundreds of channel IDs with version stamps per request; a sub-KB response
when nothing changed. Identical pattern for `users/info` with
`{userID: mtime}`, batched 30–34 IDs per call as observed. Unknown rows send
`:0`.

Steady-state boot: two sub-KB requests replace ~55 paginated enumeration
calls.

**The cold path is the same path — verified.** On a fresh browser profile
with no IndexedDB, `client.userBoot` still returns `version_all_channels=false`
and 55 channels, each carrying an `updated` version stamp. The client seeds
its cache from that response, then revalidates in batches: one
`channels/info` with 63 version-stamped IDs, plus small `:0` batches for IDs
it has never seen (observed: one `users/info` with 14 IDs, all zero). Six
`channels/info` and four `users/info` calls total, no enumeration anywhere.

This matters because `--add-workspace` is slk's cold path and the operation
that got testers signed out in #5 and #111. The design does not need a
separate cold-start strategy.

### Deletions

- **`triggerBackfill` is removed from *every* code path — not just boot.**
  This is the single largest fingerprint change. It has two call sites: from
  `OnConnect` (`main.go:3705`), which runs on first connect *and* on every
  reconnect, and from the wake-from-sleep detector (`main.go:1812`). Both go,
  along with the method itself (`main.go:3735`).
  The `BackfillCandidates` fan-out (`reconnect_backfill.go:142`) stops
  existing as a runtime behavior; the reconnect path is replaced by the
  bounded handler in *Reconnect behavior* below.

  The existing comment (`main.go:3707`) rationalizes the boot case as
  *"harmless — most `GetHistorySince` calls return zero messages quickly."*
  True for bytes, false for request count, and request count is what anomaly
  detection scores. Unread state comes from `client.counts`; stale scrollback
  is validated lazily on channel open via `cached_latest_updates`.
- **`conversations.list`** (`main.go:2177`) — replaced by
  `channels/search` on demand.
- **Boot-time `subscriptions.thread.getView`** — deferred to first open of
  the Threads view.

### Reconnect behavior

**Observed:** after 90 seconds fully offline, the official client issued
**zero** HTTP catch-up calls. No `client.counts`, no history sweep, no cache
revalidation. It resumes over the WebSocket alone.

slk currently runs its heaviest fan-out on exactly this path —
`triggerBackfill` fires from `OnConnect` (`main.go:3705`), so every laptop
sleep, wifi change, and VPN flap replays the full `BackfillCandidates` sweep.
That is the scraper signature several times a day, not once at boot.

**Design:** on WS reconnect, refresh `client.counts` (one call) plus the
**active channel only**, via the normal three-call open path with
`cached_latest_updates`. Mark all other channels stale for lazy revalidation
on next open.

This is deliberately *more* than the official client does, because slk cannot
yet prove it receives the same WebSocket replay. slk's socket URL carries
`sync_desync=1`, `ms_latest=true`, `flannel=3`, and `lazy_channels=1`
(`client.go:261`) — substantially the same parameters as the official client
— which strongly suggests it gets the same missed-event delivery, but that is
inference, not measurement.

The distinction that matters is **O(1) versus O(channels)**. A fixed two-to-
four calls does not read as enumeration at any workspace size; a sweep over
every channel ever visited does. Dropping to the official client's literal
zero is a follow-up gated on a measurement, not a guess.

**Verification task (cheap, local):** run slk with `SLK_DEBUG=1`, drop the
network for 90 s, restore it, and check whether messages sent during the
outage arrive over the socket without any HTTP fetch. If they do, the
`client.counts` call can be dropped too and slk matches the official client
exactly.

### Incremental sync as the core primitive

```
conversations.history: limit=28, inclusive=true, ignore_replies=true,
  no_user_profile=true, include_pin_count=true, include_stories=true,
  include_free_team_extra_messages=true, include_date_joined=<bool>,
  latest=<ts> | oldest=<ts>,
  cached_latest_updates={"<ts>":"<version>", …}
→ {messages:[…], unchanged_messages:[…], latest_updates:{ts:version}}
```

Observed working in `scroll.har`: client sent one cached `{ts: version}`,
server returned `unchanged_messages=1, messages=27`. slk can validate cached
scrollback without re-downloading it.

**Channel open = 3 calls**, matching the observed official pattern:
`latest=<anchor>` (older direction), `oldest=<anchor>` (newer direction), and
one tagged `_x_reason=unread-counts/onLastReadUpdated` — a bidirectional
window around last-read, not a blind latest-N.

`limit=28` everywhere. slk currently uses 50 on open and **200–500** in
backfill (`main.go:3756`); 500-message pages are a shape the official client
never emits.

### Schema additions

Revalidation requires version stamps slk does not store:

- `channels.version` — millisecond int (`1783337533019`)
- `users.version` — second int (`1612802061`)
- `messages.version` — from `latest_updates`

Rows without a version send `:0` and are fully hydrated, exactly as the
official client does for IDs it has never seen.

### Channel finder

```
POST edgeapi.slack.com/cache/{team}/channels/search
{"query":"test","count":30,"fuzz":1,"include_record_channels":true,
 "top_channels":[…frecent IDs…],"check_membership":true}
→ {results:[30 channels], member_channels:[…]}
```

Debounced ~300 ms on input, not per keystroke — the capture shows two
requests for a four-second typing session. `search.precache`
(`_x_reason=search-precache-onFocus-omniswitcher`) fires on focus.
`top_channels` / `top_users` are fed from `internal/cache/frecent.go`. Local
cache is matched first for instant feedback; server results merge on arrival.

Member lists are channel-scoped, never workspace-wide:

```
POST edgeapi.slack.com/cache/{team}/users/list
{"channels":["C06FR0Q00"],"present_first":true,
 "filter":"everyone AND NOT bots AND NOT apps","count":30}
```

### Module boundaries

`cmd/slk/main.go` is 4323 lines and `connectWorkspace` (`main.go:1877`) is a
large share of it. Rather than growing it:

- **`internal/slack/edge`** (new) — edgeapi client: `channels/info`,
  `users/info`, `channels/search`, `users/search`, `users/list`,
  `channels/membership`. A distinct protocol (JSON body,
  `content-type: text/plain;charset=UTF-8`, different host, `updated_ids`
  conditional semantics) that deserves its own package rather than more
  surface on `Client`.
- **`internal/slack/boot`** (new) — `client.userBoot` and
  `conversations.view` parsing into one `BootstrapResult`. `connectWorkspace`
  reduces to orchestration.
- **`internal/cache`** — version columns plus `ChannelVersions()`,
  `UserVersions()`, `MessageVersions()` helpers.
- **`internal/slackhttp`** — as specified in Layer 1.

### Unverified assumption

`conversations.view` carried **no `channel` param** in the capture; it
returned the last-viewed conversation. slk must open a *specific* channel
(restored last channel or config default). A `channel` param likely works but
is unverified. Fallback, fully verified: `conversations.history` with
`limit=28` plus `cached_latest_updates`. Implementation probes the param
once and falls back on error.

---

## Layer 3 — Viewport-Scoped Asset Fetch

### Root cause

Fetches are spawned inside the render path. `buildCache` loops every buffered
message (`internal/ui/messages/model.go:1747`) → `renderMessageEntry` →
`m.avatarFn(msg.UserID)` (`model.go:1607`) and `RenderBlock`
(`internal/ui/imgrender/imgrender.go:348`), each spawning a goroutine on
cache miss. Render scope and fetch scope are the same thing, so "render the
buffer" means "fetch the buffer."

### Enabler

Full-buffer render is required — `recomputeEntryOffsets` (`model.go:1679`)
needs every entry's height for scroll geometry. But `buildPlaceholder` already
returns the correct height (`target.Y`) without image bytes. Heights are known
without fetching, so render scope and fetch scope decouple cleanly.

### Design

1. **`buildCache` renders with fetching disabled.** Placeholders everywhere,
   correct heights, zero network. `RenderBlock` takes an explicit fetch gate
   rather than deciding for itself.
2. **A visibility pass requests assets.** After `yOffset` changes (scroll,
   selection move, resize, new message), compute the visible entry range from
   `entryOffsets` and request fetches for entries intersecting
   `[yOffset − margin, yOffset + height + margin]`, where margin is one
   screen.
3. **Per-entry invalidation on arrival.** `ImageReadyMsg` /
   `BlockImageReadyMsg` already re-render a single entry by index — the
   documented purpose of `renderMessageEntry` (`imgrender.go:296`). No new
   machinery.

Opening a channel then fetches roughly one screen of assets rather than 50
messages' worth — matching the official client, which pulled 1 thumb on one
channel switch and 12 on another.

### Concurrency

The limit of 4 (`fetcher.go:117`) was tuned while fetching whole buffers. With
a bounded window the burst is far smaller, and observed official concurrency
is much higher:

| Asset class | Official observed max | slk now | Proposed |
|---|---|---|---|
| `files.slack.com` thumbs | 16 (`scroll.har`) | 4 (shared) | 12 |
| `emoji.slack-edge.com` | 71 (`initial-load.har`) | 4 (shared) | 24 |
| avatars (`ca.slack-edge.com`) | few, unbounded | 4 (shared) | 24 (small pool) |

**Split the single semaphore into two pools:** small assets (avatars, emoji —
KB-scale, high count) and large assets (file thumbs, unfurls — up to 1.3 MB
observed). Today one 1.3 MB thumb blocks four avatar fetches behind it; that
head-of-line blocking is the observed UI crawl. Two pools remove it without a
full priority queue.

The large pool is held at 12 rather than the observed 16; slk's existing
429-retry telemetry (`fetcher.go:124`) is the signal for whether that is
right. Both values become config knobs with these as defaults.

### Unchanged

`image_protocol=off` and `emoji_images=off` remain full escape hatches. The
200 MB disk LRU, singleflight dedup, and per-panel failure sets are sound and
stay as-is.

---

## Testing Strategy

**Golden fixtures from the captures.** Sanitized request/response pairs are
extracted from the eight HARs into `testdata/`. Parsers are tested against real
payloads, and a shape-diff test asserts slk's generated requests match the
captured ones field-for-field. The captures become a permanent regression
harness rather than a one-time analysis.

**Layer 1.** Table tests over `RoundTrip`: exact header set for an `/api/`
host, an `edgeapi` host, and a non-Slack host (which must stay untouched);
`Referer` absent; envelope params present and well-formed; UA major version
equals client-hint major version.

**Layer 2.** Fixture-driven parser tests for `client.userBoot`,
`conversations.view`, `channels/info`, `users/info`, `channels/search`. A test
asserting the boot sequence issues exactly the Phase A–D calls and **no**
`conversations.list`, `users.list`, or per-channel `conversations.history` —
the regression guard for the scraper signature. Round-trip tests for version
columns and `cached_latest_updates` construction.

**Layer 3.** Table tests on the visibility calculation: given `entryOffsets`,
`yOffset`, and viewport height, assert the exact entry set in the fetch window
(boundaries: top, bottom, entry taller than viewport, margin clipped at buffer
edges). A fake fetcher asserts `buildCache` alone issues **zero** fetch
requests — the regression guard for the layer.

## Success Criteria

1. A boot on a busy workspace issues ≤ 10 API calls, with zero
   `users.list`, zero `conversations.list`, and zero per-channel
   `conversations.history` fan-out (verifiable from `SLK_DEBUG=1` logs).
   Cold boot (`--add-workspace`) and warm boot follow the same path and land
   within a few calls of each other, as the official client does.
2. A WS reconnect issues a constant number of calls independent of how many
   channels the user has visited — O(1), not O(channels).
3. Every slk request carries the envelope and headers its **destination**
   calls for, matching what the official client sends to that destination:
   the 13-param workspace envelope plus the XHR header set on
   `*.slack.com/api/*` with no `Referer`; the 6-param edgeapi envelope on
   `edgeapi.slack.com`; no envelope at all and Chrome's image header set —
   which *does* include a `Referer` — on asset fetches; and the smaller
   upgrade set, with no envelope, on the WebSocket.

   Stated as "every request carries the full `_x_*` envelope and sends no
   `Referer`", this criterion was unmeetable by design on three counts, and
   meeting it literally would have made slk *more* separable, not less: a
   uniform envelope is a shape the official client never produces.
4. Opening a channel fetches assets for approximately one screen, not the
   whole buffer.
5. An Enterprise Grid tester completes add-workspace, boot, and channel
   switching without a sign-out or security email.

Criterion 5 is the only one that settles the question, and it requires a
volunteer tester. Criteria 1–4 are verifiable locally and are prerequisites,
not proof.

## Risks

- **Detection may be TLS/JA3-level, not behavioral.** #111 concluded the
  fingerprint is client-side. If Grid keys on TLS cipher ordering (raised by
  `Icantjuddle` in #5), no amount of request-shape parity helps. This design
  removes the behavioral signal, which is necessary but may not be
  sufficient. Layer 1 shipping first gives the cheapest read on this.
- **Undocumented internal endpoints.** `client.userBoot`,
  `conversations.view`, and the `edgeapi` surface are unversioned and can
  change without notice. Mitigation: fixture tests fail loudly, and each
  phase keeps a documented fallback to the current call.
- **Testers bear real cost.** Every validation attempt risks signing a
  volunteer out of their work Slack and forcing a conversation with their
  security team. #111's tester was signed out twice. Ask for validation only
  once all three layers are ready, and never repeatedly from the same person.
- **Raised asset concurrency could increase 429s.** Bounded by the existing
  retry logic and made configurable; the numbers are derived from observed
  official behavior rather than guessed.
