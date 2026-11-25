# App modals (Slack views)

**Date:** 2026-11-25
**Status:** Implemented
**Research:** `2026-10-12-app-modals-research.md` (wire captures and the
giphy flow this design starts from)

## Goal

When a Slack app opens a modal for the user (bare `/giphy`, an app Home
tab button, an app's own message button), slk shows it instead of the
old toast "opened a dialog slk can't show yet". The dialog is drawn from
its Block Kit blocks, scrolls, and can be dismissed. Its buttons can be
pressed; a press's result (a replaced or pushed view) is drawn.

Read-only dialogs (the AI Budget Support dialog) are the target: they
render completely, including header, sections, contexts, dividers and
buttons.

## What is not supported yet

- **Forms.** `input` blocks render as the block renderer's
  `[unsupported block: input]` marker. A view with a submit label says
  so when Enter is pressed ("slk can't submit this dialog yet") instead
  of pretending to submit.
- **view stack navigation.** A pushed view replaces the one on screen;
  Esc closes the whole dialog rather than popping to the previous view.
  Slack's own close button is not sent (`views.close` was never
  captured).
- **Images inside a modal.** The dialog renders with the image-free
  context the app Home tab uses; image blocks draw their fallback
  links.
- **Typed input state and validation.** Nothing to submit, so there is
  nothing to collect.

`rich_text` blocks are converted to section text in the modal's block
list: the Block Kit renderer leaves them to the message panes, which run
the host's mrkdwn pass, and a modal has no second pass. Buttons in a
section accessory are pressable like actions-block buttons.

## Wire

### `view_opened` / `view_updated`

`internal/slack/events.go` parses both frames into `slackclient.View`
(`internal/slack/view.go`):

- `view_opened` carries the whole view inline. Some fields are repeated
  at the top level; the nested object wins, the top-level ones are the
  fallback for a trimmed frame. A frame with only an ID and title still
  opens an empty dialog so it can be dismissed.
- `view_updated` may carry the view inline, or carry `view: null` and
  leave fetching to the client. The old view ID still reaches the
  handler either way.

The `EventHandler` interface grows two methods:

```go
OnViewOpened(v View)
OnViewUpdated(viewID string, v *View) // v nil: fetch it
```

`cmd/slk/rtm_handler.go` sends `ui.AppViewOpenedMsg` for an inline view,
or `ui.AppViewNeedsRefreshMsg` when there is none. No HTTP runs on the
event goroutine.

### `views.get`

`(*slackclient.Client).GetView(ctx, viewID)` posts `views.get` and
decodes the response's `view`. It is called from a `tea.Cmd`, through
`core.AppViewService.Fetch`, and routed through `orgOrOwnClient` the way
the Home tab's `conversations.info` is: a Grid member workspace's token
cannot answer it on its own.

### `blocks.actions`

Pressing a modal button reuses the existing `blocks.actions` call
(`pressAppHomeAction`): `service_id` is the view's `bot_id` (falling
back to `app_id`), the container names the view ID, and the action is
the button as rendered. No retry: a press can have side effects.

## UI

- `core.AppView`: title, close/submit labels, parsed blocks, pressable
  buttons. `core.AppViewService` fetches one and presses buttons.
- `internal/ui/appview`: the dialog model. Width is two thirds of the
  terminal (40-90 columns); the body window is
  `terminal height - frame rows`, so the box never scrolls the terminal.
  Scroll state resets when a new view is installed.
- `ModeAppView`: a modal overlay. `j`/`k`/arrows, paging, space,
  `g`/`G`, and the mouse wheel scroll; `Esc`/`q` dismiss; `b` opens the
  button chooser.
- The button chooser is a confirm prompt that lists the dialog's buttons
  with 1-9 shortcuts. It is opened with `openConfirmChoiceReturning`,
  which keeps the dialog drawn behind it (`appViewBehind`) and returns
  to `ModeAppView` when the chooser closes. A button with Slack's
  confirmation step opens the usual nested prompt.
- A successful press schedules one fetch after
  `appViewRefreshDelay` (1.2 s). Slack normally publishes the result
  itself; the fetch covers an app that only answers through
  `views.get`. Two counters guard the results: installing an inline
  frame bumps `appViewGen`, and a fetch carries the generation it
  started at, so it cannot overwrite newer inline content; fetches are
  also numbered, and one that started before a fetch that already
  landed is dropped, while two fetches at the same generation both
  apply in order. A fetch result only applies while its view is the one
  on screen, so a result for a dismissed dialog cannot reopen it; an
  inline update is handled the same way (it replaces the shown content
  and never brings a dismissed dialog back). Either kind of new content
  closes an open button chooser, whose actions belong to the old view,
  and a new `view_opened` also closes any other confirm prompt that is
  up (the quit prompt, say) rather than leaving it drawn over the dialog
  with the keys routed underneath.
- A click on a button presses it (a URL button opens its link; a button
  with a confirmation step asks first). A click elsewhere inside the
  dialog is consumed, and one outside dismisses it. The dialog maps the
  Block Kit renderer's button footprints (`RenderResult.ActionHits`,
  recorded by `appendActions`) into box-local rows and columns, and
  identifies the action by (block_id, action_id).

## Tests

- `internal/slack/view_test.go`: the captured `view_opened` frame, a
  loading frame with the title only at the top level, `views.get`
  request shape and a null view.
- `internal/slack/events_test.go`: `view_updated` with and without an
  inline view.
- `cmd/slk/app_view_test.go`: block/action conversion (actions-block
  and section-accessory buttons), rich text converted in place, service
  ID fallback.
- `cmd/slk/rtm_view_opened_test.go`: the handler's messages, including
  the inactive-workspace drop and an update with neither a view nor an
  ID.
- `internal/ui/appview/model_test.go`: rendering, scrolling, clamping,
  button hit-testing including scrolled-out buttons.
- `internal/ui/app_view_test.go`: open/replace/dismiss, inline updates
  (after dismissal, while choosing, replacing content), stale fetches,
  the chooser, a button click, an outside click, confirmation before a
  URL and press failure.
- `internal/ui/golden_dialogs_test.go` (`dialog_app_view`): the composed
  dialog frame.
