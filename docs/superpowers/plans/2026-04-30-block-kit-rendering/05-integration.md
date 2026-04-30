# Phase 5: Integration

> See `00-overview.md` for goal, architecture, and conventions. Phases 1-4 must be complete.

This phase wires the `blockkit` package into the live message-rendering pipeline. After this phase the feature is observable in the running app: bot messages render with structure, fields, color stripes, and visible-but-disabled controls.

---

## Task 14: Wire `blockkit.Render` and `blockkit.RenderLegacy` into `renderMessagePlain`

**Files:**
- Modify: `internal/ui/messages/model.go` (around `renderMessagePlain` at line 988)
- Modify: `internal/ui/messages/model_test.go` (or create a new `blockkit_integration_test.go`)

The existing pipeline composes a message body in this order:

1. Username + timestamp (line 991)
2. Body text (`text` variable, line 1002)
3. Attachments via `renderAttachmentBlock` (lines 1106-1132)
4. Thread indicator
5. Reaction pills

We add two new passes between steps 2 and 3:

- 2a. `blockkit.Render(msg.Blocks, ...)` (block kit blocks)
- 2b. `blockkit.RenderLegacy(msg.LegacyAttachments, ...)` (legacy attachments)

Their outputs need to flow into the same `flushes`, `allSixel`, and `hits` aggregation that the file-attachment pass already feeds. Row indices for sixel and hit rects are caller-relative (within `RenderResult.Lines`); we convert them to message-absolute coordinates before merging.

- [ ] **Step 1: Add a constructor for `blockkit.Context` derived from the model**

In `internal/ui/messages/model.go`, add a helper near the top of the renderMessagePlain section:

```go
// blockkitContext bundles the blockkit-package dependencies sourced
// from the model's image context, theme, and per-message identity.
func (m *Model) blockkitContext(msg MessageItem, userNames map[string]string) blockkit.Context {
	return blockkit.Context{
		Protocol:    m.imgCtx.Protocol,
		Fetcher:     m.imgCtx.Fetcher,
		KittyRender: m.imgCtx.KittyRender,
		CellPixels:  m.imgCtx.CellPixels,
		MaxRows:     m.imgCtx.MaxRows,
		MaxCols:     m.imgCtx.MaxCols,
		UserNames:   userNames,
		SendMsg:     func(v any) {
			if m.imgCtx.SendMsg != nil {
				if msg, ok := v.(tea.Msg); ok {
					m.imgCtx.SendMsg(msg)
				}
			}
		},
		MessageTS:  msg.TS,
		Channel:    m.channelName,
		RenderText: RenderSlackMarkdown,
		WrapText:   WordWrap,
	}
}
```

The signature `func(s string, userNames map[string]string) string` for `RenderText` matches `RenderSlackMarkdown` exactly. The signature `func(s string, w int) string` for `WrapText` matches `WordWrap` exactly. Verify in `internal/ui/messages/render.go`.

- [ ] **Step 2: Add a failing integration test FIRST**

Create `internal/ui/messages/blockkit_integration_test.go`:

```go
package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// renderedFor builds a model with a single message, runs buildCache
// at the given width, and returns the joined plain-text rendering of
// the first cache entry. Mirrors the existing test-helper pattern in
// internal/ui/messages/plain_test.go and selection_test.go.
func renderedFor(t *testing.T, msg MessageItem, width int) string {
	t.Helper()
	m := New([]MessageItem{msg}, "general")
	m.buildCache(width)
	if len(m.cache) == 0 {
		t.Fatal("buildCache produced no entries")
	}
	// Find the entry whose msgIdx == 0 (skipping any date separators).
	var lines []string
	for _, e := range m.cache {
		if e.msgIdx == 0 {
			lines = e.linesNormal
			break
		}
	}
	if lines == nil {
		t.Fatal("no entry with msgIdx 0 in cache")
	}
	return ansi.Strip(strings.Join(lines, "\n"))
}

func TestRenderMessagePlainEmitsBlockKitContent(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		UserID:    "U-BOT",
		Text:      "PR opened",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.HeaderBlock{Text: "Pull Request opened"},
			blockkit.SectionBlock{Text: "Pay system: bug fix for retry logic"},
		},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "PR opened") {
		t.Errorf("missing message body Text: %q", plain)
	}
	if !strings.Contains(plain, "Pull Request opened") {
		t.Errorf("missing header block: %q", plain)
	}
	if !strings.Contains(plain, "Pay system: bug fix for retry logic") {
		t.Errorf("missing section block: %q", plain)
	}
}

func TestRenderMessagePlainEmitsLegacyAttachment(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "pagerduty",
		UserID:    "U-BOT",
		Text:      "alert",
		Timestamp: "1:23 PM",
		LegacyAttachments: []blockkit.LegacyAttachment{{
			Color: "danger",
			Title: "Service down",
			Text:  "checkout-svc 5xx > 1%",
		}},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "Service down") {
		t.Errorf("missing legacy title: %q", plain)
	}
	if !strings.Contains(plain, "█") {
		t.Errorf("missing color stripe glyph: %q", plain)
	}
}
```

`NewModel` and `SetWidth` already exist on `Model` — verify by `rg -n "^func NewModel|func.*SetWidth" internal/ui/messages/model.go`. If `SetWidth` doesn't exist, the test should set the width via the `width` argument it passes to `renderMessagePlain` directly (already 100 above).

- [ ] **Step 3: Run the test to verify it fails**

```bash
go test ./internal/ui/messages/ -run "TestRenderMessagePlainEmits" -v
```

Expected: FAIL — `renderMessagePlain` doesn't call `blockkit.Render` yet.

- [ ] **Step 4: Wire it in**

In `renderMessagePlain` (line 988-1146), find the `attachLineSlices` block (line 1103). Insert the blockkit passes BEFORE that block, AFTER `text` is computed and AFTER `preAttachmentRows` is calculated:

```go
	// Block Kit blocks render between the body text and file attachments.
	bkCtx := m.blockkitContext(msg, userNames)
	bkCtx.Channel = m.channelName
	bkCtx.MessageTS = msg.TS
	var bkLines []string
	var bkInteractive bool
	if len(msg.Blocks) > 0 {
		res := blockkit.Render(msg.Blocks, bkCtx, contentWidth)
		bkLines = append(bkLines, res.Lines...)
		allFlushes = append(allFlushes, res.Flushes...)
		for k, v := range res.SixelRows {
			absRow := preAttachmentRows + len(bkLines) - len(res.Lines) + k
			allSixel[absRow] = sixelEntry{bytes: v.Bytes, fallback: v.Fallback, height: v.Height}
		}
		for _, h := range res.Hits {
			rowOffset := preAttachmentRows + (len(bkLines) - len(res.Lines))
			hits = append(hits, entryHit{
				rowStartInEntry: rowOffset + h.RowStart,
				rowEndInEntry:   rowOffset + h.RowEnd,
				colStart:        contentColBase + h.ColStart,
				colEnd:          contentColBase + h.ColEnd,
				fileID:          "BK-" + h.URL, // distinguishable from file-image hits
			})
		}
		bkInteractive = bkInteractive || res.Interactive
	}
	if len(msg.LegacyAttachments) > 0 {
		baseRow := preAttachmentRows + len(bkLines)
		res := blockkit.RenderLegacy(msg.LegacyAttachments, bkCtx, contentWidth)
		bkLines = append(bkLines, res.Lines...)
		allFlushes = append(allFlushes, res.Flushes...)
		for k, v := range res.SixelRows {
			allSixel[baseRow+k] = sixelEntry{bytes: v.Bytes, fallback: v.Fallback, height: v.Height}
		}
		for _, h := range res.Hits {
			hits = append(hits, entryHit{
				rowStartInEntry: baseRow + h.RowStart,
				rowEndInEntry:   baseRow + h.RowEnd,
				colStart:        contentColBase + h.ColStart,
				colEnd:          contentColBase + h.ColEnd,
				fileID:          "BK-" + h.URL,
			})
		}
		bkInteractive = bkInteractive || res.Interactive
	}

	preAttachmentRows += len(bkLines)
```

Then in the `msgContent` composition line (currently `msgContent := broadcastLabel + line + editedMark + "\n" + text + attachmentLines + threadLine + reactionLine`), insert `bkBlock` between `text` and `attachmentLines`:

```go
	bkBlock := ""
	if len(bkLines) > 0 {
		bkBlock = "\n" + strings.Join(bkLines, "\n")
	}
	msgContent := broadcastLabel + line + editedMark + "\n" + text + bkBlock + attachmentLines + threadLine + reactionLine
```

The `bkInteractive` flag is consumed by Task 15.

- [ ] **Step 5: Run the test**

```bash
go test ./internal/ui/messages/ -run "TestRenderMessagePlainEmits" -v
```

Expected: PASS for both tests.

- [ ] **Step 6: Run the full test suite**

```bash
go test ./... -race
```

Expected: all PASS, no regressions.

- [ ] **Step 7: Build + manual smoke**

```bash
make build
./bin/slk
```

Open a channel known to have bot messages. Verify they now render with structure (headers, fields, etc.). Quit cleanly.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/
git commit -m "feat(messages): wire blockkit Render and RenderLegacy into renderMessagePlain"
```

---

## Task 15: Append the `↗ open in Slack to interact` hint when interactive

**Files:**
- Modify: `internal/ui/messages/model.go` (the `renderMessagePlain` integration block from Task 14)
- Modify: `internal/ui/messages/blockkit_integration_test.go`

The hint is a single muted line appended after all blocks/attachments rendered, but before file attachments / thread / reactions. Plain text, no OSC-8 (per spec).

- [ ] **Step 1: Failing test**

Append to `blockkit_integration_test.go`:

```go
func TestRenderMessagePlainAppendsHintWhenInteractive(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "deploy-bot",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "Deploy?"},
			blockkit.ActionsBlock{Elements: []blockkit.ActionElement{
				{Kind: "button", Label: "Approve"},
			}},
		},
	}
	plain := renderedFor(t, msg, 100)
	if !strings.Contains(plain, "↗ open in Slack to interact") {
		t.Errorf("expected hint line, got %q", plain)
	}
}

func TestRenderMessagePlainOmitsHintWhenNotInteractive(t *testing.T) {
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "PR merged"},
		},
	}
	plain := renderedFor(t, msg, 100)
	if strings.Contains(plain, "↗ open in Slack to interact") {
		t.Errorf("hint should not appear for non-interactive message: %q", plain)
	}
}
```

- [ ] **Step 2: Run, verify the first fails (`hint missing`).**

- [ ] **Step 3: Implement**

After computing `bkInteractive` and before assembling `msgContent` (Task 14's last edit), add:

```go
	if bkInteractive {
		hint := styles.Timestamp.Render("↗ open in Slack to interact")
		bkLines = append(bkLines, hint)
	}
```

(`styles.Timestamp` is the existing muted-italic style used for timestamps; any of `styles.Timestamp`, `mutedStyle()`, or a fresh muted style is fine — pick `styles.Timestamp` for visual consistency with the timestamp+edited indicator look already used on every message.)

- [ ] **Step 4: Run tests**

```bash
go test ./internal/ui/messages/ -v
```

Expected: PASS for both new tests, and all prior PASSes remain.

- [ ] **Step 5: Build + commit**

```bash
make build
git add internal/ui/messages/
git commit -m "feat(messages): append 'open in Slack to interact' hint for interactive messages"
```

---

## Task 16: End-to-end fixture-based tests

**Files:**
- Create: `internal/ui/messages/blockkit/testdata/github_pr.json`
- Create: `internal/ui/messages/blockkit/testdata/pagerduty_alert.json`
- Create: `internal/ui/messages/blockkit/testdata/deploy_approval.json`
- Create: `internal/ui/messages/blockkit/testdata/oncall_handoff.json`
- Create: `internal/ui/messages/blockkit/testdata/section_with_fields.json`
- Create: `internal/ui/messages/blockkit/testdata/header_divider_section.json`
- Create: `internal/ui/messages/blockkit/integration_test.go`

The fixtures are real-shaped Slack message JSON. We exercise `Parse → Render` end-to-end at three widths and assert key substrings.

- [ ] **Step 1: Author the six JSON fixtures**

Each fixture is the contents of a Slack message's `blocks` array (or `attachments` array). Place exactly the JSON Slack would emit. Hand-author these — each is small. Examples:

`testdata/github_pr.json`:

```json
{
  "blocks": [
    { "type": "header", "text": { "type": "plain_text", "text": "Pull Request opened" } },
    { "type": "section", "text": { "type": "mrkdwn", "text": "*<https://github.com/x/y/pull/42|#42 Fix retry logic>* by @gammons" } },
    { "type": "context", "elements": [
      { "type": "mrkdwn", "text": "x/y · 3 files changed" }
    ]}
  ]
}
```

`testdata/pagerduty_alert.json`:

```json
{
  "attachments": [{
    "color": "danger",
    "title": "Service down: checkout-svc",
    "title_link": "https://status.example.com/incidents/123",
    "text": "p99 latency > SLO for 3m",
    "fields": [
      {"title": "Service", "value": "checkout-svc", "short": true},
      {"title": "Severity", "value": "SEV-2", "short": true},
      {"title": "Region", "value": "us-east-1", "short": true},
      {"title": "Status", "value": "firing", "short": true}
    ],
    "footer": "Datadog",
    "ts": "1700000000"
  }]
}
```

`testdata/deploy_approval.json`:

```json
{
  "blocks": [
    { "type": "section", "text": { "type": "mrkdwn", "text": "*Deploy v2.3.1 to prod?*\nLast deploy: 2h ago" } },
    { "type": "actions", "elements": [
      { "type": "button", "text": { "type": "plain_text", "text": "Approve" }, "action_id": "a1" },
      { "type": "button", "text": { "type": "plain_text", "text": "Deny" }, "action_id": "a2" }
    ]}
  ]
}
```

`testdata/oncall_handoff.json`:

```json
{
  "blocks": [
    { "type": "header", "text": { "type": "plain_text", "text": "Weekly on-call handoff" } },
    { "type": "section", "fields": [
      { "type": "mrkdwn", "text": "*Outgoing*\nalice" },
      { "type": "mrkdwn", "text": "*Incoming*\nbob" }
    ]},
    { "type": "divider" },
    { "type": "context", "elements": [
      { "type": "mrkdwn", "text": "Rotation rotates Mondays at 9am UTC" }
    ]}
  ]
}
```

`testdata/section_with_fields.json`:

```json
{
  "blocks": [
    { "type": "section", "text": { "type": "mrkdwn", "text": "Build complete" }, "fields": [
      { "type": "mrkdwn", "text": "*Branch*\nmain" },
      { "type": "mrkdwn", "text": "*Commit*\n`abc1234`" },
      { "type": "mrkdwn", "text": "*Duration*\n4m 12s" }
    ]}
  ]
}
```

`testdata/header_divider_section.json`:

```json
{
  "blocks": [
    { "type": "header", "text": { "type": "plain_text", "text": "Top header" } },
    { "type": "divider" },
    { "type": "section", "text": { "type": "mrkdwn", "text": "Body text after divider." } }
  ]
}
```

- [ ] **Step 2: Write the integration test**

```go
// internal/ui/messages/blockkit/integration_test.go
package blockkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/slack-go/slack"
)

// fixturePayload mirrors the shape of the JSON files in testdata/.
// Not all fields are populated for every fixture; that's fine —
// json.Unmarshal leaves missing fields zero-valued.
type fixturePayload struct {
	Blocks      slack.Blocks       `json:"blocks"`
	Attachments []slack.Attachment `json:"attachments"`
}

func loadFixture(t *testing.T, name string) fixturePayload {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var p fixturePayload
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return p
}

func makeCtx() Context {
	return Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
}

func TestFixture_GitHubPR(t *testing.T) {
	p := loadFixture(t, "github_pr.json")
	blocks := Parse(p.Blocks)
	for _, w := range []int{60, 100, 140} {
		r := Render(blocks, makeCtx(), w)
		plain := ansi.Strip(strings.Join(r.Lines, "\n"))
		for _, want := range []string{"Pull Request opened", "Fix retry logic", "3 files changed"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width=%d missing %q in %q", w, want, plain)
			}
		}
	}
}

func TestFixture_PagerDutyAlert(t *testing.T) {
	p := loadFixture(t, "pagerduty_alert.json")
	atts := ParseAttachments(p.Attachments)
	for _, w := range []int{60, 100, 140} {
		r := RenderLegacy(atts, makeCtx(), w)
		plain := ansi.Strip(strings.Join(r.Lines, "\n"))
		for _, want := range []string{"Service down", "checkout-svc", "SEV-2", "Datadog"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width=%d missing %q in %q", w, want, plain)
			}
		}
		if !strings.Contains(plain, "█") {
			t.Errorf("width=%d missing color stripe", w)
		}
	}
}

func TestFixture_DeployApproval(t *testing.T) {
	p := loadFixture(t, "deploy_approval.json")
	blocks := Parse(p.Blocks)
	for _, w := range []int{60, 100, 140} {
		r := Render(blocks, makeCtx(), w)
		plain := ansi.Strip(strings.Join(r.Lines, "\n"))
		if !strings.Contains(plain, "Deploy v2.3.1") {
			t.Errorf("width=%d missing body: %q", w, plain)
		}
		if !strings.Contains(plain, "[ Approve ]") || !strings.Contains(plain, "[ Deny ]") {
			t.Errorf("width=%d missing buttons: %q", w, plain)
		}
		if !r.Interactive {
			t.Errorf("width=%d Interactive should be true", w)
		}
	}
}

func TestFixture_OncallHandoff(t *testing.T) {
	p := loadFixture(t, "oncall_handoff.json")
	blocks := Parse(p.Blocks)
	for _, w := range []int{60, 100, 140} {
		r := Render(blocks, makeCtx(), w)
		plain := ansi.Strip(strings.Join(r.Lines, "\n"))
		for _, want := range []string{"Weekly on-call handoff", "alice", "bob", "rotates Mondays"} {
			if !strings.Contains(plain, want) {
				t.Errorf("width=%d missing %q in %q", w, want, plain)
			}
		}
	}
}

func TestFixture_SectionWithFields(t *testing.T) {
	p := loadFixture(t, "section_with_fields.json")
	blocks := Parse(p.Blocks)
	r := Render(blocks, makeCtx(), 100)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	for _, want := range []string{"Build complete", "Branch", "Commit", "Duration", "abc1234"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in %q", want, plain)
		}
	}
}

func TestFixture_HeaderDividerSection(t *testing.T) {
	p := loadFixture(t, "header_divider_section.json")
	blocks := Parse(p.Blocks)
	r := Render(blocks, makeCtx(), 80)
	if r.Height < 3 {
		t.Errorf("Height = %d, want >= 3 (header, divider, body)", r.Height)
	}
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	for _, want := range []string{"Top header", "Body text after divider"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in %q", want, plain)
		}
	}
}
```

- [ ] **Step 3: Run the integration tests**

```bash
go test ./internal/ui/messages/blockkit/ -run "TestFixture" -v
```

Expected: all six PASS. If any fails, read the produced output carefully — fixture content vs. assertion may have a small typo.

- [ ] **Step 4: Run the full suite**

```bash
go test ./... -race
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/blockkit/testdata/ internal/ui/messages/blockkit/integration_test.go
git commit -m "test(blockkit): end-to-end fixture tests for six real bot payloads"
```

---

## Task 17: Final smoke + cleanup

- [ ] **Step 1: Run lint**

```bash
make lint
```

Expected: clean, or warnings limited to known pre-existing issues. Fix any new findings introduced by this PR (unused imports, shadowed vars, `errcheck` violations on the goroutine fetcher path).

- [ ] **Step 2: Run the full test suite under race detector**

```bash
go test ./... -race
```

Expected: all PASS.

- [ ] **Step 3: Build the release binary and confirm size**

```bash
make build
ls -lh bin/slk
```

Expected: binary at `bin/slk`, size near the README's stated ~19 MB (small growth is fine; the new package is ~600 lines of Go).

- [ ] **Step 4: Manual smoke against a real workspace**

```bash
./bin/slk
```

Open channels containing each of:
- A GitHub bot PR notification (verify header + section render)
- A PagerDuty / status-page alert (verify color stripe + fields)
- A deploy or approval bot (verify `[ Approve ]` button label and the `↗ open in Slack to interact` hint)
- A normal human conversation (verify NO regression — text+files render as before, no hint line, no extra spacing)

Resize terminal narrow (~50 cols) and confirm:
- Section accessories stack below body text
- Field grids collapse to single-column
- Long header text truncates with `…`

Quit with `q`.

- [ ] **Step 5: Update STATUS.md**

Edit `docs/STATUS.md`. Add a brief entry under the most recent date noting Block Kit and legacy attachment rendering are implemented (see existing entries for tone and length).

- [ ] **Step 6: Final commit**

```bash
git add docs/STATUS.md
git commit -m "docs: note block kit and legacy attachment rendering in STATUS"
```

---

## Phase 5 self-check (final verification before merging)

- [ ] All 4 tasks (14, 15, 16, 17) committed
- [ ] `go test ./... -race` is clean
- [ ] `make build` is clean
- [ ] `make lint` shows no NEW warnings vs. baseline
- [ ] Manual smoke confirms bot messages now render with structure
- [ ] No regressions on plain human-to-human messages
- [ ] STATUS.md mentions the new feature
- [ ] Spec at `docs/superpowers/specs/2026-04-30-block-kit-rendering-design.md` is still accurate vs. the implementation; if anything diverged during execution, update the spec in the same final commit
