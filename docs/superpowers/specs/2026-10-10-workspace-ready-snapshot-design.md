# Detach startup conversation snapshots

## Pre-existing bug

`WorkspaceReadyMsg.Channels` previously received `wctx.Channels` directly.
The sidebar retains that slice and writes its rows on the UI goroutine, while
the WebSocket event owner updates the workspace rows. Both could write the same
backing array. Deferred peer repair makes this existing race easier to trigger.
The workspace-switch path already copies channels through `withPeerStatuses`.

## Fix

Clone channel and finder slices in the startup message. Start the connection
manager only after those snapshots are cloned and handed to the UI; merely
cloning after starting the event owner could itself race its writes.
Channel/finder items contain value fields, so shallow slice clones suffice.
This startup ownership correction is distinct from retry scheduling and is
included with the requested review fixes, not an unrelated refactor.
Unsynchronized UI reads of `wctx.Channels` in Lookup, `railUnreadWorkspaces`
and the workspace-switch callback remain tracked under #208; this change does
not close the entire class of conversation-snapshot races.

## Regression check

The composition root starts the TUI and cannot be called in a unit test.
`TestWorkspaceReadySnapshotsPrecedeEventOwner` pins its wiring structurally:
both fields use `slices.Clone`, exactly one event-owner launch exists, and it
starts after their handoff.
The full race suite exercises the existing UI and deferred-repair paths.
