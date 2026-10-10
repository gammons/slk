package messages

import "github.com/gammons/slk/internal/ui/styles"

// EphemeralLabel is the marker row drawn above the author line of a
// message only the current user can see. Shared by the messages and
// thread panes so the two cannot drift.
func EphemeralLabel() string {
	return styles.Timestamp.Render("Only visible to you")
}
