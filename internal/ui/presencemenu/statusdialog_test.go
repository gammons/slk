package presencemenu

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The click geometry matches what is drawn: the line at each row a
// click selects shows that choice, and BoxSize is the drawn box.
func TestStatusDialogClickGeometry(t *testing.T) {
	var d StatusDialog
	d.Open("Acme")
	lines := strings.Split(ansi.Strip(d.renderBox(100)), "\n")
	for i, c := range ClearAfterChoices {
		y := clearRowsTop() + i
		if !strings.Contains(lines[y], c.String()) {
			t.Errorf("line %d = %q, want %q", y, lines[y], c)
		}
		if !d.ClickRow(100, 40, y) || d.ClearAfter() != c {
			t.Errorf("click on line %d selected %q, want %q", y, d.ClearAfter(), c)
		}
	}
	if d.ClickRow(100, 40, clearRowsTop()-1) || d.ClickRow(100, 40, clearRowsTop()+len(ClearAfterChoices)) {
		t.Error("a click outside the rows was taken")
	}
	w, h := d.BoxSize(100, 40)
	if box := d.renderBox(100); w != lipgloss.Width(box) || h != lipgloss.Height(box) {
		t.Errorf("BoxSize = %dx%d, drawn %dx%d", w, h, lipgloss.Width(box), lipgloss.Height(box))
	}
}

func TestStatusDialogTextLimit(t *testing.T) {
	var d StatusDialog
	d.Open("")
	d.Insert(strings.Repeat("é", MaxStatusText+20))
	if n := len([]rune(d.Text())); n != MaxStatusText {
		t.Errorf("text is %d characters, want %d", n, MaxStatusText)
	}
}

// A pasted ZWJ emoji or tag flag keeps its joiners and tags.
func TestStatusDialogKeepsEmojiSequences(t *testing.T) {
	const fam, scotland = "👨\u200d👩\u200d👧", "🏴\U000e0067\U000e0062\U000e0073\U000e0063\U000e0074\U000e007f"
	var d StatusDialog
	d.Open("W")
	d.Insert("Family " + fam + " " + scotland + "\x07")
	if want := "Family " + fam + " " + scotland; d.Text() != want {
		t.Errorf("text = %q, want %q (control character dropped, emoji whole)", d.Text(), want)
	}
}

// The text line keeps its end in view without detaching an accent.
func TestStatusDialogClipsBetweenCharacters(t *testing.T) {
	for _, n := range []int{60, 61} {
		var d StatusDialog
		d.Open("W")
		d.Insert(strings.Repeat("e\u0301", n))
		if box := d.renderBox(80); !strings.Contains(box, "…") || strings.Contains(box, "…\u0301") {
			t.Errorf("n=%d: text clipped inside a character", n)
		}
	}
}
