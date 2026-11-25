# App modals — research handoff

**Issue:** [#247](https://github.com/gammons/slk/issues/247) (slash commands), the
part deliberately left out of the slash-commands spec
**Date:** 2026-10-12
**Status:** The read-only part is implemented; see
`2026-11-25-app-modals-design.md`. Forms, the view stack and images inside a
modal remain out of scope. This note stays as the wire reference.

## Why this exists

Slack apps open **modals** (Slack calls them "views"): forms built from Block Kit
blocks, opened by the server, filled in by the user, submitted back. Bare
`/giphy` (no search term) is one, and so is any app's "open a form" button. The
slash-commands spec (stages 1–3: `chat.command`, Block Kit buttons on
ephemerals, the `/` picker) leaves them out and shows a notice when a modal
opens. This note keeps what the 2026-10-12 captures showed about modals so the
next spec starts from evidence, not guesses.

**Read first:**

- The slash-commands spec, `2026-10-12-slash-commands-design.md`, and its
  scrubbed captures in `2026-10-12-slash-commands-captures/` — it adds `blocks.actions` for buttons in
  messages, extends `core.ActionElement` with `action_id` / `block_id` /
  `value`, and adds the `view_opened` notice that a modal implementation
  replaces.
- `docs/superpowers/specs/2026-10-10-ephemeral-action-dialog-design.md`,
  "Out of scope → Interactive Block Kit → 2. Modals" — the original sketch:
  a form component, a renderer for view payloads, rendered blocks inside a
  dialog via host-rendered lines.
- RFC [#236](https://github.com/gammons/slk/issues/236) and the owner's answers
  in its comments: a new widget lives in `internal/bubbles/` in the RFC shape
  (`New(deps, opts...)`, own `Styles`/`KeyMap`), Block Kit rendering is
  injected by the host as an adapter, and no stage may add a parallel
  implementation of something that exists.

## Scope warning: this is where "rebuilding Block Kit" lives

slk already **renders** Block Kit layout blocks read-only (`header`, `divider`,
`section`, `context`, `image`, `actions`, `rich_text` in
`internal/ui/messages/blockkit/parse.go`). Interactive elements are parsed only
to an inert label (`ActionElement{Kind, Label}`). Modals require each
interactive element to become an editable widget with state, plus validation,
a view stack and submission. Every app uses a different mix of elements, so
this has no natural end.

**Recommendation carried from the brainstorm:** support an explicit whitelist
of elements and treat any view containing anything else as unsupported ("this
dialog uses features slk can't show — open it in Slack"). The giphy flow needs
only:

| Element | Where giphy uses it |
|---|---|
| `input` block + `plain_text_input` | the search term |
| `section` with a `button` accessory | Shuffle |
| `image` block (animated GIF) | the preview |
| `context` with `mrkdwn` + `image` elements | the "Searching…" spinner view |
| view `submit` / `close` buttons | Search / Share / Cancel / Back |

`static_select` is the obvious next candidate, but no capture here contains
one.

## Captures

Three HAR files from the Slack web client (Chrome), captured by the owner on
2026-10-12 against a non-Grid workspace. They contain credentials and are
**not** in the repo; the excerpts below are scrubbed (workspace, user, channel,
bot and view IDs replaced by `*0EXAMPLEnn` placeholders, Giphy `cid`
parameters redacted). Giphy's app ID `A0F827J2C` is kept: it is the same in
every workspace (`client.appCommands` reports it with `team_id: ""`).

- `giphy2.har` — in a thread, no reload: HTTP only, no WebSocket frames. Has the
  full modal flow including Shuffle inside the modal.
- `giphy3.har` — full reload, so it has the WebSocket: `view_opened` and
  `view_updated` frames, plus the HTTP calls of a second modal run (no Shuffle).
- `giphy4.har` — local storage cleared: the boot-time `client.appCommands`.

If a fact below is marked **(observed)** it is in a capture; **(docs)** means
Slack's public documentation for the app-side API, which describes the same
objects but not the client's endpoints.

## The flow, end to end (observed)

Bare `/giphy` in a thread, then search "dancer", Shuffle once, Share:

| # | When | Direction | What |
|---|---|---|---|
| 1 | t+0 | client → `chat.command` | `command=/giphy`, no `blocks` (no arguments), `thread_ts` set. Response `{"ok":true,"keep_input":false}` |
| 2 | t+0.1s | WS → client | `view_opened` — carries the **whole view** inline (below) |
| 3 | t+0.3s | client → `views.get` | `view_id` of the opened view; returns the same view. Redundant with (2)'s inline view; the web client fetches anyway |
| 4 | user types "dancer", presses Search | client → `views.submit` | `view_id`, `state` with the input's value, `client_token`. Response `response_action: "push"` with a **new view** (different ID, `previous_view_id` = the search view): a `context` block "*Searching all the GIFs…*" + spinner image, `close: "Back"`, `submit: null`, `notify_on_close: true` |
| 5 | | client → `views.get` | the pushed view's ID |
| 6 | ~0.5s later | WS → client | `view_updated` for the **pushed view's ID** — now the result: section "Not the perfect gif?" + Shuffle button accessory, `image` block (the GIF), `submit: "Share"`, `close: "Back"`, `callback_id: "giphy:share"` |
| 7 | | client → `views.get` | again, after the update |
| 8 | user presses Shuffle | client → `blocks.actions` | `container: {"type":"view","view_id":…}`, `state: {"values":{}}`; response `{"ok":true}` |
| 9 | | WS → client, then `views.get` | `view_updated` with a new GIF (in `giphy2.har` only the `views.get` calls are visible) |
| 10 | user presses Share | client → `views.submit` | `view_id` of the pushed view, `state: {"values":{}}`. Response `{"ok":true,"view":null,"response_action":"clear"}` — close the whole stack |
| 11 | | WS → client | the posted message: `user` = the current user **and** `bot_id` = Giphy's bot, `blocks: [image, context]`, in the thread |

Observed timing (giphy3): `chat.command` 10:49:29.824 → `view_opened`
10:49:29.939 → `views.submit` (push) 10:49:35.278 → `view_updated` 10:49:35.965
→ `views.submit` (clear) 10:49:38.412 → posted message 10:49:38.6.

## Wire shapes (observed, scrubbed)

All requests are `POST https://<workspace>.slack.com/api/<method>` with the usual
`_x_*` query envelope and a multipart body ending in `_x_reason`, `_x_mode=online`,
`_x_sonic=true`, `_x_app_name=client`. slk sends urlencoded bodies everywhere;
that difference is known and accepted (`internal/slackhttp/transport.go`). The
`_x_reason` values seen:

| Method | `_x_reason` |
|---|---|
| `views.get` | `fetchView` |
| `views.submit` | `submit-view` |
| `blocks.actions` (in a view or a message) | `dispatch_action_to_developer` |

### `view_opened` (WebSocket)

```json
{
 "type": "view_opened",
 "view_id": "V0EXAMPLE03",
 "root_view_id": "V0EXAMPLE03",
 "previous_view_id": null,
 "timeout_range": 10000,
 "client_token": "web-1791802169822",
 "title": {"type": "plain_text", "text": "Giphy", "emoji": true},
 "submit": {"type": "plain_text", "text": "Search", "emoji": true},
 "close": {"type": "plain_text", "text": "Cancel", "emoji": true},
 "view_type": "modal",
 "app_id": "A0F827J2C",
 "app_installed_team_id": "T0EXAMPLE01",
 "view": {
  "id": "V0EXAMPLE03",
  "team_id": "T0EXAMPLE01",
  "type": "modal",
  "blocks": [
   {
    "type": "input",
    "block_id": "giphy:search_term",
    "label": {"type": "plain_text", "text": "Search for a gif", "emoji": true},
    "optional": false,
    "dispatch_action": false,
    "element": {
     "type": "plain_text_input",
     "action_id": "search_term",
     "placeholder": {"type": "plain_text", "text": "Search Giphy", "emoji": true},
     "max_length": 1000,
     "dispatch_action_config": {"trigger_actions_on": ["on_enter_pressed"]}
    }
   }
  ],
  "private_metadata": "",
  "callback_id": "giphy:search",
  "state": {"values": {}},
  "hash": "1791802169.OiD6M0Kv",
  "title": {"type": "plain_text", "text": "Giphy", "emoji": true},
  "clear_on_close": false,
  "notify_on_close": false,
  "close": {"type": "plain_text", "text": "Cancel", "emoji": true},
  "submit": {"type": "plain_text", "text": "Search", "emoji": true},
  "previous_view_id": null,
  "root_view_id": "V0EXAMPLE03",
  "app_id": "A0F827J2C",
  "external_id": "",
  "app_installed_team_id": "T0EXAMPLE01",
  "bot_id": "B0EXAMPLE01"
 },
 "channel_id": null,
 "event_ts": "1791802169.005200"
}
```

Notes: `client_token` echoes the `client_token` the client sent with
`chat.command`, which is how the web client ties the modal to the command it
ran. `channel_id` is `null` even though the command ran in a channel.

### `view_updated` (WebSocket)

```json
{
 "type": "view_updated",
 "view_id": "V0EXAMPLE04",
 "app_id": "A0F827J2C",
 "app_installed_team_id": "T0EXAMPLE01",
 "view": {
  "id": "V0EXAMPLE04",
  "team_id": "T0EXAMPLE01",
  "type": "modal",
  "blocks": [
   {
    "type": "section",
    "block_id": "giphy:shuffle",
    "text": {"type": "mrkdwn", "text": "Not the perfect gif?", "verbatim": false},
    "accessory": {
     "type": "button",
     "text": {"type": "plain_text", "text": "Shuffle", "emoji": true},
     "action_id": "giphy:shuffle"
    }
   },
   {
    "type": "image",
    "block_id": "gif",
    "image_url": "https://media4.giphy.com/media/zQuZwn3KkgBAMV3zDf/200.gif?cid=REDACTED&ep=v1_gifs_translate&rid=200.gif&ct=g",
    "alt_text": "gif",
    "image_width": 112,
    "image_height": 200,
    "image_bytes": 290402,
    "is_animated": true,
    "fallback": "112x200px image"
   }
  ],
  "private_metadata": "",
  "callback_id": "giphy:share",
  "state": {"values": {}},
  "hash": "1791802175.8HCVtyxU",
  "title": {"type": "plain_text", "text": "Giphy", "emoji": true},
  "clear_on_close": false,
  "notify_on_close": true,
  "close": {"type": "plain_text", "text": "Back", "emoji": true},
  "submit": {"type": "plain_text", "text": "Share", "emoji": true},
  "previous_view_id": "V0EXAMPLE03",
  "root_view_id": "V0EXAMPLE03",
  "app_id": "A0F827J2C",
  "external_id": "",
  "app_installed_team_id": "T0EXAMPLE01",
  "bot_id": "B0EXAMPLE01"
 },
 "event_ts": "1791802175.005300"
}
```

### `views.get`

Request form: `token`, `view_id`, then the `_x_` envelope. Response:
`{"ok":true,"view":{…}}`, the same view object as the WebSocket events carry.

### `views.submit`

Request form, in order: `token`, `client_token=web-<unix ms>`, `view_id`,
`state`, then the envelope. `state` for the search view:

```json
{"values":{"giphy:search_term":{"search_term":{"type":"plain_text_input","value":"dancer"}}}}
```

That is `values[block_id][action_id] = {type, value}` — the shape Slack's docs
give for `view.state.values` **(docs)**. A view with no inputs submits
`{"values":{}}`.

Responses seen:

- **push** — `{"ok":true,"view":{…new view…},"response_action":"push"}`. The
  new view has a new ID, `previous_view_id` = the submitted view,
  `root_view_id` unchanged, `close: "Back"`.
- **clear** — `{"ok":true,"view":null,"response_action":"clear"}`. Close the
  whole stack.

Not seen, but documented for apps **(docs)**: `update` (replace the current
view in place) and `errors` (a map of `block_id` → message to show under
inputs). The client-side response shape for those is unconfirmed.

### `blocks.actions` with a view container

```
service_id      = B0EXAMPLE01
app_id          = A0F827J2C
service_team_id = T0EXAMPLE01
actions         = [{"action_id":"giphy:shuffle","block_id":"giphy:shuffle","text":{"type":"plain_text","text":"Shuffle","emoji":true},"type":"button"}]
container       = {"type":"view","view_id":"V0EXAMPLE02"}
client_token    = web-1791801962169
state           = {"values":{}}
```

Response `{"ok":true}`; the result arrives as `view_updated`. The same endpoint,
with `container: {"type":"message_attachment", …, "is_ephemeral":true}`, is
what the slash-commands spec implements for message buttons; build on that
code, do not write a second `blocks.actions` call. Note the action echoes the
element as the view declared it (no `value` here because the accessory has
none) plus `block_id`, and `state` presumably carries the current input values
when the view has inputs **(unconfirmed)**.

## What the captures do not show

Each of these needs a capture (or an experiment) before it is designed:

1. **Closing.** Nobody pressed Cancel or Back. With `notify_on_close: true`
   (the pushed views) the app expects to hear about a close **(docs:
   `view_closed`)**; the client call that triggers it (likely `views.close` or
   similar) was not captured. Back on a pushed view presumably pops to
   `previous_view_id` locally — unconfirmed.
2. **`response_action: update` and `errors`** from `views.submit`.
3. **Any element beyond the whitelist above** (selects, datepickers,
   checkboxes, radio buttons, multi-line input, `dispatch_action: true`
   inputs).
4. **`timeout_range: 10000`** on `view_opened` — meaning unknown. Possibly how
   long the client waits for the app before showing an error.
5. **`hash`** — documented for `views.update` concurrency on the app side
   **(docs)**; whether the client ever sends it is unknown.
6. **Modals opened from a message button** (not from a slash command). Expected
   to arrive the same way (`view_opened`), not captured.
7. **Workflow / `app_actions` shortcuts.** `client.appCommands` also returns
   `app_actions` (global shortcuts: "Declare an incident", "Create new
   issue…"). Running those opens modals too; out of scope for both specs.

## Where it would plug into slk (as of 2026-10-12)

- **WebSocket:** `internal/slack/events.go` decodes frames by `type`; the
  slash-commands spec adds `view_opened` (for its notice). Modals add
  `view_updated` and route both to a new `EventHandler` method, wired in
  `cmd/slk/rtm_handler.go`.
- **Wire calls:** `internal/slack` — `views.get`, `views.submit`, and the
  `blocks.actions` call the slash-commands spec adds, called with a view
  container. Follow `attachment_action.go`'s pattern: a request struct, a
  marshal function tested byte-for-byte against the capture, `postForm`.
- **Port:** `core.InteractionService` already holds `PressAttachmentAction`;
  slash commands add the message `blocks.actions` method. Submitting a view
  belongs beside them.
- **Data:** a neutral view type in `internal/core` (not `slack-go`'s), so
  `internal/bubbles` can take it — `internal/bubbles` may not import
  `slack-go` (`TestBubblesReachTheAppOnlyThroughCore`).
- **Component:** a new `internal/bubbles/<form>` package drawn through
  `bubbles/chrome` (`Frame`, `Box`, `FilterInput`), with buttons via
  `bubbles/buttonrow`, composited with `ui/overlay.DimmedOverlay` like every
  other dialog. Read-only blocks (section text, context, image) are rendered by
  the host with the existing Block Kit renderer and passed in as lines, per the
  dialog spec's note and RFC #236. Do not write a second Block Kit renderer.
- **Images in a dialog:** the GIF preview is an `image` block. Inline images go
  through `internal/ui/imgrender` / `internal/image` and the kitty/sixel
  paths in the panes; whether a
  composited overlay can show one (sixel especially) is an open technical
  question to settle early. Falling back to the alt text plus an "open image"
  link is acceptable for a first version.
- **Ownership (RFC #236 rule 2):** intent originates in the dialog (submit,
  button), and results arrive via the event stream (`view_updated`, the posted
  message), so the dialog may call its own submit/press callables, with errors
  through the shared toast. Opening on `view_opened` is `App`'s: it arrives
  unprompted and must arbitrate with other open modals and the composer.
- **The notice to replace:** the slash-commands spec's "this command opened a
  dialog slk can't show yet" on `view_opened`. A modal implementation keeps it
  for views containing non-whitelisted elements.
