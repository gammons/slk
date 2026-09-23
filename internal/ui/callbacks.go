// internal/ui/callbacks.go
//
// Function types the App hands to its own sub-models. Collaborators
// outside the TUI are services in internal/core.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

// TypingSendFunc is called to broadcast a typing indicator.
type TypingSendFunc func(channelID string)

// clipboardWriter creates a Bubble Tea command that copies text. The default
// emits OSC 52; cmd/slk can supply a host-specific writer without UI-side I/O.
type clipboardWriter func(text string) tea.Cmd

// defaultClipboardWriter is overridable per-App via SetClipboardWriter.
var defaultClipboardWriter clipboardWriter = tea.SetClipboard
