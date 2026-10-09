# Sized avatar URLs

## Purpose

Stop downloading, caching and decoding Slack's *original* avatar uploads
(up to 3000×3000, 4.3 MB) for a 4×2-cell avatar slot (~36×34 px).

Measured in a `SLK_DEBUG=1` session (2026-10-05, 12:43): one channel switch
decoded 23 avatars for **~1,970 ms of CPU**, the worst being a 3000×3000
JPEG at 322 ms. `~/.cache/slk/images` held 207 avatar files totalling
**89 MB** of the 200 MB default image-cache cap, 93 of them over 100 KB.
The same session decoded 119 images of 200 px or less in 13 ms total, so
the cost is the file size, not decoding as such.

### Success criteria

- In a debug log, `decode: key=avatar-…` lines show dims around 72×72 and
  `dur_ms=0` for Slack-hosted avatars.
- Newly cached avatar files in `~/.cache/slk/images` are a few KB each.
- A user who changes their avatar shows the new one after the next launch.

## Background

### Where avatar URLs come from

Every avatar fetch goes through `avatar.Cache.Preload` →
`preloadInner` (`internal/avatar/avatar.go`). The URL handed to it comes
from:

| Source | Field | Shape |
|---|---|---|
| Boot (`cmd/slk/bootstrap_adapters.go`, `wctx.AvatarURLs`) | `image_original` | original |
| Edge `users/info` batch (`cmd/slk/user_resolver.go`, `applyEdgeUser`) | `image_original` | original |
| Bootstrap revalidation → `users.avatar_url` (`internal/bootstrap/revalidate.go`) | `image_original` | original |
| Per-user `users.info` (`resolveOne`, `cmd/slk/users.go`) | `image_32` | already sized |
| Bots (`bestBotIcon`) | `image_72` etc. | already sized |

The boot and edge payloads carry no sized `image_NN` variants
(`internal/slack/boot/boot.go`, `SelfProfile.ImageOriginal` doc).

Observed in the local `users` table: 142 rows of the form
`https://s3-us-west-2.amazonaws.com/slack-files2/avatars/<date>/<id>_<hash>_original.<ext>`,
28,165 already-sized `avatars.slack-edge.com/…_32.<ext>` rows, and 14,876
Gravatar rows (already sized via a query parameter).

### The sized CDN form

Probed 2026-10-05 against a real original
(`…/2023-05-31/5349743656757_da76a902a76dda11cdbb_original.jpg`, 3000×3000,
4.3 MB):

| URL | Result |
|---|---|
| `https://avatars.slack-edge.com/2023-05-31/5349743656757_da76a902a76dda11cdbb_72.jpg` | 200 `image/jpeg`, 6 KB |
| same, `_48.jpg` / `_32.jpg` | 200, 5 KB / 4 KB |
| a `.png` original rewritten to `_72.png` | 200, PNG 72×72 |
| the same `.png` original rewritten to `_72.jpg` | 403 |

So the extension must be preserved. 72 px gives ~2× the avatar slot's
pixel footprint at the measured 9×17 cell size.

### Why the cache key must change

The disk-cache key today is `avatar-<userID>`, independent of URL. With
only the URL changed, the existing 3000 px files would keep being served
from disk forever. The same property is why a changed avatar never
refreshes.

## Design

### 1. `avatar.SizedURL(orig string, px int) string`

A pure function in `internal/avatar`. It rewrites exactly these shapes:

- `https://s3-<region>.amazonaws.com/slack-files2/avatars/<date>/<name>_original.<ext>`
- `https://avatars.slack-edge.com/<date>/<name>_original.<ext>`

to

- `https://avatars.slack-edge.com/<date>/<name>_<px>.<ext>`

where `<date>` is `YYYY-MM-DD`, `<name>` contains no `/`, and `<ext>` is
alphanumeric and kept verbatim. Matching is anchored: a URL with a query
string or fragment, another host (including lookalikes such as
`avatars.slack-edge.com.evil.example`), an already-sized name (`_32`,
`_72`, …), Gravatar, the empty string, or any other shape is returned
**unchanged**. That makes it safe to call on every avatar URL.

`px` is passed by the caller; production uses a package constant
`sizedAvatarPx = 72`.

Added to the AGENTS.md "Shared code" table.

### 2. Applied in `preloadInner`

`preloadInner` computes the fetch URL with the rewrite and fetches that.
None of the callers in the table above change; `users.avatar_url` keeps
storing what Slack sent (authoritative, and no schema or data migration).

The rewrite is held in an unexported `Cache` field
(`sizeURL func(string) string`), defaulted by the constructor to
`func(u string) string { return SizedURL(u, sizedAvatarPx) }`, so tests can
substitute one that recognizes `httptest` server URLs.

### 3. Fallback to the original

If the sized URL differs from the original and its fetch returns an error
(403/404, decode failure, network error), `preloadInner` fetches the
original URL once, under the original URL's own key. If that also fails,
behavior is today's: no render, no `onReady`.

The fetcher already evicts a cache entry whose bytes fail to decode
(`fetchInner`), so a bad sized response does not poison the cache.

### 4. URL-derived cache key

The fetch key becomes:

```
avatar-<userID>-<first 12 hex chars of sha256(fetchURL)>
```

computed by an unexported `fetchKey(userID, url string) string`.

- Old `avatar-<userID>.<ext>` files are never read again and age out
  through the existing LRU (`internal/image/cache.go`). No cleanup code.
- A changed avatar arrives with a new URL, hence a new key and a fresh
  fetch, on the next launch.
- The kitty source key in `renderAvatar` stays `avatar-<userID>`: it is
  per-process and per user, and the in-session "first URL wins" dedup in
  `Preload` is unchanged.
- `image.MigrateAvatars` is left as is. The files it produces use the old
  key and become orphans the LRU evicts.

## Out of scope

- Persisting decoded or rendered avatars across launches. After this
  change the decode cost is ~13 ms per session (measured above), and kitty
  uploads are per-process regardless.
- Actively deleting the existing oversized files.
- The render-pipeline costs from the same investigation (redundant full
  `buildCache` rebuilds, `borderWrap`, the threads list, sidebar rebuilds).
- Changing which size the `users.info` path requests (`image_32`).

## Testing

stdlib `testing`, white-box `package avatar`.

- `SizedURL` table test: S3 and slack-edge originals with `.jpg`, `.png`,
  `.gif`; the expected outputs character for character; and unchanged
  returns for an already-sized `_32` URL, Gravatar, a query string, a
  fragment, a lookalike host, a missing date segment, and `""`.
- `Cache` tests against an `httptest` server, with `sizeURL` replaced:
  - the sized URL is the one requested, and the original is not;
  - a 404 on the sized URL falls back to the original, and the avatar
    renders;
  - when the sized URL equals the original (no rewrite), exactly one
    request is made;
  - two different URLs for the same user produce different fetch keys
    (`fetchKey` unit test), and a second `Cache` sharing the same disk
    cache, given a new URL for that user, requests the new URL rather than
    serving the old file.

Before the PR: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`gofmt -l .` empty, `golangci-lint run`.

## Verification

Run slk with `SLK_DEBUG=1`, open a channel with many human authors, and
check the `decode: key=avatar-…` lines in `/tmp/slk-debug.log` for dims
and `dur_ms`.
