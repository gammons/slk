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

// clipboardWriter creates a Bubble Tea command that writes text through the
// terminal. Production uses tea.SetClipboard, which emits OSC 52 without
// invoking the native clipboard library or requiring CGO.
type clipboardWriter func(text string) tea.Cmd

// defaultClipboardWriter is overridable per-App for tests.
var defaultClipboardWriter clipboardWriter = tea.SetClipboard
