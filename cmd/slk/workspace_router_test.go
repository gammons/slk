package main

import (
	"fmt"
	"sync"
	"testing"
)

// TestWorkspaceRouter_ConcurrentAddAndByID is a race-detector test. It
// reproduces the pattern boot produces -- connect goroutines calling
// Add while UI-goroutine callbacks call ByID and All -- and fails under
// -race if the router's map is ever touched unguarded. In production
// the unguarded version is a runtime fatal ("concurrent map read and
// map write"), not merely a report.
func TestWorkspaceRouter_ConcurrentAddAndByID(t *testing.T) {
	r := newWorkspaceRouter()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("T%d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.Add(&WorkspaceContext{TeamID: id})
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.ByID(id)
				_ = r.All()
			}
		}()
	}
	wg.Wait()

	if got := len(r.All()); got != 8 {
		t.Fatalf("All() = %d workspaces, want 8", got)
	}
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("T%d", i)
		if w := r.ByID(id); w == nil || w.TeamID != id {
			t.Errorf("ByID(%s) = %+v", id, w)
		}
	}
}
