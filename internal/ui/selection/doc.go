// Package selection provides anchor / range types for representing a
// user-driven text selection inside the messages and thread panes.
//
// The package is pure data: it has no knowledge of lipgloss, bubbletea,
// message structs, or rendered caches. UI code resolves Anchor.MessageID
// to absolute cache coordinates at render time. This keeps the selection
// stable across cache rebuilds (new messages, width changes, theme
// switches).
package selection
