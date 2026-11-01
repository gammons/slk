# CLI: scripts and AI agents

Besides the TUI, slk has commands you run from a shell, a script or an AI agent.
They have the form `slk <noun> <verb> [flags]`, they act as you through the
login you gave slk (`slk --add-workspace`), and they never ask for input or
start the TUI (one exception is up to your OS; see [Signing in](#signing-in)).
Add `--json` to any of them for output a program can read.

```sh
slk workspace whoami          # who am I signed in as?
slk workspace list --json     # every configured workspace, as JSON
slk help                      # every command
```

This is a new surface ([#107](https://github.com/gammons/slk/issues/107)). It
grows a few commands at a time; `slk help` always lists what your build has.

## Choosing a workspace

A command that talks to Slack needs one workspace. slk takes the first of:

1. `--workspace <ref>`, where `<ref>` is the workspace's slug, team ID, name or
   `<domain>.slack.com` subdomain, in any case;
2. the `SLK_WORKSPACE` environment variable, the same forms;
3. `default_workspace` under `[general]` in `config.toml`, the same setting that
   picks the TUI's first workspace (see [[Configuration]]);
4. the only workspace, when you have one.

With several workspaces and none of these, the command stops with exit code 2
and lists them.

## Output

Without `--json`, commands print text for people. Its layout can change between
releases, so do not parse it.

With `--json`, stdout gets exactly one JSON document: an object whose field
names the result, such as `{"workspaces": [...]}`. Field names are snake_case,
IDs are always included, and an empty list is `[]`, never `null`. New fields can
appear in any release; existing ones are never renamed or removed.

A failure under `--json` writes one JSON document to stderr instead, and nothing
to stdout:

```json
{
    "error": {
        "code": "workspace_required",
        "message": "more than one workspace is configured; choose one with --workspace or SLK_WORKSPACE, or set default_workspace under [general] in config.toml",
        "slack_error": "",
        "candidates": [
            { "id": "T0123ABCD", "name": "Example Inc" },
            { "id": "T0456EFGH", "name": "Side Project" }
        ]
    }
}
```

`code` comes from a fixed list, and `slack_error` carries Slack's own error
string when Slack refused the request. `slk skill` documents every code.

## Exit codes

| Exit | Meaning                                                                       |
| ---- | ----------------------------------------------------------------------------- |
| 0    | Success                                                                       |
| 1    | Failure: the network, Slack, Ctrl-C, or anything unexpected                   |
| 2    | Usage: an unknown command or flag, a missing argument, or no workspace chosen |
| 3    | Authentication: no workspace configured, or Slack refused the saved login     |
| 4    | Not found: a reference matched nothing                                        |

A word slk does not know as a command is a usage error (exit 2). Before the CLI
existed, `slk <anything>` started the TUI.

## Signing in

The commands use the logins `slk --add-workspace` saved. When Slack refuses one
(an expired session), slk first takes a newer login that another slk process
(the TUI, another command) saved since, if Slack accepts it. If there is none,
it refreshes the login once from the Slack desktop app, the way the TUI does
when it starts, and tries again. That refresh reads the desktop app's key from
your keychain (or keyring), so your OS can ask you to allow it, and the command
waits until you answer. A login that works is never refreshed, so this happens
only after one expired. The refreshed login is saved, so a running TUI whose
login Slack also refused reconnects with it.

A login added with `--add-workspace --browser` cannot be refreshed that way, and
slk does not touch the keychain for it: add it again when it expires.

## Commands

### `slk workspace list`

The configured workspaces, in the order of the workspace rail. It reads local
files only, so it works offline. `slk --list-workspaces` prints the same table.

```sh
$ slk workspace list --json
{
  "workspaces": [
    {
      "team_id": "T0123ABCD",
      "slug": "work",
      "name": "Example Inc",
      "domain": "example",
      "default": true
    }
  ]
}
```

`slug` is the workspace's `[workspaces.<slug>]` key in `config.toml`, or `""`
when it has none. `default` marks the workspace commands use when you choose
none.

### `slk workspace whoami`

Signs in and shows the workspace and the user that commands act as. Run it
first: exit code 3 means the login does not work.

```sh
$ slk workspace whoami
Workspace: Example Inc (T0123ABCD), example.slack.com
User:      alice (U0456EFGH)
```

With `--json`:
`{"workspace": {...}, "user": {"id": "U0456EFGH", "name": "alice"}}`, where
`workspace` has the same fields as in `workspace list`.

### `slk skill`

Prints an agent skill: a `SKILL.md` file (Markdown with `name` and `description`
frontmatter, the Agent Skills format) that teaches an AI agent these commands,
the workspace rules, the JSON contract and the error codes. It always matches
the binary you run. To install it for an agent that reads skills from a
directory, such as Claude Code:

```sh
mkdir -p ~/.claude/skills/slk && slk skill > ~/.claude/skills/slk/SKILL.md
```

### `slk help`

`slk help` lists the commands, `slk help <noun>` a noun's verbs, and
`slk help <noun> <verb>` (or `--help` after any command) its flags.

## Caveats

slk is an unofficial client: the commands use the same session and the same
internal protocol as the TUI (see
[[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]]). Scripts that run many
commands in a loop make your account look unlike a person using Slack. Keep
loops slow, and run commands one at a time.
