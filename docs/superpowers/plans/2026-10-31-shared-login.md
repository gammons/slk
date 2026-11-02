# Shared Login Implementation Plan

**Goal.** Every slk process uses one login per workspace, the one in the token
store, and gets a new login through one path. A refresh by one process no longer
leaves another process with a refused login. This mitigates risk R2 of the
scriptable CLI plan (`2026-10-30-scriptable-cli.md`). It does not resolve R2
yet: the TUI's side rests on one assumption about Slack that is not checked (see
Limits).

**Base.** Stacked on stage 1 of the scriptable CLI (gammons/slk#412).

## Problem

- Every slk process reads the token store (`~/.local/share/slk/tokens/`) when it
  starts, and then keeps its own copy of the login in memory.
- The TUI refreshes the login only at launch (`remintTokens`). During a session,
  `ConnectionManager.Run` reconnects with the same token, with no end. If Slack
  refuses the login (the user signed out of the desktop app and in again, or the
  session was ended), the TUI stays disconnected until a restart. This happens
  even when a CLI command already saved a login that works. `wiki/Setup.md` said
  that slk refreshes tokens "mid-session if needed". No code did that.
- The CLI refreshes a refused login (stage 1), and a running TUI never sees the
  result. R2 asks whether the CLI's refresh can end the TUI's session. In the
  normal path, the refresh copies the token that the desktop app already uses
  (`slackdesktop.Tokens`), so it starts no new session. Only the fallback,
  `MintToken`, loads the workspace page, as a browser tab does. The code cannot
  answer R2; the manual check can. With this plan, a TUI whose login is refused
  takes the login that another process saved, if its reconnect loop sees the
  refusal (Limits).

## Decisions

- **S1. One path.** `loginRefresher.refresh(ctx, failed)` (`cmd/slk/login.go`)
  first reads the token store: a login there that was not refused means that
  another process refreshed it, and taking it needs no keychain read. Else it
  asks the desktop app (`desktopLogin`, which saves nothing). The CLI
  (`connectWithRemint`) and the TUI (`ConnectionManager.Relogin`) both use it.
  `slk export` and the TUI launch keep their eager refresh (`remintToken`,
  `remintTokens`); they save to the store, so the other processes find the
  result.
- **S2. Every candidate is checked before it is used or saved.** `refresh` signs
  in with a candidate on a client of its own (auth.test, `identifyLogin`) before
  it returns it. Only a desktop login that passed the check is saved to the
  store: a login that Slack refuses, or that the caller rejects as another
  account's, never reaches the store that the other processes sign in with. It
  is saved only over the entry `refresh` read (`TokenStore.SaveIf`), because the
  check takes time: a workspace that the user added again (as another user too)
  or removed meanwhile is kept as the user left it. A replacement goes through
  the check like any stored login (`reconsider`), so the TUI asks for a restart
  when it belongs to another user. When Slack refuses the replacement, or cannot
  be asked about it, the caller keeps the desktop login that passed: nothing
  else holds that login, so ending the search would lose it. Only a caller that
  gave up (cancelled, deadline) gets that cause instead. A candidate that Slack
  refuses does not end the search: after a refused stored login, the desktop app
  is asked. So the CLI's one retry is not spent on a login that Slack refuses.
  An error that is not a refusal (no network, cancelled, another account) ends
  the search; a later refresh searches again.
- **S2a. The CLI reports why a refresh stopped.** `connectWithRemint` returns
  the error that stopped the refresh (cancelled, no network, a timeout) instead
  of the first refusal, so a cancelled command exits 1 as `cancelled`. Only
  `errNoNewerLogin` leaves the refusal as the answer (`auth_failed`, exit 3).
- **S3. History per login.** `refresh` remembers every refused login (token and
  d cookie) and never chooses one again, even when a stale copy comes back into
  the store. It asks the desktop app at most once per refused login that Slack
  answered about: a desktop read can show a keychain prompt, and a login that
  Slack keeps refusing must not show one on every reconnect attempt. When Slack
  gave no answer for the desktop's login, the desktop app is asked again after
  the next refusal, because that login was not saved. The store is still read on
  every attempt, so a login that another process saves later is found.
- **S4. The TUI keeps its account.** The TUI's check (`sameAccountAs`) requires
  the team and the user that Slack returns to be the ones the workspace
  connected as. The user, the conversations and everything else the TUI built
  belong to that account, so a login of another account (the workspace was added
  again as someone else) is never installed. The TUI shows a toast that asks for
  a restart, and stops trying for the session. The status bar does not fit a
  toast to the width that is left, so the toast starts with the instruction and
  is short (`otherAccountNotice`); a rendered 80-column test checks that the
  instruction is on screen. The CLI's check (`acceptedLogin`) takes any account
  that Slack accepts: a command builds all it shows from the account it signs in
  as.
- **S5. Detection in the reconnect loop.** After a failed connect,
  `Client.CheckLogin` (auth.test) and `IsAuthError` tell a refused login from a
  network failure. Only a refused login calls `Relogin`. When `Relogin` sets a
  new login, `Run` connects again at once instead of waiting for the backoff.
- **S6. The login changes in place.** `Client.SetLogin` replaces the token, the
  d cookie, the slack-go client and the d cookie in the jar under one lock
  (`loginMu`). Everything wired to the client keeps its pointer and sends the
  new login. `applyLogin` also gives the login to the edge client
  (`edge.Client.SetToken`) and to the file credentials of the image fetcher and
  the file downloader (`SetTeamAuth`). A foreign team (Slack Connect) that
  learned a workspace's credentials records the workspace's team ID, not a copy
  of the credentials, so a download that started before a refresh cannot bring
  the old credentials back when it finishes.
- **S7. A check, not a comment.** A `*Client` method reads the guarded fields
  only through `login()` and `webAPI()`.
  `TestClient_ReadsTheLoginOnlyThroughItsAccessors` parses the package and
  enforces it.

## Limits

- The loop assumes that Slack refuses the WebSocket handshake of a refused
  login. If a handshake succeeds and Slack then closes the socket, the loop does
  not check the login, and the TUI does not recover. I could not test this
  against Slack. Until the manual check confirms the assumption, R2 is
  mitigated, not resolved. If Slack closes the socket instead, the loop must
  also check the login after a socket session that ends without Slack's `hello`.
- A request that Slack refuses with `invalid_auth` while the socket is still up
  starts no relogin. The next reconnect does.
- After the desktop app was asked once with no result, a running TUI finds a new
  desktop session only through the store: a CLI command that refreshes the
  login, or a restart.
- Every change to the token store (`Save`, `SaveIf`, `Delete`) holds the store's
  lock, a `.lock` file in the tokens directory that the OS locks (`flock`,
  `LockFileEx`) and releases when its process ends. Readers take no lock: atomic
  writes give them whole files. Two processes that refresh at the same moment
  both read the desktop app; the second one's `SaveIf` finds the first one's
  login and keeps it. The history in S3 is per process.
- The TUI launch and `slk export` still save with `Save`, not `SaveIf`: their
  eager refresh (stage 1 and earlier) does not wait for a check, so they can
  still replace an entry that another process changed a moment before.
- Each workspace refreshes on its own. When the desktop app signs out and in
  again, every workspace of that session is refused, and each reads the desktop
  app once. The TUI launch reads it once for all of them.
- The account notice is a toast: it shows once, for three seconds. After it,
  only the status bar's disconnected state shows that the workspace needs a
  restart.

## Tests

- `internal/slack`: `TestSetLogin_EveryLaterRequestCarriesTheNewLogin` (the
  slack-go client, the form posts, the jar and the WebSocket URL),
  `TestSetLogin_WhileRequestsRun` (race detector),
  `TestCheckLogin_ReturnsTheAccountOrARefusal`, `TestLoginReplaced`,
  `TestLoginReplaced_CancelEndsTheCheck`,
  `TestConnectionManager_ReconnectsWithARefreshedLogin`,
  `TestClient_ReadsTheLoginOnlyThroughItsAccessors`, `TestTokenStore_SaveIf`,
  `TestTokenStore_SaveWaitsForTheLock`.
- `internal/slack/edge`: `TestSetToken_TheNextRequestCarriesTheNewToken`.
- `internal/slackhttp`: `TestAuthResolverSetTeamAuthReplacesTheTeamEverywhere`,
  `TestAuthResolverSetTeamAuthIgnoresUnknownTeamsAndEmptyTokens`,
  `TestAuthResolverLearnAfterSetTeamAuthKeepsTheFreshAuth`,
  `TestAuthResolverLearnIgnoresAnUnregisteredWorkspace`.
- `internal/image`: `TestFetcherSetTeamAuthReachesItsResolver`.
- `cmd/slk`: `TestLoginRefresher_*` (with
  `TestLoginRefresher_SavesADesktopLoginOnlyAfterItPassed`,
  `TestLoginRefresher_TakesALoginAddedAgainDuringTheCheck`,
  `TestLoginRefresher_KeepsTheCheckedLoginWhenTheReplacementCannotBeChecked`,
  `TestLoginRefresher_ACancelDuringTheReplacementCheckIsReturned` and
  `TestLoginRefresher_DoesNotBringBackAWorkspaceRemovedDuringTheCheck`),
  `TestWorkspaceRelogin_KeepsAWorkspaceAddedAgainDuringTheCheck` (a browser
  login of another user saved while the desktop app's login is checked; on a
  real token store),
  `TestConnectWithRemint_ARefusedStoredLoginFallsBackToTheDesktopApp`,
  `TestCLISignIn_ARefreshThatStopsKeepsItsCause` (through `cli.Main`:
  `cancelled`, `failed`, `auth_failed`), `TestSameAccountAs`,
  `TestWorkspaceRelogin_*` (with
  `TestWorkspaceRelogin_NeverInstallsAnotherAccount` and
  `TestWorkspaceRelogin_AnotherAccountFromTheDesktopAppIsNotSaved`, on a real
  token store), `TestOtherAccountNoticeShowsItsInstructionAt80Columns`,
  `TestApplyLogin_GivesTheLoginToEveryHolder`.

## Manual check

1. Start slk.
2. Sign out of the Slack desktop app, then sign in again.
3. Wait until the TUI's status bar shows that it is connected again.
4. If the TUI stays disconnected, run `slk workspace whoami` in a second
   terminal. The TUI must reconnect within 30 seconds.
5. If it still stays disconnected, run slk with `SLK_DEBUG=1` and look in
   `slk-debug.log` for `workspace ...: Slack refused the login`. No such line
   means that the socket opened and then closed: the assumption in Limits is
   wrong.
