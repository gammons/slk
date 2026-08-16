package main

import (
	"sync"
	"testing"

	"github.com/slack-go/slack"
)

// messageAuthor runs on bubbletea command goroutines (the three fetch
// paths) and on the WebSocket goroutine (rtmEventHandler.OnMessage).
// It used to write resolved names back into the shared userNames map,
// which races the UI goroutine's reads and can end the process with
// "fatal error: concurrent map writes".
//
// Run with -race to see the original failure; the plain run still
// catches a regression via the explicit map-mutation assertion below.
func TestMessageAuthor_DoesNotMutateSharedNameMap(t *testing.T) {
	userNames := map[string]string{"U1": "Anna"}
	msg := slack.Message{Msg: slack.Msg{User: "U1"}}

	id, name := messageAuthor(msg, userNames, nil, nil)
	if id != "U1" || name != "Anna" {
		t.Fatalf("messageAuthor = (%q, %q), want (U1, Anna)", id, name)
	}
	if len(userNames) != 1 {
		t.Errorf("shared map grew to %d entries; messageAuthor must not write to it", len(userNames))
	}

	// An unknown user must not be memoised either — that write is what
	// raced, and it happened on exactly this path.
	unknown := slack.Message{Msg: slack.Msg{User: "U-UNKNOWN"}}
	messageAuthor(unknown, userNames, nil, nil)
	if _, written := userNames["U-UNKNOWN"]; written {
		t.Error("messageAuthor wrote an unknown user into the shared map")
	}
	if len(userNames) != 1 {
		t.Errorf("shared map grew to %d entries", len(userNames))
	}
}

// Concurrent readers and callers must coexist. Under -race this fails
// on the pre-fix code and passes after.
func TestMessageAuthor_SafeUnderConcurrentReads(t *testing.T) {
	userNames := map[string]string{"U1": "Anna", "U2": "Marek"}

	// The reader stands in for the UI goroutine. It gets its own done
	// channel rather than joining the WaitGroup: putting it in the
	// group and closing stop after Wait deadlocks — Wait blocks on the
	// reader, the reader blocks on stop.
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				_ = userNames["U1"]
				_ = userNames["U2"]
			}
		}
	}()

	var workers sync.WaitGroup
	for i := 0; i < 50; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			messageAuthor(slack.Message{Msg: slack.Msg{User: "U1"}}, userNames, nil, nil)
			messageAuthor(slack.Message{Msg: slack.Msg{User: "U-NEW"}}, userNames, nil, nil)
		}()
	}
	workers.Wait()
	close(stop)
	<-readerDone

	if len(userNames) != 2 {
		t.Errorf("shared map mutated concurrently: %d entries, want 2", len(userNames))
	}
}

// A message with neither a user nor a bot id still has to yield
// something renderable rather than an empty author.
func TestMessageAuthor_FallsBackToTheID(t *testing.T) {
	id, name := messageAuthor(slack.Message{Msg: slack.Msg{User: "U-UNRESOLVED"}}, map[string]string{}, nil, nil)
	if id != "U-UNRESOLVED" || name != "U-UNRESOLVED" {
		t.Errorf("messageAuthor = (%q, %q), want the id in both", id, name)
	}
}
