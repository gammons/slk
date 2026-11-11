package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/imgrender"
)

func TestRenderMessageBodyOnly(t *testing.T) {
	text := "• ship *v0.28*\n<@U2>"
	msg := MessageItem{UserName: "alice", Timestamp: "3:04 PM", Text: text}
	for _, compact := range []bool{false, true} {
		rc := &RenderContext{Images: imgrender.NewRenderer(), UserNames: map[string]string{"U2": "bob"}, Compact: compact}
		got := ansi.Strip(RenderMessage(msg, 60, rc, RenderOptions{BodyOnly: true}).Content)
		for _, want := range []string{"ship v0.28", "@bob"} {
			if !strings.Contains(got, want) {
				t.Errorf("compact=%v: body-only render %q lacks %q", compact, got, want)
			}
		}
		for _, bad := range []string{"alice", "3:04 PM"} {
			if strings.Contains(got, bad) {
				t.Errorf("compact=%v: body-only render %q has the header's %q", compact, got, bad)
			}
		}
	}

	rc := &RenderContext{Images: imgrender.NewRenderer(), UserNames: map[string]string{"U2": "bob"}}
	full := ansi.Strip(RenderMessage(msg, 60, rc, RenderOptions{}).Content)
	_, fullBody, _ := strings.Cut(full, "\n")
	bodyOnly := ansi.Strip(RenderMessage(msg, 60, rc, RenderOptions{BodyOnly: true}).Content)
	if fullBody != bodyOnly {
		t.Fatalf("body rows differ:\nfull render minus header: %q\nbody only:                %q", fullBody, bodyOnly)
	}
}

func TestDraftRenderContextHasNoImages(t *testing.T) {
	m := New(nil, "general")
	m.SetEmojiContext(EmojiContext{
		PlaceCtx: emojiutil.PlaceContext{Fetcher: newFakePlaceFetcher()},
		Cells:    1,
		Customs:  map[string]string{"x": "y"},
	})
	m.SetUserNames(map[string]string{"U2": "bob"})
	rc := m.DraftRenderContext()
	if rc.Emoji.PlaceCtx.Fetcher != nil || rc.Emoji.PlaceCtx.SendMsg != nil {
		t.Error("draft render context places emoji images")
	}
	if rc.Emoji.Cells != 1 || rc.Emoji.Customs["x"] != "y" {
		t.Errorf("draft render context lost the emoji cells or customs: %+v", rc.Emoji)
	}
	if rc.UserNames["U2"] != "bob" {
		t.Error("draft render context lost the pane's user names")
	}
	if rc.SearchTerms != nil || rc.ReplyAvatar != nil {
		t.Error("draft render context carries search terms or a reply avatar")
	}
}
