package compose

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/styles"
)

func TestDraftViewLaysOutLikeView(t *testing.T) {
	const width = 40
	wrap := textareaWidth(width)

	chip := []core.PendingAttachment{{Filename: "report.pdf", Size: 2048}}
	manyChips := []core.PendingAttachment{
		{Filename: strings.Repeat("a", 30), Size: 1},
		{Filename: strings.Repeat("b", 30), Size: 1},
		{Filename: strings.Repeat("c", 30), Size: 1},
	}

	cases := []struct {
		name        string
		text        string
		attachments []core.PendingAttachment
	}{
		{"empty", "", nil},
		{"one line", "hello", nil},
		{"exactly the wrap width", strings.Repeat("x", wrap), nil},
		{"wrapped", strings.Repeat("word ", 30), nil},
		{"several lines", "one\ntwo\n\nfour", nil},
		{"over the row cap", strings.Repeat("line\n", 40), nil},
		{"attachment only", "", chip},
		{"text and wrapping chips", "caption", manyChips},
	}

	for _, compact := range []bool{false, true} {
		styles.ApplyCompact(compact)
		t.Cleanup(func() { styles.ApplyCompact(false) })

		for _, tc := range cases {
			m := New("general")
			m.SetWidth(width)
			m.SetDraftContext("T1", "C2", "")
			m.SetValue(tc.text)
			for _, a := range tc.attachments {
				m.AddAttachment(a)
			}
			m.SetDraftContext("T1", "C1", "")

			stored := m.DraftView("T1", "C2", "", "general", width)

			m.SetDraftContext("T1", "C2", "")
			m.Blur()
			want := m.View(width, false)

			if ansi.Strip(stored) != ansi.Strip(want) {
				t.Errorf("compact=%v %s: stored DraftView\n%s\nwant the text of View\n%s", compact, tc.name, ansi.Strip(stored), ansi.Strip(want))
			}
			if got := m.DraftView("T1", "C2", "", "general", width); ansi.Strip(got) != ansi.Strip(want) {
				t.Errorf("compact=%v %s: visible DraftView\n%s\nwant the text of View\n%s", compact, tc.name, ansi.Strip(got), ansi.Strip(want))
			}
			if tc.text != "" && stored == want {
				t.Errorf("compact=%v %s: DraftView is drawn in View's colours, want it muted", compact, tc.name)
			}
		}
	}
}

func TestDraftViewDrawsTheLiveDraft(t *testing.T) {
	m := New("general")
	m.SetWidth(40)
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("first")

	if got := ansi.Strip(m.DraftView("T1", "C1", "", "general", 40)); !strings.Contains(got, "first") {
		t.Fatalf("DraftView of the visible conversation = %q, want its text", got)
	}

	m.SetValue("second")
	if got := ansi.Strip(m.DraftView("T1", "C1", "", "general", 40)); !strings.Contains(got, "second") || strings.Contains(got, "first") {
		t.Errorf("DraftView after the text changed = %q, want the new text", got)
	}
}

func TestDraftViewDrawsASwappedAttachment(t *testing.T) {
	m := New("general")
	m.SetWidth(40)
	m.SetDraftContext("T1", "C2", "")
	m.SetValue("caption")
	m.AddAttachment(core.PendingAttachment{Filename: "a.png", Size: 1024})
	m.SetDraftContext("T1", "C1", "")
	m.DraftView("T1", "C2", "", "general", 40)

	m.SetDraftContext("T1", "C2", "")
	m.RemoveLastAttachment()
	m.AddAttachment(core.PendingAttachment{Filename: "much-longer-name.png", Size: 4096})
	m.SetDraftContext("T1", "C1", "")

	if got := ansi.Strip(m.DraftView("T1", "C2", "", "general", 40)); !strings.Contains(got, "much-longer-name.png") {
		t.Errorf("DraftView after the attachment was swapped = %q, want the new file", got)
	}
}

func TestDraftViewLeavesTheInputAlone(t *testing.T) {
	m := New("general")
	m.SetWidth(40)
	m.SetDraftContext("T1", "C2", "")
	m.SetValue(strings.Repeat("stored\n", 30))
	m.SetDraftContext("T1", "C1", "")
	m.SetValue("one\ntwo")
	before := m.View(40, true)

	m.DraftView("T1", "C2", "", "general", 40)

	if after := m.View(40, true); after != before {
		t.Errorf("View changed after DraftView drew another draft:\n%s\nwant\n%s", after, before)
	}
}

func TestDraftViewWrapsALongPlaceholder(t *testing.T) {
	m := New("general")
	m.SetWidth(20)
	m.SetDraftContext("T1", "C1", "")
	m.Blur()

	got := lipgloss.Height(m.DraftView("T1", "C2", "", strings.Repeat("long-name-", 5), 20))
	if want := lipgloss.Height(m.View(20, false)); got <= want {
		t.Errorf("DraftView height with a long channel name = %d, want more than the short placeholder's %d", got, want)
	}
}

func TestDraftViewFollowsTheTheme(t *testing.T) {
	t.Cleanup(func() { styles.Apply("dark", core.Theme{}) })

	m := New("general")
	m.SetWidth(40)
	m.SetDraftContext("T1", "C2", "")
	m.SetValue("draft")
	m.SetDraftContext("T1", "C1", "")

	styles.Apply("dark", core.Theme{})
	m.RefreshStyles()
	dark := m.DraftView("T1", "C2", "", "general", 40)

	styles.Apply("light", core.Theme{})
	m.RefreshStyles()
	if light := m.DraftView("T1", "C2", "", "general", 40); light == dark {
		t.Error("DraftView kept the previous theme's colours after RefreshStyles")
	}
}
