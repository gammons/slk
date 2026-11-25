//go:build !windows

package main

import (
	"syscall"
	"testing"
	"time"
)

// A SIGUSR1 that arrives after the watcher is set up but before start runs
// (a theme file rewritten during startup) still triggers a reload.
func TestThemeReloadKeepsASignalSentBeforeStart(t *testing.T) {
	start := watchThemeReloadSignal()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	reloaded := make(chan struct{}, 1)
	start(func() {
		select {
		case reloaded <- struct{}{}:
		default:
		}
	})
	select {
	case <-reloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("no reload for a signal sent before start")
	}
}
