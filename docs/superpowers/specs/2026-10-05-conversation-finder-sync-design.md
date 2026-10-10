# Refresh finder snapshots on conversation deduplication

An ordinary duplicate open can update the sidebar but leave its existing
finder entry stale. Upsert both by conversation ID. `upsertFinderItem` returns
the stored entry; existing rows keep live `LastVisited`, while new rows seed
it from the startup visit map. Active UI publication uses the same stored
visit metadata.

Tests cover ordinary duplicate opens, a missing finder alongside an existing
sidebar row, fresh name/presence, visit preservation and UI publication.
Feature-specific `refreshDMPeerFromCache` remains in the starred feature PR.
