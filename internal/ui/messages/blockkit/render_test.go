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

func TestRenderSectionWithButtonAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Ready to deploy?",
		Accessory: LabelAccessory{Kind: "button", Label: "Deploy"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "Ready to deploy?") {
		t.Errorf("missing body: %q", all)
	}
	if !strings.Contains(all, "[ Deploy ]") {
		t.Errorf("missing button label: %q", all)
	}
	// Side-by-side at width 80: body and button on at least one
	// shared row.
	foundShared := false
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Ready") && strings.Contains(plain, "Deploy") {
			foundShared = true
			break
		}
	}
	if !foundShared {
		t.Error("expected body and button on at least one shared row at width 80")
	}
	if !r.Interactive {
		t.Error("Interactive should be true after rendering a button accessory")
	}
}

func TestRenderSectionWithSelectAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Pick env:",
		Accessory: LabelAccessory{Kind: "static_select", Label: "production"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "production ▾") {
		t.Errorf("expected 'production ▾' in output, got %q", all)
	}
}

func TestRenderSectionWithOverflowAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Options",
		Accessory: LabelAccessory{Kind: "overflow"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "⋯") {
		t.Errorf("expected '⋯' for overflow, got %q", all)
	}
}

func TestRenderSectionWithDatepickerAccessory(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Pick date",
		Accessory: LabelAccessory{Kind: "datepicker", Label: "2026-04-30"},
	}}, ctx, 80)
	all := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(all, "📅") || !strings.Contains(all, "2026-04-30") {
		t.Errorf("expected date glyph and value, got %q", all)
	}
}

func TestRenderSectionAccessoryStacksAtNarrowWidth(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{SectionBlock{
		Text:      "Body",
		Accessory: LabelAccessory{Kind: "button", Label: "X"},
	}}, ctx, 30) // < narrowBreakpoint
	// Body and button must NOT share a row.
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Body") && strings.Contains(plain, "X") {
			t.Errorf("at narrow width, body and accessory should stack: %q", plain)
		}
	}
}

func TestRenderContextBlockTextOnly(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{ContextBlock{
		Elements: []ContextElement{
			{Text: "Posted by"},
			{Text: "@gammons"},
			{Text: "·"},
			{Text: "2 min ago"},
		},
	}}, ctx, 80)
	if r.Height < 1 {
		t.Fatalf("Height = %d", r.Height)
	}
	plain := ansi.Strip(r.Lines[0])
	for _, want := range []string{"Posted by", "@gammons", "2 min ago"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in %q", want, plain)
		}
	}
}

func TestRenderContextBlockWithImageElementsRendersAltText(t *testing.T) {
	// Phase 2: image elements render as bracketed alt text. Phase 3
	// will swap in actual inline images.
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := Render([]Block{ContextBlock{
		Elements: []ContextElement{
			{ImageURL: "https://example.com/icon.png", AltText: "icon"},
			{Text: "by gammons"},
		},
	}}, ctx, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "[icon]") {
		t.Errorf("expected '[icon]' (Phase 2 alt-text fallback), got %q", plain)
	}
	if !strings.Contains(plain, "by gammons") {
		t.Errorf("missing text element: %q", plain)
	}
}

func TestRenderActionsBlockSetsInteractive(t *testing.T) {
	r := Render([]Block{ActionsBlock{
		Elements: []ActionElement{
			{Kind: "button", Label: "Approve"},
			{Kind: "button", Label: "Deny"},
		},
	}}, Context{}, 80)
	if !r.Interactive {
		t.Error("Interactive should be true after rendering actions")
	}
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "[ Approve ]") || !strings.Contains(plain, "[ Deny ]") {
		t.Errorf("got %q", plain)
	}
}

func TestRenderActionsBlockWrapsAtWidth(t *testing.T) {
	// Three buttons of "[ Long Button Name ]" (~20 cols each) at
	// width 30 must wrap.
	r := Render([]Block{ActionsBlock{
		Elements: []ActionElement{
			{Kind: "button", Label: "Long Button Name"},
			{Kind: "button", Label: "Long Button Name"},
			{Kind: "button", Label: "Long Button Name"},
		},
	}}, Context{}, 30)
	if r.Height < 2 {
		t.Errorf("Height = %d, want >= 2 (wrapped)", r.Height)
	}
}

func TestRenderActionsBlockMixedKinds(t *testing.T) {
	r := Render([]Block{ActionsBlock{
		Elements: []ActionElement{
			{Kind: "button", Label: "Go"},
			{Kind: "static_select", Label: "env"},
			{Kind: "datepicker", Label: "2026-01-01"},
		},
	}}, Context{}, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "[ Go ]") {
		t.Errorf("missing button: %q", plain)
	}
	if !strings.Contains(plain, "env ▾") {
		t.Errorf("missing select: %q", plain)
	}
	if !strings.Contains(plain, "📅") {
		t.Errorf("missing datepicker: %q", plain)
	}
}
