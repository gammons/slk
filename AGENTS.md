# AGENTS.md

Orientation for anyone — human or agent — writing code in this repo.

**The single most important rule: search before you write.** The most common
defect in this codebase's history is not bugs, it is the same logic implemented
a fourth time because the author did not know the first three existed. See
[Shared code](#shared-code--check-here-before-writing-a-helper) below.

## Build, test, lint

```
go build ./...
go test ./...                 # ~10s
go test ./... -race           # ~47s; this is what CI runs
go vet ./...
golangci-lint run             # v2.14.0, config in .golangci.yml
gofmt -l .                    # must be empty; enforced in CI
```

Those two timings are wall clock with `-count=1` and a warm build cache on an
8-core Linux box, re-measured at the end of Phase 0 (they were `~21s`/`~44s`
before it, on unrecorded hardware — treat them as a shape, not a target).
The shape is what matters: under `-race`, four packages are ~95% of the run —
`internal/ui` 32s, `cmd/slk` 29s, `internal/cache` 19s, `internal/ui/messages`
16s (they overlap, hence a 47s wall). If you are iterating, run the one package
you are changing; `internal/ui` alone is now longer under `-race` than the whole
suite used to be.

Tests are plain `testing.T`, stdlib only. No testify, no gomock, no golden
libraries. White-box (`package ui`, not `package ui_test`) by convention.

## Architecture in one screen

```
cmd/slk/                composition root: wiring, workspace connection,
                        WebSocket event handling, message fetch/cache pipeline
internal/core/          the ports (service interfaces) the TUI calls, and the
                        values the TUI and cmd/slk exchange through them
internal/ui/            bubbletea App: reducers, mode key handlers, view regions
internal/ui/<widget>/   self-contained sub-models (messages, thread, sidebar,
                        compose, and 14 modal packages)
internal/bubbles/       self-contained components and the substrate they
                        share (RFC #236), e.g. ansi, scrollbar, selection, and
                        chrome, the substrate every dialog draws through. Never
                        imports internal/ui; the TUI's I/O ban applies here too.
                        Widgets move here from internal/ui/<widget>/ one at
                        a time; confirmprompt, linkpicker and channelpicker
                        are the first three
internal/slack/         Slack Web API + browser-protocol WebSocket client
internal/slack/edge/    edgeapi: conditional revalidation, server-side search
internal/bootstrap/     startup fetch orchestration
internal/cache/         SQLite cache (a cache, not a source of truth)
internal/config/        TOML config
internal/demo/          fake workspaces + services behind the hidden
                        `slk --demo` flag; for recording README GIFs only
```

**`wiki/Architecture.md` is stale by roughly 7× and describes a service layer
that no longer exists. Do not trust it.** Current structural documentation:

- `docs/superpowers/plans/2026-09-06-architecture-refactor.md` — the active
  refactor: measured baseline, known problems, phase sequence
- `docs/superpowers/plans/2026-05-23-app-go-solid-refactor.md` — the completed
  `app.go` decomposition; establishes the patterns still in use
- `docs/superpowers/specs/` — one design doc per feature

### Invariants worth knowing

- **`internal/ui` does no I/O of its own.** Slack, SQLite, the filesystem, the
  clipboard, the external editor and launching apps all go through the service
  ports in `internal/core`, which `cmd/slk` wires. No `internal/slack`,
  `slackhttp`, `cache`, `config`, `filedl`, `export`, `editor`, `net/http` or
  `os/exec`; `slack-go` only in `blockkit`, as the data it renders. `internal/ui/boundary_test.go`
  enforces this. The boundary is deliberate; do not breach it. The same
  rules cover `internal/bubbles`, with no `slack-go` exemption, and it may not
  import `internal/ui` or anything under it: components are the app's building
  blocks, not part of it (`TestBubblesReachTheAppOnlyThroughCore`, same file).
- **`App.Update` routes through a reducer chain**, not a switch. Add behavior by
  adding to a `reducer_*.go` file, not by extending `Update`. The only step
  outside the chain is the thin `Update` wrapper that records `stackFront`
  (which content pane — messages or thread — last had focus, for the stacked
  thread layout) after every message; do not add other post-chain logic
  there.
- **Per-mode key handling is a table**, `modeHandlers` in
  `internal/ui/mode_handlers.go`. One `mode_*.go` file per mode.
- **SQLite is a cache.** Slack remains authoritative.
- **A map shared across goroutines is never written.** Every map the UI holds
  is either built for it (a copy, like `userNameStore.SnapshotForUI()` or the
  per-message `ExternalUsers` set) or published once and never mutated
  again (`WorkspaceContext.CustomEmoji` / `UserGroups`, swapped whole via
  `atomic.Pointer`). A copy handed to the UI is the UI's from then on: the
  sender must not keep reading it (the UI writes it). The same goes for maps
  captured by a `tea.Cmd` (`saveThreadToFile` clones them). A plain map
  written while another goroutine reads it is a `fatal error: concurrent map
  read and map write`, which bubbletea cannot recover from; it crashed slk on
  2026-10-02 via the user-name map (see `cmd/slk/usernames.go`). Engine state
  several goroutines write is locked (`userNameStore`) instead.

## Shared code — check here before writing a helper

If you are about to write text wrapping, box drawing, list windowing,
scrollbars, date formatting, case folding, or ID formatting: it already exists.

### Text and rendering

| Need | Use |
|---|---|
| Word wrap to a width | `messages.WordWrap(s, limit)` |
| Plain-text line segmentation (grapheme-correct) | `entrylist.PlainLines`, `entrylist.DisplayWidth`, `entrylist.SliceColumns` (`bubbles/entrylist`; `messages.PlainLines`/`DisplayWidthOfPlain`/`SliceColumns` delegate for existing callers) |
| Display width of a string (emoji-aware) | `emoji.Width(s)` |
| Truncate plain text to a cell budget ending in "…", never inside a grapheme cluster, measured as both lipgloss and `emoji.Width` see it | `truncateHeaderName(s, budget)` and `headerCells(s)` (`internal/ui/sidebar`, unexported; section headers). Lift them into a shared package when a second surface needs it (DM display names, per `2026-11-02-sidebar-status-emoji-images-design.md`) |
| Case/accent-insensitive fold for matching | `text.Fold(s)` |
| Slack mrkdwn → plain text | `messages.FlattenMrkdwn`, `messages.FlattenMrkdwnWithUserGroups` |
| A sent message's mentions (`<@U…>`, `<#C…>`, `<!here>`, `<!subteam^S…>`) as the `@name` / `#name` a person types, for putting it back in a composer (`E`) | `(*compose.Model).MentionsForEdit(text)`, the inverse of `TranslateMentionsForSend`; a token that would not translate back to the same mention stays in wire form |
| A sent message as the markdown a person types into the composer (line breaks, `- ` lists, `[label](url)` links, `**bold**`; mentions left in wire form), for seeding it on `E` | `messages.EditSource(msg)`: every rich_text block through `blockkit.RichTextToMarkdown` (the inverse of `mrkdwn.Convert`), else `msg.Text` with links as markdown and `&lt;`/`&gt;`/`&amp;` decoded. Literal text shaped like a mention stays escaped so saving can't ping (`blockkit.EscapeLiteralMentions`); a link markdown can't hold as typed stays `<url\|label>` (`blockkit.LinkToMarkdown`) |
| A message's text, including a forwarded message's shared content (alone, or after the comment it was forwarded with) | `messages.MessageCopyText` |
| Search-term highlighting (ANSI/OSC-safe) | `messages.HighlightSearchTerms`, `messages.SearchHighlightSGR` |
| Re-assert a background (or background+foreground) after every SGR reset, so nested lipgloss renders don't drop it | `bubbles/ansi.ReapplyAfterResets(text, style)` — the one implementation; `messages.ReapplyBgAfterResets` delegates to it for existing callers. The theme's sequences come from `messages.BgANSI()` / `messages.FgANSI()` |
| Re-assert a style's foreground after a kitty image placement (its closing `\x1b[39m` resets the foreground, and `ReapplyAfterResets` does not catch it) | `bubbles/ansi.ForegroundSGR(style.GetForeground())`, right after the placement when text in that style follows. Reaction pills, sidebar section headers and the DM header use it |
| Extract links from message text | `messages.ExtractLinks` |
| Every link a message offers to `o` (text links + Block Kit blocks + links in legacy attachment `text` (a forwarded message's shared text)/`pretext`/`title_link`/fields/nested blocks + permalinks of messages it shares, from attachment `from_url`) | `messages.MessageLinks(msg)` (the action-prompt body, `actionBody` in `action_prompt.go`, follows a different rule) |
| Which URL schemes count as a link in Slack's `<url>` / `<url\|label>` forms (http, https, ftp, ftps, sftp, ssh, file, mailto, tel; never javascript:/data:) | `linkscheme.Prefixes`, `linkscheme.Alternation`, `linkscheme.HasPrefix` (`internal/linkscheme`); the render, `slackfmt`, `edit_source` and `mrkdwn` tokenizer regexes embed it, and the mrkdwn Linkify matcher and protocol list (`convert.go`) and `blockkit.LinkToMarkdown` derive from it. Opening: `OpenLinkMsg` (`reducer_links.go`) confirms any scheme other than http, https, mailto and tel, showing the full target, before launching it; the prompt shows `displayTarget(url)`, which escapes control/bidi runes and elides the middle of a long target (head and tail kept), while the opener gets the exact URL |
| Does message text mention the current user? | `mention.InText(text, selfUserID)` |
| Reaction pill rendering | `messages.ReactionPillText` |
| The link previews a message's author can remove (legacy attachments with `FromURL` or `OriginalURL`; id = `ID`, or the 1-based position when 0), and what a prompt calls one | `messages.RemovablePreviews(msg)`, `RemovablePreview.Label()` |
| Render one `MessageItem` (author line, body, Block Kit, attachments, reactions, hits) for a content pane | `messages.RenderMessage(msg, width, rc, opt)` with a `RenderContext` the pane builds (`renderContext()` in each pane) and `RenderOptions` for its differences; `messages.BlockkitContext(msg, rc, &emojiFlushes)` for the Block Kit host context, retaining the uploads backing block emoji. System notices (`core.IsSystemNoticeSubtype`: join/leave, huddle, canvas update) take an early one-line muted branch. Full author headers wrap to `HeaderBudget` before avatar composition, with `lipgloss.NewWrapWriter` restoring styles on continuation lines; timestamps and edited marks stay whole unless a unit exceeds that budget. This keeps fitting timestamps matchable for selected-date substitution without adding rows. The actual header height feeds image placement and hit-rect row counts. Both panes; the thread pane's options are `thread.(*Model).renderMessage`. The thread `View` flushes the parent's body/block/reaction/attachment uploads before the no-replies return, independently of the reply-cache upload gate. `RenderOptions.BodyOnly` draws the body alone (no header, blocks, attachments or reactions), for the draft preview |
| The render context the draft preview draws with: a pane's names, theme options and emoji glyphs, with no emoji images, search terms, reaction cursor or reply avatar | `DraftRenderContext()` on either pane (`messages.Model`, `thread.Model`; on the `contentPane` interface), with `DraftRenderOptions(userID)` (body only, wrapped as beside the avatar the pane would draw) |
| Is a message subtype a system notice -- join/leave, `huddle_thread`, `tabbed_canvas_updated` (rendered muted, never unread/mention/notification-worthy)? | `core.IsSystemNoticeSubtype(subtype)` (`internal/core/subtype.go`, the one list; `core.SubtypeHuddleThread` names the huddle subtype); `RenderMessage` draws it as a one-line muted notice for both panes (body from the blocks when `text` is empty, else a fixed label; a thread parent keeps its reply line), `cmd/slk/rtm_handler.go` `OnMessage` gates the notifier and `shouldMarkChannel` on it, and live delivery (`internal/slack`) uses it to keep the subtype across a `message_changed` edit |
| Render emoji in a plain Block Kit header or control label without interpreting mrkdwn | `blockkit.Context.RenderPlainText`, installed by `messages.BlockkitContext`; package-local `messages.renderEmojiWith` shares image/glyph resolution with message mrkdwn and collects uploads through `RenderSlackMarkdownOpts.EmojiFlushes` |
| Turn a `RenderedMessage` into list rows: fill, invisible/tinted left border, the long-timestamp selected variant, the plain-text mirror (the part of building an `entrylist.Entry` both panes share) | `messages.WrapRows(msg, rendered, width, messages.NewWrapStyles(focused))` returning `WrappedRows` (`Lines`, `Selected`, `Plain`, `ShortSrc`); the panes add ID/Ref/hits and (channel pane) the spacer rows. `WrapStyles.SelectedVariant` builds the short-timestamp variant for `EntryPayload.SelectedLines`. The thread parent row is not wrapped this way: it is re-rendered every frame |
| Apply a reaction add/remove event idempotently; the reaction-pill cursor | `messages.ApplyReaction`, `messages.ReactionNav` |
| Apply a `message_changed` echo (edit, unfurl added, preview removed) to a shown message: replace Text, IsEdited, Blocks, LegacyAttachments, Attachments, keep the rest | `core.MessageItem.ApplyChange`, reached through `contentPane.UpdateMessageInPlace(item)` (channel pane, thread replies) and `thread.(*Model).UpdateParentInPlace(item)`; routed by `NewMessageMsg.Changed` in `reduceNewMessage`. `Changed` (a re-delivery: no unread, mention badge or notification) is not `MessageItem.IsEdited` (Slack's `edited` object, the "(edited)" mark); the two travel separately, starting at `EventHandler.OnMessage(..., changed, edited, ...)` |
| A content pane's per-entry payload next to its `entrylist.Entry` (short-timestamp selected variant, kitty flushes, sixel rows), and the rows to draw for the cursor entry | `messages.EntryPayload`, `(*EntryPayload).SelectedLines`; the emoji-image context both panes take is `messages.EmojiContext` |
| Date label from a Slack ts | `messages.DateFromTS`, `messages.FormatDateSeparator`, `messages.FormatShortDate` (compact form for a message header: "Today", "Jan 2") |
| Date-qualified timestamp for the selected message header | `messages.LongTimestamp(ts, short)`, `messages.SelectedHeader(rendered, header, ts, short, maxWidth)`; any other header timestamp swap that must fit the width budget: `messages.ReplaceHeaderTimestamp(rendered, header, short, replacement, maxWidth)` |
| Clock layout `<!date` tokens render `{time}`/`{time_secs}` with | `messages.SetDateTokenTimeLayout(layout)` — seeded once at startup from `appearance.timestamp_format` (`cmd/slk/main.go`); empty keeps the default `"3:04 PM"`. Package-level (like theme state) so every render path agrees |
| mpdm channel name → human name | `slackfmt.FormatMPDMName(name, self, lookup)`; pass `WorkspaceContext.UserHandle` (auth.test's `user`, `Client.UserName()`) as `self` so the user is left out |
| Channel-type glyph (`#` / `◆` / `●`) | `messages.ChannelGlyph(chType)` |
| May a message be opened as a thread / replied to? (a system notice with no replies may not) | `messages.CanOpenThread(msg)`; enforced at the sink every opener reaches, `(*App).openThreadPanel`, and in `openThreadForSelectedMessage`; refusal toast is `(*App).cantReplyToNotice()` |
| Marker row for a message only the current user can see | `messages.EphemeralLabel()` — drawn above the author line in both panes |
| A message's pressable legacy buttons (selects excluded) | `messages.ButtonActions(msg)`, `messages.HasButtonActions(msg)` |
| The hint under an interactive message ("b to respond" for an ephemeral with buttons, else "open in Slack") | `messages.InteractiveHint(msg)`; both panes |
| Open the action dialog for an ephemeral's buttons | `(*App).openActionPrompt(actionPromptRef)` (`internal/ui/action_prompt.go`); queueing and the interruption rule are `enqueueActionPrompt` / `tryOpenActionPrompt` |
| Replace a pane's list with fetched history without erasing ephemerals already shown | `keepEphemerals(fetched, shown)` (`internal/ui/ephemeral_keep.go`); only for a fetch of the conversation already on screen |
| Would a URL typed bare be sent as one link with that whole target (no space or non-ASCII char the typed-URL matcher stops at)? | `mrkdwn.TypesAsBareURL(url)`; `blockkit.LinkToMarkdown` keeps `[label](url)` / `<url\|label>` form when it is false |
| Where a typed URL starts and ends outside goldmark (text kept as typed, like a heading), by the same rule Linkify applies | `mrkdwn.typedURLLen(s)`, `bareURLSpans(s)`; `(*walker).appendRawText` emits such text with its URLs as links (`internal/slack/mrkdwn/typedurl.go`) |
| Slack permalink parsing | `slackurl.Parse` |
| A message's permalink without an API call (the inverse of `Parse`; how Slack's client makes one, and the fallback where Enterprise Grid refuses `chat.getPermalink` with `enterprise_is_restricted`) | `slackurl.Build(teamURL, channelID, ts, threadTS)`; used by `(*slackclient.Client).GetPermalink`, which `Y` and `F` (for its local echo) share. The selected message's thread for it: `(*App).selectedThreadTS()` |
| Huddle deep link for a conversation (`https://app.slack.com/huddle/<TEAM>/<CHANNEL>`) | `slackurl.HuddleURL` |
| Enter insert mode with a compose box focused (the shared body of `i` and a click on a compose region) | `(*App).beginInsertMode(toThread bool)` (`internal/ui/mode_command.go`); the compose regions' tops are recorded per frame in `panelLayout` |
| The OSC 8 hyperlink spans of final rendered message rows (display columns, same frame HitTest takes) | `messages.ScanOsc8Spans` + `messages.HitsFromSpans` (classifies a span as a file-attachment rect when its URL is one of the message's file attachments; used by WrapRows and the thread parent) |
| The channel a user's huddle is in (users.info) | `slackclient.UserProfile.HuddleStateChannelID` (wraps `*slack.User`; `GetUserProfile`/`GetUserProfileContext` are hand-rolled POSTs because slack-go's `UserProfile` drops that field) |
| The `room` object of a `huddle_thread` message (slack-go drops it): captured from history/replies responses by the client's `roomTap` (`(*slackclient.Client).HuddleRoom(channel, ts)`), and from WebSocket frames via the optional `slackclient.RoomObserver`; stored in `raw_json` (`slackclient.MergeRoom`; `UpsertMessage` keeps a stored room when a typed write lacks one); decoded with `core.ParseHuddleRoom` into `core.HuddleRoom` on `MessageItem.HuddleRoom`; read back from a cached row by `cmd/slk roomFromRawJSON` |
| Draw a huddle announcement: with replies, a message by the huddle's starter (render copy, stored item keeps Slackbot as author so permissions, dedup and unread are unchanged); without, the centred notice "<Starter> started a huddle · lasted 23 min · with A, B". Wording a duration ("under a minute", "23 min", "1h 5m") | `messages.HuddleView(msg, names)` (each pane renders and wraps this copy, never the stored item); `messages.FormatHuddleDuration(d)`; `huddleNoticeText` is used by the notice branch of `RenderMessage` |
| A huddle message's room in the UI (`messages.HuddleRoom` = `core.HuddleRoom`, immutable once on an item; it travels with the item through every load, live and edit path) and its read-only helpers: the starter's user ID, how long a finished huddle lasted | `messages.HuddleStarter(msg)`, `messages.HuddleDuration(room)` (`(d, true)` only when ended with `0 < DateStart < DateEnd`); `cloneHuddleRoom` (`internal/ui/winmodels.go`) copies one for each model; an edit carrying a newer room reaches the panes through `(*messages.Model).SetHuddleRoom` / `(*thread.Model).SetHuddleRoom` (a room-less edit keeps the held one) |
| Slack ts → `time.Time` (whole seconds, any timezone) | `export.TimeFromTS` |
| A since/until/overlap date range in a timezone, and "is this ts in it?" | `export.NewWindow`, `export.Window.Contains` |
| Sleep out a slack-go rate-limit error (ctx-aware) | `slackclient.WaitOutRateLimit` |
| Page through all channel history in a ts range / a thread's replies in a ts range | `(*slackclient.Client).WalkHistory`, `GetRepliesBetween` |
| Press a legacy attachment button (`chat.attachmentAction`, payload byte-identical to the web client's) | `(*slackclient.Client).AttachmentAction(ctx, AttachmentActionRequest)`, with `service_id` = the posting app's bot ID |
| Press a legacy attachment button from the UI | `core.InteractionService.PressAttachmentAction` (App field `a.interactions`); wired in `cmd/slk` via `pressAttachmentAction` to the active workspace's `Client.AttachmentAction` |
| Run a slash command (`chat.command`, request shape copied from the web client; arguments as rich text via `mrkdwn.Convert`) | `(*slackclient.Client).RunCommand(ctx, CommandRequest)`; a refusal (`unknown_command`, …) is `*slackclient.CommandRejectedError` |
| Remove a link preview from the user's own message (`chat.deleteAttachment`: `channel`, `ts`, `attachment`, `_x_reason=delete-single-attachment`; shape from emacs-slack, not a capture) | `(*slackclient.Client).DeleteAttachment(ctx, channelID, ts, attachmentID)`; no retry, not even on a 429, and never inside `WaitOutRateLimit`: the id is only good against the attachment list it was read from. Slack's refusal is `*slackclient.AttachmentRejectedError{Code}` (`MayHaveActed()` for `internal_error` / `fatal_error`), a 429 is `*slack.RateLimitedError`; any other error leaves it unknown whether Slack acted |
| Run a slash command from the UI | `core.CommandService.Run` (App field `a.commandSvc`); wired in `cmd/slk` via `runCommand` to the `Client.RunCommand` of the workspace the command was typed in (`teamID`, looked up with `router.ByID`, not the active one). Slack's refusal arrives as `*core.CommandError{Code}` |
| A web-client `client_token` (`web-<unix ms>`) for any request that sends one | `newClientToken()` (`internal/slack`); the only generator |
| Forward a message into another conversation (`F`): `chat.shareMessage`, a real shared message, no retry (a retry could post twice); the source permalink is fetched after the share, best effort, and only feeds the local stand-in | `(*slackclient.Client).ForwardMessage(ctx, sourceChannelID, ts, destinationChannelID)` (`internal/slack/forward.go`) / `core.MessageService.Forward`. The stand-in appended on `messageForwardedMsg` is replaced by the share's own WebSocket echo: `App.noteForwardStandIn(ts)` records it (capped), `App.takeForwardStandIn(ts)` in `reduceNewMessage` swaps it in with `UpsertSelfSent` and exempts it from the in-flight self-send guard (`internal/ui/forward_message.go`) |
| A web-client `client_msg_id` (random v4 UUID) | `newClientMsgID()` (`internal/slack/link_chips.go`); the only generator |
| Send a link the way the web client does when an app publishes a Work Object for it (an `attachment_mention` chip with the object's title) | automatic in `SendMessage` / `SendReply`, through `(*slackclient.Client).postMessage`: `chipCandidates` picks the bare http(s) links, `lookupLinkChips` calls `chat.unfurlLink` per link and waits up to `linkChipWait` (3s) for the matching `unfurl_preview_updated` frame (routed off the socket by `unfurlWaiters.deliver`, by `client_msg_id`), `applyChips` / `chipMrkdwn` rewrite block and fallback. No chip, no WebSocket or an `invalid_blocks` refusal sends the plain link (`internal/slack/link_chips.go`) |
| Emoji shortcode → glyph | `emoji.Sprint`, `emoji.CodeMap`, `emoji.StripSkinTone` |
| Emoji shortcode → image placement (custom emoji, aliases, skin tones), or "can't place, use your glyph/text" | `emoji.PlaceShortcode(ctx, name, customs, cells)` (`URLForShortcode` + `Place`, gated on image mode and a fetcher); fire the returned flush against `imgpkg.KittyOutput` during `View`. The sidebar uses it for section headers and DM status. A surface holding a `messages.EmojiContext` calls `EmojiContext.PlaceShortcode(name)` (its cells from `ImageCells()`) |
| Search a list of emoji the way the pickers do: prefix then substring matches, case- and accent-insensitive, the user's frecent emoji first (any alias or skin-tone spelling of a used one counts, shown once) | `emoji.Match(entries, nameFunc, query, emoji.MatchOptions{Substring, Limit, Frecency, Customs})` with `emoji.NewFrecency(names)` (names most frecent first, as `core.ReactionService.LoadFrecent` returns them); the reaction picker and the compose autocomplete both filter through it |
| The emoji a message uses as `:shortcode:`s, by the canonical name the history records | `emoji.UsedInText(text, customs)` |
| Slack's skin-tone number (2–6) for a modifier codepoint (U+1F3FB–U+1F3FF) | `emoji.SlackSkinToneNumber` |
| Does Block Kit already render the message body? | `blockkit.RendersBody(blocks)`, `messages.BlocksCarryBody(msg)`; whether the host draws a body row at all: `blockkit.HostBody(blocks)` (the rule `BlocksCarryBody` negates) |
| Splice sixel halfblock fallback rows into rendered Block Kit / legacy / file-attachment lines (panes without the sixel placement pipeline, i.e. the thread pane) | `blockkit.SpliceSixelFallbacks(lines, rows)` — grafts each row at the entry's `Col`, keeping the line's own surroundings; the only splice loop |
| Ask the terminal, before bubbletea starts, whether it draws kitty graphics or sixel, or what its cell size is in pixels (CSI 16 t) | `image.ProbeKittyGraphics`, `image.ProbeSixel`, `image.ProbeCellSize` (`internal/image/probe.go`); all take a writer, a reader and a timeout, run in raw mode, and read Windows console input through `pollProbe` (`probe_windows.go`) |
| A terminal cell's size in pixels for image encoding | `image.MeasureCellPixels(fd)` (env override, then TIOCGWINSZ; `ok` false when neither knows), `image.CellPixels(fd)` (the same with an 8x16 fallback); at startup `startupCellPixels` (`cmd/slk/cellpixels.go`) adds the CSI 16 t probe between the two |
| Rich text → plain text with no mrkdwn markers (emoji as glyphs, list items space-separated, one line) | `blockkit.RichTextToPlain(rt)` |
| Lay out a Slack `table` block as aligned ` │ ` columns (header rule, per-column alignment, glyph widths via `lipgloss.Width`, not `emoji.Width`: tables draw emoji as glyphs even in image mode) | `blockkit.TableToText(t)` — plain text: draw it verbatim (code-block style via `blockkit.Context.RenderCode`), never through the mrkdwn renderer. Whether the body or `blockkit.Render` draws each of a message's tables: `blockkit.TablesInBody(blocks)` (per block; the body takes a table only when it is first or follows rich_text or another body table) |
| Current DND state from a Slack API result | `slack.DNDStateFromStatus` |
| Persist a full Web API profile's custom status and huddle (including clears) | `cmd/slk/applyProfileStatus(teamID, userID, profile, db, send)` after `UpsertUser`, before any resolved-user notice or row repair; `UpsertUser` alone deliberately preserves status and presence on conflict (presence changes only through `UpdatePresence`, the `presence_change` path) |
| Drop cached thread replies a complete `conversations.replies` response no longer contains (missed deletes, Slackbot ephemerals) | `(*cache.DB).PruneThreadReplies(channelID, threadTS, returnedTS, fetchStartUnix)`; soft-deletes, never touches the parent or rows cached after the fetch began |
| Cache a `message_changed` echo (edit, unfurl, preview removed) without losing what the echo does not carry (reply_count, subtype, thread_ts, other raw_json keys) | `(*cache.DB).ApplyMessageChange(channelID, ts, cache.MessageChange)`: content-only `UPDATE` merging raw_json with `json_patch` (thread_ts only fills an empty one); reports false and writes nothing for an uncached or deleted message. `rtmEventHandler.cacheChange` calls it for `OnMessage(..., changed=true, ...)`; never `UpsertMessage` an echo |
| A workspace's DM rows with peers' current status and presence, not the connect-time values on `wctx.Channels` (the workspace switch does this) | `cmd/slk/withCachedPresence(db, withPeerStatuses(channels, cachedPeerStatuses(db, teamID)))`; both return copies, so `wctx.Channels` is never written |
| Peer custom status, DND and huddle rendering | `ui/peerstatus` (`Status`, glyph/expiry/summary methods; `GlyphShortcode(now)` / `StatusShortcode(now)` name the emoji `Glyph(now)` / `StatusGlyph(now)` stand for, to draw it as an image; `SummaryWith(now, layout, emoji)` is `Summary` with its emoji drawn by a callback); `messages.AuthorStatusSuffix(statuses, userID, now, opts)` for the status after an author's name, with the body's `RenderSlackMarkdownOpts` so status emoji share its image rendering and upload collector; image failures preserve `Status.Glyph`, and nil `opts` explicitly keeps glyph-only rendering. A header that can wrap needs its unexported half, `authorStatusEmoji`, which also says whether the emoji is an image: `RenderMessage` wraps an image as its own unit, so a cold image's cells survive |
| Name a message pane's header for a channel, with a DM's peer status as its topic (emoji as images in image mode) | `(*presenceController).setChannelHeader(a, mp, name, channelID)` (`internal/ui/presence.go`); a live change goes through `pushDMStatus`. Both call `messages.Model.SetDMTopic(st, now, layout)`, which keeps the status; `SetChannel` / `SetChannelTopic` set a plain topic |
| Workspace app/bot classification, from any goroutine | `WorkspaceContext.IsBotUser` / `MarkBotUser`; private synchronized set, never directly read/write a plain bot-ID map |
| Lazy user lookup for the workspace a fetch or cached row belongs to (not the active one) | `userRequesterFor(router, teamID)` in `cmd/slk/users.go` |
| Usergroup map helpers | `usergroups.Copy`, `usergroups.Equal`, `usergroups.Display` |
| A workspace's user ID → display name, from any goroutine in `cmd/slk` | `wctx.UserNames` (`*userNameStore`: `Get`/`Set`, `MentionedNames(text)` for one message; `lookupUserCached` / `resolveUserCached` add the SQLite fallback). Just `Set`: hand the UI the map from `SnapshotForUI()` and pass only its version to `NotifyFrom`, which makes every later new or changed name reach the UI as `UserResolvedMsg`. Never keep or read the map you handed over |
| A workspace's channel ID → name, including channels outside the sidebar | `wctx.ChannelResolver` (`cmd/slk/channel_resolver.go`): `Request(id)` uses local metadata before a concurrency-limited background `conversations.info` lookup. `nameStore()` returns its synchronized `*userNameStore`, or nil when disabled; use it for metadata writes and `SnapshotForUI` / `NotifyFrom` handoffs. `uiChannelNameNotifier` delivers workspace-scoped `ChannelResolvedMsg` updates without joining or adding sidebar/finder rows |
| Cache channel metadata without overwriting membership, stars, or another workspace's row | `(*cache.DB).UpsertChannelMetadata(ch)`; one atomic metadata-only upsert, used by channel mention resolution. `UpsertChannel` is for full conversation state |
| User IDs mentioned in message text | `slackfmt.MentionedUserIDs(text)` |
| Hydrate/resolve user mentions in every visible message mrkdwn lane, including forwarded attachments | `cmd/slk/resolveMessageMentions(msg, names, db, resolver, channels)`; local cache hits populate the name store, misses queue background resolution. `messageUserResolver(router, teamID)` selects only the source workspace, never the active one |
| Hydrate/resolve bare channel mentions in every visible message mrkdwn lane, including forwarded attachments | `cmd/slk/resolveMessageMentions(msg, names, db, resolver, channels)`; local metadata hits populate the name store, misses queue background resolution. `messageChannelResolver(router, teamID)` selects only the source workspace, never the active one |
| Resolve the starter and participants of a huddle message to display names (recorded in the name store, unknown ones requested from the resolver) | `resolveHuddleUsers` (`cmd/slk/users.go`), called by `OnMessage` and every history conversion site; pass it `userRequesterFor(router, teamID)`, the `UserResolver.Request` of the workspace the message belongs to (nil when none) |
| Channel IDs mentioned in message text (`<#C…>` or `<#C…\|name>`), or each with its optional label | `slackfmt.MentionedChannelIDs(text)`; `slackfmt.MentionedChannels(text)` returns `ChannelMention{ID, Label}` |
| Wake the serialized WebSocket event owner for local work | Implement `slackclient.PendingEventHandler` (`PendingEvents() <-chan struct{}`, `OnPendingEvents()`). The dispatcher selects the wake-up channel even on an idle socket and serializes the drain with Slack callbacks; nil disables it, and the channel must never close |
| Apply a deferred DM profile to conversation snapshots | `userResolver.queueResolvedPeer(userID)` after caching a resolved profile; `rtmEventHandler.OnPendingEvents()` drains relevant DM peers on the serialized event owner even on an idle socket. `rtmEventHandler.repairDMPeer(userID)` is the shared cache-to-row repair used by that drain and reconnect reconciliation. `rememberConversation` mirrors notifier metadata; `publishConversation` updates only the active UI |
| Context-bounded synchronous peer resolution at reconnect | `userResolver.ResolveNowContext(ctx, ids)` batches first; `resolveOneContext(ctx, userID)` is the fallback. Both share cache/name/workspace-scoped notifications, never mutate workspace slices |
| userBoot IM → conversation metadata | `cmd/slk/bootIMConversation`; callers decide whether an ordinary open IM or a closed starred IM is eligible. "Open" is `bootIMOpen(im, openSet)` (open flag or top-level `is_open`, not archived) |
| Open DMs userBoot has but `users.conversations` left out (a DM with a deactivated person is one) | `cmd/slk/missingOpenIMs(knownIDs, res)`; `connectWorkspace` adds them before the starred recovery |
| Starred conversation membership / recover missing sidebar rows | `bootSectionsClient` reuses `bootstrap.Result.Starred` at startup; `Client.GetStarredConversations` refreshes via `client.userBoot` on reconnect (never `stars.list`). `SectionStore.StarredConversationIDs()` returns a locked defensive copy; `cmd/slk/missingStarredConversations` hydrates missing metadata before the existing `buildChannelItem` / `addConversation` paths |
| The global set of deactivated users from the cache (not per workspace: an account is deactivated everywhere, and `users.workspace_id` is only the first workspace to cache the row) | `(*cache.DB).DeactivatedUsers()` returns a caller-owned map; `MarkUserDeleted(id)` / `ClearUserDeleted(id)` set and clear the flag without ever creating a row. Full-profile writers set `users.deleted` themselves (`User.Deleted`, `EdgeUserUpdate.Deleted`) |
| Record a deactivated flag from a profile that bypasses the full-profile writers (users.info fallback, avatar backfill, the profile dialog's fetch), and tell the UI | `recordUserDeleted(db, send, userID, deleted)` (`cmd/slk/deactivated.go`). A writer that does persist it reads `knownDeleted(db, id)` first and calls `announceUserDeleted(send, id, was, now)` after, which sends `ui.UserDeactivatedMsg` for every deactivation and only for a reactivation of a user known as deleted |
| IM peers Slack flagged `is_user_deleted`, and the set a workspace message carries | `recordDeactivatedPeer(ch, db)` wherever conversations are built (`buildChannelItem` stays pure); a user the cache has no row for is kept in the process-wide `rememberedPeers` (global, because a reactivation must clear it in every workspace), and `deactivatedUsers(db)` is the cache set plus those, with the `deactivatedSeq` it was read at, for `WorkspaceReadyMsg` / `WorkspaceSwitchedMsg` (`DeactivatedUsers`, `DeactivatedSeq`). Every `UserDeactivatedMsg` goes through `announceUserDeleted`, which numbers it, so the App can skip a snapshot entry older than a live update |
| Hide deactivated users on a UI surface | `App.hiddenUsers()` (`internal/ui/deactivated.go`) is the set to filter by, nil when `general.hide_deactivated_users` is off; `sidebar.Model.SetDeactivatedUsers` and `channelfinder.Model.SetDeactivatedUsers` keep the map they are given, so the App publishes a new map on every change and never writes one. The one row rule, for sidebar items, finder items and `#` picks alike, is `core.IsHiddenPeerConversation(type, dmUserID, hidden)` (a dm or app row with a hidden peer; a finder item carries the peer as `core.ChannelFinderItem.DMUserID`). The finder is the one surface that still offers such a row, to a non-blank query only, marked and sorted last (`channelfinder` `isDeactivated`); forwarding never offers it, also with the option off (`channelfinder.Model.SetUnsendableUsers`, given the full set). Apply the set before `SetChannels` / the finder items / `SetUserNames` |
| Why a conversation takes no messages (read-only mark, DM with a deactivated account, app-home probe), for the channel and thread composers | `(*App).sendBlockedReason(channelID)` (`internal/ui/read_only.go`) returns the toast text or ""; `syncComposeDisabled(why)` points `composeDisabled` at it for the active channel. `(*App).peerDeactivated(channelID)` (`internal/ui/deactivated.go`) finds the DM's peer in the finder rows, then the sidebar items |
| A workspace's custom emoji map, persisted, and when `emoji.list` is due | `cmd/slk/bootCustomEmoji(db, teamID, subset, now)` returns the map to publish at connect and how long it stays fresh (0 = fetch now); `cache.DB.CustomEmoji` / `ReplaceCustomEmoji` (full list, sets `fetched_at`) / `UpsertCustomEmoji` / `DeleteCustomEmoji` (deltas, leave `fetched_at`) / `ZeroCustomEmojiFetchedAt` (mark stale) / `CustomEmojiFetchedAt`. Publish under `WorkspaceContext.emojiMu` and bump `emojiSeq` for a delta, as `fetchWorkspaceEmoji` and `OnEmojiChanged` do |
| Workspace-relative cached external-user flags | `cache.DB.ExternalUsers(workspaceID)` returns a caller-owned map, derived from `User.HomeTeamID`; the legacy boolean is only a fallback within the row's original workspace |
| A WebSocket message's author ID and its UI `MessageItem` (bot-ID fallback, display-name resolution, file/block/attachment conversion) | `(*rtmEventHandler).messageAuthor`, `(*rtmEventHandler).messageItemFromEvent` (`cmd/slk/rtm_handler.go`); shared by `OnMessage` and `OnEphemeralMessage` |
| Legacy attachment action ids slack-go drops | `slackclient.EphemeralMessage.ActionIDs` (decoded from the raw frame), applied with `cmd/slk/applyActionIDs` |
| Slack original-avatar URL → sized CDN URL (e.g. 72px) | `avatar.SizedURL(orig, px)`; returns non-Slack-original URLs unchanged |
| Copy text to the clipboard | `App.clipboardWrite` / `SetClipboardWriter`; `cmd/slk/newClipboardWriter` selects local macOS `pbcopy` or terminal OSC 52 |
| The running build's identity as text: the full line (`slk v1.2.3 (commit …, built …)`) or the short form for the status bar (`v1.2.3 · abc1234`, version alone when no commit was stamped) | `version.Line(version, commit, date)` (what `slk --version` prints), `version.Short(version, commit)` (what `:version` shows, set on the App by `SetBuildInfo`) (`internal/version`) |

### UI chrome

| Need | Use |
|---|---|
| A centred, bold, muted row like a day divider (date separators, the "new" landmark's sibling, and system notices in `RenderMessage`) | `messages.DividerStyle(width)`; build per draw (reads the theme). A notice's plain mirror drops the centring pad via `trimPlainPadding` in `WrapRows` |
| The inline `#channel` autocomplete popup in the composer | `bubbles/channelpicker` (`New`, `SetChannels`, `Open`, `SetQuery`, `MoveUp`/`MoveDown`, `Select`, `View`, `SetStyles`, `SetWidth`; options `WithStyles`/`WithWidth`). Its consumer is `internal/ui/compose`, which maps the theme in `channelPickerStyles`, pushes it at construction and in `RefreshStyles` (called by `applyTheme`), and pushes the popup width before each `ChannelPickerView` |
| Scrollbar gutter on a rendered pane | `bubbles/scrollbar.Overlay`, `bubbles/scrollbar.Visible` |
| Lay out a wrapping row of labelled controls (buttons, select labels) with positions for hit-testing | `internal/bubbles/buttonrow` (`Layout`, `Rows`, `RowsFunc`); used by Block Kit actions and the confirm prompt |
| Draw a dialog: its box, title / filter / body / footer layout, filter line, list window, selected-row marker, scrollbar gutter, and the composer popups' box and marker | `bubbles/chrome` (`Frame`, `Box`, `FilterInput`, `Window`, `List`, `Marker`, `BodyTop`, `Popup`, `PopupMarker`), drawn with `messages.ChromeStyles()` called per draw. A dialog keeps its own fields, matching and row content; see the package doc |
| A scrollbar thumb's rows, for a gutter you draw yourself | `bubbles/scrollbar.Thumb` (the one formula; `Overlay` and `chrome.List` use it) |
| Centered modal over a dimmed backdrop | `ui/overlay.DimmedOverlay` |
| Pick one item from a message (the `o` link picker, the `d` file picker) | `bubbles/linkpicker` (`New`, `Open`, `HandleKey`, `View`, `SetStyles`, `SetWidth`; options `WithStyles`/`WithWidth`). The App maps the theme in `linkPickerStyles` and opens it with `App.openLinkPicker` (`internal/ui/link_picker.go`); `applyTheme` re-pushes the styles and `view_overlays.go` composites `View()` with `overlay.DimmedOverlay` |
| Prompt modal with N buttons (title, wrapped body of up to 8 lines, buttons; yes/no is `Open`, any buttons via `OpenChoice(confirmprompt.Choice)`, `Button.Then` for a follow-up step) | `bubbles/confirmprompt` (`New`, `Open`, `OpenChoice`, `Update`, `PressHighlighted`, `View`, `SetStyles`, `SetWidth`; options `WithStyles`/`WithKeyMap`/`WithWidth`; `KeyMap` field). In the App, open it with `App.openConfirmPrompt` (yes/no) or `App.openConfirmChoice` (any buttons) (`internal/ui/confirm.go`), which also map the theme onto its styles and composite it |
| Text selection ranges and anchors | `bubbles/selection` (`Range`, `Anchor`, `LessOrEqual`) |
| A scrollable, selectable list of pre-rendered rows: line offsets, scroll offset and snapping, the visible window, text selection and copy, clickable-rect translation, the scrollbar | `bubbles/entrylist` (`New` with options, `SetHeight`, `SetEntries`, `Layout`, `Compose`). The channel and thread panes are its two owners; each divergence between them is an `entrylist` option, documented on the option |
| Keep a content pane that is at the bottom at the bottom when a message grows or shrinks in place (an unfurl, an edit, a first reaction or reply) | `(*entrylist.Model).HoldBottom()`, called before the mutation; the next `Layout` pins the window to the last line. A pending `ResetSnap`, `ScrollUp`, `SetYOffset` or `Static` wins. Both panes' `UpdateMessageInPlace` and `UpdateReaction`, the channel pane's `IncrementReplyCount`, and the thread's `UpdateParentInPlace` and self-sent reply replacement (`SwapLocalSentReply`, `UpsertSelfSentReply`) use it |
| The theme mapped onto a content pane's entrylist styles (padding, scrollbar, selection highlight); the thread pane calls it; the channel pane uses the unexported `themeListStyles` it wraps | `messages.ThemeListStyles()` (`internal/ui/messages/pane_list.go`) |
| Do something to whichever message pane (channel or thread) is focused or named by a panel, without a messages/thread branch | the `contentPane` interface, `(*App).contentPaneFor(panel)` and `(*App).focusedContentPane()` (returns the pane and its channel ID) in `internal/ui/content_pane.go`; both panes are asserted against it at compile time. Built on it: `handleReactionNav`, `openReactionPicker`, `toggleReactionOnSelected` (`app.go`) and the click hit tests `toggleReactionAt` (reaction pill), `openImagePreviewAt` (inline image, via `HitTestImage`) and `downloadFileAt` (`[File]` line, via `HitTestFile`) (`reducer_mouse.go`), each taking the pane and its channel ID |
| Theme colors and styles | `ui/styles` (`Username`, `SelectionStyle`, `SearchHighlightStyle`, `MentionBadgeStyle`, `UserColor`) |
| Fill a base surface vs use its colour, with `[appearance] transparent_background` | Fill panes, sidebar, rail, status bar and splash with the paint colours `styles.Background`, `SidebarBackground`, `RailBackground`, `StatusBarBackground`, and the startup splash's `SplashBackground` (not `SurfaceDark`, which the composer, popups and image frames keep): they are `lipgloss.NoColor{}` when transparent, and their escapes `messages.BgANSI()` / `SidebarBgANSI()` are `\x1b[49m`. For a mix, a tint, a foreground, or a surface that stays solid (dialogs), use `styles.ThemeBackground()` / `ThemeSidebarBackground()` and `messages.SolidBgANSI()`; search highlight inside a dialog is `messages.DialogSearchHighlightSGR()`. Composer popups re-assert their own colour through `chrome.Styles.PopupANSI` (set only when transparent). An unselected row's left border is `styles.InvisibleLeftBorder()` (a space when transparent). The switch is `styles.SetTransparentBackground(on)`, read by every `Apply`; `styles.TransparentBackground()` says whether the last `Apply` turned it on (`internal/ui/styles/transparent.go`) |
| Change the theme inside the App | `App.applyTheme(name)` (`internal/ui/theme.go`): applies it, invalidates render caches and pushes fresh styles to components that snapshot them. A component that holds a style snapshot (anything in `internal/bubbles`) gets its push added there, not at each call site. `entrylist` is the exception: its owners (the channel and thread panes) call `SetStyles` at the top of every `View`, so `applyTheme` only invalidates for them. `styles.CurrentTheme()` names the applied one |
| Read the custom theme files (`~/.config/slk/themes/*.toml`) off the UI goroutine, then register them on it | `styles.ParseCustomThemes(fsys)` (pure, fills missing colors from dark) and `styles.RegisterCustomThemes(themes)`; `LoadCustomThemes` is both, for startup. A running slk reloads them on `SIGUSR1` (`watchThemeReloadSignal`, `cmd/slk/themereload_unix.go`), sending `ui.CustomThemesReloadedMsg`, which `reduceTheme` registers before reapplying the selected theme (`App.themeName`, which `applyTheme` records and startup seeds with `SetThemeName`) |
| Refresh channel mention names across every content pane without changing membership | `(*App).setChannelNames(names)` publishes a fresh UI-owned map to all channel windows, thread, Threads and Activity. `SetChannels` merges sidebar names while preserving resolved non-member names; workspace activation replaces the map from `WorkspaceReadyMsg.ChannelNames` / `WorkspaceSwitchedMsg.ChannelNames` |
| Status-bar toast that clears itself | `toastWithClear(a, text, d)` (`internal/ui/reducer_io.go`); `toastUntilReplaced(a, text)` for progress that a later result replaces (upload and download progress), or a toast whose owning reducer schedules its own clear (queued download failures). Set toasts on the Update goroutine, never inside a `tea.Cmd`; `statusbar.ToastSeq()` identifies the toast showing, so a clear tick leaves a newer toast alone. `TestToastsGoThroughToastSetters` fails on a direct `statusbar.SetToast` with text |
| Set or clear the `:` prompt | `(*App).setCmdline(line)` / `(*App).clearCmdline()` (`internal/ui/mode_command.go`), which keep `a.cmdline`, the Tab-completion cycle and the status bar's `SetCommandLine` / `SetCommandCandidates` in step; do not call those status-bar setters for the prompt directly |
| Window tree geometry | `ui/wintree` |
| Which panes this frame draws (side by side, stacked, full screen) and their widths | `(*App).computeFrame()` for View (stores the mouse hit-test bands); `(*App).frameInto(&scratch)` for a query that must not disturb those bands; `(*App).contentPanesDrawn()` answers "is the channel / the open thread on screen?" (an open pane can be hidden: an arrival there stays unread, and `(*App).dropMarksForHiddenPanes()` (`internal/ui/reducer_focus.go`) drops a mark staged while blurred if its pane is hidden when focus returns). Geometry is `panelLayout.Compute` / `ComputeFullscreen` (`internal/ui/panellayout.go`); full screen's pane is `(*App).fullscreenPanel()` |
| Sidebar selection across item/filter reorders | Capture `sidebar.Model.currentCursorKey()` **before** mutation, then `rebuildNavWithCursor`; `rebuildNavPreserveCursor` is only safe while old nav indices still identify the same items |
| Modal geometry / row hit-testing | `chrome.Boxed`, `chrome.Clickable` (list rows; declared in `internal/bubbles/chrome`, asserted by each dialog package) and `pointClickable` (a single glyph, e.g. the profile dialog's 📋; `internal/ui/reducer_modal_click.go`) |
| Channel/DM destination picker for forwarding | `channelfinder.Model.OpenForForwarding()` (joined conversations only); `Open()` restores the normal switcher |
| Refresh a workspace finder entry without losing visit recency | `rtmEventHandler.upsertFinderItem(item)` on the serialized event owner; returns the stored item and a changed flag, preserves existing `LastVisited`, seeds a new row from `LastVisitedByChannel` |
| Render a thread from cache on open, before the fetch lands | `cachedThreadRepliesCmd(threads, chID, threadTS)` (`internal/ui/app.go`); yields a `FromCache` `ThreadRepliesLoadedMsg`, which renders but never marks the thread read — only the fetched result does |
| The draft preview's rows for a compose box (ctrl+r: the draft as it will look once sent, above the box) | `(*App).draftPreview` (`internal/ui/draft_preview.go`), sized by `draftPreviewRows` and stacked by the two bottom-region renderers; state and cache on `compose.Model` (`PreviewOn`, `TogglePreview`, `SetPreview`, `PreviewBody`) |
| Bind a composer to the conversation its draft belongs to | `(*compose.Model).SetDraftContext(teamID, channelID, threadTS)` — an empty channel detaches (saves the prior conversation's draft, clears the visible input). `SetActiveChannel` sets only mention context; it does not move draft storage |
| Put text into a conversation's draft without overwriting anything newer (visible composer or stored draft) | `(*compose.Model).OfferDraft(teamID, channelID, threadTS, text)`; false when that conversation already has text or attachments. Used to give back a slash command that did not run (via `offerTypedBack`) and for `/msg`'s pre-fill |
| Name a navigable position (workspace, channel, message, thread reply) | `ui.Location`; `(*App).currentLocation` / `currentPosition` snapshot where the user is |
| Navigate to a `Location` (history walk, mark jump, permalink) | `(*App).applyLocation(loc, fromHistory)` (`internal/ui/reducer_links.go`); goes through `pendingLinkNav`, so it waits for the channel and thread to load, and refuses while an upload is in flight |
| Persistent marks in the cache | `(*cache.DB).UpsertMark` / `ListMarks` / `DeleteMark`, keyed by workspace and letter |

### App state and actions

| Need | Use |
|---|---|
| Selected message or reply in the focused pane (channel, TS, text, author, panel) | `App.selectedMessageContext()` (`internal/ui/app.go`) |
| Switch the active workspace from the UI | `App.switchWorkspace(teamID)` (`internal/ui/app.go`). Always use it: it saves a theme a cycle left pending, so a switch back to that workspace reads the new theme |
| Save a per-workspace theme | `settings.SaveTheme(teamID, name, scope)` with the workspace on screen (`a.activeTeamID`), never the backend's active one: a switch moves that first |
| Set the user's own presence, DND, or custom status from the UI (presence/DND optimistic; custom status forwards to the service and waits for the real event — see `presenceController.CustomStatus`) | `(*App).applyPresenceAction(action, snoozeMinutes)` (`internal/ui/presence_action.go`); the presence menu, custom snooze and the `/away` family use it |
| Close the thread because the user asked to (a key or a click), as opposed to a channel switch, jump, workspace switch or deleted parent closing it | `(*App).closeThreadByUser()` (`internal/ui/app.go`): `CloseThread`, then leaves full screen if the thread was the full-screen pane. Esc, q, ctrl+] (`ToggleThread`) and the breadcrumb click use it; every other path calls `CloseThread` and keeps full screen |
| Switch to a channel, joining it first if the user is not a member | `(*App).openChannel(channelfinder.ChannelResult)` (`internal/ui/open_channel.go`); the channel finder and `/open` use it |
| Resolve a command's channel argument (`#name` or a `<#C…\|name>` mention) to a channel, joined or not | `(*App).findChannelByID` (finder rows, by ID) and `matchChannelByName` (case- and accent-insensitive, channels and private channels only) in `internal/ui/slash_builtins.go`; a name the finder does not hold goes to the server through `ChannelService.SearchRemote` and comes back as `channelLookupMsg` (reduced in `reducer_slash_command.go`), because the finder's rows are only the user's own conversations. `/open` and `/join` use it |
| Remove a link preview from the selected own message (`x`): prompt, copies check, re-check, call, pending state | `(*App).removePreviewOfSelected` (`internal/ui/remove_preview.go`; its reducer is `reducer_remove_preview.go`). Before prompting and again on confirm, `(*App).previewCopiesAgree(channelID, ts)` requires every copy the App holds (the thread pane's, every channel window's in any view) to offer the same preview ids and URLs, else it refuses and sends nothing; it runs before the pending state is read, so a stale copy neither ends nor starts a removal. The prompt answers with `RemovePreviewMsg`; `(*App).removePreview` re-checks that the message still shows that attachment id with that URL, then calls `core.MessageService.RemoveAttachment(teamID, channelID, ts, attachmentID)` (wired in `cmd/slk` through `removeAttachment` (`cmd/slk/remove_preview.go`) to the client of `router.ByID(teamID)`, 10s, `AttachmentRemovedMsg` back). At most one removal per (channel, ts): `previewRemovals` holds the targeted URL and how many previews of it the message showed, and the removal's `seq`, which the UI stamps on the call's `AttachmentRemovedMsg`; an answer with another `seq` is ignored. It ends when the message shows fewer of that URL (the `message_changed` echo in `reduceNewMessage`, or a reload, checked lazily at the next `x`), on a definite refusal (`core.AttachmentRefusedError`, made in `cmd/slk/removeAttachment`), or, for a call whose outcome is unknown (any other error), after 30s. A removal Slack answered ok for is `confirmed`: it does not expire, and `x` says to reload the channel |
| Open (or find) a DM / group DM and switch to it, optionally pre-filling its draft | `(*App).openConversation(userIDs, msgPrefill)` (`internal/ui/open_conversation.go`); the new-message picker and `/msg` use it. Request IDs come from a counter that never resets; a `/msg` pre-fill whose open fails, is cancelled or is superseded is given back by `abandonMsgPrefill`, and `/msg` refuses while `openingConversation()` |
| Is composer text a slash command, and its name/args? | `parseSlashCommand(text)` (`internal/ui/slash_command.go`); `(*App).trySlashCommand(panel, c, channelID, threadTS)` runs it from either Enter branch. The result is `commandRanMsg`, reduced in `reducer_slash_command.go`; `commandFailureText` words the toast |
| Give a slash command's typed text back to the composer it came from (a built-in that refuses, a failed remote run, a failed `/msg` open), with a toast | `commandOrigin{teamID, channelID, threadTS, typed}` (`slash_command.go`), which `trySlashCommand` puts on `commandArgs.Origin`; `(*App).refuseCommand(origin, text)` is the toast plus the give-back, `(*App).offerTypedBack(origin)` the give-back alone (thread composer if the origin has a threadTS, else the channel composer; never over a newer draft). Every built-in refusal goes through `refuseCommand` |
| Add a command | `:` commands: an entry in `commands` (`internal/ui/command.go`); slash built-ins slk runs itself: an entry in `slashCommands` (`internal/ui/slash_command.go`). Both are `commandFunc(a, commandArgs{Fields, Rest, Origin})`, dispatched by `dispatchCommand` (a slash command by `splitCommandLine` + `runCommand`, so `Origin` can be set); a slash name not in the table runs through `chat.command`. A `:` command also needs a help-overlay entry in `DefaultKeyMap` (`internal/ui/keys.go`; a keyless `key.WithHelp(":name", …)` like `MarksList`, or the name beside its key like `H / :huddle`), which `TestHelpListsEveryCommand` enforces |
| An app's "working" status on a thread (Slack's `ai_assistant_status`, from `assistant.threads.setStatus`) | Arrives as `BotStatusMsg`; state on `a.typing` (`internal/ui/typing.go`): `SetBotStatus` (empty clears), `ClearBot` (the app replied), read with `WorkingBots(channel)` for the channel typing row and `ThreadBotStatuses(channel, thread)` for the thread pane. Lapses after `botStatusExpiry` (2 min, Slack's timeout) on the typing tick |
| Can the user post in an app DM? (`app_home.messages_tab_enabled` / `messages_tab_read_only_enabled`) | `boot.ConversationsInfo` (`return_app_home=true`) or `ConversationsView`, then `(*boot.AppHome).CanReceiveMessages()` (nil is true); `cmd/slk/probeMessagingCapability` wires both, fail-open, behind `core.ChannelService.MessagingCapability`. A send refused with `restricted_action_read_only*` marks the conversation (`App.readOnlyChannels`, `sendFailedCmd` in `internal/ui/read_only.go`); both set `App.composeDisabled` |
| Leave insert mode from code | `(*App).exitInsertAfterSend()` after a send (honours `[compose] stay_in_insert_after_send`); `(*App).exitInsert()` to leave whatever the option says (`ModeNormal`, both composers blurred) (`internal/ui/app.go`). Neither rechecks waiting action dialogs: a path that leaves insert mode at the user's request, as Esc does, follows with `tryOpenActionPrompt()` |
| Record that the user used an emoji (a reaction added, a pill toggled on, a `:shortcode:` in a sent message, reply or upload caption), or hand the emoji history to every picker | `(*App).recordEmojiUse(names...)`, `(*App).recordEmojiInText(text)`, `(*App).refreshEmojiFrecency()` (`internal/ui/emoji_frecency.go`); the history is `core.ReactionService.LoadFrecent` / `RecordFrecent` (the `frecent_emoji` cache table) |

### Test helpers

Everything here is unexported and lives in a `_test.go` file, so it is
reachable only from the package that declares it (all of these are
`package ui` unless noted). They are listed because the failure mode this
file exists to prevent — writing a sixteenth ad-hoc test-app builder — is
exactly what happened before Phase 0 consolidated them. Symbols are
greppable by name; no line numbers, because these files move.

| Need | Use |
|---|---|
| Build an `App` for a test or benchmark | `newTestApp(t, opts...)` (`internal/ui/testapp_test.go`) |
| The same without a `testing.TB` | `buildTestApp(opts...)` — only for the four legacy builders whose signatures must not change: `newPanelAtApp`, `sixelTestApp`, `makeBenchApp`, `makeWideScrollApp` |
| Size the App | `withSize(w, h)` (direct field assign) / `withWindowSize(w, h)` (real `tea.WindowSizeMsg` resize path). **Not interchangeable** — only the latter sets `forceSixelRepaint`. Mutually last-wins |
| Seed panes and data | `withMessages`, `withChannels`, `withWorkspaces`, `withThreadsView`, `withChannelFinderOpen` |
| Seed App-level state | `withMode`, `withView`, `withActiveChannel`, `withActiveTeam`, `withChannelService`, `withWindowSplit`. **`withActiveChannel` assigns only `a.activeChannelID`** — nothing derives a *name* from an ID, so the messages-pane header renders as a bare `#`, the statusbar as `#`, and the compose placeholder as `Message #...`. A test that asserts on any of those must push the name through the production trio itself; `newGoldenApp` does it via `nameGoldenActiveChannel` (`internal/ui/golden_test.go`) |
| Populate `a.layout` bands / pane caches (needed for mouse hit-testing) | `withRender()` |
| N plain message fixtures | `testMessageItems(n)` |
| A golden for a dialog or composer popup over the standard fixture | `dialogGoldens()` / `renderDialogGolden(t, d)` (`internal/ui/golden_dialogs_test.go`); add a scenario there for a new dialog |
| Compare or bless a full-screen frame against `testdata/golden/<name>.ansi` | `compareGolden(t, name, got)` (`internal/ui/golden_test.go`) |
| Prove two ANSI screens look identical even when their bytes differ (re-encoded SGR), or that every golden is cell-identical to a baseline directory | `cellDiff(want, got)` and `SLK_GOLDEN_BASELINE=<dir> go test ./internal/ui -run TestGoldenCellsMatchBaseline` (`internal/ui/goldencells_test.go`) |
| Re-bless goldens | the package-local `-update` flag: `go test ./internal/ui -run TestGolden -update`. It is not defined repo-wide, so `go test ./... -update` fails |
| Pin the render globals (dark theme, transparent background off, emoji images off, fixed clock) with auto-restore, for a `testing.TB` (tests and benchmarks) | `pinRenderGlobals(tb)` (`internal/ui/golden_test.go`) |
| Turn the transparent background on for a built `App` (through `applyTheme`, off again after the test); the truecolor backgrounds a frame sets, per line; how many visible cells of a row sit on the default background; the background one cell is drawn on | `makeTransparent(t, a, theme)`, `bgParams(frame)`, `defaultBgCells(s)`, and `cellBg(attrs)` for one `screenCell` (`internal/ui/transparent_golden_test.go`, on top of `screenCells` / `lineCells`). The same switch for one test outside `package ui`: `transparentDark(t)` (`internal/ui/messages/transparent_test.go`, `package messages`), `withTransparent(t)` (`internal/ui/styles/transparent_test.go`, `package styles`) |
| An `App` with every render nondeterminism pinned (theme, emoji mode, clock) | `newGoldenApp(t, opts...)`, with `goldenMessages()` / `goldenChannels()` as the fixtures |
| Drive a message through the real `Update` chain and render one frame | `updateAndRender(t, a, msg)` (`internal/ui/thread_breadcrumb_test.go`) |
| Run a command the way the runtime does (batch members concurrently, ticks on their real timers) and take the message it delivers | `deliveredMsgs[T](t, cmds...)` (`internal/ui/cmdmsgs_test.go`). Use it, not a hand-built tick message, to show what a real tick carries |
| An App at a given width, resized via `WindowSizeMsg`, channel focused, for thread-layout tests | `stackedApp(t, w, extra...)` (`internal/ui/thread_stacked_test.go`) |
| Assert which of channel / thread the last frame drew | `assertFront(t, a, wantChannel, wantThread)` (same file) |
| Assert the last frame drew one pane full screen (no rail, frame exactly the terminal's size), or the normal panes; a 200-wide App with a thread open and focused; a stackedApp in compact mode (restores the global compact style) | `assertFullscreen(t, a, pane)`, `assertPaned(t, a)`, `fullscreenThreadApp(t)`, `compactApp(t, w)` (`internal/ui/fullscreen_test.go`) |
| Record the read marks an App sends (channel and thread `Mark` calls), on an unsized `NewApp()` or on an App built elsewhere (sized, so its layout decides which panes are drawn) | `markCapture(t)` / `wireMarkCapture(t, app)`, with `feed(t, app, cmd, 0)` to run the flush tick (`internal/ui/reducer_focus_test.go`) |
| Open the thread panel AND size the App so the thread is drawn (an unsized App stacks it behind the channel, and a reply to a hidden thread is not marked read) | `openThreadOnScreen(app)` (`internal/ui/reducer_focus_test.go`) |
| Fake one service method on an `App` | `a.setChannelFetcherForTest(fn)` and its siblings, `setUploaderForTest`, `setClipboardReaderForTest`, `setReadStateReaderForTest`, `setDesktopForTest(func(*core.DesktopServiceFuncs))`, `setCommandRunnerForTest(fn)`, `setFilesystemForTest()`, `setEditorForTest()`, `setProfileFetcherForTest(fn)` (`internal/ui/services_helpers_test.go`). Calls for sibling methods of one service compose instead of replacing each other |
| Real Slack HTTP client against a synthetic response fixture, including under virtual time | `newTestClient(t, srv.Server)` / `newFakeSlack(t, bodies)` (`cmd/slk/bootstrap_adapters_test.go`); for `synctest`, replace the client's `BrowserTransport.Inner` with `handlerTransport{srv.Config.Handler}` so requests stay in-process |
| Table-drive a mode handler's keys | `runKeyCases(t, mode, []keyCase{...})` (`internal/ui/modekeys_test.go`). Calls `dispatchModeKey` directly, so it **bypasses** the reducer chain and the `ctrl+c` / bootstrap / scroll-flush gates ahead of it |
| Build a key message for such a table | `keyPress(r)` printable rune, `keyCode(c)` special key, `keyMod(c, mod)` modified key |
| Count the rows a finder-style modal is showing | `modalRows(bs)` (`internal/ui/mode_workspace_finder_test.go`) — `h - 7`; floors at 1, and is **not** valid for `newmessagepicker` |
| Read the highlighted (`▌`) row out of a rendered modal box | `modalHighlightedRow(t, box)` (same file) |
| Compare two `color.Color` without `==` panicking on a non-comparable dynamic type | `colorEqual(a, b)` (`internal/ui/mode_theme_switcher_test.go`; `package styles` has its own twin in `styles/styles_test.go`) |
| Observe a status-bar toast (no getter exists) | `statusbarText(a)` (`internal/ui/mode_presence_snooze_test.go`) |
| Record status-setter invocations | the `statusCall` struct (same file) |
| A fixture with real unread channels either side of the active one | `unreadOpts()` + `seedUnreads(t, a)` (`internal/ui/mode_normal_keys_test.go`) |
| Establish a focused pane with an asserted selection | `focusMessages(t, a)`, `focusThreadPanel(t, a)` (same file) |
| Park the message viewport at an exact `yOffset` | `scrollTo(off)` (same file) |
| Make nav-history entries resolvable | `navLookupOpt()` (same file) |
| An `App` with the main composer mid-upload, a thread open with its own draft, and workspaces T1/T2, counting uploader and workspace-switcher calls | `uploadGuardApp(t)` (`internal/ui/conversation_drafts_safety_test.go`) |
| Observe a compose cursor position or blur state (no getter exists) | `afterKeyValue(c, r)` (`internal/ui/mode_insert_keys_test.go`) |
| An `App` on `C1` showing given messages, whose attachment-button presses are recorded (and answered with an error you choose) | `actionApp(t, err, msgs...)` (`internal/ui/action_prompt_test.go`), returning the `*[]pressCall` log |
| Slackbot's "not in this channel" ephemeral with Add Them / Dismiss / Don't Show Again buttons | `mentionEphemeral(ts)` (`internal/ui/action_prompt_test.go`) |
| Deliver a message to the App through the real `Update` chain and get the messages its commands produced | `arrive(t, a, msg)` (`internal/ui/action_prompt_queue_test.go`) |
| Send a key and feed every resulting command's message back through `Update` | `pressKey(t, a, k)` (`internal/ui/action_prompt_test.go`) |
| The same, with each `tea.Batch`'s commands run last first, to catch code that relies on batch order instead of `tea.Sequence` | `pressKeyWorstOrder(t, a, k)` (`internal/ui/action_prompt_queue_test.go`) |
| The toast text among such messages | `toastText(msgs)` (`internal/ui/action_prompt_test.go`) |
| Is the confirm prompt visible and does its view contain some text? | `dialogShows(a, text)` (`internal/ui/action_prompt_queue_test.go`) |
| Click the cell where some text is drawn inside the open confirm prompt | `clickConfirmText(t, a, text)` (`internal/ui/confirm_click_test.go`) |
| An `App` with a normal message and then a given ephemeral in `C1`, the ephemeral selected in a focused messages pane | `selectedEphemeralApp(t, msg)` (`internal/ui/ephemeral_keys_test.go`) |
| An `App` in T1 for jump tests: every channel resolves, C1–C3 have fresh cached messages | `jumpMarkTestApp(t)` (`internal/ui/jump_mark_test.go`) |
| An `App` in T1 on C1 for set-mark tests, with no cache wired | `setMarkTestApp(t)` (`internal/ui/set_mark_test.go`) |
| An `App` in T1 on C1 whose first message is selected and focused, user U1, with `chat.deleteAttachment` calls recorded (and answered with an error you choose); press a key and feed every answer back through `Update` without waiting for toast clear ticks | `previewApp(t, err, msgs...)`, `pressKeyAll(t, a, k)`, `toastShows(a, text)`, `timeoutErr()`, `card(id, title, url)`, `ownMessage(ts, atts...)`, `a.setAttachmentRemoverForTest(fn)` (`internal/ui/remove_preview_test.go`, `services_helpers_test.go`). `previewApp` swaps the package var `toastTick` for one that never fires, so a toast stays readable; `pressKey` would run the 3s clear tick and wipe it, and feeds back only one level of answers |
| A previewApp whose own message P1 shows previews One and Two both in the channel pane and as the open thread's parent, the parent selected in the focused thread pane; select a thread's parent P1; a channel reload after One was removed (Two renumbered into id 1, thread parent not reloaded) | `staleParentApp(t)`, `focusThreadParent(t, a)`, `reloadChannelWithoutOne(a)` (`internal/ui/remove_preview_test.go`) |
| Record a mark directly in T1's store | `seedMark(t, app, letter, loc)` (`internal/ui/jump_mark_test.go`) |
| Press a mark chord through `App.Update` | `pressJumpChord(app, letter)`, `pressBackJump(app)` (`internal/ui/jump_mark_test.go`), `pressMarkChord(app, letter)` (`internal/ui/set_mark_test.go`) |
| Feed a jump's `ChannelSelectedMsg` back through `Update` | `driveJump(app, cmd)` (`internal/ui/jump_mark_test.go`) |
| A marks table backed by an in-memory cache, or one whose every call fails | `marksPersistForTest(t)` (`internal/ui/marks_test.go`), `failingMarksPersist()` (`internal/ui/command_marks_test.go`) |
| List a workspace's marks, failing the test on a read error | `mustListMarks(t, store, teamID)` (`internal/ui/marks_test.go`) |
| An 80x24 `App` with the marks list open over marks a (C2) and b (C3), and the box's top-left screen cell | `marksClickApp(t)` (`internal/ui/marks_mode_test.go`) |
| An `App` with given per-channel caches and an optional fetch-around, for history tests | `navFixtureApp(t, caches, fetchAround)` (`internal/ui/navhistory_test.go`) |
| Drive a history walk through the whole pipeline | `walkAndUpdate(t, app, cmd)`; `walkToCompletion(t, app, cmd)` also returns the selection and toasts (same file) |
| A nav stack's entries as channel IDs | `navEntryChannelIDs(entries)` (`internal/ui/app_test.go`) |
| An `App` in insert mode on C1/T1 whose slash-command runs are recorded and answered with an error you choose; press Enter through `Update` | `slashApp(t, err)`, `pressEnter(a)`, the `runCall` record (`internal/ui/slash_command_test.go`) |
| Register a slash built-in for one test (restored on cleanup) | `withSlashBuiltin(t, name, fn)` (`internal/ui/slash_command_test.go`) |
| A slashApp with the status setter recorded and a cached presence for T1 | `presenceSlashApp(t, presence)`, returning the App and the `*[]statusCall` log (`internal/ui/slash_builtins_test.go`) |
| A slashApp whose channel finder holds joined general (C1) and random (C2), whose server channel search answers with unjoined design (C9), and whose joins and searches are recorded | `openSlashApp(t)`, returning the App and the `*[]runCall`, joins and searches logs (`internal/ui/slash_builtins_test.go`) |
| A slashApp whose conversations.open is recorded and answers with DM D9 | `msgSlashApp(t)`, returning the App, the `*[]runCall` log and the `*[][]string` OpenConversation log (`internal/ui/slash_builtins_test.go`); `failOpenConversation(a)` then makes that open answer `NewMessageFailedMsg` |
| The draft of a conversation in T1, read by switching the composer to it (changes the composer's draft context) | `draftIn(a, channelID)` (`internal/ui/slash_builtins_test.go`) |
| Compare a request this repo builds with a scrubbed web-client capture (`{method, form, response}` JSON in `testdata/`) | `capturedForm(t, name)` (the capture's form minus `token` and the `_x_*` envelope) and `assertFormMatchesCapture(t, got, name)` (same fields and values, nothing extra; `client_token` checked for shape) (`internal/slack/command_test.go`, `package slackclient`) |
| Fixture bytes for the inline-image disk cache (a w×h PNG, no network) | `makeTestPNG(w, h)` (`internal/ui/messages/blockkit/image_sixel_test.go`, `package blockkit`); unexported twins: `makeTestPNG` in `internal/ui/messages/model_test.go`, `makeTestPNGBytes` in `internal/ui/app_imagepreview_test.go` |
| Inline emoji image placements or failed fetches without network I/O | `newFakePlaceFetcher()` (`internal/ui/messages/render_test.go`, `package messages`): `setPrerendered` seeds warm images; `err` supplies a fetch failure. Give failure cases unique URLs because `emoji.Place` negatively caches them for the process |
| Load a config from a TOML string | `loadConfigString(t, body)` (`internal/config/config_test.go`, `package config`) |
| Press Tab or Shift+Tab at the `:` prompt; check the buffer and its status-bar mirror together; is no Tab cycle in progress? | `pressTab(a, shift)`, `assertPrompt(t, a, want)`, `cmdCompletionZero(c)` (`internal/ui/mode_command_test.go`) |
| What the status bar draws after the `:` prompt (the completion list) | `afterPrompt(plain, line)` (`internal/ui/mode_command_test.go`) |
| A private `dbus-daemon` for one test (skips when not installed; no service directories, so an unowned name is never activated), a connection to it, and a spec-only Secret Service served on it | `startTestBus(t)`, `connectTestBus(t, addr)`, `serveSecretService(t, addr, svc)` with `specOnlyService()` (`internal/slackdesktop/password_linux_test.go`, `package slackdesktop`, Linux only) |
| An `App` whose emoji history is in memory (most recent first), returning the history; what the channel composer's emoji autocomplete lists after typing some text | `emojiHistoryApp(t, opts...)`, `autocompleteRows(t, a, s)` (`internal/ui/emoji_frecency_test.go`) |
| The emoji name on the reaction picker's highlighted row; move down n rows, commit and return the emoji | `highlightedEmoji(t, a)`, `commitAfterDowns(t, a, calls, n)` (`internal/ui/mode_reaction_picker_test.go`); a reaction picker whose whole emoji list is given names: `withEmoji(names...)` (`internal/ui/reactionpicker/click_test.go`, `package reactionpicker`) |
| A Slack-mode sidebar with one section of a given name, emoji and read state; put the cursor on its header; fail when a view line is wider than the sidebar or the view is not `height` lines | `sectionModel(emoji)`, `longSectionModel(emoji, rs)`, `namedSectionModel(name, emoji, rs)`, `selectHeader(t, &m)`, `assertOneLinePerRow(t, view, height, width)` (`internal/ui/sidebar/header_fit_test.go`, `package sidebar`) |
| Emoji images in a test: image mode on for one test, a fake fetcher serving realistic warm placements (with the image ID and the closing `\x1b[39m`), the kitty uploads captured | `package sidebar`: `withEmojiImages(t)` / `withEmojiImageCells(t, n)`, `newFakeEmojiFetcher()` and its `serve(url, id, onFlush)` (a 1-cell request gets `oneCellPlacement`), `captureKittyOutput(t)`, `emojiCtx(f)` (`internal/ui/sidebar/emoji_images_test.go`). `package messages`: `statusEmojiImages(t)` and `serveStatusEmoji(t, f, name, customs, id, upload)` over `newFakePlaceFetcher()` (`internal/ui/messages/status_emoji_test.go`, `render_test.go`); `thread` has its own `newFakePlaceFetcher()`, `channelfinder` its own `statusEmojiImages(t)` |
| A messages pane showing one message by an author with a given status; a DM pane for header topic tests | `statusPane(st, f)`, `dmHeaderPane(f)` (`internal/ui/messages/status_emoji_test.go`) |
| An `App` in insert mode on C1 with a draft in the channel box and its draft preview on; a frame's rows; where the draft preview is in a frame, and its plain rows | `draftApp(t, w, h, draft)`, `frameRows(a)`, `previewSpan(rows, width)`, `previewRows(t, a)` (`internal/ui/draft_preview_test.go`) |
| An `App` in insert mode on C1 for draft preview key tests (keys go through `a.Update`, so the upload gate runs); send one key; ctrl+r that fails unless it showed the box's preview | `previewKeysApp(t)`, `send(a, k)`, `previewOnWithCtrlR(t, a, c)` (`internal/ui/draft_preview_keys_test.go`) |
| The ANSI line whose plain text contains a string, so a highlight can be checked on that row; the selected-message highlight's background SGR (`48;2;R;G;B`), focused or not | `rawLineContaining(view, text)`, `selectionTintSGR(focused)` (`internal/ui/insert_mode_highlight_test.go`) |
| The `38;2` / `48;2` SGR fragment lipgloss emits for a color, and a rendered line through its left ▌ | `sgrChannel(ground, c)`, `borderHead(line)` (`internal/ui/compose/model_test.go`, `package compose`) |

### Known duplication — do not add to it

These are tracked in the refactor plan and are being consolidated. Do not copy
them as templates:

- **`messages.Model` and `thread.Model`** share `bubbles/entrylist` (scroll,
  selection, window, scrollbar) and `messages.RenderMessage` (one message
  renderer); their differences are `entrylist` options and `RenderOptions`
  fields, each documented where it is declared. Do not add a pane-specific
  copy of either.
- **`convertAndCacheHistory` / `fetchChannelMessages` / `fetchThreadReplies`** in
  `cmd/slk/main.go` have ~85% duplicated bodies.

## Conventions

**Adding a reusable helper?** Add it to the tables above in the same commit. An
unlisted helper gets re-implemented. When this file and the code disagree, the
code is right and this file is a bug — fix it.

**Two implementations that must stay parallel?** Express it as an interface with
a compile-time assertion (`var _ Chrome = (*Model)(nil)`) or a lockstep test.
Not a comment. The comment that used to sit at the top of
`internal/ui/thread/model.go` — "this shape mirrors
`internal/ui/messages.viewEntry` exactly; keeping them in lockstep means…" —
was the counter-example: prose asking humans to maintain a 377-line invariant
by hand. Phase 0 pinned it with a lockstep test, and Phase 3 replaced both with
structure. The `contentPane` interface (`internal/ui/content_pane.go`), which
both panes are asserted against at compile time, covers what the App calls on
either pane. Every behavioural difference between them is an `entrylist` option
(`WithEagerClamp`, `WithCursorFollowsScroll`, …) or a `RenderOptions` field,
documented where it is declared (the `entrylist` options are tested per
setting in `entrylist_test.go`). Do the same: when you find a comment standing
in for a check, replace it with the check.

**Extract the substrate, not the widget.** Share the uniform part; leave the
divergent part alone. Forcing genuinely different behavior into a common shape
is worse than the duplication it removes.

**Refactoring?** Moving functions is free — two phases of the prior refactor
moved ~3,000 lines with zero test changes. Moving *state* is not: `internal/ui`
test files make **2,905 references to 142 distinct unexported `App` fields and
methods** (type-checked count, not a grep; measured at the end of Phase 0, up
from 1,995 / 121 before it — Phase 0's characterization tests added ~910 of
them). Budget roughly **220 mechanical test-line edits per 10 extractions**, and
expect five members to dominate: `messagepane` (243), `compose` (204),
`focusedPanel` (146), `mode` (129) and `activeChannelID` (125) are 847 of the
total between them. Method and per-member breakdown:
`docs/superpowers/plans/2026-09-06-architecture-refactor.md`, Phase 0 "Achieved".

**Found a bug while refactoring?** Record it, annotate it, raise it separately.
A refactor commit that also changes behavior cannot be reviewed.

## Workflow

Per the README: brainstorm the design first, write tests, then implement.
Designs go to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`,
implementation plans to `docs/superpowers/plans/`.

Before opening a PR: `go build ./...`, `go vet ./...`, `go test ./... -race`,
`gofmt -l .` empty.
