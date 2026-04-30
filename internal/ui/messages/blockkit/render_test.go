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

func TestRenderSectionTextOnly(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{Text: "Hello world"}}, ctx, 40)
	if r.Height < 1 {
		t.Fatalf("Height = %d, want >= 1", r.Height)
	}
	if !strings.Contains(ansi.Strip(strings.Join(r.Lines, "\n")), "Hello world") {
		t.Errorf("rendered = %q", ansi.Strip(strings.Join(r.Lines, "\n")))
	}
}

func TestRenderSectionUsesRenderTextCallback(t *testing.T) {
	called := false
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string {
			called = true
			return "[rendered]" + s
		},
		WrapText: func(s string, _ int) string { return s },
	}
	Render([]Block{SectionBlock{Text: "x"}}, ctx, 40)
	if !called {
		t.Error("RenderText callback was not invoked")
	}
}

func TestRenderSectionFieldsTwoColumnGrid(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Fields: []string{
			"Service\nweb",
			"Region\nus-east-1",
			"Status\nfiring",
		},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	for _, want := range []string{"Service", "web", "Region", "us-east-1", "Status", "firing"} {
		if !strings.Contains(all, want) {
			t.Errorf("rendered missing %q: %q", want, all)
		}
	}
	// 3 fields with 2-col grid → 2 rows: row 1 has Service+Region,
	// row 2 has Status (single field on a 2-col row).
	if r.Height < 2 {
		t.Errorf("Height = %d, want >= 2 (two grid rows)", r.Height)
	}
}

func TestRenderSectionFieldsCollapseAtNarrowWidth(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Fields: []string{"A\n1", "B\n2"},
	}}, ctx, 30) // < narrowBreakpoint
	// Expected: stacked single-column. Strong assertion: no rendered
	// line contains BOTH "A" and "B".
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "A") || !strings.Contains(all, "B") {
		t.Errorf("missing field titles: %q", all)
	}
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "A") && strings.Contains(plain, "B") {
			t.Errorf("at narrow width, fields should be stacked but found both on one line: %q", plain)
		}
	}
}
