package blockkit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderLegacyEmptyReturnsZero(t *testing.T) {
	r := RenderLegacy(nil, Context{}, 80)
	if r.Height != 0 {
		t.Errorf("Height = %d, want 0", r.Height)
	}
}

func TestRenderLegacyTitleAndText(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "Service down",
		Text:  "checkout-svc returning 5xx",
	}}, ctx, 80)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "Service down") {
		t.Errorf("missing title: %q", plain)
	}
	if !strings.Contains(plain, "checkout-svc returning 5xx") {
		t.Errorf("missing text: %q", plain)
	}
}

func TestRenderLegacyHasColorStripeOnEveryRow(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Color: "danger",
		Title: "T",
		Text:  "line1\nline2\nline3",
	}}, ctx, 40)
	for i, line := range r.Lines {
		plain := ansi.Strip(line)
		if !strings.HasPrefix(plain, "█") {
			t.Errorf("line %d does not start with stripe glyph: %q", i, plain)
		}
	}
}

func TestRenderLegacyPretextRendersAboveStripe(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Pretext: "Heads up:",
		Title:   "Inside",
	}}, ctx, 40)
	if r.Height < 2 {
		t.Fatalf("Height = %d, want >= 2", r.Height)
	}
	first := ansi.Strip(r.Lines[0])
	if !strings.Contains(first, "Heads up:") {
		t.Errorf("Lines[0] = %q, want pretext", first)
	}
	if strings.HasPrefix(first, "█") {
		t.Errorf("Lines[0] = %q, pretext must NOT have stripe", first)
	}
}

func TestRenderLegacyFooterAndTimestamp(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title:  "T",
		Footer: "Datadog",
		TS:     1700000000,
	}}, ctx, 60)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "Datadog") {
		t.Errorf("missing footer: %q", plain)
	}
	// 1700000000 = 2023-11-14 in UTC
	if !strings.Contains(plain, "2023") {
		t.Errorf("expected formatted timestamp '2023…' in %q", plain)
	}
}

func TestRenderLegacyFieldsGridShortPairsShareRow(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "T",
		Fields: []LegacyField{
			{Title: "Service", Value: "web", Short: true},
			{Title: "Region", Value: "us-east-1", Short: true},
		},
	}}, ctx, 80)
	foundShared := false
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Service") && strings.Contains(plain, "Region") {
			foundShared = true
			break
		}
	}
	if !foundShared {
		t.Errorf("expected Service and Region on a shared row; lines = %q",
			ansi.Strip(strings.Join(r.Lines, "\n")))
	}
}

func TestRenderLegacyFieldsGridLongFieldFullWidth(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "T",
		Fields: []LegacyField{
			{Title: "Notes", Value: "long form", Short: false},
			{Title: "After", Value: "more", Short: false},
		},
	}}, ctx, 80)
	for _, line := range r.Lines {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "Notes") && strings.Contains(plain, "After") {
			t.Errorf("Notes and After should not share a row: %q", plain)
		}
	}
}

func TestRenderLegacyFieldRowsHaveStripe(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title: "T",
		Fields: []LegacyField{
			{Title: "K", Value: "V", Short: false},
		},
	}}, ctx, 60)
	for i, line := range r.Lines {
		plain := ansi.Strip(line)
		if !strings.HasPrefix(plain, "█") {
			t.Errorf("line %d does not start with stripe: %q", i, plain)
		}
	}
}

func TestRenderLegacyImageURLFallbackWhenNoFetcher(t *testing.T) {
	ctx := Context{
		RenderText: func(s string, _ map[string]string) string { return s },
		WrapText:   func(s string, _ int) string { return s },
	}
	r := RenderLegacy([]LegacyAttachment{{
		Title:    "T",
		ImageURL: "https://example.com/chart.png",
	}}, ctx, 60)
	plain := ansi.Strip(strings.Join(r.Lines, "\n"))
	if !strings.Contains(plain, "https://example.com/chart.png") {
		t.Errorf("expected ImageURL fallback link, got %q", plain)
	}
	if !strings.Contains(plain, "[image]") {
		t.Errorf("expected '[image]' marker in fallback, got %q", plain)
	}
}
