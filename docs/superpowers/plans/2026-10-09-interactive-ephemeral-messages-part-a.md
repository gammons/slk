# Interactive Ephemeral Messages — Part A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show ephemeral ("only visible to you") messages and their legacy attachment buttons in slk, without letting them into the cache or the read-state machinery, and ship the `chat.attachmentAction` client call that Part B will use to press the buttons.

**Architecture:** `dispatchWebSocketEvent` recognises `is_ephemeral` and routes such frames to a new `EventHandler.OnEphemeralMessage`, which in `cmd/slk` builds the UI `MessageItem` (through a helper extracted from `OnMessage`, not copied) and sends it to the UI — and does nothing else. Legacy attachment `actions` are parsed into new `blocks.LegacyAction` values and drawn with the existing Block Kit actions renderer. `reduceNewMessage` displays an ephemeral but never stages a read mark or bumps a reply count.

**Tech Stack:** Go 1.26, bubbletea v2, lipgloss v2, slack-go, stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md` (Part A). Read its "Evidence" section first: the captured frames are the ground truth for every fixture below.

## Global Constraints

- Tests are plain `testing.T`, stdlib only, white-box (`package <pkg>`, not `<pkg>_test`). No testify.
- `internal/ui` does no I/O and must not import `internal/slack`; `internal/ui/boundary_test.go` enforces it.
- An ephemeral message never reaches SQLite, never moves `latest_synced_ts`, never sets `has_unread` or a mention badge, never triggers conversation discovery or a desktop notification, and never stages a channel or thread read mark.
- Change `messages.Model` and `thread.Model` together; the lockstep test and AGENTS.md require it.
- A new reusable helper gets a row in `AGENTS.md`'s shared-code tables **in the same commit**.
- Fixtures use the spec's placeholder IDs only: channel `C0EXAMPLE01`, user `U0EXAMPLE01`, team `T0EXAMPLE01`. Never real workspace IDs.
- The 8 App-level goldens must stay byte-identical. Never run `go test ./internal/ui -run TestGolden -update` for this work.
- Before the final commit: `go build ./...`, `go vet ./...`, `gofmt -l .` (empty), `go test ./... -race`, `golangci-lint run`.

## Review Focus

Inputs the spec implies but its happy path does not exercise, most likely first. Each has a test in the task named.

1. **An edit to an ephemeral** (`message_changed` carrying `is_ephemeral`, e.g. `/giphy`'s Shuffle replacing its preview) must not be cached as a message. Part A drops it. → Task 3.
2. **An ephemeral authored by the current user while one of their sends is in flight** must still be shown; the self-send in-flight guard exists to drop echoes of `chat.postMessage`, which an ephemeral never is. → Task 5.
3. **A frame whose action ids are numeric, missing, or don't line up with the parsed actions** must still render, without panicking; ids are best-effort. → Tasks 3 and 4.
4. **An ephemeral reply in the thread that is open** shows in the thread panel but bumps no reply count and stages no thread mark. → Task 5.
5. **Buttons in a narrow pane** wrap inside the attachment bar instead of overflowing it. → Task 2.

---

### Task 1: Model and parsing for legacy actions

**Files:**
- Modify: `internal/core/blocks/blocks.go` (the `LegacyAttachment` struct, ~line 150; add two types after `LegacyField`)
- Modify: `internal/core/types.go` (`MessageItem`, ~line 23)
- Modify: `internal/ui/messages/blockkit/types.go` (alias block, ~line 28)
- Modify: `internal/ui/messages/blockkit/parse.go` (`parseAttachment`, ~line 173)
- Test: `internal/ui/messages/blockkit/parse_test.go`

**Interfaces:**
- Produces:
  - `blocks.LegacyAction{ID, Name, Text, Type, Value, Style, URL string; Confirm *ActionConfirm}`
  - `blocks.ActionConfirm{Title, Text, OKText, DismissText string}`
  - `blocks.LegacyAttachment` gains `ID int`, `CallbackID string`, `Actions []LegacyAction`
  - `core.MessageItem` gains `Ephemeral bool`
  - aliases `blockkit.LegacyAction`, `blockkit.ActionConfirm`
  - test-only fixture const `capturedMentionAttachments` and helper `decodeCapturedAttachments(t) []slack.Attachment` (package `blockkit`), reused by Task 2

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/messages/blockkit/parse_test.go`, and add `"encoding/json"` to its imports:

```go
// capturedMentionAttachments is the `attachments` array of Slackbot's
// "You mentioned @x, but they're not in this channel" ephemeral, as
// captured from the Slack web client (IDs replaced with placeholders).
// See docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md.
const capturedMentionAttachments = `[{
  "callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0",
  "fallback":"You may want to invite them.","id":1,
  "actions":[
    {"id":"1","name":"invite","text":"Add Them","type":"button","value":"invite","style":"",
     "confirm":{"text":"New members will be able to see all of the channel's history, including any files that have been shared in the channel.",
                "title":"Are you sure you want to add them?","ok_text":"Add","dismiss_text":"Cancel"}},
    {"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""},
    {"id":"3","name":"dont-show-again","text":"Don't Show Again","type":"button","value":"dont-show-again","style":""}]}]`

func decodeCapturedAttachments(t *testing.T) []slack.Attachment {
	t.Helper()
	var atts []slack.Attachment
	if err := json.Unmarshal([]byte(capturedMentionAttachments), &atts); err != nil {
		t.Fatalf("decoding captured attachments: %v", err)
	}
	return atts
}

// TestParseAttachmentLegacyActions: the buttons of a legacy attachment,
// with everything chat.attachmentAction echoes back, survive parsing.
func TestParseAttachmentLegacyActions(t *testing.T) {
	a := ParseAttachments(decodeCapturedAttachments(t))[0]
	if a.ID != 1 {
		t.Errorf("ID = %d, want 1", a.ID)
	}
	if a.CallbackID != "consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0" {
		t.Errorf("CallbackID = %q", a.CallbackID)
	}
	if len(a.Actions) != 3 {
		t.Fatalf("len(Actions) = %d, want 3", len(a.Actions))
	}
	add := a.Actions[0]
	if add.Name != "invite" || add.Text != "Add Them" || add.Type != "button" || add.Value != "invite" {
		t.Errorf("Actions[0] = %+v", add)
	}
	if add.Confirm == nil {
		t.Fatal("Actions[0].Confirm = nil, want the captured confirm dialog")
	}
	if add.Confirm.Title != "Are you sure you want to add them?" || add.Confirm.OKText != "Add" || add.Confirm.DismissText != "Cancel" {
		t.Errorf("Actions[0].Confirm = %+v", *add.Confirm)
	}
	if a.Actions[1].Confirm != nil {
		t.Errorf("Actions[1].Confirm = %+v, want nil", *a.Actions[1].Confirm)
	}
	// slack-go's AttachmentAction has no id field: the parser cannot
	// see it. The WebSocket path fills it from the raw frame (Task 4).
	if a.Actions[1].ID != "" {
		t.Errorf("Actions[1].ID = %q, want empty from slack-go parsing", a.Actions[1].ID)
	}
}

func TestParseAttachmentWithoutActions(t *testing.T) {
	a := ParseAttachments([]slack.Attachment{{Title: "T"}})[0]
	if a.Actions != nil {
		t.Errorf("Actions = %+v, want nil", a.Actions)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/messages/blockkit/ -run 'TestParseAttachmentLegacyActions|TestParseAttachmentWithoutActions' -count=1`
Expected: FAIL to compile — `a.ID undefined`, `a.CallbackID undefined`, `a.Actions undefined`.

- [ ] **Step 3: Add the model types**

In `internal/core/blocks/blocks.go`, add three fields at the end of `LegacyAttachment` (after `Blocks []Block`):

```go
	// ID is the attachment's numeric id, echoed back as attachment_id
	// when one of its Actions is pressed.
	ID int
	// CallbackID identifies the interaction to the app that posted it.
	CallbackID string
	// Actions are the legacy interactive buttons (and menus) on the
	// attachment. nil when absent.
	Actions []LegacyAction
```

and after the `LegacyField` type, add:

```go
// LegacyAction is one button (or menu) in a legacy attachment's
// `actions` array. Pressing it echoes the action back to Slack as
// received, so every field Slack sent is kept.
type LegacyAction struct {
	// ID is the action's "id". slack-go does not decode it; the
	// WebSocket path fills it from the raw frame.
	ID    string
	Name  string
	Text  string // the button's label
	Type  string // "button" or "select"
	Value string
	Style string // "", "default", "primary" or "danger"
	// URL, when set, makes the button a link rather than a Slack call.
	URL     string
	Confirm *ActionConfirm
}

// ActionConfirm is a legacy action's confirmation dialog.
type ActionConfirm struct {
	Title       string
	Text        string
	OKText      string
	DismissText string
}
```

In `internal/core/types.go`, add to `MessageItem` after `LegacyAttachments`:

```go
	// Ephemeral is set for a message only the current user can see
	// (Slack's is_ephemeral). It is never cached and never a
	// read-marking target; Slack does not return it from history.
	Ephemeral bool
```

In `internal/ui/messages/blockkit/types.go`, extend the alias block:

```go
	LegacyAttachment = blocks.LegacyAttachment
	LegacyField      = blocks.LegacyField
	LegacyAction     = blocks.LegacyAction
	ActionConfirm    = blocks.ActionConfirm
```

- [ ] **Step 4: Parse them**

In `internal/ui/messages/blockkit/parse.go`, set the two scalar fields in the `la := LegacyAttachment{...}` literal of `parseAttachment`:

```go
		ID:         a.ID,
		CallbackID: a.CallbackID,
```

then, after the `for _, f := range a.Fields` loop, add:

```go
	for _, act := range a.Actions {
		la.Actions = append(la.Actions, parseLegacyAction(act))
	}
```

and add the helper below `parseAttachment`:

```go
// parseLegacyAction converts one slack-go attachment action. ID is left
// empty: slack-go's AttachmentAction does not declare it.
func parseLegacyAction(a slack.AttachmentAction) LegacyAction {
	out := LegacyAction{
		Name:  a.Name,
		Text:  a.Text,
		Type:  string(a.Type),
		Value: a.Value,
		Style: a.Style,
		URL:   a.URL,
	}
	if a.Confirm != nil {
		out.Confirm = &ActionConfirm{
			Title:       a.Confirm.Title,
			Text:        a.Confirm.Text,
			OKText:      a.Confirm.OkText,
			DismissText: a.Confirm.DismissText,
		}
	}
	return out
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/ui/messages/blockkit/ ./internal/core/... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/blocks/blocks.go internal/core/types.go internal/ui/messages/blockkit/types.go internal/ui/messages/blockkit/parse.go internal/ui/messages/blockkit/parse_test.go
git commit -m "feat(blockkit): parse legacy attachment actions and the ephemeral flag"
```

---

### Task 2: Draw legacy attachment buttons

**Files:**
- Modify: `internal/ui/messages/blockkit/attachments.go` (`appendLegacyAttachment`, immediately before the `// Footer.` block, ~line 195; add a helper at the end of the file)
- Test: `internal/ui/messages/blockkit/attachments_test.go`

**Interfaces:**
- Consumes: `LegacyAttachment.Actions`, `LegacyAction` (Task 1); `appendActions(out *RenderResult, a ActionsBlock, width int)` and `renderControlLabel` (existing, `render.go`); `decodeCapturedAttachments(t)` (Task 1).
- Produces: `legacyActionsBlock(actions []LegacyAction) ActionsBlock` (package-private).

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/messages/blockkit/attachments_test.go` (add `"charm.land/lipgloss/v2"` to its imports):

```go
func plainCtx() Context {
	return Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
}

// TestRenderLegacyActionsDrawButtonsInsideStripe: the captured Slackbot
// attachment has no title, text or fields, only actions -- the buttons
// must still render, inside the attachment's bar.
func TestRenderLegacyActionsDrawButtonsInsideStripe(t *testing.T) {
	r := RenderLegacy(ParseAttachments(decodeCapturedAttachments(t)), plainCtx(), 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	for _, want := range []string{"[ Add Them ]", "[ Dismiss ]", "[ Don't Show Again ]"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing button %q in %q", want, plain)
		}
	}
	if len(r.Lines) == 0 {
		t.Fatal("no lines rendered")
	}
	for i, line := range r.Lines {
		if !strings.HasPrefix(ansi.Strip(line), "█") {
			t.Errorf("line %d is outside the stripe: %q", i, ansi.Strip(line))
		}
	}
	if !r.Interactive {
		t.Error("Interactive = false, want true for an attachment with buttons")
	}
}

// fallback is notification text; Slack's web client does not draw it.
func TestRenderLegacyDoesNotRenderFallback(t *testing.T) {
	r := RenderLegacy(ParseAttachments(decodeCapturedAttachments(t)), plainCtx(), 80)
	if plain := ansi.Strip(strings.Join(r.Lines, "\n")); strings.Contains(plain, "You may want to invite them.") {
		t.Errorf("fallback text rendered: %q", plain)
	}
}

func TestRenderLegacyActionsWrapAtNarrowWidth(t *testing.T) {
	const width = 24
	r := RenderLegacy(ParseAttachments(decodeCapturedAttachments(t)), plainCtx(), width)
	if len(r.Lines) < 2 {
		t.Fatalf("got %d lines, want the buttons wrapped over several", len(r.Lines))
	}
	for i, line := range r.Lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line %d is %d cols wide, want <= %d: %q", i, w, width, ansi.Strip(line))
		}
	}
}

func TestRenderLegacyWithoutActionsIsNotInteractive(t *testing.T) {
	r := RenderLegacy([]LegacyAttachment{{Title: "T"}}, plainCtx(), 80)
	if r.Interactive {
		t.Error("Interactive = true for an attachment without actions")
	}
}

func TestRenderLegacySelectActionDrawsAsSelect(t *testing.T) {
	r := RenderLegacy([]LegacyAttachment{{
		Actions: []LegacyAction{{Name: "env", Text: "Pick env", Type: "select"}},
	}}, plainCtx(), 80)
	if plain := ansi.Strip(strings.Join(r.Lines, "\n")); !strings.Contains(plain, "Pick env ▾") {
		t.Errorf("select not drawn as a select: %q", plain)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages/blockkit/ -run 'TestRenderLegacy(Actions|DoesNotRenderFallback|WithoutActions|SelectAction)' -count=1`
Expected: `TestRenderLegacyActionsDrawButtonsInsideStripe`, `TestRenderLegacyActionsWrapAtNarrowWidth` and `TestRenderLegacySelectActionDrawsAsSelect` FAIL (no buttons are drawn, so the narrow render has 0 lines); the other two already pass and guard against regressions.

- [ ] **Step 3: Render the buttons**

In `appendLegacyAttachment`, immediately before the `// Footer.` comment, insert:

```go
	// Buttons from the legacy `actions` array, after any image and
	// before the footer. Drawn by the Block Kit actions renderer so both
	// kinds of button look the same.
	if len(a.Actions) > 0 {
		var acts RenderResult
		appendActions(&acts, legacyActionsBlock(a.Actions), contentW)
		body = append(body, acts.Lines...)
		if acts.Interactive {
			out.Interactive = true
		}
	}
```

At the end of the file add:

```go
// legacyActionsBlock adapts legacy attachment actions to the Block Kit
// actions block appendActions draws. A legacy "select" draws as a
// static select labelled with its text.
func legacyActionsBlock(actions []LegacyAction) ActionsBlock {
	b := ActionsBlock{Elements: make([]ActionElement, 0, len(actions))}
	for _, a := range actions {
		kind := "button"
		if a.Type == "select" {
			kind = "static_select"
		}
		b.Elements = append(b.Elements, ActionElement{Kind: kind, Label: a.Text})
	}
	return b
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/ui/messages/... -count=1`
Expected: PASS. (The `messages` package is included because its render tests consume `RenderLegacy`.)

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/blockkit/attachments.go internal/ui/messages/blockkit/attachments_test.go
git commit -m "feat(blockkit): draw legacy attachment buttons"
```

---

### Task 3: Route ephemeral frames in `internal/slack`

**Files:**
- Modify: `internal/slack/events.go` — `EventHandler` (~line 24), `wsMessageEvent` (~line 152), `wsSubMsg` (~line 171), the message arm of `dispatchWebSocketEvent` (~lines 423–443); add `EphemeralMessage`, `wsActionIDs`, `legacyActionIDs`, `dispatchEphemeral`.
- Modify: `cmd/slk/rtm_handler.go` — temporary `OnEphemeralMessage` that preserves today's behaviour (replaced in Task 4).
- Test: `internal/slack/events_test.go`

**Interfaces:**
- Produces:
  - `slackclient.EphemeralMessage{ChannelID, UserID, BotID, Username, TS, ThreadTS, Subtype, Text string; Blocks slack.Blocks; Attachments []slack.Attachment; ActionIDs [][]string}`
  - `EventHandler.OnEphemeralMessage(m EphemeralMessage)`
  - `ActionIDs[i][j]` is the `"id"` of `Attachments[i].Actions[j]`; nil when the frame's ids are absent or not strings.

- [ ] **Step 1: Write the failing tests**

In `internal/slack/events_test.go`, add a field to `mockEventHandler`:

```go
	ephemerals []EphemeralMessage
```

and the method next to `OnMessage`:

```go
func (m *mockEventHandler) OnEphemeralMessage(msg EphemeralMessage) {
	m.ephemerals = append(m.ephemerals, msg)
}
```

Then append (add `"strings"` to the imports; `"fmt"` is already imported):

```go
// capturedMentionEphemeral is Slackbot's "not in this channel" frame as
// captured from the Slack web client (IDs replaced with placeholders).
const capturedMentionEphemeral = `{"type":"message","subtype":"bot_message","channel":"C0EXAMPLE01",
 "text":"You mentioned <@U0EXAMPLE01>, but they’re not in this private channel.",
 "blocks":[{"type":"rich_text","block_id":"amnwN","elements":[{"type":"rich_text_section","elements":[
   {"type":"text","text":"You mentioned "},{"type":"user","user_id":"U0EXAMPLE01"},
   {"type":"text","text":", but they’re not in this private channel."}]}]}],
 "username":"slackbot","user":"USLACKBOT","bot_id":"B01","ts":"1791541429.559220",
 "attachments":[{"callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0",
   "fallback":"You may want to invite them.","id":1,"actions":[
   {"id":"1","name":"invite","text":"Add Them","type":"button","value":"invite","style":"",
    "confirm":{"text":"New members will be able to see all of the channel's history, including any files that have been shared in the channel.",
               "title":"Are you sure you want to add them?","ok_text":"Add","dismiss_text":"Cancel"}},
   {"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""},
   {"id":"3","name":"dont-show-again","text":"Don't Show Again","type":"button","value":"dont-show-again","style":""}]}],
 "is_ephemeral":true,"event_ts":"1791541429.004800"}`

func TestDispatchEphemeralMessage_RoutesToOnEphemeralMessage(t *testing.T) {
	h := &mockEventHandler{}
	dispatchWebSocketEvent([]byte(capturedMentionEphemeral), h)

	if len(h.messages) != 0 {
		t.Fatalf("OnMessage called %d times, want 0: an ephemeral must not take the cached path", len(h.messages))
	}
	if len(h.ephemerals) != 1 {
		t.Fatalf("OnEphemeralMessage called %d times, want 1", len(h.ephemerals))
	}
	got := h.ephemerals[0]
	if got.ChannelID != "C0EXAMPLE01" || got.UserID != "USLACKBOT" || got.BotID != "B01" ||
		got.Username != "slackbot" || got.TS != "1791541429.559220" || got.Subtype != "bot_message" {
		t.Errorf("identity fields = %+v", got)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != 1 || len(got.Attachments[0].Actions) != 3 {
		t.Fatalf("attachments not decoded: %+v", got.Attachments)
	}
	want := [][]string{{"1", "2", "3"}}
	if fmt.Sprint(got.ActionIDs) != fmt.Sprint(want) {
		t.Errorf("ActionIDs = %v, want %v", got.ActionIDs, want)
	}
}

func TestDispatchMessageWithoutEphemeralFlag_StillRoutesToOnMessage(t *testing.T) {
	h := &mockEventHandler{}
	frame := strings.Replace(capturedMentionEphemeral, `"is_ephemeral":true,`, "", 1)
	dispatchWebSocketEvent([]byte(frame), h)
	if len(h.ephemerals) != 0 || len(h.messages) != 1 {
		t.Errorf("ephemerals=%d messages=%d, want 0 and 1", len(h.ephemerals), len(h.messages))
	}
}

// A non-string action id must not lose the message: ids are best-effort.
func TestDispatchEphemeralMessage_NumericActionIDs(t *testing.T) {
	h := &mockEventHandler{}
	frame := `{"type":"message","channel":"C0EXAMPLE01","user":"USLACKBOT","ts":"1.000000","is_ephemeral":true,
	  "attachments":[{"id":1,"actions":[{"id":7,"name":"x","text":"X","type":"button"}]}]}`
	dispatchWebSocketEvent([]byte(frame), h)
	if len(h.ephemerals) != 1 {
		t.Fatalf("OnEphemeralMessage called %d times, want 1", len(h.ephemerals))
	}
	if got := h.ephemerals[0]; got.ActionIDs != nil || len(got.Attachments) != 1 {
		t.Errorf("ActionIDs = %v (want nil), attachments = %d (want 1)", got.ActionIDs, len(got.Attachments))
	}
}

// An edit of an ephemeral (Slack replacing an app's preview) must not
// reach OnMessage, which would cache it. Part A drops it.
func TestDispatchEphemeralMessageChanged_IsDropped(t *testing.T) {
	for name, frame := range map[string]string{
		"flag on event": `{"type":"message","subtype":"message_changed","channel":"C1","is_ephemeral":true,
		  "message":{"user":"U1","text":"shuffled","ts":"1.000000"}}`,
		"flag on inner message": `{"type":"message","subtype":"message_changed","channel":"C1",
		  "message":{"user":"U1","text":"shuffled","ts":"1.000000","is_ephemeral":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := &mockEventHandler{}
			dispatchWebSocketEvent([]byte(frame), h)
			if len(h.messages) != 0 || len(h.ephemerals) != 0 {
				t.Errorf("messages=%d ephemerals=%d, want both 0", len(h.messages), len(h.ephemerals))
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run 'TestDispatchEphemeral|TestDispatchMessageWithoutEphemeralFlag' -count=1`
Expected: FAIL to compile — `undefined: EphemeralMessage`.

- [ ] **Step 3: Declare the type and the interface method**

In `internal/slack/events.go`, add to `EventHandler` directly after `OnMessage`:

```go
	// OnEphemeralMessage delivers a message only the current user can
	// see (is_ephemeral): Slackbot's "not in this channel" notice, an
	// app's slash-command reply. Slack never returns these from
	// history, so receivers must not persist them.
	OnEphemeralMessage(m EphemeralMessage)
```

After the `EventHandler` interface, add:

```go
// EphemeralMessage is a message only the current user can see. A
// struct rather than OnMessage's positional parameters, which are easy
// to transpose.
type EphemeralMessage struct {
	ChannelID string
	UserID    string
	BotID     string
	Username  string
	TS        string
	ThreadTS  string
	Subtype   string
	Text      string

	Blocks      slack.Blocks
	Attachments []slack.Attachment
	// ActionIDs[i][j] is the "id" of Attachments[i].Actions[j]. slack-go's
	// AttachmentAction does not declare the field, and
	// chat.attachmentAction echoes it back. nil when the frame's ids are
	// absent or not strings.
	ActionIDs [][]string
}
```

Add `IsEphemeral bool \`json:"is_ephemeral"\`` as the last field of both `wsMessageEvent` and `wsSubMsg`.

Below `wsSubMsg`, add:

```go
// wsActionIDs is a second, narrow decode of a message frame: the "id"
// of every legacy attachment action, which slack-go drops.
type wsActionIDs struct {
	Attachments []struct {
		Actions []struct {
			ID string `json:"id"`
		} `json:"actions"`
	} `json:"attachments"`
}

// legacyActionIDs returns the action ids of a message frame, indexed
// like its attachments and their actions. nil when there are none or
// they are not strings -- ids are best-effort and never block delivery.
func legacyActionIDs(data []byte) [][]string {
	var raw wsActionIDs
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Attachments) == 0 {
		return nil
	}
	out := make([][]string, len(raw.Attachments))
	for i, a := range raw.Attachments {
		for _, act := range a.Actions {
			out[i] = append(out[i], act.ID)
		}
	}
	return out
}

// dispatchEphemeral routes a message only the current user can see.
// data is the raw frame, decoded again for the action ids.
func dispatchEphemeral(data []byte, msg wsMessageEvent, handler EventHandler) {
	debuglog.WS("ephemeral: channel=%s user=%s ts=%s subtype=%q thread_ts=%s attachments=%d",
		msg.Channel, msg.User, msg.TS, msg.SubType, msg.ThreadTS, len(msg.Attachments))
	handler.OnEphemeralMessage(EphemeralMessage{
		ChannelID:   msg.Channel,
		UserID:      msg.User,
		BotID:       msg.BotID,
		Username:    msg.Username,
		TS:          msg.TS,
		ThreadTS:    msg.ThreadTS,
		Subtype:     msg.SubType,
		Text:        msg.Text,
		Blocks:      msg.Blocks,
		Attachments: msg.Attachments,
		ActionIDs:   legacyActionIDs(data),
	})
}
```

- [ ] **Step 4: Route in `dispatchWebSocketEvent`**

Replace the two arms `case "", "bot_message", "thread_broadcast", "file_share":` and `case "message_changed":` so they read as below. The `case "message_deleted":` arm and everything after it are unchanged. `break` inside an `if` within a `case` leaves the `switch`.

```go
		case "", "bot_message", "thread_broadcast", "file_share":
			if msg.IsEphemeral {
				dispatchEphemeral(data, msg, handler)
				break
			}
			// thread_broadcast is a thread reply that the author also
			// posted to the main channel; render it like a regular
			// message but with the subtype preserved so the UI can
			// label it. file_share is a regular message that has one
			// or more files attached (Slack's V2 upload flow uses
			// this subtype).
			debuglog.WS("message: channel=%s user=%s ts=%s subtype=%q thread_ts=%s files=%d",
				msg.Channel, msg.User, msg.TS, msg.SubType, msg.ThreadTS, len(msg.Files))
			handler.OnMessage(msg.Channel, msg.User, msg.TS, msg.Text, msg.ThreadTS, msg.SubType, false, msg.Files, msg.Blocks, msg.Attachments, msg.BotID, msg.Username)
		case "message_changed":
			if msg.Message == nil {
				break
			}
			if msg.IsEphemeral || msg.Message.IsEphemeral {
				// An app replacing its ephemeral (e.g. /giphy's Shuffle).
				// OnMessage would cache it; Part A has nowhere to apply
				// it, so it is dropped.
				debuglog.WS("message_changed: channel=%s ts=%s ephemeral=true decision=dropped",
					msg.Channel, msg.Message.TS)
				break
			}
			debuglog.WS("message_changed: channel=%s user=%s ts=%s thread_ts=%s edited=true",
				msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.ThreadTS)
			handler.OnMessage(msg.Channel, msg.Message.User, msg.Message.TS, msg.Message.Text, msg.Message.ThreadTS, "", true, msg.Message.Files, msg.Message.Blocks, msg.Message.Attachments, msg.Message.BotID, msg.Message.Username)
```

- [ ] **Step 5: Keep `cmd/slk` compiling, behaviour unchanged**

`rtmEventHandler` must satisfy the interface. Until Task 4, forward to `OnMessage`, so an ephemeral is handled exactly as before this task. In `cmd/slk/rtm_handler.go`, add the import `slackclient "github.com/gammons/slk/internal/slack"` and, after `OnMessage`:

```go
// OnEphemeralMessage takes OnMessage's path until it gets its own
// display-only handling.
func (h *rtmEventHandler) OnEphemeralMessage(m slackclient.EphemeralMessage) {
	h.OnMessage(m.ChannelID, m.UserID, m.TS, m.Text, m.ThreadTS, m.Subtype, false, nil, m.Blocks, m.Attachments, m.BotID, m.Username)
}
```

- [ ] **Step 6: Run the tests**

Run: `go build ./... && go test ./internal/slack/ ./cmd/slk/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/slack/events.go internal/slack/events_test.go cmd/slk/rtm_handler.go
git commit -m "feat(ws): route ephemeral messages to OnEphemeralMessage"
```

---

### Task 4: Display-only ephemeral handling in `cmd/slk`

**Files:**
- Modify: `cmd/slk/rtm_handler.go` — extract `messageAuthor` and `messageItemFromEvent` from `OnMessage`; replace Task 3's `OnEphemeralMessage`.
- Modify: `cmd/slk/attachments.go` — add `applyActionIDs`.
- Modify: `AGENTS.md` — two rows in "Text and rendering".
- Test: `cmd/slk/event_handler_test.go`

**Interfaces:**
- Consumes: `slackclient.EphemeralMessage` (Task 3); `MessageItem.Ephemeral`, `LegacyAction.ID` (Task 1); test helpers `newTestDB(t)` (`reconnect_sync_test.go`) and `sendFunc` (`event_handler_test.go`).
- Produces:
  - `(*rtmEventHandler).messageAuthor(userID, botID, username string) string`
  - `(*rtmEventHandler).messageItemFromEvent(authorID, userID, username, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment) messages.MessageItem`
  - `applyActionIDs(atts []blockkit.LegacyAttachment, ids [][]string)`
  - `ui.NewMessageMsg` with `Message.Ephemeral == true`, consumed by Task 5.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/slk/event_handler_test.go`, adding `slackclient "github.com/gammons/slk/internal/slack"` and `"github.com/gammons/slk/internal/ui/messages/blockkit"` to its imports:

```go
// capturedMentionAttachmentsJSON: the attachments of Slackbot's "not in
// this channel" ephemeral, captured from the web client (placeholder IDs).
const capturedMentionAttachmentsJSON = `[{"callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541429559219_0",
 "fallback":"You may want to invite them.","id":1,"actions":[
 {"id":"1","name":"invite","text":"Add Them","type":"button","value":"invite","style":""},
 {"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""},
 {"id":"3","name":"dont-show-again","text":"Don't Show Again","type":"button","value":"dont-show-again","style":""}]}]`

func ephemeralFixture(t *testing.T) slackclient.EphemeralMessage {
	t.Helper()
	var atts []slack.Attachment
	if err := json.Unmarshal([]byte(capturedMentionAttachmentsJSON), &atts); err != nil {
		t.Fatal(err)
	}
	return slackclient.EphemeralMessage{
		ChannelID: "C1", UserID: "USLACKBOT", BotID: "B01", Username: "slackbot",
		TS: "1791541429.559220", Subtype: "bot_message",
		Text:        "You mentioned <@U0EXAMPLE01>, but they’re not in this private channel.",
		Attachments: atts,
		ActionIDs:   [][]string{{"1", "2", "3"}},
	}
}

// Slack never returns an ephemeral from history: a cached copy would be
// a ghost row, and a moved watermark or unread flag would point at a
// message Slack does not know. It goes to the UI and nowhere else.
func TestOnEphemeralMessage_DisplaysButTouchesNoDurableState(t *testing.T) {
	db := newTestDB(t)
	_ = db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel"})
	var sent []ui.NewMessageMsg
	h := &rtmEventHandler{
		program: sendFunc(func(msg tea.Msg) {
			if m, ok := msg.(ui.NewMessageMsg); ok {
				sent = append(sent, m)
			}
		}),
		db:              db,
		wsCtx:           &WorkspaceContext{},
		workspaceID:     "T1",
		currentUserID:   "USELF",
		isActive:        func() bool { return true },
		activeChannelID: func() string { return "C1" },
		channelNames:    map[string]string{},
		channelTypes:    map[string]string{},
	}

	h.OnEphemeralMessage(ephemeralFixture(t))

	if _, err := db.GetMessage("C1", "1791541429.559220"); err == nil {
		t.Error("the ephemeral was cached")
	}
	if ts := db.GetChannelLatestSyncedTS("C1"); ts != "" {
		t.Errorf("latest_synced_ts = %q, want it untouched", ts)
	}
	if s, _ := db.GetChannelReadState("C1"); s.HasUnread {
		t.Error("has_unread set by an ephemeral")
	}
	if len(sent) != 1 {
		t.Fatalf("sent %d NewMessageMsg, want 1", len(sent))
	}
	item := sent[0].Message
	if sent[0].ChannelID != "C1" || !item.Ephemeral || item.TS != "1791541429.559220" {
		t.Errorf("sent %+v, want an ephemeral item for C1", sent[0])
	}
	if len(item.LegacyAttachments) != 1 || len(item.LegacyAttachments[0].Actions) != 3 {
		t.Fatalf("legacy attachments = %+v", item.LegacyAttachments)
	}
	if got := item.LegacyAttachments[0].Actions[1]; got.ID != "2" || got.Name != "ignore" {
		t.Errorf("Actions[1] = %+v, want id 2 from the raw frame", got)
	}
}

func TestOnEphemeralMessage_InactiveWorkspaceDrops(t *testing.T) {
	sent := 0
	h := &rtmEventHandler{
		program:  sendFunc(func(tea.Msg) { sent++ }),
		wsCtx:    &WorkspaceContext{},
		isActive: func() bool { return false },
	}
	h.OnEphemeralMessage(ephemeralFixture(t))
	if sent != 0 {
		t.Errorf("sent %d messages for an inactive workspace, want 0", sent)
	}
}

// Ids are positional and best-effort: a frame with fewer or more ids
// than parsed actions must not panic or shift ids onto the wrong button.
func TestApplyActionIDs_Misaligned(t *testing.T) {
	atts := []blockkit.LegacyAttachment{
		{Actions: []blockkit.LegacyAction{{Name: "a"}, {Name: "b"}}},
		{Actions: []blockkit.LegacyAction{{Name: "c"}}},
	}
	applyActionIDs(atts, [][]string{{"1"}})
	if atts[0].Actions[0].ID != "1" || atts[0].Actions[1].ID != "" || atts[1].Actions[0].ID != "" {
		t.Errorf("short ids: %+v", atts)
	}
	applyActionIDs(atts, [][]string{{"1", "2", "3"}, {"4", "5"}, {"6"}})
	if atts[0].Actions[1].ID != "2" || atts[1].Actions[0].ID != "4" {
		t.Errorf("long ids: %+v", atts)
	}
	applyActionIDs(nil, [][]string{{"1"}}) // must not panic
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/slk/ -run 'TestOnEphemeralMessage|TestApplyActionIDs' -count=1`
Expected: FAIL to compile — `undefined: applyActionIDs`.

- [ ] **Step 3: Add `applyActionIDs`**

In `cmd/slk/attachments.go`, after `extractLegacyAttachments`:

```go
// applyActionIDs copies legacy action ids -- captured from the raw
// WebSocket frame, since slack-go's AttachmentAction does not declare
// the field -- onto parsed attachments by position: ids[i][j] belongs to
// atts[i].Actions[j]. Missing ids leave ID empty; extra ids are ignored.
func applyActionIDs(atts []blockkit.LegacyAttachment, ids [][]string) {
	for i := range atts {
		if i >= len(ids) {
			return
		}
		for j := range atts[i].Actions {
			if j >= len(ids[i]) {
				break
			}
			atts[i].Actions[j].ID = ids[i][j]
		}
	}
}
```

Run: `go test ./cmd/slk/ -run 'TestOnEphemeralMessage|TestApplyActionIDs' -count=1`
Expected: `TestApplyActionIDs_Misaligned` passes; `TestOnEphemeralMessage_DisplaysButTouchesNoDurableState` FAILS with "the ephemeral was cached" (Task 3's forwarding stub is still in place). Don't rely on `TestOnEphemeralMessage_InactiveWorkspaceDrops`'s result yet: through the stub it exercises `OnMessage` with no DB, which is not what it is testing. Both must pass after Step 5.

- [ ] **Step 4: Extract the shared helpers from `OnMessage`**

In `cmd/slk/rtm_handler.go`, replace the opening block of `OnMessage`:

```go
	authorID := userID
	if authorID == "" && botID != "" {
		authorID = botID
		if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
			h.wsCtx.UserResolver.RequestBot(botID, username)
		}
	}
```

with (keeping the comment lines above it):

```go
	authorID := h.messageAuthor(userID, botID, username)
```

Replace the tail of `OnMessage` — from `userName, ok := resolveUserCached(authorID, h.userNames, h.db)` to the function's closing brace — with:

```go
	item := h.messageItemFromEvent(authorID, userID, username, ts, text, threadTS, subtype, edited, files, blocks, attachments)
	debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=dispatched_to_app",
		h.workspaceID, channelID, ts, subtype, threadTS)
	if h.program != nil {
		h.program.Send(ui.NewMessageMsg{ChannelID: channelID, Message: item})
	}
}
```

This keeps `OnMessage`'s order of side effects: the name is resolved (and requested when unknown) before the debug line and the send, as before.

Add the two helpers after `OnMessage`:

```go
// messageAuthor returns the ID a WebSocket message is attributed to. A
// bot message carries no user, only a bot_id and username: it is keyed
// on the bot_id and its avatar/name resolved via bots.info, mirroring
// the fetch-path messageAuthor helper.
func (h *rtmEventHandler) messageAuthor(userID, botID, username string) string {
	if userID != "" || botID == "" {
		return userID
	}
	if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
		h.wsCtx.UserResolver.RequestBot(botID, username)
	}
	return botID
}

// messageItemFromEvent builds the UI's MessageItem for a message that
// arrived over the WebSocket: resolves the author's display name
// (requesting it when unknown; a bot's username stands in until
// bots.info answers) and converts files, blocks and legacy attachments.
// Shared by OnMessage and OnEphemeralMessage.
func (h *rtmEventHandler) messageItemFromEvent(authorID, userID, username, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment) messages.MessageItem {
	userName, ok := resolveUserCached(authorID, h.userNames, h.db)
	if !ok {
		userName = authorID
		if userID != "" {
			if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
				h.wsCtx.UserResolver.Request(userID)
			}
		} else if username != "" {
			userName = username
		}
	}
	return messages.MessageItem{
		TS:                ts,
		UserID:            authorID,
		UserName:          userName,
		Text:              text,
		Timestamp:         formatTimestamp(ts, h.tsFormat),
		ThreadTS:          threadTS,
		Subtype:           subtype,
		IsEdited:          edited,
		Attachments:       extractAttachments(files),
		Blocks:            extractBlocks(blocks),
		LegacyAttachments: extractLegacyAttachments(attachments),
	}
}
```

Run: `go test ./cmd/slk/ -count=1`
Expected: every pre-existing test passes (the extraction moved code, nothing else); only `TestOnEphemeralMessage_DisplaysButTouchesNoDurableState` still fails.

- [ ] **Step 5: Replace the forwarding stub**

Replace Task 3's `OnEphemeralMessage` with:

```go
// OnEphemeralMessage handles a message only the current user can see:
// Slackbot's "not in this channel" notice, an app's slash-command
// reply. Slack never returns these from history, so nothing durable is
// touched -- no cache row, no sync watermark, no unread or mention
// write, no conversation discovery, no notification. It goes to the UI
// only, and only for the active workspace: there is nowhere to keep one
// for later.
func (h *rtmEventHandler) OnEphemeralMessage(m slackclient.EphemeralMessage) {
	if h.isActive != nil && !h.isActive() {
		debuglog.Cache("OnEphemeralMessage: team=%s channel=%s ts=%s decision=dropped_inactive_workspace",
			h.workspaceID, m.ChannelID, m.TS)
		return
	}
	authorID := h.messageAuthor(m.UserID, m.BotID, m.Username)
	item := h.messageItemFromEvent(authorID, m.UserID, m.Username, m.TS, m.Text, m.ThreadTS, m.Subtype, false, nil, m.Blocks, m.Attachments)
	applyActionIDs(item.LegacyAttachments, m.ActionIDs)
	item.Ephemeral = true
	debuglog.Cache("OnEphemeralMessage: team=%s channel=%s ts=%s thread_ts=%s decision=dispatched_to_app",
		h.workspaceID, m.ChannelID, m.TS, m.ThreadTS)
	if h.program != nil {
		h.program.Send(ui.NewMessageMsg{ChannelID: m.ChannelID, Message: item})
	}
}
```

- [ ] **Step 6: Record the helpers in `AGENTS.md`**

In `AGENTS.md`, "Text and rendering" table, after the `slackfmt.MentionedUserIDs(text)` row, add:

```markdown
| A WebSocket message's author ID and its UI `MessageItem` (bot-ID fallback, display-name resolution, file/block/attachment conversion) | `(*rtmEventHandler).messageAuthor`, `(*rtmEventHandler).messageItemFromEvent` (`cmd/slk/rtm_handler.go`); shared by `OnMessage` and `OnEphemeralMessage` |
| Legacy attachment action ids slack-go drops | `slackclient.EphemeralMessage.ActionIDs` (decoded from the raw frame), applied with `cmd/slk/applyActionIDs` |
```

- [ ] **Step 7: Run the package tests**

Run: `go test ./cmd/slk/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/slk/rtm_handler.go cmd/slk/attachments.go cmd/slk/event_handler_test.go AGENTS.md
git commit -m "feat(ws): show ephemeral messages without caching them"
```

---

### Task 5: Ephemerals never touch read state in `reduceNewMessage`

**Files:**
- Modify: `internal/ui/reducer_send.go` (`reduceNewMessage`, ~lines 261–316)
- Test: create `internal/ui/reducer_ephemeral_test.go`

**Interfaces:**
- Consumes: `NewMessageMsg.Message.Ephemeral` (Tasks 1, 4); test helpers `markCapture(t) (*App, *[]string)` and `feed(t, app, cmd, depth)` (`reducer_focus_test.go`), `openThreadPanel(a, channelID, threadTS)` and `hasReplyTS(a, ts)` (`reducer_thread_live_test.go`); `a.selfSend.MarkInFlight(channelID)` (`selfsend.go`).
- Produces: nothing new for later tasks.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/reducer_ephemeral_test.go`:

```go
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
)

func paneHasTS(a *App, ts string) bool {
	for _, m := range a.messagepane.Messages() {
		if m.TS == ts {
			return true
		}
	}
	return false
}

// An ephemeral on screen and focused is shown, but its ts must never be
// staged as a read cursor: Slack does not know it as a message, so the
// mark would land on a ts Slack never returns.
func TestEphemeralArrival_ShownButNoChannelMark(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "USLACKBOT", Text: "not in channel", Ephemeral: true},
	})
	feed(t, app, cmd, 0)

	if !paneHasTS(app, "5.000000") {
		t.Error("the ephemeral was not appended to the pane")
	}
	if len(*calls) != 0 || app.pendingChannelMark.ts != "" {
		t.Errorf("calls = %v, pending = %q; want no channel mark", *calls, app.pendingChannelMark.ts)
	}
}

// An ephemeral reply in the open thread: shown in the panel, no thread
// mark, and not counted as a reply on the parent.
func TestEphemeralThreadReply_ShownButNotCountedOrMarked(t *testing.T) {
	app, calls := markCapture(t)
	app.activeChannelID = "C1"
	app.messagepane.SetMessages([]messages.MessageItem{{TS: "1.000000", UserID: "U1", Text: "parent"}})
	openThreadPanel(app, "C1", "1.000000")

	_, cmd := app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message: messages.MessageItem{
			TS: "2.000000", ThreadTS: "1.000000", UserID: "USLACKBOT", Text: "not in channel", Ephemeral: true,
		},
	})
	feed(t, app, cmd, 0)

	if !hasReplyTS(app, "2.000000") {
		t.Error("the ephemeral reply did not reach the open thread panel")
	}
	if len(*calls) != 0 || app.pendingThreadMark.ts != "" {
		t.Errorf("calls = %v, pending = %q; want no thread mark", *calls, app.pendingThreadMark.ts)
	}
	if got := app.messagepane.Messages()[0].ReplyCount; got != 0 {
		t.Errorf("parent ReplyCount = %d, want 0: an ephemeral is not a reply", got)
	}
}

// The in-flight guard drops WS echoes of the user's own chat.postMessage.
// An ephemeral is never such an echo, even when it carries the user's ID.
func TestEphemeralFromSelf_ShownWhileSendInFlight(t *testing.T) {
	app, _ := markCapture(t)
	app.activeChannelID = "C1"
	app.currentUserID = "USELF"
	app.selfSend.MarkInFlight("C1")

	_, _ = app.Update(NewMessageMsg{
		ChannelID: "C1",
		Message:   messages.MessageItem{TS: "5.000000", UserID: "USELF", Text: "preview", Ephemeral: true},
	})

	if !paneHasTS(app, "5.000000") {
		t.Error("an ephemeral from the current user was dropped by the self-send in-flight guard")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestEphemeral' -count=1`
Expected: FAIL — a channel mark at `5.000000` is issued; a thread mark is issued and `ReplyCount` is 1; the in-flight case is not appended.

- [ ] **Step 3: Implement**

In `reduceNewMessage` (`internal/ui/reducer_send.go`):

1. Exempt ephemerals from the in-flight guard. Change its condition to:

```go
	if !m.Message.Ephemeral && m.Message.UserID != "" && m.Message.UserID == a.currentUserID && a.selfSend.InFlight(m.ChannelID) {
```

and append to the comment above it: `An ephemeral is never an echo of chat.postMessage, so it is exempt.`

2. In the fan-out loop, don't count an ephemeral reply:

```go
		if m.Message.ThreadTS != "" && m.Message.ThreadTS != m.Message.TS && !m.Message.Ephemeral {
			mm.IncrementReplyCount(m.Message.ThreadTS, m.Message.TS)
		}
```

3. Immediately after `if inOpenThreadPanel { a.threadPanel.AddReply(m.Message) }`, return before any read-state work:

```go
	if m.Message.Ephemeral {
		// Only the current user can see it, and Slack does not know it
		// as a message in the conversation: no read cursor may land on
		// its ts (Slack never returns it, so the cursor would sit
		// behind a ghost), it changes no read state, and it does not
		// make the threads list dirty. Display only.
		debuglog.Cache("NewMessageMsg: channel=%s ts=%s decision=ephemeral_display_only",
			m.ChannelID, m.Message.TS)
		return nil
	}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/ui/ -count=1`
Expected: PASS, including the goldens and every existing arrival test in `reducer_focus_test.go`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/reducer_send.go internal/ui/reducer_ephemeral_test.go
git commit -m "fix(ui): ephemeral messages never stage read marks or count as replies"
```

---

### Task 6: "Only visible to you" marker in both panes

**Files:**
- Create: `internal/ui/messages/ephemeral.go`
- Modify: `internal/ui/messages/model.go` (`renderMessagePlain`: the label block ~lines 2240–2254 and the `msgContent` line ~2449)
- Modify: `internal/ui/thread/model.go` (`renderThreadMessage`, ~lines 2040–2300)
- Modify: `AGENTS.md` — one row in "Text and rendering"
- Test: `internal/ui/messages/render_test.go`, `internal/ui/messages/blockkit_integration_test.go`, `internal/ui/thread/render_test.go`

**Interfaces:**
- Consumes: `MessageItem.Ephemeral` (Task 1).
- Produces: `messages.EphemeralLabel() string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/messages/render_test.go`:

```go
// TestEphemeralLabel: an ephemeral renders a muted "Only visible to
// you" row above its author line; a regular message does not.
func TestEphemeralLabel(t *testing.T) {
	const labelText = "Only visible to you"
	msg := MessageItem{TS: "1.0", UserName: "slackbot", Text: "You mentioned someone", Timestamp: "3:04 PM", Ephemeral: true}

	out := ansi.Strip(New([]MessageItem{msg}, "general").View(20, 60))
	labelIdx, nameIdx := strings.Index(out, labelText), strings.Index(out, "slackbot")
	if labelIdx < 0 || nameIdx < 0 || labelIdx >= nameIdx {
		t.Errorf("want %q before the author; label@%d name@%d in:\n%s", labelText, labelIdx, nameIdx, out)
	}

	msg.Ephemeral = false
	if out := ansi.Strip(New([]MessageItem{msg}, "general").View(20, 60)); strings.Contains(out, labelText) {
		t.Errorf("regular message rendered the ephemeral label:\n%s", out)
	}
}
```

Append to `internal/ui/messages/blockkit_integration_test.go`:

```go
// TestBuildCache_EphemeralLabelReactionHitRow: the label row must enter
// the row arithmetic, or every hit below it lands one row high.
func TestBuildCache_EphemeralLabelReactionHitRow(t *testing.T) {
	msg := MessageItem{
		TS: "1700000000.000000", UserName: "slackbot", UserID: "USLACKBOT",
		Text: "not in channel", Timestamp: "9:02 AM", Ephemeral: true,
		Reactions: []ReactionItem{{Emoji: "tada", Count: 1}},
	}
	m := New([]MessageItem{msg}, "general")
	m.buildCache(100)
	for _, e := range m.cache {
		if e.msgIdx != 0 {
			continue
		}
		if len(e.reactionHits) == 0 {
			t.Fatal("no reaction hits recorded")
		}
		row := e.reactionHits[0].rowStartInEntry
		if row < 0 || row >= len(e.linesNormal) {
			t.Fatalf("reaction hit row %d outside the entry's %d lines", row, len(e.linesNormal))
		}
		if got := ansi.Strip(e.linesNormal[row]); !strings.Contains(got, "1") || strings.Contains(got, "not in channel") {
			t.Errorf("reaction hit row %d is %q, want the reaction line", row, got)
		}
		return
	}
	t.Fatal("no entry with msgIdx 0 in cache")
}
```

Append to `internal/ui/thread/render_test.go`:

```go
// TestRenderThreadMessageEphemeralLabel: same marker as the messages
// pane, above the author, and the reaction hit row accounts for it.
func TestRenderThreadMessageEphemeralLabel(t *testing.T) {
	const width = 60
	m := New()
	msg := messages.MessageItem{
		TS: "1700000005.000000", UserName: "slackbot", Timestamp: "10:34 AM",
		Text: "not in channel", Ephemeral: true,
		Reactions: []messages.ReactionItem{{Emoji: "tada", Count: 1}},
	}
	got, _, hits, _, _ := m.renderThreadMessage(msg, width, nil, nil, false)
	lines := strings.Split(ansi.Strip(got), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "Only visible to you") || !strings.Contains(lines[1], "slackbot") {
		t.Fatalf("want the label on row 0 and the author on row 1; got %q", lines)
	}
	if len(hits) == 0 {
		t.Fatal("no reaction hits recorded")
	}
	if row := hits[0].rowStartInEntry; row != len(lines)-1 {
		t.Errorf("reaction hit row = %d, want %d (the reaction line); got %q", row, len(lines)-1, lines)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages/ ./internal/ui/thread/ -run 'EphemeralLabel' -count=1`
Expected: FAIL — no label rendered (the reaction-hit tests pass trivially until the label exists, then pin the row math).

- [ ] **Step 3: Add the shared label**

Create `internal/ui/messages/ephemeral.go`:

```go
package messages

import "github.com/gammons/slk/internal/ui/styles"

// EphemeralLabel is the marker row drawn above the author line of a
// message only the current user can see. Shared by the messages and
// thread panes so the two cannot drift.
func EphemeralLabel() string {
	return styles.Timestamp.Render("Only visible to you")
}
```

- [ ] **Step 4: Messages pane**

In `renderMessagePlain` (`internal/ui/messages/model.go`), replace:

```go
	var broadcastLabel string
	preAttachmentRows := 0
	if msg.Subtype == "thread_broadcast" {
		broadcastLabel = styles.Timestamp.Render("\u21b3 replied to a thread") + "\n"
		preAttachmentRows++ // the broadcast label occupies its own row
	}
```

with:

```go
	var labelRows string
	preAttachmentRows := 0
	if msg.Ephemeral {
		labelRows += EphemeralLabel() + "\n"
		preAttachmentRows++ // the ephemeral label occupies its own row
	}
	if msg.Subtype == "thread_broadcast" {
		labelRows += styles.Timestamp.Render("\u21b3 replied to a thread") + "\n"
		preAttachmentRows++ // the broadcast label occupies its own row
	}
```

In the row-layout comment directly above, change the line `//   row 0: broadcastLabel (only when subtype=thread_broadcast)` to `//   rows 0..: label rows (ephemeral, then thread_broadcast), each optional` and `//   row 0|1: username line + editedMark` to `//   next row: username line + editedMark`. Then change the `msgContent` line to:

```go
	msgContent := labelRows + line + editedMark + bodyRow + bkBlock + attachmentLines + threadLine + reactionLine
```

- [ ] **Step 5: Thread pane**

In `renderThreadMessage` (`internal/ui/thread/model.go`), directly after the `line := ...` statement add:

```go
	// Label rows above the author line; mirrors the messages pane.
	labelRows, labelRowCount := "", 0
	if msg.Ephemeral {
		labelRows, labelRowCount = messages.EphemeralLabel()+"\n", 1
	}
```

In the reaction-hit block, change the row-layout comment's first entry to `//   rows [0 .. labelRowCount): label rows (ephemeral)` followed by `//   next row: username + timestamp line`, and the base to:

```go
		reactionRowBase := labelRowCount + 1 + bodyRows + bkLineCount + attachmentLineCount
```

Change the return's first value to:

```go
	return labelRows + line + bodyRow + bkBlock + attachmentLines + reactionLine, flushes, reactionHits, line, min(contentWidth, width-1)
```

(The fourth return value stays `line`: it is the author header that `messages.SelectedHeader` swaps, not the label.)

- [ ] **Step 6: Record the helper in `AGENTS.md`**

In "Text and rendering", after the `messages.ChannelGlyph(chType)` row, add:

```markdown
| Marker row for a message only the current user can see | `messages.EphemeralLabel()` — drawn above the author line in both panes |
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/ui/... -count=1`
Expected: PASS, including `thread/lockstep_test.go` and the App goldens (no fixture is ephemeral).

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/ephemeral.go internal/ui/messages/model.go internal/ui/messages/render_test.go internal/ui/messages/blockkit_integration_test.go internal/ui/thread/model.go internal/ui/thread/render_test.go AGENTS.md
git commit -m "feat(ui): mark ephemeral messages 'Only visible to you'"
```

---

### Task 7: `Client.AttachmentAction`

**Files:**
- Create: `internal/slack/attachment_action.go`
- Create: `internal/slack/attachment_action_test.go`
- Modify: `AGENTS.md` — one row in "Text and rendering"

**Interfaces:**
- Consumes: `blocks.LegacyAction`, `blocks.ActionConfirm` (Task 1); `(*Client).postForm`, `parseOKResponse` (existing, `client.go`); test helper `pointClientAtTestServer(t, c, srv)` (`client_test.go`), which points the client at `http://slack.com/api/` routed to the test server.
- Produces (for Part B):
  - `slackclient.AttachmentActionRequest{ChannelID, MessageTS string; AttachmentID int; CallbackID string; Ephemeral bool; Action blocks.LegacyAction}`
  - `(*Client).AttachmentAction(ctx context.Context, req AttachmentActionRequest) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/slack/attachment_action_test.go`:

```go
package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core/blocks"
)

// capturedDismissPayload is the `payload` the Slack web client sent for
// "Dismiss" on Slackbot's mention ephemeral (placeholder IDs). Slack
// accepted it; slk must send the same bytes.
const capturedDismissPayload = `{"actions":[{"id":"2","name":"ignore","text":"Dismiss","type":"button","value":"ignore","style":""}],"attachment_id":"1","callback_id":"consistentephemeralmentions_U0EXAMPLE01_1791541587716039_0","channel_id":"C0EXAMPLE01","is_ephemeral":true,"message_ts":"1791541587.716040","prompt_app_install":false}`

func dismissRequest() AttachmentActionRequest {
	return AttachmentActionRequest{
		ChannelID:    "C0EXAMPLE01",
		MessageTS:    "1791541587.716040",
		AttachmentID: 1,
		CallbackID:   "consistentephemeralmentions_U0EXAMPLE01_1791541587716039_0",
		Ephemeral:    true,
		Action:       blocks.LegacyAction{ID: "2", Name: "ignore", Text: "Dismiss", Type: "button", Value: "ignore"},
	}
}

func TestMarshalAttachmentAction_MatchesWebClientCapture(t *testing.T) {
	got, err := marshalAttachmentAction(dismissRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got != capturedDismissPayload {
		t.Errorf("payload =\n  %s\nwant\n  %s", got, capturedDismissPayload)
	}
}

// A pressed action is echoed as received, confirm dialog included. The
// capture covers only Dismiss, so this shape is unverified; the order
// follows the inbound frame's confirm object.
func TestMarshalAttachmentAction_EchoesConfirm(t *testing.T) {
	req := dismissRequest()
	req.Action = blocks.LegacyAction{
		ID: "1", Name: "invite", Text: "Add Them", Type: "button", Value: "invite",
		Confirm: &blocks.ActionConfirm{Title: "Are you sure?", Text: "History & files <visible>", OKText: "Add", DismissText: "Cancel"},
	}
	got, err := marshalAttachmentAction(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `"confirm":{"text":"History & files <visible>","title":"Are you sure?","ok_text":"Add","dismiss_text":"Cancel"}`
	if !strings.Contains(got, want) {
		t.Errorf("payload %s\nmissing %s (HTML must not be escaped)", got, want)
	}
}

func TestAttachmentAction_PostsPayloadForm(t *testing.T) {
	var gotPath, gotPayload, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotPayload = r.PostForm.Get("payload")
		gotToken = r.PostForm.Get("token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	if err := c.AttachmentAction(context.Background(), dismissRequest()); err != nil {
		t.Fatalf("AttachmentAction: %v", err)
	}
	if gotPath != "/api/chat.attachmentAction" {
		t.Errorf("path = %q, want /api/chat.attachmentAction", gotPath)
	}
	if gotPayload != capturedDismissPayload {
		t.Errorf("payload form field =\n  %s\nwant\n  %s", gotPayload, capturedDismissPayload)
	}
	if gotToken != "xoxc-test" {
		t.Errorf("token = %q, want the xoxc token in the form body", gotToken)
	}
}

func TestAttachmentAction_OKFalseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_callback"}`))
	}))
	defer srv.Close()
	c := NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)

	err := c.AttachmentAction(context.Background(), dismissRequest())
	if err == nil || !strings.Contains(err.Error(), "invalid_callback") {
		t.Errorf("err = %v, want one carrying invalid_callback", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/slack/ -run 'AttachmentAction' -count=1`
Expected: FAIL to compile — `undefined: AttachmentActionRequest`.

- [ ] **Step 3: Implement**

Create `internal/slack/attachment_action.go`:

```go
package slackclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gammons/slk/internal/core/blocks"
)

// AttachmentActionRequest identifies one press of a legacy attachment
// button. Action is the button as received; it is echoed back verbatim.
type AttachmentActionRequest struct {
	ChannelID    string
	MessageTS    string
	AttachmentID int
	CallbackID   string
	Ephemeral    bool
	Action       blocks.LegacyAction
}

// AttachmentAction presses a legacy attachment button, as Slack's web
// client does (chat.attachmentAction, undocumented). Slack answers the
// press over the WebSocket -- for an ephemeral, a message_deleted -- so
// only the error is returned.
func (c *Client) AttachmentAction(ctx context.Context, req AttachmentActionRequest) error {
	payload, err := marshalAttachmentAction(req)
	if err != nil {
		return err
	}
	raw, err := c.postForm(ctx, "chat.attachmentAction", url.Values{"payload": {payload}})
	if err != nil {
		return err
	}
	return parseOKResponse("chat.attachmentAction", raw)
}

// attachmentActionPayload is the `payload` form field, key for key in
// the order the web client sends it. See the capture in
// docs/superpowers/specs/2026-10-09-interactive-ephemeral-messages-design.md.
// slk mimics the web client's request shape deliberately: an
// unfamiliar shape is the kind of signature Enterprise Grid anomaly
// detection flags (see postForm).
type attachmentActionPayload struct {
	Actions          []attachmentActionWire `json:"actions"`
	AttachmentID     string                 `json:"attachment_id"`
	CallbackID       string                 `json:"callback_id"`
	ChannelID        string                 `json:"channel_id"`
	IsEphemeral      bool                   `json:"is_ephemeral"`
	MessageTS        string                 `json:"message_ts"`
	PromptAppInstall bool                   `json:"prompt_app_install"`
}

// attachmentActionWire is one action as Slack sent it. style is kept
// even when empty, as captured.
type attachmentActionWire struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Text    string                 `json:"text"`
	Type    string                 `json:"type"`
	Value   string                 `json:"value"`
	Style   string                 `json:"style"`
	URL     string                 `json:"url,omitempty"`
	Confirm *attachmentConfirmWire `json:"confirm,omitempty"`
}

type attachmentConfirmWire struct {
	Text        string `json:"text"`
	Title       string `json:"title"`
	OKText      string `json:"ok_text"`
	DismissText string `json:"dismiss_text"`
}

// marshalAttachmentAction encodes req as the payload field. HTML is not
// escaped: the web client sends confirm text verbatim.
func marshalAttachmentAction(req AttachmentActionRequest) (string, error) {
	a := req.Action
	wire := attachmentActionWire{
		ID: a.ID, Name: a.Name, Text: a.Text, Type: a.Type,
		Value: a.Value, Style: a.Style, URL: a.URL,
	}
	if a.Confirm != nil {
		wire.Confirm = &attachmentConfirmWire{
			Text: a.Confirm.Text, Title: a.Confirm.Title,
			OKText: a.Confirm.OKText, DismissText: a.Confirm.DismissText,
		}
	}
	p := attachmentActionPayload{
		Actions:      []attachmentActionWire{wire},
		AttachmentID: strconv.Itoa(req.AttachmentID),
		CallbackID:   req.CallbackID,
		ChannelID:    req.ChannelID,
		IsEphemeral:  req.Ephemeral,
		MessageTS:    req.MessageTS,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		return "", fmt.Errorf("encoding chat.attachmentAction payload: %w", err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
```

- [ ] **Step 4: Record it in `AGENTS.md`**

In "Text and rendering", after the `(*slackclient.Client).WalkHistory`, `GetRepliesBetween` row, add:

```markdown
| Press a legacy attachment button (`chat.attachmentAction`, payload byte-identical to the web client's) | `(*slackclient.Client).AttachmentAction(ctx, AttachmentActionRequest)` |
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/slack/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/attachment_action.go internal/slack/attachment_action_test.go AGENTS.md
git commit -m "feat(slack): chat.attachmentAction client call for legacy buttons"
```

---

### Task 8: Full verification

**Files:** none, unless a check fails.

- [ ] **Step 1: Format, build, vet**

Run: `gofmt -l . && go build ./... && go vet ./...`
Expected: no output from `gofmt -l`; build and vet succeed.

- [ ] **Step 2: Lint**

Run: `golangci-lint run`
Expected: `0 issues.`

- [ ] **Step 3: Full race suite**

Run: `go test ./... -race -count=1`
Expected: PASS (about 47s). The App goldens must pass **without** `-update`.

- [ ] **Step 4: Manual check against a live workspace (human)**

Build and run slk against a real workspace with `SLK_DEBUG=1`. In a private channel, send a message that @-mentions someone who is not a member. Expect, within a second:
- a Slackbot message headed `Only visible to you`, with `[ Add Them ]  [ Dismiss ]  [ Don't Show Again ]` inside an attachment bar, followed by the `↗ open in Slack to interact` hint;
- `slk-debug.log` containing `ephemeral: channel=…`, `OnEphemeralMessage: … decision=dispatched_to_app` and `NewMessageMsg: … decision=ephemeral_display_only`;
- no unread dot on the channel.

Then switch to another channel and back: the Slackbot message is gone (accepted: ephemerals live in memory only). Trigger it again and dismiss it from the Slack web client: slk's copy disappears via `message_deleted`.

- [ ] **Step 5: Commit any fixes**

If any step required a change, commit it with a message naming the check that failed (e.g. `style: gofmt`).
