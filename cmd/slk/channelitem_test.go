package main

import (
	"testing"

	"github.com/gammons/slk/internal/config"
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

// fakeStore mocks the parts of *service.SectionStore the resolver uses.
type fakeStore struct {
	ready   bool
	mapping map[string]string // channelID → sectionID
}

func (f *fakeStore) Ready() bool { return f.ready }
func (f *fakeStore) SectionForChannel(id string) (string, bool) {
	if !f.ready {
		return "", false
	}
	s, ok := f.mapping[id]
	return s, ok
}

func TestBuildChannelItem_StoreReady_StoreWins(t *testing.T) {
	cfg := config.Config{
		Sections: map[string]config.SectionDef{
			"Globbed": {Channels: []string{"alerts*"}, Order: 1},
		},
	}
	wctx := &WorkspaceContext{
		SectionStore:      &fakeStore{ready: true, mapping: map[string]string{"C1": "L_SLACK"}},
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
	wctx := &WorkspaceContext{
		SectionStore:      &fakeStore{ready: true, mapping: map[string]string{}},
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
	wctx := &WorkspaceContext{
		SectionStore:      &fakeStore{ready: false, mapping: map[string]string{"C1": "L_SLACK"}},
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
