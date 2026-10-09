# Persist full-profile retry status before notification

`UpsertUser` must retain placeholder-safe conflict behavior. Full Web API
profiles instead call `applyProfileStatus` immediately after upsert, before name
store publication or `UserResolvedMsg`. Updates, clears and huddle changes now
reach SQLite before a consumer can re-read the profile.

Tests start with an existing stale row and check cache values during the status
notification itself, notification ordering, clearing and cache-only resolution.
This change does not add resolver context APIs or starred recovery logic.
