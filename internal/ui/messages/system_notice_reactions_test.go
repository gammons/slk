package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
)

// A reaction on a system notice (someone reacts to "joined the channel")
// renders as a pill below the notice and is clickable like any other.
func TestNoticeRendersReactionPill(t *testing.T) {
	msg := centreNotice()
	msg.Reactions = []ReactionItem{{Emoji: "wave", Count: 3}}
	rc := &RenderContext{UserNames: map[string]string{"U2": "alice"}}
	res := RenderMessage(msg, 60, rc, RenderOptions{})

	lines := strings.Split(ansi.Strip(res.Content), "\n")
	if len(lines) < 2 {
		t.Fatalf("no reaction row under the notice: %q", res.Content)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "3") {
		t.Fatalf("reaction pill (with its count) missing from last row %q", last)
	}
	var hit *entrylist.Hit
	for i := range res.Hits {
		if res.Hits[i].Kind == entrylist.HitReaction {
			hit = &res.Hits[i]
		}
	}
	if hit == nil {
		t.Fatalf("no HitReaction recorded: %+v", res.Hits)
	}
	if hit.Key != "wave" || hit.RowStart != len(lines)-1 || hit.ColStart < 1 {
		t.Errorf("hit = %+v, want key wave on row %d", *hit, len(lines)-1)
	}
}
