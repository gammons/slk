package sidebar

import (
	"fmt"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
)

func TestCursorIdentityAcrossStalenessChanges(t *testing.T) {
	now := time.Unix(2000000000, 0)
	for _, setter := range []string{"threshold", "active ID", "clock"} {
		for _, selected := range []string{"D1", "D2"} {
			t.Run(setter+"/"+selected, func(t *testing.T) {
				m := New([]ChannelItem{
					{ID: "D1", Name: "old", Type: "dm"},
					{ID: "D2", Name: "new", Type: "dm"},
					{ID: "D3", Name: "newer", Type: "dm"},
				})
				m.SetReadStateReader(func() map[string]core.ReadState {
					return map[string]core.ReadState{
						"D1": {LastReadTS: fmt.Sprintf("%d.000000", now.Add(-48*time.Hour).Unix())},
						"D2": {LastReadTS: fmt.Sprintf("%d.000000", now.Unix())},
						"D3": {LastReadTS: fmt.Sprintf("%d.000000", now.Unix())},
					}
				})
				m.SetNowFunc(func() time.Time { return now })
				switch setter {
				case "active ID":
					m.SetActiveChannelID("D1")
					m.SetStaleThreshold(24 * time.Hour)
				case "clock":
					m.SetNowFunc(func() time.Time { return now.Add(-72 * time.Hour) })
					m.SetStaleThreshold(24 * time.Hour)
				}
				m.SelectByID(selected)
				switch setter {
				case "threshold":
					m.SetStaleThreshold(24 * time.Hour)
				case "active ID":
					m.SetActiveChannelID("D3")
				case "clock":
					m.SetNowFunc(func() time.Time { return now })
				}
				if selected == "D1" {
					if !m.IsThreadsSelected() {
						t.Fatalf("removed target selected %s instead of Threads", m.SelectedID())
					}
				} else if m.SelectedID() != selected {
					t.Fatalf("surviving target selected %s, want %s", m.SelectedID(), selected)
				}
			})
		}
	}
}
