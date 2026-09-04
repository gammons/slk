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
