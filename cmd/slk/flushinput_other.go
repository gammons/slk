//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd && !windows

package main

// flushInput is a no-op where no input flush is wired.
func flushInput(int) error { return nil }

// inputPending reports nothing pending where it cannot be queried.
func inputPending(int) (int, error) { return 0, nil }
