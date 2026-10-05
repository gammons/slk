# Preserve sidebar cursor identity before refiltering

Old nav indices cannot identify the selected row after items or filtered
indices change. Setters capture `currentCursorKey` before mutation and restore
it with `rebuildNavWithCursor`. Removed targets fall back to Threads; typed
search still intentionally resets selection.

Tests cover item replacement, upsert sort-key changes, removed targets and
staleness threshold/active-channel/clock changes. Existing sorting semantics
are untouched; the starred section type partition belongs to the feature PR.
