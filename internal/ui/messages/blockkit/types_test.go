package blockkit

import "testing"

func TestBlockTypesImplementInterface(t *testing.T) {
	var blocks []Block = []Block{
		SectionBlock{},
		HeaderBlock{},
		ContextBlock{},
		DividerBlock{},
		ImageBlock{},
		ActionsBlock{},
		UnknownBlock{Type: "video"},
	}
	if got := blocks[6].blockType(); got != "video" {
		t.Errorf("UnknownBlock.blockType() = %q, want %q", got, "video")
	}
}

func TestRenderResultZeroValueIsSafe(t *testing.T) {
	var r RenderResult
	if r.Height != 0 {
		t.Error("zero RenderResult should have Height 0")
	}
	if r.Interactive {
		t.Error("zero RenderResult should not be Interactive")
	}
	if len(r.Lines) != 0 {
		t.Error("zero RenderResult should have empty Lines")
	}
	if len(r.Flushes) != 0 {
		t.Error("zero RenderResult should have empty Flushes")
	}
	if r.SixelRows != nil {
		t.Error("zero RenderResult should have nil SixelRows (map zero value)")
	}
	if len(r.Hits) != 0 {
		t.Error("zero RenderResult should have empty Hits")
	}
}
