# localConfig_v2 Token Read Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Read each workspace's `xoxc` token directly from the Slack desktop app's `localConfig_v2` (Local Storage leveldb) instead of scraping it from a network page load, fixing Enterprise Grid onboarding (#111).

**Architecture:** `internal/slackdesktop.Workspaces()` is reimplemented to read `localConfig_v2` from a temp copy of the locked leveldb using pure-Go `goleveldb`, returning teams with their tokens. Onboarding and startup refresh use those tokens directly; the network-mint machinery is deleted. The `d` cookie is still read from the Cookies sqlite DB.

**Tech Stack:** Go, `github.com/syndtr/goleveldb` (pure Go), existing `modernc.org/sqlite` + keyring stack.

**Spec:** `docs/superpowers/specs/2026-07-28-localconfig-token-read-design.md`

**Module path:** `github.com/gammons/slk`. The Slack API package is at `internal/slack`, package name `slackclient`.

**Verified leveldb facts (from spike):** key is `_https://app.slack.com\x00\x01localConfig_v2`; it ends with `localConfig_v2` and contains `app.slack.com`. Value first byte is an encoding marker (`0x01` = UTF-8/Latin-1, `0x00` = UTF-16LE) followed by JSON `{"teams":{"T…":{"id","name","url","domain","token"}}}`. Open with `leveldb.OpenFile(dir, &opt.Options{ReadOnly: true})`.

---

## Sequencing (keep the build green)

Delete `internal/slack/mint.go` **last** (Task 6), after onboarding, refresh, and `--dump-mint` all stop referencing it.

## File Structure

- Modify: `internal/slackdesktop/workspaces.go` — add `Token` field; reimplement `Workspaces()` to read localConfig_v2 (replaces root-state.json parsing).
- Create: `internal/slackdesktop/localconfig.go` — leveldb read, value decode, JSON parse.
- Create: `internal/slackdesktop/localconfig_test.go`, `internal/slackdesktop/testdata/localconfig.json`.
- Delete: `internal/slackdesktop/testdata/root-state.json`, `internal/slackdesktop/workspaces_test.go` (root-state tests).
- Modify: `cmd/slk/onboarding_core.go`, `onboarding_core_test.go`, `onboarding.go`.
- Modify: `cmd/slk/remint.go`, `remint_test.go`, `cmd/slk/main.go`.
- Delete: `internal/slack/mint.go`, `internal/slack/mint_test.go`.

---

## Task 1: `Workspace.Token` + pure localConfig decode/parse

**Files:**
- Modify: `internal/slackdesktop/workspaces.go` (add `Token` field only)
- Create: `internal/slackdesktop/localconfig.go` (pure functions only for now)
- Create: `internal/slackdesktop/localconfig_test.go`
- Create: `internal/slackdesktop/testdata/localconfig.json`

- [ ] **Step 1: Add `Token` to `Workspace`** in `internal/slackdesktop/workspaces.go`:

```go
type Workspace struct {
	Name   string
	Domain string // subdomain under .slack.com
	TeamID string
	Token  string // xoxc-… from localConfig_v2
}
```

- [ ] **Step 2: Write fixture `internal/slackdesktop/testdata/localconfig.json`:**

```json
{"teams":{
  "T054JFC9S2Z":{"id":"T054JFC9S2Z","name":"Truelist","url":"https://truelist-workspace.slack.com/","domain":"truelist-workspace","token":"xoxc-truelist"},
  "TUJLNE62Z":{"id":"TUJLNE62Z","name":"UserEvidence","url":"https://userevidence.slack.com/","domain":"userevidence","token":"xoxc-ue"},
  "TBROKEN":{"id":"TBROKEN","name":"NoToken","url":"https://x.slack.com/","domain":"x","token":""}
}}
```

- [ ] **Step 3: Write failing test `internal/slackdesktop/localconfig_test.go`:**

```go
package slackdesktop

import (
	"os"
	"testing"
	"unicode/utf16"
)

func TestParseLocalConfig(t *testing.T) {
	data, err := os.ReadFile("testdata/localconfig.json")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := parseLocalConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	// TBROKEN has an empty token and must be skipped; 2 remain, sorted by name.
	if len(ws) != 2 {
		t.Fatalf("got %d workspaces, want 2: %+v", len(ws), ws)
	}
	if ws[0].Name != "Truelist" || ws[0].Domain != "truelist-workspace" || ws[0].TeamID != "T054JFC9S2Z" || ws[0].Token != "xoxc-truelist" {
		t.Errorf("ws[0] = %+v", ws[0])
	}
}

func TestParseLocalConfigEmpty(t *testing.T) {
	if _, err := parseLocalConfig([]byte(`{"teams":{}}`)); err != ErrNotSignedIn {
		t.Errorf("got %v, want ErrNotSignedIn", err)
	}
}

func TestDecodeLocalStorageValue(t *testing.T) {
	// 0x01 marker → UTF-8/Latin-1 remainder.
	if got := decodeLocalStorageValue(append([]byte{0x01}, []byte(`{"a":1}`)...)); got != `{"a":1}` {
		t.Errorf("latin1 decode = %q", got)
	}
	// 0x00 marker → UTF-16LE remainder.
	u16 := utf16.Encode([]rune(`{"a":1}`))
	b := []byte{0x00}
	for _, c := range u16 {
		b = append(b, byte(c), byte(c>>8))
	}
	if got := decodeLocalStorageValue(b); got != `{"a":1}` {
		t.Errorf("utf16 decode = %q", got)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/slackdesktop/ -run 'TestParseLocalConfig|TestDecodeLocalStorageValue' -v`
Expected: FAIL — `parseLocalConfig` / `decodeLocalStorageValue` undefined.

- [ ] **Step 5: Write the pure functions in `internal/slackdesktop/localconfig.go`:**

```go
package slackdesktop

import (
	"encoding/json"
	"sort"
	"unicode/utf16"
)

// decodeLocalStorageValue strips the 1-byte encoding marker Chromium's Local
// Storage prepends to values: 0x00 => UTF-16LE, 0x01 => UTF-8/Latin-1.
func decodeLocalStorageValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		b := v[1:]
		u16 := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return string(utf16.Decode(u16))
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

// parseLocalConfig parses the localConfig_v2 JSON into signed-in workspaces
// (teams with a usable token). Entries missing domain/id/token are skipped.
func parseLocalConfig(data []byte) ([]Workspace, error) {
	var cfg struct {
		Teams map[string]struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Domain string `json:"domain"`
			Token  string `json:"token"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	var out []Workspace
	for _, t := range cfg.Teams {
		if t.Domain == "" || t.ID == "" || t.Token == "" {
			continue
		}
		out = append(out, Workspace{Name: t.Name, Domain: t.Domain, TeamID: t.ID, Token: t.Token})
	}
	if len(out) == 0 {
		return nil, ErrNotSignedIn
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/slackdesktop/ -run 'TestParseLocalConfig|TestDecodeLocalStorageValue' -v`
Expected: PASS. (Note: `go build ./...` may now fail because the old `Workspaces()` in workspaces.go still exists and other code is unchanged — that's fine, `Workspaces()` is replaced in Task 2. If the package itself doesn't compile due to an unused import, fix imports; do not touch `Workspaces()` yet.)

- [ ] **Step 7: Commit**

```bash
git add internal/slackdesktop/workspaces.go internal/slackdesktop/localconfig.go internal/slackdesktop/localconfig_test.go internal/slackdesktop/testdata/localconfig.json
git commit -m "feat(slackdesktop): parse localConfig_v2 teams+tokens (pure)"
```

---

## Task 2: `Workspaces()` reads localConfig_v2 from leveldb

**Files:**
- Modify: `internal/slackdesktop/localconfig.go` (add leveldb read + new `Workspaces()`)
- Modify: `internal/slackdesktop/workspaces.go` (remove old root-state `Workspaces()` + `rootState` + `parseWorkspaces`)
- Delete: `internal/slackdesktop/workspaces_test.go`, `internal/slackdesktop/testdata/root-state.json`
- Modify: `go.mod`, `go.sum` (add goleveldb)

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/syndtr/goleveldb/leveldb@v1.0.0 && go mod tidy`

- [ ] **Step 2: Add leveldb read + `Workspaces()` to `internal/slackdesktop/localconfig.go`.** Add these imports to the file's import block: `"os"`, `"io"`, `"path/filepath"`, `"strings"`, `"github.com/syndtr/goleveldb/leveldb"`, `"github.com/syndtr/goleveldb/leveldb/opt"`, `"github.com/syndtr/goleveldb/leveldb/util"`. Add:

```go
// Workspaces reads the desktop app's signed-in teams (with tokens) from the
// localConfig_v2 value in its Local Storage leveldb.
func Workspaces() ([]Workspace, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	value, err := readLocalConfigValue(dir)
	if err != nil {
		return nil, err
	}
	return parseLocalConfig([]byte(decodeLocalStorageValue(value)))
}

// readLocalConfigValue returns the raw localConfig_v2 value bytes (including
// the 1-byte encoding marker) from the Local Storage leveldb. Slack keeps the
// DB open, so we read a temp copy.
func readLocalConfigValue(configDir string) ([]byte, error) {
	src := filepath.Join(configDir, "Local Storage", "leveldb")
	if _, err := os.Stat(src); err != nil {
		return nil, ErrNotSignedIn
	}
	tmp, err := os.MkdirTemp("", "slk-lvl-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyLevelDB(src, tmp); err != nil {
		return nil, err
	}

	db, err := leveldb.OpenFile(tmp, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var value []byte
	iter := db.NewIterator(&util.Range{}, nil)
	for iter.Next() {
		k := string(iter.Key())
		if strings.HasSuffix(k, "localConfig_v2") && strings.Contains(k, "app.slack.com") {
			value = append([]byte(nil), iter.Value()...) // last write wins
		}
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, ErrNotSignedIn
	}
	return value, nil
}

// copyLevelDB copies the leveldb files (skipping the LOCK) into dst.
func copyLevelDB(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "LOCK" {
			continue
		}
		in, err := os.Open(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}
		out, err := os.Create(filepath.Join(dst, e.Name()))
		if err != nil {
			in.Close()
			return err
		}
		_, cerr := io.Copy(out, in)
		in.Close()
		out.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}
```

- [ ] **Step 3: Remove the old root-state implementation** from `internal/slackdesktop/workspaces.go`: delete the old `Workspaces()` function, the `rootState` type, and `parseWorkspaces`. Keep the `Workspace` struct (with the new `Token` field) in this file. If that leaves `workspaces.go` with only the `Workspace` struct + now-unused imports, remove the unused imports (`encoding/json`, `os`, `path/filepath`, `sort`).

- [ ] **Step 4: Delete the root-state test + fixture**

```bash
git rm internal/slackdesktop/workspaces_test.go internal/slackdesktop/testdata/root-state.json
```

- [ ] **Step 5: Build + test the package**

Run: `go build ./internal/slackdesktop/ && go test ./internal/slackdesktop/ -v`
Expected: build clean; existing + new tests pass. (`go build ./...` may still fail in `cmd/slk`/`internal/slack` until later tasks — that's expected; verify the whole module builds by the end of Task 6.)

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(slackdesktop): Workspaces() reads localConfig_v2 via goleveldb"
```

---

## Task 3: Onboarding uses tokens directly (drop the minter)

**Files:**
- Modify: `cmd/slk/onboarding_core.go`
- Modify: `cmd/slk/onboarding_core_test.go`
- Modify: `cmd/slk/onboarding.go`

- [ ] **Step 1: Rewrite the test `cmd/slk/onboarding_core_test.go`:**

```go
package main

import (
	"testing"

	"github.com/gammons/slk/internal/slackdesktop"
)

func TestBuildWorkspaceTokens(t *testing.T) {
	ws := []slackdesktop.Workspace{
		{Name: "Acme", Domain: "acme", TeamID: "T1", Token: "xoxc-acme"},
		{Name: "Beta", Domain: "beta", TeamID: "T2", Token: "xoxc-beta"},
	}
	selected := map[string]bool{"T1": true} // only Acme
	toks := buildWorkspaceTokens("xoxd-c", ws, selected)
	if len(toks) != 1 {
		t.Fatalf("got %d tokens, want 1", len(toks))
	}
	got := toks[0]
	if got.TeamID != "T1" || got.AccessToken != "xoxc-acme" || got.Domain != "acme" || got.Cookie != "xoxd-c" || got.TeamName != "Acme" {
		t.Fatalf("unexpected token: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestBuildWorkspaceTokens -v`
Expected: FAIL (signature mismatch — `buildWorkspaceTokens` still takes a minter).

- [ ] **Step 3: Rewrite `cmd/slk/onboarding_core.go`:**

```go
package main

import (
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// buildWorkspaceTokens builds a Token per selected workspace from the token the
// desktop app already holds (localConfig_v2) plus the d cookie. Workspaces
// whose TeamID is not in `selected` are skipped.
func buildWorkspaceTokens(cookie string, ws []slackdesktop.Workspace, selected map[string]bool) []slackclient.Token {
	var out []slackclient.Token
	for _, w := range ws {
		if !selected[w.TeamID] {
			continue
		}
		out = append(out, slackclient.Token{
			AccessToken: w.Token,
			Cookie:      cookie,
			Domain:      w.Domain,
			TeamID:      w.TeamID,
			TeamName:    w.Name,
		})
	}
	return out
}
```

- [ ] **Step 4: Update `addWorkspace()` in `cmd/slk/onboarding.go`.** Replace the minting block:

```go
	// Mint tokens for the selected workspaces.
	fmt.Println()
	fmt.Println(stepStyle.Render("Connecting..."))
	tokens, err := buildWorkspaceTokens(context.Background(), cookie, workspaces, selected, slackclient.MintToken)
	if err != nil {
		fmt.Println(errorStyle.Render("  Failed to mint token: " + err.Error()))
		return err
	}
```

with:

```go
	fmt.Println()
	fmt.Println(stepStyle.Render("Connecting..."))
	tokens := buildWorkspaceTokens(cookie, workspaces, selected)
```

The subsequent validate/save loop (`NewClient` + `Connect` + `Save`) is unchanged. If `context` is now unused in `onboarding.go`, keep it only if `context.Background()` is still referenced in the Connect calls (it is — leave the import).

- [ ] **Step 5: Build + test**

Run: `go build ./cmd/slk/ 2>&1 | head; go test ./cmd/slk/ -run TestBuildWorkspaceTokens -v`
Expected: `cmd/slk` compiles; test PASS. (`slackclient.MintToken` still exists, now unused by onboarding.)

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/onboarding.go cmd/slk/onboarding_core.go cmd/slk/onboarding_core_test.go
git commit -m "feat(onboarding): use localConfig_v2 tokens directly (no minting)"
```

---

## Task 4: Startup refresh from localConfig_v2

**Files:**
- Modify: `cmd/slk/remint.go` (rewrite as `refreshTokens`)
- Modify: `cmd/slk/remint_test.go`
- Modify: `cmd/slk/main.go` (call site)

- [ ] **Step 1: Rewrite `cmd/slk/remint_test.go`:**

```go
package main

import (
	"context"
	"testing"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

func TestRefreshTokens(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old", Cookie: "c-old", Domain: "acme", TeamID: "T1", TeamName: "Acme"}}
	saved := map[string]slackclient.Token{}
	out := refreshTokens(in,
		func() (string, error) { return "c-new", nil },
		func() ([]slackdesktop.Workspace, error) {
			return []slackdesktop.Workspace{{Name: "Acme", Domain: "acme", TeamID: "T1", Token: "xoxc-new"}}, nil
		},
		func(tok slackclient.Token) error { saved[tok.TeamID] = tok; return nil },
	)
	if out[0].AccessToken != "xoxc-new" || out[0].Cookie != "c-new" {
		t.Fatalf("token not refreshed: %+v", out[0])
	}
	if saved["T1"].AccessToken != "xoxc-new" {
		t.Fatalf("refreshed token not persisted: %+v", saved["T1"])
	}
}

func TestRefreshTokensKeepsOldOnReadFailure(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old", Cookie: "c-old", Domain: "acme", TeamID: "T1"}}
	out := refreshTokens(in,
		func() (string, error) { return "", context.Canceled },
		func() ([]slackdesktop.Workspace, error) { return nil, context.Canceled },
		func(slackclient.Token) error { return nil },
	)
	if out[0].AccessToken != "old" {
		t.Fatalf("expected cached token, got %+v", out[0])
	}
}

func TestRefreshTokensKeepsOldWhenTeamMissing(t *testing.T) {
	in := []slackclient.Token{{AccessToken: "old", Cookie: "c-old", Domain: "acme", TeamID: "T1"}}
	out := refreshTokens(in,
		func() (string, error) { return "c-new", nil },
		func() ([]slackdesktop.Workspace, error) {
			return []slackdesktop.Workspace{{Name: "Other", Domain: "other", TeamID: "T9", Token: "xoxc-other"}}, nil
		},
		func(slackclient.Token) error { return nil },
	)
	if out[0].AccessToken != "old" {
		t.Fatalf("expected cached token when team absent, got %+v", out[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/slk/ -run TestRefresh -v`
Expected: FAIL — `refreshTokens` undefined.

- [ ] **Step 3: Rewrite `cmd/slk/remint.go`:**

```go
package main

import (
	"log"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

// refreshTokens re-reads each workspace's xoxc token from the desktop app's
// localConfig_v2 (and the live d cookie) on startup, so a rotated/expired
// stored token never causes a failed launch. On any read failure, or when a
// stored team is not present in localConfig_v2, the cached token is kept.
func refreshTokens(
	tokens []slackclient.Token,
	cookieFn func() (string, error),
	teamsFn func() ([]slackdesktop.Workspace, error),
	saveFn func(slackclient.Token) error,
) []slackclient.Token {
	cookie, cerr := cookieFn()
	teams, terr := teamsFn()
	if cerr != nil || terr != nil {
		log.Printf("refresh: could not read desktop session (cookie err=%v, teams err=%v); using cached tokens", cerr, terr)
		return tokens
	}
	byID := make(map[string]slackdesktop.Workspace, len(teams))
	for _, w := range teams {
		byID[w.TeamID] = w
	}
	out := make([]slackclient.Token, len(tokens))
	copy(out, tokens)
	for i := range out {
		w, ok := byID[out[i].TeamID]
		if !ok || w.Token == "" {
			log.Printf("refresh: %s not in localConfig_v2; keeping cached token", out[i].TeamName)
			continue
		}
		out[i].AccessToken = w.Token
		out[i].Cookie = cookie
		if err := saveFn(out[i]); err != nil {
			log.Printf("refresh: %s: save failed: %v", out[i].TeamName, err)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/slk/ -run TestRefresh -v`
Expected: PASS.

- [ ] **Step 5: Update the call site in `cmd/slk/main.go`.** Replace:

```go
	tokens = remintTokens(context.Background(), tokens,
		slackdesktop.Cookie,
		slackclient.MintToken,
		tokenStore.Save,
	)
```

with:

```go
	tokens = refreshTokens(tokens,
		slackdesktop.Cookie,
		slackdesktop.Workspaces,
		tokenStore.Save,
	)
```

- [ ] **Step 6: Build + test**

Run: `go build ./cmd/slk/ 2>&1 | head; go test ./cmd/slk/ -run TestRefresh -v`
Expected: compiles; PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/slk/remint.go cmd/slk/remint_test.go cmd/slk/main.go
git commit -m "feat: refresh tokens from localConfig_v2 on startup"
```

---

## Task 5: Repurpose `--dump-mint`

**Files:**
- Modify: `cmd/slk/main.go` (`dumpMint`)

- [ ] **Step 1: Rewrite `dumpMint()` in `cmd/slk/main.go`** so it no longer uses `slackclient.MintDiag`. New body:

```go
func dumpMint() error {
	fmt.Println("slk auth diagnostic (#111)")
	fmt.Println()

	fmt.Println("Slack desktop profiles (* = the one slk uses):")
	for _, c := range slackdesktop.ProfileCandidates() {
		marker := " "
		if c.Active {
			marker = "*"
		}
		fmt.Printf("  [%s] %-7s exists=%-5v cookieDB=%-5v %s\n", marker, c.Kind, c.Exists, c.HasCookie, c.Path)
	}
	fmt.Println()

	dir, err := slackdesktop.ConfigDir()
	if err != nil {
		fmt.Printf("ConfigDir: ERROR: %v\n", err)
		return nil
	}
	fmt.Printf("Using config dir: %s\n", dir)

	cookie, err := slackdesktop.Cookie()
	if err != nil {
		fmt.Printf("Cookie: ERROR: %v\n", err)
		return nil
	}
	format := "(not xoxd-)"
	if strings.HasPrefix(cookie, "xoxd-") {
		format = "xoxd-..."
	}
	fmt.Printf("Cookie: OK (len=%d, format=%s)\n\n", len(cookie), format)

	workspaces, err := slackdesktop.Workspaces()
	if err != nil {
		fmt.Printf("Workspaces (localConfig_v2): ERROR: %v\n", err)
		return nil
	}

	ctx := context.Background()
	for _, ws := range workspaces {
		fmt.Printf("=== %s (%s.slack.com) ===\n", ws.Name, ws.Domain)
		hasTok := strings.HasPrefix(ws.Token, "xoxc-")
		fmt.Printf("  token in localConfig_v2: %v (len %d)\n", hasTok, len(ws.Token))
		client := slackclient.NewClient(ws.Token, cookie)
		if err := client.Connect(ctx); err != nil {
			fmt.Printf("  connect: FAILED: %v\n\n", err)
			continue
		}
		fmt.Printf("  connect: OK (teamID=%s, apiHost=%s)\n\n", client.TeamID(), client.TeamSubdomain())
	}
	return nil
}
```

Note: `client.TeamSubdomain()` already exists on `*slackclient.Client`. If it does not, replace that field with `client.TeamID()` only. Verify before finalizing.

- [ ] **Step 2: Build + vet**

Run: `go build ./cmd/slk/ && go vet ./cmd/slk/`
Expected: clean. (`slackclient.MintDiag` is now unreferenced.)

- [ ] **Step 3: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(diagnostic): --dump-mint reports localConfig_v2 token + connect"
```

---

## Task 6: Delete the dead network-mint code

**Files:**
- Delete: `internal/slack/mint.go`, `internal/slack/mint_test.go`

- [ ] **Step 1: Confirm nothing references the mint symbols**

Run: `grep -rn "MintToken\|MintDiag\|mintTokenAt\|newMintRequest\|parseRetryAfter\|apiTokenRE" --include=*.go . | grep -v internal/slack/mint`
Expected: no matches (empty output).

- [ ] **Step 2: Delete the files**

```bash
git rm internal/slack/mint.go internal/slack/mint_test.go
```

- [ ] **Step 3: Build whole module + vet + slack package tests**

Run: `go build ./... && go vet ./... && go test ./internal/slack/`
Expected: build clean, vet clean, tests pass.

- [ ] **Step 4: Tidy deps** (the network-mint may have been the only thing pinning nothing extra, but run tidy to drop anything now-unused):

Run: `go mod tidy`

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(slack): remove network token-mint (superseded by localConfig_v2)"
```

---

## Task 7: Full sweep + manual verification

- [ ] **Step 1: Cross-compile + vet + full tests**

Run:
```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
go vet ./...
go test ./...
```
Expected: all succeed; no failures.

- [ ] **Step 2: Manual (Linux maintainer machine, in the graphical session so the keyring is unlocked)**

- `go build -o /tmp/slk-lc ./cmd/slk`
- `/tmp/slk-lc --dump-mint` → confirm each team shows `token in localConfig_v2: true` and `connect: OK`.
- In an isolated sandbox: `XDG_DATA_HOME=/tmp/slk-lc-sb/data XDG_CONFIG_HOME=/tmp/slk-lc-sb/config XDG_CACHE_HOME=/tmp/slk-lc-sb/cache /tmp/slk-lc --add-workspace` → multi-select, confirm workspaces onboard and connect with no network mint; launch the TUI; relaunch to exercise refresh.

- [ ] **Step 3: Commit any fixes; the branch is ready for PR (referencing #111). Ask larsks to confirm Enterprise Grid.**
