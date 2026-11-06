package ui

import (
	"context"
	goimage "image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// sidebarEmojiFetcher serves one prerendered placement per emoji URL.
type sidebarEmojiFetcher map[string]imgpkg.Render

func (f sidebarEmojiFetcher) Prerendered(key string, _ goimage.Point, _ imgpkg.Protocol) (imgpkg.Render, bool) {
	r, ok := f[key]
	return r, ok
}

func (f sidebarEmojiFetcher) Fetch(context.Context, imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	return imgpkg.FetchResult{}, nil
}

type sidebarSections []sidebar.SectionMeta

func (s sidebarSections) Ready() bool                                 { return true }
func (s sidebarSections) OrderedSlackSections() []sidebar.SectionMeta { return s }

// The App hands the sidebar its emoji context and, once they load, the
// workspace's custom emoji: a section header's custom emoji then shows
// its artwork in the frame.
func TestSidebarEmoji_CustomSectionEmojiReachesTheFrame(t *testing.T) {
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })

	customs := map[string]string{"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif"}
	const imageSGR = "\x1b[38;2;0;0;43m"
	ff := sidebarEmojiFetcher{
		emoji.EmojiCacheKey(customs["partyparrot"]): {
			Cells: goimage.Pt(2, 1),
			Lines: []string{imageSGR + "\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"},
		},
	}

	a := newTestApp(t, withChannels(sidebar.ChannelItem{ID: "C1", Name: "handbook", Type: "channel", Section: "S1"}))
	a.sidebar.SetSectionsProvider(sidebarSections{{ID: "S1", Name: "Docs", Emoji: "partyparrot", Type: "standard"}})
	a.SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: ff}, Cells: 2})

	if frame := a.View().Content; !strings.Contains(frame, ":partyparrot: Docs") {
		t.Fatalf("before custom emoji load the header should show the text form:\n%s", frame)
	}
	a.SetCustomEmoji(customs)
	frame := a.View().Content
	if !strings.Contains(frame, imageSGR+"\U0010EEEE") {
		t.Errorf("custom section emoji not drawn as an image after SetCustomEmoji:\n%q", frame)
	}
	if strings.Contains(frame, ":partyparrot:") {
		t.Errorf("header still shows the text form:\n%s", frame)
	}
}

// The debounced emoji invalidation reaches the sidebar: a fetch that
// lands turns a cold reservation into the image on the next frame.
func TestSidebarEmoji_InvalidationTickRebuildsTheSidebar(t *testing.T) {
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })
	url, _ := emoji.URLForShortcode("orange_book", nil)
	const imageSGR = "\x1b[38;2;0;0;44m"
	ff := sidebarEmojiFetcher{}

	a := newTestApp(t, withChannels(sidebar.ChannelItem{ID: "C1", Name: "handbook", Type: "channel", Section: "S1"}))
	a.sidebar.SetSectionsProvider(sidebarSections{{ID: "S1", Name: "Docs", Emoji: "orange_book", Type: "standard"}})
	a.SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: ff}, Cells: 2})
	if frame := a.View().Content; strings.Contains(frame, imageSGR) || !strings.Contains(ansi.Strip(frame), "▾    Docs") {
		t.Fatalf("precondition: the cold header reserves two blank cells before the name:\n%s", frame)
	}

	// The fetch lands: the image is warm, but the sidebar's cached rows
	// still hold the reservation until the tick.
	ff[emoji.EmojiCacheKey(url)] = imgpkg.Render{Cells: goimage.Pt(2, 1), Lines: []string{imageSGR + "\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"}}
	if frame := a.View().Content; strings.Contains(frame, imageSGR) {
		t.Fatalf("precondition: the cached frame keeps the reservation until invalidated:\n%q", frame)
	}
	_, _ = a.Update(emojiInvalidateMsg{})
	if frame := a.View().Content; !strings.Contains(frame, imageSGR+"\U0010EEEE") {
		t.Errorf("after the invalidation tick the frame does not draw the image:\n%q", frame)
	}
}
