# Setup

slk uses your existing Slack browser session — no Slack App, no admin
approval, no OAuth flow. You'll need two things from the browser: your
`xoxc` token and the `d` cookie.

## 1. Log into Slack in your browser

Open [https://app.slack.com](https://app.slack.com) and sign into your
workspace. Use the browser version of Slack (not the desktop app).

## 2. Grab your browser tokens

### The `d` cookie

- Open DevTools (F12 / Cmd+Option+I)
- Go to **Application → Cookies → `https://app.slack.com`**
- Copy the value of the cookie named `d`

### The `xoxc` token

In the DevTools **Console**, run:

```javascript
Object.entries(JSON.parse(localStorage.localConfig_v2).teams).forEach(([id,t]) => console.log(t.name, t.token))
```

Copy the `xoxc-…` token for the workspace you want.

## 3. Add the workspace

```bash
slk --add-workspace
```

Or just run `slk`. Onboarding launches automatically when no workspaces are
configured.

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
`Ctrl+w` for the picker, or `1`–`9` to jump directly. Configure rail order
and per-workspace settings in [[Configuration]].

## Token expiry

Browser-cookie tokens expire when you log out of the browser or Slack
rotates them. Re-run `slk --add-workspace` and you're back in business.
See the auth caveat in [[Tradeoffs and Non-Goals|Tradeoffs-and-Non-Goals]].
