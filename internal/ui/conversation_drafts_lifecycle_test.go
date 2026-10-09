package ui

// Tests for draft preservation across conversation changes: each
// channel/DM and each thread keeps its own unsent text + attachments,
// keyed by (workspace, channel, thread). Switching away stashes the
// visible composer; switching back restores text and attachments.
//
// These tests drive the real App.Update paths (SetInitialChannel,
// ChannelSelectedMsg, WorkspaceSwitchedMsg, openThreadPanel,
// CloseThread) rather than composing sub-model calls, so they pin the
// wiring between the App reducers and the composer's draft store.

import (
	"testing"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// TestConversationDrafts_SwitchBetweenChannels keeps a channel draft
// (text + attachment) and a DM draft independent across switches.
func TestConversationDrafts_SwitchBetweenChannels(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))
	a.SetInitialChannel("C1", "alpha", nil)
	a.compose.SetValue("alpha draft")
	a.compose.AddAttachment(core.PendingAttachment{Filename: "a.txt", Size: 1})

	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})
	if got := a.compose.Value(); got != "" {
		t.Fatalf("D1 compose should start empty, got %q", got)
	}
	if got := a.compose.Attachments(); len(got) != 0 {
		t.Fatalf("D1 compose should have no attachments, got %d", len(got))
	}

	a.compose.SetValue("dm draft")

	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})
	if got := a.compose.Value(); got != "alpha draft" {
		t.Fatalf("C1 draft not restored, got %q", got)
	}
	atts := a.compose.Attachments()
	if len(atts) != 1 || atts[0].Filename != "a.txt" {
		t.Fatalf("C1 attachment not restored, got %+v", atts)
	}

	_, _ = a.Update(ChannelSelectedMsg{ID: "D1", Name: "alice", Type: "dm"})
	if got := a.compose.Value(); got != "dm draft" {
		t.Fatalf("D1 draft not restored, got %q", got)
	}
}

// TestConversationDrafts_SwitchBetweenWorkspaces keeps same-ID channel
// drafts independent across workspaces, so a workspace switch does not
// bleed one team's draft into another.
func TestConversationDrafts_SwitchBetweenWorkspaces(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))
	a.SetInitialChannel("C1", "alpha", nil)
	a.compose.SetValue("team one")
	a.compose.AddAttachment(core.PendingAttachment{Filename: "one.txt", Size: 1})

	_, _ = a.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "other", Type: "channel"}},
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "other", Type: "channel"})

	if got := a.compose.Value(); got != "" {
		t.Fatalf("T2 C1 compose should start empty, got %q", got)
	}
	if got := a.compose.Attachments(); len(got) != 0 {
		t.Fatalf("T2 C1 should have no attachments, got %d", len(got))
	}

	a.compose.SetValue("team two")

	_, _ = a.Update(WorkspaceSwitchedMsg{
		TeamID:   "T1",
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "alpha", Type: "channel"}},
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "alpha", Type: "channel"})
	if got := a.compose.Value(); got != "team one" {
		t.Fatalf("T1 draft not restored, got %q", got)
	}
	if got := a.compose.Attachments(); len(got) != 1 || got[0].Filename != "one.txt" {
		t.Fatalf("T1 attachment not restored, got %+v", got)
	}

	_, _ = a.Update(WorkspaceSwitchedMsg{
		TeamID:   "T2",
		Channels: []sidebar.ChannelItem{{ID: "C1", Name: "other", Type: "channel"}},
	})
	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "other", Type: "channel"})
	if got := a.compose.Value(); got != "team two" {
		t.Fatalf("T2 draft not restored, got %q", got)
	}
}

// TestConversationDrafts_ThreadsPerChannelAndThread keeps thread drafts
// keyed by (channel, thread), independent of each other and of the main
// channel composer. Closing and reopening a thread restores its draft.
func TestConversationDrafts_ThreadsPerChannelAndThread(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withWindowSize(200, 60))
	a.SetInitialChannel("C1", "alpha", nil)
	a.compose.SetValue("main draft")

	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	a.threadCompose.SetValue("thread one")
	a.threadCompose.AddAttachment(core.PendingAttachment{Filename: "t.txt", Size: 2})

	a.openThreadPanel(messages.MessageItem{TS: "200.0"}, "C1", "200.0")
	if got := a.threadCompose.Value(); got != "" {
		t.Fatalf("C1 thread 200 should start empty, got %q", got)
	}
	a.threadCompose.SetValue("thread two")

	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C2", "100.0")
	if got := a.threadCompose.Value(); got != "" {
		t.Fatalf("C2 thread 100 should start empty, got %q", got)
	}
	a.threadCompose.SetValue("other channel")

	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	if got := a.threadCompose.Value(); got != "thread one" {
		t.Fatalf("C1 thread 100 not restored, got %q", got)
	}
	a.openThreadPanel(messages.MessageItem{TS: "200.0"}, "C1", "200.0")
	if got := a.threadCompose.Value(); got != "thread two" {
		t.Fatalf("C1 thread 200 not restored, got %q", got)
	}
	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C2", "100.0")
	if got := a.threadCompose.Value(); got != "other channel" {
		t.Fatalf("C2 thread 100 not restored, got %q", got)
	}

	// Reopening C1 thread 100 after closing restores text + attachment,
	// and leaves the main channel composer's draft untouched.
	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	a.CloseThread()
	a.openThreadPanel(messages.MessageItem{TS: "100.0"}, "C1", "100.0")
	if got := a.threadCompose.Value(); got != "thread one" {
		t.Fatalf("C1 thread 100 not restored after reopen, got %q", got)
	}
	if got := a.threadCompose.Attachments(); len(got) != 1 || got[0].Filename != "t.txt" {
		t.Fatalf("C1 thread 100 attachment not restored after reopen, got %+v", got)
	}
	if got := a.compose.Value(); got != "main draft" {
		t.Fatalf("main compose draft changed to %q", got)
	}
}
