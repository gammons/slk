# Permalinks on a restricted Enterprise Grid — design

## Problem

On an Enterprise Grid workspace, copying a permalink (`Y` / `C`) failed with `enterprise_is_restricted`, while it works in Slack's own client: it goes through `chat.getPermalink`, and Grid can keep that method from a session token. Slack's client does not ask: it builds the link from the workspace URL it already has. Forwarding (`F`) failed the same way until it moved to `chat.shareMessage`; it still fetches the permalink afterwards, best effort, for the local echo of the forwarded message, which on such a workspace came out empty.

## Design

- `slackurl.Build(teamURL, channelID, ts, threadTS)` makes the link in the form `chat.getPermalink` returns and `slackurl.Parse` reads: `https://<host>/archives/<CHANNEL>/p<ts without the dot>`, plus `?thread_ts=<parent>&cid=<CHANNEL>` for a thread reply. It refuses anything but https on a `*.slack.com` host, so a link is never built for another host.
- `(*slackclient.Client).GetPermalink` still asks Slack first. On `enterprise_is_restricted` alone it builds the link from auth.test's URL (the host every API request on the workspace already goes to); any other error, or no usable URL, fails as before. Asking first keeps every other workspace's links exactly as Slack makes them.
- A reply's link needs its thread, which `chat.getPermalink` worked out for itself. `GetPermalink`, `ForwardMessage` (for the echo's link) and the core `Permalink` / `Forward` service methods take a `threadTS`, filled by `(*App).selectedThreadTS()`: the thread pane's parent when that pane has focus, else the channel message's own `thread_ts`.

## Not in scope

- Other restricted methods. Each needs its own replacement for what the official client does.
- Choosing a host other than auth.test's: on Grid that is the workspace's own URL or the org's `*.enterprise.slack.com`, both of which open the message.
