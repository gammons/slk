package main

import (
	"fmt"
	"testing"

	"github.com/gammons/slk/internal/ui"
)

// TestNameHandoff_UIMapIsNotTheNotifyBaseline drives a real ui.App with
// the map cmd/slk hands it, while NotifyFrom reconciles off the Update
// goroutine, exactly as the AfterSwitch cmd does. The UI writes the map
// it was handed (PatchUserName on every UserResolvedMsg), so NotifyFrom
// must not read that map. A first version used it as NotifyFrom's
// baseline, and this was "fatal error: concurrent map read and map
// write" again. Run with -race.
func TestNameHandoff_UIMapIsNotTheNotifyBaseline(t *testing.T) {
	seed := map[string]string{}
	for i := 0; i < 5000; i++ {
		seed[fmt.Sprintf("U%05d", i)] = "name"
	}
	store := newUserNameStore(seed)
	names, since := store.SnapshotForUI()

	app := ui.NewApp()
	app.Update(ui.WorkspaceSwitchedMsg{TeamID: "T1", UserNames: names})

	done := make(chan struct{})
	go func() { // the AfterSwitch cmd goroutine
		store.NotifyFrom(since, func(string, string) {})
		close(done)
	}()
	for i := 0; i < 2000; i++ { // the Update goroutine
		app.Update(ui.UserResolvedMsg{TeamID: "T1", UserID: fmt.Sprintf("UNEW%04d", i), DisplayName: "n"})
	}
	<-done
}
