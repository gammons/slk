package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func newApp_WithOpenConvCapture(t *testing.T) (*App, *capturedOpenConv) {
	t.Helper()
	cap := &capturedOpenConv{}
	// withSize(0, 0) preserves NewApp's unsized state: the original
	// builder set no dimensions.
	app := newTestApp(t,
		withSize(0, 0),
		withChannelService(ChannelServiceFuncs{
			OpenConversation: func(userIDs []string, requestID uint64) tea.Cmd {
				cap.calls = append(cap.calls, openConvCall{UserIDs: userIDs, RequestID: requestID})
				return nil // tests synthesize the result message directly
			},
		}),
	)
	app.currentUserID = "USELF"
	app.SetUserNames(map[string]string{"USELF": "Me", "U1": "Alice", "U2": "Bob"})
	return app, cap
}

type openConvCall struct {
	UserIDs   []string
	RequestID uint64
}

type capturedOpenConv struct {
	calls []openConvCall
}

func TestReducer_EnterNewMessageMsg_OpensPickerAndEntersMode(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)

	_, _ = app.Update(EnterNewMessageMsg{})

	if app.mode != ModeNewMessage {
		t.Errorf("expected ModeNewMessage, got %v", app.mode)
	}
	if !app.newMessagePicker.IsVisible() {
		t.Error("expected picker visible")
	}
}

func TestReducer_NewMessageOpenedMsg_SwitchesChannelAndEntersInsertMode(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 7 // simulate an in-flight submit

	_, cmd := app.Update(NewMessageOpenedMsg{
		ChannelID:   "D123",
		AlreadyOpen: true,
		UserIDs:     []string{"U1"},
		RequestID:   7,
	})

	if app.mode != ModeInsert {
		t.Errorf("expected ModeInsert after opened msg, got %v", app.mode)
	}
	if app.newMessagePicker.IsVisible() {
		t.Error("expected picker closed")
	}
	// The reducer should emit a ChannelSelectedMsg (batched with
	// a compose-focus cmd).
	if cmd == nil {
		t.Fatal("expected a tea.Cmd that fires ChannelSelectedMsg")
	}
	sel, ok := findChannelSelected(cmd())
	if !ok {
		t.Fatalf("expected ChannelSelectedMsg in batch, got %T", cmd())
	}
	if sel.ID != "D123" {
		t.Errorf("expected ChannelID=D123, got %s", sel.ID)
	}
}

// findChannelSelected walks a tea.Cmd result (which may be a
// tea.BatchMsg) and returns the first ChannelSelectedMsg it finds.
func findChannelSelected(msg tea.Msg) (ChannelSelectedMsg, bool) {
	if sel, ok := msg.(ChannelSelectedMsg); ok {
		return sel, true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			if sel, ok := findChannelSelected(c()); ok {
				return sel, true
			}
		}
	}
	return ChannelSelectedMsg{}, false
}

func TestReducer_NewMessageOpenedMsg_DroppedWhenCancelled(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 7
	app.newMessageCancelled = true
	priorMode := app.mode

	_, cmd := app.Update(NewMessageOpenedMsg{
		ChannelID: "D123",
		RequestID: 7,
	})

	if app.mode != priorMode {
		t.Errorf("mode should not change for cancelled result, got %v", app.mode)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd for cancelled result, got %T", cmd())
	}
}

func TestReducer_NewMessageOpenedMsg_DroppedWhenRequestIDMismatches(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 9 // current in-flight is 9
	priorMode := app.mode

	// Late arrival for an older request 7.
	_, cmd := app.Update(NewMessageOpenedMsg{
		ChannelID: "D123",
		RequestID: 7,
	})

	if app.mode != priorMode {
		t.Errorf("mode should not change for stale RequestID, got %v", app.mode)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd for stale result")
	}
}

func TestReducer_NewMessageOpenedMsg_AlreadyOpenSkipsCacheInsert(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 1
	priorCount := len(app.sidebar.AllItems())

	_, _ = app.Update(NewMessageOpenedMsg{
		ChannelID:   "D456",
		AlreadyOpen: true,
		UserIDs:     []string{"U1"},
		RequestID:   1,
	})

	if got := len(app.sidebar.AllItems()); got != priorCount {
		t.Errorf("expected no sidebar mutation for AlreadyOpen=true, count went from %d to %d", priorCount, got)
	}
}

func TestReducer_NewMessageOpenedMsg_NewChannelInsertedAsDM(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 1

	_, _ = app.Update(NewMessageOpenedMsg{
		ChannelID:   "D789",
		AlreadyOpen: false,
		UserIDs:     []string{"U1"},
		RequestID:   1,
	})

	found := false
	for _, ch := range app.sidebar.AllItems() {
		if ch.ID == "D789" {
			found = true
			if ch.Type != "dm" {
				t.Errorf("expected Type=dm, got %s", ch.Type)
			}
			if ch.DMUserID != "U1" {
				t.Errorf("expected DMUserID=U1, got %s", ch.DMUserID)
			}
		}
	}
	if !found {
		t.Error("expected D789 inserted into sidebar")
	}
}

func TestReducer_NewMessageOpenedMsg_NewChannelInsertedAsGroupDM(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 1

	_, _ = app.Update(NewMessageOpenedMsg{
		ChannelID:   "G999",
		AlreadyOpen: false,
		UserIDs:     []string{"U1", "U2"},
		RequestID:   1,
	})

	found := false
	for _, ch := range app.sidebar.AllItems() {
		if ch.ID == "G999" {
			found = true
			if ch.Type != "group_dm" {
				t.Errorf("expected Type=group_dm, got %s", ch.Type)
			}
		}
	}
	if !found {
		t.Error("expected G999 inserted into sidebar")
	}
}

func TestReducer_NewMessageFailedMsg_KeepsModalOpenAndEmitsToast(t *testing.T) {
	app, _ := newApp_WithOpenConvCapture(t)
	_, _ = app.Update(EnterNewMessageMsg{})
	app.newMessageInFlightID = 7

	_, cmd := app.Update(NewMessageFailedMsg{
		RequestID: 7,
		Err:       errors.New("rate_limited"),
	})

	if app.mode != ModeNewMessage {
		t.Errorf("expected to stay in ModeNewMessage on failure, got %v", app.mode)
	}
	if !app.newMessagePicker.IsVisible() {
		t.Error("expected picker still visible on failure")
	}
	if cmd == nil {
		t.Fatal("expected a tea.Cmd emitting ToastMsg")
	}
	msg := cmd()
	toast, ok := msg.(ToastMsg)
	if !ok {
		t.Fatalf("expected ToastMsg, got %T", msg)
	}
	if toast.Text == "" {
		t.Error("expected non-empty toast text")
	}
}

func TestEndToEnd_CtrlN_SelectUser_Enter_OpensDM(t *testing.T) {
	app, cap := newApp_WithOpenConvCapture(t)

	// 1. User presses Ctrl+N.
	_, _ = app.Update(EnterNewMessageMsg{})

	if app.mode != ModeNewMessage {
		t.Fatalf("step 1: expected ModeNewMessage, got %v", app.mode)
	}

	// 2. User types "ali" to filter down to Alice.
	app.newMessagePicker.HandleKey("a")
	app.newMessagePicker.HandleKey("l")
	app.newMessagePicker.HandleKey("i")

	// 3. User presses Enter on the highlighted (Alice) row.
	result := app.newMessagePicker.HandleKey("enter")
	if result == nil {
		t.Fatal("step 3: expected non-nil Result from Enter")
	}
	if len(result.UserIDs) != 1 || result.UserIDs[0] != "U1" {
		t.Errorf("step 3: expected [U1], got %v", result.UserIDs)
	}

	// 4. The mode handler would call OpenConversation here. Simulate.
	app.newMessageInFlightID++
	reqID := app.newMessageInFlightID
	_ = app.channels.OpenConversation(result.UserIDs, reqID)

	if len(cap.calls) != 1 {
		t.Fatalf("step 4: expected exactly 1 OpenConversation call, got %d", len(cap.calls))
	}
	if cap.calls[0].UserIDs[0] != "U1" {
		t.Errorf("step 4: expected userID U1, got %s", cap.calls[0].UserIDs[0])
	}

	// 5. Slack returns success; reducer transitions to ModeInsert.
	_, cmd := app.Update(NewMessageOpenedMsg{
		ChannelID:   "D123",
		AlreadyOpen: true,
		UserIDs:     []string{"U1"},
		RequestID:   reqID,
	})

	if app.mode != ModeInsert {
		t.Errorf("step 5: expected ModeInsert, got %v", app.mode)
	}
	if app.newMessagePicker.IsVisible() {
		t.Error("step 5: expected picker hidden")
	}

	// 6. The cmd should emit ChannelSelectedMsg{ID: D123, Type: dm}
	// (batched with a compose-focus cmd).
	sel, ok := findChannelSelected(cmd())
	if !ok {
		t.Fatalf("step 6: expected ChannelSelectedMsg in batch, got %T", cmd())
	}
	if sel.ID != "D123" || sel.Type != "dm" {
		t.Errorf("step 6: want {D123, dm}, got {%s, %s}", sel.ID, sel.Type)
	}
}
