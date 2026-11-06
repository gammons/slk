package ui

import (
	goimage "image"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
)

// statusEmojiApp is the peer-status fixture (DM D1 with alice, U1) in
// image mode, with ff as its emoji fetcher and a custom company-logo
// emoji not loaded yet.
func statusEmojiApp(t *testing.T, ff sidebarEmojiFetcher) (*App, map[string]string) {
	t.Helper()
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })
	a := newPeerStatusTestApp(t)
	a.activeTeamID = "T1"
	a.SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: ff}, Cells: 2})
	return a, map[string]string{"company-logo": "https://emoji.slack-edge.com/T1/company-logo/abc.png"}
}

func warmEmoji(ff sidebarEmojiFetcher, url, id string) string {
	placement := "\x1b[38;2;0;0;" + id + "m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
	ff[emoji.EmojiCacheKey(url)] = imgpkg.Render{Cells: goimage.Pt(2, 1), Lines: []string{placement}}
	return placement
}

// The App hands the finder its emoji context and, once they load, the
// custom emoji: a DM peer's custom status shows its artwork in the box.
func TestStatusEmoji_FinderGetsTheContextAndCustoms(t *testing.T) {
	ff := sidebarEmojiFetcher{}
	a, customs := statusEmojiApp(t, ff)
	placement := warmEmoji(ff, customs["company-logo"], "91")
	_, _ = a.Update(UserStatusChangeMsg{TeamID: "T1", UserID: "U1", Emoji: ":company-logo:", Text: "Shipping"})
	a.channelFinder.Open()

	if box := a.channelFinder.View(120); strings.Contains(box, placement) {
		t.Fatalf("custom emoji drawn before the customs loaded:\n%q", box)
	}
	a.SetCustomEmoji(customs)
	if box := a.channelFinder.View(120); !strings.Contains(box, placement) {
		t.Errorf("finder DM row lacks the custom status image after SetCustomEmoji:\n%q", box)
	}
}

// A DM's header draws its peer's status emoji as an image, whether the
// status was there when the DM opened or changed while it is open.
func TestStatusEmoji_DMHeaderThroughTheProductionPath(t *testing.T) {
	ff := sidebarEmojiFetcher{}
	a, _ := statusEmojiApp(t, ff)
	cal, _ := emoji.URLForShortcode("calendar", nil)
	palm, _ := emoji.URLForShortcode("palm_tree", nil)
	calImg := warmEmoji(ff, cal, "92")
	palmImg := warmEmoji(ff, palm, "93")

	_, _ = a.Update(UserStatusChangeMsg{TeamID: "T1", UserID: "U1", Emoji: ":calendar:", Text: "In a meeting"})
	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})
	if got := a.messagepane.View(10, 80); !strings.Contains(got, calImg) || !strings.Contains(got, "In a meeting") {
		t.Fatalf("opening the DM did not draw the status emoji as an image:\n%q", got)
	}

	_, _ = a.Update(UserStatusChangeMsg{TeamID: "T1", UserID: "U1", Emoji: ":palm_tree:", Text: "Vacation"})
	got := a.messagepane.View(10, 80)
	if !strings.Contains(got, palmImg) || strings.Contains(got, calImg) {
		t.Errorf("a live status change did not reach the open DM header as an image:\n%q", got)
	}
}

// The debounced emoji invalidation reaches the DM header: a fetch that
// lands turns its cold reservation into the image.
func TestStatusEmoji_InvalidationTickRebuildsTheDMHeader(t *testing.T) {
	ff := sidebarEmojiFetcher{}
	a, _ := statusEmojiApp(t, ff)
	_, _ = a.Update(UserStatusChangeMsg{TeamID: "T1", UserID: "U1", Emoji: ":hourglass_flowing_sand:", Text: "Waiting"})
	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})
	_ = a.messagepane.View(10, 80) // cold: a blank reservation

	url, _ := emoji.URLForShortcode("hourglass_flowing_sand", nil)
	placement := warmEmoji(ff, url, "94")
	if got := a.messagepane.View(10, 80); strings.Contains(got, placement) {
		t.Fatalf("precondition: the header should still hold the cold reservation:\n%q", got)
	}
	_, _ = a.Update(emojiInvalidateMsg{})
	if got := a.messagepane.View(10, 80); !strings.Contains(got, placement) {
		t.Errorf("emojiInvalidateMsg did not rebuild the DM header:\n%q", got)
	}
}
