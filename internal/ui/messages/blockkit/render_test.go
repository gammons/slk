package blockkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderEmptyBlocksProducesNoLines(t *testing.T) {
	r := Render(nil, Context{}, 80)
	if r.Height != 0 || len(r.Lines) != 0 {
		t.Errorf("got Height=%d Lines=%d, want 0/0", r.Height, len(r.Lines))
	}
}

func TestRenderDividerProducesHorizontalRule(t *testing.T) {
	r := Render([]Block{DividerBlock{}}, Context{}, 20)
	if r.Height != 1 {
		t.Fatalf("Height = %d, want 1", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	if len([]rune(plain)) != 20 {
		t.Errorf("rune width = %d, want 20", len([]rune(plain)))
	}
	for _, ch := range plain {
		if ch != '─' && ch != '-' {
			t.Errorf("unexpected rune %q in divider %q", ch, plain)
			break
		}
	}
}

func TestRenderHeaderProducesBoldText(t *testing.T) {
	r := Render([]Block{HeaderBlock{Text: "Deploy successful"}}, Context{}, 80)
	if r.Height != 1 {
		t.Fatalf("Height = %d, want 1", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	if !strings.Contains(plain, "Deploy successful") {
		t.Errorf("plain = %q, want it to contain header text", plain)
	}
}

func TestRenderHeaderTruncatesIfTooWide(t *testing.T) {
	long := strings.Repeat("X", 200)
	r := Render([]Block{HeaderBlock{Text: long}}, Context{}, 20)
	plain := ansi.Strip(r.Lines[0])
	if len([]rune(plain)) > 20 {
		t.Errorf("rune width = %d, want <= 20", len([]rune(plain)))
	}
}

func TestRenderUnknownBlockShowsTypePlaceholder(t *testing.T) {
	r := Render([]Block{UnknownBlock{Type: "rich_text"}}, Context{}, 80)
	if r.Height != 1 {
		t.Fatalf("Height = %d", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	if !strings.Contains(plain, "rich_text") {
		t.Errorf("plain = %q, want it to mention type", plain)
	}
	if !strings.Contains(plain, "[unsupported block:") {
		t.Errorf("plain = %q, want '[unsupported block:' marker", plain)
	}
}

func TestRenderMultipleBlocksConcatInOrder(t *testing.T) {
	r := Render([]Block{
		HeaderBlock{Text: "First"},
		DividerBlock{},
		HeaderBlock{Text: "Second"},
	}, Context{}, 40)
	if r.Height != 3 {
		t.Fatalf("Height = %d, want 3", r.Height)
	}
	if !strings.Contains(ansi.Strip(r.Lines[0]), "First") {
		t.Errorf("Lines[0] = %q", ansi.Strip(r.Lines[0]))
	}
	if !strings.Contains(ansi.Strip(r.Lines[2]), "Second") {
		t.Errorf("Lines[2] = %q", ansi.Strip(r.Lines[2]))
	}
}
