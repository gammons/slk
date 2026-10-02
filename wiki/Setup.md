# Setup

slk reads your session directly from the **Slack desktop app** — no Slack App,
no admin approval, no OAuth flow, and no tokens to copy. The only requirement is
that the Slack desktop app is installed and you're signed in to it.

## 1. Sign in to the Slack desktop app

Install the Slack desktop app if you haven't already, and sign in to each
workspace you want to use in slk. Native packages, flatpak
(`com.slack.Slack`), and snap installs are all detected on Linux.

## 2. Add your workspaces

```bash
slk --add-workspace
```

Or just run `slk`. Onboarding launches automatically when no workspaces are
configured.

slk detects the workspaces you're signed in to in the desktop app and shows
them in a list. Select the ones you want (all are selected by default) and
you're done.

## Linux secret stores

Slack encrypts its session cookie with a key it keeps in your desktop's secret
store, and Electron picks that store from the desktop session: **KWallet** on
KDE Plasma, **gnome-keyring** (or any other Secret Service provider) elsewhere.
slk reads both, so either one works and there is nothing to configure.

Two cases still need you:

- **"Your system keyring is locked"** — unlock it. On Plasma this means opening
  your wallet in KWallet Manager; PAM normally unlocks it at login, so a locked
  wallet usually means you changed your login password without changing the
  wallet's.
- **"No Slack entry found in your keyring or KWallet"** — Slack stored no key
  at all. This happens when it was launched with `--password-store=basic`, which
  falls back to a hardcoded password instead of a real secret store. Relaunch
  Slack without that flag and sign in again.

The first time slk reads KWallet, KWallet may ask you to grant access to an
application called `slk`. Allow it once and it is remembered.

## Without the desktop app

On a machine without the Slack desktop app (a server, a container, an SSH
session), or without a system keyring to decrypt its session, slk can sign in
from your browser session instead. It offers this on its own when it cannot
read the desktop app, or you can ask for it:

```bash
slk --add-workspace --browser
```

1. Open https://app.slack.com in your browser and sign in.
2. DevTools > Network, filter on `api/`, click a channel, then right click one
   of the requests > Copy > Copy as cURL (the bash, cmd and PowerShell forms
   all work).
3. Paste it at the prompt and press Enter. Nothing is echoed.

The copied request carries both values slk needs: the `xoxc-` token and the
`d` cookie, which is HttpOnly and cannot be read from the DevTools console. A
bare `xoxc-` token also works; slk then asks for the `d` cookie (DevTools >
Application > Cookies). The pair is checked with `auth.test` and saved like any
other workspace.

Such a token is not re-minted on launch, since there is no desktop cookie to
mint from: it lasts as long as the browser session. When Slack signs it out,
run `slk --add-workspace --browser` again: it refreshes the token and leaves
your `config.toml` as it is.

## Removing a workspace

```bash
slk --remove-workspace
```

Interactive picker. This deletes the saved token from
`~/.local/share/slk/tokens/`; your `config.toml` and SQLite cache are left
untouched.

## Multiple workspaces

You can add as many workspaces as you like by running `slk --add-workspace`
again. They all stay connected in parallel for live unread badges. Use
`:ws` for the picker, or `1`–`9` to jump directly. Configure rail order
and per-workspace settings in [[Configuration]].

## Token expiry

You don't need to do anything when a token expires. slk re-mints tokens
automatically from the Slack desktop app on each launch (and mid-session if
needed), so sessions stay fresh on their own.

If you ever sign out of the desktop app, just sign back in — slk will pick the
session back up the next time it needs to re-mint. See the auth caveat in
[[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]].

## Enterprise Grid

slk reuses the **desktop app's** existing signed-in session (the same session
your admin already sanctioned) rather than a browser session, which avoids the
session-anomaly alerts that browser-token extraction can trigger. If you're on
Enterprise Grid and still hit a sign-out or security alert after adding a
workspace, please file an issue — include your OS and Slack desktop version.
See [#5](https://github.com/gammons/slk/issues/5) for history.
