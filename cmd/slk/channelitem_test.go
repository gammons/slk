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
