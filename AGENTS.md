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
                        a time; confirmprompt is the first
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
| Case/accent-insensitive fold for matching | `text.Fold(s)` |
| Slack mrkdwn → plain text | `messages.FlattenMrkdwn`, `messages.FlattenMrkdwnWithUserGroups` |
| Search-term highlighting (ANSI/OSC-safe) | `messages.HighlightSearchTerms`, `messages.SearchHighlightSGR` |
| Re-assert a background (or background+foreground) after every SGR reset, so nested lipgloss renders don't drop it | `bubbles/ansi.ReapplyAfterResets(text, style)` — the one implementation; `messages.ReapplyBgAfterResets` delegates to it for existing callers. The theme's sequences come from `messages.BgANSI()` / `messages.FgANSI()` |
| Extract links from message text | `messages.ExtractLinks` |
| Every link a message offers to `o` (text links + Block Kit blocks + links in legacy attachment `text`/`pretext`/`title_link`/fields/nested blocks + permalinks of messages it shares, from attachment `from_url`) | `messages.MessageLinks(msg)` |
| Does message text mention the current user? | `mention.InText(text, selfUserID)` |
| Reaction pill rendering | `messages.ReactionPillText` |
| Render one `MessageItem` (author line, body, Block Kit, attachments, reactions, hits) for a content pane | `messages.RenderMessage(msg, width, rc, opt)` with a `RenderContext` the pane builds (`renderContext()` in each pane) and `RenderOptions` for its differences; `messages.BlockkitContext(msg, rc)` for the Block Kit host context. Both panes; the thread pane's options are `thread.(*Model).renderMessage` |
| Turn a `RenderedMessage` into list rows: fill, invisible/tinted left border, the long-timestamp selected variant, the plain-text mirror (the part of building an `entrylist.Entry` both panes share) | `messages.WrapRows(msg, rendered, width, messages.NewWrapStyles(focused))` returning `WrappedRows` (`Lines`, `Selected`, `Plain`, `ShortSrc`); the panes add ID/Ref/hits and (channel pane) the spacer rows. `WrapStyles.SelectedVariant` builds the short-timestamp variant for `EntryPayload.SelectedLines`. The thread parent row is not wrapped this way: it is re-rendered every frame |
| Apply a reaction add/remove event idempotently; the reaction-pill cursor | `messages.ApplyReaction`, `messages.ReactionNav` |
| A content pane's per-entry payload next to its `entrylist.Entry` (short-timestamp selected variant, kitty flushes, sixel rows), and the rows to draw for the cursor entry | `messages.EntryPayload`, `(*EntryPayload).SelectedLines`; the emoji-image context both panes take is `messages.EmojiContext` |
| Date label from a Slack ts | `messages.DateFromTS`, `messages.FormatDateSeparator`, `messages.FormatShortDate` (compact form for a message header: "Today", "Jan 2") |
| Date-qualified timestamp for the selected message header | `messages.LongTimestamp(ts, short)`, `messages.SelectedHeader(rendered, header, ts, short, maxWidth)`; any other header timestamp swap that must fit the width budget: `messages.ReplaceHeaderTimestamp(rendered, header, short, replacement, maxWidth)` |
| Clock layout `<!date` tokens render `{time}`/`{time_secs}` with | `messages.SetDateTokenTimeLayout(layout)` — seeded once at startup from `appearance.timestamp_format` (`cmd/slk/main.go`); empty keeps the default `"3:04 PM"`. Package-level (like theme state) so every render path agrees |
| mpdm channel name → human name | `slackfmt.FormatMPDMName` |
| Channel-type glyph (`#` / `◆` / `●`) | `messages.ChannelGlyph(chType)` |
| Marker row for a message only the current user can see | `messages.EphemeralLabel()` — drawn above the author line in both panes |
| A message's pressable legacy buttons (selects excluded) | `messages.ButtonActions(msg)`, `messages.HasButtonActions(msg)` |
| The hint under an interactive message ("b to respond" for an ephemeral with buttons, else "open in Slack") | `messages.InteractiveHint(msg)`; both panes |
| Open the action dialog for an ephemeral's buttons | `(*App).openActionPrompt(actionPromptRef)` (`internal/ui/action_prompt.go`); queueing and the interruption rule are `enqueueActionPrompt` / `tryOpenActionPrompt` |
| Replace a pane's list with fetched history without erasing ephemerals already shown | `keepEphemerals(fetched, shown)` (`internal/ui/ephemeral_keep.go`); only for a fetch of the conversation already on screen |
| Slack permalink parsing | `slackurl.Parse` |
| Huddle deep link for a conversation (`https://app.slack.com/huddle/<TEAM>/<CHANNEL>`) | `slackurl.HuddleURL` |
| The channel a user's huddle is in (users.info) | `slackclient.UserProfile.HuddleStateChannelID` (wraps `*slack.User`; `GetUserProfile`/`GetUserProfileContext` are hand-rolled POSTs because slack-go's `UserProfile` drops that field) |
| Slack ts → `time.Time` (whole seconds, any timezone) | `export.TimeFromTS` |
| A since/until/overlap date range in a timezone, and "is this ts in it?" | `export.NewWindow`, `export.Window.Contains` |
| Sleep out a slack-go rate-limit error (ctx-aware) | `slackclient.WaitOutRateLimit` |
| Page through all channel history in a ts range / a thread's replies in a ts range | `(*slackclient.Client).WalkHistory`, `GetRepliesBetween` |
| Press a legacy attachment button (`chat.attachmentAction`, payload byte-identical to the web client's) | `(*slackclient.Client).AttachmentAction(ctx, AttachmentActionRequest)`, with `service_id` = the posting app's bot ID |
| Press a legacy attachment button from the UI | `core.InteractionService.PressAttachmentAction` (App field `a.interactions`); wired in `cmd/slk` via `pressAttachmentAction` to the active workspace's `Client.AttachmentAction` |
| Run a slash command (`chat.command`, request shape copied from the web client; arguments as rich text via `mrkdwn.Convert`) | `(*slackclient.Client).RunCommand(ctx, CommandRequest)`; a refusal (`unknown_command`, …) is `*slackclient.CommandRejectedError` |
| Run a slash command from the UI | `core.CommandService.Run` (App field `a.commandSvc`); wired in `cmd/slk` via `runCommand` to the `Client.RunCommand` of the workspace the command was typed in (`teamID`, looked up with `router.ByID`, not the active one). Slack's refusal arrives as `*core.CommandError{Code}` |
| A web-client `client_token` (`web-<unix ms>`) for any request that sends one | `newClientToken()` (`internal/slack`); the only generator |
| Emoji shortcode → glyph | `emoji.Sprint`, `emoji.CodeMap`, `emoji.StripSkinTone` |
| Does Block Kit already render the message body? | `blockkit.RendersBody(blocks)`, `messages.BlocksCarryBody(msg)` |
| Current DND state from a Slack API result | `slack.DNDStateFromStatus` |
| Persist a full Web API profile's custom status and huddle (including clears) | `cmd/slk/applyProfileStatus(teamID, userID, profile, db, send)` after `UpsertUser`, before any resolved-user notice or row repair; `UpsertUser` alone deliberately preserves status on conflict |
| Drop cached thread replies a complete `conversations.replies` response no longer contains (missed deletes, Slackbot ephemerals) | `(*cache.DB).PruneThreadReplies(channelID, threadTS, returnedTS, fetchStartUnix)`; soft-deletes, never touches the parent or rows cached after the fetch began |
| Peer custom status, DND and huddle rendering | `ui/peerstatus` (`Status`, glyph/expiry/summary methods); `messages.AuthorStatusSuffix` for author headers |
| Workspace app/bot classification, from any goroutine | `WorkspaceContext.IsBotUser` / `MarkBotUser`; private synchronized set, never directly read/write a plain bot-ID map |
| Usergroup map helpers | `usergroups.Copy`, `usergroups.Equal`, `usergroups.Display` |
| A workspace's user ID → display name, from any goroutine in `cmd/slk` | `wctx.UserNames` (`*userNameStore`: `Get`/`Set`, `MentionedNames(text)` for one message; `lookupUserCached` / `resolveUserCached` add the SQLite fallback). Just `Set`: hand the UI the map from `SnapshotForUI()` and pass only its version to `NotifyFrom`, which makes every later new or changed name reach the UI as `UserResolvedMsg`. Never keep or read the map you handed over |
| User IDs mentioned in message text | `slackfmt.MentionedUserIDs(text)` |
| Channel IDs mentioned in message text (`<#C…\|name>`), or each with its label | `slackfmt.MentionedChannelIDs(text)`; `slackfmt.MentionedChannels(text)` returns `ChannelMention{ID, Label}` |
| Wake the serialized WebSocket event owner for local work | Implement `slackclient.PendingEventHandler` (`PendingEvents() <-chan struct{}`, `OnPendingEvents()`). The dispatcher selects the wake-up channel even on an idle socket and serializes the drain with Slack callbacks; nil disables it, and the channel must never close |
| Apply a deferred DM profile to conversation snapshots | `userResolver.queueResolvedPeer(userID)` after caching a resolved profile; `rtmEventHandler.OnPendingEvents()` drains relevant DM peers on the serialized event owner even on an idle socket. `rtmEventHandler.repairDMPeer(userID)` is the shared cache-to-row repair used by that drain and reconnect reconciliation. `rememberConversation` mirrors notifier metadata; `publishConversation` updates only the active UI |
| Context-bounded synchronous peer resolution at reconnect | `userResolver.ResolveNowContext(ctx, ids)` batches first; `resolveOneContext(ctx, userID)` is the fallback. Both share cache/name/workspace-scoped notifications, never mutate workspace slices |
| userBoot IM → conversation metadata | `cmd/slk/bootIMConversation`; callers decide whether an ordinary open IM or a closed starred IM is eligible |
| Starred conversation membership / recover missing sidebar rows | `SectionStore.StarredConversationIDs()` (locked defensive copy); `cmd/slk/missingStarredConversations` (read-only metadata hydration), then the existing `buildChannelItem` / `addConversation` paths |
| Workspace-relative cached external-user flags | `cache.DB.ExternalUsers(workspaceID)` returns a caller-owned map, derived from `User.HomeTeamID`; the legacy boolean is only a fallback within the row's original workspace |
| A WebSocket message's author ID and its UI `MessageItem` (bot-ID fallback, display-name resolution, file/block/attachment conversion) | `(*rtmEventHandler).messageAuthor`, `(*rtmEventHandler).messageItemFromEvent` (`cmd/slk/rtm_handler.go`); shared by `OnMessage` and `OnEphemeralMessage` |
| Legacy attachment action ids slack-go drops | `slackclient.EphemeralMessage.ActionIDs` (decoded from the raw frame), applied with `cmd/slk/applyActionIDs` |
| Slack original-avatar URL → sized CDN URL (e.g. 72px) | `avatar.SizedURL(orig, px)`; returns non-Slack-original URLs unchanged |
| Copy text to the clipboard | `App.clipboardWrite` / `SetClipboardWriter`; `cmd/slk/newClipboardWriter` selects local macOS `pbcopy` or terminal OSC 52 |

### UI chrome

| Need | Use |
|---|---|
| Scrollbar gutter on a rendered pane | `bubbles/scrollbar.Overlay`, `bubbles/scrollbar.Visible` |
| Lay out a wrapping row of labelled controls (buttons, select labels) with positions for hit-testing | `internal/bubbles/buttonrow` (`Layout`, `Rows`, `RowsFunc`); used by Block Kit actions and the confirm prompt |
| Draw a dialog: its box, title / filter / body / footer layout, filter line, list window, selected-row marker, scrollbar gutter, and the composer popups' box and marker | `bubbles/chrome` (`Frame`, `Box`, `FilterInput`, `Window`, `List`, `Marker`, `BodyTop`, `Popup`, `PopupMarker`), drawn with `messages.ChromeStyles()` called per draw. A dialog keeps its own fields, matching and row content; see the package doc |
| A scrollbar thumb's rows, for a gutter you draw yourself | `bubbles/scrollbar.Thumb` (the one formula; `Overlay` and `chrome.List` use it) |
| Centered modal over a dimmed backdrop | `ui/overlay.DimmedOverlay` |
| Prompt modal with N buttons (title, wrapped body of up to 8 lines, buttons; yes/no is `Open`, any buttons via `OpenChoice(confirmprompt.Choice)`, `Button.Then` for a follow-up step) | `bubbles/confirmprompt` (`New`, `Open`, `OpenChoice`, `Update`, `PressHighlighted`, `View`, `SetStyles`, `SetWidth`; options `WithStyles`/`WithKeyMap`/`WithWidth`; `KeyMap` field). In the App, open it with `App.openConfirmPrompt` (`internal/ui/confirm.go`), which also maps the theme onto its styles and composites it |
| Text selection ranges and anchors | `bubbles/selection` (`Range`, `Anchor`, `LessOrEqual`) |
| A scrollable, selectable list of pre-rendered rows: line offsets, scroll offset and snapping, the visible window, text selection and copy, clickable-rect translation, the scrollbar | `bubbles/entrylist` (`New` with options, `SetHeight`, `SetEntries`, `Layout`, `Compose`). The channel and thread panes are its two owners; each divergence between them is an `entrylist` option, documented on the option |
| The theme mapped onto a content pane's entrylist styles (padding, scrollbar, selection highlight); the thread pane calls it; the channel pane uses the unexported `themeListStyles` it wraps | `messages.ThemeListStyles()` (`internal/ui/messages/pane_list.go`) |
| Do something to whichever message pane (channel or thread) is focused or named by a panel, without a messages/thread branch | the `contentPane` interface, `(*App).contentPaneFor(panel)` and `(*App).focusedContentPane()` (returns the pane and its channel ID) in `internal/ui/content_pane.go`; both panes are asserted against it at compile time. Built on it: `handleReactionNav`, `openReactionPicker`, `toggleReactionOnSelected` (`app.go`) and the click hit tests `toggleReactionAt` (reaction pill) and `openImagePreviewAt` (inline image, via `HitTestImage`) (`reducer_mouse.go`), each taking the pane and its channel ID |
| Theme colors and styles | `ui/styles` (`Username`, `SelectionStyle`, `SearchHighlightStyle`, `MentionBadgeStyle`, `UserColor`) |
| Change the theme inside the App | `App.applyTheme(name)` (`internal/ui/theme.go`): applies it, invalidates render caches and pushes fresh styles to components that snapshot them. A component that holds a style snapshot (anything in `internal/bubbles`) gets its push added there, not at each call site. `entrylist` is the exception: its owners (the channel and thread panes) call `SetStyles` at the top of every `View`, so `applyTheme` only invalidates for them. `styles.CurrentTheme()` names the applied one |
| Status-bar toast that clears itself | `toastWithClear(a, text, d)` (`internal/ui/reducer_io.go`); `toastUntilReplaced(a, text)` only for a toast a later one replaces on purpose (upload progress). Set toasts on the Update goroutine, never inside a `tea.Cmd`; `statusbar.ToastSeq()` identifies the toast showing, so a clear tick leaves a newer toast alone. `TestToastsGoThroughToastSetters` fails on a direct `statusbar.SetToast` with text |
| Window tree geometry | `ui/wintree` |
| Sidebar selection across item/filter reorders | Capture `sidebar.Model.currentCursorKey()` **before** mutation, then `rebuildNavWithCursor`; `rebuildNavPreserveCursor` is only safe while old nav indices still identify the same items |
| Modal geometry / row hit-testing | `chrome.Boxed`, `chrome.Clickable` (list rows; declared in `internal/bubbles/chrome`, asserted by each dialog package) and `pointClickable` (a single glyph, e.g. the profile dialog's 📋; `internal/ui/reducer_modal_click.go`) |
| Channel/DM destination picker for forwarding | `channelfinder.Model.OpenForForwarding()` (joined conversations only); `Open()` restores the normal switcher |
| Refresh a workspace finder entry without losing visit recency | `rtmEventHandler.upsertFinderItem(item)` on the serialized event owner; returns the stored item and a changed flag, preserves existing `LastVisited`, seeds a new row from `LastVisitedByChannel` |
| Render a thread from cache on open, before the fetch lands | `cachedThreadRepliesCmd(threads, chID, threadTS)` (`internal/ui/app.go`); yields a `FromCache` `ThreadRepliesLoadedMsg`, which renders but never marks the thread read — only the fetched result does |
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
| Set the user's own presence or DND from the UI (optimistic cache + status bar, then the service) | `(*App).applyPresenceAction(action, snoozeMinutes)` (`internal/ui/presence_action.go`); the presence menu, custom snooze and the `/away` family use it |
| Switch to a channel, joining it first if the user is not a member | `(*App).openChannel(channelfinder.ChannelResult)` (`internal/ui/open_channel.go`); the channel finder and `/open` use it |
| Resolve a command's channel argument (`#name` or a `<#C…\|name>` mention) to a channel, joined or not | `(*App).findChannelByID` (finder rows, by ID) and `matchChannelByName` (case- and accent-insensitive, channels and private channels only) in `internal/ui/slash_builtins.go`; a name the finder does not hold goes to the server through `ChannelService.SearchRemote` and comes back as `channelLookupMsg` (reduced in `reducer_slash_command.go`), because the finder's rows are only the user's own conversations. `/open` and `/join` use it |
| Open (or find) a DM / group DM and switch to it, optionally pre-filling its draft | `(*App).openConversation(userIDs, msgPrefill)` (`internal/ui/open_conversation.go`); the new-message picker and `/msg` use it. Request IDs come from a counter that never resets; a `/msg` pre-fill whose open fails, is cancelled or is superseded is given back by `abandonMsgPrefill`, and `/msg` refuses while `openingConversation()` |
| Is composer text a slash command, and its name/args? | `parseSlashCommand(text)` (`internal/ui/slash_command.go`); `(*App).trySlashCommand(panel, c, channelID, threadTS)` runs it from either Enter branch. The result is `commandRanMsg`, reduced in `reducer_slash_command.go`; `commandFailureText` words the toast |
| Give a slash command's typed text back to the composer it came from (a built-in that refuses, a failed remote run, a failed `/msg` open), with a toast | `commandOrigin{teamID, channelID, threadTS, typed}` (`slash_command.go`), which `trySlashCommand` puts on `commandArgs.Origin`; `(*App).refuseCommand(origin, text)` is the toast plus the give-back, `(*App).offerTypedBack(origin)` the give-back alone (thread composer if the origin has a threadTS, else the channel composer; never over a newer draft). Every built-in refusal goes through `refuseCommand` |
| Add a command | `:` commands: an entry in `commands` (`internal/ui/command.go`); slash built-ins slk runs itself: an entry in `slashCommands` (`internal/ui/slash_command.go`). Both are `commandFunc(a, commandArgs{Fields, Rest, Origin})`, dispatched by `dispatchCommand` (a slash command by `splitCommandLine` + `runCommand`, so `Origin` can be set); a slash name not in the table runs through `chat.command` |
| An app's "working" status on a thread (Slack's `ai_assistant_status`, from `assistant.threads.setStatus`) | Arrives as `BotStatusMsg`; state on `a.typing` (`internal/ui/typing.go`): `SetBotStatus` (empty clears), `ClearBot` (the app replied), read with `WorkingBots(channel)` for the channel typing row and `ThreadBotStatuses(channel, thread)` for the thread pane. Lapses after `botStatusExpiry` (2 min, Slack's timeout) on the typing tick |
| Can the user post in an app DM? (`app_home.messages_tab_enabled` / `messages_tab_read_only_enabled`) | `boot.ConversationsInfo` (`return_app_home=true`) or `ConversationsView`, then `(*boot.AppHome).CanReceiveMessages()` (nil is true); `cmd/slk/probeMessagingCapability` wires both, fail-open, behind `core.ChannelService.MessagingCapability`. A send refused with `restricted_action_read_only*` marks the conversation (`App.readOnlyChannels`, `sendFailedCmd` in `internal/ui/read_only.go`); both set `App.composeDisabled` |

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
| An `App` with every render nondeterminism pinned (theme, emoji mode, clock) | `newGoldenApp(t, opts...)`, with `goldenMessages()` / `goldenChannels()` as the fixtures |
| Drive a message through the real `Update` chain and render one frame | `updateAndRender(t, a, msg)` (`internal/ui/thread_breadcrumb_test.go`) |
| Run a command the way the runtime does (batch members concurrently, ticks on their real timers) and take the message it delivers | `deliveredMsgs[T](t, cmds...)` (`internal/ui/cmdmsgs_test.go`). Use it, not a hand-built tick message, to show what a real tick carries |
| An App at a given width, resized via `WindowSizeMsg`, channel focused, for thread-layout tests | `stackedApp(t, w, extra...)` (`internal/ui/thread_stacked_test.go`) |
| Assert which of channel / thread the last frame drew | `assertFront(t, a, wantChannel, wantThread)` (same file) |
| Fake one service method on an `App` | `a.setChannelFetcherForTest(fn)` and its siblings, `setUploaderForTest`, `setClipboardReaderForTest`, `setReadStateReaderForTest`, `setDesktopForTest(func(*core.DesktopServiceFuncs))`, `setCommandRunnerForTest(fn)`, `setFilesystemForTest()`, `setEditorForTest()`, `setProfileFetcherForTest(fn)` (`internal/ui/services_helpers_test.go`). Calls for sibling methods of one service compose instead of replacing each other |
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
| The toast text among such messages | `toastText(msgs)` (`internal/ui/action_prompt_test.go`) |
| Is the confirm prompt visible and does its view contain some text? | `dialogShows(a, text)` (`internal/ui/action_prompt_queue_test.go`) |
| Click the cell where some text is drawn inside the open confirm prompt | `clickConfirmText(t, a, text)` (`internal/ui/confirm_click_test.go`) |
| An `App` with a normal message and then a given ephemeral in `C1`, the ephemeral selected in a focused messages pane | `selectedEphemeralApp(t, msg)` (`internal/ui/ephemeral_keys_test.go`) |
| An `App` in T1 for jump tests: every channel resolves, C1–C3 have fresh cached messages | `jumpMarkTestApp(t)` (`internal/ui/jump_mark_test.go`) |
| An `App` in T1 on C1 for set-mark tests, with no cache wired | `setMarkTestApp(t)` (`internal/ui/set_mark_test.go`) |
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
| Load a config from a TOML string | `loadConfigString(t, body)` (`internal/config/config_test.go`, `package config`) |

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
