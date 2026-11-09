# Draft preview above the compose box

Task: SLK-908. Status: design, awaiting Grant's approval. No code yet.

"Preview" already means two other things in slk: a link preview (an
unfurl, which `x` removes) and the image preview overlay. This feature
is called the **draft preview** everywhere (code, help, wiki) so it is
never confused with either.

## Why

Grant sometimes writes long messages with a lot of formatting: links
with labels, lists, bold. In the composer that text is raw markdown
(`- ship **v0.28** on Tuesday`, `[the notes](https://...)`), and the
only way to see how it will look is to send it. He wants to see the
rendered message before he sends it, on demand.

The idea is old: `2026-04-25-qol-ui-fixes.md` lists "Markdown preview in
compose box" as deferred, and `2026-05-01-commonmark-compose-design.md`
lists "Live preview in compose" as separate UX work. Nothing in
`internal/ui/compose` does it today.

## What it looks like

```
 #general
 ...messages...
 ── draft preview ───────────────────────
 Release plan:
  • ship v0.28 on Tuesday
  • read the notes
 ────────────────────────────────────────
 Release plan:
 - ship **v0.28** on Tuesday
 - read [the notes](https://...)
```

The pane sits directly above the compose box, inside the same pane
border, and shows the draft the way slk will show the message once it
is sent: bullets for list items, bold as bold, a link as its label,
`@name` mentions as names.

## Decided by Grant

1. **Off by default.** A Ctrl key shows it; pressing the key again
   hides it.
2. **Closes on send.** Sending the message closes the preview, so the
   next message starts with it off.
3. **The key is `ctrl+r`.** Pressing it again hides the preview without
   sending anything. (`ctrl+r` is bound nowhere in slk today and is not
   a key of the bubbles textarea keymap.)
4. **Live.** The preview updates as you type.
5. **Both composers.** It works the same in the thread reply box as in
   the channel box. The key only affects the box you are typing in;
   each box has its own on/off state.
6. **Empty draft.** With nothing typed, the pane shows one dim line,
   "nothing to preview", instead of disappearing.
7. **Height cap; show the end.** The pane grows with the draft up to a
   cap (about a third of the pane's height). Past the cap it shows the
   end of the rendered draft, not the top. It shows the end whether or
   not the cursor is there; following the cursor through the
   markdown-to-rendered mapping is not part of this.

## Goals

- What the preview shows is what the channel will show for the sent
  message, not a look-alike. That means the preview goes through the
  same two steps a send does, with no second converter or renderer:
  1. the draft's `@name` / `#channel` mentions translated the way Enter
     translates them (`(*compose.Model).TranslateMentionsForSend`), then
  2. `mrkdwn.Convert`, the converter `client.SendMessage` /
     `SendReply` / `EditMessage` use and the optimistic row already
     calls in the UI (`reducer_send.go`, `reducer_threads.go`),

  and the resulting mrkdwn is drawn the way `messages.RenderMessage`
  draws a message body, with the pane's user, channel and user-group
  names. That is more than `messages.RenderSlackMarkdownWith`: the body
  path also passes the width (code blocks and quotes wrap to it), word
  wraps to the content width and applies `styles.MessageText`
  (`render_message.go`). In short: the preview shows exactly what the
  optimistic row would show if you pressed Enter now.
- Typing stays as fast as it is today with the preview on.

## Non-goals

- **Things Slack adds after sending.** Link unfurls, Work Object link
  chips (`link_chips.go`, decided by Slack at send time) and anything
  else that arrives with the server's copy are not shown.
- **Rich-text block quirks.** The preview renders the converted mrkdwn
  text, as the optimistic row does, not the `rich_text` block sent
  beside it. Where the two differ (the "paragraph quirks" the comment
  in `reducer_send.go` mentions), the preview matches the optimistic
  row, not the message after Slack's echo replaces it.
- **Fixing what gets sent.** If the preview reveals that something
  converts badly, that is a separate task. This feature only shows.
- **Inline images.** Custom emoji show as their glyph or `:name:` text,
  not as kitty/sixel images (see Constraints).
- **Interaction.** The preview is read-only: no cursor, no scrolling,
  no clickable links.
- **Remembering the setting.** No config option and no persistence
  across sends or restarts (decision 2).

## Anti-goals (would make this a failure even if it works)

- A preview that disagrees with what lands in the channel for an
  ordinary message (text, lists, bold/italic/strike, code, links,
  mentions). That is the whole point.
- Typing that lags with the preview open on a long draft.
- The messages area jumping around while you type, beyond the rows the
  preview itself takes or gives back.
- The preview changing anything about the draft or the send. (A
  refactor on the send path that keeps its output byte-identical, like
  caching the mention list, is fine; see "Calls I made".)

## Constraints

- **`internal/ui` does no I/O** (AGENTS.md, `boundary_test.go`).
  `internal/slack/mrkdwn` is a pure converter and is already imported by
  `internal/ui` for the optimistic row, so the boundary is unchanged.
- **Reducer chain and mode table.** The key is handled in insert mode
  (`mode_insert.go`), next to the composer keys `ctrl+o` and `ctrl+e`,
  and is bound in `DefaultKeyMap` (`internal/ui/keys.go`) so the help
  overlay lists it. It is a composer key, so it does not join
  `isAppHotkey`.
- **State lives in the composer, not on `App`.** Each box's on/off
  state belongs to its `compose.Model` (decision 5 makes it per box
  anyway). The refactor plan's Phase 5 is trying to shrink `App`'s
  field count, so do not add App fields for this.
- **One renderer.** Use the existing body renderer; do not write a
  pane-specific or preview-specific copy (AGENTS.md, "Known duplication
  — do not add to it"). If `RenderMessage` needs a body-only mode to
  serve this, add it as a `RenderOptions` field documented where it is
  declared.
- **No image placement in the bottom region.** The composer region is
  redrawn fresh every frame outside the panes' kitty/sixel placement
  pipeline. Render the preview with emoji images off.
- **Layout.** Both panes stack extra rows above the compose box today
  (pickers, the thread's bot-status line) in `renderChannelMessagesPanel`
  (`view_messages.go`) and `renderThreadRegion` (`view_thread.go`); the
  preview joins that stack. The messages area keeps its existing
  minimum of 3 rows.
- **Repo checks.** Tests are stdlib `testing` only, white-box, built with
  `newTestApp`; a golden for the new pane through the existing golden
  helpers. `wiki/Keybindings.md` gains the key; no test checks the
  wiki, so this is easy to forget.

## Calls I made (Devon; Grant hasn't ruled on these)

- **Name:** "draft preview", for the reason at the top.
- **Body only.** The pane shows the rendered message body with no author
  line, avatar or timestamp, as in the sketch Grant saw.
- **Title rule.** A one-row rule labelled "draft preview" tops the pane,
  so it can't be mistaken for a sent message.
- **Insert mode only.** `ctrl+r` does something only while you are
  typing in a box. In normal mode it stays unbound.
- **What else closes it:** sending (decision 2), and moving the box to a
  different conversation (switching channels, opening another thread),
  since the preview belongs to the draft you turned it on for. Leaving
  insert mode with Esc does not close it: the pane stays while you look
  around, and is there when you press `i` again.
- **Edits (`E`).** The preview works while editing a message in the box,
  since `EditMessage` converts the same way. Saving the edit closes it,
  like a send, and so does cancelling it (Esc during an edit puts the
  stashed draft back, which is a different draft).
- **Slash commands.** A draft starting with a slash command is not a
  message, so the pane shows one dim line ("slash command, not a
  message") instead of rendering it.
- **Files attached.** A caption sent with files goes to Slack as typed
  (`UploadFile` sets `initial_comment` without `mrkdwn.Convert`), so a
  rendered preview would be wrong. With attachments in the box, the
  pane shows one dim line ("caption is sent as typed") instead.
- **Pickers.** When an emoji/mention/channel picker is open, it sits
  between the preview and the compose box, next to the cursor it
  belongs to; the preview moves up.
- **The cap.** A third of the pane's content height, at least 3 rows,
  and never so tall that the messages area drops below its 3-row
  minimum; when space is short the preview shrinks first. Rows cut
  off the top are marked with a dim "…" first row.
- **Disabled box.** The preview follows what each box draws. When the
  channel box is replaced by the read-only line (`composeDisabled`), no
  preview is drawn. The thread box is always drawn today (a blocked
  reply is refused with a toast on Enter), so its preview works as
  usual there.
- **Speed.** Mention translation is the likely cost, not
  `mrkdwn.Convert`: `TranslateMentionsForSend` rebuilds and sorts the
  full user/group/channel list (`sendMentions`) on every call, which
  today is once per Enter. The preview must not do that per keystroke on
  a large workspace. Allowed fixes, in order of preference: render the
  preview from `View` with a cache keyed on the draft text, width,
  theme and the names it uses (below), so it runs at most once per
  drawn frame for a changed draft;
  and cache the sorted mention list in `compose.Model`, cleared by
  `SetUsers` / `SetUserGroups` / `SetChannels` (and whatever else feeds
  it), with a test that the translated text is unchanged. Debouncing the
  preview is not allowed (decision 4).

## Left to the builder

- Where the render happens (a compose method given a render function,
  or the view code calling the renderer with the composer's text), so
  long as it obeys the constraints above.
- Caching details, within the "Speed" call above. Key on the draft
  text, width and theme, plus something that changes whenever the names
  the preview uses change: the composer's mention data (`SetUsers`,
  `SetUserGroups`, `SetChannels`) and the pane's user, channel and
  user-group names used to draw mentions. Otherwise a name that
  resolves while the draft sits unchanged leaves a stale preview while
  the send uses the new name. Don't key on compose `Version()`: `Update`
  bumps the version on every call, and no production code reads it. Add a
  benchmark of the full preview path (translation, conversion, render)
  on a long draft and a large user list.
- The exact styling of the rule and dim lines, from existing styles
  (`messages.DividerStyle`, `styles.TextMuted`).
- Mouse behaviour: clicks on preview rows should not count as a click
  on the compose box (`SetMsgComposeTop` / `SetThreadComposeTop`), or
  should; pick whichever keeps the existing click tests honest and say
  which in the PR.
- Test layout and the split into commits.
