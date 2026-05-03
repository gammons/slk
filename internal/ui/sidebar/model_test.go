package sidebar

import (
	"strings"
	"testing"
)

func TestSidebarView(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 3},
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

	// Nav order: Threads → "Channels" header → C1 → C2 → C3.
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

func TestMarkUnread_IncrementsCount(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 2},
	})
	m.MarkUnread("C1")
	if got := m.Items()[0].UnreadCount; got != 1 {
		t.Errorf("MarkUnread should bump UnreadCount from 0 to 1, got %d", got)
	}
	m.MarkUnread("C2")
	if got := m.Items()[1].UnreadCount; got != 3 {
		t.Errorf("MarkUnread should bump existing count from 2 to 3, got %d", got)
	}
}

func TestMarkUnread_BumpsVersionAndInvalidatesCache(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	// Prime the cache.
	_ = m.View(10, 30)
	v1 := m.Version()
	m.MarkUnread("C1")
	v2 := m.Version()
	if v2 == v1 {
		t.Errorf("MarkUnread should bump version, got %d -> %d", v1, v2)
	}
}

func TestMarkUnread_UnknownChannelIsNoop(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	v1 := m.Version()
	m.MarkUnread("C-does-not-exist")
	v2 := m.Version()
	if v1 != v2 {
		t.Errorf("MarkUnread on unknown channel should not bump version, got %d -> %d", v1, v2)
	}
}

func TestMarkUnread_RendersDotAndBold(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0}})
	// Expand Channels so the channel row itself is rendered (otherwise
	// the unread bump only changes the aggregate badge on the header).
	m.ToggleCollapse("Channels")
	before := m.View(10, 30)
	// Find the line for "general" before bumping.
	var beforeLine string
	for _, line := range strings.Split(before, "\n") {
		if strings.Contains(line, "general") {
			beforeLine = line
			break
		}
	}
	m.MarkUnread("C1")
	after := m.View(10, 30)
	var afterLine string
	for _, line := range strings.Split(after, "\n") {
		if strings.Contains(line, "general") {
			afterLine = line
			break
		}
	}
	if beforeLine == afterLine {
		t.Errorf("expected sidebar render to change after MarkUnread; before=%q after=%q", beforeLine, afterLine)
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

func TestSetUnreadCount_SetsExactValue(t *testing.T) {
	m := New([]ChannelItem{
		{ID: "C1", Name: "general", Section: "Channels"},
	})

	m.SetUnreadCount("C1", 7)

	for _, it := range m.Items() {
		if it.ID == "C1" {
			if it.UnreadCount != 7 {
				t.Errorf("expected UnreadCount=7, got %d", it.UnreadCount)
			}
			return
		}
	}
	t.Fatal("C1 not found in items")
}

func TestSetUnreadCount_Zero_ClearsBadge(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Section: "Channels"}})
	m.MarkUnread("C1")
	m.MarkUnread("C1")
	// preconditions: count is 2.

	m.SetUnreadCount("C1", 0)

	for _, it := range m.Items() {
		if it.ID == "C1" {
			if it.UnreadCount != 0 {
				t.Errorf("expected UnreadCount=0, got %d", it.UnreadCount)
			}
			return
		}
	}
}

func TestSetUnreadCount_UnknownChannel_NoOp(t *testing.T) {
	m := New([]ChannelItem{{ID: "C1", Name: "general", Section: "Channels"}})

	// Should not panic, should not affect existing items.
	m.SetUnreadCount("CDOESNOTEXIST", 5)

	for _, it := range m.Items() {
		if it.ID == "C1" && it.UnreadCount != 0 {
			t.Errorf("untouched item changed: %d", it.UnreadCount)
		}
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
	m.SetItems([]ChannelItem{{ID: "G1", Name: "old name", Type: "group_dm", UnreadCount: 3}})

	m.UpsertItem(ChannelItem{ID: "G1", Name: "new name", Type: "group_dm"})

	items := m.AllItems()
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Name != "new name" {
		t.Errorf("Name = %q, want %q", items[0].Name, "new name")
	}
	// UnreadCount must be preserved on update — Slack's mpim_open does
	// not carry unread state, and clobbering a live count to 0 would
	// erase the indicator we're trying to fix.
	if items[0].UnreadCount != 3 {
		t.Errorf("UnreadCount = %d, want 3 (preserved)", items[0].UnreadCount)
	}
}

func TestUpsertItem_ThenMarkUnread(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{{ID: "C1", Name: "general", Type: "channel"}})
	// Simulate the bug scenario: a new mpdm shows up via mpim_open,
	// then a message arrives. MarkUnread must successfully bump the
	// count on the freshly-upserted item.
	m.UpsertItem(ChannelItem{ID: "G1", Name: "alice, bob", Type: "group_dm"})
	m.MarkUnread("G1")

	items := m.AllItems()
	for _, it := range items {
		if it.ID == "G1" && it.UnreadCount != 1 {
			t.Errorf("G1 UnreadCount = %d, want 1", it.UnreadCount)
		}
	}
}

func TestRender_UnreadDMRow_KeepsBoldAfterPrefixReset(t *testing.T) {
	m := New(nil)
	m.SetItems([]ChannelItem{
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active", UnreadCount: 1},
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
		{ID: "D1", Name: "alice", Type: "dm", Presence: "active", UnreadCount: 0},
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
