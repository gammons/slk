package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// deliveredMsgs runs each command the way the Bubble Tea runtime does
// (the members of a batch concurrently, ticks on their real timers) and
// returns, for each command in order, the first message of type T it
// delivers. All commands start together, so a test that schedules ticks
// and then acts can run them afterwards and still see each tick fire
// after the actions, as it would in the app. Members that deliver
// other types, such as a toast's clear tick, keep running in the
// background and are dropped.
//
// It opens tea.Batch but not tea.Sequence: a sequence arrives as
// Bubble Tea's unexported sequenceMsg, so a T inside one never comes
// and the test fails after 10s with "delivered no T". No production
// code uses tea.Sequence today.
//
// Use it instead of building a tick's message by hand: a hand-built
// message cannot show what the real tick carries.
func deliveredMsgs[T any](t *testing.T, cmds ...tea.Cmd) []T {
	t.Helper()
	chans := make([]chan T, len(cmds))
	for i, cmd := range cmds {
		// A nil command delivers nothing; fail now, not after the timeout.
		if cmd == nil {
			t.Fatalf("command %d is nil, want one that delivers %T", i, *new(T))
		}
		chans[i] = make(chan T, 1)
		go deliverFirst(cmd, chans[i])
	}
	out := make([]T, len(cmds))
	timeout := time.After(10 * time.Second)
	for i, ch := range chans {
		select {
		case out[i] = <-ch:
		case <-timeout:
			t.Fatalf("command %d delivered no %T within 10s", i, out[i])
		}
	}
	return out
}

// deliverFirst runs cmd, starts each member of a batch on its own
// goroutine, and sends the first message of type T on ch.
func deliverFirst[T any](cmd tea.Cmd, ch chan T) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			go deliverFirst(c, ch)
		}
		return
	}
	if m, ok := msg.(T); ok {
		select {
		case ch <- m:
		default:
		}
	}
}
