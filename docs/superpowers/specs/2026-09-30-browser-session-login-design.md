# Browser-session login

## Problem

`slk --add-workspace` only reads the Slack desktop app's session. Without the
desktop app (servers, containers, SSH sessions, browser-only users), or without
a keyring to decrypt its cookie, there is no way to add a workspace. The
previous flow (paste the `d` cookie and the `xoxc` token) is gone, while
getslk.sh/install still documents it (issue #141).

## Design

- `slk --add-workspace --browser` runs the browser-session flow directly.
- When `slackdesktop.Cookie()` or `slackdesktop.Workspaces()` fails and stdin is
  a terminal, `--add-workspace` prints the usual desktop error, then asks
  "Sign in with a browser session instead?" (default yes). Non-interactive runs
  keep returning the desktop error unchanged.
- The user pastes a "Copy as cURL" of any request to a Slack `/api/` endpoint.
  It carries the `xoxc` token (form body) and the `d` cookie (`-b` or a
  `Cookie:` header), the latter being HttpOnly and unreachable from a DevTools
  console snippet. A bare `xoxc-` token also works; the `d` cookie is then
  asked for.
- The paste is read with the terminal in raw mode, without echo, until the
  command is complete: a line that does not end with a continuation (`\` bash,
  `^` cmd, a backtick for PowerShell, whose `$session` lines only end once its
  `Invoke-WebRequest` does and whose `-Headers @{ ... }` block only ends at its
  `}`), or Ctrl-D; Ctrl-C cancels. Canonical mode would
  truncate lines past 4095 bytes, which a cURL line with its cookies can reach.
- Whatever is still queued once reading stops (the tail of a paste, keys typed
  meanwhile) is flushed from the terminal input until the input has been
  quiet for 500 ms (5 s at most), so no token or cookie is left for the shell
  to read, even when a paste arrives in chunks over SSH. The terminal is
  restored by a defer, and on SIGINT / SIGTERM / SIGHUP.
- The cmd form's caret escapes are stripped before parsing; the PowerShell
  form's cookie is read from its `System.Net.Cookie("d", ...)`. A cookie pasted
  decoded (`xoxd-AbC/def+Ghi==`) is URL-encoded, as the desktop flow stores it.
- The pair goes through `Client.Connect` (auth.test), which yields the team ID,
  name (new `Client.TeamName`) and subdomain; the token is saved with the same
  `saveWorkspace` helper as the desktop flow, so the token file and the
  `[workspaces.<slug>]` block are identical. Re-adding a team already in
  `config.toml` only refreshes its token: a second block for the same
  `team_id` would make `config.Load` reject the file (the general fix for the
  desktop flow is #148). A `config.toml` that already fails to load is left
  alone: one more block cannot fix it.

## Limits

Startup re-minting needs the desktop cookie; `remintTokens` already keeps the
cached token when it cannot read it, so a browser-session token lasts as long
as the browser session. The success message says to run the command again when
Slack signs it out.

## Tests

`parseBrowserSession` (Chrome and Firefox shapes, `d-s` not taken for `d`, bare
token, bare cookie), `pasteComplete`, and `readPaste` (multi-line continuation,
a 10 KB line, Ctrl-C, Ctrl-D, EOF). `TestConnect_DiscoversStandardWorkspaceAPIBaseURL`
now also checks `TeamName` and `TeamSubdomain`.
