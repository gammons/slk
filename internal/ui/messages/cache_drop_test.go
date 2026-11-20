package messages

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

func cacheMsgs() []MessageItem {
	return []MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "ann", Text: "hello", Timestamp: "9:00 AM"},
		{TS: "2.0", UserID: "U2", UserName: "bob", Text: "see <@U1> and <#C9>", Timestamp: "9:01 AM"},
		{TS: "3.0", UserID: "U3", UserName: "cara", Text: "plain :wave:", Timestamp: "9:02 AM"},
	}
}

func primed(t *testing.T) *Model {
	t.Helper()
	m := New(cacheMsgs(), "general")
	_ = m.View(24, 80)
	if m.cache == nil {
		t.Fatal("cache was not built")
	}
	return m
}

func lineAddr(m *Model, ts string) *string {
	i := m.entryIdx(ts)
	if i < 0 || len(m.list.Entries()[i].Lines) == 0 {
		return nil
	}
	return &m.list.Entries()[i].Lines[0]
}

func TestPatchUserName_LeavesUnrelatedRows(t *testing.T) {
	m := primed(t)
	keep := lineAddr(m, "3.0")
	m.PatchUserName("U1", "alice")
	if m.cache == nil {
		t.Fatal("full cache drop")
	}
	if _, ok := m.staleEntries["3.0"]; ok {
		t.Fatal("unrelated row marked stale")
	}
	_ = m.View(24, 80)
	if lineAddr(m, "3.0") != keep {
		t.Fatal("unrelated row was rebuilt")
	}
	if view := ansi.Strip(m.View(24, 80)); !strings.Contains(view, "alice") {
		t.Fatalf("resolved name did not render:\n%s", view)
	}
}

func TestPatchUserName_UnknownUserDoesNotDrop(t *testing.T) {
	m := primed(t)
	ver := m.Version()
	m.PatchUserName("U99", "zed")
	if m.cache == nil || len(m.staleEntries) != 0 {
		t.Fatalf("cache=%v stale=%v; a user this channel does not show must not rebuild", m.cache == nil, m.staleEntries)
	}
	if m.Version() != ver {
		t.Fatal("version bumped for a name nothing on screen uses")
	}
	if m.ResolveUserName("U99") != "zed" {
		t.Fatal("name was not recorded")
	}
}

func TestPatchUserStatus_OnlyStalesAuthor(t *testing.T) {
	m := primed(t)
	keep := lineAddr(m, "3.0")
	m.PatchUserStatus("U1", peerstatus.Status{Emoji: ":calendar:", Text: "Busy"})
	if m.cache == nil {
		t.Fatal("full cache drop")
	}
	if _, ok := m.staleEntries["1.0"]; !ok {
		t.Fatal("author row not stale")
	}
	if _, ok := m.staleEntries["2.0"]; ok || m.staleEntries != nil && len(m.staleEntries) != 1 {
		t.Fatalf("stale = %v, want only the author", m.staleEntries)
	}
	_ = m.View(24, 80)
	if lineAddr(m, "3.0") != keep {
		t.Fatal("unrelated row was rebuilt")
	}
	if view := ansi.Strip(m.View(24, 80)); !strings.Contains(view, emojiutil.CodeMap()[":calendar:"]) {
		t.Fatalf("status did not render:\n%s", view)
	}
}

func TestSetUserNames_SameMapKeepsCache(t *testing.T) {
	m := primed(t)
	names := map[string]string{"U1": "ann", "U2": "bob"}
	m.SetUserNames(names)
	// The primed pane had no names. One wholesale replace drops.
	_ = m.View(24, 80)
	ver := m.Version()
	m.SetUserNames(names)
	if m.cache == nil || m.Version() != ver {
		t.Fatal("republishing the same name map rebuilt the pane")
	}
	m.SetUserNames(map[string]string{"U1": "ann", "U2": "bob", "U9": "zoe"})
	if _, ok := m.staleEntries["2.0"]; ok {
		t.Fatal("a name nobody mentions was marked stale")
	}
	if m.cache == nil {
		t.Fatal("adding an unused name dropped the cache")
	}
}

func TestSetChannelNames_OnlyStalesTheMention(t *testing.T) {
	m := primed(t)
	keep := lineAddr(m, "1.0")
	m.SetChannelNames(map[string]string{"C9": "general"})
	if m.cache == nil {
		t.Fatal("full cache drop")
	}
	if _, ok := m.staleEntries["2.0"]; !ok {
		t.Fatal("the <#C9> row was not marked stale")
	}
	if _, ok := m.staleEntries["1.0"]; ok {
		t.Fatal("a row with no channel mention was marked stale")
	}
	_ = m.View(24, 80)
	if lineAddr(m, "1.0") != keep {
		t.Fatal("unrelated row was rebuilt")
	}
	if view := ansi.Strip(m.View(24, 80)); !strings.Contains(view, "#general") && !strings.Contains(view, "general") {
		t.Fatalf("channel name did not render:\n%s", view)
	}
}

func TestSetLastReadTS_SameGapKeepsCache(t *testing.T) {
	m := primed(t)
	m.SetLastReadTS("1.0")
	if m.cache != nil {
		t.Fatal("moving the landmark onto a message should rebuild")
	}
	_ = m.View(24, 80)
	ver := m.Version()
	// Still the first unread message: "2.0" is the first TS greater than both.
	m.SetLastReadTS("1.5")
	if m.cache == nil || m.Version() != ver {
		t.Fatal("a mark that does not move the landmark rebuilt the pane")
	}
	m.SetLastReadTS("2.0")
	if m.cache != nil {
		t.Fatal("moving the landmark past 2.0 should rebuild")
	}
}

func TestHandleEmojiImageReady_SkipsUnrelatedURL(t *testing.T) {
	m := primed(t)
	ver := m.Version()
	m.HandleEmojiImageReady("https://emoji.example/not-in-channel.png")
	if m.cache == nil || len(m.staleEntries) != 0 || m.Version() != ver {
		t.Fatalf("unrelated emoji dropped the pane: cacheNil=%v stale=%v ver %d->%d",
			m.cache == nil, m.staleEntries, ver, m.Version())
	}
}

func TestHandleEmojiImageReady_StalesOnlyTheMessage(t *testing.T) {
	m := primed(t)
	url, ok := emojiutil.URLForShortcode("wave", nil)
	if !ok || url == "" {
		t.Fatal("no URL for :wave:")
	}
	keep := lineAddr(m, "1.0")
	m.HandleEmojiImageReady(url)
	if m.cache == nil {
		t.Fatal("full cache drop")
	}
	if _, ok := m.staleEntries["3.0"]; !ok {
		t.Fatal(":wave: row was not marked stale")
	}
	if _, ok := m.staleEntries["1.0"]; ok {
		t.Fatal("a row without the emoji was marked stale")
	}
	_ = m.View(24, 80)
	if lineAddr(m, "1.0") != keep {
		t.Fatal("unrelated row was rebuilt")
	}
}

func TestHandleEmojiImageReady_EmptyURLDrops(t *testing.T) {
	m := primed(t)
	m.HandleEmojiImageReady("")
	if m.cache != nil {
		t.Fatal("an unknown emoji arrival should still rebuild the pane")
	}
}

func TestExpireStatuses_OnlyStalesThatAuthor(t *testing.T) {
	m := primed(t)
	soon := time.Now().Add(-time.Minute)
	m.SetUserStatuses(map[string]peerstatus.Status{
		"U1": {Emoji: ":calendar:", Expires: soon},
		"U3": {Emoji: ":wave:", Expires: time.Now().Add(time.Hour)},
	})
	_ = m.View(24, 80)
	keep := lineAddr(m, "3.0")
	if !m.ExpireStatuses(time.Now()) {
		t.Fatal("expected U1's status to expire")
	}
	if m.cache == nil {
		t.Fatal("full cache drop")
	}
	if _, ok := m.staleEntries["1.0"]; !ok {
		t.Fatal("expired author's row was not stale")
	}
	if _, ok := m.staleEntries["3.0"]; ok {
		t.Fatal("a still-live status was marked stale")
	}
	_ = m.View(24, 80)
	if lineAddr(m, "3.0") != keep {
		t.Fatal("unrelated row was rebuilt")
	}
}
