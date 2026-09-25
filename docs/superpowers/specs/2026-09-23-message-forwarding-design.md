# Message forwarding

## UX

In normal mode, `F` forwards the selected channel message or thread reply.
Reuse the Ctrl+T channel finder, titled **Forward message to…**, with its
existing fuzzy matching, recency order, arrow/Ctrl+N/Ctrl+P navigation and
mouse selection. Offer joined channels and existing DMs/group DMs in the
current workspace; omit synthetic view shortcuts and non-joined channels.
Forwarding must not implicitly join a channel. Enter on a forwarded permalink message navigates to the original message, including selecting the exact reply when the source is a thread.

Enter (or a result click) sends once and closes the picker. Esc or an outside
click cancels without sending. Leave the current channel, selection, focus,
thread and compose draft unchanged. Report success or failure in a toast.
No selection means no action; the Threads list itself is not a message pane
(its focused thread panel is supported). Mode changes abandon an unsubmitted forward.

## Transport and boundaries

Capture the source workspace, channel and exact selected timestamp on opening,
including a reply's timestamp rather than its parent's. Invoke a new
`core.MessageService.Forward` operation from a Bubble Tea command, with a
bounded context. Production wiring resolves the captured workspace by ID,
never whichever workspace happens to be active when the command runs.

Share the source's Slack permalink through `chat.postMessage` with link/media
unfurling enabled. Do not flatten or re-author the source body, run rich-text
conversion, or reuse compose's send-success/failure reducers. Slack controls
preview availability and source-access permissions; this is link sharing,
not a copy that grants access to a private source. The service returns the
posted timestamp and permalink. A workspace-scoped completion appends this
message idempotently to any destination panes without touching compose state.
This covers an existing self-send suppression window that could otherwise
drop a forwarded message's WebSocket echo. Normal WebSocket/cache updates
remain authoritative; no optimistic placeholder is created for forwarding.

## Tests

- Finder forwarding scope, matching/order, navigation/clicks, updates, and
  restoration of the ordinary switcher.
- Channel and thread sources; captured identity; destination dispatch;
  no navigation/draft mutation; success/error feedback; cancellation and
  mode/workspace transitions; normal Ctrl+T behavior after forwarding.
- Core adapter forwarding and unsupported-operation error.
- Slack request fields, context, permalink/post failures and empty permalink.
- Composition-root wiring resolves the source workspace by ID.
