# Clickable channel mentions - design

**Date:** 2026-11-24
**Status:** implemented

## Why

Slack renders `<#C123|name>` as a `#name` link: clicking it opens the
conversation, asking to join when the user is not a member. slk rendered it
as styled plain text, so a message like "open a request in
#help-cs-rhel-pipeline" offered no way to get there without finding the
channel by hand.

## Design

The mention is rendered as an OSC 8 hyperlink carrying an **internal**
address:

```
slack://channel?id=<C123>&name=<url-encoded label>
```

The mention regex already captures both the channel ID and its label
(`channelMentionRe`); the label is wrapped with the renderer's existing
`osc8Hyperlink` helper, so the link-hit cache (`ScanOsc8Spans` /
`HitsFromSpans`) makes it clickable with no new hit-testing.

The App owns the `slack://` scheme. The `OpenLinkMsg` arm checks for it
*before* the external-link confirmation and routes it to
`(*App).openInternalLink`. The scheme is recognized by prefix before the
URL is parsed: a malformed `slack:` string is refused with a toast, so it
cannot fall through to the external-link confirmation and the OS launcher.

- `slack://channel`: the finder's item for the channel supplies both its
  name and its membership. A joined channel opens directly
  (`openChannel`, `Joined: true`); one the user has not joined (including a
  browseable channel the local store knows) is joined with the label as its
  name (`Joined: false`), which is how `/join` and the channel finder already
  work. A join Slack refuses surfaces as a "Could not join #name: reason"
  toast, so a click never ends in silence.
- Any other `slack://` target is refused with a toast. Internal links never
  reach the OS launcher, so a message cannot use the scheme to launch
  something on the user's machine.

`ExtractLinks` includes the same internal URL for a mention, so the `o`
picker offers it when a mention wraps past the clickable span or is the
message's only link; `linkOpensInApp` reports `slack://` as in-app so the
picker does not label it external.

The address carries no team ID: the pane showing the message belongs to the
active workspace, so its channel store is the one that can resolve it.

Plain-text mirrors strip the OSC 8 escape as they already do for other
links, so copied text stays `#name`.

## Limitations

- A mention of a channel in another Grid workspace cannot be resolved from
  the active workspace's store; the join fails with a toast. Resolving
  through the same-user org session is a follow-up.
- A private channel the user is not in cannot be joined; Slack refuses and
  the toast says so.
- Slack's `slack://app?...` links that appear as text in app Home content
  are still rendered literally (they are not in the mrkdwn link scheme);
  routing them to the app DM is a separate follow-up.
