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
   Otherwise the command fails with `workspace_required`.

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
| 2 | Usage: an unknown command or flag, a missing argument, or no workspace chosen |
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
| `auth_failed` | 3 | Slack refused the saved login. |
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
- `slk skill` prints this file.

`slk help <noun> <verb>` shows a command's flags. `slk help` lists every
command.
