// Package core is the boundary between slk's application code and its
// terminal UI. It declares the services the TUI calls (the ports) and the
// values they exchange. The TUI depends only on these; cmd/slk wires the
// implementations, which talk to Slack, SQLite and the OS.
//
// core declares interfaces and values only; it does no I/O itself.
package core

// Msg is what a service hands back for the TUI to dispatch: one of the
// TUI's message values, or nil when there is nothing to report. It is
// the same type as a Bubble Tea message's underlying interface, so the
// TUI can return it from a tea.Cmd unchanged.
type Msg = any

// Cmd is deferred work returned by services that must capture state at
// call time but do their I/O later. nil means nothing to run.
type Cmd = func() Msg
