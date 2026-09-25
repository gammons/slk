package peerstatus

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/emoji"
)

func TestInHuddle(t *testing.T) {
	cases := []struct {
		name string
		st   Status
		want bool
	}{
		{"never set", Status{}, false},
		{"unset default", Status{Huddle: "default_unset"}, false},
		{"in a huddle", Status{Huddle: "in_a_huddle"}, true},
		// Only the captured in-huddle value counts.
		{"unrecognised state", Status{Huddle: "something_new"}, false},
		{"expiry passed", Status{Huddle: "in_a_huddle", HuddleExpires: testNow}, false},
		{"expiry ahead", Status{Huddle: "in_a_huddle", HuddleExpires: testNow.Add(time.Minute)}, true},
	}
	for _, c := range cases {
		if got := c.st.InHuddle(testNow); got != c.want {
			t.Errorf("%s: InHuddle = %v; want %v", c.name, got, c.want)
		}
	}
}

func TestHuddle_TakesTheGlyphSlotAndLeadsTheSummary(t *testing.T) {
	st := Status{Emoji: ":calendar:", Text: "In a meeting", Huddle: "in_a_huddle"}
	if got := st.Glyph(testNow); got != HuddleGlyph {
		t.Errorf("Glyph = %q; want the huddle glyph over the status emoji", got)
	}
	want := HuddleGlyph + " In a huddle · " + emoji.CodeMap()[":calendar:"] + " In a meeting"
	if got := st.Summary(testNow, "15:04"); got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}

	st = st.WithHuddle("default_unset", time.Time{})
	if got := st.Glyph(testNow); got != emoji.CodeMap()[":calendar:"] {
		t.Errorf("after the huddle ends Glyph = %q; want the status emoji back", got)
	}
}

func TestHuddle_ExpiryAndDeadline(t *testing.T) {
	st := Status{Huddle: "in_a_huddle", HuddleExpires: testNow.Add(-time.Second), Emoji: ":calendar:"}
	if !st.HasDeadline() || !st.Expired(testNow) {
		t.Fatal("an expired huddle must count as a deadline that has passed")
	}
	cleared := st.Clear(testNow)
	if cleared.Huddle != "" || !cleared.HuddleExpires.IsZero() || cleared.Emoji != ":calendar:" {
		t.Errorf("Clear = %+v; want the huddle gone and the status kept", cleared)
	}
	if (Status{Huddle: "in_a_huddle"}).HasDeadline() {
		t.Error("a huddle with no expiry must not count as a deadline")
	}
}
