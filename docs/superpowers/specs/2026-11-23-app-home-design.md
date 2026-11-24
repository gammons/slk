# App Home tabs - design

**Date:** 2026-11-23
**Status:** implemented

## Why

An app DM has two tabs in Slack: **Messages** and **Home**. Some apps put
everything the user needs on the Home tab and keep Messages read-only or
nearly empty. `AI Budget Help` is one: its budget summary, per-provider spend,
credential buttons and support links are all Home-tab Block Kit content, and
slk showed only the (read-only) messages, so the app looked broken there.

slk already renders Block Kit for messages. A Home tab is the same view
language, served by a different endpoint and with buttons whose press goes to
a view-scoped interaction rather than to a message.

## Evidence

Both calls are undocumented; they were checked against a live workspace.

**Fetch.** `conversations.info` with `channel=<app DM>&return_app_home=true`
returns the `app_home` metadata *and* the whole view:

```json
"app_home": {
  "app_id": "A0…", "app_installed_team_id": "T0…",
  "home_tab_enabled": true, "messages_tab_enabled": true,
  "messages_tab_read_only_enabled": true,
  "home_view_id": "V0…"
},
"home_view": {
  "id": "V0…", "type": "home", "bot_id": "B0…",
  "title": {"type": "plain_text", "text": "View Title"},
  "blocks": [ {"type": "header", …}, {"type": "section", …},
              {"type": "actions", "block_id": "8iInl", "elements": [
                {"type": "button", "action_id": "home_refresh",
                 "text": {"type": "plain_text", "text": "Refresh"},
                 "value": "home_refresh"},
                {"type": "button", "action_id": "vx_creds_email", …,
                 "confirm": {"title": …, "text": …, "confirm": …, "deny": …}}]} ]
}
```

`home_view` is present but null when the app has no Home tab, so slk treats
"no view" as "messages only", not as an error.

**Press.** The button press goes to `blocks.actions` with four form fields:
`service_id` (the view's `bot_id`), `client_token` (`web-<unix millis>`),
`actions` (a JSON array holding the button as it was rendered) and `container`
(`{"type":"view","view_id":"V0…"}`). Sending exactly that for the view's
`Refresh` button answered `{"ok": true}`; a second `conversations.info` showed
the app had re-published the view with new block ids and refreshed numbers.
Slack answers interactivity over the WebSocket, so the HTTP call reports only
dispatch.

## Design

### Wire (`internal/slack`)

- `(*Client).GetAppHome(ctx, channelID)` returns an `AppHome` (`AppID`,
  `ViewID`, `Title`, `BotID`, raw `slack.Blocks`), or nil when the conversation
  has no Home tab.
- `(*Client).BlockAction(ctx, BlockActionRequest)` posts `blocks.actions`.
  One request, no retry: a press can send an email or open a modal, and a
  replay after a transport failure could act twice.

### Ports (`internal/core`)

`AppHomeService` with `Fetch(teamID, channelID) Msg` (blocking; callers run it
from a Cmd) and `Press(teamID, channelID, viewID, serviceID, action) error`.
`core.AppHome` carries parsed `blocks.Block`s for rendering plus
`[]AppHomeAction` (buttons in render order: action id, block id, label, value,
URL, style, optional confirm). `cmd/slk` converts `slack.Blocks` and extracts
buttons with `blockkit.Parse` and a walk of `slack.ActionBlock`s.

Both calls are org-served on Enterprise Grid: a member workspace's own token
answers `team_is_restricted` for `blocks.actions` (and can miss the
conversation for `conversations.info`), so `cmd/slk` runs them through
`sameUserOrgClient` - the org-level sibling session of the *same authenticated
user* - as the Activity feed does.

### UI

A fetch is fired when an `app`-typed conversation is selected. When it lands:

- The Home tab is installed and the messages region switches to a new
  `ViewAppHome`, the first time only and only in normal mode. The user can
  return with `t` (or Esc), and `t` toggles back. The choice is per visit:
  picking another conversation clears the tab, and the next open fetches again.
  A switch of window focus (`ctrl+w`) treats the newly focused window's
  conversation the same way, since it retargets the channel context without a
  channel selection.
- A channel opening that is navigating to a message or a thread (a permalink, a
  saved position, a history walk) fetches the tab for `t` but does not show it:
  the linked message must stay on screen.
- The pane is a full-height bordered panel (no composer), drawn by a small
  `internal/ui/apphome` model: a title row (`<app>  Home · Messages`), a key
  hint, and the Block Kit blocks rendered with the shared message renderer
  (mention names, emoji, wrapping). `j`/`k`, paging, the mouse wheel, `g` and
  `G` scroll it.
- `b` opens the existing confirm-prompt chooser, one numbered entry per button.
  A button with Slack's `confirm` object opens Slack's confirmation step
  first; only its OK button presses. URL buttons open the link.
- A successful press refetches the view (the app re-publishes it), and one
  delayed follow-up fetch catches an app that publishes only after it finished
  handling the press. A failed fetch says nothing about whether the app has a
  Home tab, so it keeps the tab already on screen; only a successful answer
  with no view clears it. A failed press toasts and leaves the tab as it was.
  A button that opens a modal dispatches too; Slack's `view_opened` frame
  arrives and the existing handler toasts that slk can't show the dialog.
- While Home is up, the conversation counts as not drawn for read marks: a
  message arriving behind the tab stays unread, exactly as in a stacked layout
  where the pane is hidden.
- While the tab is up, keys that act on the messages pane's selection (`r`,
  `E`, `D`, `y`, `F`, `o`, `i`, marks, search, …) are ignored: the pane's
  selection is not on screen. `t`/Esc/b and the global navigation keys still
  work. Insert mode is refused from any focus except a visible thread pane
  (whose own composer is still on screen), and reaction navigation left over
  from the messages tab is ended when the tab appears.

Both panes keep the existing rule that `internal/ui` does no I/O: the fetch
and the press go through `AppHomeService`, wired in `cmd/slk`.

## Limitations and follow-ups

- Only `actions`-block **buttons** are pressable. Selects, date pickers and
  overflow menus are rendered as labels and cannot be operated.
- The same interaction machinery is not yet used for Block Kit buttons that
  appear in **messages** (a different container: message-scoped payloads).
- The tab renders with an image-free Block Kit context, so image blocks show
  their fallback links: this pane has no placement pipeline and no repaint
  when an image fetch finishes.
- A button whose action opens a Slack modal is dispatched, but the modal
  itself is not rendered: slk shows its existing "opened a dialog slk can't
  show yet" toast.
- Slack pushes the re-published view over the WebSocket as
  `view_updated` (`view:null` when a modal closes). Consuming that frame
  instead of the timed follow-up fetch after a press is a follow-up; slk
  currently logs it as an unknown event.
- Opening an app DM asks `conversations.info` twice: the messaging-capability
  probe (`boot.ConversationsInfo`) and this fetch decode different subsets of
  the same answer. Merging them is a follow-up.
