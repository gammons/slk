package thread

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// The parent, drawn outside WrapRows, gets the same mention highlight as
// a reply when it mentions the user and is not selected.
func TestThreadParentMentionHighlight(t *testing.T) {
	sgr := func(kind int, c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("%d;2;%d;%d;%d", kind, r>>8, g>>8, b>>8)
	}
	for _, c := range []struct {
		text string
		want bool
	}{
		{"<@UME> can you check?", true},
		{"no mention here", false},
	} {
		m := p3Thread(t, 0)
		m.SetCurrentUser("UME")
		parent := messages.MessageItem{TS: "1700000000.000000", UserID: "UOTHER", UserName: "pat", Text: c.text, Timestamp: "9:00 AM"}
		replies := []messages.MessageItem{
			{TS: "1700000001.000000", UserID: "UME", UserName: "me", Text: "on it", Timestamp: "9:01 AM"},
		}
		m.SetThread(parent, replies, "C1", parent.TS)
		parentRows := strings.SplitN(m.View(40, 80), "on it", 2)[0]
		bar := sgr(38, styles.MentionHighlightBar())
		tint := sgr(48, styles.MentionHighlightBg())
		if got := strings.Contains(parentRows, bar) && strings.Contains(parentRows, tint); got != c.want {
			t.Errorf("%q: parent highlighted = %v, want %v", c.text, got, c.want)
		}
	}
}
