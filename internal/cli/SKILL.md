---
name: slk
description: Run slk commands to work in Slack as the user, through the login
  the user gave slk. Use when the user asks you to look something up in Slack
  or to act in a Slack workspace, and the slk command is installed.
---

# slk

slk is a Slack client for the terminal. Besides its full-screen TUI, it has
commands for scripts and agents, of the form `slk <noun> <verb> [flags]`. A
command acts as the signed-in user, through the login the user gave slk with
`slk --add-workspace`. A command never asks for input and never starts the
TUI. One wait is outside slk: when a command refreshes an expired login from
the Slack desktop app, the operating system can ask the user to allow
keychain access, and the command waits for that answer. If a command seems
stuck, ask the user to check for such a dialog.

## Before you start

1. Run `slk workspace whoami --json`. It signs in and shows the workspace and
   the user that commands act as. Exit code 3 means the login failed: ask the
   user to run `slk --add-workspace`, which is interactive. Do not run it
   yourself.
2. Choose the workspace. `slk workspace list --json` lists them. Give every
   command `--workspace <slug|team ID|name|domain>`, or set `SLK_WORKSPACE`
   once. Without either, slk uses the workspace that `default_workspace` names
   under `[general]` in `~/.config/slk/config.toml`, else the only one.
   Otherwise the command fails with `workspace_required`. Exception: `auth import`
   takes the workspace from its input file; it rejects explicit
   `--workspace` (even empty) and ignores `SLK_WORKSPACE` and configured defaults.
3. Never export credentials without explicit user authorization. Auth transfer
   files contain plaintext credentials that grant the user's Slack access.
   Never display their contents, attach them to logs, or put them in chat.

## Output

- Add `--json` to any command. stdout then gets exactly one JSON document, an
  object. Without `--json`, the output is text for people, and its format
  can change.
- Field names are snake_case. IDs are always present. An empty list is `[]`,
  never `null`.
- Fields are added over time, but never renamed or removed. Ignore fields you
  do not know.

## Errors and exit codes

| Exit | Meaning |
|---|---|
| 0 | Success |
| 1 | Failure: the network, Slack, an interruption, or anything unexpected |
| 2 | Usage: an unknown command or flag, a missing argument, no workspace chosen, or an invalid auth bundle |
| 3 | Authentication: no workspace is configured, or Slack refused the login |
| 4 | Not found: a reference matched nothing |

With `--json`, a failure writes one JSON document to stderr, and nothing to
stdout:

```json
{
  "error": {
    "code": "not_found",
    "message": "workspace \"acme\" not found; see 'slk --list-workspaces'",
    "slack_error": "",
    "candidates": [{"id": "T0123ABCD", "name": "Example Inc"}]
  }
}
```

`code` is one of these:

| Code | Exit | Meaning |
|---|---|---|
| `usage` | 2 | The command line is wrong. `message` says how. |
| `workspace_required` | 2 | More than one workspace is configured, and none was chosen. `candidates` lists them. |
| `no_workspace` | 3 | No workspace is configured. |
| `auth_failed` | 3 | Slack refused the saved or supplied login. |
| `invalid_auth_bundle` | 2 | Invalid, oversized, unsupported or identity-mismatched auth document. |
| `auth_conflict` | 1 | Existing workspace without `--replace`, another or unverifiable account, or destination changed during validation. |
| `unsafe_auth_file` | 1 | Unsafe input/destination type or POSIX permissions. |
| `not_found` | 4 | The reference matched nothing. `candidates` lists what exists. |
| `slack_error` | 1 | Slack refused the request. `slack_error` is Slack's own error code. |
| `cancelled` | 1 | The command was interrupted. |
| `failed` | 1 | Any other failure. |

## Commands

- `slk workspace whoami` signs in and prints
  `{"workspace": Workspace, "user": {"id", "name"}}`. `name` is the user's
  handle.
- `slk workspace list` prints `{"workspaces": [Workspace]}`, in the order of
  the TUI's workspace rail. It reads local files only. A Workspace is
  `{"team_id", "slug", "name", "domain", "default"}`. `default` is true for
  the workspace that commands use when none is chosen.
- `slk auth export` requires `--output <file>`; `-` is refused. It selects a
  workspace using the normal rules and writes its saved login offline, without
  refreshing it. It refuses an existing file. JSON contains only metadata:
  `{"workspace": {"team_id", "name", "domain"}, "output", "plaintext": true}`.
  Text output warns about plaintext credentials and shows the path. Transfer
  securely and delete the file when finished; both machines share revocation.
- `slk auth import` requires `--input <file>`; `-` is refused. It validates the
  supplied login with Slack before saving it. `--replace` permits replacing an
  existing login only for the same workspace and user. JSON contains only
  metadata: `{"workspace": {"team_id", "name", "domain"}, "user": {"id"},
  "replaced"}`. Restart running slk processes afterward. Imported sessions are
  not refreshed from the desktop app; when one expires, transfer a new login
  with deliberate replacement or sign in interactively.
- `slk skill` prints this file.

If output fails after a successful export or import, the completed file/store
write is not rolled back. Check before rerunning: export refuses an existing
file, and import may require deliberate `--replace` for the same account.

`slk help <noun> <verb>` shows a command's flags. `slk help` lists every
command.
