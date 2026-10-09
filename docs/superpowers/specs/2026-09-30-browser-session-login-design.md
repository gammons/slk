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

A browser-session token is saved with `source = "browser"` and `remintTokens`
skips it: the desktop app can hold the same workspace under another account,
and re-minting would replace the chosen identity with that one. So it lasts as
long as the browser session; `slk --add-workspace` (the desktop flow) saves a
token without the mark and re-minting resumes; its picker lists such a team
without pre-selecting it, so that is a deliberate choice. Without a desktop app,
`remintTokens` already kept the cached token. The success message says to run the command again when
Slack signs it out.

## Tests

- `parseBrowserSession`: the Chrome bash, cmd and PowerShell shapes, Firefox,
  a PowerShell form with the cookie in its headers, `d-s`, `xd` and `x-d` not
  taken for `d`, a bare token, a bare cookie encoded or decoded.
- `pasteComplete` and `readPaste`: each continuation, the `$session` and
  `-Headers @{ ... }` rules, every shape read whole and not past its end, a
  10 KB line, Ctrl-C, Ctrl-D, EOF.
- `drainInput`, against a fake terminal: nothing pending, a tail arriving
  late, input that never stops (bounded at 5 s).
- `browserLogin.run`, with its read, auth and save injected: no token (Slack
  never called), a bare token then the cookie, no cookie, a full cURL, Slack
  refusing.
- `offerBrowserFallback`: not a terminal (never asks, returns the desktop
  error itself), declined, accepted.
- `addWorkspaceArgs`: `--browser` in either order, alone, a misspelt argument.
- `saveWorkspace`: the same team twice (one block, the config loads, the token
  is refreshed), and a `config.toml` that already fails to load (left alone).
- `remintTokens`: a browser-session token is left alone (no desktop token, no
  mint, no save) while a desktop one beside it is refreshed.
- Piped input: a token then the cookie on its own line are both found in one
  read.
- `desktopChoices`: a team held as a browser session is listed, labelled and
  not pre-selected; the others are pre-selected as before.
- `TestConnect_DiscoversStandardWorkspaceAPIBaseURL` also checks `TeamName`
  and `TeamSubdomain`.

Not covered by `go test`, since they need a pty: the flush itself, and the
flush on a signal. Both were checked by driving the binary through a pty with
a second reader on the terminal after slk exits.
