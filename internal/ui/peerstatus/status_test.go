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

func TestStatusGlyph(t *testing.T) {
	calendar := emoji.CodeMap()[":calendar:"]
	if calendar == "" {
		t.Fatal("test fixture missing from the emoji code map")
	}
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"known shortcode", Status{Emoji: ":calendar:"}, calendar},
		{"unknown shortcode falls back", Status{Emoji: ":company-logo-not-unicode:"}, FallbackGlyph},
		{"no status", Status{}, ""},
		{"expired status", Status{Emoji: ":calendar:", Expires: testNow}, ""},
		{
			"huddle active does not change it",
			Status{Emoji: ":calendar:", Huddle: HuddleActive},
			calendar,
		},
	}
	for _, c := range cases {
		if got := c.st.StatusGlyph(testNow); got != c.want {
			t.Errorf("%s: StatusGlyph = %q; want %q", c.name, got, c.want)
		}
	}
}

func TestGlyphShortcode_NamesTheEmojiGlyphStandsFor(t *testing.T) {
	inHuddle := Status{Emoji: ":calendar:", Huddle: HuddleActive}
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"standard shortcode", Status{Emoji: ":calendar:"}, "calendar"},
		{"skin tone is kept for the image", Status{Emoji: ":+1::skin-tone-3:"}, "+1::skin-tone-3"},
		{"custom workspace emoji", Status{Emoji: ":company-logo:"}, "company-logo"},
		{"text without emoji", Status{Text: "Heads down"}, fallbackShortcode},
		{"huddle wins over status", inHuddle, huddleShortcode},
		{"nothing set", Status{}, ""},
		{"expired", Status{Emoji: ":calendar:", Expires: testNow}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.st.GlyphShortcode(testNow); got != c.want {
				t.Errorf("GlyphShortcode = %q, want %q", got, c.want)
			}
			// Whenever there is no glyph there is nothing to place.
			if (c.st.Glyph(testNow) == "") != (c.want == "") {
				t.Errorf("Glyph %q and GlyphShortcode %q disagree on whether anything shows",
					c.st.Glyph(testNow), c.want)
			}
		})
	}
}

// The image placed for a huddle or a text-only status must be the same
// emoji as the glyph shown when it can't be placed.
func TestGlyphShortcode_FixedNamesMatchTheirGlyphs(t *testing.T) {
	if got := emoji.CodeMap()[":"+huddleShortcode+":"]; got != HuddleGlyph {
		t.Errorf(":%s: is %q, want HuddleGlyph %q", huddleShortcode, got, HuddleGlyph)
	}
	if got := emoji.CodeMap()[":"+fallbackShortcode+":"]; got != FallbackGlyph {
		t.Errorf(":%s: is %q, want FallbackGlyph %q", fallbackShortcode, got, FallbackGlyph)
	}
}

func TestStatusShortcode_IgnoresHuddle(t *testing.T) {
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"standard shortcode", Status{Emoji: ":calendar:"}, "calendar"},
		{"custom workspace emoji", Status{Emoji: ":company-logo:"}, "company-logo"},
		{"text without emoji", Status{Text: "Heads down"}, fallbackShortcode},
		{"huddle is ignored", Status{Emoji: ":calendar:", Huddle: HuddleActive}, "calendar"},
		{"huddle alone", Status{Huddle: HuddleActive}, ""},
		{"nothing set", Status{}, ""},
		{"expired", Status{Emoji: ":calendar:", Expires: testNow}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.st.StatusShortcode(testNow); got != c.want {
				t.Errorf("StatusShortcode = %q, want %q", got, c.want)
			}
			if (c.st.StatusGlyph(testNow) == "") != (c.want == "") {
				t.Errorf("StatusGlyph %q and StatusShortcode %q disagree on whether anything shows",
					c.st.StatusGlyph(testNow), c.want)
			}
		})
	}
}

func TestSummaryWith(t *testing.T) {
	end := testNow.Add(time.Hour)
	all := Status{Emoji: ":company-logo:", Text: "Busy", Huddle: HuddleActive, DND: true, DNDEnd: end}
	mark := func(shortcode, glyph string) string { return "<" + shortcode + "|" + glyph + ">" }
	cases := []struct {
		name string
		st   Status
		want string
	}{
		{"every part", all, "<headphones|" + HuddleGlyph + "> In a huddle · <company-logo|" + FallbackGlyph + "> Busy · " +
			DNDGlyph + " Do not disturb until " + end.Local().Format("15:04")},
		{"emoji without text keeps no trailing space", Status{Emoji: ":calendar:"}, "<calendar|📆>"},
		{"text without emoji", Status{Text: "Lunch"}, "<speech_balloon|" + FallbackGlyph + "> Lunch"},
		{"nothing", Status{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.st.SummaryWith(testNow, "15:04", mark); got != c.want {
				t.Errorf("SummaryWith = %q, want %q", got, c.want)
			}
		})
	}

	// A blank reservation (an image still loading) is kept, not trimmed.
	blank := func(string, string) string { return "  " }
	if got := (Status{Emoji: ":calendar:"}).SummaryWith(testNow, "15:04", blank); got != "  " {
		t.Errorf("blank reservation = %q, want two spaces", got)
	}

	// A nil emoji is Summary.
	for _, st := range []Status{all, {Emoji: ":calendar:"}, {Text: "Lunch"}, {Emoji: ":calendar:", Text: "Out"}, {}} {
		if got, want := st.SummaryWith(testNow, "15:04", nil), st.Summary(testNow, "15:04"); got != want {
			t.Errorf("SummaryWith(nil) = %q, Summary = %q", got, want)
		}
	}
}
