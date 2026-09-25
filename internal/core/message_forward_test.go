package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ids"
)

func TestMessageServiceForward(t *testing.T) {
	for _, wantErr := range []error{nil, errors.New("forward failed")} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		called := false
		wantResult := ForwardResult{}
		if wantErr == nil {
			wantResult = ForwardResult{TS: "124.000000", Text: "https://example.slack.com/archives/Csource/p123456"}
		}
		var forward MessageForwardFunc = func(gotCtx context.Context, teamID string, source ids.ChannelID, ts ids.MessageTS, destination ids.ChannelID) (ForwardResult, error) {
			called = true
			if gotCtx != ctx || teamID != "T1" || source != "Csource" || ts != "123.456" || destination != "Cdestination" {
				t.Errorf("unexpected forwarding arguments: %v, %q, %q, %q, %q", gotCtx, teamID, source, ts, destination)
			}
			return wantResult, wantErr
		}
		svc := NewMessageService(MessageServiceFuncs{Forward: forward})
		result, err := svc.Forward(ctx, "T1", "Csource", "123.456", "Cdestination")
		if result != wantResult {
			t.Errorf("Forward result = %+v, want %+v", result, wantResult)
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("Forward error = %v, want %v", err, wantErr)
		}
		if !called {
			t.Error("Forward did not call the closure")
		}
	}
}

func TestMessageServiceForwardUnsupported(t *testing.T) {
	svc := NewMessageService(MessageServiceFuncs{})
	result, err := svc.Forward(context.Background(), "T1", "Csource", "123.456", "Cdestination")
	if result != (ForwardResult{}) {
		t.Errorf("unsupported Forward result = %+v, want zero value", result)
	}
	if err == nil || !strings.Contains(err.Error(), "forward") || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("Forward error = %v, want descriptive unsupported error", err)
	}
}
