package main

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/slack-go/slack"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
)

// readStates collects the ReadStateChangedMsg workspace IDs from sent.
func readStates(t *testing.T, sent []tea.Msg) []string {
	t.Helper()
	var out []string
	for _, m := range sent {
		if r, ok := m.(ui.ReadStateChangedMsg); ok {
			out = append(out, r.WorkspaceID)
		}
	}
	return out
}

// TestOnMessage_GridSiblingRailRefresh_Inactive pins that an INACTIVE
// session whose copy of a grid message survived the event dedupe still
// refreshes the rail: one ReadStateChangedMsg, because the receiver
// ignores WorkspaceID and refreshes every dot per message - a
// per-sibling fan-out would repeat the same refresh once per session.
func TestOnMessage_GridSiblingRailRefresh_Inactive(t *testing.T) {
	var sent []tea.Msg
	h := &rtmEventHandler{
		program:      sendFunc(func(msg tea.Msg) { sent = append(sent, msg) }),
		workspaceID:  "T1",
		isActive:     func() bool { return false },
		gridSiblings: func() []string { return []string{"T1", "T2", "E1"} },
	}
	h.OnMessage("C1", "U9", "1.001", "hi", "", "", false, false, nil, slack.Blocks{}, nil, "", "")

	got := readStates(t, sent)
	if len(got) != 1 || got[0] != "T1" {
		t.Errorf("ReadStateChangedMsg workspaces = %v; want exactly [T1] (one refresh, not one per sibling)", got)
	}
}

// TestOnMessage_GridSiblingRailRefresh_Active pins the ACTIVE arm: it
// renders the message and, on a grid, sends exactly one rail refresh
// (its own dot is owned by the NewMessageMsg path; the single message
// covers the siblings because the receiver refreshes every dot).
func TestOnMessage_GridSiblingRailRefresh_Active(t *testing.T) {
	var sent []tea.Msg
	h := &rtmEventHandler{
		program:      sendFunc(func(msg tea.Msg) { sent = append(sent, msg) }),
		workspaceID:  "T1",
		isActive:     func() bool { return true },
		gridSiblings: func() []string { return []string{"T1", "T2", "E1"} },
	}
	h.OnMessage("C1", "U9", "1.001", "hi", "", "", false, false, nil, slack.Blocks{}, nil, "", "")

	rendered := false
	for _, m := range sent {
		if _, ok := m.(ui.NewMessageMsg); ok {
			rendered = true
		}
	}
	if !rendered {
		t.Error("the active handler must still dispatch NewMessageMsg")
	}
	got := readStates(t, sent)
	if len(got) != 1 {
		t.Errorf("ReadStateChangedMsg workspaces = %v; want exactly one (a single refresh covers every rail)", got)
	}
}

// TestOnMessage_NoGrid_RefreshesOnlyOwnWorkspace pins the off-grid
// behaviour: unchanged from before the dedupe landed, the inactive
// arm refreshes only its own workspace.
func TestOnMessage_NoGrid_RefreshesOnlyOwnWorkspace(t *testing.T) {
	var sent []tea.Msg
	h := &rtmEventHandler{
		program:     sendFunc(func(msg tea.Msg) { sent = append(sent, msg) }),
		workspaceID: "T1",
		isActive:    func() bool { return false },
	}
	h.OnMessage("C1", "U9", "1.001", "hi", "", "", false, false, nil, slack.Blocks{}, nil, "", "")

	got := readStates(t, sent)
	if len(got) != 1 || got[0] != "T1" {
		t.Errorf("ReadStateChangedMsg workspaces = %v; want [T1] only", got)
	}
}

// TestOnMessage_MentionBumpedOnceAcrossSiblings pins the grid
// double-delivery order the reviewer described: an inactive sibling's
// copy of a mentioning message runs the handler first, then the active
// socket's copy (never gated) runs it again. The mention badge must
// read 1, not 2.
func TestOnMessage_MentionBumpedOnceAcrossSiblings(t *testing.T) {
	notifyOnceMap.reset()
	mentionOnceMap.reset()
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "CG1", WorkspaceID: "T1", Name: "cg1", Type: "channel"}); err != nil {
		t.Fatal(err)
	}

	newHandler := func(active bool) *rtmEventHandler {
		return &rtmEventHandler{
			program:       sendFunc(func(tea.Msg) {}),
			db:            db,
			workspaceID:   "T1",
			isActive:      func() bool { return active },
			currentUserID: "U1",
			channelTypes:  map[string]string{"CG1": "channel"},
		}
	}
	text := "hey <@U1> look"
	// Inactive sibling's copy first, then the active socket's copy.
	newHandler(false).OnMessage("CG1", "U9", "8.001", text, "", "", false, false, nil, slack.Blocks{}, nil, "", "")
	newHandler(true).OnMessage("CG1", "U9", "8.001", text, "", "", false, false, nil, slack.Blocks{}, nil, "", "")

	state, err := db.GetChannelReadState("CG1")
	if err != nil {
		t.Fatal(err)
	}
	if state.MentionCount != 1 {
		t.Errorf("MentionCount = %d; want 1 (one @-mention delivered twice must badge once)", state.MentionCount)
	}
}

// TestOnReactionAdded_IdempotentAcrossSiblings pins the same order for
// reactions: two deliveries of one reaction_added must leave one user
// and count 1.
func TestOnReactionAdded_IdempotentAcrossSiblings(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "CG1", WorkspaceID: "T1", Name: "cg1", Type: "channel"}); err != nil {
		t.Fatal(err)
	}

	newHandler := func(active bool) *rtmEventHandler {
		return &rtmEventHandler{
			program:     sendFunc(func(tea.Msg) {}),
			db:          db,
			workspaceID: "T1",
			isActive:    func() bool { return active },
		}
	}
	newHandler(false).OnReactionAdded("CG1", "8.001", "U9", "tada")
	newHandler(true).OnReactionAdded("CG1", "8.001", "U9", "tada")

	rows, err := db.GetReactions("8.001", "CG1")
	if err != nil || len(rows) != 1 {
		t.Fatalf("GetReactions = %v, %v; want one row", rows, err)
	}
	if len(rows[0].UserIDs) != 1 || rows[0].UserIDs[0] != "U9" {
		t.Errorf("UserIDs = %v; want [U9] once", rows[0].UserIDs)
	}
	if rows[0].Count != 1 {
		t.Errorf("Count = %d; want 1", rows[0].Count)
	}

	// And the removal is idempotent the same way.
	newHandler(false).OnReactionRemoved("CG1", "8.001", "U9", "tada")
	newHandler(true).OnReactionRemoved("CG1", "8.001", "U9", "tada")
	rows, err = db.GetReactions("8.001", "CG1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("reactions after double removal = %v; want none", rows)
	}
}

// TestOnReactionRemoved_TruncatedUserListStillDecrements pins the
// truncated-list case: Slack shortens UserIDs on busy reactions, so an
// unlisted user removing theirs must still decrement the count off a
// grid; on a grid the conservative skip stands (a sibling's duplicate
// removal cannot be told from a truncation).
func TestOnReactionRemoved_TruncatedUserListStillDecrements(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertChannel(cache.Channel{ID: "CG2", WorkspaceID: "T1", Name: "cg2", Type: "channel"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertReaction("8.003", "CG2", "tada", []string{"U9"}, 5); err != nil {
		t.Fatal(err)
	}

	// gridSiblings returning nil is the production shape off a grid:
	// main.go wires the closure onto every handler.
	offGrid := &rtmEventHandler{
		program:      sendFunc(func(tea.Msg) {}),
		db:           db,
		workspaceID:  "T1",
		isActive:     func() bool { return false },
		gridSiblings: func() []string { return nil },
	}
	// U7 is not in the truncated list, but Count 5 > len 1: decrement.
	offGrid.OnReactionRemoved("CG2", "8.003", "U7", "tada")
	rows, err := db.GetReactions("8.003", "CG2")
	if err != nil || len(rows) != 1 {
		t.Fatalf("GetReactions = %v, %v; want one row", rows, err)
	}
	if rows[0].Count != 4 {
		t.Errorf("Count = %d; want 4 (truncated list, unlisted remover)", rows[0].Count)
	}

	// On a grid the same removal skips: the double-remove guard wins.
	onGrid := &rtmEventHandler{
		program:      sendFunc(func(tea.Msg) {}),
		db:           db,
		workspaceID:  "T1",
		isActive:     func() bool { return false },
		gridSiblings: func() []string { return []string{"T1", "T2"} },
	}
	onGrid.OnReactionRemoved("CG2", "8.003", "U7", "tada")
	rows, err = db.GetReactions("8.003", "CG2")
	if err != nil || len(rows) != 1 {
		t.Fatalf("GetReactions = %v, %v; want one row", rows, err)
	}
	if rows[0].Count != 4 {
		t.Errorf("Count = %d; want 4 unchanged on a grid (conservative skip)", rows[0].Count)
	}
}

// TestGridDroppedMessage_StillDiscoversOnSibling is the end-to-end pin
// the reviewer asked for: two sessions of one grid, the active one's
// copy of a message on a brand-new group DM arrives first, and the
// sibling whose copy the gate dropped still ends up with the
// conversation in its workspace list - via the discovery hook the gate
// runs for dropped message frames.
func TestGridDroppedMessage_StillDiscoversOnSibling(t *testing.T) {
	db := newTestDB(t)

	activeCtx := &WorkspaceContext{UserNames: newUserNameStore(nil), UserNamesByHandle: map[string]string{}}
	active := &rtmEventHandler{
		program:      sendFunc(func(tea.Msg) {}),
		db:           db,
		wsCtx:        activeCtx,
		workspaceID:  "T1",
		isActive:     func() bool { return true },
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
		resolveConversation: func(ctx context.Context, id string) (*slack.Channel, error) {
			return &slack.Channel{GroupConversation: slack.GroupConversation{
				Conversation: slack.Conversation{ID: id, IsMpIM: true},
				Name:         "mpdm-test",
			}}, nil
		},
	}
	siblingCtx := &WorkspaceContext{UserNames: newUserNameStore(nil), UserNamesByHandle: map[string]string{}}
	sibling := &rtmEventHandler{
		program:      sendFunc(func(tea.Msg) {}),
		db:           db,
		wsCtx:        siblingCtx,
		workspaceID:  "T2",
		isActive:     func() bool { return false },
		channelNames: map[string]string{},
		channelTypes: map[string]string{},
		resolveConversation: func(ctx context.Context, id string) (*slack.Channel, error) {
			return &slack.Channel{GroupConversation: slack.GroupConversation{
				Conversation: slack.Conversation{ID: id, IsMpIM: true},
				Name:         "mpdm-test",
			}}, nil
		},
	}

	// The delivery the gate performs: the active session's copy wins
	// and processes the message; the sibling's copy is dropped and the
	// gate runs only the discovery hook on it.
	active.OnMessage("G9", "U2", "1.001", "hi", "", "", false, false, nil, slack.Blocks{}, nil, "", "")
	sibling.discoverAndPublish("G9")

	if len(activeCtx.Channels) != 1 || activeCtx.Channels[0].ID != "G9" {
		t.Fatalf("active session Channels = %+v; want the group DM G9", activeCtx.Channels)
	}
	if len(siblingCtx.Channels) != 1 || siblingCtx.Channels[0].ID != "G9" {
		t.Fatalf("sibling session Channels = %+v; want the group DM G9 discovered despite the dropped frame", siblingCtx.Channels)
	}
	if _, known := sibling.channelTypes["G9"]; !known {
		t.Error("sibling session must know the channel type after discovery")
	}
}
