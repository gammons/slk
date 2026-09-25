package peerstatus

import (
	"testing"
	"time"

	"github.com/gammons/slk/internal/emoji"
)

var testNow = time.Unix(1700000000, 0)

func TestGlyph_ResolvesShortcodesAndFallsBack(t *testing.T) {
	calendar := emoji.CodeMap()[":calendar:"]
	thumbs := emoji.CodeMap()[":+1:"]
	if calendar == "" || thumbs == "" {
		t.Fatal("test fixtures missing from the emoji code map")
	}
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"standard shortcode", Status{Emoji: ":calendar:"}, calendar},
		{"skin tone is dropped", Status{Emoji: ":+1::skin-tone-3:"}, thumbs},
		{"custom workspace emoji", Status{Emoji: ":company-logo-not-unicode:"}, FallbackGlyph},
		{"text without emoji", Status{Text: "Heads down"}, FallbackGlyph},
		{"nothing set", Status{}, ""},
		{"expired", Status{Emoji: ":calendar:", Expires: testNow}, ""},
		{"expires later", Status{Emoji: ":calendar:", Expires: testNow.Add(time.Minute)}, calendar},
	}
	for _, c := range cases {
		if got := c.st.Glyph(testNow); got != c.want {
			t.Errorf("%s: Glyph = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestInDND(t *testing.T) {
	if (Status{DND: true}).InDND(testNow) != true {
		t.Error("DND with unknown end must count as in DND")
	}
	if (Status{DND: true, DNDEnd: testNow}).InDND(testNow) {
		t.Error("DND whose end is now must not count as in DND")
	}
	if (Status{DNDEnd: testNow.Add(time.Hour)}).InDND(testNow) {
		t.Error("an end time without DND on must not count as in DND")
	}
}

func TestSummary(t *testing.T) {
	end := testNow.Add(time.Hour)
	st := Status{Emoji: ":calendar:", Text: "In a meeting", DND: true, DNDEnd: end}
	want := emoji.CodeMap()[":calendar:"] + " In a meeting · " + DNDGlyph + " Do not disturb until " + end.Local().Format("15:04")
	if got := st.Summary(testNow, "15:04"); got != want {
		t.Errorf("Summary = %q; want %q", got, want)
	}
	if got := (Status{}).Summary(testNow, "15:04"); got != "" {
		t.Errorf("empty Summary = %q; want empty", got)
	}
	if got := (Status{DND: true}).Summary(testNow, "15:04"); got != DNDGlyph+" Do not disturb" {
		t.Errorf("DND without end Summary = %q", got)
	}
}

func TestExpiredAndClear(t *testing.T) {
	st := Status{
		Emoji: ":calendar:", Text: "In a meeting", Expires: testNow.Add(-time.Second),
		DND: true, DNDEnd: testNow.Add(time.Hour),
	}
	if !st.Expired(testNow) {
		t.Fatal("a passed status expiry must report Expired")
	}
	cleared := st.Clear(testNow)
	if cleared.Emoji != "" || cleared.Text != "" || !cleared.Expires.IsZero() {
		t.Errorf("Clear kept the expired status: %+v", cleared)
	}
	if !cleared.DND || !cleared.DNDEnd.Equal(st.DNDEnd) {
		t.Errorf("Clear dropped DND that has not ended: %+v", cleared)
	}
	if cleared.Expired(testNow) {
		t.Error("a cleared status must not still report Expired")
	}
	if (Status{Emoji: ":calendar:"}).Expired(testNow) {
		t.Error("a status that never expires must not report Expired")
	}
}

func TestWithDNDOffClearsEnd(t *testing.T) {
	st := Status{}.WithDND(true, testNow).WithDND(false, testNow)
	if st.DND || !st.DNDEnd.IsZero() {
		t.Errorf("WithDND(false) = %+v; want DND off with no end", st)
	}
}
