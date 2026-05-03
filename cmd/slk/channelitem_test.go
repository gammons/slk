package main

import (
	"context"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/service"
	slk "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

func TestBuildChannelItem_DM(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{"U123": "alice"},
		UserNamesByHandle: map[string]string{"alice": "alice"},
	}
	cfg := config.Config{}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID:   "D1",
				IsIM: true,
				User: "U123",
			},
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.ID != "D1" {
		t.Errorf("ID = %q, want D1", item.ID)
	}
	if item.Type != "dm" {
		t.Errorf("Type = %q, want dm", item.Type)
	}
	if item.Name != "alice" {
		t.Errorf("Name = %q, want alice", item.Name)
	}
	if item.DMUserID != "U123" {
		t.Errorf("DMUserID = %q, want U123", item.DMUserID)
	}
}

func TestBuildChannelItem_GroupDM(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{"alice": "Alice", "bob": "Bob"},
	}
	cfg := config.Config{}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID:     "G1",
				IsMpIM: true,
			},
			Name: "mpdm-alice--bob-1",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Type != "group_dm" {
		t.Errorf("Type = %q, want group_dm", item.Type)
	}
	if item.Name != "Alice, Bob" {
		t.Errorf("Name = %q, want %q", item.Name, "Alice, Bob")
	}
}

func TestBuildChannelItem_Channel(t *testing.T) {
	wctx := &WorkspaceContext{
		BotUserIDs:        map[string]bool{},
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
	}
	cfg := config.Config{}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1"},
			Name:         "general",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Type != "channel" {
		t.Errorf("Type = %q, want channel", item.Type)
	}
	if item.Name != "general" {
		t.Errorf("Name = %q, want general", item.Name)
	}
}

// fakeSectionsClient implements service.SectionsClient for tests; it
// returns a fixed slice of sections so we can construct a real
// *service.SectionStore (Bootstrap-driven) from a known mapping.
type fakeSectionsClient struct {
	sections []slk.SidebarSection
}

func (f *fakeSectionsClient) GetChannelSections(_ context.Context) ([]slk.SidebarSection, error) {
	return f.sections, nil
}

// bootstrappedStore returns a Ready() *service.SectionStore whose
// channelToSection map is built from the supplied (sectionID -> []channelID)
// pairs. All synthetic sections use Type="channels" so they pass
// includeInSidebar's filter (not that it matters for SectionForChannel).
func bootstrappedStore(t *testing.T, mapping map[string][]string) *service.SectionStore {
	t.Helper()
	secs := make([]slk.SidebarSection, 0, len(mapping))
	for id, chans := range mapping {
		secs = append(secs, slk.SidebarSection{
			ID:         id,
			Name:       id,
			Type:       "channels",
			ChannelIDs: chans,
		})
	}
	store := service.NewSectionStore()
	if err := store.Bootstrap(context.Background(), &fakeSectionsClient{sections: secs}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return store
}

func TestBuildChannelItem_StoreReady_StoreWins(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	wctx := &WorkspaceContext{
		SectionStore:      bootstrappedStore(t, map[string][]string{"L_SLACK": {"C1"}}),
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1", NameNormalized: "alerts-prod"},
			Name:         "alerts-prod",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "L_SLACK" {
		t.Errorf("Section = %q, want L_SLACK (store wins over glob)", item.Section)
	}
}

func TestBuildChannelItem_StoreReady_StoreMisses_FallsToGlob(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	// Bootstrap a Ready store with no entry for C1; the resolver should
	// fall through to config-glob matching.
	wctx := &WorkspaceContext{
		SectionStore:      bootstrappedStore(t, map[string][]string{}),
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1"},
			Name:         "alerts-prod",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "Globbed" {
		t.Errorf("Section = %q, want Globbed (store had no match)", item.Section)
	}
}

func TestBuildChannelItem_StoreNil_UsesGlob(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	wctx := &WorkspaceContext{
		SectionStore:      nil,
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1"},
			Name:         "alerts-prod",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "Globbed" {
		t.Errorf("Section = %q, want Globbed", item.Section)
	}
}

func TestBuildChannelItem_StoreNotReady_UsesGlob(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	// Fresh store (never bootstrapped) reports Ready()==false; the
	// resolver must skip it even though we'd otherwise expect a match.
	wctx := &WorkspaceContext{
		SectionStore:      service.NewSectionStore(),
		UserNames:         map[string]string{},
		UserNamesByHandle: map[string]string{},
		BotUserIDs:        map[string]bool{},
	}
	ch := slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: "C1"},
			Name:         "alerts-prod",
		},
	}
	item, _ := buildChannelItem(ch, wctx, cfg, "T1")
	if item.Section != "Globbed" {
		t.Errorf("Section = %q, want Globbed (store not ready, even though it has a mapping)", item.Section)
	}
}
