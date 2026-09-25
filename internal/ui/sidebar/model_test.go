package sidebar

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui/messages"
)

func TestSidebarView(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "alice", Type: "dm", Presence: "active"},
	}

	m := New(channels)
	// The Channels section starts collapsed by default; expand it so
	// the per-channel rows show up in the rendered view.
	m.ToggleCollapse("Channels")
	view := m.View(20, 25) // height=20, width=25

	if !strings.Contains(view, "general") {
		t.Error("expected 'general' in view")
	}
	if !strings.Contains(view, "random") {
		t.Error("expected 'random' in view")
	}
}

func TestSidebarNavigation(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	// Expand the Channels section so j/k can reach the channel rows.
	m.ToggleCollapse("Channels")

	// Nav order: Threads → Activity → "Channels" header → C1 → C2 → C3.
	m.MoveDown() // onto the Activity row
	m.MoveDown() // onto the "Channels" section header
	if name, ok := m.IsSectionHeaderSelected(); !ok || name != "Channels" {
		t.Errorf("expected Channels header selected, got name=%q ok=%v", name, ok)
	}

	m.MoveDown()
	if m.SelectedID() != "C1" {
		t.Errorf("expected C1, got %q", m.SelectedID())
	}

	m.MoveDown()
	if m.SelectedID() != "C2" {
		t.Errorf("expected C2 after move down, got %q", m.SelectedID())
	}

	m.MoveDown()
	m.MoveDown() // should stop at bottom (C3)
	if m.SelectedID() != "C3" {
		t.Errorf("expected C3 at bottom, got %q", m.SelectedID())
	}

	m.MoveUp()
	if m.SelectedID() != "C2" {
		t.Errorf("expected C2 after move up, got %q", m.SelectedID())
	}
}

func TestThreadsItem_DefaultSelected(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	if !m.IsThreadsSelected() {
		t.Errorf("expected Threads entry to be selected by default (top of list)")
	}
}

func TestThreadsItem_MoveDownLeavesIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "design", Type: "channel"},
	})
	m.ToggleCollapse("Channels")
	m.MoveDown() // Activity
	m.MoveDown() // header
	m.MoveDown() // first channel
	if m.IsThreadsSelected() {
		t.Errorf("MoveDown should leave the Threads entry")
	}
	item, ok := m.SelectedItem()
	if !ok || item.ID != "C1" {
		t.Errorf("first channel should be selected, got %+v ok=%v", item, ok)
	}
}

func TestThreadsItem_MoveUpReturnsToIt(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
	})
	m.ToggleCollapse("Channels")
	m.MoveDown() // header
	m.MoveDown() // C1
	if m.IsThreadsSelected() {
		t.Fatalf("precondition: should be on a channel")
	}
	m.MoveUp() // back to header
	m.MoveUp() // back to Threads
	if !m.IsThreadsSelected() {
		t.Errorf("MoveUp from first channel should land on Threads entry")
	}
}

func TestThreadsItem_UnreadBadgeRenders(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	out := m.View(10, 30)
	if !strings.Contains(out, "Threads") {
		t.Errorf("View should contain 'Threads': %q", out)
	}
	// Find the line containing "Threads" and assert the badge glyph and count
	// appear together as the literal substring "•3".
	var threadsLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Threads") {
			threadsLine = line
			break
		}
	}
	if threadsLine == "" {
		t.Fatalf("no line containing 'Threads' in view: %q", out)
	}
	if !strings.Contains(threadsLine, "•3") {
		t.Errorf("Threads line should contain badge '•3', got %q", threadsLine)
	}
}

func TestThreadsItem_VisibleWhenNoChannels(t *testing.T) {
	m := New(nil)
	out := m.View(10, 30)
	if !strings.Contains(out, "Threads") {
		t.Errorf("View should contain 'Threads' even when there are no channels: %q", out)
	}
	if !m.IsThreadsSelected() {
		t.Errorf("Threads row should still be selected when there are no channels")
	}
}

func TestSetThreadsUnreadCount_NegativeClampsToZero(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(-5)
	if got := m.ThreadsUnreadCount(); got != 0 {
		t.Errorf("negative count should clamp to 0, got %d", got)
	}
}

func TestSetThreadsUnreadCount_NoChangeNoVersionBump(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	v1 := m.Version()
	m.SetThreadsUnreadCount(3) // identical -- no state change
	v2 := m.Version()
	if v1 != v2 {
		t.Errorf("identical SetThreadsUnreadCount should not bump version, got %d -> %d", v1, v2)
	}
}

func TestSetThreadsUnreadCount_ZeroRemovesBadge(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	m.SetThreadsUnreadCount(3)
	out := m.View(10, 30)
	if !strings.Contains(out, "•3") {
		t.Fatalf("precondition: badge '•3' should be present, got %q", out)
	}
	m.SetThreadsUnreadCount(0)
	out = m.View(10, 30)
	if strings.Contains(out, "•") {
		t.Errorf("badge glyph '•' should be gone after setting count to 0, got %q", out)
	}
}

func TestThreadsItem_SelectedItemFalseWhenOnThreadsRow(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if _, ok := m.SelectedItem(); ok {
		t.Errorf("SelectedItem should return ok=false when Threads row is selected")
	}
}

func TestThreadsItem_SelectByIDClearsThreadsSelection(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	if !m.IsThreadsSelected() {
		t.Fatal("precondition")
	}
	m.SelectByID("C1")
	if m.IsThreadsSelected() {
		t.Errorf("SelectByID should clear Threads selection")
	}
}

func TestSidebarFilter(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	m.SetFilter("gen")

	visible := m.VisibleItems()
	if len(visible) != 1 {
		t.Errorf("expected 1 filtered result, got %d", len(visible))
	}
	if visible[0].Name != "general" {
		t.Errorf("expected 'general', got %q", visible[0].Name)
	}

	m.SetFilter("")
	visible = m.VisibleItems()
	if len(visible) != 3 {
		t.Errorf("expected 3 items after clear filter, got %d", len(visible))
	}
}

func TestUpsertItem_AddsNewChannel(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})

	m.UpsertItem(ChannelItem{ID: "G1", Name: "alice, bob", Type: "group_dm"})

	items := m.AllItems()
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	found := false
	for _, it := range items {
		if it.ID == "G1" && it.Type == "group_dm" {
			found = true
		}
	}
	if !found {
		t.Errorf("G1 not present after upsert: %+v", items)
	}
}

func TestUpsertItem_UpdatesExistingChannel(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{{ID: "G1", Name: "old name", Type: "group_dm"}})

	m.UpsertItem(ChannelItem{ID: "G1", Name: "new name", Type: "group_dm"})

	items := m.AllItems()
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Name != "new name" {
		t.Errorf("Name = %q, want %q", items[0].Name, "new name")
	}
	// Read state (HasUnread, LastReadTS) used to live on ChannelItem
	// and needed preservation across upserts because Slack's mpim_open
	// / im_created / group_joined payloads don't carry it. Those
	// fields are gone now -- the read-state DB is the single source of
	// truth -- so there's nothing for UpsertItem to preserve and
	// nothing to assert here beyond the descriptive-field overwrite
	// above.
}

func TestRender_UnreadDMRow_KeepsBoldAfterPrefixReset(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"D1": {HasUnread: true}}
	})

	out := m.View(20, 40)

	if !strings.Contains(out, "alice") {
		t.Fatalf("output missing channel name; got: %q", out)
	}
	aliceIdx := strings.Index(out, "alice")
	prefix := out[:aliceIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		t.Skip("no mid-label reset found before name; render path changed")
	}
	afterReset := prefix[lastResetIdx:]
	if !strings.Contains(afterReset, "\x1b[1m") {
		t.Errorf("bold attribute not re-emitted after prefix reset for unread DM row\nafterReset=%q", afterReset)
	}
}

func TestRender_ReadDMRow_DoesNotEmitBoldAfterReset(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active"},
	})

	out := m.View(20, 40)
	aliceIdx := strings.Index(out, "alice")
	if aliceIdx < 0 {
		t.Fatalf("output missing channel name")
	}
	prefix := out[:aliceIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		return
	}
	afterReset := prefix[lastResetIdx:]
	if strings.Contains(afterReset, "\x1b[1m") {
		t.Errorf("read DM row unexpectedly emitted bold after reset; afterReset=%q", afterReset)
	}
}

// TestRender_UnreadThreadsRow_KeepsBoldAfterPrefixReset is the Threads-row
// counterpart to TestRender_UnreadDMRow_KeepsBoldAfterPrefixReset. The ⚑
// glyph emits a mid-label ANSI reset; an unread Threads row must re-emit
// \x1b[1m so "Threads" stays bold past the reset.
func TestRender_UnreadThreadsRow_KeepsBoldAfterPrefixReset(t *testing.T) {
	m := New(nil)
	m.SetThreadsUnreadCount(3)
	out := m.View(20, 40)

	threadsIdx := strings.Index(out, "Threads")
	if threadsIdx < 0 {
		t.Fatalf("output missing Threads label; got: %q", out)
	}
	prefix := out[:threadsIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		t.Skip("no mid-label reset found before Threads; render path changed")
	}
	afterReset := prefix[lastResetIdx:]
	if !strings.Contains(afterReset, "\x1b[1m") {
		t.Errorf("bold attribute not re-emitted after ⚑ reset for unread Threads row\nafterReset=%q", afterReset)
	}
}

// TestRender_ReadThreadsRow_DoesNotEmitBoldAfterReset locks in the negative
// case: a Threads row with zero unread must NOT emit \x1b[1m after the
// prefix reset, so the muted ChannelNormal style stays muted.
func TestRender_ReadThreadsRow_DoesNotEmitBoldAfterReset(t *testing.T) {
	m := New(nil)
	m.SetThreadsUnreadCount(0)
	out := m.View(20, 40)

	threadsIdx := strings.Index(out, "Threads")
	if threadsIdx < 0 {
		t.Fatalf("output missing Threads label")
	}
	prefix := out[:threadsIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		return
	}
	afterReset := prefix[lastResetIdx:]
	if strings.Contains(afterReset, "\x1b[1m") {
		t.Errorf("read Threads row unexpectedly emitted bold after reset; afterReset=%q", afterReset)
	}
}

// TestRender_ReadDMRow_RestoresMutedFgAfterPrefixReset locks in the
// bug fix: a read DM with a colored presence prefix (● green for
// online, ○ grey for away) emits a mid-label ANSI reset right after
// the prefix glyph. Before the fix, ReapplyBgAfterResets re-injected
// the BRIGHT SidebarFgANSI() for everything after that reset,
// overriding ChannelNormal's muted foreground and making read DM
// names render visibly brighter than read channel names (which have
// no inline ANSI in their prefix and stay muted via the lipgloss
// outer wrap).
//
// After the fix, the row attrs must re-inject the MUTED foreground
// (matching ChannelNormal) for the post-prefix span, so read DMs
// match read channels in brightness.
func TestRender_ReadDMRow_RestoresMutedFgAfterPrefixReset(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active"},
	})
	out := m.View(20, 40)

	aliceIdx := strings.Index(out, "alice")
	if aliceIdx < 0 {
		t.Fatalf("output missing channel name; got: %q", out)
	}
	prefix := out[:aliceIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		t.Skip("no mid-label reset found before name; render path changed")
	}
	afterReset := prefix[lastResetIdx:]

	bright := messages.SidebarFgANSI()
	muted := messages.SidebarMutedFgANSI()
	if bright == muted {
		// Themes where SidebarText == SidebarTextMuted can't exhibit
		// the bug. The default themes have distinct values, so this
		// branch only protects against a degenerate test environment.
		t.Skip("theme has equal SidebarText / SidebarTextMuted; nothing to assert")
	}
	if strings.Contains(afterReset, bright) {
		t.Errorf("read DM row re-injected the BRIGHT SidebarFgANSI after the prefix reset; this is the bug — read DM names render brighter than read channel names.\nbright=%q\nafterReset=%q", bright, afterReset)
	}
	if !strings.Contains(afterReset, muted) {
		t.Errorf("read DM row did not re-inject the muted SidebarMutedFgANSI after the prefix reset; the name will render at the terminal-default foreground.\nmuted=%q\nafterReset=%q", muted, afterReset)
	}
}

// TestRender_UnreadDMRow_RestoresBrightFgAfterPrefixReset is the
// positive case for the same fix: unread rows DO want the bright
// SidebarFgANSI re-injected after the prefix reset (matching
// ChannelUnread's bright foreground), so the name pops.
func TestRender_UnreadDMRow_RestoresBrightFgAfterPrefixReset(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active"},
	})
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"D1": {HasUnread: true}}
	})
	out := m.View(20, 40)

	aliceIdx := strings.Index(out, "alice")
	if aliceIdx < 0 {
		t.Fatalf("output missing channel name; got: %q", out)
	}
	prefix := out[:aliceIdx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		t.Skip("no mid-label reset found before name; render path changed")
	}
	afterReset := prefix[lastResetIdx:]

	bright := messages.SidebarFgANSI()
	muted := messages.SidebarMutedFgANSI()
	if bright == muted {
		t.Skip("theme has equal SidebarText / SidebarTextMuted; nothing to assert")
	}
	if !strings.Contains(afterReset, bright) {
		t.Errorf("unread DM row did not re-inject the bright SidebarFgANSI after the prefix reset; the name will render at the terminal-default foreground.\nbright=%q\nafterReset=%q", bright, afterReset)
	}
	if strings.Contains(afterReset, muted) {
		t.Errorf("unread DM row unexpectedly re-injected the muted foreground after the prefix reset\nmuted=%q\nafterReset=%q", muted, afterReset)
	}
}

// TestRender_ReadGroupDMRow_RestoresMutedFgAfterPrefixReset:
// group DMs use the same styled-prefix pattern as 1:1 DMs (a grey
// PresenceAway-colored "● "), so they hit the same bug. Lock in the
// muted re-inject for them too.
func TestRender_ReadGroupDMRow_RestoresMutedFgAfterPrefixReset(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{
		{ID: "G1", Name: "design-trio", Type: "group_dm"},
	})
	out := m.View(20, 40)

	idx := strings.Index(out, "design-trio")
	if idx < 0 {
		t.Fatalf("output missing channel name; got: %q", out)
	}
	prefix := out[:idx]
	lastResetIdx := strings.LastIndex(prefix, "\x1b[m")
	if lastResetIdx == -1 {
		t.Skip("no mid-label reset found before name; render path changed")
	}
	afterReset := prefix[lastResetIdx:]
	bright := messages.SidebarFgANSI()
	if bright == messages.SidebarMutedFgANSI() {
		t.Skip("theme has equal SidebarText / SidebarTextMuted")
	}
	if strings.Contains(afterReset, bright) {
		t.Errorf("read group_dm re-injected the bright SidebarFgANSI after the prefix reset\nafterReset=%q", afterReset)
	}
}

type fakeProvider struct {
	ready    bool
	sections []SectionMeta
}

func (f *fakeProvider) Ready() bool                         { return f.ready }
func (f *fakeProvider) OrderedSlackSections() []SectionMeta { return f.sections }

func TestOrderedSections_SlackMode_HonorsLinkedListOrder(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "ch1", Type: "channel", Section: "B"},
		{ID: "C2", Name: "ch2", Type: "channel", Section: "A"},
		{ID: "D1", Name: "u", Type: "dm", Section: "DMS"},
	}
	provider := &fakeProvider{
		ready: true,
		sections: []SectionMeta{
			{ID: "A", Name: "Alerts", Type: "standard"},
			{ID: "B", Name: "Books", Type: "standard"},
			{ID: "DMS", Name: "Direct Messages", Type: "direct_messages"},
		},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	got := slackModeNavHeaders(&m)
	want := []string{"A", "B", "DMS"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSlackMode_UnclaimedItemFallsToTypeDefault(t *testing.T) {
	// Item D1 is type "dm" with no Section; should bucket into the
	// provider's direct_messages-type section.
	items := []ChannelItem{
		{ID: "C1", Name: "ch1", Type: "channel", Section: "A"},
		{ID: "D1", Name: "u", Type: "dm"}, // no Section
	}
	provider := &fakeProvider{
		ready: true,
		sections: []SectionMeta{
			{ID: "A", Name: "Alerts", Type: "standard"},
			{ID: "DMS", Name: "Direct Messages", Type: "direct_messages"},
		},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	// Force the DM section open (it defaults to expanded).
	got := slackModeNavHeaders(&m)
	if len(got) != 2 {
		t.Fatalf("got %v, want both sections present", got)
	}
	// Find the DM and confirm it's bucketed under the DMS section.
	dmIdx := -1
	for i, n := range m.nav {
		if n.kind == navChannel && m.items[m.filtered[n.fi]].ID == "D1" {
			dmIdx = i
			break
		}
	}
	if dmIdx < 0 {
		t.Fatalf("D1 not in nav: %+v", m.nav)
	}
	// Walk backwards to find the most recent header before D1.
	headerBefore := ""
	for i := dmIdx - 1; i >= 0; i-- {
		if m.nav[i].kind == navHeader {
			headerBefore = m.nav[i].header
			break
		}
	}
	if headerBefore != "DMS" {
		t.Errorf("D1 bucketed under %q, want DMS (direct_messages-type fallback)", headerBefore)
	}
}

func TestOrderedSections_ConfigMode_UnchangedWhenNoProvider(t *testing.T) {
	// Regression guard: existing config-glob behavior must be intact.
	items := []ChannelItem{
		{ID: "C1", Name: "ch1", Type: "channel", Section: "Custom", SectionOrder: 1},
		{ID: "C2", Name: "ch2", Type: "channel"},
		{ID: "D1", Name: "u", Type: "dm"},
	}
	m := New(items)
	got := orderedSections(m.items, m.filtered)
	// Custom first, then DMs, then Channels.
	want := []string{"Custom", "Direct Messages", "Channels"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// slackModeNavHeaders returns the header strings (section IDs in
// Slack mode) currently in the model's nav list.
func slackModeNavHeaders(m *Model) []string {
	var out []string
	for _, n := range m.nav {
		if n.kind == navHeader {
			out = append(out, n.header)
		}
	}
	return out
}

func TestSectionHeader_RendersEmojiPrefix(t *testing.T) {
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
	provider := &fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Emoji: "rocket", Type: "standard"}},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	out := m.View(20, 40)
	if !strings.Contains(out, "Alerts") {
		t.Errorf("missing section name in output:\n%s", out)
	}
	// kyokomi/emoji renders :rocket: as 🚀. If unresolved (unknown
	// shortcode), the raw :rocket: stays. Either is acceptable.
	if !strings.Contains(out, "🚀") && !strings.Contains(out, ":rocket:") {
		t.Errorf("missing emoji prefix in output:\n%s", out)
	}
}

// Slack sends the stars section with name="" (it's a built-in section,
// not user-named). The sidebar must fall back to a friendly header
// ("Starred") rather than rendering "(unnamed)".
func TestSectionHeader_StarsSection_FallsBackToStarred(t *testing.T) {
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "ST"}}
	provider := &fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "ST", Name: "", Type: "stars"}},
	}
	m := New(items)
	m.SetSectionsProvider(provider)
	out := m.View(20, 40)
	if !strings.Contains(out, "Starred") {
		t.Errorf("stars section with empty name should fall back to \"Starred\"; got:\n%s", out)
	}
	if strings.Contains(out, "(unnamed)") {
		t.Errorf("stars section should not render the generic \"(unnamed)\" fallback; got:\n%s", out)
	}
}

func TestCollapseByID_PreservedAcrossRename(t *testing.T) {
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
	p := &fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Type: "standard"}},
	}
	m := New(items)
	m.SetSectionsProvider(p)
	m.ToggleCollapse("A")
	if !m.IsCollapsed("A") {
		t.Fatalf("collapse failed (set on A then queried A in same mode)")
	}
	// Rename: provider returns the same ID with a new name.
	p.sections = []SectionMeta{{ID: "A", Name: "Renamed", Type: "standard"}}
	m.SetSectionsProvider(p) // triggers a rebuild
	if !m.IsCollapsed("A") {
		t.Errorf("collapse state lost after rename (must key by ID, not by displayed name)")
	}
}

func TestCollapseByID_IndependentFromConfigMode(t *testing.T) {
	// Switching back from Slack mode to config mode should fall through
	// to the name-keyed collapse map; collapseByID entries shouldn't
	// leak into config-mode behavior.
	items := []ChannelItem{{ID: "C1", Name: "ch1", Type: "channel", Section: "A"}}
	p := &fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "A", Name: "Alerts", Type: "standard"}},
	}
	m := New(items)
	m.SetSectionsProvider(p)
	m.ToggleCollapse("A")      // collapse via ID
	m.SetSectionsProvider(nil) // back to config mode
	// Now "A" is just a string in config mode; whether it's collapsed
	// depends on the name-keyed `collapsed` map (which is the default
	// state set in New). The ID-mode collapse must NOT bleed into
	// config-mode IsCollapsed lookups.
	// In config mode the default Channels section starts collapsed,
	// but a custom "A" name has not been touched so it should be expanded.
	if m.IsCollapsed("A") {
		t.Errorf("collapse state for ID 'A' bled into config mode")
	}
}

// TestView_RendersDotFromReadStateReader confirms the unread dot
// indicator is driven by the readStateReader callback (the DB),
// not by ChannelItem.UnreadCount. C1 has HasUnread=true via the
// reader so its row should render the "●" glyph; C2 has
// HasUnread=false and should not.
func TestView_RendersDotFromReadStateReader(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
	})
	// Channels section starts collapsed by default; expand so the
	// per-row dots are rendered (the collapsed-header aggregate
	// badge uses a different glyph "•").
	m.ToggleCollapse("Channels")
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{
			"C1": {HasUnread: true},
			"C2": {HasUnread: false},
		}
	})

	out := m.View(20, 30)
	dotCount := strings.Count(out, "●")
	if dotCount != 1 {
		t.Errorf("expected exactly 1 unread dot, got %d. Output:\n%s", dotCount, out)
	}
}

// TestRebuildFilter_OrdersChannelsWithinSectionByChannelOrder verifies
// that within a section, channels with an explicit ChannelOrder > 0
// sort ahead of un-annotated channels (input order), and that
// annotated channels are themselves sorted by ChannelOrder ascending.
// This is the config-mode (use_slack_sections=false) behavior driven
// by the SectionDef "<pattern>:<N>" suffix syntax.
func TestRebuildFilter_OrdersChannelsWithinSectionByChannelOrder(t *testing.T) {
	// Inserted in deliberately scrambled input order. All in the
	// same custom section "Eng" so the section-rank tie-breaker is
	// what's being tested.
	items := []ChannelItem{
		{ID: "C1", Name: "alpha", Type: "channel", Section: "Eng", SectionOrder: 1, ChannelOrder: 0},
		{ID: "C2", Name: "beta", Type: "channel", Section: "Eng", SectionOrder: 1, ChannelOrder: 2},
		{ID: "C3", Name: "gamma", Type: "channel", Section: "Eng", SectionOrder: 1, ChannelOrder: 0},
		{ID: "C4", Name: "delta", Type: "channel", Section: "Eng", SectionOrder: 1, ChannelOrder: 1},
	}
	m := New(items)

	// Collect IDs in filtered (display) order.
	var got []string
	for _, idx := range m.filtered {
		got = append(got, m.items[idx].ID)
	}

	// Expected: annotated first (C4 order=1, C2 order=2), then
	// un-annotated in input order (C1, C3).
	want := []string{"C4", "C2", "C1", "C3"}
	if len(got) != len(want) {
		t.Fatalf("filtered len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

// TestRebuildFilter_NoChannelOrderPreservesInputOrder is a regression
// guard: when every item has ChannelOrder == 0 (the default), the
// existing input-order behavior must be preserved bit-for-bit.
func TestRebuildFilter_NoChannelOrderPreservesInputOrder(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "alpha", Type: "channel", Section: "Eng", SectionOrder: 1},
		{ID: "C2", Name: "beta", Type: "channel", Section: "Eng", SectionOrder: 1},
		{ID: "C3", Name: "gamma", Type: "channel", Section: "Eng", SectionOrder: 1},
	}
	m := New(items)
	var got []string
	for _, idx := range m.filtered {
		got = append(got, m.items[idx].ID)
	}
	want := []string{"C1", "C2", "C3"}
	if len(got) != len(want) {
		t.Fatalf("filtered len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestView_MutedChannelNoDot confirms that a muted channel never
// renders the unread dot even when HasUnread=true. Slack's contract:
// muted = no notification surface.
func TestView_MutedChannelNoDot(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "noisy", Type: "channel", IsMuted: true},
	})
	m.ToggleCollapse("Channels")
	m.SetReadStateReader(func() map[string]cache.ReadState {
		return map[string]cache.ReadState{"C1": {HasUnread: true}}
	})

	out := m.View(20, 30)
	if strings.Count(out, "●") != 0 {
		t.Errorf("muted channel should not show a dot. Output:\n%s", out)
	}
}
