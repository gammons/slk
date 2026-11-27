package thread

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// A thread with no replies still draws the parent; dragging over it
// must show the selection while the drag is held.
func TestNoRepliesViewShowsSelection(t *testing.T) {
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })

	m := New()
	parent := messages.MessageItem{
		TS: "100.0", UserName: "alice", Text: "hello world from parent",
		Timestamp: "9:00 AM",
	}
	m.SetThread(parent, nil, "C1", parent.TS)
	_ = m.View(40, 80)

	m.BeginSelectionAt(m.chromeHeight, 2)
	m.ExtendSelectionAt(m.chromeHeight, 10)
	if !m.HasSelection() {
		t.Fatal("selection did not start on the parent")
	}

	r, g, b, _ := styles.SelectionBackground.RGBA()
	sgr := fmt.Sprintf("48;2;%d;%d;%d", uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if !strings.Contains(m.View(40, 80), sgr) {
		t.Fatalf("view has no selection background %s", sgr)
	}
}
