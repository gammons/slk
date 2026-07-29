package slackfmt

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStripMarkup_ResolvesAndStrips(t *testing.T) {
	names := map[string]string{"U1": "alice"}
	got := StripMarkup("<@U1> hi <#C1|general> *bold* <!here> <https://x.io|link>", names)
	for _, want := range []string{"@alice", "#general", "bold", "@here", "link"} {
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in %q", want, got)
		}
	}
	if strings.ContainsAny(got, "*`~") {
		t.Fatalf("emphasis punctuation should be stripped: %q", got)
	}
}

// StripMarkup applies NO length cap (the card/notification clip
// themselves) and must never slice a multibyte body mid-rune.
func TestStripMarkup_NoTruncationAndUTF8Safe(t *testing.T) {
	long := strings.Repeat("あ", 500) // 1500 bytes, 500 runes
	got := StripMarkup(long, nil)
	if got != long {
		t.Fatalf("StripMarkup must not truncate; len(got)=%d want %d", len([]rune(got)), 500)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("StripMarkup produced invalid UTF-8")
	}
}
