package mrkdwn

import (
	"encoding/json"
	"testing"

	"github.com/slack-go/slack"
)

// blockJSON serialises a *slack.RichTextBlock to JSON for readable
// test failure messages.
func blockJSON(b *slack.RichTextBlock) string {
	if b == nil {
		return "nil"
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func TestConvert_EmptyInput(t *testing.T) {
	mr, blk := Convert("")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty", mr)
	}
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_WhitespaceOnly(t *testing.T) {
	mr, blk := Convert("   \n  ")
	if mr != "" {
		t.Errorf("mrkdwn = %q, want empty for whitespace-only input", mr)
	}
	if blk != nil {
		t.Errorf("block = %s, want nil", blockJSON(blk))
	}
}

func TestConvert_PlainText(t *testing.T) {
	mr, blk := Convert("hello world")
	if mr != "hello world" {
		t.Errorf("mrkdwn = %q, want %q", mr, "hello world")
	}
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 1 {
		t.Fatalf("got %d elements, want 1", len(blk.Elements))
	}
	sec, ok := blk.Elements[0].(*slack.RichTextSection)
	if !ok {
		t.Fatalf("element[0] is %T, want *RichTextSection", blk.Elements[0])
	}
	if len(sec.Elements) != 1 {
		t.Fatalf("section has %d elements, want 1", len(sec.Elements))
	}
	te, ok := sec.Elements[0].(*slack.RichTextSectionTextElement)
	if !ok {
		t.Fatalf("section element is %T, want *RichTextSectionTextElement", sec.Elements[0])
	}
	if te.Text != "hello world" {
		t.Errorf("text = %q, want %q", te.Text, "hello world")
	}
	if te.Style != nil {
		t.Errorf("style = %+v, want nil", te.Style)
	}
}

func TestConvert_TwoParagraphs(t *testing.T) {
	mr, blk := Convert("para one\n\npara two")
	want := "para one\n\npara two"
	if mr != want {
		t.Errorf("mrkdwn = %q, want %q", mr, want)
	}
	if blk == nil {
		t.Fatal("block is nil")
	}
	if len(blk.Elements) != 2 {
		t.Fatalf("got %d elements, want 2 (one section per paragraph)", len(blk.Elements))
	}
}
