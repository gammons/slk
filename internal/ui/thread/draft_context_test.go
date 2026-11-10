package thread

import (
	"testing"

	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestDraftRenderContextHasNoImages(t *testing.T) {
	m := New()
	m.SetEmojiContext(messages.EmojiContext{
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

func TestDraftRenderOptionsWrapLikeAReply(t *testing.T) {
	m := New()
	m.SetAvatarFunc(func(string) string { return "████" })
	if opt := m.DraftRenderOptions("U1"); !opt.BodyOnly || opt.Avatar != "" {
		t.Fatalf("avatars off: %+v, want body only and no avatar", opt)
	}
	m.SetShowAvatars(true)
	if opt := m.DraftRenderOptions("U1"); !opt.BodyOnly || opt.Avatar == "" {
		t.Fatalf("avatars on: %+v, want body only beside the reply's avatar", opt)
	}
}
