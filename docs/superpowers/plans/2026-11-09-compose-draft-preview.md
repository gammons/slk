# Draft preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `ctrl+r` in insert mode shows a live, send-faithful rendering of the draft in a small pane above that compose box, and sending closes it.

**Architecture:** The on/off state, the translated-and-converted draft, and the cached rendered body all live in `compose.Model` (no `App` fields). The body is drawn by `messages.RenderMessage` with a new `RenderOptions.BodyOnly`, using a render context each pane hands out (`DraftRenderContext`, on the `contentPane` interface). One App function, `(*App).draftPreview`, builds the pane's rows (title rule, then the body or one dim note, capped and tail-cut), and the two bottom-region renderers stack it above the spacer, so pickers sit between it and the box.

**Tech Stack:** Go, bubbletea v2, lipgloss v2, `internal/slack/mrkdwn`, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-11-09-compose-draft-preview-design.md` (approved by Grant on SLK-908, 2026-11-09). Read it first; this plan argues from it.

## Global Constraints

- Name: **draft preview** everywhere: code identifiers (`Preview…` inside `compose`, `draftPreview…` in `internal/ui`), help text, wiki.
- Key: `ctrl+r`, insert mode only, bound in `DefaultKeyMap` as field `DraftPreview`, help `key.WithHelp("ctrl+r", "show / hide the draft preview")`. Not added to `isAppHotkey`.
- Off by default; per compose box; no config option, no persistence.
- Closes on: send (channel, thread, upload success, slash command run), the box moving to another conversation (`SetDraftContext` with a different key), saving or cancelling an edit (`cancelEdit`). Stays on through Esc to normal mode, `i` again, and `ctrl+u`.
- Send path must stay byte-identical: same `TranslateMentionsForSend`, same `mrkdwn.Convert`.
- No debounce. Render at most once per drawn frame for a changed draft.
- `internal/ui` does no I/O; `internal/slack/mrkdwn` is already allowed (`boundary_test.go` bans exact paths).
- Emoji images off in the preview (no `PlaceCtx`).
- Exact copy: title `── draft preview ` then `─` to the width; notes `nothing to preview`, `slash command, not a message`, `caption is sent as typed`; cut marker `…`.
- Cap: `max(3, inner/3)` rows including the title, where `inner = contentHeight - 2`; never so tall that the messages area goes below 3 rows; fewer than 2 rows available draws no preview.
- Tests: stdlib `testing`, white-box, `newTestApp`; goldens via `compareGolden`; re-bless with `go test ./internal/ui -run TestGolden -update`.
- Local checks with `CGO_ENABLED=0` (no `-race` here; CI runs it).

## Review Focus

1. **Content wider than the pane** (a 200-char URL with no break, a fenced code line, CJK and emoji glyphs): every preview row is exactly the box width and the pane border stays intact. Test in Task 5 (`TestDraftPreviewRowsFitWidth`).
2. **A short terminal** (80x12) with a 40-line draft: the preview shrinks or disappears, the messages area keeps 3 rows, nothing panics. Test in Task 5 (`TestDraftPreviewShortTerminal`).
3. **Whitespace-only draft** (`"  \n\n "`): shows `nothing to preview`, not a blank pane. Test in Task 5 (`TestDraftPreviewNotes`).
4. **Theme switch while open**: the preview redraws in the new theme, not from a stale cache. Test in Task 5 (`TestDraftPreviewFollowsTheme`).
5. **Editing a message whose text starts with `/`**: rendered as a message (edits never run commands), not the slash note. Test in Task 5 (`TestDraftPreviewNotes`, case `edit starting with slash`).

---

### Task 1: Cache the mention list translation uses

A refactor with no behaviour change: `sendMentions` rebuilds and sorts every user, group and channel on each call (8 ms of a 15 ms `TranslateMentionsForSend` at 20,000 users, measured on this box). The preview calls translation per changed draft, so the list must be built once per change of mention data.

**Files:**
- Modify: `internal/ui/compose/model.go` (fields near `reverseNames`; `SetUsers`, `SetUserGroups`, `SetChannels`; `sendMentions`, `TranslateMentionsForSend`, `MentionsForEdit`)
- Test: `internal/ui/compose/model_test.go`

**Interfaces:**
- Produces: `func (m *Model) TranslateMentionsForSend(text string) string` (now a pointer receiver; output unchanged); `func (m *Model) MentionsForEdit(text string) string` (pointer receiver); unexported `mentionGen uint64` on `Model`, bumped by every write of `reverseNames`, `userGroups`, `reverseChannels`. Task 2 keys on `mentionGen`.

- [ ] **Step 1: Write the failing tests**

`TestTranslateSeesMentionDataChanges`: on `New("general")`, translate `"hi @bob #ops @team"` → unchanged. Then `SetUsers([]mentionpicker.User{{ID: "U2", DisplayName: "bob"}})` → `"hi <@U2> #ops @team"`; then `SetChannels([]channelpicker.Channel{{ID: "C9", Name: "ops"}})` → `"hi <@U2> <#C9|ops> @team"`; then `SetUserGroups(map[string]string{"S1": "team"})` → `"hi <@U2> <#C9|ops> <!subteam^S1|@team>"`. Then `SetUsers(nil)` → the `@bob` stays `@bob`.

`TestSendMentionsBuiltOncePerChange`: call `m.sendMentions()` twice with no setter between; assert the two slices share the backing array (`&a[0] == &b[0]`). After `SetUsers`, the next call returns a different backing array.

`BenchmarkTranslateMentionsForSend`: 20,000 users `person.<i>` / `U%05d`, draft = 25 × `"- ship **v0.28** on Tuesday with @person.42 and [the notes](https://example.com/x)\n"`, `b.Loop()` over `TranslateMentionsForSend(draft)`.

- [ ] **Step 2: Run them**

Run: `CGO_ENABLED=0 go test ./internal/ui/compose -run 'TestTranslateSeesMentionDataChanges|TestSendMentionsBuiltOncePerChange' -v`
Expected: the first PASSES (behaviour is already right), the second FAILS (a fresh slice each call). Keep the first: it is the guard for the cache invalidation.

- [ ] **Step 3: Implement**

Add `sendEntries []sendMention` and `mentionGen uint64` to `Model`. `SetUsers`, `SetUserGroups` and `SetChannels` set `m.sendEntries = nil` and `m.mentionGen++`. `sendMentions` becomes `func (m *Model) sendMentions() []sendMention`: return `m.sendEntries` when non-nil, else build exactly as today, store, return. Change `TranslateMentionsForSend` and `MentionsForEdit` to pointer receivers. Callers must not modify the returned slice; say so on `sendMentions`.

- [ ] **Step 4: Run the package and the benchmark**

Run: `CGO_ENABLED=0 go test ./internal/ui/compose ./internal/ui -count=1` → PASS.
Run: `CGO_ENABLED=0 go test ./internal/ui/compose -run x -bench TranslateMentionsForSend -benchtime 20x` → record ns/op for the PR (before: about 15 ms/op).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/model.go internal/ui/compose/model_test.go
git commit -m "refactor(compose): build the mention list once per change of mention data"
```

### Task 2: Draft preview state and cached body in the composer

**Files:**
- Create: `internal/ui/compose/preview.go`
- Modify: `internal/ui/compose/model.go` (`clearVisibleDraft` clears the flag; new fields on `Model`)
- Test: `internal/ui/compose/preview_test.go`

**Interfaces:**
- Consumes: `mentionGen`, `(*Model).TranslateMentionsForSend` (Task 1); `mrkdwn.Convert(string) (string, *slack.RichTextBlock)`.
- Produces:
  - `func (m *Model) PreviewOn() bool`
  - `func (m *Model) TogglePreview() bool` (returns the new state; `dirty()`)
  - `func (m *Model) SetPreview(on bool)` (`dirty()` on a change)
  - `type PreviewRender func(mrkdwn string, width int) string`
  - `type RenderKey struct{ Theme, Pane, Window int64 }`: everything the render reads besides the mrkdwn and width; the caller changes it when any of that changes.
  - `func (m *Model) PreviewBody(width int, key RenderKey, render PreviewRender) string`: `render(mrkdwn.Convert(m.TranslateMentionsForSend(m.Value())), width)`, cached in two levels: the mrkdwn on (`Value()`, `mentionGen`), the rendered body on (mrkdwn, width, key).

- [ ] **Step 1: Write the failing tests**

- `TestPreviewOffByDefault`: `New("g").PreviewOn() == false`.
- `TestTogglePreview`: true, then false; `SetPreview(true)` then `PreviewOn()`.
- `TestResetClosesPreview`: `SetPreview(true)`, `Reset()` → off.
- `TestSwitchingConversationClosesPreview`: `SetDraftContext("T1","C1","")`, `SetPreview(true)`, `SetDraftContext("T1","C2","")` → off; and on a fresh model, `SetDraftContext("T1","C1","")` twice with the preview on in between → still on.
- `TestPreviewBodyIsTheSendPath`: users `{U2 "bob"}`, value `"**hi** @bob"`, identity render (`func(s string, _ int) string { return s }`) → equals the first result of `mrkdwn.Convert(m.TranslateMentionsForSend(m.Value()))`, and contains `<@U2>`.
- `TestPreviewBodyCaches`: a render that counts calls. Same (width 40, key K) twice → 1 call. Change value → 2. Width 41 → 3. `RenderKey{Theme: 1}` → 4. `SetUsers` with a user the draft does not mention (mrkdwn unchanged) → still 4. `SetUsers` with `bob` when the draft says `@bob` → 5.

- [ ] **Step 2: Run them to see them fail**

Run: `CGO_ENABLED=0 go test ./internal/ui/compose -run 'Preview' -v`
Expected: FAIL to compile (undefined `PreviewOn` …).

- [ ] **Step 3: Implement in `preview.go`**

Fields on `Model` (declared in `model.go` next to `broadcast`, with a comment that the draft preview is per box, off by default and closed by `clearVisibleDraft`): `preview bool`, and two small unexported cache structs, one holding `{text string, gen uint64, mrkdwn string, ok bool}` and one `{mrkdwn string, width int, key RenderKey, body string, ok bool}`. `clearVisibleDraft` sets `m.preview = false`; this one line is what closes it on send, clear and conversation switch. `PreviewBody` does no work on a double hit.

- [ ] **Step 4: Run the package**

Run: `CGO_ENABLED=0 go test ./internal/ui/compose -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/compose/preview.go internal/ui/compose/preview_test.go internal/ui/compose/model.go
git commit -m "feat(compose): draft preview state and a cached send-faithful body"
```

### Task 3: Body-only rendering and a draft render context per pane

**Files:**
- Modify: `internal/ui/messages/render_message.go` (`RenderOptions`, `RenderMessage`)
- Modify: `internal/ui/messages/model.go` (new method next to `renderContext`)
- Modify: `internal/ui/thread/model.go` (new method next to `renderContext`)
- Modify: `internal/ui/content_pane.go` (interface)
- Modify: `AGENTS.md` (the `RenderMessage` row: mention `BodyOnly`; new row for `DraftRenderContext`)
- Test: `internal/ui/messages/render_body_only_test.go`, `internal/ui/thread/draft_context_test.go`

**Interfaces:**
- Produces:
  - `RenderOptions.BodyOnly bool`, documented where declared: "only the message body (the draft preview): no labels, header, reply line, blocks, attachments or reactions; the header never joins the body, even in compact mode".
  - `func (m *messages.Model) DraftRenderContext() *messages.RenderContext` and `func (m *thread.Model) DraftRenderContext() *messages.RenderContext`: the pane's `renderContext()` with `Emoji.PlaceCtx` zeroed (Cells and Customs kept), `SearchTerms` nil, `Nav` zero, `ReplyAvatar` nil.
  - `DraftRenderContext() *messages.RenderContext` added to `contentPane`.

- [ ] **Step 1: Write the failing tests**

`TestRenderMessageBodyOnly` (package `messages`), with `rc := &RenderContext{Images: imgrender.NewRenderer(), UserNames: map[string]string{"U2": "bob"}}` and `msg := MessageItem{UserName: "alice", Timestamp: "3:04 PM", Text: "• ship *v0.28*\n<@U2>"}` at width 60:
- `ansi.Strip(RenderMessage(msg, 60, rc, RenderOptions{BodyOnly: true}).Content)` contains `ship v0.28` and `@bob`, and contains neither `alice` nor `3:04 PM`.
- With `rc.Compact = true` the same holds.
- The full render (every option off, `Compact` false), stripped, with its first line (the header) dropped, equals the stripped `BodyOnly` content: the body rows are the same rendering.

`TestDraftRenderContextHasNoImages` (package `thread`, and the same in `messages`): after `SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: f}, Cells: 1, Customs: map[string]string{"x": "y"}})` (`f` any non-nil `emoji.PlaceFetcher`; `newFakePlaceFetcher()` in package `messages`) and `SetUserNames(map[string]string{"U2": "bob"})`, `DraftRenderContext()` has a zero `PlaceCtx`, `Cells == 1`, the customs, and `UserNames["U2"] == "bob"`.

- [ ] **Step 2: Run them to see them fail**

Run: `CGO_ENABLED=0 go test ./internal/ui/messages ./internal/ui/thread -run 'BodyOnly|DraftRenderContext' -v` → FAIL to compile.

- [ ] **Step 3: Implement**

In `RenderMessage`: `headerOnSeparateLine := opt.BodyOnly || !rc.Compact || …`; right after `text := bodyStyle.Render(WordWrap(rendered, contentWidth))`, `if opt.BodyOnly { out.Content = text; return out }`. Nothing before that point may draw the header into `text` when `BodyOnly`. Add the two pane methods and the interface line; the compile-time assertions in `content_pane.go` check both panes.

- [ ] **Step 4: Run the packages**

Run: `CGO_ENABLED=0 go test ./internal/ui/... -count=1` → PASS (goldens unchanged: nothing draws a preview yet).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages internal/ui/thread internal/ui/content_pane.go AGENTS.md
git commit -m "feat(messages): body-only rendering and a draft render context per pane"
```

### Task 4: The key and when the preview closes

**Files:**
- Modify: `internal/ui/keys.go` (field `DraftPreview` after `OpenInEditor`, with a comment: insert mode, either box)
- Modify: `internal/ui/mode_insert.go` (handler; `ctrl+u` keeps the state; the header comment's key list gains `Ctrl+R`)
- Modify: `internal/ui/app.go` (`cancelEdit`)
- Modify: `wiki/Keybindings.md` (row after `Ctrl+O`: `` | `Ctrl+R` | Insert | Show / hide the draft preview: the draft as it will look once sent, above the box. Sending closes it | ``)
- Re-bless: `internal/ui/testdata/golden/dialog_help*.ansi` (the help list is sorted by description, so a new entry moves rows)
- Test: `internal/ui/draft_preview_keys_test.go`

**Interfaces:**
- Consumes: `TogglePreview`, `SetPreview`, `PreviewOn` (Task 2).
- Produces: `KeyMap.DraftPreview key.Binding`.

- [ ] **Step 1: Write the failing tests**

All built with `newTestApp(t, withSize(120, 40), withActiveChannel("C1"), …)` and keys sent through `a.Update` (not `runKeyCases`, which skips the upload gate); `ctrl+r` is `keyMod('r', tea.ModCtrl)`.
- `TestCtrlRTogglesChannelPreview`: insert mode on the channel box: `ctrl+r` → `a.compose.PreviewOn()`; again → off. The draft text is unchanged both times.
- `TestCtrlRTogglesOnlyTheThreadBox`: thread open and focused (as `focusThreadPanel`), insert mode: `ctrl+r` → `a.threadCompose.PreviewOn()` and not `a.compose.PreviewOn()`.
- `TestCtrlRInNormalModeDoesNothing`: normal mode, `ctrl+r` → both off, mode unchanged.
- `TestSendClosesPreview`: type `hello`, `ctrl+r`, Enter → off. Same for the thread box.
- `TestEscKeepsPreview`: `ctrl+r`, Esc, `i` → still on.
- `TestCtrlUKeepsPreview`: type `x`, `ctrl+r`, `ctrl+u` → on, value `""`.
- `TestEditSaveAndCancelClosePreview`: start an edit of an own message (`beginEditOfSelected`), `ctrl+r`; Esc (cancel) → off. Again with `MessageEditedMsg` for the edit's channel and TS → off.
- `TestSlashCommandClosesPreview`: `slashApp(t, nil)` plus `withSlashBuiltin(t, "noop", func(*App, commandArgs) tea.Cmd { return nil })`; type `/noop`, `ctrl+r`, `pressEnter(a)` → off.
- `TestHelpListsDraftPreview`: `help.FromKeyMap(DefaultKeyMap())` has an entry `{Key: "ctrl+r", Desc: "show / hide the draft preview"}`.

- [ ] **Step 2: Run them to see them fail**

Run: `CGO_ENABLED=0 go test ./internal/ui -run 'CtrlR|ClosesPreview|KeepsPreview|HelpListsDraftPreview' -v` → FAIL.

- [ ] **Step 3: Implement**

In `handleInsertMode`, after `target` is chosen and before the `ctrl+u` check: `if key.Matches(msg, a.keys.DraftPreview) { target.TogglePreview(); return nil }`. It works with a picker open. The `ctrl+u` branch becomes: remember `target.PreviewOn()`, `Reset()`, `SetPreview(remembered)`. In `cancelEdit`, call `SetPreview(false)` on the compose of `a.editing.Panel()` (both the Esc cancel and `MessageEditedMsg` go through it).

- [ ] **Step 4: Re-bless the help goldens and run the package**

Run: `CGO_ENABLED=0 go test ./internal/ui -run 'TestGolden' -update`, then `git diff --stat internal/ui/testdata/golden`: only `dialog_help*.ansi` may change, and the change is the new `ctrl+r` row. Then `CGO_ENABLED=0 go test ./internal/ui -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/keys.go internal/ui/mode_insert.go internal/ui/app.go internal/ui/draft_preview_keys_test.go internal/ui/testdata/golden wiki/Keybindings.md
git commit -m "feat: ctrl+r shows or hides the draft preview; sending closes it"
```

### Task 5: Draw the preview above both compose boxes

**Files:**
- Create: `internal/ui/draft_preview.go`
- Modify: `internal/ui/view_messages.go` (`renderChannelMessagesPanel`)
- Modify: `internal/ui/view_thread.go` (`renderThreadRegion`)
- Modify: `internal/ui/golden_test.go` (two scenarios)
- Create: `internal/ui/testdata/golden/draft_preview.ansi`, `draft_preview_thread.ansi` (via `-update`)
- Modify: `AGENTS.md` (UI chrome table row: "The draft preview's rows for a compose box | `(*App).draftPreview` (`internal/ui/draft_preview.go`); state and cache on `compose.Model` (`PreviewOn`, `PreviewBody`)")
- Test: `internal/ui/draft_preview_test.go`

**Interfaces:**
- Consumes: Task 2's `PreviewOn`, `PreviewBody`, `RenderKey`, `PreviewRender`; Task 3's `RenderOptions.BodyOnly`, `contentPane.DraftRenderContext`; `parseSlashCommand`; `a.editing`.
- Produces: `func (a *App) draftPreview(c *compose.Model, pane contentPane, panel Panel, width, maxRows int, key compose.RenderKey) string`, and `func draftPreviewRows(inner, room int) int` returning `min(max(3, inner/3), room)`, or 0 when that is under 2.

**What `draftPreview` returns** ("" when `!c.PreviewOn()` or `maxRows < 2`); every row is exactly `width` cells on `styles.Background`:
1. Title row: `── draft preview ` + `─` to `width`, `styles.TextMuted`.
2. Then one dim row (`styles.TextMuted`, two-cell left pad) when, in this order: `parseSlashCommand(c.Value())` matches and `!(a.editing.IsActive() && a.editing.Panel() == panel)` → `slash command, not a message`; `len(c.Attachments()) > 0` → `caption is sent as typed`; `strings.TrimSpace(c.Value()) == ""` → `nothing to preview`.
3. Else the body: `c.PreviewBody(width, key, render)`, where `render` calls `messages.RenderMessage(messages.MessageItem{Text: mrkdwn}, width, pane.DraftRenderContext(), messages.RenderOptions{BodyOnly: true})` and pads each line to `width` with a two-cell left pad (inside the cached render, so a hit costs nothing). When the body has more than `maxRows-1` lines, show a dim `…` row and the last `maxRows-2` lines.

**Layout.** Channel pane: build `composeView` as today (picker + box), compute its height plus the spacer's, then `room := (contentHeight-2) - 1 /*typing*/ - thatHeight - 3`, `draftPreview(&a.compose, a.messagepane, PanelMessages, msgWidth-2, draftPreviewRows(contentHeight-2, room), compose.RenderKey{Theme: themeVer, Pane: a.messagepane.Version(), Window: int64(a.focusedWin)})`, and stack preview, spacer, picker, box (top to bottom). Not drawn when the read-only line replaces the box. Thread pane: the same with `a.threadCompose`, `a.threadPanel`, `PanelThread`, `threadWidth-2`, `Window: 0`; the bot-status line, when present, counts in the height and stays on top: status, preview, spacer, picker, box. `composeHeight` / `threadComposeHeight` are measured after the preview is added, so the message area, its cache key and `SetMsgComposeTop` / `SetThreadComposeTop` include it: a click on the preview enters insert mode in that box, like a click on the box (say so in the PR).

- [ ] **Step 1: Write the failing tests**

Helper in the test file: `previewRows(t, a) []string`, the stripped frame rows from the one containing `draft preview` down to the row before the compose box (find the box by the draft's first line).
- `TestDraftPreviewShowsRenderedDraft`: draft `"Release plan:\n- ship **v0.28** on Tuesday\n- read [the notes](https://example.com)"`, preview on → rows contain `• ship v0.28 on Tuesday` and `• read the notes`, and no `**`, `[` or `](`.
- `TestDraftPreviewNotes` (table): `""` and `"  \n\n "` → `nothing to preview`; `"/remind me"` → `slash command, not a message`; an attachment with caption `"**x**"` → `caption is sent as typed`; edit in progress with text `"/not a command"` → no slash note, and the row shows `/not a command`.
- `TestDraftPreviewMentionsUseNames`: compose users `{U2 bob}`, draft `"hi @bob"`; the pane's names lack `U2` → rows contain `@U2` (the renderer's unresolved form) and not `Bob Smith`; then `a.messagepane.SetUserNames(map[string]string{"U2": "Bob Smith"})` and render again → rows contain `@Bob Smith`.
- `TestDraftPreviewCapShowsTheEnd`: 120x80, a 40-line draft `line 1` … `line 40`. The box itself takes 23 rows here (20 text rows, padding, spacer), so the cap binds, not the room: preview rows (title included) = `max(3, inner/3)`, with `inner` read from the frame as the rows strictly between the messages pane's top and bottom border rows; the first body row is `…`; the last is `line 40`.
- `TestDraftPreviewYieldsToMessages`: 120x40, the same draft. The room binds: either no `draft preview` row, or the `a.layout` message height is exactly 3 (the preview took only what was left). It is never below 3.
- `TestDraftPreviewShortTerminal`: 80x12, the same draft: no panic, message height ≥ 3, and either no `draft preview` row or at most `draftPreviewRows` rows.
- `TestDraftPreviewRowsFitWidth`: draft = a 200-char URL, a fenced code block with a 150-char line, `"日本語のテキスト 🎉🎉🎉"` → the frame has as many rows as the same app with the preview off, each row is as wide (`ansi.StringWidth`) as that row of the preview-off frame, and every preview row is `msgWidth-2` wide inside the border.
- `TestDraftPreviewSitsAbovePicker`: draft `"@"` (mention picker open), preview on → the `draft preview` row is above the first picker row, which is above the box.
- `TestDraftPreviewHiddenWhenComposeDisabled`: `composeDisabled` set, preview on → no `draft preview` row.
- `TestDraftPreviewFollowsTheme`: render with `"dark"`, `a.applyTheme("nord")`, render again → the preview rows' raw bytes differ; restore `"dark"` in `t.Cleanup`.
- `TestClickOnDraftPreviewEntersInsert`: as `TestMouseClick_OnComposeEntersInsert`, preview on in normal mode, click the title row → `ModeInsert`, `PanelMessages`.
- Goldens: `draft_preview` (base fixture, `goldenBaseW`×`goldenBaseH`, channel compose holding the draft above, preview on, insert mode) and `draft_preview_thread` (`goldenThreadApp(t, 120, 30)`, thread focused, same draft in `threadCompose`, `layout: goldenStackedThread`).
- `BenchmarkDraftPreview`: `pinRenderGlobals(b)`, `buildTestApp(withSize(160, 50))`, 20,000 users on `a.compose`, the Task 1 draft, preview on; each iteration appends one character to the draft, then calls `a.View()`. Record ns/op in the PR. **If it is over 16 ms/op on this box, stop and report to Grant before opening the PR**, with the numbers from Task 1's benchmark and this one.

- [ ] **Step 2: Run them to see them fail**

Run: `CGO_ENABLED=0 go test ./internal/ui -run 'DraftPreview' -v` → FAIL.

- [ ] **Step 3: Implement `draft_preview.go` and the two layout changes**

As specified above. `draftPreview` is the only place that decides the notes and the tail cut; the two view functions only size and stack it.

- [ ] **Step 4: Bless the new goldens and run everything**

Run: `CGO_ENABLED=0 go test ./internal/ui -run TestGolden -update`; `git status internal/ui/testdata/golden` shows only the two new files (if any other golden changed, the layout change leaked into a preview-off frame: fix that, do not bless it). Then the five local checks:

```
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go vet ./...
gofmt -l .
golangci-lint run
CGO_ENABLED=0 go test ./... -count=1
CGO_ENABLED=0 go test ./internal/ui -run x -bench DraftPreview -benchtime 50x
```

Expected: all pass, `gofmt -l` prints nothing, benchmark under 16 ms/op.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/draft_preview.go internal/ui/draft_preview_test.go internal/ui/view_messages.go internal/ui/view_thread.go internal/ui/golden_test.go internal/ui/testdata/golden AGENTS.md
git commit -m "feat: draw the draft preview above the channel and thread compose boxes"
```

## After the tasks

Open one PR against `main`, `SLK-908: feat: draft preview above the compose box (ctrl+r)`, branch `slk-908-draft-preview`. The body lists the builder's calls the spec left open: the cache key (pane `Version()`, so a scroll re-renders the body but not the translation), clicks on the preview enter insert mode, `ctrl+r` works with a picker open, `ctrl+u` keeps the preview open, the sketch's lower rule is the compose box's own edge (no second rule), and the benchmark numbers.
