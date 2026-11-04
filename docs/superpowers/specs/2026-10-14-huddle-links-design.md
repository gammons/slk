# Huddle links — design (2026-10-14)

Copy a huddle deep link for a conversation or a person. Joining or
starting huddles stays a non-goal (see `wiki/Tradeoffs-and-Non-Goals.md`);
opening the copied link happens in Slack's own clients.

## What Slack gives us

- **The URL is a pure function of team + channel:**
  `https://app.slack.com/huddle/<TEAM>/<CHANNEL>` — exactly what Slack's
  own "Copy huddle link" produces. `<CHANNEL>` may be `C` (channel), `G`
  (group DM) or `D` (DM); `<TEAM>` is `T…`/`E…`. No API call is needed to
  build it. Opening the link when no huddle is active is what Slack's web
  client itself does (it shows the conversation's huddle panel), so slk
  does not gate the copy on knowing a huddle is live.
- **A user's profile names the channel their huddle is in:**
  `profile.huddle_state_channel_id` (alongside the `huddle_state` and
  `huddle_state_expiration_ts` slk already decodes, and a
  `huddle_state_call_id` this design does not need). Evidence: independent
  captures of browser profile payloads (users.list / users.info / profile
  events) while a huddle is active. slack-go v0.23.0's `UserProfile`
  struct models only `HuddleState`/`HuddleStateExpirationTS` and drops the
  channel id, and Slack has no public huddle-list API.

## Design

- `slackurl.HuddleURL(teamID, channelID)` — the one builder; pure, in
  the Slack-URL package both `cmd/slk` and the UI can reach.
- **users.info is hand-rolled** (`internal/slack.GetUserProfile*`): a
  raw POST through `postForm` (token in the body, browser-shaped
  headers), decoded once into `*slack.User` and once into a small
  struct picking `huddle_state_channel_id`. Returns
  `slackclient.UserProfile` — `*slack.User` with the extra field — so
  every field access on the embedded user is unchanged. Rate limits are
  re-raised as `*slack.RateLimitedError`, the type existing callers
  type-assert.
- `core.UserProfile.HuddleChannelID` carries it to the UI's
  `ProfileService` (the K dialog's fetch is live, so no cache schema
  change is needed and the link never comes from a stale row).
- **UI affordances:**
  - `H` / `:huddle` copies the conversation on screen's link
    (`cmdHuddle`), toast "Copied huddle link"; refuses with "No
    conversation open" when there is none. The channel comes from the
    focused thread pane when it has one (see the review follow-up below),
    from the highlighted Threads/Activity row in those list views, and
    from the active channel otherwise.
  - The K dialog's "🎧 In a huddle" row gains a 📋 (and `c`) when the
    loaded profile named the huddle's channel; a click on the icon does
    what `c` does (`userprofile.LastClickTarget` distinguishes it from
    the email's 📋). The footer advertises the binding.
- The WS event, edge-cache and SQLite paths are deliberately untouched:
  nothing in this scope renders a link from live peer status, and those
  files' field lists are pinned to captures. A live in-huddle capture
  should still pin `huddle_state_channel_id` the way `huddle_state` was
  pinned; until then the dialog simply shows no link when Slack does
  not send the field, which degrades to today's behavior.

## Review follow-ups

- **Thread channel.** The Threads view opens a thread without moving
  `a.activeChannelID`, so `H` uses the focused pane's channel
  (`focusedContentPane`) and falls back to the active channel.
- **Profile freshness.** The dialog's 5-minute profile cache would hide a
  huddle that starts (or moves) inside the TTL. The dialog is now
  stale-while-revalidate: `ProfileService.Cached` is shown at once and
  `Profile` — wired to `profileCache.Fresh`, which always fetches and
  refreshes the cache — runs behind it on every open. A failed refresh
  behind a shown profile is ignored (`Model.SetError`), so a background
  429 never replaces good data with the error line. The cache's original
  read-through `profileCache.Profile` is gone: the wiring is
  `Cached` + `Fresh` only.
- **List views.** The Threads and Activity views also leave
  `a.activeChannelID` pointing at the last-opened channel, so `H` there
  copies the highlighted row's channel (`SelectedSummary` /
  `SelectedItem`); no highlighted row refuses.
- **The `c` offer follows the icon.** The row's 📋 appears only while
  the live status says the person is in a huddle; the copy action
  (`c` and the click) now checks the same conditions
  (`Model.HuddleLinkOffered`), so a cached profile whose huddle has
  ended cannot copy its dead link.
