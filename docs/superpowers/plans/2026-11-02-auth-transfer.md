# Auth Export / Import Implementation Plan

**Status:** Proposed; no implementation changes yet.

**Goal:** Move a single workspace login between machines without installing the
Slack desktop app or repeating browser credential extraction on the destination.
Transfer the complete login: the `xoxc-` access token **and** the `d` cookie.

## 1. Proposed user experience

```sh
# Source machine
slk auth export --workspace acme --output ./acme-auth.json

# Transfer over SSH (replace destination with your SSH host)
scp ./acme-auth.json destination:acme-auth.json

# Destination machine
slk auth import --input ./acme-auth.json
slk workspace whoami --workspace T0123ABCD
```

Both commands require explicit file paths. Export follows the existing workspace
selection rules: `--workspace`, then `SLK_WORKSPACE`, then the configured default
or only workspace. Import works on a fresh installation with no configured
workspaces. It takes its workspace from the file, not from destination defaults.

If the destination already has that workspace, import refuses without changing
anything. The user can deliberately replace its login for the **same account**:

```sh
slk auth import --input ./acme-auth.json --replace
```

Success output contains only non-secret metadata. Help and export's success text
warn that the file is a plaintext login credential. After verifying the import,
delete the transfer files on both machines; deletion is not guaranteed secure
erasure on SSDs or backed-up filesystems.

### Scope decisions

- One workspace per file; no `--all` or partial multi-workspace transactions.
- Files only in v1. No raw token arguments, secret environment variables,
  clipboard integration, terminal credential output, stdin/stdout payloads, or
  implicit default output path. Reject `--input -` / `--output -` with a clear
  usage error. A later stream mode needs its own explicit secret-output contract.
- Plaintext JSON with private-file handling, not base64 presented as security.
  Transfer only over SSH or another encrypted channel. Users needing encrypted
  storage can encrypt the file with their existing tools. Built-in encryption
  and passphrase handling are out of scope, not silently implied.
- Export is an offline snapshot of the stored login. It does not refresh, mint,
  call Slack, read the desktop app/keychain, or promise the login is still valid.
- Import checks Slack before saving. No offline or `--skip-validation` mode.
- Import neither moves nor revokes the original login. Both machines share its
  lifetime; expiry, revocation and workspace policy can invalidate both copies.
- No config, workspace slug/default, cache, drafts, or marks are transferred.
  Do not launch onboarding or change `config.toml`. Restart a running TUI to
  reliably use a newly imported workspace/login; there is no new live reload.

## 2. Existing code to reuse

| Concern | Existing code | Implication |
|---|---|---|
| Credential record and persistence | `internal/slack/auth.go`: `Token`, `TokenStore` | Both `AccessToken` and `Cookie` are required. Do not expose the store's JSON format as the portable contract. |
| Atomic store writes and cross-process coordination | `TokenStore.SaveIf`, `withLock`, `write`, `lockTokenDir` | Preserve compare-and-save semantics while import performs a network check. Never use an unconditional `Save` for import. |
| Workspace resolution | `internal/cli/session.go`: `SelectWorkspace`, `Invocation.Session` | Extract the local selection part so export can reuse it without connecting. |
| CLI table, flags, output and errors | `internal/cli/registry.go`, `cli.go`, `output.go`, `errors.go` | Add an `auth` noun and two command rows. Keep the one-object `--json` contract. |
| Composition boundary | `cmd/slk/cli.go`: `newCLIEnv` | CLI commands reach files, token storage and Slack only through injected `cli.Env` operations. |
| Checking the supplied login | `cmd/slk/login.go`: `identifyLogin` | Reuse the context-bounded `auth.test` check directly, not `connectWithRemint` or a fallback desktop login. |
| Session source / refresh | `cmd/slk/remint.go`, `onboarding_core.go` | Imported sessions must be protected from destination desktop credentials, just like browser sessions. |
| File replacement semantics | `internal/atomicfile/atomicfile.go`: `Write` | It preserves existing permissions and replaces targets; it is not a no-clobber export primitive. Do not change its default behavior. |
| Existing test fixtures | `runCLI`, `compareGoldenJSON`, `mustSave`, remint fakes and login-server helpers | Extend these rather than adding another general test harness. |

The token directory is `filepath.Join(xdgData(), "tokens")`, normally
`~/.local/share/slk/tokens/`, respecting `XDG_DATA_HOME`. Never hard-code that
path in transfer commands.

## 3. Portable format and input validation

Use a dedicated versioned transfer document, separate from `Token`:

```json
{
  "format": "slk-auth",
  "version": 1,
  "workspace": {
    "team_id": "T0123ABCD",
    "name": "Example Inc",
    "domain": "example"
  },
  "credentials": {
    "access_token": "xoxc-FAKE-EXAMPLE",
    "cookie": "FAKE-COOKIE-EXAMPLE"
  }
}
```

These are deliberately fake credentials. Never use real captures in fixtures.
Do not export `Source`: the destination chooses the refresh policy. No timestamp,
checksum, machine-specific paths or arbitrary token-store fields are needed.

Add the codec beside authentication storage, preferably
`internal/slack/auth_transfer.go`, with tests. The import adapter owns filesystem
reads; the codec accepts bounded bytes or a reader, not arbitrary paths.

Validation before any store write:

1. Limit input to 64 KiB, reading at most limit + 1; reject oversized documents.
2. Require one JSON object and EOF after it (whitespace allowed). Reject unknown
   fields, unsupported format/version, duplicate keys and trailing documents.
   Implement duplicate-key detection only in this small codec; first search for
   an existing JSON validator before adding one. Do not let decoder errors quote
   untrusted field names or document contents into diagnostics.
3. Require a nonempty `xoxc-` token and cookie, with sensible bounded lengths and
   no whitespace/control characters. Preserve the cookie's existing encoding;
   do not unescape, trim, or re-encode either secret.
4. Require a conservative Slack workspace ID shape (`T` followed by uppercase
   ASCII letters/digits), checked before constructing any token-store path.
   Test traversal attempts, separators, absolute paths and control characters.
5. Bound workspace display metadata and reject terminal control characters.
   Domain is optional; when present require a subdomain label, not a URL or host
   path. Metadata is a label, never a destination for sending credentials.
6. Call `identifyLogin(ctx, candidate)` on a client created from exactly this
   token/cookie. Require a nonempty user ID and a returned team ID matching the
   document. An auth refusal, mismatch, timeout or network error saves nothing.

`auth.test` verifies the login identity, not every downstream operation or the
independent validity of the cookie. A manual check must cover API, WebSocket and
file access on the destination. Do not claim cryptographic authenticity for the
file: anyone can edit it, and `auth.test` is the identity authority at import.

Error messages never contain the payload, tokens, cookies, raw parser input or
request bodies. Boundary errors are allowlisted/sanitized before CLI output;
retain typed cancellation and Slack refusal classification without forwarding
arbitrary sensitive error strings. Test with distinctive fake secret markers.

## 4. Imported-session policy

Add `TokenSourceImported = "imported"`. Every successful import sets this source,
regardless of whether the source machine used a desktop or browser login.

Centralize the demonstrated common decision as `Token.UsesDesktopLogin()`:
only the legacy empty source allows desktop refresh; browser, imported and
unknown nonempty sources do not. Existing stored records need no migration.
Register this reusable method in `AGENTS.md` when implementing it.

Use the method in:

- `remintTokenWith`: return before any desktop/keychain read for protected tokens.
- `remintTokens`: when no token uses the desktop login, return before reading
  its cookie or tokens; for a mixed batch, refresh only desktop-backed entries.
- `desktopChoices`: browser/imported sessions are not preselected for replacement.
  Label imported entries as an imported session and make replacement deliberate.

The existing `desktopLogin` and `loginRefresher` paths then inherit the single-
token rule. Keep accepting a newer stored login through the shared refresher;
only the fallback to this machine's desktop app is prohibited. An expired
imported login requires another export/import or an explicit sign-in.

Also make the eager startup saves in the TUI and `slk export` conditional on
still holding the token they refreshed. Today they pass unconditional `Save`:
startup can read a desktop token, an import can replace it, and the delayed
startup refresh can then overwrite the imported login and source. Snapshot each
original token, save refreshes with `SaveIf`, and on a failed comparison reread
the store before connecting (respect deletion; do not connect the stale result).
Use a bounded retry/conflict result rather than an unbounded loop under churn.
Test this ordering explicitly in both startup paths. This is required for import
safety, not a general refresh refactor.

## 5. Persistence and filesystem safety

### Export

- Load the selected stored token once and validate its structural fields before
  encoding. Copy nothing else from the token directory.
- Require an existing parent directory. Open the requested destination with
  `O_CREATE | O_EXCL | O_WRONLY`, mode `0600`; never truncate or overwrite it,
  including an existing symlink. No export `--force` flag in v1.
- Write fully, sync, and close; remove only the file this invocation created on
  a write/sync/close failure. Never remove a pre-existing target.
- A new exclusive file may be incomplete while export is still running. Transfer
  it only after successful completion; a crash can leave a partial file, which
  import must reject. This avoids inventing a cross-platform atomic no-replace
  rename helper just for this command.
- POSIX tests assert owner-only permissions. On Windows, mode bits are not an
  ACL guarantee: document reliance on a private user directory and inherited ACLs.

### Import

- Open a regular input file, enforce the size limit, and close it on every path.
  On POSIX refuse group/world-accessible input with an actionable instruction to
  restrict its permissions; do not silently chmod someone else's file.
- After decoding the team ID, snapshot the destination entry **before** the
  network check. Without `--replace`, an existing entry is a conflict even if
  the credentials appear identical. Corrupt/unreadable entries are errors, not
  absence, and are never automatically replaced.
- Add a narrow `TokenStore.ImportIf(candidate Token, expected *Token)
  (bool, error)` using `withLock` and `write`. A nil expectation means the path
  must still be absent; otherwise require the exact snapshot, as `SaveIf` does.
  A false result means a concurrent change; report conflict and never retry
  against a newer snapshot automatically. Any corrupt existing entry is an
  error, never absence. Register this helper in `AGENTS.md`. Keep `SaveIf`'s
  existing semantics; reuse its lock/write substrate rather than calling
  `Load` then unconditional `Save`.
- At snapshot time and again inside the guarded import write, refuse nonregular
  or symlink token targets and POSIX targets with group/world permissions.
  A replacement must not publish the imported secret through a permissive token
  file: `atomicfile.Write` deliberately preserves existing permissions.
- Validate the token directory before reading a snapshot or opening `.lock`:
  reject a symlink directory, unexpected owner, or POSIX group/world-writable
  directory. Require trusted parent directories for token storage and export;
  inspect existing writable ancestors up to the user-owned private boundary.
  Refuse an unsafe or symlink lock file too. Do not silently repair permissions.
  These checks protect against other local users, not a malicious process
  running as the same user. Windows relies on private-user-directory ACLs;
  document that boundary instead of claiming mode bits secure it.
- Keep the lock only around local comparison/write, never the Slack request.
  Recheck context cancellation before committing. Success leaves unrelated
  workspace files, config and cache untouched.

`--replace` must not switch accounts: persisted messages and marks are scoped
by workspace, not by signed-in user, so restarting alone does not isolate the
previous account's cached private content. Add optional `UserID` (`user_id`) to
the internal `Token` record and populate it from the verified import response.
Do not trust an identity supplied by the transfer file. Audit every writer that
changes the token/cookie pair: it must set UserID from an identity check of the
new pair, or clear it, never carry forward the previous pair's verified identity.
Add a regression test for desktop refresh changing the account. For replacement,
compare against the stored verified UserID; for a legacy record without one, identify
that exact existing login with Slack before allowing replacement. If it is
expired or cannot be identified, fail closed with recovery guidance rather than
assuming the new account is the same. Another account requires a separate
`XDG_DATA_HOME` profile, or a future account-isolation feature; `--replace` is
not an override. Existing cache left after removing a workspace is a pre-existing
account-isolation limitation, not something this credential-only feature fixes.

Success reports the verified user ID and asks for a restart. Existing same-
account reconnect checks remain intact.

## 6. CLI integration and output contract

### `internal/cli`

Add two registry rows and the `auth` noun:

- `auth export`: required `--output`; existing workspace selection; no Slack call.
- `auth import`: required `--input`; optional `--replace`; reject explicit
  `--workspace` as a usage error rather than silently overriding the file.
  `SLK_WORKSPACE` and configured defaults do not affect import.

Extract local workspace selection from `Invocation.Session` into a shared method
used by session and export. Preserve existing precedence, errors, and tests.

Extend `Env` with narrow operations such as:

- `ExportAuth(ctx, workspace, outputPath) (AuthExportResult, error)`
- `ImportAuth(ctx, inputPath, replace) (AuthImportResult, error)`

Return only non-secret DTOs. Credentials never enter the generic printer or
`Session`, and commands do no filesystem/network work themselves. Wire these
operations in `cmd/slk/cli.go`; keep transfer orchestration in a focused
`cmd/slk/auth_transfer.go` with injected checks for tests. No UI/core service
changes are needed.

Pin JSON results with goldens:

```json
{"workspace":{"team_id":"T0123ABCD","name":"Example Inc","domain":"example"},"output":"./acme-auth.json","plaintext":true}
```

```json
{"workspace":{"team_id":"T0123ABCD","name":"Example Inc","domain":"example"},"user":{"id":"U0123ABCD"},"replaced":false}
```

Without `--json`, print a short export warning/path or import workspace/user
confirmation and restart guidance. With `--json`, print exactly one metadata
object on stdout, no extra warning prose. The `plaintext` field and help carry
the warning. No format mode ever prints the secrets.

Add stable errors to `errors.go`, `errorExits`, and `SKILL.md`:

| Code | Exit | Meaning |
|---|---|---|
| `invalid_auth_bundle` | 2 | Invalid, oversized, unsupported or identity-mismatched document. |
| `auth_conflict` | 1 | Existing workspace without `--replace`, another/unverifiable existing account, or destination changed during validation. |
| `unsafe_auth_file` | 1 | Unsafe input/destination type or POSIX permissions. |

Use existing `usage` for missing/inapplicable flags, `auth_failed` (3) for Slack
refusing the supplied login, `cancelled` (1) for cancellation, and existing
failure codes for network/I/O errors. Do not relabel every validation problem as
an expired token. File read failures must not reveal document content.

## 7. Implementation sequence (tests first)

### Task 1 — Portable codec

- Add failing round-trip and validation table tests beside auth storage.
- Cover both required secrets, exact cookie preservation, metadata rules, every
  rejected JSON form above, traversal IDs, and secret-free diagnostics.
- Implement the dedicated versioned codec with no new dependencies.

### Task 2 — Protected imported sessions

- Add tests using existing remint fakes proving browser/imported/unknown-source
  single tokens and all-protected batches perform zero desktop/keychain reads.
- Test mixed batches and the desktop picker: imported entries remain unchanged
  and unselected; ordinary desktop entries retain existing behavior.
- Add the source constant and shared policy method, then update the callers.
- Characterize and fix stale eager-refresh saves in both the TUI and channel
  export startup paths: import winning the save race must remain authoritative.

### Task 3 — Safe store and file operations

- Extend token-store tests for `ImportIf` with absence and guarded replacement.
  Race two imports; exactly one succeeds when both expected absence/the same
  existing record. Exercise deletion and refresh during the check.
- Test corrupt files, symlinks, permissive files/directories, unsafe lock files,
  lock serialization and write failures without losing an old login or changing
  unrelated files.
- Test exclusive export creation, full bytes, no-clobber, POSIX mode, cleanup
  after failure, missing parent and symlink targets.
- Implement only the missing persistence primitives; reuse the lock/write code.

### Task 4 — Transfer orchestration

- Use a temporary real token store and injected `identifyLogin`-shaped check.
- Prove export is offline and does not refresh the stored pair.
- Prove fresh-store import works, validates exactly the supplied pair once, sets
  `SourceImported`, and saves only after a matching team/nonempty user response.
- Cover expired/revoked tokens, mismatch, network failure, timeout, cancellation,
  explicit same-account replacement, rejected other-account replacement,
  unverifiable legacy identities, and a concurrent save/delete during the check.
- Exercise actual client wiring with the existing fake Slack login server;
  make no real Slack requests in automated tests.
- Assert fake secrets never appear in errors, command output or captured debug
  logs. No unredacted credential structs in test failure formatting.

### Task 5 — Commands, help and contract tests

- Extend `fakeEnv` / `runCLI` with fake transfer operations rather than bypassing
  the CLI. Add tests before the command handlers.
- Cover required flags, explicit import workspace rejection, ignored import
  defaults, export selection precedence/ambiguity, help, interspersed flags,
  normal/error `--json` behavior and failing stdout.
- Add success/error JSON goldens and registry/SKILL coverage. Ensure `cli.Handles`
  routes the new noun without adding a parallel switch in `cmd/slk/main.go`.
- If output fails after a successful file/store write, report the output error;
  do not roll back a completed credential operation. Document safe rerun behavior
  (export refuses an existing file; import may require deliberate replacement).

### Task 6 — Documentation and validation

- Update `wiki/Setup.md` with the two-machine flow, plaintext warning, SSH
  transfer, shared revocation, cleanup, imported-session expiry and replacement.
- Add a brief pointer under README Setup; document commands/errors and the rule
  that agents must not export credentials without explicit user authorization in
  `internal/cli/SKILL.md`.
- Update shared-helper entries in `AGENTS.md`; leave existing storage/config
  formats backward-compatible, with no migration or new dependencies.
- Run package tests while iterating, then the full validation below.

## 8. Validation and acceptance criteria

```sh
go test ./internal/slack ./internal/cli ./cmd/slk
go build ./...
go vet ./...
go test ./... -race
gofmt -l .
golangci-lint run
```

`gofmt -l .` must be empty. Run Windows/Linux build/test coverage through the
existing CI, especially exclusive file creation, store locks and permission
handling. Do not interpret Windows mode-bit tests as ACL enforcement.

Manual acceptance on two machines:

1. Export a workspace; inspect only mode/size, not its secret contents. Confirm
   normal and JSON stdout contain no credentials.
2. Transfer via SSH and import into an empty token directory on a machine without
   Slack desktop or a usable keychain. `workspace whoami`, channel loading,
   WebSocket updates and an authenticated file download should work.
3. Repeat for an Enterprise Grid workspace to verify the preserved workspace
   subdomain and the actual `auth.test` team ID agree.
4. Put a different desktop account for the same workspace on the destination.
   Restart slk: the imported account must remain selected without keychain reads.
   Adding another desktop workspace must not preselect the imported one.
5. Confirm existing-workspace import refuses by default, same-account
   `--replace` reports the verified account, and another-account replacement
   refuses. Restart a running TUI after replacement.
6. Revoke the source session and verify the destination does not silently switch
   to desktop credentials. Re-export/import a fresh session to recover.
7. Delete transfer files on both machines. Confirm local configuration/cache was
   not exported or changed.

**Done means:** a documented two-machine transfer works without exposing secrets
in CLI output, import validates the exact supplied login before saving, concurrent
changes are not overwritten, and the destination never silently substitutes its
own desktop account.
