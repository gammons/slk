# Selected-message long timestamp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The selected message's header (messages pane and thread panel, including the thread parent) shows a date-qualified timestamp (`Tue Sep 29, 3:42 PM`); every other row keeps the short one.

**Architecture:** Two pure helpers in `internal/ui/messages` (`LongTimestamp`, `SelectedHeader`) do the formatting and the fit-checked substitution. Each pane bakes the long form into its cached *selected* variant at cache-build time (before tinting/filling/bordering), so `j`/`k` stays an O(1) variant pick. The width fallback keeps `len(linesSelected) == len(linesNormal)`.

**Tech Stack:** Go 1.26, bubbletea / lipgloss v2, `github.com/charmbracelet/x/ansi` (tests: `ansi.Strip`), stdlib `testing`.

**Spec:** `docs/superpowers/specs/2026-09-30-selected-message-timestamp-design.md`

## Global Constraints

- Tests: stdlib `testing` only, white-box (`package messages` / `package thread` / `package ui`), no `t.Parallel`.
- Clock: tests pin `messages.SetNowFunc(...)` and restore with `t.Cleanup(func() { messages.SetNowFunc(nil) })`. Build clocks and TS values in `time.Local` (see `goldenClock` / `lockstepClock` for why).
- Format, same year: `"Mon Jan 2, " + short`; other year: `"Mon Jan 2 2006, " + short`; unparseable `ts` or empty `short`: `short` unchanged.
- The time part is `msg.Timestamp` verbatim; only a date prefix is added. No relative words, seconds, zone, or edit time.
- Width is measured with `lipgloss.Width` on the assembled header (username + status suffix + gap + timestamp + `(edited)` mark).
- `linesNormal`, `linesPlain` and `m.parentEntry.linesPlain` are built from the **unmodified** render; the long form is display-only.
- `internal/ui` does no I/O; nothing here adds any.
- AGENTS.md "Text and rendering" table gains `messages.LongTimestamp` / `messages.SelectedHeader` in the same commit as the helpers.
- Before merge: `go build ./...`, `go vet ./...`, `go test ./... -race`, `gofmt -l .` empty, `golangci-lint run`.

## Deviation from the spec (deliberate, noted for review)

The spec says the long header must fit in `contentWidth`. Both renderers floor
`contentWidth` at 20, so at very narrow panes `contentWidth` can exceed the
columns the header row actually has (`width - 1` minus the 5-col avatar
gutter in the messages pane). Using `contentWidth` alone would let the long
header wrap there and break the height invariant the spec exists to protect.
The fit budget is therefore `min(contentWidth, width - 1 - gutter)`, returned
by each renderer as `headerBudget`. Pinned by Review Focus item 1.

## Review Focus

1. **Very narrow panes where the 20-col `contentWidth` floor exceeds the real row width** (messages pane with an avatar at width ≈24; thread at width ≈20): the selected row must fall back to the short form and keep its height. Tests: `TestSelectedTimestamp_AvatarNarrowFallsBack` (Task 2), `TestThreadSelectedTimestamp_TinyWidthFallsBack` (Task 3).
2. **Empty `msg.Timestamp`** (short = ""): `strings.Replace` with an empty needle inserts at position 0 — must return `rendered` untouched. Test: `TestSelectedHeader_EmptyShortUnchanged` (Task 1).
3. **`(edited)` pushing an otherwise-fitting long header over budget** (messages pane only; the thread pane renders no edited mark): must fall back. Test: `TestSelectedTimestamp_EditedMarkCountsTowardWidth` (Task 2).
4. **Focus flip re-renders only the selected slot via `partialRebuild`**: the unfocused selection must still show the long form. Test: `TestSelectedTimestamp_UnfocusedStillLong` (Task 2).
5. **Selected thread parent too wide for the pane**: the parent is not width-filled, so an overflowing header would be wrapped by the outer pane style and push the pane past its height. Must fall back; every pane row stays exactly `width` wide. Test: `TestThreadSelectedTimestamp_NarrowParentFallsBack` (Task 3).

~~Known, accepted by the spec: during a mouse drag over the selected row's header, the highlight is painted over the long text but the clipboard receives the short form.~~ Wrong: the overlay splices `linesPlain` into the displayed row by column, so the header was garbled. Caught in Task 4 by `TestGolden_DragSelectionIsActuallySelected`; resolved by showing the short form while a text selection exists. See the spec's "Amendment: text selection" and the ledger.

---

### Task 1: `LongTimestamp` and `SelectedHeader` helpers

**Files:**
- Create: `internal/ui/messages/longtimestamp.go`
- Create: `internal/ui/messages/longtimestamp_test.go`
- Modify: `internal/ui/messages/model.go:3591-3605` (`DateFromTS` reuses the new `timeFromTS`)
- Modify: `AGENTS.md` ("Text and rendering" table)

**Interfaces:**
- Consumes: package-level `nowFunc` / `SetNowFunc` (`internal/ui/messages/model.go:3624-3633`), `styles.Timestamp`.
- Produces:
  - `func LongTimestamp(ts, short string) string`
  - `func SelectedHeader(rendered, header, ts, short string, maxWidth int) string`
  - unexported `func timeFromTS(ts string) (time.Time, bool)` (local time of a Slack ts)

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/messages/longtimestamp_test.go`:

```go
package messages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// longTSClock is Tuesday 2026-09-29 16:00 in the LOCAL zone. Local (not
// UTC) so the message's local calendar day and the clock's year agree in
// every zone; mid-afternoon so fixture times stay clear of midnight.
func longTSClock() time.Time {
	return time.Date(2026, 9, 29, 16, 0, 0, 0, time.Local)
}

// longTSAt formats t as a Slack ts.
func longTSAt(t time.Time) string {
	return fmt.Sprintf("%d.000100", t.Unix())
}

func pinLongTSClock(t *testing.T) {
	t.Helper()
	styles.Apply("dark", config.Theme{})
	SetNowFunc(longTSClock)
	t.Cleanup(func() { SetNowFunc(nil) })
}

func TestLongTimestamp_SameYear(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	if got, want := LongTimestamp(ts, "3:42 PM"), "Tue Sep 29, 3:42 PM"; got != want {
		t.Fatalf("LongTimestamp = %q, want %q", got, want)
	}
}

func TestLongTimestamp_EarlierYear(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2025, 9, 29, 15, 42, 0, 0, time.Local))
	if got, want := LongTimestamp(ts, "3:42 PM"), "Mon Sep 29 2025, 3:42 PM"; got != want {
		t.Fatalf("LongTimestamp = %q, want %q", got, want)
	}
}

func TestLongTimestamp_LaterYear(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2027, 9, 29, 15, 42, 0, 0, time.Local))
	if got, want := LongTimestamp(ts, "3:42 PM"), "Wed Sep 29 2027, 3:42 PM"; got != want {
		t.Fatalf("LongTimestamp = %q, want %q", got, want)
	}
}

func TestLongTimestamp_UnparseableTSUnchanged(t *testing.T) {
	pinLongTSClock(t)
	for _, ts := range []string{"", "abc", "x.123"} {
		if got := LongTimestamp(ts, "3:42 PM"); got != "3:42 PM" {
			t.Errorf("LongTimestamp(%q) = %q, want short unchanged", ts, got)
		}
	}
}

func TestLongTimestamp_EmptyShortUnchanged(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	if got := LongTimestamp(ts, ""); got != "" {
		t.Fatalf("LongTimestamp(ts, \"\") = %q, want \"\"", got)
	}
}

// selectedHeaderFixture builds a header and a rendered message the way
// the renderers do: styled name + gap + styled short timestamp, then body.
func selectedHeaderFixture(name, short, body string) (header, rendered string) {
	header = name + "  " + styles.Timestamp.Render(short)
	return header, header + "\n" + body
}

func TestSelectedHeader_SubstitutesWhenItFits(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "hello")
	got := SelectedHeader(rendered, header, ts, "3:42 PM", 80)
	want := strings.Replace(rendered, styles.Timestamp.Render("3:42 PM"),
		styles.Timestamp.Render("Tue Sep 29, 3:42 PM"), 1)
	if got != want {
		t.Fatalf("SelectedHeader did not substitute:\n got %q\nwant %q", got, want)
	}
}

func TestSelectedHeader_TooWideUnchanged(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "hello")
	// "sam  Tue Sep 29, 3:42 PM" is 24 columns; one short of that must refuse.
	longWidth := lipgloss.Width("sam  Tue Sep 29, 3:42 PM")
	if got := SelectedHeader(rendered, header, ts, "3:42 PM", longWidth-1); got != rendered {
		t.Fatalf("SelectedHeader substituted despite exceeding maxWidth:\n%q", got)
	}
	if got := SelectedHeader(rendered, header, ts, "3:42 PM", longWidth); got == rendered {
		t.Fatal("SelectedHeader refused a header exactly at maxWidth")
	}
}

func TestSelectedHeader_ReplacesOnlyHeaderOccurrence(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	styledShort := styles.Timestamp.Render("3:42 PM")
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "see you at "+styledShort)
	got := SelectedHeader(rendered, header, ts, "3:42 PM", 80)
	styledLong := styles.Timestamp.Render("Tue Sep 29, 3:42 PM")
	if n := strings.Count(got, styledLong); n != 1 {
		t.Fatalf("long timestamp occurs %d times, want 1:\n%q", n, got)
	}
	if !strings.HasSuffix(got, "see you at "+styledShort) {
		t.Fatalf("body occurrence was rewritten:\n%q", got)
	}
}

func TestSelectedHeader_EmptyShortUnchanged(t *testing.T) {
	pinLongTSClock(t)
	ts := longTSAt(time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local))
	header, rendered := selectedHeaderFixture("sam", "", "hello")
	if got := SelectedHeader(rendered, header, ts, "", 80); got != rendered {
		t.Fatalf("SelectedHeader with empty short changed output:\n%q", got)
	}
}

func TestSelectedHeader_UnparseableTSUnchanged(t *testing.T) {
	pinLongTSClock(t)
	header, rendered := selectedHeaderFixture("sam", "3:42 PM", "hello")
	if got := SelectedHeader(rendered, header, "not-a-ts", "3:42 PM", 80); got != rendered {
		t.Fatalf("SelectedHeader with bad ts changed output:\n%q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages -run 'TestLongTimestamp|TestSelectedHeader' -count=1`
Expected: FAIL to compile — `undefined: LongTimestamp`, `undefined: SelectedHeader`.

- [ ] **Step 3: Implement**

Create `internal/ui/messages/longtimestamp.go`:

```go
package messages

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
)

// timeFromTS returns the local time of a Slack timestamp
// ("1700000000.000100"). ok is false when the seconds part is not an
// integer.
func timeFromTS(ts string) (time.Time, bool) {
	sec, err := strconv.ParseInt(strings.SplitN(ts, ".", 2)[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// LongTimestamp returns short prefixed with the message's local date
// ("Tue Sep 29, " or "Mon Sep 29 2025, " when not the current year).
// Returns short unchanged when ts is unparseable or short is empty.
func LongTimestamp(ts, short string) string {
	if short == "" {
		return short
	}
	t, ok := timeFromTS(ts)
	if !ok {
		return short
	}
	layout := "Mon Jan 2, "
	if t.Year() != nowFunc().Year() {
		layout = "Mon Jan 2 2006, "
	}
	return t.Format(layout) + short
}

// SelectedHeader returns rendered with the header's styled short
// timestamp replaced by the styled long one, provided the resulting
// header line is no wider than maxWidth. Otherwise returns rendered
// unchanged.
//
// The first occurrence of the styled short timestamp in rendered is the
// header's: the only earlier Timestamp-styled text is the
// thread-broadcast label, whose text differs.
func SelectedHeader(rendered, header, ts, short string, maxWidth int) string {
	long := LongTimestamp(ts, short)
	if long == short {
		return rendered
	}
	styledShort := styles.Timestamp.Render(short)
	styledLong := styles.Timestamp.Render(long)
	longHeader := strings.Replace(header, styledShort, styledLong, 1)
	if longHeader == header || lipgloss.Width(longHeader) > maxWidth {
		return rendered
	}
	return strings.Replace(rendered, styledShort, styledLong, 1)
}
```

Replace the body of `DateFromTS` in `internal/ui/messages/model.go` (keep its doc comment):

```go
func DateFromTS(ts string) string {
	t, ok := timeFromTS(ts)
	if !ok {
		return ""
	}
	return t.Format("2006-01-02")
}
```

If `strconv` becomes unused in `model.go`, `go build` will say so; remove the import only in that case.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/messages -count=1`
Expected: PASS (new tests plus the existing `DateFromTS` / day-divider tests, which guard the refactor).

- [ ] **Step 5: Document the helpers**

In `AGENTS.md`, "Text and rendering" table, directly after the `| Date label from a Slack ts | ... |` row, add:

```markdown
| Date-qualified timestamp for the selected message header | `messages.LongTimestamp(ts, short)`, `messages.SelectedHeader(rendered, header, ts, short, maxWidth)` |
```

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/ui/messages
git add internal/ui/messages/longtimestamp.go internal/ui/messages/longtimestamp_test.go internal/ui/messages/model.go AGENTS.md
git commit -m "feat(messages): LongTimestamp and SelectedHeader helpers"
```

---

### Task 2: Messages pane — bake the long timestamp into `linesSelected`

**Files:**
- Modify: `internal/ui/messages/model.go` — `renderMessagePlain` (≈1991-2472) returns `header`, `headerBudget`; `renderMessageEntry` (≈1672-1741) uses `SelectedHeader`
- Create: `internal/ui/messages/selected_timestamp_test.go`

**Interfaces:**
- Consumes: `SelectedHeader(rendered, header, ts, short string, maxWidth int) string`; test helpers `longTSClock`, `longTSAt`, `pinLongTSClock` from `longtimestamp_test.go` (Task 1).
- Produces: `renderMessagePlain(...) (content string, flushes []func(io.Writer) error, sixelRows map[int]sixelEntry, hits []entryHit, reactionHits []reactionEntryHit, header string, headerBudget int)`

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/messages/selected_timestamp_test.go`:

```go
package messages

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// longTSItems: three messages on 2026-09-29 (the pinned clock's day).
// messages.New selects the newest (lee).
func longTSItems() []MessageItem {
	day := time.Date(2026, 9, 29, 15, 0, 0, 0, time.Local)
	return []MessageItem{
		{TS: longTSAt(day.Add(40 * time.Minute)), UserID: "U1", UserName: "priya", Text: "first", Timestamp: "3:40 PM"},
		{TS: longTSAt(day.Add(41 * time.Minute)), UserID: "U2", UserName: "sam", Text: "second", Timestamp: "3:41 PM"},
		{TS: longTSAt(day.Add(42 * time.Minute)), UserID: "U3", UserName: "lee", Text: "third", Timestamp: "3:42 PM"},
	}
}

func longTSModel(t *testing.T, items []MessageItem, width int) (*Model, string) {
	t.Helper()
	pinLongTSClock(t)
	m := New(items, "general")
	view := ansi.Strip(m.View(40, width))
	return &m, view
}

// selectedEntry returns the cache entry for the selected message.
func selectedEntry(t *testing.T, m *Model) viewEntry {
	t.Helper()
	for _, e := range m.cache {
		if e.msgIdx == m.selected {
			return e
		}
	}
	t.Fatalf("no cache entry for selected index %d", m.selected)
	return viewEntry{}
}

func assertHeightsMatch(t *testing.T, m *Model) {
	t.Helper()
	for i, e := range m.cache {
		if len(e.linesSelected) != len(e.linesNormal) {
			t.Errorf("entry %d: len(linesSelected)=%d != len(linesNormal)=%d",
				i, len(e.linesSelected), len(e.linesNormal))
		}
	}
}

func TestSelectedTimestamp_SelectedRowShowsLong(t *testing.T) {
	m, view := longTSModel(t, longTSItems(), 80)
	for _, want := range []string{"lee  Tue Sep 29, 3:42 PM", "priya  3:40 PM", "sam  3:41 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "Tue Sep 29, "); n != 1 {
		t.Errorf("long timestamp appears %d times, want 1:\n%s", n, view)
	}
	assertHeightsMatch(t, m)
}

func TestSelectedTimestamp_MovesWithSelection(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	m.MoveUp()
	view := ansi.Strip(m.View(40, 80))
	for _, want := range []string{"sam  Tue Sep 29, 3:41 PM", "lee  3:42 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("after MoveUp, view missing %q:\n%s", want, view)
		}
	}
}

func TestSelectedTimestamp_UnfocusedStillLong(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	m.SetFocused(true)
	_ = m.View(40, 80)
	m.SetFocused(false)
	view := ansi.Strip(m.View(40, 80))
	if !strings.Contains(view, "lee  Tue Sep 29, 3:42 PM") {
		t.Fatalf("unfocused selection lost the long timestamp:\n%s", view)
	}
}

func TestSelectedTimestamp_NarrowFallsBackToShort(t *testing.T) {
	items := longTSItems()
	// 24-col name: short header 33 cols fits in contentWidth 36; long
	// header 45 does not, and would wrap inside the width-1 fill.
	items[2].UserName = "a-quite-long-displayname"
	m, view := longTSModel(t, items, 40)
	if strings.Contains(view, "Sep 29") {
		t.Fatalf("narrow pane shows long timestamp:\n%s", view)
	}
	if !strings.Contains(view, "a-quite-long-displayname  3:42 PM") {
		t.Fatalf("narrow pane lost the short timestamp:\n%s", view)
	}
	assertHeightsMatch(t, m)
}

// Review Focus 1: with an avatar at width 24, contentWidth is floored at
// 20 but the header row only has width-1-5 = 18 columns. A 20-col long
// header must be refused.
func TestSelectedTimestamp_AvatarNarrowFallsBack(t *testing.T) {
	pinLongTSClock(t)
	day := time.Date(2026, 9, 29, 15, 42, 0, 0, time.Local)
	m := New([]MessageItem{
		{TS: longTSAt(day), UserID: "U1", UserName: "a", Text: "hi", Timestamp: "15:42"},
	}, "general")
	m.SetAvatarFunc(func(string) string { return "AAAA\nAAAA" })
	_ = m.View(40, 24)
	e := selectedEntry(t, &m)
	if got := ansi.Strip(strings.Join(e.linesSelected, "\n")); strings.Contains(got, "Sep 29") {
		t.Fatalf("avatar-narrow selected row shows long timestamp:\n%s", got)
	}
	assertHeightsMatch(t, &m)
}

// Review Focus 3: the (edited) mark is part of the header row.
func TestSelectedTimestamp_EditedMarkCountsTowardWidth(t *testing.T) {
	items := longTSItems()
	// "abcdefghijklmn  Tue Sep 29, 3:42 PM" = 35 cols fits contentWidth 36
	// at width 40; " (edited)" makes it 44 and must force the short form.
	items[2].UserName = "abcdefghijklmn"
	items[2].IsEdited = true
	m, view := longTSModel(t, items, 40)
	if strings.Contains(view, "Sep 29") {
		t.Fatalf("edited header overflowed with long timestamp:\n%s", view)
	}
	assertHeightsMatch(t, m)
}

func TestSelectedTimestamp_CopyUsesShortForm(t *testing.T) {
	m, _ := longTSModel(t, longTSItems(), 80)
	m.BeginSelectionAt(m.chromeHeight, 0)
	m.ExtendSelectionAt(m.chromeHeight+40, 80)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatal("EndSelection ok=false")
	}
	if !strings.Contains(text, "lee  3:42 PM") || strings.Contains(text, "Sep 29") {
		t.Fatalf("copied text should carry the short form only; got %q", text)
	}
}
```

Note on `TestSelectedTimestamp_EditedMarkCountsTowardWidth`: this test must *fail* before Step 3 only through the fit check. Before Step 3 there is no long form at all, so it passes vacuously; its falsifiability comes in Step 4b.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/messages -run TestSelectedTimestamp -count=1`
Expected: FAIL — `SelectedRowShowsLong`, `MovesWithSelection`, `UnfocusedStillLong` fail with "view missing \"lee  Tue Sep 29, 3:42 PM\"" (etc.). The fallback / copy tests pass (nothing substitutes yet).

- [ ] **Step 3: Implement**

In `renderMessagePlain`, extend the named results:

```go
func (m *Model) renderMessagePlain(msg MessageItem, width int, avatarStr string, userNames map[string]string, channelNames map[string]string, isSelected bool, stats *entryPerfStats) (
	content string, flushes []func(io.Writer) error, sixelRows map[int]sixelEntry, hits []entryHit, reactionHits []reactionEntryHit, header string, headerBudget int,
) {
```

Immediately before the final `return`, add (and update the return):

```go
	// The header row is the only row SelectedHeader may lengthen. It
	// must fit both the width the body wraps to and the columns the row
	// really has: contentWidth is floored at 20, which at narrow widths
	// exceeds width-1 (the fill) minus the avatar gutter.
	headerBudget = min(contentWidth, width-1-(contentColBase-1))
	return msgContent, append(allFlushes, flushes...), allSixel, hits, reactionHits, line + editedMark, headerBudget
```

In `renderMessageEntry`:

```go
	rendered, attachFlushes, attachSixel, attachHits, reactHits, header, headerBudget := m.renderMessagePlain(msg, width, avatarStr, m.userNames, m.channelNames, i == m.selected, stats)
	// The selected variant carries the date-qualified timestamp. Only
	// it: linesNormal and linesPlain keep the short form, so clipboard
	// text and mouse->column mapping are unchanged.
	renderedSel := SelectedHeader(rendered, header, msg.TS, msg.Timestamp, headerBudget)
```

and change

```go
	renderedTinted := RepaintBgToSelectionTint(rendered, m.focused)
```

to

```go
	renderedTinted := RepaintBgToSelectionTint(renderedSel, m.focused)
```

`filledNormal` and `linesP` stay derived from `rendered`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ui/messages -count=1`
Expected: PASS.

- [ ] **Step 4b: Prove the fallback tests are falsifiable**

Temporarily change `headerBudget = min(...)` to `headerBudget = 1000`, run
`go test ./internal/ui/messages -run TestSelectedTimestamp -count=1`, and confirm
`NarrowFallsBack`, `AvatarNarrowFallsBack` and `EditedMarkCountsTowardWidth`
fail. Then change it to `headerBudget = contentWidth` and confirm
`AvatarNarrowFallsBack` fails. Restore `min(...)` and re-run: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/ui/messages
git add internal/ui/messages/model.go internal/ui/messages/selected_timestamp_test.go
git commit -m "feat(messages): long timestamp on the selected message header"
```

---

### Task 3: Thread panel — selected replies and the parent

**Files:**
- Modify: `internal/ui/thread/model.go` — `renderThreadMessage` (≈1975-2233) returns `header`, `headerBudget`; reply cache loop (≈1573-1590); parent render (≈1442-1463)
- Modify: `internal/ui/thread/render_test.go:39,74,97,121`, `internal/ui/thread/blockkit_background_test.go:69` (destructure two extra results)
- Modify: `internal/ui/thread/lockstep_test.go` (carol anchor)
- Create: `internal/ui/thread/selected_timestamp_test.go`

**Interfaces:**
- Consumes: `messages.SelectedHeader`, `messages.SetNowFunc`.
- Produces: `func (m *Model) renderThreadMessage(msg messages.MessageItem, width int, userNames, channelNames map[string]string, isSelected bool) (string, []func(io.Writer) error, []reactionEntryHit, string, int)` — 4th result is the header line, 5th the header width budget.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/thread/selected_timestamp_test.go`:

```go
package thread

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// Tuesday 2026-09-29 16:00 LOCAL; see messages/longtimestamp_test.go.
func threadLongTSClock() time.Time {
	return time.Date(2026, 9, 29, 16, 0, 0, 0, time.Local)
}

func threadLongTSAt(minute int) string {
	return fmt.Sprintf("%d.000100", time.Date(2026, 9, 29, 15, minute, 0, 0, time.Local).Unix())
}

// threadLongTS: parent priya 3:40, replies sam 3:41 and lee 3:42.
// SetThread selects the newest reply (lee).
func threadLongTS(t *testing.T, parentName, lastName string, width, height int) (*Model, []string) {
	t.Helper()
	styles.Apply("dark", config.Theme{})
	messages.SetNowFunc(threadLongTSClock)
	t.Cleanup(func() { messages.SetNowFunc(nil) })
	m := New()
	parent := messages.MessageItem{TS: threadLongTSAt(40), UserID: "U1", UserName: parentName, Text: "parent", Timestamp: "3:40 PM"}
	replies := []messages.MessageItem{
		{TS: threadLongTSAt(41), UserID: "U2", UserName: "sam", Text: "first reply", Timestamp: "3:41 PM"},
		{TS: threadLongTSAt(42), UserID: "U3", UserName: lastName, Text: "second reply", Timestamp: "3:42 PM"},
	}
	m.SetThread(parent, replies, "C1", parent.TS)
	return m, stripRows(m.View(height, width))
}

func stripRows(view string) []string {
	rows := strings.Split(view, "\n")
	for i := range rows {
		rows[i] = ansi.Strip(rows[i])
	}
	return rows
}

func assertThreadHeightsMatch(t *testing.T, m *Model) {
	t.Helper()
	for i, e := range m.cache {
		if len(e.linesSelected) != len(e.linesNormal) {
			t.Errorf("reply %d: len(linesSelected)=%d != len(linesNormal)=%d",
				i, len(e.linesSelected), len(e.linesNormal))
		}
	}
}

func assertPaneGeometry(t *testing.T, rows []string, width, height int) {
	t.Helper()
	if len(rows) != height {
		t.Errorf("pane has %d rows, want %d", len(rows), height)
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w != width {
			t.Errorf("row %d width %d, want %d: %q", i, w, width, r)
		}
	}
}

func TestThreadSelectedTimestamp_SelectedReplyShowsLong(t *testing.T) {
	m, rows := threadLongTS(t, "priya", "lee", 80, 30)
	view := strings.Join(rows, "\n")
	for _, want := range []string{"lee  Tue Sep 29, 3:42 PM", "priya  3:40 PM", "sam  3:41 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	if n := strings.Count(view, "Tue Sep 29, "); n != 1 {
		t.Errorf("long timestamp appears %d times, want 1:\n%s", n, view)
	}
	assertThreadHeightsMatch(t, m)
}

func TestThreadSelectedTimestamp_MovesWithSelection(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.MoveUp()
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	for _, want := range []string{"sam  Tue Sep 29, 3:41 PM", "lee  3:42 PM"} {
		if !strings.Contains(view, want) {
			t.Errorf("after MoveUp, view missing %q:\n%s", want, view)
		}
	}
}

func TestThreadSelectedTimestamp_ParentSelectedShowsLong(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.GoToTop()
	m.MoveUp() // parent
	view := strings.Join(stripRows(m.View(30, 80)), "\n")
	if !strings.Contains(view, "priya  Tue Sep 29, 3:40 PM") {
		t.Fatalf("selected parent missing long timestamp:\n%s", view)
	}
	if !strings.Contains(view, "lee  3:42 PM") {
		t.Fatalf("unselected reply should be short:\n%s", view)
	}
	var plain []string
	for _, pl := range m.parentEntry.linesPlain {
		plain = append(plain, pl.Text)
	}
	if got := strings.Join(plain, "\n"); strings.Contains(got, "Sep 29") || !strings.Contains(got, "3:40 PM") {
		t.Fatalf("parent linesPlain must keep the short form; got %q", got)
	}
}

func TestThreadSelectedTimestamp_UnselectedParentShort(t *testing.T) {
	_, rows := threadLongTS(t, "priya", "lee", 80, 30)
	view := strings.Join(rows, "\n")
	if !strings.Contains(view, "priya  3:40 PM") || strings.Contains(view, "priya  Tue") {
		t.Fatalf("unselected parent should show the short timestamp:\n%s", view)
	}
}

func TestThreadSelectedTimestamp_NarrowReplyFallsBack(t *testing.T) {
	// 24-col name: short header 33 fits contentWidth 36; long 45 does not.
	m, rows := threadLongTS(t, "priya", "a-quite-long-displayname", 40, 30)
	view := strings.Join(rows, "\n")
	if strings.Contains(view, "Sep 29") {
		t.Fatalf("narrow thread shows long timestamp:\n%s", view)
	}
	assertThreadHeightsMatch(t, m)
}

// Review Focus 5: the parent is not width-filled; an overflowing header
// would be wrapped by the outer pane style and break the pane geometry.
func TestThreadSelectedTimestamp_NarrowParentFallsBack(t *testing.T) {
	m, _ := threadLongTS(t, "a-quite-long-displayname", "lee", 40, 30)
	m.GoToTop()
	m.MoveUp() // parent
	rows := stripRows(m.View(30, 40))
	if view := strings.Join(rows, "\n"); strings.Contains(view, "Sep 29") {
		t.Fatalf("narrow selected parent shows long timestamp:\n%s", view)
	}
	assertPaneGeometry(t, rows, 40, 30)
}

// Review Focus 1: at width 20, contentWidth is floored at 20 but the row
// has only width-1 = 19 columns. "a  Tue Sep 29, 15:42" is 20 wide.
func TestThreadSelectedTimestamp_TinyWidthFallsBack(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	messages.SetNowFunc(threadLongTSClock)
	t.Cleanup(func() { messages.SetNowFunc(nil) })
	m := New()
	parent := messages.MessageItem{TS: threadLongTSAt(40), UserID: "U1", UserName: "p", Text: "x", Timestamp: "15:40"}
	reply := messages.MessageItem{TS: threadLongTSAt(42), UserID: "U2", UserName: "a", Text: "hi", Timestamp: "15:42"}
	m.SetThread(parent, []messages.MessageItem{reply}, "C1", parent.TS)
	_ = m.View(30, 20)
	for _, e := range m.cache {
		if got := ansi.Strip(strings.Join(e.linesSelected, "\n")); strings.Contains(got, "Sep 29") {
			t.Fatalf("tiny-width selected reply shows long timestamp:\n%s", got)
		}
	}
	assertThreadHeightsMatch(t, m)
}

func TestThreadSelectedTimestamp_CopyUsesShortForm(t *testing.T) {
	m, _ := threadLongTS(t, "priya", "lee", 80, 30)
	m.BeginSelectionAt(firstContentY(m), 0)
	m.ExtendSelectionAt(firstContentY(m)+40, 80)
	text, ok := m.EndSelection()
	if !ok {
		t.Fatal("EndSelection ok=false")
	}
	if !strings.Contains(text, "lee  3:42 PM") || strings.Contains(text, "Sep 29") {
		t.Fatalf("copied text should carry the short form only; got %q", text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/thread -run TestThreadSelectedTimestamp -count=1`
Expected: FAIL — `SelectedReplyShowsLong`, `MovesWithSelection`, `ParentSelectedShowsLong` fail on the missing long form; fallback/copy tests pass.

- [ ] **Step 3: Implement `renderThreadMessage` results**

Change the signature and the final return:

```go
func (m *Model) renderThreadMessage(msg messages.MessageItem, width int, userNames map[string]string, channelNames map[string]string, isSelected bool) (string, []func(io.Writer) error, []reactionEntryHit, string, int) {
```

```go
	// Header width budget for messages.SelectedHeader: the body's wrap
	// width, capped at the columns the row really has (width-1 after the
	// thick left border) because contentWidth is floored at 20.
	return line + bodyRow + bkBlock + attachmentLines + reactionLine, flushes, reactionHits, line, min(contentWidth, width-1)
```

Update test call sites to discard the new results:
- `internal/ui/thread/render_test.go:39,74,97`: `got, _, _ :=` → `got, _, _, _, _ :=`
- `internal/ui/thread/render_test.go:121`: `got, _, hits :=` → `got, _, hits, _, _ :=`
- `internal/ui/thread/blockkit_background_test.go:69`: `got, _, _ :=` → `got, _, _, _, _ :=`

- [ ] **Step 4: Wire the reply cache loop**

```go
			rendered, attachFlushes, reactHits, header, headerBudget := m.renderThreadMessage(reply, width, m.userNames, m.channelNames, i == m.selected)
			// Selected variant only; linesNormal / linesPlain keep the short form.
			renderedSel := messages.SelectedHeader(rendered, header, reply.TS, reply.Timestamp, headerBudget)
```

and change `messages.RepaintBgToSelectionTint(rendered, m.focused)` to `messages.RepaintBgToSelectionTint(renderedSel, m.focused)`. `filledNormal` stays derived from `rendered`.

- [ ] **Step 5: Wire the parent**

```go
	parentContent, _, _, parentHeader, parentHeaderBudget := m.renderThreadMessage(m.parent, width, m.userNames, m.channelNames, parentIsSelected)
	m.parentEntry = viewEntry{ /* unchanged: built from the unmodified parentContent */ }
	...
	if parentIsSelected {
		parentContent = messages.SelectedHeader(parentContent, parentHeader, m.parent.TS, m.parent.Timestamp, parentHeaderBudget)
		parentContent = lipgloss.NewStyle().BorderStyle(thickLeftBorder). /* existing selected border chain */ Render(parentContent)
	}
```

The `SelectedHeader` call goes after `m.parentEntry` is assigned and before the selected border.

- [ ] **Step 6: Update the lockstep anchors**

`TestLockstep_SharedRenderBehaviour` selects the last row (carol) in both panes, so both now render `carol  Sun Mar 15, 9:01 AM`. In `internal/ui/thread/lockstep_test.go`, replace the anchor `"carol  9:01 AM"` with `"carol  Sun Mar 15, 9:01 AM"` in both the `shared` table and the `gaps` table (and in any other lockstep test that uses it — `grep -n 'carol  9:01' internal/ui/thread/lockstep_test.go`). Both panes change identically, so no divergence entry is added. If a lockstep assertion still fails for any other reason, stop and report; do not force a match.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/ui/thread ./internal/ui/messages -count=1`
Expected: PASS.

- [ ] **Step 7b: Prove the fallback tests are falsifiable**

Temporarily return `1000` as the 5th result of `renderThreadMessage`; confirm
`NarrowReplyFallsBack`, `NarrowParentFallsBack` and `TinyWidthFallsBack` fail.
Return `contentWidth`; confirm `TinyWidthFallsBack` fails. Restore
`min(contentWidth, width-1)`; PASS.

- [ ] **Step 8: Commit**

```bash
gofmt -l internal/ui/thread
git add internal/ui/thread
git commit -m "feat(thread): long timestamp on the selected reply and parent header"
```

---

### Task 4: Goldens and full verification

**Files:**
- Modify: `internal/ui/testdata/golden/*.ansi` (re-blessed)
- Possibly modify: any `internal/ui` test asserting on a selected row's header text

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces: nothing new.

- [ ] **Step 1: See which goldens changed**

Run: `go test ./internal/ui -run TestGolden -count=1`
Expected: FAIL for goldens containing a selected message row (at least `base`, `thread_open`; likely `narrow`, `wide`, `no_sidebar`, `window_split`, `drag_selection`).

- [ ] **Step 2: Re-bless and review the diff**

```bash
go test ./internal/ui -run TestGolden -update -count=1
git diff --stat internal/ui/testdata/golden
git diff --word-diff=color internal/ui/testdata/golden | cat -v | grep -n 'Sun Mar 15' | head -40
```

Confirm, per changed golden, that the only differing row is a selected header, whose timestamp became `Sun Mar 15, <time>` (the golden clock is Sunday 2026-03-15; `alice`'s `9:00 AM` row is on Mar 14 → `Sat Mar 14, 9:00 AM` if it is the selected one). Any other changed row is a bug: stop and investigate.

- [ ] **Step 3: Run the rest of `internal/ui`**

Run: `go test ./internal/ui/... -count=1`
Expected: PASS. If a non-golden test fails because it asserted a selected row's short header, update that assertion to the long form only if the row is genuinely the selected one; otherwise investigate.

- [ ] **Step 4: Full verification**

```bash
go build ./...
go vet ./...
gofmt -l .
golangci-lint run
go test ./... -race
```

Expected: build/vet clean, `gofmt -l .` prints nothing, lint clean, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/testdata/golden internal/ui
git commit -m "test(ui): re-bless goldens for the selected-message long timestamp"
```
