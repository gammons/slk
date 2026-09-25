# Phase 1 — `cmd/slk/main.go` topical splits

> **Parent:** [`../plans/2026-09-06-architecture-refactor.md`](../plans/2026-09-06-architecture-refactor.md), Phase 1
> **Addresses:** F1 (partially)
> **Baseline for this spec:** `b733cce` (main, 2026-09-22)
> **Status:** design approved, plan not yet written

Move ~3,655 lines of package-scope declarations out of `cmd/slk/main.go` into
sixteen new topical files in the same package, plus one append to an existing
file. Nothing is renamed, re-signatured or semantically changed. Add one test
that keeps `main.go` from regrowing.

---

## Why this differs from the tracking document

The Phase 1 table in the tracking document was written against commit `4184e60`,
when `main.go` was 4,842 lines. It is **5,421 today** — every source range in
that table is off by roughly 580 lines, and the table omits four large
self-contained regions that have since become obvious candidates
(`userResolver`, `connectWorkspace`, the workspace types, the CLI dump
commands).

Two of its exit criteria are also superseded:

- *"~1,900 lines moved"* — predates the growth and the four omitted regions.
  This spec moves ~3,655.
- *"zero test changes"* — this spec adds one new test file and edits comments
  in three existing ones (see §7).

Both are amended in the tracking document in the same commit as this spec.

---

## Goals

1. Every declaration in `cmd/slk` lives in a file named for its topic, so
   "where does this go?" has an obvious answer.
2. `main.go` holds the entrypoint and `run()`, nothing else — enforced
   mechanically, so it cannot regrow.
3. The change is provably pure motion, demonstrated by a mechanical check
   rather than by review.
4. The six in-flight PRs that touch `main.go` have a precise, mechanical
   rebase path.

## Non-goals

- `run()` (1,455 lines), the `wireCallbacks` closure, and `connectWorkspace`'s
  *internals*. All Phase 2.
- The three F2 data races (`activeTeamID`, `workspaces`, the aliased `cfg`).
  They live inside `run()`, which does not move. PR #147 is fixing one of them
  independently and this work must not race it.
- Any signature change, including Phase 2 step 8's history-fetch interface.
- Any code change outside `cmd/slk`. The only external edits are seven
  one-line comment corrections across four files (§7).
- The pre-existing comment rot §7 uncovered. Recorded and filed, not fixed
  here — tracking-document ground rule 2.

---

## Relationship to RFC #236

RFC #236 (`internal/ui/<widget>/` → `internal/bubbles/<widget>/`) prescribes
nothing about `cmd/slk`'s file organization; its target layout covers
`internal/` only. The two efforts are disjoint in *code* touched — the sole
overlap is two one-line comment corrections in `internal/ui`
(`reducer_focus_test.go`, `reducer_workspace.go`; see §7). Four interactions
are worth recording:

1. **Import surface widens.** `cmd/slk` imports 15 `internal/ui` packages
   across 39 non-test sites, including `sidebar` (7) and `messages` (2), both
   of which #236 renames. Spreading `cmd/slk` across more files means #236's
   rename PR touches more `cmd/slk` files. Mechanically a wash — a rename tool
   handles it — but it is a real, if small, cost this spec accepts.
2. **`markread.go` helps #236.** "Marking a conversation read" is the RFC's
   flagship example of a feature that should collapse into a widget, and it
   currently sprawls across `main.go`. Isolating it in a named file makes that
   future change legible rather than hidden inside a large handler file.
3. **One idiom for enforcement, not two.** #236 proposes "an AST-walk test
   modelled on the existing `boundary_test.go`, landing first with skip-lists".
   The regrowth guard in §4 below is the same species of artifact and is
   written in the same idiom deliberately.
4. **Ordering does not matter.** Neither effort blocks the other, and neither
   needs to land first. The two `internal/ui` comment edits are one line each
   in comment text; if #236 work touches those lines first, the resolution is
   trivial either way.

---

## 1. Decomposition

Grouping is **topical**, not positional. This follows the convention
`cmd/slk` already uses: `peer_status.go` holds four `rtmEventHandler` methods
(`OnUserStatusChange`, `OnUserInvalidated`, `OnDNDInvalidated`,
`OnUserDNDChange`) alongside the non-handler code for the same topic. Handler
methods therefore go to the file that owns their subject, not to a single
`rtm_handler.go`.

The alternative — one contiguous cut per file, as the tracking document's table
implies — was rejected because it produces a 1,090-line `rtm_handler.go`. That
file would answer "where does this go?" with "anything WebSocket-shaped", which
is the same non-answer `main.go` gives today, only smaller, and it would make
the regrowth guard in §4 unenforceable in spirit.

Line counts below are declaration bodies including doc comments, measured by
AST at `b733cce`. They exclude the blank line between declarations, so the
per-file figures sum to slightly less than the reduction in `main.go`.

### New files

| File | Declarations | Lines |
|---|---|---|
| `history.go` | `fetchOlderMessages`, `fetchMessagesAround`, `convertAndCacheHistory`, `summarizeMessages`, `summarizeCachedRows`, `enrichPerfStats`, `loadCachedMessages`, `enrichCachedRow`, `loadCachedThreadReplies`, `fetchChannelMessages`, `fetchThreadReplies`, `formatTimestamp` | 609 |
| `connect.go` | `shouldReloadTimeout`, `connectWorkspace`, `(h).OnConnect`, `(h).refreshActiveMembership`, `(h).syncOnReconnect`, `(h).OnDisconnect` | 548 |
| `rtm_handler.go` | `rtmEventHandler`, `discoveryRetryAfter`, `(h).discoverConversation`, `(h).OnMessage`, `(h).OnMessageDeleted`, `(h).OnReactionAdded`, `(h).OnReactionRemoved`, `(h).OnUserTyping` | 465 |
| `user_resolver.go` | `userResolverConcurrency`, `userResolverBatchWindow`, `userBatcher`, `userResolver`, `newUserResolver`, `(r).Request`, `(r).resolveOne`, `(r).flush`, `(r).ResolveNow`, `(r).applyEdgeUser`, `(r).RequestBot`, `bestBotIcon` | 421 |
| `markread.go` | `threadMarker`, `markThreadRead`, `channelMarker`, `messageMentionsSelf`, `countMentionsSince`, `markChannelRead`, `markChannelReadAndNotify`, `markChannelReadAsync`, `(h).OnChannelMarked`, `(h).OnThreadMarked` | 311 |
| `workspace.go` | `UnresolvedDM`, `WorkspaceContext` + 4 methods, `workspaceRouter`, `newWorkspaceRouter` + 5 methods, `mostRecentlyVisitedChannel` | 240 |
| `users.go` | `lookupUserCached`, `resolveUserCached`, `resolveUser`, `resolveDMNames`, `messageAuthor` | 206 |
| `presence.go` | `bootstrapPresenceAndDND`, `subscribeWorkspacePresence`, `workspacePresenceIDs`, `(h).OnPresenceChange`, `(h).OnSelfPresenceChange`, `(h).OnDNDChange` | 146 |
| `dump.go` | `listWorkspaces`, `dumpPrefs`, `dumpSections` | 132 |
| `workspace_search.go` | `searchWorkspaceFunc`, `userIDShapeRe`, `searchResultItems`, `formatSearchTimestamp` | 107 |
| `sections.go` | `sectionsProviderAdapter` + 2 methods, `(h).refreshSectionsForActive`, `(h).OnChannelSectionUpserted`, `(h).OnChannelSectionDeleted`, `(h).OnChannelSectionChannelsUpserted`, `(h).OnChannelSectionChannelsRemoved` | 116 |
| `attachments.go` | `extractAttachments`, `extractBlocks`, `extractLegacyAttachments`, `collectThumbs`, `pickAttachmentURL` | 92 |
| `membership.go` | `(h).OnPrefChange`, `(h).OnMemberJoined`, `(h).OnMemberLeft`, `(h).refreshMutedForActive`, `muteRefreshMsg` | 87 |
| `conversations.go` | `(h).OnConversationOpened`, `(h).addConversation`, `(h).publishConversation` | 77 |
| `usergroups.go` | `usergroupHandles`, `slugifyHandle` | 40 |
| `paths.go` | `xdgConfig`, `xdgData`, `xdgCache` | 21 |

### Append to an existing file

| File | Declaration | Lines |
|---|---|---|
| `thread_subscriptions.go` | `(h).OnThreadSubscriptionChanged` | 37 |

**Total moved: 3,655 lines across 17 files.**

### Grouping decisions worth justifying

The rule is **topic first, caller-plurality as the tiebreak for leaf helpers
with no topic of their own.** Three cases where that rule was applied rather
than assumed:

- **`(h).OnPrefChange` → `membership.go`, not a `prefs.go`.** Its doc comment
  states that the only pref slk reacts to is `muted_channels`, and its body
  routes entirely into `MuteStore` and `refreshMutedForActive`. It is mute
  logic wearing a generic name. A `prefs.go` holding one mute-specific function
  would be a misleading file name.
- **`formatTimestamp` → `history.go`, not `workspace_search.go`.** It is a
  topicless leaf, so callers decide: seven of them — two in `run`, **four in
  the functions moving to `history.go`** (`convertAndCacheHistory`,
  `enrichCachedRow`, `fetchChannelMessages`, `fetchThreadReplies`), one in
  `OnMessage`. None in search. `formatSearchTimestamp`, by contrast, has a
  single caller inside `searchResultItems` and stays with it.
- **`messageAuthor` → `users.go`, though all three callers are in
  `history.go`.** Here topic wins: it maps a Slack message to a display name
  via the same `userNames`/cache path as its `users.go` neighbours, and
  grouping it with them is what makes the user-naming logic findable in one
  place. This is deliberate, not an oversight — recording it so a later reader
  does not "fix" it by caller locality.

### What stays in `main.go`

`version`/`commit`/`date`, `main`, `printHelp`, `newImageHTTPClient`, `run`, and
the (pruned) import block. Roughly **1,650 lines, of which `run` is 1,455.**
Phase 2 then has exactly one target instead of a mixed bag.

---

## 2. Mechanism: copy-then-prune imports

Each commit lifts one topical group of package-scope declarations from
`main.go` into its new file. Because file placement is invisible to the Go
compiler within a package, the moved bytes are **byte-identical**, doc comments
included. Nothing is renamed, re-signatured, or reordered relative to its
peers.

The only non-trivial mechanical step is the import block, and it carries a real
hazard. `main.go` has four aliased imports, two of which shadow packages that
are *also* imported into the same file:

```go
slackclient "github.com/gammons/slk/internal/slack"   // and: "github.com/slack-go/slack"
imgpkg      "github.com/gammons/slk/internal/image"   // and: "image"
emojiwidth  "github.com/gammons/slk/internal/emoji"
versionpkg  "github.com/gammons/slk/internal/version"
```

A tool asked to *synthesize* imports for a new file referencing
`slackclient.Client` cannot be relied on to reproduce that alias; resolving it
to `slack-go/slack` would produce a plausible-looking wrong answer.

**Therefore: never synthesize imports.** Each new file begins with `main.go`'s
import block copied verbatim; unused entries are then removed. `main.go`'s own
block is pruned the same way. Removal is deterministic and safe; synthesis is
not. The import-union check in §3 verifies this held.

---

## 3. Purity proof

A throwaway program, not committed, run against the base commit and the branch
tip. Its output goes in the PR description. It asserts two invariants:

**(a) Declaration multiset identity.** For every non-import package-scope
declaration in `cmd/slk`'s non-test files, emit
`(name, sha256(source bytes from doc-comment start through declaration end))`.
The sorted multiset must be byte-identical before and after. This catches any
accidental edit inside a moved block, including whitespace, and does not care
which file a declaration landed in — which is exactly the property being
claimed.

Receiver-bearing methods are keyed as `(*T).Method` so that same-named methods
on different types do not collide.

**(b) Import union identity.** The set of `(path, alias)` pairs across all
non-test files in `cmd/slk` must be unchanged. Nothing left the package, so
nothing may enter or leave the union. This is what guards the alias hazard in
§2: a synthesized `"github.com/gammons/slk/internal/slack"` with no alias would
appear as a new pair and fail.

Together these make "pure motion" a mechanical claim rather than a reviewer's
judgement. They are the reason a 3,655-line diff is reviewable at all.

---

## 4. Regrowth guard

`cmd/slk/main_scope_test.go` — one new test file, written in the idiom of
`internal/ui/boundary_test.go` (AST walk over source, one explicit list, a
failure message that says what to do next):

```go
// main.go is the entrypoint, not a junk drawer. Every other declaration
// in package main belongs in a file named for its topic.
//
// Adding an entry here is a design decision, not a formality: say why the
// declaration cannot live in a topical file.
var mainGoAllowed = map[string]string{
	"version":            "build stamp, set via -ldflags",
	"commit":             "build stamp, set via -ldflags",
	"date":               "build stamp, set via -ldflags",
	"main":               "process entrypoint",
	"printHelp":          "usage text for main",
	"newImageHTTPClient": "constructed by run; single caller",
	"run":                "composition root — Phase 2 decomposes this",
}
```

The test fails on any package-scope declaration in `main.go` outside that map,
naming the offender and pointing at the topical-file convention.

It is scoped to `main.go` alone rather than imposing a size ceiling on every
file in `cmd/slk`. A ceiling would need re-blessing whenever `run` legitimately
changes size, and it permits new junk as long as it is small. An allow-list
needs no maintenance as sizes drift, and when Phase 2 extracts `run`'s startup
phases the entry is *deleted* rather than a number adjusted.

This file and the three comment-only edits in §7 are the whole of this change's
departure from the tracking document's "zero test changes". No test assertion,
fixture or helper is touched.

---

## 5. Commit sequence

Eighteen commits in one PR — seventeen moves plus the guard. Ordered
leaf-first, so a reviewer builds confidence on small obvious moves before
reaching the large ones:

```
 1. paths.go                 21    10. presence.go               146
 2. thread_subscriptions.go +37    11. users.go                  206
 3. usergroups.go            40    12. workspace.go              240
 4. conversations.go         77    13. markread.go               311
 5. membership.go            87    14. user_resolver.go          421
 6. attachments.go           92    15. rtm_handler.go            465
 7. workspace_search.go     107    16. connect.go                548
 8. sections.go             116    17. history.go                609
 9. dump.go                 132    18. main_scope_test.go       (guard)
```

The guard is last because it cannot pass until every prior move has landed.

Each commit must build and pass `go test ./cmd/slk/` on its own, so the PR is
bisectable.

A move commit touches exactly two files — `main.go` and its destination — which
makes "is this pure motion?" answerable by eye per commit, independently of the
§3 proof covering the whole. Four commits also carry a §7 comment correction and
so touch more:

| Commit | Extra files | Sites |
|---|---|---|
| `users.go` | `internal/bootstrap/revalidate.go`, `revalidate_test.go` | 3 |
| `connect.go` | `cmd/slk/bootstrap_adapters_test.go` | 1 |
| `markread.go` | `internal/ui/reducer_focus_test.go` | 1 |
| `rtm_handler.go` | `internal/ui/reducer_workspace.go` | 1 |

In every case the extra diff is comment text only. Reviewers should expect
exactly that and nothing more.

---

## 6. Rebase recipe for in-flight work

Six open PRs touch `cmd/slk/main.go`. #167 and #168 carry an identical
`main.go` diff and are listed as one row:

| PR | Region touched | Affected by this change? |
|---|---|---|
| #147 data race on `userNames` | `lookupUserCached`, `resolveDMNames`, `messageAuthor`, `OnMessage` | yes → `users.go`, `rtm_handler.go` |
| #167 / #168 sidebar DMs | `resolveUser`, `resolveDMNames`, `connectWorkspace`, `WorkspaceContext` | yes → `users.go`, `connect.go`, `workspace.go` |
| #150 unread dot | `run()`, near `summarizeCachedRows` | partly → `history.go` |
| #109 activity feed | `run()` only | no — should rebase cleanly |
| #227 export to Markdown | `main()`, `printHelp` | no — should rebase cleanly |

The PR description and an appendix to the tracking document publish a generated
**symbol → new file → commit sha** table. The procedure:

1. `git rebase origin/main`.
2. Conflicts in `main.go` present as your hunk against deleted context. Take
   `main.go` from upstream wholesale.
3. Re-apply the original hunk to the declaration in its new home, located via
   the table.
4. Verify with `git diff origin/main...HEAD` — it should show only your
   intended change.

Because no declaration was altered, step 3 applies the hunk verbatim. The table
is the load-bearing artifact: without it, contributors hunt through sixteen new
files.

Note for sequencing: PR #147 fixes one of the three F2 data races inside the
region moving to `users.go`. This work does not attempt that fix and must not
be read as superseding it.

---

## 7. Testing strategy and comment accuracy

### Tests

No new behavioural tests. The change is pure motion, and the existing `cmd/slk`
suite — **31 test files, 7,555 lines**, including `event_handler_test.go`,
`event_handler_marked_test.go`, `reconnect_sync_test.go` and
`user_resolver_test.go` — already exercises the moved code in place. Those files
reference package-level symbols, which do not change package, so **no test
assertion or fixture needs editing**. That no test needs editing is itself part
of the evidence that the motion was pure.

Verification is therefore:

- the §3 purity proof, for the whole change;
- per-commit build + `go test ./cmd/slk/`, for bisectability;
- the full `go test ./... -race` suite, for the tree;
- the §4 guard, for the future.

### Comments this change invalidates

Eight comments pair a moving symbol with the filename `main.go` and become wrong
the moment it moves. They are corrected in the commit that performs the
corresponding move:

| Site | Names | Moves to |
|---|---|---|
| `cmd/slk/bootstrap_adapters_test.go:848-852` | `connectWorkspace` | `connect.go` |
| `internal/bootstrap/revalidate.go:416` | `connectWorkspace` | `connect.go` |
| `internal/ui/reducer_focus_test.go:673` | `OnChannelMarked` | `markread.go` |
| `internal/ui/reducer_workspace.go:86` | `rtmEventHandler` | `rtm_handler.go` |
| `internal/bootstrap/revalidate.go:431` | `resolveUser` | `users.go` |
| `internal/bootstrap/revalidate.go:449` | `resolveUser` | `users.go` |
| `internal/bootstrap/revalidate_test.go:727` | `resolveUser` | `users.go` |
| `internal/bootstrap/revalidate_test.go:757` | `resolveUser` | `users.go` |

> **Correction, made during execution.** This table originally listed six sites.
> Two were missed because the sweep that produced it grepped for the moving
> symbol and the string `main.go` on the *same* line, and both of those
> citations wrap across two comment lines —
> `…resolveUser already uses this exact` / `chain (main.go:2432).` The Task 12
> implementer found the first; re-sweeping over whole comment blocks rather
> than single lines found the second, which this document had previously
> misfiled as pre-existing rot because only its dead line number was noticed,
> not that it also names a moving symbol.
>
> One site that *looks* like a ninth is not: `internal/ui/reducer_channels.go:363`
> says "via `main.go`'s recorder closure", and that closure is inside `run()`,
> which does not move. It stays correct and is left alone.
>
> The lesson generalises beyond this document: **grep line-by-line for a fact
> that spans lines and you will under-count it.** Sweep the block.

**The fix is to make them file-agnostic, not to re-point them.** A comment
saying "`resolveUser` in `cmd/slk`" is correct today, correct after Phase 1, and
correct after Phase 2 moves things again; one saying "in `cmd/slk/users.go`"
merely resets the rot clock. This is not a new convention:
`internal/ui/reducer_workspace.go:78` already reads "cmd/slk's
`rtmEventHandler`" — eight lines above one of the sites being fixed.

Five files carry these eight edits; four of them are outside `cmd/slk`. The exit
criteria in §8 are stated accordingly.

### Pre-existing rot — recorded, not fixed

The sweep that found the six also found that **every line-number citation into
`main.go` in the repository is already wrong**, and that two name a symbol which
does not exist:

| Site | Claim | Reality at `b733cce` | Fate |
|---|---|---|---|
| `internal/ui/msgs.go:76` | `rtmEventHandler.refreshChannel` | no such symbol anywhere | issue |
| `internal/ui/reducer_focus_test.go:885` | `rtmEventHandler.refreshChannel` | no such symbol anywhere | issue |
| `cmd/slk/main.go:4447` | `main.go:2076` | a `context.WithTimeout` call | issue |
| `internal/bootstrap/revalidate.go:416` | `main.go:1941` | a comment about `ThreadsListDirtyMsg` | fixed in §7 above |
| `internal/bootstrap/revalidate.go:431` | `main.go:2432` | a comment about issue #111 | fixed in §7 above |
| `internal/bootstrap/revalidate.go:449` | `main.go:2440` | a bare `return` | fixed in §7 above |
| `internal/bootstrap/revalidate_test.go:727` | `main.go:2432` | a comment about issue #111 | fixed in §7 above |
| `internal/bootstrap/revalidate_test.go:757` | `main.go:2440` | a bare `return` | fixed in §7 above |

The last five appear in both tables because there the filename and the dead
line number are a single token — `(main.go:2432)`. Replacing it with a
file-agnostic reference necessarily retires the line number too. That is a
side effect of the §7 fix, not extra scope.

The remaining three are **filed as one issue, not fixed here**. Repairing the
`refreshChannel` references requires deciding what they *should* say, which is a
judgement about current behaviour rather than code motion — ground rule 2. Note
also that Phase 1 makes `main.go:4447`'s dead citation *differently* dead, since
everything above `run()` moves out and `run()` shifts up by ~660 lines; that is
a wrong pointer becoming a different wrong pointer, not a regression.

The issue should record the general lesson: **a line-number citation into
another file rots silently and is worthless within weeks. Cite the symbol.**

---

## 8. Exit criteria

| Criterion | Bar |
|---|---|
| `cmd/slk/main.go` | ≤ 1,700 lines (from 5,421) |
| Lines moved | ~3,655 across 16 new files + 1 existing |
| Declaration multiset (§3a) | byte-identical before and after |
| Import union incl. aliases (§3b) | identical before and after |
| Test assertions or fixtures changed | **0** |
| Existing `_test.go` files touched | 3, **comment-only** (§7) |
| New test files | exactly 1 (`main_scope_test.go`) |
| Non-comment changes outside `cmd/slk` | **0** |
| Files touched outside `cmd/slk` | 4, comment-only: `internal/ui/reducer_focus_test.go`, `internal/ui/reducer_workspace.go`, `internal/bootstrap/revalidate.go`, `internal/bootstrap/revalidate_test.go` |
| Issue filed for pre-existing comment rot | 1, covering 3 sites (§7) |
| `go test ./... -race` | green |
| `go vet ./...`, `golangci-lint run`, `gofmt -l .` | clean |
| Every commit | builds, `go test ./cmd/slk/` green |
| Rebase table | published in PR description and tracking doc |

## 9. Risks

| Risk | Mitigation |
|---|---|
| Import alias mis-resolution (`slackclient` vs `slack-go/slack`) | §2 copy-then-prune; §3b import-union check would fail |
| A moved block is accidentally edited | §3a byte-level multiset check |
| Six PRs conflict | §6 rebase recipe with generated symbol table; accepted cost, decided deliberately |
| A grouping decision proves wrong later | Moving a declaration between files in one package is a one-commit, zero-risk change. Not worth optimising for now. |
| `main.go` regrows | §4 guard |
| Comments silently go stale | §7 fixes the eight this change invalidates, and fixes them file-agnostically so they do not rot again |
| Merge conflicts *with* #236 | Near-none — disjoint code. Of the four externally-touched files, two are in `internal/ui` (`reducer_focus_test.go`, `reducer_workspace.go`); both edits are one line of comment text, trivially resolvable in either direction. See the RFC section above. |

---

## Appendix: measurement method

Declaration boundaries and line counts come from an AST walk of
`cmd/slk/main.go` at `b733cce` using `go/parser` with `parser.ParseComments`,
taking each declaration's start as its doc-comment position where one exists
and its end as `decl.End()`. Counts are inclusive of both bounds and exclusive
of the blank line separating declarations, which is why the per-file figures
(3,655) sum to less than `main.go`'s expected reduction (~3,770).

PR-to-region mapping comes from `gh pr diff <n>` hunk headers for
`cmd/slk/main.go`, resolved against each PR's own merge base rather than
current `main`.
