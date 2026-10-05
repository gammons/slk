# Display starred people alongside starred channels

Treat Slack channel and IM stars as conversation membership; ignore
message/file/comment stars. Use one real Slack Starred section, channels first
(public/private), then people/app/group conversation rows, stably preserving
order within each block. Custom sections named Starred remain unchanged.

Recover accessible closed starred one-to-one DMs with existing boot metadata
or read-only conversation info, never conversations.open or invented user rows.
Deduplicate against loaded conversations. Reconnect repeats reconciliation with
a shared HTTP deadline; unknown peers use a context-bounded edge batch and
per-user fallback. Cache-only repairs still run after cancellation.

Peer resolution and row hydration are independent. Existing DMUserID values
retain retry targets. `refreshDMPeerFromCache` repairs sidebar/finder snapshots
independently, preserves section/order/visits/DND, mirrors notifier maps and
publishes only changed active-workspace rows. Inactive snapshots remain correct
for subsequent workspace switches. Only the serialized event owner mutates
conversation slices; the background sweep updates synchronized profile state.

Tests cover parsing/membership/order/navigation, startup and reconnect recovery,
failed/canceled profile retries, late names/app classification, finder/status
repair, workspace switching and concurrent sweep integration. Independent cache,
notification, profile, bot, finder and cursor fixes are reviewed in separate PRs.

No new configuration, subheaders, star/unstar commands, pagination rewrite or
new star WebSocket protocol. Large-list pagination and immediate star events
remain follow-ups.
