package searchresults

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func items() []Item {
	return []Item{
		{ChannelID: "C1", ChannelName: "general", UserName: "grant", TS: "1.0", Text: "deploy fine"},
		{ChannelID: "C2", ChannelName: "ops", UserName: "sam", TS: "2.0", Text: "deploy bad", ThreadTS: "1.5"},
	}
}

// manyItems builds n distinct results for scroll-window tests.
func manyItems(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{
			ChannelID:   fmt.Sprintf("C%d", i),
			ChannelName: "general",
			UserName:    "u",
			TS:          fmt.Sprintf("%d.0", i),
			Text:        fmt.Sprintf("msg %d", i),
		}
	}
	return out
}

// submitQuery types q and presses enter, leaving the model in stateLoading.
func submitQuery(m *Model, q string) {
	for _, r := range q {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
}

func TestOpenStartsAtInput(t *testing.T) {
	m := New()
	m.Open()
	if !m.IsVisible() || m.Query() != "" {
		t.Fatal("open state wrong")
	}
}

func TestTypingAndSubmit(t *testing.T) {
	m := New()
	m.Open()
	for _, r := range "deploy" {
		m.HandleKey(string(r))
	}
	if m.Query() != "deploy" {
		t.Fatalf("query = %q", m.Query())
	}
	if act := m.HandleKey("enter"); act != ActionSubmit {
		t.Fatalf("enter on query = %v, want ActionSubmit", act)
	}
	if !m.Loading() {
		t.Fatal("not in loading state after submit")
	}
}

func TestEmptyQuerySubmitIsNoop(t *testing.T) {
	m := New()
	m.Open()
	if act := m.HandleKey("enter"); act != ActionNone {
		t.Fatalf("enter on empty query = %v", act)
	}
}

func TestResultsNavigationAndSelect(t *testing.T) {
	m := New()
	m.Open()
	for _, r := range "deploy" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	m.SetResults(items(), 2)
	if m.Loading() {
		t.Fatal("still loading after SetResults")
	}

	m.HandleKey("down")
	if act := m.HandleKey("enter"); act != ActionSelect {
		t.Fatalf("enter on result = %v, want ActionSelect", act)
	}
	sel, ok := m.Selected()
	if !ok || sel.ChannelID != "C2" || sel.ThreadTS != "1.5" {
		t.Fatalf("selected = %+v ok=%v", sel, ok)
	}
}

func TestErrorStateKeepsQuery(t *testing.T) {
	m := New()
	m.Open()
	for _, r := range "x" {
		m.HandleKey(string(r))
	}
	m.HandleKey("enter")
	m.SetError("rate limited")
	if m.Query() != "x" {
		t.Fatal("query lost on error")
	}
	// retry works
	if act := m.HandleKey("enter"); act != ActionSubmit {
		t.Fatalf("retry = %v", act)
	}
}

func TestEscCloses(t *testing.T) {
	m := New()
	m.Open()
	if act := m.HandleKey("esc"); act != ActionClose {
		t.Fatalf("esc = %v", act)
	}
	if m.IsVisible() {
		t.Fatal("still visible")
	}
}

func TestNewQueryTypingReplacesResults(t *testing.T) {
	m := New()
	m.Open()
	m.HandleKey("d")
	m.HandleKey("enter")
	m.SetResults(items(), 2)
	m.HandleKey("x") // typing returns focus to the input
	if m.Query() != "dx" {
		t.Fatalf("query = %q", m.Query())
	}
}

func TestSpaceKeyAppendsSpace(t *testing.T) {
	// bubbletea v2's Key.String() renders a literal space as "space";
	// multi-term queries must map it back to ' '.
	m := New()
	m.Open()
	m.HandleKey("a")
	m.HandleKey("space")
	m.HandleKey("b")
	if m.Query() != "a b" {
		t.Fatalf("query = %q", m.Query())
	}
}

func TestViewSmoke(t *testing.T) {
	m := New()
	if got := m.View(80, 24); got != "" {
		t.Fatalf("hidden View = %q, want empty", got)
	}
	m.Open()
	for _, r := range "deploy" {
		m.HandleKey(string(r))
	}
	out := m.View(80, 24)
	if out == "" || !strings.Contains(out, "deploy") {
		t.Fatal("View must render the query")
	}

	m.HandleKey("enter")
	if out := m.View(80, 24); !strings.Contains(out, "Searching") {
		t.Fatal("loading View must show spinner line")
	}

	m.SetResults(items(), 5)
	out = m.View(80, 24)
	if !strings.Contains(out, "general") || !strings.Contains(out, "grant") {
		t.Fatal("results View must show channel and author")
	}
	if !strings.Contains(out, "showing 2 of 5") {
		t.Fatal("results View must show footer when total > len(items)")
	}

	// Re-submit so the error lands on an in-flight search (SetError is a
	// no-op outside stateLoading).
	m.HandleKey("x")
	m.HandleKey("enter")
	m.SetError("rate limited")
	if out := m.View(80, 24); !strings.Contains(out, "rate limited") {
		t.Fatal("error View must show error message")
	}
}

func TestEnterWhileLoadingIsNoop(t *testing.T) {
	m := New()
	m.Open()
	for _, r := range "deploy" {
		m.HandleKey(string(r))
	}
	if act := m.HandleKey("enter"); act != ActionSubmit {
		t.Fatalf("first enter = %v, want ActionSubmit", act)
	}
	if act := m.HandleKey("enter"); act != ActionNone {
		t.Fatalf("enter while loading = %v, want ActionNone", act)
	}
	if !m.Loading() {
		t.Fatal("loading state must survive a re-pressed enter")
	}
}

// ActionSelect implies Selected() reports a hit: with zero results,
// enter is a no-op rather than a select of nothing.
// handleWorkspaceSearchMode relies on this and does not re-check ok.
func TestEnterOnEmptyResultsIsNoop(t *testing.T) {
	m := New()
	m.Open()
	for _, r := range "deploy" {
		m.HandleKey(string(r))
	}
	if act := m.HandleKey("enter"); act != ActionSubmit {
		t.Fatalf("first enter = %v, want ActionSubmit", act)
	}
	m.SetResults(nil, 0)
	if m.Loading() {
		t.Fatal("precondition: still loading after SetResults")
	}
	if _, ok := m.Selected(); ok {
		t.Fatal("precondition: Selected reports a hit with zero results")
	}
	if act := m.HandleKey("enter"); act != ActionNone {
		t.Fatalf("enter on empty results = %v, want ActionNone", act)
	}
}

func TestCtrlPNNavigation(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(3), 3)

	m.HandleKey("ctrl+n")
	m.HandleKey("ctrl+n")
	if m.selected != 2 {
		t.Fatalf("after 2x ctrl+n selected = %d, want 2", m.selected)
	}
	m.HandleKey("ctrl+p")
	if m.selected != 1 {
		t.Fatalf("after ctrl+p selected = %d, want 1", m.selected)
	}
}

// gutterRune returns the rune in the scrollbar gutter column (just inside
// the right padding/border) of the stripped, full-width line.
func gutterRune(line string) rune {
	r := []rune(line)
	return r[len(r)-3]
}

func TestScrollbarAppearsOnOverflow(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(50), 50)

	// 30 rows: the window fills up to 70% of the terminal height (see
	// visibleRowCap); 50 items overflow it, so the scrollbar shows.
	cap := m.visibleRowCap(30)
	if cap < 2 {
		t.Fatalf("visibleRowCap(30) = %d, want >= 2 for this test", cap)
	}
	lines := strings.Split(ansi.Strip(m.View(80, 30)), "\n")
	// The gutter spans all rowLines lines of every visible row,
	// separator included.
	rows := lines[listTopOffset : listTopOffset+rowLines*cap]
	var thumbs, tracks int
	for i, row := range rows {
		switch gutterRune(row) {
		case '█':
			thumbs++
		case '│':
			tracks++
		default:
			t.Fatalf("line %d gutter = %q, want thumb or track", i, gutterRune(row))
		}
	}
	if thumbs == 0 || tracks == 0 {
		t.Fatalf("want proportional thumb and track, got %d thumbs / %d tracks", thumbs, tracks)
	}
	// Selection at the top: the thumb hugs the top of the gutter.
	if gutterRune(rows[0]) != '█' {
		t.Error("thumb should start at the top when the window is at the start")
	}
	// All lines of a row always share the same gutter rune.
	for i := 0; i < len(rows); i += rowLines {
		for j := 1; j < rowLines; j++ {
			if gutterRune(rows[i]) != gutterRune(rows[i+j]) {
				t.Errorf("row %d gutter split across lines 0 and %d: %q vs %q",
					i/rowLines, j, gutterRune(rows[i]), gutterRune(rows[i+j]))
			}
		}
	}
}

func TestNoScrollbarWhenListFits(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(3), 3)

	// 30-row terminal: visibleRowCap(30) = 3, so all 3 items fit.
	lines := strings.Split(ansi.Strip(m.View(80, 30)), "\n")
	for i, row := range lines[listTopOffset : listTopOffset+rowLines*3] {
		if g := gutterRune(row); g != ' ' {
			t.Fatalf("line %d gutter = %q, want blank (no scrollbar)", i, g)
		}
	}
}

func TestSnippetSanitization(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults([]Item{
		{ChannelID: "C1", ChannelName: "general", UserName: "u", TS: "1.0",
			Text: "a\tb\rc\nd\x07e"},
	}, 1)

	plain := ansi.Strip(m.View(80, 24))
	if !strings.Contains(plain, "a bc de") {
		t.Fatalf("snippet not sanitized; view:\n%s", plain)
	}
}

// TestHeaderFieldsSanitized verifies control characters in the channel
// or author name (hostile or odd server data) can't wrap the box or
// desync the width math: header fields get the same flattenText
// treatment as snippets.
func TestHeaderFieldsSanitized(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults([]Item{
		{ChannelID: "C1", ChannelName: "gen\neral", UserName: "gr\tant", TS: "1.0", Text: "hi"},
	}, 1)

	box := m.renderBox(80, 24)
	w, h := m.BoxSize(80, 24)
	if gw, gh := lipgloss.Width(box), lipgloss.Height(box); gw != w || gh != h {
		t.Errorf("rendered %dx%d, BoxSize %dx%d (control chars in header?)", gw, gh, w, h)
	}
	plain := ansi.Strip(box)
	if !strings.Contains(plain, "#gen eral") || !strings.Contains(plain, "gr ant") {
		t.Errorf("header fields not flattened; view:\n%s", plain)
	}
}

func TestDMRowsRenderAtSigil(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults([]Item{
		{ChannelID: "D1", ChannelName: "ayush", UserName: "ayush", TS: "1.0", Text: "hi", IsDM: true},
		{ChannelID: "C1", ChannelName: "general", UserName: "grant", TS: "2.0", Text: "yo"},
	}, 2)

	plain := ansi.Strip(m.View(80, 24))
	if !strings.Contains(plain, "@ayush") {
		t.Errorf("DM row must render @name; view:\n%s", plain)
	}
	if strings.Contains(plain, "#ayush") {
		t.Errorf("DM row must not render #name; view:\n%s", plain)
	}
	if !strings.Contains(plain, "#general") {
		t.Errorf("channel row must keep #name; view:\n%s", plain)
	}
}

func TestLongQueryDoesNotWrapBox(t *testing.T) {
	m := New()
	m.Open()
	for i := 0; i < 200; i++ {
		m.HandleKey("x")
	}
	m.HandleKey("!") // distinctive tail rune

	box := m.renderBox(80, 24)
	w, h := m.BoxSize(80, 24)
	if gw := lipgloss.Width(box); gw != w {
		t.Errorf("rendered width = %d, BoxSize width = %d", gw, w)
	}
	if gh := lipgloss.Height(box); gh != h {
		t.Errorf("rendered height = %d, BoxSize height = %d (input line wrapped?)", gh, h)
	}
	// The tail of the query and the cursor stay visible.
	if plain := ansi.Strip(box); !strings.Contains(plain, "x!█") {
		t.Error("input truncation must keep the query tail and cursor visible")
	}
}

func TestErrorNewlinesFlattened(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetError("line1\nline2")

	box := m.renderBox(80, 24)
	w, h := m.BoxSize(80, 24)
	if gw, gh := lipgloss.Width(box), lipgloss.Height(box); gw != w || gh != h {
		t.Errorf("rendered %dx%d, BoxSize %dx%d", gw, gh, w, h)
	}
	if plain := ansi.Strip(box); !strings.Contains(plain, "line1 line2") {
		t.Error("error text must be flattened to one line")
	}
}

func TestSetResultsIgnoredWhenNotLoading(t *testing.T) {
	m := New()
	m.Open()
	// No search in flight: a stale async result must not inject state.
	m.SetResults(items(), 2)
	if m.st != stateInput {
		t.Fatalf("stale SetResults moved state to %v", m.st)
	}
	m.SetError("stale")
	if m.st != stateInput || m.errMsg != "" {
		t.Fatal("stale SetError must be ignored outside stateLoading")
	}

	// After results land, a second late SetResults is also ignored.
	submitQuery(&m, "deploy")
	m.SetResults(items(), 2)
	m.SetResults(manyItems(5), 5)
	if len(m.items) != 2 {
		t.Fatalf("late SetResults overwrote items: len=%d, want 2", len(m.items))
	}
}

func TestScrollWindowEdges(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(15), 15)

	// 30 rows: visibleRowCap(30) = 3 (70% budget 21, chrome 7, 4-line
	// rows). Walk to the bottom: window pins to the last 3 items.
	for i := 0; i < 20; i++ {
		m.HandleKey("down")
	}
	if m.selected != 14 {
		t.Fatalf("selected = %d, want 14", m.selected)
	}
	if start, end := m.visibleWindow(30); start != 12 || end != 15 {
		t.Fatalf("bottom window = [%d,%d), want [12,15)", start, end)
	}

	// Walk back to the top.
	for i := 0; i < 20; i++ {
		m.HandleKey("up")
	}
	if m.selected != 0 {
		t.Fatalf("selected = %d, want 0", m.selected)
	}
	if start, end := m.visibleWindow(30); start != 0 || end != 3 {
		t.Fatalf("top window = [%d,%d), want [0,3)", start, end)
	}
}

// resultLines returns the stripped rendered list lines for the current
// visible window (rowLines lines per result row). The 30-row terminal
// gives a 3-row window (visibleRowCap(30) = 3 at rowLines = 4).
func resultLines(m Model, n int) []string {
	lines := strings.Split(ansi.Strip(m.View(80, 30)), "\n")
	return lines[listTopOffset : listTopOffset+n]
}

// blankLine reports whether a rendered box line has no content between
// the borders ("│ ... │") once padding is trimmed.
func blankLine(line string) bool {
	return strings.Trim(line, " │") == ""
}

func TestFourLineRowsWithSeparator(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	long := strings.Repeat("lorem ipsum ", 30) + "ENDMARK"
	m.SetResults([]Item{
		{ChannelID: "C1", ChannelName: "general", UserName: "grant", TS: "1.0", Text: long},
		{ChannelID: "C2", ChannelName: "ops", UserName: "sam", TS: "2.0", Text: "short"},
	}, 2)

	lines := resultLines(m, 8)

	// Row 0 line 1: metadata only — channel, author, timestamp, and
	// crucially no snippet text.
	if !strings.Contains(lines[0], "#general") || !strings.Contains(lines[0], "grant") {
		t.Errorf("line 1 of row 0 = %q, want metadata header", lines[0])
	}
	if strings.Contains(lines[0], "lorem") {
		t.Errorf("line 1 of row 0 = %q, must not carry snippet text", lines[0])
	}
	// Lines 2-3: the snippet, indented 2 spaces. Row 0 is selected, so
	// the ▌ indicator precedes the indent.
	if !strings.Contains(lines[1], "▌  lorem") {
		t.Errorf("line 2 of row 0 = %q, want 2-space-indented snippet", lines[1])
	}
	if !strings.Contains(lines[2], "lorem") {
		t.Errorf("line 3 of row 0 = %q, want snippet continuation", lines[2])
	}
	if !strings.Contains(lines[2], "…") {
		t.Errorf("line 3 of row 0 = %q, want … overflow marker", lines[2])
	}
	if strings.Contains(lines[2], "ENDMARK") {
		t.Errorf("line 3 of row 0 should be truncated before the snippet tail")
	}
	// Line 4 of row 0 is the blank separator.
	if !blankLine(lines[3]) {
		t.Errorf("line 4 of row 0 = %q, want blank separator", lines[3])
	}

	// Row 1: metadata line, then the short snippet fits on line 2
	// (indented, unselected: indicator column + 2-space indent between
	// border and text); line 3 is blank.
	if !strings.Contains(lines[4], "#ops") || !strings.Contains(lines[4], "sam") {
		t.Errorf("line 1 of row 1 = %q, want metadata header", lines[4])
	}
	if strings.Contains(lines[4], "short") {
		t.Errorf("line 1 of row 1 = %q, must not carry snippet text", lines[4])
	}
	if !strings.Contains(lines[5], "   short") {
		t.Errorf("line 2 of row 1 = %q, want indented snippet", lines[5])
	}
	if !blankLine(lines[6]) {
		t.Errorf("line 3 of row 1 = %q, want blank continuation", lines[6])
	}
	// Trailing separator after the last row is fine (and expected).
	if !blankLine(lines[7]) {
		t.Errorf("line 4 of row 1 = %q, want blank separator", lines[7])
	}
}

func TestContinuationNoMidRunSplitOfWideRunes(t *testing.T) {
	// A snippet of wide runes must split at a cell boundary without
	// duplicating or dropping content between line 1 and line 2.
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	wide := strings.Repeat("日本語テキスト", 20)
	m.SetResults([]Item{
		{ChannelID: "C1", ChannelName: "general", UserName: "grant", TS: "1.0", Text: wide},
	}, 1)

	lines := resultLines(m, rowLines)
	// Every full screen line (borders included) is exactly the box wide;
	// a mid-rune split or unclipped overflow would change that.
	for i, l := range lines {
		if w := lipgloss.Width(l); w != boxWidth(80) {
			t.Errorf("line %d width = %d, want boxWidth(80) = %d", i, w, boxWidth(80))
		}
	}
	// The snippet occupies lines 2-3 of the block (indices 1-2); the
	// metadata line carries none of it.
	snipLines := lines[1:3]

	// Extract the snippet portion of each line: the snippet is the only
	// source of these wide runes, so filtering to the snippet alphabet
	// drops the header, borders, padding, and the "…" overflow marker.
	// A mid-rune byte split would also surface here: the resulting
	// invalid UTF-8 decodes to U+FFFD, which is not in the alphabet, so
	// the broken rune goes missing and head+tail stops being a prefix.
	wideSet := map[rune]bool{}
	for _, r := range "日本語テキスト" {
		wideSet[r] = true
	}
	extract := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if wideSet[r] {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	head, tail := extract(snipLines[0]), extract(snipLines[1])
	if got := extract(lines[0]); got != "" {
		t.Errorf("metadata line carries snippet runes: %q", got)
	}
	if head == "" || tail == "" {
		t.Fatalf("expected snippet content on both lines; head=%q tail=%q", head, tail)
	}
	// Continuity: line1's snippet followed by line2's must reproduce the
	// start of the original text — no dropped, duplicated, or reordered
	// runes across the split.
	if got := head + tail; !strings.HasPrefix(wide, got) {
		t.Errorf("head+tail is not a prefix of the original snippet (content dropped/duplicated at the split):\nhead=%q\ntail=%q", head, tail)
	}
}

func TestSelectedIndicatorOnContentLinesOnly(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(3), 3)
	m.HandleKey("down") // select row 1

	// The ▌ indicator marks the metadata and snippet lines (1-3) of the
	// selected row but never its blank separator line.
	lines := resultLines(m, 12)
	for i, want := range []bool{
		false, false, false, false, // row 0
		true, true, true, false, // row 1 (selected): content yes, separator no
		false, false, false, false, // row 2
	} {
		has := strings.HasPrefix(strings.TrimLeft(lines[i], " "), "▌") ||
			strings.Contains(lines[i], "▌")
		if has != want {
			t.Errorf("line %d indicator = %v, want %v (%q)", i, has, want, lines[i])
		}
	}
}

// TestSeventyPercentSizing locks the modal to 70% of the terminal in
// both dimensions: a 200x60 terminal gets a 140-wide box whose rows
// grow to fill the 42-line (70% of 60) height budget.
func TestSeventyPercentSizing(t *testing.T) {
	m := New()
	m.Open()
	submitQuery(&m, "deploy")
	m.SetResults(manyItems(50), 80) // footer: "showing 50 of 80"

	if w := boxWidth(200); w != 140 {
		t.Errorf("boxWidth(200) = %d, want 140 (70%%, no 100-col ceiling)", w)
	}
	if w := boxWidth(50); w != 40 {
		t.Errorf("boxWidth(50) = %d, want the 40-col floor", w)
	}

	w, h := m.BoxSize(200, 60)
	if w != 140 {
		t.Errorf("BoxSize width = %d, want 140", w)
	}
	// Height budget is 42 (70% of 60); chrome is 8 (incl. footer), so
	// 8 rows of rowLines(4) lines fit: 8*4 + 8 = 40.
	if h != 40 {
		t.Errorf("BoxSize height = %d, want 40 (fills the 70%% budget)", h)
	}
	box := m.renderBox(200, 60)
	if gw, gh := lipgloss.Width(box), lipgloss.Height(box); gw != w || gh != h {
		t.Errorf("rendered %dx%d, BoxSize %dx%d", gw, gh, w, h)
	}
}

func TestMultiByteBackspace(t *testing.T) {
	m := New()
	m.Open()
	m.HandleKey("a")
	m.HandleKey("é")
	m.HandleKey("🙂")
	m.HandleKey("backspace")
	if m.Query() != "aé" {
		t.Fatalf("query = %q, want %q", m.Query(), "aé")
	}
	m.HandleKey("backspace")
	if m.Query() != "a" {
		t.Fatalf("query = %q, want %q", m.Query(), "a")
	}
}
