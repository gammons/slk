# Channel export to Markdown

Status: implemented
Date: 2026-09-17

## Goal

`S` saves one open thread. This adds the bulk counterpart: export one
channel's messages and threaded replies for a date range, from the command
line, without launching the TUI.

```sh
slk export \
  --workspace example-workspace \
  --channel project_alpha \
  --since 2026-04-01 \
  --until 2026-07-01 \
  --timezone America/New_York \
  --output ./project-alpha-export \
  --overlap 14
```

## Behaviour

- **Window.** `--since` is inclusive, `--until` exclusive; both are calendar
  dates read as midnight in `--timezone` (IANA name, default local).
  `--until` defaults to tomorrow, so the export runs through today.
- **Overlap.** `--overlap N` (default 0) moves the start N calendar days
  earlier and the end N calendar days later. Calendar days, not `N*24h`, so
  the bounds stay at local midnight across DST changes. The widened window is
  what messages are tested against; the requested dates are still what the
  index and the default output folder are named for.
- **Conversations.** A conversation is a top-level message plus its replies.
  Every top-level message in the window is one conversation, threaded or not.
  Only replies inside the window are included.
- **Older parents.** A reply inside the window is exported even when its
  parent predates the window. That parent is kept as the first message of the
  conversation and labelled as context, in the file and in the index. It does
  not count toward the index's message total.
- **Output.** One Markdown file per conversation, named
  `<local date>-<local time>-<ts sequence>.md`, and an `index.md` with a
  summary header and the conversations linked under a heading per day.
  Message bodies go through `export.ThreadToMarkdown`, so a channel export and
  a thread saved with `S` render identically.
- **Output directory.** Must be empty or absent, and its parent must be
  writable. An export never overwrites or mixes into an earlier one. The
  files are staged in a sibling `<dir>.partial-*` directory and moved into
  place only once all of them are written, so a failed write leaves the
  directory empty and the command can simply be re-run. Both directories
  are created before fetching anything, so a directory problem fails the
  command at once rather than after the fetch.
  Default: `<exports dir>/slk-channel-<channel>-<since>-to-<until>`.
- **Workspace and channel.** `--workspace` matches the config slug, team ID,
  team name or domain, case-insensitively, and is optional with a single
  workspace. `--channel` is a name (leading `#` allowed) or an ID; a channel
  the user has not joined can only be given by ID.

## Finding older threads

Slack has no query for "threads with activity in this period", and a parent of
any age can receive a reply. The only complete answer from documented
endpoints is to walk `conversations.history` for everything before the window
and keep thread parents whose `latest_reply` is at or after the window start.

This was chosen over two cheaper alternatives:

- **A bounded lookback** (scan only the last N days before the window) silently
  drops replies to older threads, which is exactly the case the feature exists
  to cover.
- **`search.messages`** with `in:`/`after:`/`before:` returns replies directly,
  but its date modifiers are day-granular in the account's own timezone, it is
  capped at 100 pages, and its completeness depends on search indexing and the
  user's search preferences.

The cost is one history request per 200 messages of earlier history on every
export. `latest_reply` keeps the reply requests down: a thread that went quiet
before the window is ruled out without one. A thread whose latest reply is
*after* the window cannot be ruled out that way, so it is fetched and dropped
if nothing lands in range.

Both walks stream pages to a visitor instead of accumulating them, since the
span can be a channel's entire history.

## Structure

| Piece | Location |
|---|---|
| Window parsing and `Contains` | `internal/export/window.go` |
| Markdown files and index | `internal/export/channel.go` |
| `WalkHistory`, `GetRepliesBetween`, `WaitOutRateLimit` | `internal/slack/history_range.go` |
| Conversation collection, conversion, name resolution | `cmd/slk/export_collect.go` |
| Flags, workspace and channel selection, orchestration | `cmd/slk/export_command.go` |

Collection lives in `cmd/slk` because it needs the `slack.Message` to
`MessageItem` conversion helpers that already live there (`messageAuthor`,
`extractAttachments`, `extractBlocks`). It reaches Slack through two small
consumer-side interfaces, so it is tested against an in-memory fake.

Credentials are the stored workspace token, re-minted from the desktop app the
same way the TUI does at launch. The command calls `Connect` (auth.test) but
never opens the WebSocket. User names come from the SQLite cache first, then
`users.info`; an ID that cannot be resolved stays as the raw ID, and a warning
naming the ID and the lookup error goes to stderr so a network failure is not
mistaken for a bot or deleted user. The cache is read-only here, and the
export proceeds without it if it cannot be opened.

Both bulk reads and the name lookups wait out rate limits rather than failing,
and honour context cancellation while waiting.

Flags use the standard library `flag` package. The rest of the CLI is a
hand-rolled `os.Args` switch; one subcommand does not justify a CLI framework
dependency.

## Known limitations

- Message bodies carried only in Block Kit blocks or legacy attachments (many
  bot posts) export as author and timestamp with an empty body. This is
  `ThreadToMarkdown`'s existing behaviour and applies to `S` too; fixing it
  needs a plain-text Block Kit renderer and belongs in its own change.
- System messages (joins, leaves, topic changes) are exported like any other
  top-level message.
- Files are linked, not downloaded.
