# Setup

The easiest way to sign in is to let slk read your session from the **Slack
desktop app** — no Slack App, no admin approval, no OAuth flow, and no tokens to
copy. Without the desktop app, you can use a browser session or copy an existing
slk login from another machine (see below).

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
session), or without a system keyring to decrypt its session, you can
[copy a login from another machine](#copy-a-login-to-another-machine) or sign in
from your browser session. slk offers browser sign-in when it cannot read the
desktop app, or you can ask for it:

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

Such a token is not re-minted on launch: it lasts as long as the browser
session. That holds even if the desktop app is installed later, since it may
be signed in to the same workspace as someone else; to go back to the desktop
session, run `slk --add-workspace` and tick the workspace: it is listed as a
browser session and left unticked, so that adding another workspace does not
replace it by accident. When Slack signs it out,
run `slk --add-workspace --browser` again: it refreshes the token and leaves
your `config.toml` as it is.

## Copy a login to another machine

Already using slk elsewhere? Transfer one workspace login without installing the
Slack desktop app or extracting browser credentials on the destination.

**The transfer file contains plaintext credentials: both the `xoxc-` token and
`d` cookie. Treat it like a password.** Use SSH or another encrypted channel;
never paste its contents into chat, logs or a repository.

On the **source machine**, replace `acme` with your workspace and `destination`
with your SSH host:

```bash
slk auth export --workspace acme --output "$HOME/acme-auth.json"
scp "$HOME/acme-auth.json" destination:acme-auth.json
```

Wait for export to succeed before copying. On the **destination machine**:

```bash
chmod 600 "$HOME/acme-auth.json"
slk auth import --input "$HOME/acme-auth.json"
slk workspace whoami --workspace T0123ABCD
```

Replace `T0123ABCD` with the workspace ID printed by import. Import works on a
fresh installation and checks the supplied login with Slack before saving it.
It takes the workspace from the file, not destination defaults; do not pass
`--workspace` to import. Export is only an offline snapshot: it does not refresh
the login or check whether Slack still accepts it.

Both commands require file paths (`-` is not supported). Export requires an
existing trusted parent directory and refuses to overwrite any existing file.
On POSIX, export creates an owner-only file (`0600`); import refuses symlinks,
nonregular files and group/world-accessible credentials. Keep transfer and token
storage directories private. On Windows, use a private user directory with
restrictive inherited ACLs; POSIX mode bits are not an ACL guarantee.

After a successful import, check API, live updates and file access on the
destination, then delete the transfer file on **both machines**
(`rm "$HOME/acme-auth.json"` on each). Deleting files is not guaranteed secure
erasure on SSDs or backups.

Import copies, rather than moves or revokes, the source session. **Both copies
share its lifetime**: expiry, revocation or workspace policy can invalidate both.
Imported logins are never automatically replaced by the destination's desktop
session, even if Slack is installed later. They are labelled as imported and
left unticked in `slk --add-workspace`; replacing one there requires an explicit
selection. On expiry, export/import a fresh login or explicitly sign in again.

If the destination already has that workspace, import refuses unless you
explicitly replace the login for the **same account**:

```bash
slk auth import --input "$HOME/acme-auth.json" --replace
```

`--replace` cannot switch users. slk compares the verified account IDs; for a
legacy login without a saved verified ID, it checks that exact existing login
with Slack first. If the old login is expired or cannot be identified, replacement
fails closed: restore a verifiable login for that account or use a separate
profile. For a different account, set a separate `XDG_DATA_HOME` for import and
every subsequent slk invocation, keeping tokens and cached private data isolated.
Removing a workspace or restarting alone does not clear or isolate its cache.

Only the login is transferred: no `config.toml`, workspace slugs/defaults, cache,
drafts or marks. Import does not run onboarding or change your config. **Restart
any running slk TUI** to use the newly imported workspace/login. Failure to print
the success message does not undo an export or import; export still refuses an
existing file on rerun, and import may require same-account `--replace`.

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

For desktop-backed logins, slk re-mints tokens automatically from the Slack
desktop app on each launch, and again while it runs if Slack refuses a login.
Browser and imported logins are not re-minted: renew them with explicit sign-in
or a fresh export/import when their source session expires.

Every slk process (the TUI, and each `slk` command) shares the logins saved in
`~/.local/share/slk/tokens/`. When Slack refuses a login, slk first takes a
newer one that another slk process saved there, if Slack accepts it. Only for a
desktop-backed login, when there is none, does it ask the desktop app, which can
show a keychain prompt. A running TUI asks the desktop app once per refused
login, not on every reconnect attempt. It also takes only a login of the account it is signed in as: if the
workspace was added again as someone else, it asks you to restart slk to
switch.

If a desktop-backed login expires after you sign out of the desktop app, sign
back in — slk picks the session back up the next time it re-mints: at the next
launch, or when any `slk` command refreshes the login, which a running TUI then
takes over. See the
auth caveat in [[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]].

## Enterprise Grid

Desktop sign-in reuses the **desktop app's** existing session (the same session
your admin already sanctioned) rather than a browser session, which avoids the
session-anomaly alerts that browser-token extraction can trigger. If you're on
Enterprise Grid and still hit a sign-out or security alert after adding a
workspace, please file an issue — include your OS and Slack desktop version.
See [#5](https://github.com/gammons/slk/issues/5) for history.
