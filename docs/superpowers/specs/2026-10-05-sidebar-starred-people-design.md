# Display starred people alongside starred channels

Treat Slack channel and IM stars as conversation membership; ignore
message/file/comment stars. Use one real Slack Starred section, channels first
(public/private), then people/app/group conversation rows, stably preserving
order within each block. Custom sections named Starred remain unchanged.

Recovery requires Slack-native sections (`wctx.SectionStore`), created when the
Slack section bootstrap succeeds. Config-defined sections and the no-provider
fallback do not run starred-conversation recovery or get this ordering behavior;
a custom section named Starred is not the native stars section.

At startup, recovery runs after `users.conversations` (or the boot-conversation
fallback on failure) and the closed-group-DM filter, before building sidebar and
finder rows. It only recovers starred IDs absent from that result. The successful
`users.conversations` path does not locally filter closed one-to-one IMs; this
design does not assume Slack always includes or always omits them. The boot
fallback does exclude closed one-to-one IMs, making starred recovery necessary
there. Available boot IM metadata supplies the peer without a network lookup;
otherwise recovery uses read-only conversation info. Neither path calls
`conversations.open` or invents user rows. Startup recovery has a shared 10 s
HTTP deadline.

Initial WebSocket connect and reconnect reconcile missing starred conversations.
The entire section-refresh/reconciliation block is debounced to once per 30 s
per event handler, including failed lookups and skipped section rebootstrap.
Its shared 30 s HTTP deadline bounds the delay before presence/DND bootstrap;
on a connection flap the block is skipped and presence starts immediately.
Presence peer IDs are captured on the serialized event owner after recovery so
new human DM rows are included and bots excluded. Unknown peers use a
context-bounded edge batch and per-user fallback. Cache-only repairs still run
after cancellation. Any peer still unresolved gets a non-blocking deferred retry
outside that shared budget,
so quiet peers do not depend on a message or membership lookup to resolve.
Unnamed cache placeholders remain eligible for a first successful lookup.

Peer resolution and row hydration are independent. Existing DMUserID values
retain retry targets. `repairDMPeer` imports cached bot classification and uses
`refreshDMPeerFromCache` to repair sidebar/finder snapshots
independently, preserves section/order/visits/DND, mirrors notifier maps and
publishes only changed active-workspace rows. Finder upserts keep the newer of
the incoming and UI-owned visit times so late profile repair cannot move a DM
down in this session's recents. Inactive snapshots remain correct
for subsequent workspace switches. Only the serialized event owner mutates
conversation slices; the background sweep updates synchronized profile state.

Successful edge and per-user resolver lookups also coalesce peer IDs in a
synchronized pending set and signal the event owner without blocking. The
WebSocket reader feeds a separate serialized dispatcher, which accepts this
local wake-up even when Slack sends no new frames. The dispatcher drains the
pending IDs, filters out users without DM rows before reading SQLite and calls
`repairDMPeer`. The automatically scheduled retry repairs both rows when it
succeeds, without another reconnect or a startup UnresolvedDMs entry.
No new Slack event or UI I/O is introduced.

The DM sweep's per-user fallback also queues a repair, including for inactive
workspaces with no UI sender.

Startup hands the UI cloned channel/finder slices before starting the event
owner. This fixes a pre-existing shared-array race, documented separately in
`2026-10-10-workspace-ready-snapshot-design.md`.

Tests cover parsing/membership/order/navigation, startup and reconnect recovery,
failed/canceled profile retries, late names/app classification, finder/status
repair without reconnect (edge and per-user, active and inactive workspaces),
idle-socket wake-up and serialized dispatch, UI visits newer than workspace
snapshots, reconnect debounce and expiry, malformed type flags, redacted stars,
cancellation after a successful lookup, workspace switching and concurrent
sweep integration. Independent cache, notification, profile, bot, finder and
cursor fixes are reviewed in separate PRs.

No new configuration, subheaders, star/unstar commands, pagination rewrite or
new star WebSocket protocol. Large-list pagination and immediate star events
remain follow-ups.
