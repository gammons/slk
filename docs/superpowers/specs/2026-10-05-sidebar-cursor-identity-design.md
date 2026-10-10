# Preserve sidebar cursor identity before refiltering

Old nav indices cannot identify the selected row after items or filtered
indices change. Setters capture `currentCursorKey` before mutation and restore
it with `rebuildNavWithCursor`. Removed targets fall back to Threads; typed
search still intentionally resets selection.

Tests cover item replacement, sections-provider reordering, upsert sort-key
changes and new DMs sorting ahead of the selected channel, removed targets,
and staleness threshold/active-channel/clock changes. Existing sorting semantics
are untouched; the starred section type partition belongs to the feature PR.
