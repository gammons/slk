package chrome

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/ansi"
)

// testStyles has the real geometry (a rounded border and one cell of
// padding, like the theme's box) and attributes in place of colours.
func testStyles() Styles {
	return Styles{
		Box:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 1),
		Popup:       lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()),
		Title:       lipgloss.NewStyle().Bold(true),
		Input:       lipgloss.NewStyle().BorderStyle(lipgloss.Border{Left: "▌"}).BorderLeft(true).PaddingLeft(1),
		Placeholder: lipgloss.NewStyle().Faint(true),
		Row:         lipgloss.NewStyle(),
		RowSelected: lipgloss.NewStyle().Bold(true),
		Marker:      lipgloss.NewStyle().Bold(true),
		PopupMarker: lipgloss.NewStyle().Bold(true),
		Empty:       lipgloss.NewStyle().Italic(true),
		Footer:      lipgloss.NewStyle().Faint(true),
		Thumb:       lipgloss.NewStyle(),
		Track:       lipgloss.NewStyle(),
	}
}

// inner returns the box's lines with the border and padding trimmed.
func inner(box string) []string {
	var out []string
	for _, l := range strings.Split(xansi.Strip(box), "\n") {
		out = append(out, strings.Trim(l, "╭╮╰╯─│ "))
	}
	return out
}

func equalLines(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines\n got %q\nwant %q", got, want)
	}
}

func TestBox_IsTheBoxStyleOverContentWithBaseReasserted(t *testing.T) {
	s := testStyles()
	s.BaseANSI = "\x1b[48;5;1m"
	content := "\x1b[1mx\x1b[m y"
	want := s.Box.Width(12).Render(ansi.ReapplyAfterResets(content, s.BaseANSI))
	if got := Box(s, content, 12); got != want {
		t.Errorf("Box = %q, want %q", got, want)
	}
	if w := lipgloss.Width(Box(s, "x", 12)); w != 12 {
		t.Errorf("Box width = %d, want 12", w)
	}
}

func TestFrame_Layout(t *testing.T) {
	s := testStyles()
	cases := []struct {
		name string
		f    Frame
		want []string
	}{
		{"input, body and footer",
			Frame{Title: "T", Input: "I", Body: []string{"a", "b"}, Footer: "F"},
			[]string{"", "", "T", "I", "", "a", "b", "", "F", "", ""}},
		{"no input, no footer",
			Frame{Title: "T", Body: []string{"a"}},
			[]string{"", "", "T", "", "a", "", ""}},
		{"empty body draws Empty",
			Frame{Title: "T", Input: "I", Empty: "none"},
			[]string{"", "", "T", "I", "", "none", "", ""}},
		{"empty body, no Empty, keeps the blank body line",
			Frame{Title: "T", Input: "I"},
			[]string{"", "", "T", "I", "", "", "", ""}},
		{"Empty ignored when there is a body",
			Frame{Title: "T", Body: []string{"a"}, Empty: "none"},
			[]string{"", "", "T", "", "a", "", ""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			equalLines(t, inner(c.f.Render(s, 20)), c.want)
		})
	}
}

func TestBodyTop_IsTheFirstBodyRow(t *testing.T) {
	s := testStyles()
	for _, hasInput := range []bool{true, false} {
		f := Frame{Title: "T", Body: []string{"BODY"}}
		if hasInput {
			f.Input = "I"
		}
		lines := strings.Split(xansi.Strip(f.Render(s, 20)), "\n")
		row := -1
		for i, l := range lines {
			if strings.Contains(l, "BODY") {
				row = i
			}
		}
		if got := BodyTop(s, hasInput); got != row {
			t.Errorf("BodyTop(hasInput=%v) = %d, body drawn on row %d", hasInput, got, row)
		}
	}
	if BodyTop(s, true) != 5 || BodyTop(s, false) != 4 {
		t.Errorf("BodyTop = %d/%d, want 5/4 for the theme's box", BodyTop(s, true), BodyTop(s, false))
	}
}

func TestFilterInput(t *testing.T) {
	s := testStyles()
	if got := xansi.Strip(FilterInput(s, "", "Type to filter...")); got != "▌ █ Type to filter..." {
		t.Errorf("empty query = %q", got)
	}
	if got := xansi.Strip(FilterInput(s, "abc", "Type to filter...")); got != "▌ abc█" {
		t.Errorf("query = %q", got)
	}
}

func TestPopup(t *testing.T) {
	s := testStyles()
	if got := Popup(s, nil, 20); got != "" {
		t.Errorf("Popup with no rows = %q, want empty", got)
	}
	box := Popup(s, []string{PopupMarker(s, true) + "a", PopupMarker(s, false) + "b"}, 20)
	if w := lipgloss.Width(box); w != 18 {
		t.Errorf("popup width = %d, want 18 (width-2)", w)
	}
	equalLines(t, inner(box), []string{"", "▌ a", "b", ""})
}

func TestPopupReassertsPopupANSIAfterResets(t *testing.T) {
	s := Styles{PopupANSI: "\x1b[48;5;17m"}
	out := Popup(s, []string{"\x1b[1ma\x1b[m b"}, 10)
	if !strings.Contains(out, "\x1b[m\x1b[48;5;17m") {
		t.Errorf("Popup did not re-assert PopupANSI after the reset: %q", out)
	}
	if got := Popup(Styles{}, []string{"\x1b[1ma\x1b[m b"}, 10); strings.Contains(got, "\x1b[48;5;17m") {
		t.Errorf("empty PopupANSI changed the rows: %q", got)
	}
}
