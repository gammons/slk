# Workspace-scoped external-user notifications

External classification depends on the workspace. Both per-user and edge
resolver notices carry `TeamID`; the reducer ignores inactive-workspace notices
before mutating picker flags. Active notifications can still set or clear flags.

Tests exercise both production senders and reducer set/clear behavior for active
and inactive workspaces. This fixes live delivery only: persisted classification
is addressed independently by the external-user cache change.
