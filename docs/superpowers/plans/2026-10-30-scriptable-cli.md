# Scriptable CLI Implementation Plan

> **Tracks:** [gammons/slk#107](https://github.com/gammons/slk/issues/107) —
> scriptable CLI commands with TUI parity, for AI-agent automation.
>
> **Status:** stage 1 implemented on `feat/issue_107_scriptable_CLI`, not yet
> merged. The [review notes](#review-notes-2026-10-30) list what the review
> changed in this plan.
>
> **Written against:** `main` at `02a96005` (2026-10-28). This plan names
> symbols, not line numbers, because files move. Run `git grep` on every
> symbol before you rely on it.
>
> **For agentic workers:** one stage is one branch and one PR (stage 9: one PR
> per item). Do the tasks of a stage in order. Each task is test-first: write
> the failing test, make it pass, then clean up. Steps use checkbox (`- [ ]`)
> syntax. When a stage lands, fill in its row in the [Status](#status) table.

**Goal:** Give slk non-interactive commands (`slk <noun> <verb>`) with a stable
JSON output, so that a script or an AI agent can read and write Slack through
the user's existing slk login, without the TUI.

**Architecture:** A new package, `internal/cli`, holds the command table,
argument parsing, output, errors and every command. Commands reach Slack
through interfaces that `*slackclient.Client` and `*edge.Client` satisfy. This
is the pattern `slk export` already uses (`exportSource`). One new file,
`cmd/slk/cli.go`, builds the environment (workspace token, clients, cache).
`main()` hands every first argument that is not a flag to `cli.Main`. The CLI
never starts the TUI and never writes the SQLite cache. It opens the WebSocket
only for `slk events` (stage 10).

**Tech stack:** Go standard library (`flag`, `encoding/json`,
`text/tabwriter`), the in-tree `internal/slack` client and `internal/cache`
(read only). No new module dependencies.

---

## Contents

0. [Review notes (2026-10-30)](#review-notes-2026-10-30)
1. [Background](#1-background)
2. [Scope](#2-scope)
3. [Design decisions](#3-design-decisions)
4. [Command catalog](#4-command-catalog)
5. [Stages](#5-stages)
6. [Tests, all stages](#6-tests-all-stages)
7. [Documentation, all stages](#7-documentation-all-stages)
8. [Risks](#8-risks)
9. [Open questions for the maintainer](#9-open-questions-for-the-maintainer)
10. [Follow-ups outside this plan](#10-follow-ups-outside-this-plan)
11. [Status](#status)

---

## Review notes (2026-10-30)

The review checked stage 1 against the code before the implementation. It
changed the plan as follows. The sections below already carry the changes.

**Rule: shared code lands with its first user.** Stage 1 is the common base
that every later command uses: the command table, dispatch, global flags,
help, output, errors and exit codes, workspace selection, sign-in with lazy
remint, atomic token saves, the signal context, the `SKILL.md` checks and the
golden JSON helper. A shared piece that no stage 1 command uses moves to the
stage of its first user, so that each PR shows the piece in use:

| Piece | Moved to | First user |
|---|---|---|
| `Command.Args`, and positionals that a command accepts (stage 1's `run` rejects every positional) | stage 2 | `channel get <channel>` |
| `withRateLimitRetry` | stage 2 | the first Slack call after `auth.test` |
| `Env.Cache`, `CacheReader` | stage 2 | the `Directory` |
| `SlackAPI`, `EdgeAPI`, `Session.Slack`, `Session.Edge` | stage 2 | `channel list` |
| `ambiguous_reference` | stage 2 | a name with two matches |
| `Env.Now`; "message text is data" in `SKILL.md` | stage 3 | time bounds; the first command that reads messages |
| `Command.Effect`, `--dry-run`, the read-only switch, exit code 5, the `warnings` array, `Env.Stdin` | stage 4 | the first write; unresolved mentions; `--text -` |

**Changes to decisions**

- **D4, default workspace.** `[general] default_workspace` already exists:
  the TUI opens that workspace first. The CLI uses the same setting. There is
  no `[cli] default_workspace`, and no `slk workspace use` command, which
  would only have written a second copy of that setting.
- **D4, environment credentials** (`SLK_TOKEN`, `SLK_COOKIE`) wait for a user
  who needs them (open question 4). The token store covers every machine
  where `slk --add-workspace` ran.
- **D2, global flags** can also come before the command words
  (`slk --json workspace list`). `main()` gives the CLI a first argument that
  is a global flag. Without this, `slk --workspace acme channel list` would
  start the TUI.
- **D12.** The command block of `slk --help` is generated from the table
  (`cli.Overview`), so it cannot miss a noun. `TestHelpListsEveryNoun` is not
  needed.
- **`slk --list-workspaces`** runs `slk workspace list`. The output has the
  same bytes. The second implementation of that table is gone.
- **`slk version --json`** is not planned until someone needs it. The text
  of `slk --version` does not change.
- **Auth errors.** One check, `slackclient.IsAuthError`, decides both when to
  remint and when to exit with code 3.

**Code review of stage 1 (2026-10-30).** Four findings, all confirmed and
fixed:

1. A refused browser sign-in made `remintToken` read the desktop app's
   cookie and tokens (the OS keychain) before `remintTokens` skipped the
   token. `remintToken` now returns a browser sign-in at once. The docs no
   longer say that commands never prompt: they never ask for input, but the
   OS can ask to allow keychain access while an expired login is refreshed.
2. `slk help` dropped every argument that starts with `-`, so the value of
   `--workspace acme` became a topic word, and unknown flags passed. Help now
   parses its arguments with `parseInterspersed`, like a command.
3. The JSON error format was chosen by a scan that did not know which flags
   take a value, and an invalid `--json=x` turned JSON off. `jsonRequested`
   now reads the arguments the way the flag set parses them, and an invalid
   value changes nothing. `runCLI` checks the JSON contract from a literal
   `--json`, not through that helper.
4. Help ignored write errors and exited 0. Help texts are now built in memory
   and written once, and a failed write exits 1.

**Second code review of stage 1 (2026-10-30).** Three parsing findings, all
confirmed and fixed with one change: a single scanner, `splitArgs`, now reads
every command line, and `parseInterspersed` and `jsonRequested` both use it.

1. `slk help` deleted help flags before it parsed, so in
   `--workspace --help` the value was lost. Help now declares `-h` and
   `--help` as ordinary flags, so a value stays a value.
2. `parseInterspersed` read flags again after the argument that followed
   `--`, while `jsonRequested` stopped at `--`. Now `--` makes every later
   argument positional in both.
3. After a bare noun (`slk workspace --help`), a help-looking word anywhere
   gave the noun's help, also as a flag's value or after an unknown flag.
   That path now parses its arguments like a command, so only a real help
   flag gives the help, and an unknown flag before it is a usage error. A
   verb after a flag (`slk workspace --json list`) gets a message that flags
   go before or after the command words.

A command's `run` parses with `parseInterspersed` too, so all paths read
`--` and flag values alike.

**Third code review of stage 1 (2026-10-30).** One finding, confirmed and
fixed: `splitLeadingFlags` keeps a leading `--` in the leading flags, and
`Main` handed those to `run` only. Help and a bare noun therefore read flags
again after the `--` (`slk --json -- help workspace --help` printed help).
`Main` now puts the leading flags in front of the arguments of every route.
On the same boundary, `cli.Handles` left a line that starts with `--` to the
TUI (`slk -- workspace list`). A `--` with arguments after it now goes to the
CLI; `slk --` alone still starts the TUI.

**Found during the review, not changed** (raise separately):

- `Client.Connect` calls `AuthTest()` without the context. Ctrl-C does not
  cancel a sign-in that hangs. A second Ctrl-C still ends the process.
- `TestWriteConfigAtomic_PreservesMode` (`cmd/slk/save_atomic_test.go`) fails
  under umask 077: `os.WriteFile` seeds the file through the umask. CI uses
  umask 022.
- `TestWriteMacOSClipboard/exact_stdin` (`cmd/slk/clipboard_write_test.go`) can
  fail on macOS under the load of a full `go test ./...`: it gives a new fake
  `pbcopy` script 2 seconds, and macOS can take longer to start a new script.

## 1. Background

- **The request.** #107 asks for plain commands with `--json` output, no
  prompts, stable IDs, meaningful exit codes, and auth that works without the
  TUI. A collaborator's comment suggests that slk reuse the command surface of
  Slack's own CLI.
- **Why not `slackapi/slack-cli`.** That tool builds Slack apps (`create`,
  `deploy`, `triggers`, `datastore`, `manifest`). Its one general command,
  `slack api <method>`, sends a token from an app install (`--token`,
  `SLACK_BOT_TOKEN`, `SLACK_USER_TOKEN`) and sends no `d` cookie. It therefore
  cannot use slk's browser-session login (an `xoxc` token plus the `d`
  cookie). It also has no high-level verbs and no name resolution. We borrow
  its conventions, not its code:
  - the noun–verb command tree;
  - a switch for machine-readable output (`--output json` there, `--json`
    here);
  - `slack skill`, which prints an agent skill in the Agent Skills format,
    becomes `slk skill`;
  - `slack api` becomes `slk api` (stage 10).
- **Why not ACP.** The Agent Client Protocol connects an editor (the client)
  to a coding agent. It does not give an agent access to Slack. If agents
  must call slk as typed tools, the matching protocol is MCP. An MCP server
  can wrap this command table later (§10).
- **The precedent.** `slk export` (`cmd/slk/export_command.go`) already runs
  without the TUI. Its path is: token store → `selectExportToken` →
  `remintTokens` → `slackclient.NewClient` → `Connect` → client calls, with a
  context that Ctrl-C cancels. This plan makes that path general.

## 2. Scope

**In scope**

- Every TUI action that reads or changes Slack state (catalog groups A–K).
- Slack features that the TUI does not have but that scripts often need. These
  come one at a time, on demand (stage 9).
- A real-time event stream, for agents that wait for a reply (stage 10).

**Out of scope**

- TUI layout and local state: themes, sidebar width, windows, marks,
  back/forward history, image preview, clipboard copy, `$EDITOR` drafts,
  desktop notifications, the status-command hook and the typing indicator.
  These have no meaning without a screen, or the user edits `config.toml`.
- Project non-goals (`wiki/Tradeoffs-and-Non-Goals.md`): huddles, Slack
  Connect, Workflow Builder, bot and app management, custom emoji management,
  and editing unfurls.
- Pressing buttons on ephemeral messages (`chat.attachmentAction`). An
  ephemeral message exists only on the WebSocket of the session that received
  it.
- Interactive onboarding. `--add-workspace` and `--remove-workspace` prompt the
  user, and they stay as they are.

## 3. Design decisions

Each decision has a short reason. Decisions marked **(confirm)** are repeated
in §9 for the maintainer.

### D1. Command shape (confirm)

- The form is `slk <noun> <verb> [target] [flags]`. Nouns are singular.
- The thing that the verb acts on is positional. Everything else is a flag.
  When the target can be one of two kinds, both kinds are flags (`message send
  --channel …` or `message send --user …`).
- Standard verbs: `list` (many items), `get` (one item, by reference), `find`
  (server-side text search), `send`, `reply`, `edit`, `delete`, `add`,
  `remove`, `set`, `clear`, `join`, `open`, `mark-read`, `mark-unread`.
- Shortcuts come from #107 only: `slk send` is `slk message send`, and
  `slk read` is `slk message list`. `slk search` is a command by itself
  (message search). There are no other aliases.
- The existing entry points keep their behavior and output: `export`,
  `version`/`--version`, `help`/`--help`, `--list-workspaces`,
  `--add-workspace`, `--remove-workspace`, `--dump-sections`, `--dump-prefs`
  and `--demo`.
- **Behavior change.** Today `slk <unknown-word>` ignores the word and starts
  the TUI. After stage 1, a first argument that does not start with `-` and is
  not a command exits with code 2 and a usage message. An agent that mistypes
  a command must not start a full-screen TUI that waits forever. A first
  argument that is a global flag (`--json`, `--workspace`) also goes to the
  CLI, and so does a `--` with arguments after it (they are all words, and the
  TUI takes none). Other unknown flags still start the TUI, as today.

### D2. Parsing: the standard library and a command table (confirm)

- Each command gets its own `flag.FlagSet`, as `parseExportArgs` does.
- Commands are rows in one table (`internal/cli/registry.go`). This is the
  convention of `modeHandlers`, `commands` (the `:` commands) and
  `slashCommands`. The table drives dispatch, help, the documentation checks
  (D12) and shell completion (stage 10).
- Flags can come after positionals (`slk reaction add <ref> thumbsup --json`).
  The `flag` package stops at the first positional, so one helper,
  `parseInterspersed(fs, args)`, separates flags from positionals first and
  then parses the flags. A flag's value is never a positional, whatever it
  looks like, and `--` makes every later argument positional. Stage 1 adds it,
  and every path parses with it: a command's `run`, `slk help` and the
  arguments after a bare noun. `jsonRequested` reads the arguments through
  the same scanner (`splitArgs`), so the error format always matches the
  parse. In stage 1, a command's positional is a usage error; stage 2 lets a
  command accept positionals.
- Global flags: `--workspace <ref>` and `--json`. The dispatcher registers them
  into every command's FlagSet. They can go before the command words or after
  them: `splitLeadingFlags` moves the leading ones behind the words, so that
  one parse sees them all.
- Rejected alternative: `spf13/cobra`. It adds two dependencies and a second
  command style next to `slk export`. The table gives dispatch, help,
  completion and the documentation checks in a few hundred lines. Revisit this
  if the hand-written help or completion becomes a maintenance cost.

### D3. Package layout

```
internal/cli/
  cli.go            Main(ctx, args, env) int, Handles: dispatch, global flags, help routing
  registry.go       Command, the table, the noun summaries, Overview and the other help texts
  args.go           parseInterspersed; readText (--text / --text-file / stdin)   (readText: stage 4)
  output.go         printer: one JSON document, or text; errors on stderr
  errors.go         Error, error codes, exit codes, errorFrom (Slack errors → codes)
  refs.go           pure parsers: channel / user / message references, time bounds   (stage 2)
  directory.go      Directory: names → IDs and IDs → names (cache first, then Slack)   (stage 2)
  session.go        Env, Workspace, Session, SelectWorkspace, Invocation.Session
  dto.go            the JSON contract types and their converters   (stage 2)
  skill.go          //go:embed SKILL.md, and the `slk skill` command
  SKILL.md          the agent skill (Agent Skills format)
  cmd_<noun>.go     one file per noun: cmd_workspace.go, cmd_channel.go, …
  testdata/json/    golden JSON documents, one per command and case
cmd/slk/cli.go      cliMain, newCLIEnv(): token store, config, lazy remint
                    (connectWithRemint); later the clients, the cache and
                    compile-time assertions that the real clients satisfy
                    the CLI's interfaces
cmd/slk/signal.go   signalContext, shared with slk export
```

Why a new package and not more files in `cmd/slk`:

- `cmd/slk` is the composition root, and the refactor plan makes it smaller
  (Phase 2). Command logic belongs where tests can reach it without the root's
  wiring.
- `internal/cli` cannot reach `rtmEventHandler`, the workspace router or other
  TUI state by accident.
- The seam is explicit: `Env` is everything that the CLI gets from outside.

The interfaces. Stage 1 has the fields without a stage mark; later stages add
the marked ones and the methods they need:

```go
// Env is everything the CLI receives from the composition root.
// Tests build it from fakes; cmd/slk/cli.go builds the real one.
type Env struct {
	Stdin          io.Reader             // stage 4 (--text -)
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Now            func() time.Time      // stage 3 (time bounds)
	// Workspaces lists the configured workspaces in rail order
	// (config.OrderTokens), the [general] default_workspace one marked
	// Default.
	Workspaces func() ([]Workspace, error)
	// Connect signs in: auth.test, plus one remint and one retry when
	// Slack refuses the stored login (D4).
	Connect func(ctx context.Context, ws Workspace) (*Session, error)
	// Cache returns a read-only view of the SQLite cache, or nil when the
	// cache cannot be opened; every lookup then goes to Slack (D8).
	Cache func() CacheReader             // stage 2
}

// Session is one signed-in workspace.
type Session struct {
	Workspace Workspace // as `workspace list` shows it; auth.test fills only empty fields
	UserID    string    // from auth.test
	UserName  string    // the handle, from auth.test
	Slack     SlackAPI  // stage 2
	Edge      EdgeAPI   // stage 2
}

// SlackAPI is the union of the per-noun interfaces (workspaceAPI,
// channelAPI, messageAPI, …). Each one is declared in its cmd_<noun>.go
// file, next to the code that calls it. cmd/slk/cli.go asserts
//
//	var _ cli.SlackAPI = (*slackclient.Client)(nil)
type SlackAPI interface {
	workspaceAPI
	// channelAPI, messageAPI, … are added by later stages.
}
```

A command row. Stage 1 has `Path`, `Summary` and `Setup`:

```go
// Command is one row of the CLI table.
type Command struct {
	Path    []string // {"message", "send"}
	Args    string   // stage 2: positional synopsis for help, e.g. "<message> <emoji>"
	Summary string   // one line, shown in help
	Effect  Effect   // stage 4: Read, Write or Destructive (D9)
	// Setup declares the command's own flags on fs and returns the function
	// that runs the command. The closure reads the parsed flag values.
	Setup func(fs *flag.FlagSet) runFunc
	// RawArgs (stage 3) hands the arguments over unparsed. Only `export`
	// uses it, because it keeps its own parser.
	RawArgs bool
}
```

A one-word path (`skill`) is a command on its own. A longer path belongs to the
noun of its first word, and `nouns` gives each noun its line in help.

`Invocation` carries the printer, the `Env`, the global flags and a lazy
`Session(ctx)`; stage 2 adds the parsed positionals. Commands that need no
Slack call (`help`, `skill`, `workspace list`) never sign in.

### D4. Workspace, credentials and token refresh (confirm: environment credentials)

**Which workspace.** The first match wins:

1. `--workspace <ref>`
2. `SLK_WORKSPACE`
3. the subdomain of a permalink argument, matched against the token's
   `Domain` (stage 3)
4. `[general] default_workspace` in `config.toml`: the setting that already
   picks the TUI's first workspace (`Config.TeamIDForDefaultWorkspace`). A
   value that names no workspace is ignored, as the TUI ignores it.
5. the only configured workspace

If several workspaces are configured and nothing chooses one, the CLI exits
with code 2 (`workspace_required`) and lists them. A `<ref>` that matches
nothing exits with code 4 (`not_found`) and lists them. A `<ref>` matches the
slug, team ID, team name or domain, case-insensitively.
`cli.SelectWorkspace` holds that rule, and `selectExportToken` is a thin
adapter over it, so `slk export` and the CLI cannot drift apart. Export marks
no workspace as the default, so it keeps its old behavior.

**Credentials.** The CLI uses the token store that `--add-workspace` fills
(`slackclient.NewTokenStore(<data>/tokens)`). There is no new login flow. With
no workspace configured, the CLI exits with code 3 and tells the user to run
`slk --add-workspace`. It never starts onboarding, which `run()` does for the
TUI.

**Environment credentials** (not implemented; on demand, open question 4), for
machines without a token store (CI, containers): `SLK_TOKEN` (`xoxc-…`) and
`SLK_COOKIE` (the `d` cookie value). Both would be required. The CLI would call
`auth.test` to learn the team, and never save or remint such a token.

**Lazy remint.** The CLI signs in with the stored token first. Only when Slack
refuses the login (`slackclient.IsAuthError`: `invalid_auth`, `not_authed`,
`token_expired`, `token_revoked`, `account_inactive`) does
`connectWithRemint` call `remintToken` for that one token, and then sign in
once more. A remint that returns the same token (a browser sign-in, or a
failed refresh) is not retried. The reason: a remint reads the desktop app's
cookie database and the OS keychain every time. That is slow on every command
and can show a keychain prompt, and an agent runs many commands. `slk export`
keeps its eager remint.

**Atomic token save.** `TokenStore.Save` used `os.WriteFile`, and
`TokenStore.List` skips a file it cannot parse. A TUI that starts while
another slk process saves a reminted token could therefore start without
that workspace. Stage 1 moves `writeConfigAtomic` (`cmd/slk/save_theme.go`)
to a leaf package, `atomicfile.Write`, and `TokenStore.Save` uses it too.

### D5. Output contract

- **The default is text**, for people: aligned columns, like
  `--list-workspaces`. The text form is not a contract. It can change.
- **`--json` writes exactly one JSON document to stdout**, followed by a
  newline. Nothing else goes to stdout. Progress and warnings go to stderr.
  From stage 4, warnings also go into the document's `warnings` array.
- **Shapes.** The document is always an object with a named field:
  - one item: `{"channel": {…}}`
  - a list: `{"channels": […]}`. A paged list adds `"has_more"` and one resume
    field: `"next_cursor"`, or `"next_before"` for message history.
  - a write: the object that it created or changed, e.g. `{"message": {…}}`
  - a dry run: `{"dry_run": true, "request": {…}}`
- **Fields.** Names are snake_case, as in the Slack API. IDs are always present
  (`id`, `channel_id`, `user_id`, `ts`, `thread_ts`). Names are extra fields.
  They never replace IDs. Every Slack `ts` comes with a `time` field in RFC
  3339, UTC. Empty lists are `[]`, never `null`.
- **Stability.** We add fields. We never rename, remove or retype a field.
  Golden files in `internal/cli/testdata/json/` pin every shape, so a change to
  an existing field shows in review.

The core types. Later stages add more (File, Thread, Unread, ActivityItem,
SearchMatch):

```jsonc
// Message
{
  "channel_id": "C0123ABCD",
  "ts": "1761815600.000100",
  "time": "2026-10-30T09:13:20Z",
  "thread_ts": "",            // the parent ts, for a reply or for a parent with replies
  "user_id": "U0456EFGH",
  "user_name": "alice",       // resolved display name; "" when unknown
  "bot_id": "",
  "subtype": "",
  "text": "<@U0789IJKL> see <#C0123ABCD|general>",  // Slack mrkdwn, as Slack stores it
  "markdown": "@bob see #general",                 // CommonMark with names resolved (the slk export conversion)
  "edited": false,
  "reply_count": 0,
  "reactions": [{"name": "thumbsup", "count": 2, "user_ids": ["U1", "U2"]}],
  "files": [{"id": "F1", "name": "report.pdf", "mimetype": "application/pdf", "size": 48213}]
}

// Channel
{
  "id": "C0123ABCD",
  "name": "general",
  "type": "public",           // public | private | im | mpim
  "is_member": true,
  "is_archived": false,
  "topic": "",
  "purpose": "",
  "num_members": 42,
  "user_id": ""               // the other person, for type "im"
}

// User
{
  "id": "U0456EFGH",
  "name": "alice",
  "display_name": "Alice",
  "real_name": "Alice Doe",
  "title": "",
  "email": "",
  "tz": "Europe/Berlin",
  "is_bot": false,
  "deleted": false,
  "status_text": "",
  "status_emoji": "",
  "status_expiration": ""     // RFC 3339, or "" for none
}

// Workspace
{"team_id": "T0123", "slug": "acme", "name": "Acme", "domain": "acme", "default": true}
```

### D6. Errors and exit codes

| Exit | Meaning | Examples |
|---|---|---|
| 0 | Success | |
| 1 | Failure | a network error, Slack `fatal_error`, Ctrl-C |
| 2 | Usage error | an unknown command or flag, a missing target, `--workspace` is needed, a destructive command without `--yes` |
| 3 | Authentication | no workspace is configured; `invalid_auth` after one remint |
| 4 | Not found or ambiguous | `channel_not_found`, `user_not_found`, `message_not_found`; a name with zero matches or more than one |
| 5 | Not permitted (from stage 4) | `not_in_channel`, `restricted_action`, `cant_update_message`, `cant_delete_message`, `is_archived`; read-only mode; a broadcast mention without `--allow-broadcast` |

Stage 1 defines exit codes 0–4 and these error codes: `usage` (2),
`workspace_required` (2), `no_workspace` (3), `auth_failed` (3),
`not_found` (4), `slack_error` (1), `cancelled` (1) and `failed` (1).
Later stages add codes to the same list (`errorExits` in `errors.go`).

- With `--json`, the error is a JSON document on stderr:

  ```json
  {"error": {"code": "ambiguous_reference",
             "message": "\"design\" matches 2 channels",
             "slack_error": "",
             "candidates": [{"id": "C1", "name": "design"}, {"id": "C2", "name": "Design"}]}}
  ```

  Without `--json`, the CLI writes `Error: <message>` to stderr, as today.
- `code` comes from a closed list in `errors.go`. `SKILL.md` documents the
  list. It is part of the contract, like the JSON fields.
- `slack_error` is Slack's own error string when there is one. An agent can
  act on a detail that the CLI does not map.

### D7. References and name resolution

| Kind | Accepted forms |
|---|---|
| Channel | an ID (`C…`, `G…`, `D…`); a name with or without `#`; `<#C…\|name>`; a permalink |
| User | an ID (`U…`, `W…`); a name with or without `@` (handle, display name or real name); `<@U…>`; an email (stage 9) |
| Message | a permalink; or `--channel <channel> --ts <ts>` |
| Thread | a reference to the parent message. A reply's permalink also works, because it carries `thread_ts`. |

**Shell note.** In `sh`, `bash` and `zsh`, a word that starts with `#` is a
comment, so `slk read #general` loses its argument. The CLI accepts bare names
(`slk read general`). The documentation always quotes (`'#general'`).

**Resolution order:**

1. A value with the shape of an ID is used as is. There is no lookup.
2. A name is matched in the cache (`ListChannels(workspaceID, false)`,
   `ListUsers(workspaceID)`), with `text.Fold` on both sides.
3. If the cache has no match, the CLI asks Slack. For channels: `GetChannels`
   (joined channels), then edge `ChannelsSearch` (public channels that the
   user did not join). For users: edge `UsersSearch`.
4. Exactly one match: use it. No match: `not_found` (exit 4). More than one:
   `ambiguous_reference` (exit 4), with `candidates`.

Names must match exactly, after folding. Partial and fuzzy matching belong to
`find`.

ID → name (for output) uses the cache first. For all IDs that the cache does
not know, the CLI makes one batched edge `users/info` call
(`edge.UsersInfo`; `userResolver.ResolveNow` shows how to call it). It never
makes one `users.info` call per user.

Every output carries IDs, so a script resolves a name once and passes the ID
afterwards.

### D8. The cache: read, never write

- The CLI reads the cache to save Slack calls (names, channel lists). It never
  writes the cache.
- The reason: the TUI's sync logic assumes that it is the only writer of
  messages, read state and membership (the `latest_synced_ts` watermark,
  unread counts, the 24-hour membership TTL). A CLI that inserted messages or
  read state could make the TUI skip a backfill or show a wrong badge.
- `cache.New` opens the database with WAL and `busy_timeout(5000)`, so a
  running TUI and the CLI can share it. `cache.New` also runs schema
  migrations. Both processes are the same binary, so they agree on the schema
  (see risk R7).
- A missing or empty cache is normal: the TUI never ran on this machine. Every
  lookup then goes to Slack. `openExportCache` already works this way, and the
  CLI reuses it.

### D9. Write safety (confirm: read-only switch, broadcast guard)

- Each command declares an `Effect`: `Read`, `Write` or `Destructive`.
- Every `Write` and `Destructive` command has `--dry-run`. A dry run resolves
  the references, builds the request and prints it. It sends nothing that
  changes Slack.
- `Destructive` commands (delete, archive, kick, remove-preview, file delete)
  need `--yes`. There are no interactive prompts anywhere.
- **Broadcast guard.** Text that would send `<!here>`, `<!channel>` or
  `<!everyone>` needs `--allow-broadcast`. Without it, the CLI exits with
  code 5. An agent must not notify a whole channel by accident.
- **Read-only switch.** `SLK_READ_ONLY=1`, or `[cli] read_only = true`, makes
  every `Write` and `Destructive` command exit with code 5. The switch
  protects against mistakes. It is not a security boundary: the process that
  runs slk can change its own environment and read the token file.
- **Text input.** Every command that sends text takes exactly one of
  `--text <s>`, `--text-file <path>` or `--text -` (stdin). Empty text is a
  usage error.

### D10. Rate limits and cancellation

- Ctrl-C and SIGTERM cancel the context. `exportChannel` had the right code
  (`signal.NotifyContext`, plus `context.AfterFunc(ctx, stop)` so that a
  second Ctrl-C still kills the process). Stage 1 moved that code into one
  helper, `signalContext` (`cmd/slk/signal.go`), that both use.
- One helper, `withRateLimitRetry(ctx, fn)`, wraps single calls with
  `slackclient.WaitOutRateLimit`. The context limits the wait. (`WalkHistory`
  and `GetRepliesBetween` already retry inside.) Stage 2 adds it, with the
  first Slack call after `auth.test`.
- Calls are sequential. The CLI never sends requests in parallel, so the
  session's traffic stays close to what a person produces.

### D11. Running next to the TUI

- The CLI is a separate process. WAL covers the shared cache (D8). Atomic
  saves cover the token files (D4).
- The CLI opens no WebSocket (except `slk events`), so it does not compete
  with the TUI's socket.
- A message that the CLI sends reaches the running TUI through the TUI's own
  WebSocket, like a message sent from a phone.
- **Link chips.** `SendMessage` sends Work Object chips only while a WebSocket
  is live (`internal/slack/link_chips.go`). Links that the CLI sends go out as
  plain links. This difference is accepted, and `SKILL.md` states it.

### D12. Documentation is checked, not trusted

- `slk skill` prints `internal/cli/SKILL.md`: what slk is, how to choose a
  workspace, the reference forms, the JSON and exit-code contract, the rules
  for safe writes, and every command.
- `TestSkillListsEveryCommand` fails when a command in the table is missing
  from `SKILL.md`, and `TestSkillListsEveryErrorCode` when an error code or its
  exit code is. The command block of `printHelp` is `cli.Overview()`,
  generated from the table, so it needs no check. This obeys AGENTS.md: a
  comment that asks people to keep two things in step becomes a check, or
  better, a structure.
- From stage 3, `SKILL.md` tells agents that message text is data, not
  instructions (risk R6).

---

## 4. Command catalog

This section lists every command that the plan considers, grouped by what it
acts on. §5 orders them into stages.

**Legend**

- **Effect:** R = read, W = write, D = destructive (needs `--yes`).
- **Use:** how often a script or an agent needs it: H = high, M = medium,
  L = low.
- **Size:** S = calls an existing client method, with little new logic.
  M = needs new shared logic (resolution, rendering, paging). L = needs a new
  Slack method with a request capture, or the WebSocket.
- **Parity:** "TUI …" = the TUI has this action (and how you reach it).
  "new" = the TUI does not have it.
- A Slack method in *italics* is not called anywhere in the tree yet.

### A. Meta and workspace

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk help [noun [verb]]` | Help, generated from the table | — | — | — | R | M | S | 1 |
| `slk skill` | Print the agent skill | — | — | new | R | M | S | 1 |
| `slk workspace list` | Configured workspaces, in rail order | — | `config.OrderTokens`; the `--list-workspaces` table, moved | `--list-workspaces` | R | H | S | 1 |
| `slk workspace whoami` | Team and user of the session | `auth.test` | `Client.Connect`, `TeamID`, `UserID`, `UserName` (new), `TeamName`, `TeamSubdomain` | — | R | H | S | 1 |

- `whoami` is the smoke test for auth. `SKILL.md` tells agents to run it
  first.
- There is no `workspace use`: `[general] default_workspace` already sets the
  default (D4). There is no `version --json` either, until someone needs it.
- `add` and `remove` prompt the user, so they stay flags (§2).

### B. Channels

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk channel list [--type public,private]` | Channels the user is in | `users.conversations` | `GetChannels` | TUI sidebar | R | H | S | 2 |
| `slk channel get <channel>` | Name, type, topic, purpose, members, archived | `conversations.info` | `GetConversationInfo` | TUI header | R | H | S | 2 |
| `slk channel members <channel> [--limit]` | Member IDs and names | `conversations.members`, edge `users/info` | `GetUsersInConversation`, `Directory` | TUI membership | R | M | M | 2 |
| `slk channel find <query>` | Server-side channel search, joined or not | edge `channels/search` | `edge.ChannelsSearch`, `finderChannelType` (moved, stage 2) | TUI finder (ctrl+t) | R | M | S | 2 |
| `slk channel join <channel>` | Join a public channel | `conversations.join` | `JoinChannel` | TUI finder, `/join` | W | M | S | 4 |
| `slk channel mark-read <channel> [--ts]` | Mark read up to a ts (default: the newest message) | `conversations.mark` | `MarkChannel` | TUI, on open | W | M | S | 5 |
| `slk channel sections` | Sidebar sections, starred and muted conversations | `users.channelSections.list`, `stars.list`, `users.prefs.get` | `GetChannelSections`, `GetStarredConversations`, `GetMutedChannels` | TUI sidebar | R | L | S | 8 |
| `slk channel leave <channel>` | Leave | *`conversations.leave`* | — | new (TUI refuses `/leave`) | W | L | S | 9 |
| `slk channel create <name> [--private]` | Create | *`conversations.create`* | — | new | W | L | S | 9 |
| `slk channel archive <channel>` | Archive | *`conversations.archive`* | — | new | D | L | S | 9 |
| `slk channel unarchive <channel>` | Unarchive | *`conversations.unarchive`* | — | new | W | L | S | 9 |
| `slk channel rename <channel> <name>` | Rename | *`conversations.rename`* | — | new | W | L | S | 9 |
| `slk channel set-topic <channel> --text …` | Set the topic | *`conversations.setTopic`* | — | new | W | L | S | 9 |
| `slk channel set-purpose <channel> --text …` | Set the purpose | *`conversations.setPurpose`* | — | new | W | L | S | 9 |
| `slk channel invite <channel> <user>...` | Invite people | *`conversations.invite`* | — | new | W | L | S | 9 |
| `slk channel kick <channel> <user>` | Remove a member | *`conversations.kick`* | — | new | D | L | S | 9 |
| `slk channel mute <channel>` / `unmute` | Mute, unmute | *unknown; needs a capture* | — | new | W | L | L | 9 |

- `list` asks Slack, not the cache, because the cache can miss channels that
  the TUI never loaded. `get` also asks Slack. The cache serves only name
  resolution.
- `members` on a large channel is the one place where names could cost
  thousands of calls. The batched edge `users/info` call (D7) and `--limit`
  (default 100) prevent that.
- The administration verbs (create … kick) are standard Web API methods that
  slack-go already wraps, so each one is small. They are in stage 9 because
  the TUI does not have them and nobody asked for them yet.
- Mute is not a standard method (it lives in `users.prefs`). It needs a
  web-client capture first.

### C. Direct messages

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk dm list` | DMs and group DMs, with the names of the other people | `users.conversations` (`im`, `mpim`) | `GetChannels`, `Directory` | TUI sidebar | R | M | S | 2 |
| `slk dm open <user>...` | Find or open a DM (1 person) or group DM (2–8); print the channel ID | `conversations.open` | `OpenConversation` | TUI ctrl+n, `/msg` | W | M | S | 4 |

- To send to a person, use `slk message send --user <user>`. It runs
  `dm open` first.
- `resolveDMNames` (`cmd/slk/users.go`) sends its results to the TUI, so
  `dm list` uses the `Directory` instead.

### D. Users and groups

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk user get <user>` | Profile and custom status | `users.info` | `GetUserProfileContext` | TUI `K` | R | H | S | 2 |
| `slk user find <query>` | Server-side people search | edge `users/search` | `edge.UsersSearch` (no caller today) | new (TUI pickers are local only) | R | H | S | 2 |
| `slk user list` | People in the local cache | — | `cache.ListUsers` | TUI pickers | R | L | S | 2 |
| `slk usergroup list` | Handles, names and IDs | `usergroups.list` | `GetUserGroups`, `usergroups.Display` | TUI mentions | R | L | S | 2 |
| `slk usergroup members <group>` | Member IDs and names | *`usergroups.users.list`* | — | new | R | L | S | 9 |
| `slk user get <email>` | Lookup by email | *`users.lookupByEmail`* | — | new | R | L | S | 9 |

- `user list` reads only the cache, and its output says so
  (`"source": "cache"`). A full directory needs `users.list`, which slk does
  not call, and enterprise workspaces can be very large. `user find` is the
  server-side path.
- Presence and DND of another person are in group K, so `user get` stays one
  API call.

### E. Messages

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk message list <channel>` (`slk read`) | History, printed oldest first. Flags: `--limit` (default 50), `--since`, `--until`, `--before <ts>`, `--around <ts>` | `conversations.history` | `GetHistory`, `GetOlderHistory`, `WalkHistory`, `GetHistoryAround` | TUI open, scroll, jump | R | H | M | 3 |
| `slk message get <message>` | One message | `conversations.history` or `conversations.replies` | `GetHistoryAround`, `GetRepliesBetween` | TUI jump to a permalink | R | H | M | 3 |
| `slk message permalink <message>` | The permalink | `chat.getPermalink` | `GetPermalink` | TUI `Y` | R | M | S | 3 |
| `slk message send (--channel <c> \| --user <u>...) --text …` (`slk send`) | Post a message | `chat.postMessage` (+ `conversations.open`) | `SendMessage`, `OpenConversation`, mention translation (stage 4) | TUI composer | W | H | M | 4 |
| `slk message edit <message> --text …` | Edit an own message | `chat.update` | `EditMessage` | TUI `E` | W | M | S | 6 |
| `slk message delete <message> --yes` | Delete an own message | `chat.delete` | `RemoveMessage` | TUI `D` | D | L | S | 6 |
| `slk message forward <message> (--channel <c> \| --user <u>...)` | Share into another conversation | `chat.getPermalink` + `chat.postMessage` | `ForwardMessage` | TUI `F` | W | L | S | 6 |
| `slk message mark-unread <message>` | Mark the conversation unread from this message | `conversations.mark` / `subscriptions.thread.mark` | `MarkChannelUnread`, `MarkThreadUnread` | TUI `U` | W | L | S | 5 |
| `slk message remove-preview <message> [--preview N] --yes` | Remove a link preview from an own message | `chat.deleteAttachment` | `DeleteAttachment`, `messages.RemovablePreviews` | TUI `x` | D | L | S | 8 |
| `slk message schedule (--channel <c> \| --user <u>) --at <time> --text …` | Schedule a message | *`chat.scheduleMessage`* | — | new | W | L | S | 9 |
| `slk message scheduled list` | Scheduled messages | *`chat.scheduledMessages.list`* | — | new | R | L | S | 9 |
| `slk message scheduled delete <id> --yes` | Cancel a scheduled message | *`chat.deleteScheduledMessage`* | — | new | D | L | S | 9 |

- Slack returns history newest first. The CLI prints oldest first, like a
  transcript, and `next_before` pages further back.
- `--since` and `--until` accept the same forms everywhere (the stage 3 time
  parser). A date means the same day boundary as `slk export --since`.
- `send` is the riskiest common command: it acts as the user, in public. D9
  applies in full.
- `mark-unread` needs the ts of the message before the target. The TUI's `U`
  key passes that "boundary" ts to the service. The CLI gets it with one
  history call.

### F. Threads

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk thread get <message>` | The parent and its replies. Flags: `--since`, `--limit` | `conversations.replies` | `GetReplies`, `GetRepliesBetween` | TUI thread pane | R | H | S | 3 |
| `slk thread reply <message> --text … [--broadcast]` | Reply. `--broadcast` also posts to the channel | `chat.postMessage` (`thread_ts`, `reply_broadcast`) | `SendReply` | TUI thread composer, ctrl+o | W | H | S | 4 |
| `slk thread list [--unread]` | Followed threads | `subscriptions.thread.getView` | `ListThreadSubscriptions` | TUI Threads view | R | M | M | 5 |
| `slk thread mark-read <message> [--ts]` | Mark a thread read | `subscriptions.thread.mark` | `MarkThread` | TUI, on open | W | M | S | 5 |
| `slk thread follow <message>` / `unfollow` | Follow, unfollow | *`subscriptions.thread.add`*, *`subscriptions.thread.remove`* | — | new | W | L | L | 9 |

### G. Reactions and emoji

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk reaction add <message> <emoji>` | Add a reaction | `reactions.add` | `AddReaction` | TUI `r`, `R` | W | M | S | 6 |
| `slk reaction remove <message> <emoji>` | Remove a reaction | `reactions.remove` | `RemoveReaction` | TUI `R` | W | L | S | 6 |
| `slk reaction list <message>` | Who reacted with what | (a message fetch) | the `message get` path | TUI `L` | R | L | S | 6 |
| `slk emoji list [--custom]` | Emoji names; custom ones with their URL or alias | `emoji.list` | `ListCustomEmoji`, `emoji.CodeMap` | TUI pickers | R | L | S | 2 |

- `<emoji>` accepts `thumbsup`, `:thumbsup:` and `thumbsup::skin-tone-2`.
  Slack checks the name (`invalid_name` gives exit 2).

### H. Search

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk search <query> [--limit] [--sort score\|timestamp] [--page]` | Message search. Slack modifiers pass through (`in:`, `from:`, `before:`, `has:`) | `search.messages` | `SearchMessages` (stage 5 adds sort and page options) | TUI ctrl+f | R | H | M | 5 |

- `channel find`, `user find` and `file find` live with their nouns. `search`
  alone means messages, as #107 proposes. This also keeps a query such as
  `slk search files` from looking like a subcommand.

### I. Read state and triage

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk unread list [--mentions]` | Conversations with unread messages: mention count, last read; thread totals | `client.counts` | `GetUnreadCounts`, `Directory` | TUI badges | R | H | M | 5 |
| `slk activity list [--unread] [--limit] [--cursor]` | Mentions, reactions and replies, with the message text | `activity.feed`, `messages.list` | `GetActivityFeed`, `GetActivityMessages` | TUI Activity (ctrl+a) | R | M | M | 5 |
| `slk activity mark-read <item>` / `mark-unread` | Read state of an Activity item | `activity.markRead`, `activity.markUnread` | `SetActivityRead` | TUI `U` in Activity | W | L | S | 5 |
| `slk later list` / `add` / `remove` / `complete` | Saved for later | *`saved.*` (private API)* | — | new | R / W | M | L | 9 |
| `slk reminder list` / `add` / `delete` | Reminders | *`reminders.*`* | — | new | R / W | L | M | 9 |

- `unread list` followed by `message list --since <last_read>` is the
  "summarize my unread messages" loop that an agent needs. `last_read` comes
  in the `client.counts` answer (`UnreadInfo.LastRead`), so it costs no extra
  call.

### J. Files

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk file upload <path>... (--channel <c> \| --user <u>... \| --thread <message>) [--comment …]` | Upload. The comment goes on the last file, as in the TUI | `files.getUploadURLExternal`, upload, `files.completeUploadExternal` | `UploadFile` | TUI ctrl+v | W | M | M | 7 |
| `slk file download <message> [--file N] [--output dir]` | Download the files of a message | (an authenticated GET) | `filedl.New`, `slackhttp.NewAuthResolver` | TUI `d` | R (local write) | M | M | 7 |
| `slk file download <file-id>` | Download one file by ID | *`files.info`* + GET | as above | new | R (local write) | L | S | 7 |
| `slk file get <file-id>` | File metadata | *`files.info`* | — | new | R | L | S | 7 |
| `slk file list [--channel] [--user]` | List files | *`files.list`* | — | new | R | L | S | 9 |
| `slk file find <query>` | Search files | *`search.files`* | — | new | R | L | S | 9 |
| `slk file delete <file-id> --yes` | Delete a file | *`files.delete`* | — | new | D | L | S | 9 |

### K. Own status, presence and do-not-disturb

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk status get` | Own custom status | `users.info` (self) | `GetUserProfileContext` | TUI status bar | R | M | S | 8 |
| `slk status set --text … [--emoji …] [--expires 2h\|today\|<RFC 3339>]` | Set the custom status | `users.profile.set` | `SetCustomStatus` | TUI presets (3) | W | M | S | 8 |
| `slk status clear` | Clear the custom status | `users.profile.set` | `SetCustomStatus` | TUI preset | W | M | S | 8 |
| `slk presence get [<user>]` | Active or away | `users.getPresence` | `GetUserPresence` | TUI dots | R | L | S | 8 |
| `slk presence set auto\|away` | Set own presence | `users.setPresence` | `SetUserPresence` | TUI ctrl+s, `/away` | W | L | S | 8 |
| `slk dnd get [<user>]` | DND or snooze state | `dnd.info` | `GetDNDInfo`, `slackclient.DNDStateFromStatus` | TUI | R | L | S | 8 |
| `slk dnd snooze <duration>` | Snooze notifications | `dnd.setSnooze` | `SetSnooze` | TUI ctrl+s | W | L | S | 8 |
| `slk dnd end` | End the snooze and DND | `dnd.endSnooze`, `dnd.endDnd` | `EndSnooze`, `EndDND` | TUI ctrl+s | W | L | S | 8 |

### L. Organizing (beyond the TUI)

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk pin list <channel>` | Pinned messages | *`pins.list`* | — | new | R | L | S | 9 |
| `slk pin add <message>` / `remove` | Pin, unpin | *`pins.add`*, *`pins.remove`* | — | new | W | L | S | 9 |
| `slk bookmark list <channel>` | Channel bookmarks | *`bookmarks.list`* | — | new | R | L | S | 9 |

Later, reminders and scheduled messages are in groups I and E.

### M. Real-time and power tools

| Command | What it does | Slack method | Reuse | Parity | Effect | Use | Size | Stage |
|---|---|---|---|---|---|---|---|---|
| `slk events [--channel <c>]... [--thread <message>] [--type …] [--count N] [--timeout D]` | Stream events as JSON Lines until a count or a timeout | WebSocket | `StartWebSocket`, `EventHandler` | TUI (internal) | R | M | L | 10 |
| `slk command run <channel> '/name args' [--wait D]` | Run a slash command and collect its output | `chat.command` + WebSocket | `RunCommand`, `CommandRejectedError` | TUI slash commands | W | L | L | 10 |
| `slk api <method> [key=value]...` | A raw call with the session; prints Slack's JSON | any | `PostForm` | — | W | L | S | 10 |
| `slk completion bash\|zsh\|fish` | Shell completion, generated from the table | — | the table | — | R | L | S | 10 |

- `events --thread <message> --count 1 --timeout 10m` means "wait for a
  reply": an agent asks a question and waits until someone answers.
- `command run` is not useful without `--wait`, because Slack answers slash
  commands over the WebSocket. It therefore comes after `events`.
- `api` is the escape hatch for anything that no command covers. Its effect
  is `Write` (read-only mode refuses it), because the CLI cannot know what an
  arbitrary method does. Its output is Slack's, not part of our contract.

### N. Not planned

| TUI action | Why not |
|---|---|
| Typing indicator | It has no meaning without a composer. |
| Copy text, a permalink, an email or a huddle link | Use stdout (`message get`, `message permalink`, `user get`). |
| Image preview; open a file in the OS | The script decides what to open. |
| Themes, sidebar width, windows, marks, history, section collapse | These are screen state. |
| `$EDITOR` drafts | Scripts pass text with `--text-file` or stdin. |
| Desktop notifications; the status-command hook | These are features of the long-running TUI process. Use `slk events`. |
| Press buttons on ephemeral messages | The ephemeral exists only on the socket of the session that received it. |
| Huddles, Slack Connect, Workflow Builder, app management, custom emoji management | Project non-goals. |
| Interactive workspace add and remove | They prompt the user. The existing flags stay. |

---

## 5. Stages

### Overview

| Stage | Theme | Commands | New shared code | Needs |
|---|---|---|---|---|
| 1 | Foundation and workspace | `help`, `skill`, `workspace list/whoami` | the table, dispatch, global flags, help, `Env`, output, errors, workspace selection, lazy remint, atomic token save, the signal context, documentation checks, golden JSON | — |
| 2 | Directory reads | `channel list/get/members/find`, `dm list`, `user get/find/list`, `usergroup list`, `emoji list` | positionals (`parseInterspersed`), `withRateLimitRetry`, the cache reader, the Slack and edge interfaces, reference parsing, `Directory`, the Channel and User types | 1 |
| 3 | Read messages | `message list` (`read`), `message get`, `message permalink`, `thread get`; `export` in help | the Message type, Markdown rendering, time bounds (`Env.Now`), history paging, the workspace from a permalink | 2 |
| 4 | Send | `message send` (`send`), `thread reply`, `dm open`, `channel join` | `Effect`, text input (`Env.Stdin`), mention translation (moved out of `compose`), `--dry-run`, the broadcast guard, the read-only switch, exit code 5, `warnings` | 3 |
| 5 | Triage and search | `unread list`, `channel mark-read`, `thread mark-read`, `message mark-unread`, `thread list`, `activity list/mark-read/mark-unread`, `search` | search options, the Unread and ActivityItem types | 3 |
| 6 | Change messages and reactions | `message edit/delete/forward`, `reaction add/remove/list` | the `--yes` gate, emoji-name normalization | 4 |
| 7 | Files | `file upload/download/get` | the File type, a `files.info` client method | 4 |
| 8 | Own state and the remaining parity | `status get/set/clear`, `presence get/set`, `dnd get/snooze/end`, `channel sections`, `message remove-preview` | expiry parsing | 4 |
| 9 | Beyond the TUI, on demand | the items marked *new* in groups B, D, E, F, I, J, L | one client method per item, with request-capture tests | 2–4 |
| 10 | Real-time and power tools | `events`, `command run`, `api`, `completion` | a WebSocket → JSON Lines mapper | 1–4 |

**Order rule.** Each stage reads before it writes, and each stage uses only the
shared code of earlier stages. Stages 5–8 need stage 3 or 4, but not each
other. They can land in any order, or on parallel branches.

**Milestones.** After stage 4, the core example of #107 works. After stage 5,
the agent loop "find my unread messages, read them, summarize them" works.
Stages 9 and 10 are optional.

**Every stage's PR** runs `go build ./...`, `go vet ./...`,
`go test ./... -race`, `gofmt -l .` (empty) and `golangci-lint run`. It also
updates the documentation in §7 and lists a manual smoke run (§6).

---

### Stage 1 — Foundation and workspace identity

**Goal.** The command table, dispatch, output, errors and the sign-in path
exist and have tests. The workspace commands use them from end to end.

**State.** Implemented on `feat/issue_107_scriptable_CLI`; not yet merged.

**Commands**

| Command | Output (`--json`) |
|---|---|
| `slk help`, `slk help <noun>`, `slk help <noun> <verb>`, `slk <noun> --help`, `slk <noun> <verb> --help` | text only |
| `slk skill` | `SKILL.md` as text; `{"skill": "…"}` |
| `slk workspace list` | `{"workspaces": [Workspace]}`. The text form is the table that `--list-workspaces` printed, which now runs this command. |
| `slk workspace whoami` | `{"workspace": Workspace, "user": {"id", "name"}}`. The workspace is the one `workspace list` shows, so its name and domain work as `--workspace` references; auth.test fills only empty fields. |

`whoami` needs the user's name from `auth.test`. `Client.Connect` read it but
did not keep it, so stage 1 adds a `UserName()` accessor next to `UserID()`.

**Tasks**

- [x] **1.1 Move the atomic writer to a leaf package.** `writeConfigAtomic`
  is now a one-line wrapper over `atomicfile.Write(path, data, defaultPerm)`,
  with the same temp-file name (`.config-*.toml.tmp`). The config tests pass
  unchanged.
- [x] **1.2 Save tokens atomically.** `TestTokenStore_ListNeverSeesPartialSave`
  failed against `os.WriteFile` (`List` returned one token of two) and passes
  with `atomicfile.Write(path, data, 0600)`.
- [x] **1.3 Share the workspace selection.** `cli.Workspace` and
  `cli.SelectWorkspace(list, ref)`; `selectExportToken` is an adapter, and
  the `TestSelectExportToken_*` tests pass unchanged. `TestSelectWorkspace_*`
  and `TestSession_WorkspaceOrder` cover the default and the order of D4.
- [x] **1.4 Table, dispatch and help** (`registry.go`, `cli.go`, `args.go`).
  Tests: `TestHandles`, `TestMain_UnknownCommandExits2`,
  `TestMain_MissingCommandIsAUsageError`, `TestMain_HelpListsNouns`,
  `TestMain_NounHelpListsVerbs`, `TestMain_VerbHelpShowsFlags`,
  `TestMain_GlobalFlagsBeforeOrAfterTheCommand`,
  `TestMain_BadFlagsAndArgumentsExit2`, `TestJSONRequested`,
  `TestMain_ParseErrorsKeepTheRequestedJSONFormat`, `TestParseInterspersed`,
  `TestMain_HelpAcceptsGlobalFlagsAnywhere`, `TestMain_HelpRejectsUnknownFlags`,
  `TestMain_HelpReportsAFailedWrite`, `TestMain_DoubleDashEndsTheFlagsEverywhere`,
  `TestMain_HelpTakesHelpFlagsAsFlagValues`, `TestMain_NounHelpOnlyForARealHelpFlag`,
  `TestMain_LeadingDoubleDashEndsTheFlagsForEveryRoute`,
  `TestRegistry_EveryCommandIsComplete`.
- [x] **1.5 Output and errors** (`output.go`, `errors.go`). Tests:
  `TestErrorFrom_MapsCodes`, `TestErrorExits_CoverEveryCode`,
  `TestPrinter_JSONIsOneDocument`, `TestPrinter_TextGoesThroughTheTextFunction`,
  `TestError_JSONGoesToStderr`, `TestError_TextListsCandidates`. `runCLI`
  checks the JSON contract on every `--json` run: one object, an error object
  on stderr for a failure, no `null`.
- [x] **1.6 Cancellation.** `signalContext` (`cmd/slk/signal.go`) holds the
  signal code that `exportChannel` had; export and the CLI use it.
- [x] **1.7 Sign-in** (`cmd/slk/cli.go`). `newCLIEnv`, `cliWorkspaces`, and
  `connectWithRemint(ctx, tok, connect, remint)` with `remintToken` (one
  token; shared with export). `slackclient.IsAuthError` decides when to
  remint and when to exit with code 3. Tests:
  `TestConnectWithRemint_StoredTokenWorks`,
  `TestConnectWithRemint_InvalidAuthRemintsOnce`,
  `TestConnectWithRemint_StillRefusedIsAnAuthError`,
  `TestConnectWithRemint_UnchangedTokenIsNotRetried`,
  `TestConnectWithRemint_OtherErrorsDoNotRemint`,
  `TestCLIWorkspaces_RailOrderSlugsAndDefault`, `TestIsAuthError`,
  `TestSession_RefusedLoginExits3`,
  `TestRemintTokenWithSkipsBrowserSessionsBeforeAnyDesktopRead`,
  `TestRemintTokenWithRefreshesDesktopSessions`.
- [x] **1.8 The hook in `main()`.** A `default:` branch in the first-argument
  `switch`, before the config pre-load and the emoji width probe, hands
  `cli.Handles(os.Args[1:])` to `cliMain`. `help <command>` goes to the CLI;
  `--list-workspaces` runs `workspace list`. `main.go` gets no new
  declarations.
- [x] **1.9 The workspace commands** (`cmd_workspace.go`), with golden files:
  `workspace_list`, `workspace_list_empty`, `workspace_whoami`,
  `error_not_found`. `TestWorkspaceList_TextIsTheListWorkspacesTable` pins the
  old table.
- [x] **1.10 `slk skill`** (`skill.go`, `SKILL.md`): `TestSkill_*`,
  `TestSkillListsEveryCommand`, `TestSkillListsEveryErrorCode`.
- [x] **1.11 The help text.** `printHelp` prints `cli.Overview()`, which the
  table generates.
- [x] **1.12 Docs.** README "Scripting and agents"; `wiki/CLI.md`, linked from
  `wiki/_Sidebar.md` and `wiki/Commands.md`; the `default_workspace` comment
  in `wiki/Configuration.md`; AGENTS.md: the `internal/cli/` architecture line,
  a "Command line, sign-in and files" table, and test-helper rows for
  `runCLI` and `compareGoldenJSON`.

The help block that `slk --help` now ends with:

```
Commands for scripts and AI agents (add --json for JSON output):
  slk skill      Print the agent skill: how an AI agent uses these commands
  slk workspace  Configured workspaces and the signed-in user (list, whoami)
  Run 'slk help <command>' for details.
```

**Exit criteria**

- [ ] `slk workspace whoami --json` works against a real workspace, both while
  the TUI runs and while it does not. (Manual; not done yet.)
- [x] The `slk export` tests pass unchanged.
- [ ] The check for risk R2 is done and its result is in the PR description:
  force a remint (for example, edit the stored `access_token` so that Slack
  refuses it), run `slk workspace whoami`, then keep using a running TUI.
  (Manual; not done yet.)

**Risk.** The hook in `main()` changes what an unknown word does (D1). The PR
description must say so.

---

### Stage 2 — Directory reads

**Goal.** Resolve and list channels, DMs, users, user groups and emoji. Every
later stage resolves its references through the code that this stage builds.

**Commands:** `channel list`, `channel get`, `channel members`, `channel find`,
`dm list`, `user get`, `user find`, `user list`, `usergroup list`,
`emoji list` (catalog groups B, C, D, G).

**Shared code**

- From stage 1's review (shared code lands with its first user):
  - positionals: `Command.Args` for help, and the positionals that
    `parseInterspersed` already returns in `run` (stage 1 rejects them) on
    `Invocation`. A command checks its own count.
  - `withRateLimitRetry(ctx, fn)` (D10).
  - `Env.Cache` and `CacheReader` (D8); `SlackAPI` and `EdgeAPI`, with
    `Session.Slack` and `Session.Edge`, set by `cliSession`; the compile-time
    assertions in `cmd/slk/cli.go`.
  - the error code `ambiguous_reference` (exit 4), and `errorFrom` mapping
    `channel_not_found` and `user_not_found` to `not_found`.
- `refs.go`: `ParseChannelRef(s string) (ChannelRef, error)` and
  `ParseUserRef(s string) (UserRef, error)`. Both are pure. Kinds: ID, name,
  mention, and permalink (channels only). They check the shape of IDs.
- `directory.go`: `Directory`, built from a `CacheReader`, the Slack and edge
  interfaces and the workspace ID. Methods: `Channel(ctx, ChannelRef)`,
  `User(ctx, UserRef)`, `UserNames(ctx, ids)` (batched, D7) and
  `ChannelNames(ctx, ids)`.
- `dto.go`: `Channel`, `User`, `UserGroup` and `Emoji`, with converters from
  `slack.Channel`, `slackclient.UserProfile`, `edge.Channel`, `edge.User`,
  `cache.Channel` and `cache.User`.

**Reuse:** `text.Fold`; `edge.ChannelsSearch` (with no top channels);
`edge.UsersSearch`; `edge.UsersInfo` (call it as `userResolver.ResolveNow`
does); `GetChannels`; `GetConversationInfo`; `GetUsersInConversation`;
`GetUserProfileContext`; `GetUserGroups`; `usergroups.Display`;
`ListCustomEmoji`. The session builds its edge client as `connectWorkspace`
does: `edge.New(token, client.TeamID(), client.HTTPClient())`.

`edge.Channel` carries flags, not a channel type. `finderChannelType`
(`cmd/slk/channel_search.go`) already maps those flags to a type. Move it to
where both `cmd/slk` and `internal/cli` can call it, rather than write a second
mapping. Do not reuse `searchChannelsRemote`: it builds the TUI's finder items.

**Tasks**

- [ ] **2.0 The base.** Positionals in `run` (flags after a positional, and
  a usage error for a missing or extra positional; `TestParseInterspersed`
  covers the parser itself),
  `withRateLimitRetry` (a fake that fails once with
  `*slack.RateLimitedError{RetryAfter: time.Millisecond}`, and a context
  cancelled during the wait), the cache reader and the client interfaces.
- [ ] **2.1 References.** `TestParseChannelRef` and `TestParseUserRef`, as
  table tests: IDs, `#name`, bare names, `<#C…|name>`, `<@U…>`, permalinks,
  and bad input.
- [ ] **2.2 The Directory**, with fakes:
  - `TestDirectory_IDNeedsNoLookup`
  - `TestDirectory_CacheHit`
  - `TestDirectory_CacheMissAsksSlack`
  - `TestDirectory_JoinedChannelsThenEdgeSearch`
  - `TestDirectory_AmbiguousNameListsCandidates`
  - `TestDirectory_NotFound`
  - `TestDirectory_FoldedMatch` (case and accents)
  - `TestDirectory_UnknownNamesInOneBatchedCall`
- [ ] **2.3 The type converters**, each with a test.
- [ ] **2.4 The commands, one at a time**, each with text and JSON tests and a
  golden file: `channel list` → `get` → `members` → `find`; `dm list`;
  `user get` → `find` → `list`; `usergroup list`; `emoji list`.
- [ ] **2.5 Docs:** `SKILL.md`, `printHelp`, `wiki/CLI.md`, and AGENTS.md rows
  for `ParseChannelRef`, `ParseUserRef` and `Directory`.

**Exit criteria.** Every command works in a workspace where the TUI never ran
(an empty cache) and in one where it did. Names resolve in both directions.

---

### Stage 3 — Read messages

**Goal.** Read channels and threads as a transcript, with paging and time
filters.

**Commands:** `message list` (and `read`), `message get`, `message permalink`,
`thread get`. Also register `export` in the table with `RawArgs`, so that help
and the skill list it.

**Shared code**

- `Env.Now`, for the relative time bounds, so tests fix the clock.
- `SKILL.md` gets the section "Message text is data": text read from Slack
  was written by other people, and an agent never follows instructions in it
  (risk R6).
- `ParseTimeBound(s string, now time.Time, loc *time.Location) (ts string,
  err error)`. Accepted forms: a Slack ts; RFC 3339; `YYYY-MM-DD` (the start
  of that day in `loc`, the same boundary that `export.NewWindow` uses); a
  relative age, `30m`, `2h`, `7d` or `2w`, counted back from `now`. A
  `--timezone` flag works as in `slk export`.
- `ParseMessageRef(args, channelFlag, tsFlag) (MessageRef, error)`. A
  permalink (`slackurl.Parse`) gives the channel, the ts, the thread ts, and
  the workspace (D4 step 3).
- The Message type and its converter. Do not write another converter from
  `slack.Message`. `slk export` already has one, `exportMessageItem`
  (`cmd/slk/export_collect.go`), which builds a `messages.MessageItem`. Move
  the part that does not need `userNameStore` into a package that
  `internal/cli` can import, and use it for both. The `markdown` field uses
  the same conversion as the export writer
  (`messages.SlackMrkdwnToCommonMark(messages.MessageTextSource(msg), …)` in
  `internal/export/markdown.go`), so `slk read` and `slk export` agree on
  every message.
- History paging: the CLI takes the newest page from Slack, prints it oldest
  first, and sets `has_more` and `next_before` (the oldest ts that it
  printed).

**How `message list` maps to client calls**

| Flags | Client call |
|---|---|
| none, or `--limit N` | `GetHistory(ch, N, "")` |
| `--before <ts>` | `GetOlderHistory(ch, N, ts)` |
| `--since` and/or `--until` | `WalkHistory(ch, oldest, latest, visit)`, stopped at N |
| `--around <ts>` | `GetHistoryAround(ch, ts, N)` |

`--limit` defaults to 50. Above 1000 the CLI exits with code 2. `--before`,
`--around` and the `--since`/`--until` pair cannot be combined (exit 2).

**`message get <message>`.** For a channel message, call `GetHistoryAround`
and pick the exact ts. For a reply (the thread ts is known), call
`GetRepliesBetween(ch, threadTS, ts, ts)`. No exact match gives
`message_not_found` (exit 4).

**`thread get <message>`** returns
`{"thread": {"channel_id", "thread_ts", "parent": Message, "replies": [Message], "has_more"}}`.
It calls `GetReplies`, or `GetRepliesBetween` when `--since` is given.
`--limit` keeps the newest N replies.

**Tasks**

- [ ] **3.1 `ParseTimeBound`** table tests: each form, a daylight-saving day,
  bad input, and `--until` before `--since` (exit 2).
- [ ] **3.2 `ParseMessageRef`** tests: a permalink with and without
  `thread_ts`, the flags, and both forms at once (exit 2).
- [ ] **3.3 The shared converter.** Move it (pure motion, with the export tests
  unchanged), then test the Message type: a person, a bot, an edited message,
  reactions, files, a body only in blocks, an unknown user.
- [ ] **3.4 `message list`**, one test and golden file for each row of the
  table above, plus a test for the `read` alias.
- [ ] **3.5 `message get`, `message permalink` and `thread get`.**
- [ ] **3.6 The workspace from a permalink:**
  `TestSelectWorkspace_FromPermalinkDomain`; an explicit `--workspace` still
  wins.
- [ ] **3.7 `export` in the table:** `TestExportIsListedInHelp`. The export
  tests pass unchanged.
- [ ] **3.8 Docs**, and AGENTS.md rows for `ParseTimeBound`, `ParseMessageRef`
  and the moved converter.

**Exit criteria.** An agent can run `slk read general --since 1d --json` and
`slk thread get <permalink> --json`, and get complete transcripts with names.

---

### Stage 4 — Send messages

**Goal.** The core example of #107: post to a channel, a person or a thread,
safely.

**Commands:** `message send` (and `send`), `thread reply`, `dm open`,
`channel join`.

**Shared code**

- **Mention translation, moved out of the UI.**
  `(compose.Model).TranslateMentionsForSend` changes `@name` and `#name` into
  the wire form, from maps that the composer holds. Move its body to
  `slackfmt.TranslateMentions(text string, n slackfmt.MentionNames) string`,
  where `MentionNames` holds `Users` (name → ID), `UserGroups` (ID → handle)
  and `Channels` (name → ID). The compose method becomes a one-line call. This
  is pure motion: the compose tests pass unchanged. If the rule that picks the
  user-name keys (`rebuildMentionUsers`) is more than a choice of field, move
  it too, so that the CLI builds the same keys as the TUI.
- **Mention names for the CLI.** The CLI builds them only when the text
  contains `@` or `#`: users and channels from the cache, user groups from
  `GetUserGroups`. For each `@word` that is still not translated, the CLI asks
  edge `UsersSearch` for an exact match. A word that is still unknown goes out
  as plain text and appears in `warnings`.
- `Command.Effect` (D9) and the contract test that every row has one;
  exit code 5 with the error code `not_permitted`; the `warnings` array of
  D5; `Env.Stdin`.
- `readText` (the text input of D9), `--dry-run`, the `Effect` gate (the
  read-only switch, `SLK_READ_ONLY` or `[cli] read_only`), and the broadcast
  guard (it looks for `<!here>`, `<!channel>` and `<!everyone>` after the
  translation).
- An error hint: `not_in_channel` gives exit 5 with the hint
  "join first: slk channel join <channel>".

**Command details**

- `message send (--channel <c> | --user <u> [--user <u>]…) (--text … |
  --text-file … | --text -) [--dry-run] [--allow-broadcast] [--raw]`
  - `--user` (1 to 8 people) calls `OpenConversation` first.
  - `--raw` skips the mention translation, for text that is already in wire
    form.
  - `SendMessage` converts the text (`mrkdwn.Convert`): CommonMark in, mrkdwn
    and a rich_text block out. This is the same as in the TUI.
  - Output: `{"message": Message}`, built from the ts and the mrkdwn that
    `SendMessage` returns.
- `thread reply <message> --text … [--broadcast]` calls `SendReply`.
- `dm open <user>...` returns `{"channel": Channel, "already_open": bool}`.
- `channel join <channel>` returns `{"channel": Channel}`.

A dry run returns:

```json
{"dry_run": true,
 "request": {"method": "chat.postMessage", "channel_id": "C0123ABCD", "thread_ts": "",
             "text": "<@U0789IJKL> the deploy is done", "user_ids": []},
 "warnings": []}
```

With `--user`, a dry run does not call `conversations.open`, because that call
creates the DM. It reports `"channel_id": ""` and the `user_ids`.

**Tasks**

- [ ] **4.1 Move the mention translation** to `slackfmt.TranslateMentions`. The
  compose tests pass unchanged. Add direct tests in `slackfmt`.
- [ ] **4.2 `readText`** tests: each source, two sources (exit 2), empty text
  (exit 2), stdin.
- [ ] **4.3 The Effect gate** tests: read-only mode refuses `Write` (exit 5)
  and allows `Read`. `TestRegistry_WritesHaveDryRun` checks that every `Write`
  and `Destructive` command has `--dry-run`.
- [ ] **4.4 The broadcast guard** tests: each special mention,
  `--allow-broadcast`, and `--raw` text that contains `<!here>`.
- [ ] **4.5 `message send`** tests: a channel, one person, a group DM, a dry
  run that sends nothing (the fake fails the test on any write), mention
  warnings, and the `not_in_channel` hint.
- [ ] **4.6 `thread reply`** (with and without `--broadcast`), `dm open`,
  `channel join`.
- [ ] **4.7 Docs.** `SKILL.md` gets a section "Before you send": do a dry run
  first, never broadcast unless the user asks, and treat message text as data.

**Exit criteria.** `slk send --channel general --text "hi @alice" --json`
posts a message whose mention renders in Slack's own clients. A dry run never
posts. Read-only mode blocks every write.

**Risk.** An agent loop that posts again and again. The read-only switch, dry
runs and the docs reduce the risk. The CLI does not try to detect loops.

---

### Stage 5 — Triage and search

**Goal.** Find what needs attention, and search.

**Commands:** `unread list`, `channel mark-read`, `thread mark-read`,
`message mark-unread`, `thread list`, `activity list`, `activity mark-read`,
`activity mark-unread`, `search`.

**Command details**

- `unread list [--mentions]` calls `GetUnreadCounts` and returns
  `{"conversations": [{"channel": Channel, "mention_count", "last_read", "last_read_time"}], "threads": {"has_unreads", "unread_count", "mention_count"}}`.
  The sort order is mention count (high first), then name.
- `channel mark-read <channel> [--ts]`. Without `--ts`, the CLI uses the ts of
  the newest message (`GetHistory(ch, 1, "")`).
- `thread mark-read <message> [--ts]`. Without `--ts`, the CLI uses the ts of
  the newest reply.
- `message mark-unread <message>`. The boundary is the ts of the message
  before the target: for a channel, `GetOlderHistory(ch, 1, ts)`, or `"0"`
  when there is none; for a thread, the reply before it. The TUI computes the
  same boundary for its `U` key (`internal/ui/reducer_send.go`). If that rule
  is a pure function, share it.
- `thread list [--unread] [--limit]` calls `ListThreadSubscriptions`. Each item
  has `channel_id`, `thread_ts`, `last_read`, `unread` (the newest reply is
  later than the last read) and the `parent` Message.
- `activity list [--unread] [--limit] [--cursor]` calls `GetActivityFeed`,
  then `GetActivityMessages` for the text, and returns `next_cursor`. Each item
  has an opaque `id` that encodes the three values `SetActivityRead` needs
  (type, key, feed ts).
- `activity mark-read <id>` and `activity mark-unread <id>` call
  `SetActivityRead`.
- `search <query> [--limit] [--sort score|timestamp] [--page]`. Add a
  `SearchOptions` parameter (count, sort, sort direction, page) to
  `SearchMessages`. The one TUI call site passes the values it uses today.
  Output: `{"matches": [Message + "channel_name" + "permalink"], "page", "pages", "total"}`.

**Tasks** (each one test-first, with golden files)

- [ ] **5.1 `unread list`**, including the sort order and `--mentions`.
- [ ] **5.2 `channel mark-read` and `thread mark-read`**, with and without
  `--ts`.
- [ ] **5.3 `message mark-unread`** boundary tests: the first message in a
  channel (`"0"`), a middle message, a thread reply.
- [ ] **5.4 `thread list`.**
- [ ] **5.5 `activity list`**, with hydration and paging, and the two mark
  commands.
- [ ] **5.6 `search` and `SearchOptions`.** The TUI's workspace-search tests
  pass unchanged.
- [ ] **5.7 Docs.** `SKILL.md` gets a "Triage loop" recipe:
  `unread list` → `message list --since <last_read>` → `channel mark-read`. It
  also says: do not mark anything read unless the user asks, because marking
  read changes what the user sees in every Slack client.

**Exit criteria.** The triage loop works on a real workspace. The TUI shows the
read state that the CLI set (through its WebSocket).

---

### Stage 6 — Change messages and reactions

**Commands:** `message edit`, `message delete`, `message forward`,
`reaction add`, `reaction remove`, `reaction list`.

**Command details**

- `message edit <message> --text …` uses the same text pipeline as `send`
  (translation, broadcast guard, dry run) and calls `EditMessage`. Slack
  refuses other people's messages (`cant_update_message`, exit 5).
- `message delete <message> --yes` calls `RemoveMessage`. The command is
  `Destructive`: without `--yes` it exits with code 2.
  `TestRegistry_DestructiveNeedsYes` checks this for every destructive
  command.
- `message forward <message> (--channel <c> | --user <u>...)` calls
  `ForwardMessage` (and `OpenConversation` for people). It returns the new
  message and its permalink.
- `reaction add <message> <emoji>` and `reaction remove <message> <emoji>`
  normalize the name (they strip the colons and keep `::skin-tone-N`). They
  return `{"reaction": {"channel_id", "ts", "name"}}`. Both are idempotent:
  `already_reacted` and `no_reaction` give exit 0 and a warning, so an agent
  can retry safely.
- `reaction list <message>` reads the message and returns
  `{"reactions": [{"name", "count", "user_ids", "user_names"}]}`.

**Tasks:** one test-first task per command; the idempotency tests; the
`--yes` contract test; docs.

---

### Stage 7 — Files

**Commands:** `file upload`, `file download`, `file get`.

**Command details**

- `file upload <path>... (--channel <c> | --user <u>... | --thread <message>)
  [--comment …]` calls `UploadFile` once per file. The comment goes on the
  last file, which is the TUI's rule. The CLI checks every path first (it
  exists and is a regular file). A bad path gives exit 2, and nothing is
  uploaded. Without `--json`, progress lines go to stderr. Output:
  `{"files": [File]}`. A dry run lists what it would upload.
- `file download <message> [--file N] [--output dir]` and
  `file download <file-id>` use `filedl.New(slackhttp.NewAuthResolver(…),
  dir)`, as `main.go` builds it for the TUI. The default output directory is
  the current directory, because scripts expect that. (The TUI's
  `[general] download_dir` is for people.) An existing file is never
  overwritten. Check what `filedl` already does about name clashes before you
  add anything. Output: `{"files": [{"id", "name", "path", "size"}]}`.
- `file get <file-id>` needs a new method, `Client.GetFileInfo` (slack-go's
  `GetFileInfoContext`, which calls `files.info`). `file download <file-id>`
  uses it too.
- The File type: `id`, `name`, `title`, `mimetype`, `size`, `url_private`,
  `permalink`, `user_id`, `created` and its `time`, `channel_ids`.

**Tasks:** path checks; the comment-on-the-last-file rule; download naming; a
test of the `files.info` request; golden files; docs.

---

### Stage 8 — Own status, presence and DND; the remaining parity

**Commands:** `status get`, `status set`, `status clear`, `presence get`,
`presence set`, `dnd get`, `dnd snooze`, `dnd end`, `channel sections`,
`message remove-preview`.

**Command details**

- `--expires` and the snooze duration reuse the relative forms of
  `ParseTimeBound` and add `today` (the end of the local day) and `never`. The
  TUI's "Working from home" preset also expires at the end of the day: look in
  `internal/ui/presencemenu` for a helper to share before you write one.
- `status set --text … [--emoji :house:] [--expires …]` calls
  `SetCustomStatus(text, emoji, unixExpiry)`.
- `dnd end` calls `EndSnooze` and then `EndDND`, as the TUI does.
- `channel sections` returns
  `{"sections": [{"id", "name", "type", "channel_ids"}], "starred": [ids], "muted": [ids]}`.
- `message remove-preview <message> [--preview N] --yes` lists the removable
  previews with `messages.RemovablePreviews`. When a message has more than one
  preview and `--preview` is not given, the CLI exits with code 2 and lists
  them.

**Tasks:** one test-first task per command; golden files; docs.

---

### Stage 9 — Beyond the TUI, on demand

**Goal.** Add Slack features that the TUI does not have, one at a time, and
only when someone needs them.

**Rule.** Do not start an item without a demonstrated need: an issue or a
user's request. Each item is its own PR.

**The procedure for each item**

1. Find the request shape. For standard Web API methods (`conversations.*`,
   `pins.*`, `bookmarks.list`, `files.*`, `usergroups.users.list`,
   `users.lookupByEmail`, `chat.scheduleMessage`), slack-go has a wrapper. Use
   it. For a private method (`saved.*`, mute, thread follow), capture what the
   web client sends first.
2. Add one `Client` method. Test a private method against the capture with
   `assertFormMatchesCapture` (AGENTS.md test helpers), never against a
   hand-written guess.
3. Add the command, its golden files, `SKILL.md` and help.

**Items, in the order of the demand we expect**

| Item | Commands | Slack methods | Note |
|---|---|---|---|
| Scheduled messages | `message schedule`, `message scheduled list/delete` | `chat.scheduleMessage`, `chat.scheduledMessages.list`, `chat.deleteScheduledMessage` | standard |
| Saved for later | `later list/add/remove/complete` | `saved.*` (private) | capture first |
| Reminders | `reminder list/add/delete` | `reminders.*` | Slack moves reminders into Later; check first that it still serves `reminders.*` to session tokens |
| Channel administration | `channel create/archive/unarchive/rename/set-topic/set-purpose/invite/kick/leave` | `conversations.*` | standard; a workspace policy can refuse (exit 5) |
| Files | `file list/find/delete` | `files.list`, `search.files`, `files.delete` | standard |
| Users | `user get <email>`, `usergroup members` | `users.lookupByEmail`, `usergroups.users.list` | standard |
| Pins and bookmarks | `pin list/add/remove`, `bookmark list` | `pins.*`, `bookmarks.list` | standard |
| Thread follow | `thread follow/unfollow` | `subscriptions.thread.add/remove` (private) | capture first |
| Mute | `channel mute/unmute` | private (`users.prefs`) | capture first |

---

### Stage 10 — Real-time and power tools

**Commands:** `events`, `command run`, `api`, `completion`.

**`slk events`**

- It opens a WebSocket with `StartWebSocket(handler)`. The handler implements
  `slackclient.EventHandler` and writes one JSON object per line (JSON Lines)
  for each event that passes the filters.
- Flags: `--channel <c>` (repeatable), `--thread <message>`,
  `--type message,reaction_added,…`, `--count N` (stop after N events) and
  `--timeout D`.
- Ending: with `--count`, a timeout before N events is the error `timeout`
  (exit 1). Without `--count`, the timeout is the normal end (exit 0).
- A message event line is the stage 3 Message type plus `"type": "message"`. A
  reaction line is
  `{"type": "reaction_added", "channel_id", "ts", "user_id", "name"}`.
- A disconnect ends the command with the error `disconnected` (exit 1). The
  agent can run it again. The first version has no reconnect logic.
- JSON Lines is the only machine format, so `--json` changes nothing. Without
  `--json`, the CLI prints one text line per event.

**`slk command run <channel> '/name args' [--wait D]`** calls `RunCommand`.
With `--wait`, it collects the ephemeral messages and messages that the
command produces for D, through the `events` code. `CommandRejectedError`
gives exit 5, and `unknown_command` gives exit 2.

**`slk api <method> [key=value]... [--data-file f]`** calls `PostForm` and
prints Slack's raw JSON. When `ok` is false, the CLI still prints the body and
exits with code 1. The effect is `Write`.

**`slk completion bash|zsh|fish`** is generated from the table: nouns, verbs
and flags.

**Risk.** A second WebSocket for the same user while the TUI runs. The
official clients open several sessions (desktop, browser, phone), so we expect
this to work. Test it before the stage lands: run the TUI and `slk events`
side by side for an hour, and check that neither one disconnects.

---

## 6. Tests, all stages

- **Style:** stdlib `testing`, white-box (`package cli`), no testify, as
  AGENTS.md requires.
- **Pure parsers** (references, time bounds, arguments): table tests.
- **Command tests** go through one helper:

  ```go
  // runCLI runs args through cli.Main with a fake environment and returns
  // what the user would see. Under --json it also checks the JSON contract.
  func runCLI(t *testing.T, args []string, opts ...envOpt) (stdout, stderr string, exit int)
  ```

  Options in stage 1: `withWorkspaces(...)`, `withEnv(k, v)`,
  `withConnect(fn)`, `withListError(err)`. Later stages add `withSlack(fake)`,
  `withEdge(fake)`, `withCache(fake)`, `withStdin(s)` and `withNow(t)` with the
  `Env` fields they fake.
- **Fakes.** `fakeSlack` embeds `SlackAPI` (nil) and overrides only the
  methods that a test uses, as function fields. Any other call panics, so a
  test that writes by mistake fails. Write commands record their calls, and
  dry-run tests assert that the record is empty.
- **Golden JSON.** `compareGoldenJSON(t, name, got)` compares with
  `testdata/json/<name>.json`. A package-local `-update` flag re-blesses the
  files (`go test ./internal/cli -run TestGolden -update`), as `internal/ui`
  does for its screen goldens.
- **Contract tests:** every command has a `Summary` and an `Effect`; every
  `Write` and `Destructive` command has `--dry-run`; every `Destructive`
  command needs `--yes`; every command and every error code is in `SKILL.md`;
  `printHelp` lists every noun.
- **Captured request shapes** for private Slack methods (stage 9):
  `assertFormMatchesCapture`.
- **No test talks to Slack.** Each stage's PR lists a manual smoke run against
  a real workspace (the commands and their output), both while the TUI runs
  and while it does not.
- **Speed.** The `internal/cli` tests must run in well under a second. They
  inject `Now` and timers instead of sleeping.

## 7. Documentation, all stages

Each stage updates these in the same PR:

- `internal/cli/SKILL.md` (tests check it),
- `printHelp` in `cmd/slk/main.go` (a test checks it),
- `wiki/CLI.md`: one section per noun, with examples,
- `README.md`: the "Scripting and agents" section. Stage 1 writes it. Later
  stages add an example only when it matters.
- `AGENTS.md`: a "Shared code" row for every reusable helper and a "Test
  helpers" row for every test helper, in the commit that adds it.

## 8. Risks

| # | Risk | Mitigation |
|---|---|---|
| R1 | Automation through a session token uses the same unofficial protocol as the TUI (wiki, "Unofficial / TOS caveat"). Many requests from one user session can look abnormal to Slack. | Sequential calls; obey every rate limit; no bulk loops inside the CLI; a warning in `SKILL.md` and in the wiki. |
| R2 | A remint by the CLI could end the session of the running TUI. | Check this in stage 1: remint while the TUI runs, then keep using the TUI. If the TUI's session breaks, remove lazy remint; the CLI then exits with code 3 and tells the user to start slk, which refreshes the login. |
| R3 | Names in the cache are old (a renamed channel, a changed display name). | Every output has IDs; `get` commands read Slack; an ambiguous name is an error, not a guess. |
| R4 | A second WebSocket (stage 10). | The side-by-side test before the stage lands. |
| R5 | Agents depend on the JSON fields, so a rename breaks them. | Additive changes only; golden files make every change visible in review. |
| R6 | Prompt injection: an agent that reads messages can find text that tells it to send or delete something. | `SKILL.md` says that message text is data, not instructions, and that writes need the user's request; the read-only switch for read-only agents; dry runs. |
| R7 | A newer CLI binary migrates the cache while an older TUI still runs. | Migrations only add columns (`addColumnIfMissing`). This is the same situation as two TUI versions today. Say so in the stage 1 PR. |
| R8 | This plan was written against `main` at `02a96005`, and the code moves. | Each stage checks the symbols it uses before it starts, and corrects this plan when they changed. |

## 9. Open questions for the maintainer

1. **Command shape (D1).** Noun–verb, plus the shortcuts `send` and `read`
   from #107. Is that acceptable?
2. **Parser (D2).** The standard library and a table, not cobra. Is that
   acceptable?
3. **Unknown command word (D1).** Exit with code 2 instead of starting the
   TUI. Is that acceptable? (Stage 1 does this.)
4. **Environment credentials (D4).** Are `SLK_TOKEN` and `SLK_COOKIE` wanted,
   or is the token store enough? (Not implemented until someone needs them.)
4a. **Default workspace (D4).** Stage 1 uses the existing
   `[general] default_workspace` for the CLI too, so the TUI's first
   workspace and the CLI's default are one setting. Is that acceptable, or do
   they need to differ?
5. **Write safety (D9).** Is the broadcast guard on by default wanted? Is the
   read-only switch wanted?
6. **Stage 9.** Which items have real demand?
7. **Agent docs.** Is `slk skill` plus `wiki/CLI.md` enough, or must the skill
   also go to a skills or plugin repository?

## 10. Follow-ups outside this plan

- **An MCP server (`slk mcp`).** It would serve the command table as MCP tools
  over stdio: each `Command` becomes a tool, its flags become the input
  schema, and the JSON contract becomes the output. Start it only after stages
  1–5, so that the tools have stable shapes.
- **ACP.** It is not related to #107. ACP would let slk host a coding agent in
  a TUI pane, in the editor's role. That needs a separate RFC.

---

## Status

| Stage | State | PR | Achieved |
|---|---|---|---|
| 1 Foundation and workspace | implemented, PR not open | | `help`, `skill`, `workspace list/whoami`; the base (table, dispatch, output, errors, sign-in, atomic token saves); manual checks pending |
| 2 Directory reads | not started | | |
| 3 Read messages | not started | | |
| 4 Send | not started | | |
| 5 Triage and search | not started | | |
| 6 Change messages and reactions | not started | | |
| 7 Files | not started | | |
| 8 Own state and the remaining parity | not started | | |
| 9 Beyond the TUI (per item) | not started | | |
| 10 Real-time and power tools | not started | | |
