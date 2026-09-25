# Thread Stacked Layout and Breadcrumb Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Threads are always openable and readable: an 80-column thread beside the channel when both fit, otherwise the thread and channel stack and focus decides which is drawn, with a breadcrumb header naming the channel and thread.

**Architecture:** `panelLayout.Compute` gains a `threadFront` input and a stacked branch; the render-time auto-hide in `App.View` is deleted. `App` records the last focused content pane (`stackFront`) in a thin `Update` wrapper, and derives `threadInFront()` from focus. `thread.Model` renders a breadcrumb row in place of its `Thread  N replies` header, fed by a new `SetBreadcrumb` setter.

**Tech Stack:** Go 1.26, bubbletea v2 (`charm.land/bubbletea/v2`), lipgloss v2 (`charm.land/lipgloss/v2`), `github.com/charmbracelet/x/ansi`. Tests are stdlib `testing` only, white-box.

**Spec:** `docs/superpowers/specs/2026-09-24-thread-stacked-layout-design.md`

**Worktree:** `/home/dev/local_code/slk/.worktrees/thread-stacked-layout`, branch `feat/thread-stacked-layout`. Run every command from there.

## Global Constraints

- Layout constants: `minMsgWidth = 40`, `minThreadW = 80`, thread share `35%`, pane floor `10`, `paneBorder = 2`.
- Side by side iff `msgAreaWidth - 4 >= 120` (≥ 162 cols with the default 6-col rail + 30-col sidebar; ≥ 130 with the sidebar hidden).
- Breadcrumb: `<glyph> <channel> › Thread from <author> · N replies`, right-aligned `esc close` with ≥ 2 spaces before it. Drop order: hint, then `from <author>`, then truncate channel with `…`. `Thread · N replies` always kept. Header stays 2 rows.
- Glyphs come from `messages.ChannelGlyph` (exported in Task 1): `#` default, `◆` private, `●` dm / group_dm.
- `internal/ui` gains no I/O imports (`internal/ui/boundary_test.go`).
- `View()` must not write `threadVisible`, `focusedPanel` or `stackFront`.
- Every commit: `go build ./...`, `go vet ./...`, `go test ./internal/ui/...` green, `gofmt -l .` empty.
- Goldens are re-blessed only in the task whose change moves them, and only after reading the ANSI-stripped diff. (The spec put goldens in milestone 3; they move here so every commit is green.)
- Behaviour found broken that this plan does not own: record it in the commit message, do not fix it.

## Review Focus

Inputs the spec implies but its own test list does not exercise; each has a test in the owning task.

1. **Resize across the 162-column threshold with a thread open and the channel focused** — the thread goes behind the channel, stays open, and comes back beside it when widened. Test: Task 3, `TestStacked_ResizeAcrossThreshold`.
2. **Switching channel while the thread is in front** — the thread closes (existing `CloseThread` in `reducer_channels.go`) and the channel is drawn again. Test: Task 3, `TestStacked_ChannelSwitchClosesThread`.
3. **Toggling the sidebar with a thread open at 140 columns** — hiding it frees enough room to go side by side. Test: Task 3, `TestStacked_SidebarToggleUnstacks`.
4. **Replying (`i`) while stacked at 80 columns** — insert mode focuses the thread compose and the thread stays in front. Test: Task 3, `TestStacked_InsertModeKeepsThreadInFront`.
5. **Very narrow terminals (30–60 cols) with a thread open** — rendering does not panic and bands still end at the terminal width where the pane is above its floor. Test: Task 3, `TestStacked_TinyTerminalsRender`.

---

## Milestone 1 — runnable build (Tasks 1–3), then STOP for a hands-on test drive

### Task 1: Breadcrumb in `thread.Model`

**Files:**
- Modify: `internal/ui/messages/model.go:616-627` (export glyph), `:2926` (call site)
- Create: `internal/ui/thread/breadcrumb.go`
- Create: `internal/ui/thread/breadcrumb_test.go`
- Modify: `internal/ui/thread/model.go` (fields near `:149-160`, `SetBreadcrumb` after `SetUnreadBoundary` `:342-349`, chrome build `:1346-1380`)
- Modify: `internal/ui/thread/lockstep_test.go:450-453` (divergence 5 text)
- Modify: `AGENTS.md` (Text and rendering table)
- Re-bless: `internal/ui/testdata/golden/thread_open.ansi`, `wide.ansi`

**Interfaces:**
- Produces: `func messages.ChannelGlyph(chType string) string`; `func (m *thread.Model) SetBreadcrumb(channelName, channelType string)`; unexported `renderBreadcrumb(width int, channelName, channelType, author string, replyCount int) string` in package `thread`.

- [ ] **Step 1: Export the glyph helper**

In `internal/ui/messages/model.go` replace:

```go
// channelGlyph returns the prefix glyph to render before the channel
// name in the header. Mirrors the sidebar's type-to-glyph mapping.
func channelGlyph(chType string) string {
```

with:

```go
// ChannelGlyph returns the prefix glyph for a channel type: "◆" for
// "private", "●" for "dm" and "group_dm", "#" otherwise. Used by the
// messages-pane header and the thread breadcrumb.
func ChannelGlyph(chType string) string {
```

and at the header call site (`fmt.Sprintf("%s %s", channelGlyph(m.channelType), m.channelName)`) change `channelGlyph` to `ChannelGlyph`.

Run: `go build ./internal/ui/...` — Expected: success.

- [ ] **Step 2: Write the failing breadcrumb tests**

Create `internal/ui/thread/breadcrumb_test.go`:

```go
package thread

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
)

// pad right-pads s with spaces to width cells.
func pad(s string, width int) string {
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

// hinted places the close hint flush right of left at width cells.
func hinted(left string, width int) string {
	return pad(left, width-ansi.StringWidth(breadcrumbHint)) + breadcrumbHint
}

func TestRenderBreadcrumb(t *testing.T) {
	const full = "# general › Thread from alice · 2 replies" // 41 cells
	tests := []struct {
		name    string
		width   int
		channel string
		chType  string
		author  string
		replies int
		want    string
	}{
		{"room for everything", 60, "general", "channel", "alice", 2, hinted(full, 60)},
		{"hint fits exactly with a two-space gap", 52, "general", "channel", "alice", 2, full + "  " + breadcrumbHint},
		{"one short of the hint drops only the hint", 51, "general", "channel", "alice", 2, pad(full, 51)},
		{"full crumb fits exactly", 41, "general", "channel", "alice", 2, full},
		{"author dropped next", 40, "general", "channel", "alice", 2, pad("# general › Thread · 2 replies", 40)},
		{"channel truncated last", 25, "general", "channel", "alice", 2, "# g… › Thread · 2 replies"},
		{"no room for any channel", 22, "general", "channel", "alice", 2, pad("Thread · 2 replies", 22)},
		{"core hard-truncated below its own width", 10, "general", "channel", "alice", 2, "Thread · 2"},
		{"singular reply", 60, "general", "channel", "alice", 1, hinted("# general › Thread from alice · 1 reply", 60)},
		{"no author", 60, "general", "channel", "", 2, hinted("# general › Thread · 2 replies", 60)},
		{"no channel", 60, "", "", "alice", 2, hinted("Thread from alice · 2 replies", 60)},
		{"private glyph", 60, "design", "private", "alice", 2, hinted("◆ design › Thread from alice · 2 replies", 60)},
		{"dm glyph", 60, "bob", "dm", "alice", 2, hinted("● bob › Thread from alice · 2 replies", 60)},
		{"group dm glyph", 60, "bob, carol", "group_dm", "alice", 2, hinted("● bob, carol › Thread from alice · 2 replies", 60)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Guards the table itself: a mistyped expectation must not
			// silently assert a different width than the row claims.
			if w := ansi.StringWidth(tt.want); w != tt.width {
				t.Fatalf("bad row: want is %d cells, width is %d", w, tt.width)
			}
			got := renderBreadcrumb(tt.width, tt.channel, tt.chType, tt.author, tt.replies)
			if w := ansi.StringWidth(got); w != tt.width {
				t.Errorf("width = %d, want %d", w, tt.width)
			}
			if plain := ansi.Strip(got); plain != tt.want {
				t.Errorf("breadcrumb =\n%q\nwant\n%q", plain, tt.want)
			}
		})
	}
}

func breadcrumbTestModel() *Model {
	m := New()
	m.SetThread(
		messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"},
		[]messages.MessageItem{
			{TS: "2.0", UserID: "U2", UserName: "bob", Text: "one"},
			{TS: "3.0", UserID: "U2", UserName: "bob", Text: "two"},
		},
		"C1", "1.0")
	return m
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func TestView_HeaderIsTheBreadcrumb(t *testing.T) {
	m := breadcrumbTestModel()
	m.SetBreadcrumb("general", "channel")
	got := ansi.Strip(firstLine(m.View(20, 60)))
	if want := hinted("# general › Thread from alice · 2 replies", 60); got != want {
		t.Errorf("header =\n%q\nwant\n%q", got, want)
	}
	if m.chromeHeight != 2 {
		t.Errorf("chromeHeight = %d, want 2 (breadcrumb + separator); hit-testing depends on it", m.chromeHeight)
	}
}

func TestView_BreadcrumbChangeInvalidatesChrome(t *testing.T) {
	m := breadcrumbTestModel()
	m.SetBreadcrumb("general", "channel")
	_ = m.View(20, 60)
	v := m.Version()
	m.SetBreadcrumb("design", "private")
	if m.Version() == v {
		t.Error("SetBreadcrumb did not bump Version; the App panel cache would serve the old header")
	}
	if got := ansi.Strip(firstLine(m.View(20, 60))); !strings.HasPrefix(got, "◆ design › Thread") {
		t.Errorf("header after SetBreadcrumb = %q, want the new channel", got)
	}
	v = m.Version()
	m.SetBreadcrumb("design", "private")
	if m.Version() != v {
		t.Error("an unchanged SetBreadcrumb bumped Version")
	}
}

func TestView_BreadcrumbAuthorFallsBackToUserNames(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "1.0", UserID: "U9", Text: "parent"}, nil, "C1", "1.0")
	m.SetUserNames(map[string]string{"U9": "zed"})
	if got := ansi.Strip(firstLine(m.View(20, 60))); !strings.HasPrefix(got, "Thread from zed · 0 replies") {
		t.Errorf("header = %q, want the author resolved from userNames", got)
	}
}
```

Run: `go test ./internal/ui/thread -run 'Breadcrumb' -count=1`
Expected: FAIL to compile — `undefined: breadcrumbHint`, `undefined: renderBreadcrumb`, `m.SetBreadcrumb undefined`.

- [ ] **Step 3: Implement `renderBreadcrumb`**

Create `internal/ui/thread/breadcrumb.go`:

```go
package thread

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// breadcrumbHint is the right-aligned close hint on the breadcrumb row.
const breadcrumbHint = "esc close"

// breadcrumbHintGap is the minimum run of spaces before the hint.
const breadcrumbHintGap = 2

// renderBreadcrumb renders the thread header row at exactly width
// cells:
//
//	# general › Thread from alice · 2 replies        esc close
//
// When width is short, parts drop in a fixed order: the hint, then
// "from <author>", then the channel name is truncated with "…". An
// empty channelName omits the channel and its separator; an empty
// author omits "from". "Thread · N replies" is always kept and is only
// hard-truncated when width is narrower than it.
func renderBreadcrumb(width int, channelName, channelType, author string, replyCount int) string {
	if width <= 0 {
		return ""
	}
	label := "replies"
	if replyCount == 1 {
		label = "reply"
	}
	const (
		sep    = " › "
		thread = "Thread"
	)
	count := fmt.Sprintf(" · %d %s", replyCount, label)
	channel := ""
	if channelName != "" {
		channel = messages.ChannelGlyph(channelType) + " " + channelName
	}
	from := ""
	if author != "" {
		from = " from " + author
	}
	leftWidth := func() int {
		w := ansi.StringWidth(thread + from + count)
		if channel != "" {
			w += ansi.StringWidth(channel + sep)
		}
		return w
	}

	showHint := leftWidth()+breadcrumbHintGap+ansi.StringWidth(breadcrumbHint) <= width
	if leftWidth() > width {
		from = ""
	}
	if channel != "" && leftWidth() > width {
		room := width - ansi.StringWidth(sep+thread+count)
		if room < 2 {
			channel = ""
		} else {
			channel = ansi.Truncate(channel, room, "…")
		}
	}

	bg := lipgloss.NewStyle().Background(styles.Background)
	var b strings.Builder
	if channel != "" {
		b.WriteString(bg.Foreground(styles.TextMuted).Render(channel + sep))
	}
	b.WriteString(bg.Foreground(styles.Accent).Bold(true).Render(thread + from))
	b.WriteString(bg.Foreground(styles.TextPrimary).Render(count))
	left := b.String()
	if ansi.StringWidth(left) > width {
		left = ansi.Truncate(left, width, "")
	}
	used := ansi.StringWidth(left)
	if showHint {
		gap := width - used - ansi.StringWidth(breadcrumbHint)
		return left + bg.Render(strings.Repeat(" ", gap)) +
			bg.Foreground(styles.TextMuted).Render(breadcrumbHint)
	}
	return left + bg.Render(strings.Repeat(" ", width-used))
}
```

Run: `go test ./internal/ui/thread -run 'Breadcrumb' -count=1`
Expected: still FAIL to compile, now only on `m.SetBreadcrumb undefined` — Step 4 adds it.

- [ ] **Step 4: Wire the breadcrumb into `thread.Model`**

In `internal/ui/thread/model.go`, add to the `Model` struct directly after `chromeChannelNamesV uint64 ...`:

```go
	// Breadcrumb inputs (SetBreadcrumb), and the values the chrome
	// cache was last built from.
	crumbChannel       string
	crumbType          string
	chromeCrumbChannel string
	chromeCrumbType    string
	chromeAuthor       string
```

Add after `SetUnreadBoundary`:

```go
// SetBreadcrumb sets the channel the header breadcrumb names.
// channelType picks the glyph (see messages.ChannelGlyph); an empty
// channelName omits the channel segment.
func (m *Model) SetBreadcrumb(channelName, channelType string) {
	if m.crumbChannel == channelName && m.crumbType == channelType {
		return
	}
	m.crumbChannel, m.crumbType = channelName, channelType
	m.dirty()
}

// breadcrumbAuthor is the parent's display name: the parent's own
// UserName, else the userNames entry for its UserID.
func (m *Model) breadcrumbAuthor() string {
	if m.parent.UserName != "" {
		return m.parent.UserName
	}
	return m.userNames[m.parent.UserID]
}
```

In `View`, replace the chrome block from `if !m.chromeCacheValid ||` through the closing `}` of that `if` (currently builds `replyLabel` and `header := lipgloss.NewStyle()...Render(fmt.Sprintf("Thread  %d %s", ...))`) with:

```go
	author := m.breadcrumbAuthor()
	if !m.chromeCacheValid ||
		m.chromeWidth != width ||
		m.chromeReplyCount != chromeReplyCount ||
		m.chromeUserNamesV != m.userNamesV ||
		m.chromeChannelNamesV != m.channelNamesV ||
		m.chromeCrumbChannel != m.crumbChannel ||
		m.chromeCrumbType != m.crumbType ||
		m.chromeAuthor != author {
		header := renderBreadcrumb(width, m.crumbChannel, m.crumbType, author, chromeReplyCount)
		separator := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Border).
			Render(strings.Repeat("-", width))
		m.chromeCache = header + "\n" + separator
		m.chromeHeight = lipgloss.Height(m.chromeCache)
		m.chromeCacheValid = true
		m.chromeWidth = width
		m.chromeReplyCount = chromeReplyCount
		m.chromeUserNamesV = m.userNamesV
		m.chromeChannelNamesV = m.channelNamesV
		m.chromeCrumbChannel = m.crumbChannel
		m.chromeCrumbType = m.crumbType
		m.chromeAuthor = author
	}
```

If `fmt` becomes unused in `model.go`, `go build` will say so; remove the import only in that case.

Run: `go test ./internal/ui/thread -count=1`
Expected: PASS (all thread tests, including the existing `TestViewRendersContent`, which only checks for `Thread`).

- [ ] **Step 5: Update lockstep divergence 5**

In `internal/ui/thread/lockstep_test.go`, replace the three lines

```go
//     thread renders fmt.Sprintf("Thread  %d %s", n, replyLabel) -- the
//     label is pluralised, "reply" at n==1 (thread/model.go:1292-1301)
//     -- plus a "-" rule (thread/model.go:1302-1306). Height is always
```

with

```go
//     thread renders a breadcrumb, renderBreadcrumb in
//     thread/breadcrumb.go: "<glyph> <channel> › Thread from <author>
//     · N replies" plus a right-aligned "esc close", fed by
//     SetBreadcrumb(channelName, channelType) -- the TYPE, not a
//     pre-formatted title, as the note above asks of a chrome hook. The
//     label is pluralised, "reply" at n==1. Then a "-" rule. Height is always
```

(the following line, `//     2 rows and chromeHeight stays unexported.`, stays).

Run: `go test ./internal/ui/thread -run Lockstep -count=1` — Expected: PASS.

- [ ] **Step 6: Add the helper to `AGENTS.md`**

In the "Text and rendering" table, after the `mpdm channel name → human name` row, add:

```
| Channel-type glyph (`#` / `◆` / `●`) | `messages.ChannelGlyph(chType)` |
```

- [ ] **Step 7: Re-bless the thread goldens and read the diff**

Run: `go test ./internal/ui -run TestGolden -count=1`
Expected: FAIL on `thread_open` and `wide` only (their thread header changed).

Run: `go test ./internal/ui -run 'TestGolden$' -update -count=1` then:

```bash
for f in thread_open wide; do
  diff <(git show HEAD:internal/ui/testdata/golden/$f.ansi | sed 's/\x1b\[[0-9;:]*m//g') \
       <(sed 's/\x1b\[[0-9;:]*m//g' internal/ui/testdata/golden/$f.ansi)
done
git status --short internal/ui/testdata/golden
```

Expected: exactly one changed line per file, the thread header: `Thread  2 replies` becomes `Thread from carol · 2 replies` (`wide` also shows `esc close` flush right; `thread_open`'s 33-column pane does not fit the hint). Only those two files modified. Anything else: stop and investigate.

Run: `go test ./internal/ui/... -count=1` — Expected: PASS.

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./internal/ui/...
git add internal/ui/messages/model.go internal/ui/thread AGENTS.md internal/ui/testdata/golden
git commit -m "feat(thread): breadcrumb header naming channel, author and reply count"
```

---

### Task 2: Feed the breadcrumb from the App

**Files:**
- Modify: `internal/ui/app.go` (`openThreadPanel` `:1856-1879`, `threadComposeChannelName` `:1844-1849`, `openSelectedThreadCmd` `:2038-2066`)
- Create: `internal/ui/thread_breadcrumb_test.go`
- Modify: `internal/ui/golden_test.go` (`goldenThreadApp`)
- Re-bless: `thread_open.ansi`, `wide.ansi`

**Interfaces:**
- Consumes: `(*thread.Model).SetBreadcrumb(channelName, channelType string)` (Task 1).
- Produces: `func (a *App) applyThreadBreadcrumb(channelID, channelType string)`; test helper `func updateAndRender(t *testing.T, a *App, msg tea.Msg)` in `internal/ui/thread_breadcrumb_test.go` (Task 3 and 4 use it).

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/thread_breadcrumb_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/cache"
)

// updateAndRender drives msg through the real Update chain and renders
// one frame, as the bubbletea runtime does after every message.
func updateAndRender(t *testing.T, a *App, msg tea.Msg) {
	t.Helper()
	_, _ = a.Update(msg)
	_ = a.View()
}

func TestThreadBreadcrumb_EnterNamesTheSidebarChannel(t *testing.T) {
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30))...)
	focusMessages(t, a)
	updateAndRender(t, a, keyCode(tea.KeyEnter))

	screen := ansi.Strip(a.View().Content)
	if !strings.Contains(screen, "# general › Thread from alice") {
		t.Errorf("thread header does not name the channel and author:\n%s", screen)
	}
}

func TestThreadBreadcrumb_ThreadsViewUsesSummaryChannelType(t *testing.T) {
	// C2 is a plain "channel" in the sidebar; the summary says private.
	// The summary is the fresher source for a thread opened from the
	// list, so its type must win.
	sums := []cache.ThreadSummary{{
		ChannelID: "C2", ChannelName: "random", ChannelType: "private",
		ThreadTS: "50.0", ParentTS: "50.0", ParentUserID: "U1",
		ParentText: "hello", ReplyCount: 1,
	}}
	a := newTestApp(t, append(normalOpts(), withWindowSize(200, 30), withThreadsView(sums))...)
	updateAndRender(t, a, ThreadsViewActivatedMsg{})

	if !a.threadVisible {
		t.Fatal("precondition: activating the Threads view did not open the selected thread")
	}
	screen := ansi.Strip(a.View().Content)
	if !strings.Contains(screen, "◆ random › Thread") {
		t.Errorf("thread header does not show the summary's private glyph:\n%s", screen)
	}
}
```

Run: `go test ./internal/ui -run TestThreadBreadcrumb -count=1`
Expected: FAIL — headers read `Thread from alice · …` with no channel segment.

- [ ] **Step 2: Implement `applyThreadBreadcrumb` and call it**

In `internal/ui/app.go`, add directly after `threadComposeChannelName`:

```go
// applyThreadBreadcrumb names the open thread's channel in the thread
// header. The name is the one the thread compose placeholder shows.
// channelType may be "" when the caller has none, in which case the
// sidebar's entry for channelID supplies it; an unknown channel falls
// back to the default "#" glyph.
func (a *App) applyThreadBreadcrumb(channelID, channelType string) {
	if channelType == "" {
		for _, it := range a.sidebar.Items() {
			if it.ID == channelID {
				channelType = it.Type
				break
			}
		}
	}
	a.threadPanel.SetBreadcrumb(a.threadComposeChannelName(channelID), channelType)
}
```

In `openThreadPanel`, after `a.threadCompose.SetChannel(a.threadComposeChannelName(channelID))` add:

```go
	a.applyThreadBreadcrumb(channelID, "")
```

In `openSelectedThreadCmd`, after `a.threadCompose.SetChannel(a.threadComposeChannelName(sum.ChannelID))` add:

```go
	a.applyThreadBreadcrumb(sum.ChannelID, sum.ChannelType)
```

Run: `go test ./internal/ui -run TestThreadBreadcrumb -count=1` — Expected: PASS.

- [ ] **Step 3: Give the golden thread a breadcrumb**

In `internal/ui/golden_test.go`, `goldenThreadApp`, after `a.threadPanel.SetThread(msgs[2], msgs[3:5], "C1", msgs[2].ThreadTS)` add:

```go
	// The production helper, not a literal: the golden then pins how
	// openThreadPanel names the channel ("general" via SetChannels'
	// name map, "channel" type via the sidebar entry).
	a.applyThreadBreadcrumb("C1", "")
```

Run: `go test ./internal/ui -run 'TestGolden$' -update -count=1`, then the same stripped `diff` loop as Task 1 Step 7.

Expected: one changed line per file. `wide`: `# general › Thread from carol · 2 replies` with `esc close` flush right. `thread_open` (33-col content): `# general › Thread · 2 replies`. No other golden changes.

- [ ] **Step 4: Run the package and commit**

Run: `go test ./internal/ui/... -count=1` — Expected: PASS.

```bash
gofmt -l . && go vet ./internal/ui/...
git add internal/ui/app.go internal/ui/thread_breadcrumb_test.go internal/ui/golden_test.go internal/ui/testdata/golden
git commit -m "feat(ui): name the thread's channel in its breadcrumb"
```

---

### Task 3: Stacked layout, focus-decided front pane, no render-time mutation

**Files:**
- Modify: `internal/ui/panellayout.go` (package comment `:22-26`, `panelLayoutFrame` `:50-67`, `Compute` `:69-148`)
- Create: `internal/ui/panellayout_test.go`
- Modify: `internal/ui/app.go` (struct field after `:137`, `Update` `:850`, `View` `:3097-3131`, `collectSixelPlacements` `:3212`)
- Modify: `internal/ui/windows.go:50-53`
- Create: `internal/ui/thread_stacked_test.go`
- Modify (mechanical): `internal/ui/winmodels_test.go`, `view_window_region_test.go`, `sixelpaint_test.go`, `view_composite_test.go`, `view_messages_border_test.go`
- Modify: `internal/ui/golden_test.go` (scenario struct, scenarios, delete two tests, add one)
- Modify: `AGENTS.md` (Test helpers table)
- Re-bless: `thread_open.ansi`, `wide.ansi`, `narrow.ansi`

**Interfaces:**
- Consumes: `updateAndRender` (Task 2).
- Produces: `func (l *panelLayout) Compute(width, height, railWidth, sidebarWidth int, sidebarVisible, threadVisible, threadFront bool) panelLayoutFrame` (no `ThreadAutoHidden` field); `App.stackFront Panel`; `func (a *App) threadInFront() bool`; `func (a *App) computeFrame() panelLayoutFrame`; test helpers `stackedApp(t, w, extra...)`, `assertFront(t, a, wantChannel, wantThread)` in `thread_stacked_test.go`.

- [ ] **Step 1: Write the failing layout tests**

Create `internal/ui/panellayout_test.go`:

```go
package ui

import (
	"fmt"
	"testing"
)

// The chrome every layout case assumes: the 6-column workspace rail
// and the default 30-column sidebar (+2 border when shown).
const (
	testRailW    = 6
	testSidebarW = 30
)

func TestPanelLayoutCompute(t *testing.T) {
	type want struct{ msgW, msgB, thrW, thrB int }
	tests := []struct {
		name        string
		width       int
		sidebar     bool
		thread      bool
		threadFront bool
		want        want
	}{
		{"no thread", 120, true, false, false, want{80, 2, 0, 0}},
		{"80 stacked, thread front", 80, true, true, true, want{0, 0, 40, 2}},
		{"80 stacked, channel front", 80, true, true, false, want{40, 2, 0, 0}},
		{"120 stacked, thread front", 120, true, true, true, want{0, 0, 80, 2}},
		{"120 stacked, channel front", 120, true, true, false, want{80, 2, 0, 0}},
		{"140 stacked, thread front", 140, true, true, true, want{0, 0, 100, 2}},
		{"161 one below threshold, thread front", 161, true, true, true, want{0, 0, 121, 2}},
		{"161 one below threshold, channel front", 161, true, true, false, want{121, 2, 0, 0}},
		{"162 threshold, both minima", 162, true, true, true, want{40, 2, 80, 2}},
		{"162 threshold, front irrelevant", 162, true, true, false, want{40, 2, 80, 2}},
		{"200 thread lifted to 80", 200, true, true, false, want{78, 2, 80, 2}},
		{"300 35% already above 80", 300, true, true, false, want{167, 2, 91, 2}},
		{"sidebar hidden 129 stacked", 129, false, true, true, want{0, 0, 121, 2}},
		{"sidebar hidden 130 side by side", 130, false, true, true, want{40, 2, 80, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newPanelLayout()
			f := l.Compute(tt.width, 30, testRailW, testSidebarW, tt.sidebar, tt.thread, tt.threadFront)
			got := want{f.MsgWidth, f.MsgBorder, f.ThreadWidth, f.ThreadBorder}
			if got != tt.want {
				t.Fatalf("widths = %+v, want %+v", got, tt.want)
			}
			sbEnd := testRailW
			if tt.sidebar {
				sbEnd += testSidebarW + 2
			}
			if l.sidebarEnd != sbEnd || l.msgEnd != sbEnd+got.msgW+got.msgB || l.threadEnd != tt.width {
				t.Errorf("bands = rail %d / sidebar %d / msg %d / thread %d, want sidebarEnd %d, msgEnd %d, threadEnd %d",
					l.railWidth, l.sidebarEnd, l.msgEnd, l.threadEnd, sbEnd, sbEnd+got.msgW+got.msgB, tt.width)
			}
			if f.ContentHeight != 29 {
				t.Errorf("ContentHeight = %d, want 29", f.ContentHeight)
			}
		})
	}
}

func TestPanelLayoutCompute_Sweep(t *testing.T) {
	const floorPaneW = 10
	for width := 20; width <= 300; width++ {
		for _, sidebar := range []bool{true, false} {
			for _, thread := range []bool{true, false} {
				for _, front := range []bool{true, false} {
					l := newPanelLayout()
					f := l.Compute(width, 30, testRailW, testSidebarW, sidebar, thread, front)
					area := width - testRailW
					if sidebar {
						area -= testSidebarW + 2
					}
					where := func() string {
						return fmt.Sprintf("width %d sidebar %v thread %v front %v", width, sidebar, thread, front)
					}
					if f.MsgWidth < 0 || f.ThreadWidth < 0 {
						t.Fatalf("%s: negative width %+v", where(), f)
					}
					msgDrawn, thrDrawn := f.MsgBorder > 0, f.ThreadBorder > 0
					if !msgDrawn && !thrDrawn {
						t.Fatalf("%s: no content pane drawn", where())
					}
					if !thread && thrDrawn {
						t.Fatalf("%s: thread drawn with no thread open", where())
					}
					sideBySide := thread && area-4 >= 120
					if thread && sideBySide != (msgDrawn && thrDrawn) {
						t.Fatalf("%s: side by side = %v, want %v", where(), msgDrawn && thrDrawn, sideBySide)
					}
					if sideBySide && (f.ThreadWidth < 80 || f.MsgWidth < 40) {
						t.Fatalf("%s: side by side below minima %+v", where(), f)
					}
					if thread && !sideBySide && thrDrawn != front {
						t.Fatalf("%s: stacked pane is thread=%v, want %v", where(), thrDrawn, front)
					}
					if area-2 >= floorPaneW && l.threadEnd != width {
						t.Fatalf("%s: bands end at %d, want %d", where(), l.threadEnd, width)
					}
				}
			}
		}
	}
}
```

Run: `go test ./internal/ui -run TestPanelLayoutCompute -count=1`
Expected: FAIL to compile — `too many arguments in call to l.Compute`.

- [ ] **Step 2: Rewrite `Compute`**

In `internal/ui/panellayout.go`, replace the package-comment paragraph starting `// Auto-hide: if there isn't room` (four lines) with:

```go
// Stacking: when a thread is open and there is room for the messages
// pane (≥40 cols) AND an 80-col thread pane side by side, both are
// drawn. Otherwise the two stack: only one is drawn, across the whole
// content area, and the caller's threadFront decides which. Compute is
// pure geometry; App.threadInFront owns the focus rule.
```

Remove the `ThreadAutoHidden` field and its comment from `panelLayoutFrame`. Replace the `Compute` doc comment's width-algorithm bullet for the thread with:

```go
//   - thread, when visible and there is room for both panes, consumes
//     max(35% of (width - rail - sidebar), 80) plus 2 cols of border,
//     capped so messages keeps 40. Without room, the panes stack and
//     only the front one (threadFront) is drawn, across the whole area.
```

Replace the body of `Compute` (signature through `return`) with:

```go
func (l *panelLayout) Compute(width, height, railWidth, sidebarWidth int, sidebarVisible, threadVisible, threadFront bool) panelLayoutFrame {
	const (
		statusHeight = 1
		paneBorder   = 2 // left + right border cols
		minMsgWidth  = 40
		minThreadW   = 80
		floorPaneW   = 10
	)
	contentHeight := height - statusHeight

	sbWidth := 0
	sbBorder := 0
	if sidebarVisible {
		sbWidth = sidebarWidth
		sbBorder = paneBorder
	}
	msgAreaWidth := width - railWidth - sbWidth - sbBorder

	var msgWidth, msgBorder, threadWidth, threadBorder int
	switch {
	case !threadVisible:
		msgWidth, msgBorder = msgAreaWidth-paneBorder, paneBorder
	case msgAreaWidth-2*paneBorder >= minMsgWidth+minThreadW:
		threadWidth = max(msgAreaWidth*35/100, minThreadW)
		threadWidth = min(threadWidth, msgAreaWidth-2*paneBorder-minMsgWidth)
		threadBorder, msgBorder = paneBorder, paneBorder
		msgWidth = msgAreaWidth - msgBorder - threadWidth - threadBorder
	case threadFront:
		threadWidth, threadBorder = msgAreaWidth-paneBorder, paneBorder
	default:
		msgWidth, msgBorder = msgAreaWidth-paneBorder, paneBorder
	}
	if msgBorder > 0 {
		msgWidth = max(msgWidth, floorPaneW)
	}
	if threadBorder > 0 {
		threadWidth = max(threadWidth, floorPaneW)
	}

	// Bands for PanelAt and the mouse routers. A pane that is not drawn
	// has a zero-width band, so its range can never match.
	l.railWidth = railWidth
	l.sidebarEnd = railWidth + sbWidth + sbBorder
	l.msgEnd = l.sidebarEnd + msgWidth + msgBorder
	l.threadEnd = l.msgEnd + threadWidth + threadBorder

	return panelLayoutFrame{
		RailWidth:     railWidth,
		SidebarWidth:  sbWidth,
		SidebarBorder: sbBorder,
		MsgWidth:      msgWidth,
		MsgBorder:     msgBorder,
		ThreadWidth:   threadWidth,
		ThreadBorder:  threadBorder,
		ContentHeight: contentHeight,
	}
}
```

(`go build` fails at the callers until Step 4; that is expected.)

- [ ] **Step 3: Write the failing App-level tests**

Create `internal/ui/thread_stacked_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// stackedApp is normalOpts at w×30, sized through the real
// tea.WindowSizeMsg path, with the channel pane focused on a selected
// message.
func stackedApp(t *testing.T, w int, extra ...testOpt) *App {
	t.Helper()
	opts := append(normalOpts(), withWindowSize(w, 30))
	a := newTestApp(t, append(opts, extra...)...)
	focusMessages(t, a)
	return a
}

// assertFront checks which content panes the last View() drew, read
// from the bands it stored for mouse hit-testing.
func assertFront(t *testing.T, a *App, wantChannel, wantThread bool) {
	t.Helper()
	gotChannel := a.layout.msgEnd > a.layout.sidebarEnd
	gotThread := a.layout.threadEnd > a.layout.msgEnd
	if gotChannel != wantChannel || gotThread != wantThread {
		t.Fatalf("drawn: channel=%v thread=%v, want channel=%v thread=%v (bands %+v)",
			gotChannel, gotThread, wantChannel, wantThread, *a.layout)
	}
	if a.layout.threadEnd != a.width {
		t.Fatalf("bands end at %d, want the terminal width %d", a.layout.threadEnd, a.width)
	}
}

func TestStacked_FocusDecidesWhichPaneIsDrawn(t *testing.T) {
	a := stackedApp(t, 120)

	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if !a.threadVisible || a.focusedPanel != PanelThread {
		t.Fatalf("Enter: threadVisible=%v focus=%v, want open and focused", a.threadVisible, a.focusedPanel)
	}
	assertFront(t, a, false, true)
	if !strings.Contains(statusbarText(a), "> Thread") {
		t.Errorf("status bar = %q, want the thread indicator", statusbarText(a))
	}

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // thread → channel
	if a.focusedPanel != PanelMessages || !a.threadVisible {
		t.Fatalf("shift+tab: focus=%v threadVisible=%v, want channel focused, thread still open", a.focusedPanel, a.threadVisible)
	}
	assertFront(t, a, true, false)
	if !strings.Contains(statusbarText(a), "> Thread") {
		t.Errorf("status bar = %q, want the thread indicator while the thread is behind", statusbarText(a))
	}

	updateAndRender(t, a, keyCode(tea.KeyTab)) // channel → thread
	if a.focusedPanel != PanelThread {
		t.Fatalf("tab: focus=%v, want PanelThread", a.focusedPanel)
	}
	assertFront(t, a, false, true)

	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread → sidebar
	if a.focusedPanel != PanelSidebar {
		t.Fatalf("tab: focus=%v, want PanelSidebar", a.focusedPanel)
	}
	assertFront(t, a, false, true) // the last content pane stays in front

	updateAndRender(t, a, keyCode(tea.KeyEscape))
	if a.threadVisible {
		t.Fatal("Esc did not close the thread")
	}
	assertFront(t, a, true, false)
}

func TestStacked_WideTerminalShowsBoth(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, true, true)
	if w := a.layout.threadEnd - a.layout.msgEnd; w != 82 {
		t.Errorf("thread band = %d cols, want 82 (80 + border)", w)
	}
}

// Credit: the Enter → fetch → replies-land shape is mkozjak's, from
// PR #246. Before this change the reply was dropped because the render
// between Enter and the fetch result had auto-hidden the pane (#223).
func TestStacked_RepliesLandAt120(t *testing.T) {
	a := stackedApp(t, 120)
	selected, _ := a.messagepane.SelectedMessage()
	reply := messages.MessageItem{TS: "6.0", ThreadTS: selected.TS, UserName: "bob", Text: "a narrow reply"}
	a.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg {
		return ThreadRepliesLoadedMsg{ThreadTS: selected.TS, Replies: []messages.MessageItem{reply}}
	})

	_, cmd := a.Update(keyCode(tea.KeyEnter))
	_ = a.View()
	for _, msg := range drainBatch(cmd) {
		if msg != nil {
			updateAndRender(t, a, msg)
		}
	}
	if got := a.threadPanel.Replies(); len(got) != 1 || got[0].TS != reply.TS {
		t.Fatalf("thread replies = %+v, want the fetched reply", got)
	}
	if !strings.Contains(ansi.Strip(a.View().Content), "a narrow reply") {
		t.Error("the fetched reply is not on screen")
	}
}

func TestStacked_ThreadsViewKeepsTheListInFront(t *testing.T) {
	sums := []cache.ThreadSummary{{
		ChannelID: "C1", ChannelName: "general", ChannelType: "channel",
		ThreadTS: "50.0", ParentTS: "50.0", ParentUserID: "U1",
		ParentText: "hello", ReplyCount: 1,
	}}
	a := stackedApp(t, 120, withThreadsView(sums))

	updateAndRender(t, a, ThreadsViewActivatedMsg{})
	if a.view != ViewThreads || !a.threadVisible || a.focusedPanel != PanelMessages {
		t.Fatalf("activation: view=%v threadVisible=%v focus=%v", a.view, a.threadVisible, a.focusedPanel)
	}
	assertFront(t, a, true, false)

	updateAndRender(t, a, keyCode(tea.KeyEnter))
	if a.focusedPanel != PanelThread {
		t.Fatalf("Enter: focus=%v, want PanelThread", a.focusedPanel)
	}
	assertFront(t, a, false, true)

	updateAndRender(t, a, keyCode(tea.KeyEscape))
	if a.threadVisible || a.view != ViewThreads {
		t.Fatalf("Esc: threadVisible=%v view=%v, want closed, still in the Threads view", a.threadVisible, a.view)
	}
	assertFront(t, a, true, false)
}

func TestStacked_ViewDoesNotMutateState(t *testing.T) {
	for _, w := range []int{80, 120, 200} {
		a := stackedApp(t, w)
		updateAndRender(t, a, keyCode(tea.KeyEnter))
		type snap struct {
			visible bool
			focus   Panel
			front   Panel
		}
		before := snap{a.threadVisible, a.focusedPanel, a.stackFront}
		_ = a.View()
		_ = a.View()
		if after := (snap{a.threadVisible, a.focusedPanel, a.stackFront}); after != before {
			t.Errorf("width %d: View changed state %+v → %+v", w, before, after)
		}
	}
}

// Review focus 1.
func TestStacked_ResizeAcrossThreshold(t *testing.T) {
	a := stackedApp(t, 200)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift))
	assertFront(t, a, true, true)

	updateAndRender(t, a, tea.WindowSizeMsg{Width: 120, Height: 30})
	if !a.threadVisible {
		t.Fatal("narrowing closed the thread")
	}
	assertFront(t, a, true, false)

	updateAndRender(t, a, tea.WindowSizeMsg{Width: 200, Height: 30})
	assertFront(t, a, true, true)
}

// Review focus 2.
func TestStacked_ChannelSwitchClosesThread(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, false, true)

	updateAndRender(t, a, ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})
	if a.threadVisible {
		t.Fatal("switching channel left the thread open")
	}
	assertFront(t, a, true, false)
}

// Review focus 3.
func TestStacked_SidebarToggleUnstacks(t *testing.T) {
	a := stackedApp(t, 140)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, false, true)

	a.ToggleSidebar()
	_ = a.View()
	assertFront(t, a, true, true)
}

// Review focus 4.
func TestStacked_InsertModeKeepsThreadInFront(t *testing.T) {
	a := stackedApp(t, 80)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyPress('i'))
	if a.mode != ModeInsert || a.focusedPanel != PanelThread {
		t.Fatalf("i: mode=%v focus=%v, want insert in the thread", a.mode, a.focusedPanel)
	}
	assertFront(t, a, false, true)
}

// Review focus 5.
func TestStacked_TinyTerminalsRender(t *testing.T) {
	for w := 30; w <= 60; w++ {
		a := stackedApp(t, w)
		updateAndRender(t, a, keyCode(tea.KeyEnter))
		if !a.threadVisible || a.layout.threadEnd <= a.layout.msgEnd {
			t.Fatalf("width %d: thread not drawn", w)
		}
	}
}
```

- [ ] **Step 4: App state, `computeFrame`, `Update` wrapper, `View`**

In `internal/ui/app.go`, add after `threadVisible  bool` in the `App` struct:

```go
	// stackFront is the content pane (PanelMessages or PanelThread)
	// that last had focus. Recorded by Update, read by threadInFront.
	stackFront Panel
```

Rename `func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {` to `func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {`, and insert directly above it:

```go
// Update is the bubbletea entry point: the reducer chain in update,
// then one piece of bookkeeping that must see the result of every
// message — which content pane last had focus (see threadInFront).
// Recorded here once rather than at the ~30 sites that set
// focusedPanel.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.update(msg)
	if a.focusedPanel == PanelMessages || a.focusedPanel == PanelThread {
		a.stackFront = a.focusedPanel
	}
	return m, cmd
}

// threadInFront reports whether the thread is the pane drawn when the
// layout is too narrow for both (panelLayout.Compute). Focus decides;
// with focus elsewhere (the sidebar), the content pane that last had
// focus stays in front.
func (a *App) threadInFront() bool {
	if !a.threadVisible {
		return false
	}
	switch a.focusedPanel {
	case PanelThread:
		return true
	case PanelMessages:
		return false
	}
	return a.stackFront == PanelThread
}

// computeFrame resolves this frame's layout from the App's state and
// stores the hit-test bands. The one place View's layout inputs are
// assembled; tests call it instead of repeating Compute's arguments.
func (a *App) computeFrame() panelLayoutFrame {
	return a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(),
		a.sidebarVisible, a.threadVisible, a.threadInFront())
}
```

In `View`, replace

```go
	// Resolve per-pane widths/borders. Compute stores horizontal bands
	// for subsequent mouse hit-testing (panelAt) and surfaces a
	// ThreadAutoHidden flag when the available width can't fit the
	// thread pane at its minimum.
	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	if frame.ThreadAutoHidden {
		a.threadVisible = false
		if a.focusedPanel == PanelThread {
			a.focusedPanel = PanelMessages
		}
	}
```

with

```go
	// Resolve per-pane widths/borders. Compute stores horizontal bands
	// for subsequent mouse hit-testing (panelAt). When the thread and
	// channel stack, the pane behind has zero width and is not drawn.
	// View reads App state here; it never writes it.
	frame := a.computeFrame()
```

and replace

```go
	if s := a.renderWindowsRegion(frame, themeVer, previewActive); s != "" {
		panels = append(panels, s)
	}
```

with

```go
	if frame.MsgWidth > 0 {
		if s := a.renderWindowsRegion(frame, themeVer, previewActive); s != "" {
			panels = append(panels, s)
		}
	}
```

In `collectSixelPlacements`, change `if a.view != ViewChannels {` to `if a.view != ViewChannels || frame.MsgWidth == 0 {`.

In `internal/ui/windows.go` `windowBounds`, replace the `Compute` call with `frame := a.computeFrame()` (the thread-in-front case is Task 4).

Mechanically update the five test files:

```bash
sed -i 's/a\.layout\.Compute(a\.width, a\.height, a\.workspaceRail\.Width(), a\.sidebar\.Width(), a\.sidebarVisible, a\.threadVisible)/a.computeFrame()/' \
  internal/ui/winmodels_test.go internal/ui/view_window_region_test.go internal/ui/sixelpaint_test.go \
  internal/ui/view_composite_test.go internal/ui/view_messages_border_test.go
grep -rn 'layout.Compute(' internal/ui --include='*_test.go'
```

Expected grep output: only `golden_test.go` (fixed in Step 5) and `panellayout_test.go`.

In `view_composite_test.go` `buildViewPanels` (it mirrors `View`), wrap the messages-region append the same way as `View`:

```go
	if frame.MsgWidth > 0 {
		if s := a.renderMessagesRegion(frame, themeVer, false); s != "" {
			panels = append(panels, s)
		}
	}
```

- [ ] **Step 5: Replace the auto-hide golden guards**

The test package does not compile yet: `golden_test.go` still reads `ThreadAutoHidden` and calls `Compute` with six arguments. This step removes both.

In `internal/ui/golden_test.go`:

1. In `goldenScenario`, replace the `autoHidesThread` field and its comment with:

```go
	// layout is the arrangement this scenario must render, asserted
	// against the built App by TestGolden_ScenarioLayouts. It keeps the
	// guard the old auto-hide checks provided: a golden whose name
	// promises a thread cannot silently render none.
	layout goldenLayout
```

and add after the struct:

```go
// goldenLayout names the four arrangements of the channel and thread
// panes. goldenNoThread is the zero value.
type goldenLayout string

const (
	goldenNoThread       goldenLayout = ""
	goldenSideBySide     goldenLayout = "side-by-side"
	goldenStackedThread  goldenLayout = "stacked, thread in front"
	goldenStackedChannel goldenLayout = "stacked, channel in front"
)

// frameLayout classifies what a's current state renders.
func frameLayout(a *App) goldenLayout {
	f := a.computeFrame()
	switch {
	case !a.threadVisible:
		return goldenNoThread
	case f.MsgWidth > 0 && f.ThreadWidth > 0:
		return goldenSideBySide
	case f.ThreadWidth > 0:
		return goldenStackedThread
	}
	return goldenStackedChannel
}
```

2. Delete `goldenThreadMinWidth` and its doc comment. In the `goldenThreadApp` and `goldenThreadScenario` doc comments, delete the sentences that cite `goldenThreadMinWidth`, auto-hide, or `app.go:2726-2731`/`app.go:2747` line references.

3. Replace the `thread_open`, `wide` and `narrow` scenario entries with:

```go
		{
			// The default 120-column terminal, thread focused: the
			// thread takes over the channel pane's space (#223).
			name: "thread_open", w: 120, h: 30, layout: goldenStackedThread,
			build: func(t *testing.T) *App {
				a := goldenThreadApp(t, 120, 30)
				a.focusedPanel = PanelThread
				_ = a.View()
				return a
			},
		},
		{
			// Wide enough for both: the thread is lifted to 80 columns.
			name: "wide", w: 200, h: 50, layout: goldenSideBySide,
			build: func(t *testing.T) *App { return goldenThreadScenario(t, 200, 50) },
		},
		{
			// 80x24 with the thread focused: a 40-column stacked
			// thread, narrow enough that the breadcrumb drops its
			// author and hint.
			name: "narrow", w: 80, h: 24, layout: goldenStackedThread,
			build: func(t *testing.T) *App {
				a := goldenThreadApp(t, 80, 24)
				a.focusedPanel = PanelThread
				_ = a.View()
				return a
			},
		},
```

4. Delete `TestGolden_NarrowAutoHidesThreadPane` and `TestGolden_ThreadScenariosAreWideEnough` (with their doc comments), and add in their place:

```go
// TestGolden_ScenarioLayouts pins each scenario's pane arrangement
// against the real layout code, because a golden cannot say what it
// meant to show: a two-pane frame and a three-pane frame are equally
// well-formed files.
//
// The "declares nothing" check runs in the test body, not a subtest, so
// an unanchored -run filter selecting this test through the TestGolden
// prefix cannot empty it.
func TestGolden_ScenarioLayouts(t *testing.T) {
	declared := map[goldenLayout]bool{}
	for _, sc := range goldenScenarios() {
		declared[sc.layout] = true
	}
	for _, l := range []goldenLayout{goldenSideBySide, goldenStackedThread} {
		if !declared[l] {
			t.Fatalf("no scenario declares %q; that arrangement is pinned by nothing", l)
		}
	}
	for _, sc := range goldenScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			a := sc.build(t)
			if !a.threadPanel.IsEmpty() && !a.threadVisible {
				t.Fatal("scenario loaded a thread that is not visible")
			}
			if got := frameLayout(a); got != sc.layout {
				t.Errorf("%dx%d renders %q, scenario declares %q", sc.w, sc.h, got, sc.layout)
			}
		})
	}
}
```

5. In `TestGolden_ScenariosArePairwiseDistinct`'s doc comment, replace `(see goldenThreadMinWidth)` with `(the old auto-hide, since removed)`.

- [ ] **Step 6: Run the new tests**

Run: `go vet ./internal/ui/ && go test ./internal/ui -run 'TestGolden_ScenarioLayouts|TestPanelLayoutCompute|TestStacked_|TestThreadBreadcrumb' -count=1`
Expected: PASS. (`TestGolden` itself still fails until Step 7 re-blesses.)

- [ ] **Step 7: Re-bless the three thread goldens and read them**

Run: `go test ./internal/ui -run 'TestGolden$' -update -count=1`, then:

```bash
git status --short internal/ui/testdata/golden
for f in thread_open wide narrow; do
  echo "== $f"; sed 's/\x1b\[[0-9;:]*m//g' internal/ui/testdata/golden/$f.ansi | head -8
done
```

Expected: only `thread_open.ansi`, `wide.ansi`, `narrow.ansi` modified. `thread_open`: rail, sidebar, then one 80-col bordered thread pane whose first row reads `# general › Thread from carol · 2 replies` … `esc close`; no channel messages visible. `wide`: channel pane 78 wide beside an 80-wide thread with the same breadcrumb. `narrow`: rail, sidebar, one 40-col thread whose first row reads `# general › Thread · 2 replies`. Anything else: stop and investigate.

- [ ] **Step 8: Run the whole package and triage**

Run: `go test ./internal/ui/... -count=1`

Existing tests that fail here were written against the removed auto-hide or the old widths. Triage each by these rules, and list every edited test in the commit message:

1. The test asserts the auto-hide itself (thread hidden, or focus dropped to the channel, after a `View()` below 124 cols): rewrite the assertion to the stacked result (`threadVisible` stays true; the channel band is empty when the thread is focused).
2. The test is about something else and only incidentally opened a thread at a width that used to auto-hide, and it needs the channel pane drawn: widen it with `withSize(200, h)` so both panes show. Do not change what it asserts.
3. The test asserts old thread widths (35% / ≥30): update the expected numbers to the new table in `panellayout_test.go`.
4. Anything else: stop and report it — it may be a real regression.

Never loosen an assertion (e.g. `==` to `Contains`) to make it pass.

Expected after triage: PASS.

- [ ] **Step 9: Document the test helpers**

In `AGENTS.md`'s "Test helpers" table, add after the `newGoldenApp` row:

```
| Drive a message through the real `Update` chain and render one frame | `updateAndRender(t, a, msg)` (`internal/ui/thread_breadcrumb_test.go`) |
| An App at a given width, resized via `WindowSizeMsg`, channel focused, for thread-layout tests | `stackedApp(t, w, extra...)` (`internal/ui/thread_stacked_test.go`) |
| Assert which of channel / thread the last frame drew | `assertFront(t, a, wantChannel, wantThread)` (same file) |
```

- [ ] **Step 10: Verify and commit**

```bash
go build ./... && go vet ./... && gofmt -l . && go test ./internal/ui/... -count=1
git add -A internal/ui AGENTS.md
git commit -m "feat(ui): stack thread and channel when both cannot fit

The thread pane is at least 80 columns beside the channel; below
162 columns (130 without the sidebar) the two stack and focus
decides which is drawn. View no longer hides the thread, which
fixed nothing and dropped the reply fetch (#223, #244)."
```

- [ ] **Step 11: STOP — hands-on test drive**

Build (`go build -o /tmp/opencode/slk ./cmd/slk`) and hand over to the user with this checklist: open a thread at ~120 cols; `shift+tab` / `tab` between thread, sidebar and channel; `esc` / `q`; the Threads view with `j`/`k` and Enter; resize across 162 cols; reply with `i`. Known gap until Task 4: `ctrl+w` window commands while the thread is in front. Do not start Task 4 until the user has given feedback on the feel.

---

## Milestone 2 — input edges

### Task 4: Window commands and mouse with the thread in front

**Files:**
- Create: `internal/ui/thread_stacked_input_test.go`
- Modify: `internal/ui/windows.go:47-53`

**Interfaces:**
- Consumes: `stackedApp`, `assertFront` (Task 3), `updateAndRender` (Task 2), `pressCtrlW`, `press` (`windows_chord_test.go`).
- Produces: `windowBounds()` that sizes windows for the channel-in-front layout and does not touch `a.layout`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ui/thread_stacked_input_test.go`:

```go
package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/wintree"
)

func TestStacked_WindowSplitWithThreadInFront(t *testing.T) {
	a := stackedApp(t, 150)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	assertFront(t, a, false, true)

	bands := *a.layout
	want := wintree.Rect{W: 150 - testRailW - testSidebarW - 2, H: 29}
	if got := a.windowBounds(); got != want {
		t.Errorf("windowBounds = %+v, want %+v (the channel-in-front area the windows fill)", got, want)
	}
	if *a.layout != bands {
		t.Errorf("windowBounds overwrote the hit-test bands: %+v → %+v", bands, *a.layout)
	}

	pressCtrlW(a)
	_ = press(a, 'v')
	if a.wins.Len() != 2 {
		t.Fatalf("ctrl+w v with the thread in front: %d windows, want 2", a.wins.Len())
	}
	if a.focusedPanel != PanelMessages || !a.threadVisible {
		t.Fatalf("after split: focus=%v threadVisible=%v, want channel focused, thread open", a.focusedPanel, a.threadVisible)
	}
	_ = a.View()
	assertFront(t, a, true, false)
}

func TestStacked_MouseRoutesToThePaneInFront(t *testing.T) {
	a := stackedApp(t, 120)
	updateAndRender(t, a, keyCode(tea.KeyEnter))
	updateAndRender(t, a, keyCode(tea.KeyTab)) // thread → sidebar; thread stays in front
	assertFront(t, a, false, true)

	x, y := a.layout.sidebarEnd+5, 4
	if panel, px, py, ok := a.panelAt(x, y); !ok || panel != PanelThread || px != 4 || py != 3 {
		t.Fatalf("panelAt(%d,%d) = %v,%d,%d,%v; want PanelThread,4,3,true", x, y, panel, px, py, ok)
	}

	msgVer := a.messagepane.Version()
	updateAndRender(t, a, tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if a.messagepane.Version() != msgVer {
		t.Error("a wheel over the thread scrolled the hidden channel pane")
	}

	updateAndRender(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if a.focusedPanel != PanelThread {
		t.Fatalf("click over the thread focused %v, want PanelThread", a.focusedPanel)
	}

	updateAndRender(t, a, keyMod(tea.KeyTab, tea.ModShift)) // thread → channel
	assertFront(t, a, true, false)
	updateAndRender(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if a.focusedPanel != PanelMessages || !a.threadVisible {
		t.Fatalf("click over the channel: focus=%v threadVisible=%v, want channel focused, thread open", a.focusedPanel, a.threadVisible)
	}
}
```

Run: `go test ./internal/ui -run 'TestStacked_(WindowSplit|MouseRoutes)' -count=1`
Expected: `TestStacked_WindowSplitWithThreadInFront` FAILS (`windowBounds` W = 0 and "Not enough room"). `TestStacked_MouseRoutesToThePaneInFront` is expected to PASS — it pins the spec's claim that mouse routing needs no change. If it fails, stop and report: the spec's routing analysis is wrong and the fix needs design.

- [ ] **Step 2: Fix `windowBounds`**

Replace `windowBounds` in `internal/ui/windows.go` with:

```go
// windowBounds returns the messages-region rectangle the window tree
// subdivides. Windows are only drawn when the channel is in front, so
// the rectangle is always the channel-in-front layout, even while a
// stacked thread is in front. A scratch layout keeps the bands View
// stored for mouse hit-testing intact.
func (a *App) windowBounds() wintree.Rect {
	var scratch panelLayout
	frame := scratch.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(),
		a.sidebarVisible, a.threadVisible, false)
	return wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
}
```

Run: `go test ./internal/ui -run 'TestStacked_|Chord|Window' -count=1` — Expected: PASS.

- [ ] **Step 3: Commit**

```bash
go vet ./internal/ui/ && gofmt -l . && go test ./internal/ui/... -count=1
git add internal/ui/windows.go internal/ui/thread_stacked_input_test.go
git commit -m "fix(ui): size windows for the channel while a thread is in front"
```

---

## Milestone 3 — finish

### Task 5: Stale references, spec note, full verification

**Files:**
- Modify: any file the grep below finds
- Modify: `docs/superpowers/specs/2026-09-24-thread-stacked-layout-design.md` (Delivery section)

- [ ] **Step 1: Sweep stale references**

```bash
grep -rn -i 'auto-hid\|autohid\|ThreadAutoHidden\|goldenThreadMinWidth\|minThreadW *= *30' --include='*.go' --include='*.md' internal AGENTS.md docs/superpowers/specs/2026-09-24-*
```

Expected: no hits in code or comments except historical prose in the spec's Problem section. Rewrite any comment that describes the thread auto-hiding as current behaviour (e.g. `view_thread.go`'s visibility-gate note, `reducer_thread_live_test.go`'s "hidden thread panel" wording if it refers to auto-hide rather than a closed thread).

- [ ] **Step 2: Record the goldens deviation in the spec**

In the spec's "Delivery" section, replace milestone 3's description with:

```
3. Stale-reference sweep and full verification. (Goldens were re-blessed
   in the task whose change moved them — Tasks 1–3 of the plan — so
   every commit stays green.)
```

- [ ] **Step 3: Full verification**

```bash
go build ./...
go vet ./...
gofmt -l .
golangci-lint run
go test ./... -race -count=1
```

Expected: build/vet clean, `gofmt -l .` empty, `golangci-lint` 0 issues, all packages PASS.

- [ ] **Step 4: Commit and hand off**

```bash
git add -A
git commit -m "docs: retire auto-hide references; note golden sequencing"
```

Then use superpowers:finishing-a-development-branch. The PR description must: close #223 and #244; say it supersedes #246 and #149; credit mkozjak (the Enter-through-`Update` test shape and the 120-column `thread_open` golden) and piotrsynowiec (the stale `> Thread` diagnosis); note that the side-by-side thread is now 80 columns on wide screens.
