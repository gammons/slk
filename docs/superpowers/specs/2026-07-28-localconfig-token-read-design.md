# Desktop `localConfig_v2` Token Read Design

## Problem

slk v0.12.0 acquires each workspace's `xoxc` token by loading the workspace
page (`GET https://<domain>.slack.com`) and scraping `api_token` from the HTML.
On **Enterprise Grid** this fails: the request redirects to the enterprise host
(`https://<org>.enterprise.slack.com/…`) and returns a **sign-in page** — no
token. Reported in #111 (Red Hat); confirmed via the `--dump-mint` diagnostic:

```
=== Red Hat Inc. (redhat.slack.com) ===
  status: 200
  final URL: https://redhat.enterprise.slack.com/?redir=%2F…
  api_token: false
  login signs: [signin-prompt]
```

Scraping the token from a page load fundamentally cannot work for Grid.

## Key Insight

The desktop app already stores every team's `xoxc` token on disk, in the
`localConfig_v2` value of its Local Storage leveldb — the same structure the
*old* browser method read. A pure-Go spike confirmed we can read it:

```
key:   _https://app.slack.com\x00\x01localConfig_v2   (value: 0x01 prefix + JSON)
teams: {"T…":{"id","name","url","domain","token":"xoxc-…"}, …}
```

Critically, **API calls (`auth.test`, etc.) with `token`+`d` cookie always
worked** — the only broken step was scraping the token from a page. Reading the
token from `localConfig_v2` bypasses exactly that step, so Grid works.

## Solution

Replace the network page-scrape mint with a **direct read of `localConfig_v2`**
from the Slack desktop app's Local Storage leveldb. The `d` cookie is still read
from the Cookies sqlite DB (unchanged). `localConfig_v2` becomes the single
source for both the workspace list and each team's token.

### Design decisions

- **`localConfig_v2` is the single source** for the workspace list and tokens.
  Drop the `root-state.json` parser — `localConfig_v2` carries the same team
  info plus the token, and the spike showed it is the accurate live set.
- **Remove the network mint entirely** — `MintToken`, `mintTokenAt`,
  `newMintRequest`, `parseRetryAfter`, `MintDiag`, and the nav-header / 429
  machinery all go away (moot once no page is loaded).
- **Repurpose `--dump-mint`** into a leveldb/auth diagnostic.
- New dependency: `github.com/syndtr/goleveldb` (pure Go — `CGO_ENABLED=0`
  preserved).

## `internal/slackdesktop`

New `localconfig.go`. `Workspaces()` (the existing public entry point that
onboarding/refresh already call) is reimplemented here to read
`localConfig_v2`:

```go
func Workspaces() ([]Workspace, error)          // now reads localConfig_v2
func readLocalConfigValue(configDir string) ([]byte, error) // leveldb read
func decodeLocalStorageValue(v []byte) string   // 0x00 UTF-16LE / 0x01 Latin-1
func parseLocalConfig(jsonBytes []byte) ([]Workspace, error)
```

`Workspaces()`:
1. Locate `ConfigDir()/Local Storage/leveldb`. Missing → `ErrNotSignedIn`.
2. **Copy the leveldb directory to a temp dir** (it's locked while Slack runs;
   copying also avoids side effects), then `leveldb.OpenFile(tmp, ReadOnly)`.
3. Iterate keys; find the one that ends with `localConfig_v2` and contains
   `app.slack.com`. Missing → `ErrNotSignedIn`.
4. `decodeLocalStorageValue`: first byte `0x00` → UTF-16LE, `0x01` →
   UTF-8/Latin-1; else raw.
5. `parseLocalConfig`: JSON-parse `{"teams": {teamID:
   {id,name,url,domain,token}}}`; skip entries missing `domain`/`id`/`token`;
   empty → `ErrNotSignedIn`; sort by name.

`Workspace` gains `Token string`:

```go
type Workspace struct {
	Name   string
	Domain string
	TeamID string
	Token  string // xoxc-… from localConfig_v2
}
```

The `root-state.json` parser (`workspaces.go`), its test, and the
`testdata/root-state.json` fixture are removed.

Unchanged: `Cookie()` (sqlite), `ConfigDir()`, `ProfileCandidates()`, all
existing typed errors.

## `internal/slack`

Delete the network-mint machinery: `mint.go` (`MintToken`, `mintTokenAt`,
`newMintRequest`, `parseRetryAfter`, `apiTokenRE`, `MintDiagnostics`,
`MintDiag`, `mintDiagAt`, `detectLoginMarkers`) and `mint_test.go`. Nothing
else in the package depends on them.

`Token` struct, `NewClient`, `Connect`, cookie HTTP client, browser transport:
all unchanged (still used for the ongoing API/WebSocket traffic).

## `cmd/slk`

### Onboarding (`onboarding.go`, `onboarding_core.go`)

`buildWorkspaceTokens` no longer takes a `minter`. It builds a `Token` per
selected workspace directly from the workspace's `Token` + the cookie:

```go
func buildWorkspaceTokens(cookie string, ws []slackdesktop.Workspace, selected map[string]bool) []slackclient.Token
```

`addWorkspace` flow: `Cookie()` + `Workspaces()` (now token-bearing) →
multi-select → `buildWorkspaceTokens` → for each: `NewClient(tok, cookie)` +
`Connect()` (validates + resolves Grid host) → `Save`. Targeted
`desktopErrorMessage` mapping unchanged.

### Startup refresh (`remint.go`)

Rename to reflect the new source; refresh tokens from `localConfig_v2` instead
of minting:

```go
func refreshTokens(tokens []slackclient.Token,
    cookieFn func() (string, error),
    teamsFn func() ([]slackdesktop.Workspace, error),
    saveFn func(slackclient.Token) error) []slackclient.Token
```

For each stored token, look up the matching team (by `TeamID`) in the freshly
read `localConfig_v2` and update `AccessToken` + `Cookie`. On any read failure,
keep cached tokens (offline-friendly). Wire into `main.go` where `remintTokens`
was called.

### `--dump-mint` (repurposed)

Reads cookie + `localConfig_v2`; prints profiles, cookie status, and per team:
whether a token was found in `localConfig_v2`, and whether `NewClient(tok,
cookie).Connect()` succeeds (with the resolved API host). No secrets in output.

## Testing

Unit:
- `decodeLocalStorageValue` for `0x00` (UTF-16LE) and `0x01` (Latin-1).
- `parseLocalConfig` against a checked-in JSON fixture (multiple teams; skips
  incomplete entries; empty → `ErrNotSignedIn`).
- `buildWorkspaceTokens` (token+cookie assembly, selection filter).
- `refreshTokens` (updates by TeamID; keeps cached on failure).

Manual (maintainer, Linux, non-Grid): `--dump-mint` and full `--add-workspace`
against the live desktop app; confirm token comes from `localConfig_v2`,
`Connect` succeeds, TUI loads; second launch exercises refresh.

Community: **larsks confirms Enterprise Grid** (`redhat.slack.com`) now
onboards and connects.

## Dependencies

- `github.com/syndtr/goleveldb` — pure-Go leveldb reader (no cgo).

## Out of Scope

- Reading the cookie differently (still the Cookies sqlite).
- Any change to API/WebSocket traffic or the browser-header transport.
- Windows/macOS Local Storage paths differ only by `ConfigDir()`, which is
  already OS-aware; the leveldb subpath (`Local Storage/leveldb`) is the same
  across platforms.
