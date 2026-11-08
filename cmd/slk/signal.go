package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalContext returns the context a one-shot command (slk export,
// the scriptable CLI) runs under: Ctrl-C or SIGTERM cancels it, and
// the history walks, reply fetches and rate-limit waits all watch it.
// Once it fires, default signal handling is restored, so a second
// Ctrl-C still kills the process during any step that does not watch
// the context. The caller defers stop.
func signalContext() (ctx context.Context, stop context.CancelFunc) {
	ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	return ctx, stop
}
