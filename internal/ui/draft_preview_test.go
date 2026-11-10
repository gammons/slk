package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

// richDraft is the plan's example of a draft worth previewing.
const richDraft = "Release plan:\n- ship **v0.28** on Tuesday\n- read [the notes](https://example.com)"

// numberedDraft is "line 1" ... "line 40", one per line.
func numberedDraft() string {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return strings.Join(lines, "\n")
}

// draftApp is an App in insert mode on C1, the channel box holding draft
// with its draft preview on.
func draftApp(t testing.TB, w, h int, draft string) *App {
	t.Helper()
	pinRenderGlobals(t)
	a := newTestApp(t, append(insertOpts(), withSize(w, h), withMode(ModeInsert))...)
	a.focusedPanel = PanelMessages
	_ = a.compose.Focus()
	a.compose.SetValue(draft)
	// SetValue leaves the box's height alone; any key through Update
	// sizes it (capped) the way typing the draft would. End is a no-op
	// here: the cursor is already at the end.
	a.compose, _ = a.compose.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	a.compose.SetPreview(true)
	return a
}

// frameRows renders a frame and returns its rows.
func frameRows(a *App) []string {
	return strings.Split(a.View().Content, "\n")
}

// previewSpan finds the draft preview in a frame: the row of its title,
// the column the pane's inside starts at, and how many rows it has. The
// preview's rows after the title all start with a blank cell (its
// two-cell pad), and so does the spacer row under it; the next row
// that doesn't (a picker's or the compose box's edge) is two below the
// preview's last row. ok is false when no preview is drawn.
func previewSpan(rows []string, width int) (top, col, n int, ok bool) {
	for i, r := range rows {
		plain := ansi.Strip(r)
		at := strings.Index(plain, "── draft preview")
		if at < 0 {
			continue
		}
		col = ansi.StringWidth(plain[:at])
		end := i + 1
		for end < len(rows) && strings.HasPrefix(ansi.Strip(ansi.Cut(rows[end], col, col+width)), " ") {
			end++
		}
		return i, col, end - 1 - i, true
	}
	return 0, 0, 0, false
}

// previewRows is the draft preview's rows in the frame, plain, cut to
// the inside of the pane; the title is the first.
func previewRows(t *testing.T, a *App) []string {
	t.Helper()
	width := a.computeFrame().MsgWidth - 2
	rows := frameRows(a)
	top, col, n, ok := previewSpan(rows, width)
	if !ok {
		t.Fatal("no draft preview in the frame")
	}
	out := make([]string, n)
	for i := range out {
		out[i] = ansi.Strip(ansi.Cut(rows[top+i], col, col+width))
	}
	return out
}

func joinedHas(rows []string, s string) bool { return strings.Contains(strings.Join(rows, "\n"), s) }

func TestDraftPreviewShowsRenderedDraft(t *testing.T) {
	a := draftApp(t, 120, 40, richDraft)
	rows := previewRows(t, a)
	for _, want := range []string{"Release plan:", "• ship v0.28 on Tuesday", "• read the notes"} {
		if !joinedHas(rows, want) {
			t.Errorf("preview lacks %q:\n%s", want, strings.Join(rows, "\n"))
		}
	}
	for _, raw := range []string{"**", "[", "]("} {
		if joinedHas(rows[1:], raw) {
			t.Errorf("preview shows raw markdown %q:\n%s", raw, strings.Join(rows, "\n"))
		}
	}
}

// A line after a list stays on its own line (Grant's draft on SLK-908
// showed "• threeand another line").
func TestDraftPreviewLineAfterList(t *testing.T) {
	draft := strings.Join([]string{"bug with rendering lists:", "", "- one", "- two", "- three", "", "and another line"}, "\n")
	a := draftApp(t, 120, 40, draft)
	var three, after = -1, -1
	for i, r := range previewRows(t, a) {
		switch strings.TrimSpace(r) {
		case "• three":
			three = i
		case "and another line":
			after = i
		}
	}
	if three < 0 || after <= three {
		t.Fatalf("want \"• three\" and then \"and another line\" as their own rows:\n%s", strings.Join(previewRows(t, a), "\n"))
	}
}

func TestDraftPreviewNotes(t *testing.T) {
	cases := []struct {
		name, draft string
		setup       func(a *App)
		want        string
		notWant     string
	}{
		{name: "empty", draft: "", want: "nothing to preview"},
		{name: "whitespace only", draft: "  \n\n ", want: "nothing to preview"},
		{name: "slash command", draft: "/remind me", want: "slash command, not a message"},
		{
			name: "attachment", draft: "**x**", want: "caption is sent as typed",
			setup: func(a *App) {
				a.compose.AddAttachment(core.PendingAttachment{Filename: "a.png", Bytes: []byte{1}})
			},
		},
		{
			name: "edit starting with slash", draft: "/not a command", want: "/not a command", notWant: "slash command",
			setup: func(a *App) { a.editing.Begin("C1", "1.0", PanelMessages, "") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := draftApp(t, 120, 40, tc.draft)
			if tc.setup != nil {
				tc.setup(a)
			}
			rows := previewRows(t, a)
			if !joinedHas(rows[1:], tc.want) {
				t.Errorf("preview lacks %q:\n%s", tc.want, strings.Join(rows, "\n"))
			}
			if tc.notWant != "" && joinedHas(rows, tc.notWant) {
				t.Errorf("preview has %q:\n%s", tc.notWant, strings.Join(rows, "\n"))
			}
		})
	}
}

func TestDraftPreviewMentionsUseNames(t *testing.T) {
	a := draftApp(t, 120, 40, "hi @bob")
	a.compose.SetUsers([]mentionpicker.User{{ID: "U2", DisplayName: "bob"}})
	rows := previewRows(t, a)
	if !joinedHas(rows, "@U2") || joinedHas(rows, "Bob Smith") {
		t.Fatalf("with no name for U2 the preview should show @U2:\n%s", strings.Join(rows, "\n"))
	}
	a.messagepane.SetUserNames(map[string]string{"U2": "Bob Smith"})
	rows = previewRows(t, a)
	if !joinedHas(rows, "@Bob Smith") {
		t.Fatalf("after the pane learns U2's name the preview should show it:\n%s", strings.Join(rows, "\n"))
	}
}

func TestDraftPreviewCapShowsTheEnd(t *testing.T) {
	// 120x80 leaves the preview more room than its cap, so the cap
	// binds.
	a := draftApp(t, 120, 80, numberedDraft())
	rows := previewRows(t, a)
	inner := a.computeFrame().ContentHeight - 2
	if want := max(3, inner/3); len(rows) != want {
		t.Fatalf("preview has %d rows, want the cap %d:\n%s", len(rows), want, strings.Join(rows, "\n"))
	}
	if got := strings.TrimSpace(rows[1]); got != "…" {
		t.Errorf("first body row = %q, want the cut marker …", got)
	}
	if got := strings.TrimSpace(rows[len(rows)-1]); got != "line 40" {
		t.Errorf("last row = %q, want line 40", got)
	}
}

func TestDraftPreviewYieldsToMessages(t *testing.T) {
	// 25 lines at 120x40: the box (27 rows) leaves the preview less
	// than its cap, so it takes what is left above the messages area's
	// 3 rows and no more.
	lines := strings.Split(numberedDraft(), "\n")[:25]
	a := draftApp(t, 120, 40, strings.Join(lines, "\n"))
	rows := frameRows(a)
	if _, _, _, ok := previewSpan(rows, a.computeFrame().MsgWidth-2); !ok {
		t.Fatal("no draft preview drawn, though there is room for one")
	}
	if h := a.layout.msgHeight; h != 3 {
		t.Fatalf("messages area is %d rows, want 3: the preview takes only what is left", h)
	}
}

func TestDraftPreviewShortTerminal(t *testing.T) {
	// The box alone overflows 80x12. Nothing may panic, and a preview,
	// if any, must not take the messages area's rows.
	a := draftApp(t, 80, 12, numberedDraft())
	rows := frameRows(a)
	if _, _, n, ok := previewSpan(rows, a.computeFrame().MsgWidth-2); ok {
		if h := a.layout.msgHeight; h < 3 {
			t.Fatalf("a %d-row preview left the messages area %d rows", n, h)
		}
	}
}

func TestDraftPreviewRowsFitWidth(t *testing.T) {
	draft := "https://example.com/" + strings.Repeat("a", 180) + "\n" +
		"```\n" + strings.Repeat("x", 150) + "\n```\n" +
		"日本語のテキスト 🎉🎉🎉"
	on := draftApp(t, 120, 50, draft)
	off := draftApp(t, 120, 50, draft)
	off.compose.SetPreview(false)
	onRows, offRows := frameRows(on), frameRows(off)
	if len(onRows) != len(offRows) {
		t.Fatalf("frame has %d rows with the preview, %d without", len(onRows), len(offRows))
	}
	width := on.computeFrame().MsgWidth - 2
	top, col, n, ok := previewSpan(onRows, width)
	if !ok {
		t.Fatal("no draft preview in the frame")
	}
	for i := range onRows {
		if got, want := ansi.StringWidth(onRows[i]), ansi.StringWidth(offRows[i]); got != want {
			t.Errorf("row %d is %d cells wide with the preview, %d without", i, got, want)
		}
	}
	for i := top; i < top+n; i++ {
		edge := ansi.Strip(ansi.Cut(onRows[i], col+width, col+width+1))
		if want := ansi.Strip(ansi.Cut(offRows[i], col+width, col+width+1)); edge != want {
			t.Errorf("row %d: pane edge is %q with the preview, %q without", i, edge, want)
		}
	}
}

func TestDraftPreviewNarrowPane(t *testing.T) {
	drafts := []struct {
		name, text string
		attach     bool
	}{
		{"rich", richDraft, false},
		{"empty", "", false},
		{"slash", "/remind me", false},
		{"attachment", "**x**", true},
	}
	for _, w := range []int{10, 12, 21, 24} {
		for _, d := range drafts {
			t.Run(fmt.Sprintf("%d/%s", w, d.name), func(t *testing.T) {
				a := draftApp(t, 120, 40, d.text)
				if d.attach {
					a.compose.AddAttachment(core.PendingAttachment{Filename: "a.png", Bytes: []byte{1}})
				}
				out := a.draftPreview(&a.compose, a.messagepane, PanelMessages, w, 10, compose.RenderKey{})
				if out == "" {
					t.Fatal("no preview drawn")
				}
				rows := strings.Split(out, "\n")
				for i, row := range rows {
					if got := ansi.StringWidth(row); got != w {
						t.Errorf("row %d is %d cells wide, want %d: %q", i, got, w, ansi.Strip(row))
					}
				}
				// Cut, not wrapped: a wrapped row would come back as
				// several rows of the right width.
				if !strings.HasPrefix(ansi.Strip(rows[0]), "──") || !strings.HasPrefix(ansi.Strip(rows[1]), "  ") {
					t.Errorf("title or first line wrapped:\n%s", ansi.Strip(out))
				}
				if d.name != "rich" && len(rows) != 2 {
					t.Errorf("a note preview has %d rows, want the title and the note:\n%s", len(rows), ansi.Strip(out))
				}
			})
		}
	}
}

func TestDraftPreviewSitsAbovePicker(t *testing.T) {
	a := draftApp(t, 120, 40, "")
	typeInto(t, &a.compose, "@")
	if !a.compose.IsMentionActive() {
		t.Fatal("precondition: typing @ did not open the mention picker")
	}
	rows := frameRows(a)
	top, _, n, ok := previewSpan(rows, a.computeFrame().MsgWidth-2)
	if !ok {
		t.Fatal("no draft preview in the frame")
	}
	picker := -1
	for i, r := range rows {
		if strings.Contains(ansi.Strip(r), "everyone") {
			picker = i
			break
		}
	}
	if picker < 0 {
		t.Fatal("precondition: no picker row in the frame")
	}
	if picker <= top+n-1 {
		t.Fatalf("picker row %d is not below the preview (rows %d..%d)", picker, top, top+n-1)
	}
}

func TestDraftPreviewHiddenWhenComposeDisabled(t *testing.T) {
	a := draftApp(t, 120, 40, richDraft)
	a.composeDisabled = true
	if _, _, _, ok := previewSpan(frameRows(a), a.computeFrame().MsgWidth-2); ok {
		t.Fatal("a draft preview is drawn where the box is replaced by the read-only line")
	}
}

func TestDraftPreviewFollowsTheme(t *testing.T) {
	a := draftApp(t, 120, 40, richDraft)
	t.Cleanup(func() { a.applyTheme("dark") })
	body := func() string {
		width := a.computeFrame().MsgWidth - 2
		rows := frameRows(a)
		top, col, n, ok := previewSpan(rows, width)
		if !ok {
			t.Fatal("no draft preview in the frame")
		}
		var b strings.Builder
		for i := top + 1; i < top+n; i++ {
			b.WriteString(ansi.Cut(rows[i], col, col+width))
		}
		return b.String()
	}
	dark := body()
	a.applyTheme("nord")
	if nord := body(); nord == dark {
		t.Fatal("the preview body kept the old theme's colours after a theme switch")
	}
}

func TestClickOnDraftPreviewEntersInsert(t *testing.T) {
	a := draftApp(t, 120, 40, richDraft)
	a.SetMode(ModeNormal)
	a.compose.Blur()
	rows := frameRows(a)
	top, _, _, ok := previewSpan(rows, a.computeFrame().MsgWidth-2)
	if !ok {
		t.Fatal("no draft preview in the frame")
	}
	a.Update(tea.MouseClickMsg{X: a.layout.sidebarEnd + 5, Y: top, Button: tea.MouseLeft})
	if a.mode != ModeInsert || a.focusedPanel != PanelMessages {
		t.Fatalf("after a click on the preview: mode %v, panel %v; want insert in the channel box", a.mode, a.focusedPanel)
	}
}

// BenchmarkDraftPreview is one keystroke and the frame it draws, on a
// 2 KB draft in a 20,000-person workspace, with the draft preview on
// and, for comparison, off.
func BenchmarkDraftPreview(b *testing.B) {
	for _, on := range []bool{true, false} {
		b.Run(map[bool]string{true: "on", false: "off"}[on], func(b *testing.B) {
			pinRenderGlobals(b)
			a := newTestApp(b, withSize(160, 50))
			users := make([]mentionpicker.User, 20000)
			for i := range users {
				users[i] = mentionpicker.User{ID: fmt.Sprintf("U%05d", i), DisplayName: fmt.Sprintf("person.%d", i)}
			}
			a.compose.SetUsers(users)
			draft := strings.Repeat("- ship **v0.28** on Tuesday with @person.42 and [the notes](https://example.com/x)\n", 25)
			_ = a.compose.Focus()
			a.compose.SetValue(draft)
			a.compose.SetPreview(on)
			_ = a.View()
			key := tea.KeyPressMsg{Code: 'x', Text: "x"}
			for b.Loop() {
				a.compose, _ = a.compose.Update(key)
				_ = a.View()
			}
		})
	}
}

// With room for only one body row, the preview shows the draft's last
// line, not a cut marker and nothing else.
func TestDraftPreviewTwoRowsShowTheLastLine(t *testing.T) {
	a := draftApp(t, 120, 40, "first\nsecond\nthird")
	plain := func(maxRows int) []string {
		out := a.draftPreview(&a.compose, a.messagepane, PanelMessages, 40, maxRows, compose.RenderKey{})
		rows := strings.Split(ansi.Strip(out), "\n")
		for i := range rows {
			rows[i] = strings.TrimSpace(rows[i])
		}
		return rows
	}
	if got := plain(2); len(got) != 2 || got[1] != "third" {
		t.Errorf("2 rows: %q, want the title and third", got)
	}
	if got := plain(3); len(got) != 3 || got[1] != "…" || got[2] != "third" {
		t.Errorf("3 rows: %q, want the title, … and third", got)
	}
}

// A sent message wraps 3 cells narrower when the pane draws an avatar
// beside it; the preview wraps where the sent message will.
func TestDraftPreviewWrapsLikeASentMessage(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("aaaa ", 11)) // 54 cells
	bodyRows := func(a *App) int {
		out := a.draftPreview(&a.compose, a.messagepane, PanelMessages, 60, 20, compose.RenderKey{})
		return len(strings.Split(out, "\n")) - 1
	}
	a := draftApp(t, 120, 40, words)
	a.SetCurrentUserID("U1")
	if n := bodyRows(a); n != 1 {
		t.Fatalf("no avatars: %d body rows, want 1 (the pane wraps at 56)", n)
	}
	a = draftApp(t, 120, 40, words)
	a.SetCurrentUserID("U1")
	a.messagepane.SetAvatarFunc(func(string) string { return "████\n████" })
	if n := bodyRows(a); n != 2 {
		t.Fatalf("with avatars: %d body rows, want 2 (the pane wraps at 53 beside an avatar)", n)
	}
}

// Your avatar can arrive while the draft sits unchanged and the pane's
// version stays put; the preview must re-wrap for it, not keep the
// cached wider body.
func TestDraftPreviewRewrapsWhenAvatarArrives(t *testing.T) {
	words := strings.TrimSpace(strings.Repeat("aaaa ", 11)) // 54 cells
	a := draftApp(t, 120, 40, words)
	a.SetCurrentUserID("U1")
	loaded := false
	a.messagepane.SetAvatarFunc(func(string) string {
		if loaded {
			return "████\n████"
		}
		return ""
	})
	bodyRows := func() int {
		out := a.draftPreview(&a.compose, a.messagepane, PanelMessages, 60, 20, compose.RenderKey{})
		return len(strings.Split(out, "\n")) - 1
	}
	if n := bodyRows(); n != 1 {
		t.Fatalf("avatar not loaded: %d body rows, want 1", n)
	}
	loaded = true
	if n := bodyRows(); n != 2 {
		t.Fatalf("avatar loaded: %d body rows, want 2 (the sent message wraps beside it)", n)
	}
}
