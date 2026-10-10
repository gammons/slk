# Workspace-relative external-user cache

Profiles remain keyed globally by user ID. Store Slack's home team and
derive external classification relative to the destination workspace, rather
than overwriting the original workspace's boolean during inactive resolution.

An additive migration resets conditional versions once for profile backfill.
Empty profile/placeholder values preserve a known home team. Legacy booleans
are used only in their original workspace. Ready and switch snapshots use a
caller-owned `DB.ExternalUsers(workspaceID)` set.

Tests cover all conflict writers and avatar branches, unknown/legacy teams,
placeholder preservation, migration/reopen behavior and real workspace switching
through the mention picker. No starred parsing or sidebar behavior changes.
