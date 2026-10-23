# Workspace status presets — design

Issue: https://github.com/gammons/slk/issues/356 (approved)
PR: https://github.com/gammons/slk/pull/360
Author: SieuJS

## Problem

The `Ctrl+S` status menu (`internal/ui/presencemenu`) only set presence
(Active/Away) and DND snooze. Slack's actual custom status (free text +
emoji + expiration, `users.profile.set`) was never exposed from `slk`.

Custom status in Slack is scoped **per workspace** — your profile,
including status, belongs to each workspace membership independently,
same as presence/DND already are in `slk` (see `presenceController` in
`internal/ui/presence.go`).

## What shipped

Two preset rows — **"Working from home"** (🏠) and **"On vacation"**
(🌴) — plus a **"Clear status"** row (shown only once a status is set)
in the `Ctrl+S` menu. Presence (Active/Away) never clears an existing
custom status; they're independent, matching real Slack. The status
bar shows a segment (e.g. `🏠 Working from home`) when a status is
active, and the menu shows a `✓` on the matching row when reopened.

## Architecture: self-status reads from the peer-status store

This is the one decision worth recording, because it wasn't the first
draft — PR review (two independent passes) caught that the initial
approach was wrong.

**First draft:** a second, client-only cache — `workspaceStatus.
StatusText`/`StatusEmoji` in `presenceController`, written only when
`Apply()` ran a WFH/Vacation/Clear action from the menu.

**Why that was wrong**, per review: `slk` already has a complete store
for exactly this data — `presenceController.peers` (`peerstatus.
Status`), used today to show *other people's* custom status in the
sidebar, DM headers, and channel finder. It's seeded at connect/switch
from `cachedPeerStatuses`/`ListUserStatuses` (which has no self-filter
on the `users` cache table), kept live via `UserStatusChangeMsg` (fed
by `user_change`/`user_status_changed`, which also carry no self-filter
— `cmd/slk/peer_status.go`'s `OnUserStatusChange`), and already expired
on a schedule via `peerStatusTickMsg`. The authenticated user's own
status flows through this exact pipeline; the first draft just never
read it back out for `a.currentUserID`.

Keeping a second, write-only-by-`Apply` cache meant:
- The status vanished on restart (nothing re-read it from the peer
  store or re-fetched it from Slack).
- An edit made from the real Slack client was never reflected.
- A failed `users.profile.set` call still displayed as if it had
  succeeded, because the cache was written optimistically and nothing
  ever corrected it (presence self-corrects via the next
  `StatusChangeMsg`; the custom-status cache had no equivalent).
- WFH's end-of-day expiration was never locally honored — no deadline
  field existed on the self-only cache at all.

**Final architecture:** `presenceController.CustomStatus(userID)` reads
`p.peers[userID]` directly (`HasStatus`/`StatusGlyph` on `peerstatus.
Status` already handle expiry and shortcode→glyph). `Apply()` is now a
no-op for `ActionSetWFH`/`ActionSetVacation`/`ActionClearStatus` — they
only forward to `presenceSvc.SetStatus`, which calls the real API
(`Client.SetCustomStatus`, wrapping `slack-go`'s
`SetUserCustomStatusContext`). The status bar and the menu's checkmark
update only once the real `UserStatusChangeMsg` echo arrives (via
`applyPeer`) — not optimistically. This fixes all four problems above
at once: restart persistence, external edits, failed-request display,
and local expiry, all for free, with less code than the first draft
had.

One ordering constraint this implies: whichever code seeds the active
workspace's custom-status display (workspace switch, `InitialActive`
activation) must call `a.presence.SetPeers(...)` (which seeds
`p.peers` for the new workspace) *before* calling `a.statusbar.
SetCustomStatus(a.presence.CustomStatus(a.currentUserID))` — reading
`CustomStatus` first would see the previous workspace's (or no)
peers.

## Decisions

1. **Presets, not a free-text editor.** Free-text/custom-emoji entry
   is out of scope for this pass.
2. **Expiration:** WFH auto-clears server-side at end of day (local
   time, 23:59:59, `cmd/slk/main.go`'s `endOfDayUnix`). Vacation has no
   expiration — cleared manually. Both now also clear/display correctly
   client-side once Slack's event reflects the change, per the
   architecture above.
3. **Presence and custom status are independent**, matching real
   Slack — picking Active/Away never touches the other.
4. **"Clear status"** only appears once a status is actually set.

## Known test-fragility traps (useful if this menu grows further)

- `internal/ui/mode_presence_menu_test.go` hardcodes the unfiltered row
  count in `presenceMenuAllRows` and walks the cursor with a fixed
  number of `j`/down presses in several cases. Both need updating for
  any future row count change.
- The same file's "a printable key filters the menu" case filters by
  the literal rune `"a"` — "On vacation" substring-matches via
  "vacation", so any future row whose label contains "a" changes the
  expected count there too.
- `internal/ui/golden_dialogs_test.go` / `testdata/golden/
  dialog_presence_menu.ansi` needs regenerating with `go test
  ./internal/ui/ -run TestGolden_Dialogs -update` whenever the row
  count changes.

## Out of scope

- Free-text / custom-emoji status entry.
- Syncing status across workspaces — each workspace's status is
  independent, intentionally.

## Review history

Three rounds of automated review (Greptile, plus a bot posting as
@gammons — no human maintainer review yet as of this writing) on
PR #360 found:

- The self-status architecture issue above (fixed by this redesign).
- The optimistic-display-on-failure issue (fixed as a side effect of
  the same redesign).
- Emoji-only statuses (no text) were dropped everywhere — the status
  bar and the menu's Clear-status row both gated visibility on status
  text alone, discarding the emoji. Fixed: visibility now checks
  text-or-emoji.
- Status-bar width overflow for long statuses. The redesign above made
  this reachable: `slk` now shows *any* status set from the real Slack
  client, not just this feature's own two short fixed presets, and
  Slack allows up to 100 characters. Fixed: the custom-status segment
  now degrades (full text → emoji + truncated text + ellipsis → emoji
  alone → dropped) to stay within the status bar's exact-width budget,
  rather than assuming a fixed short length.
- That the `Ctrl+S` menu can now exceed a short terminal's height with
  no scrolling — a pre-existing limitation of the menu, not new to this
  change, just easier to hit. Left as a follow-up, not fixed here.

A follow-up local review (`codex-batch-review`, scoped to just the
emoji-only/truncation fix commit) caught three more real issues in that
fix itself, all fixed with regression tests:

- The truncation budget math double-counted a separator when the
  status segment landed between two other segments (e.g. an unread
  badge before it, the connection indicator after it) — it always
  needs exactly one *more* separator than the other segments' own
  joined width already accounts for, not one per side.
- The truncation loop hand-rolled rune-by-rune width summation instead
  of reusing `muesli/reflow/truncate.StringWithTail` — already the
  established width-aware truncator elsewhere in this codebase
  (`internal/ui/presencemenu`, `internal/ui/thread/breadcrumb.go`,
  and a dozen other files). Rune-by-rune summation can mismeasure
  combined grapheme clusters (variation selectors, ZWJ sequences) in
  status text and let a segment exceed its budget.
- When the budget allowed the emoji but not even one character of
  text, the result was a bare "emoji …" with nothing actually
  truncated — now falls back to emoji alone instead.
