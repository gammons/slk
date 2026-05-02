package mrkdwn

import "testing"

func TestTokenize_NoTokens(t *testing.T) {
	got, table := tokenize("hello world")
	if got != "hello world" {
		t.Errorf("text changed: %q", got)
	}
	if len(table) != 0 {
		t.Errorf("expected empty table, got %d entries", len(table))
	}
}

func TestTokenize_UserMention(t *testing.T) {
	got, table := tokenize("hi <@U12345>!")
	want := "hi \uE000" + "0" + "\uE001!"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if len(table) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(table))
	}
	if table[0].kind != tokUser || table[0].id != "U12345" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_ChannelMentionWithName(t *testing.T) {
	got, table := tokenize("see <#C123|general>")
	want := "see \uE000" + "0" + "\uE001"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if table[0].kind != tokChannel || table[0].id != "C123" || table[0].label != "general" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_ChannelMentionBare(t *testing.T) {
	got, table := tokenize("<#C9>")
	want := "\uE000" + "0" + "\uE001"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if table[0].kind != tokChannel || table[0].id != "C9" || table[0].label != "" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_BroadcastHere(t *testing.T) {
	_, table := tokenize("<!here> deploy")
	if table[0].kind != tokBroadcast || table[0].id != "here" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_BroadcastSubteam(t *testing.T) {
	_, table := tokenize("<!subteam^S01|@team>")
	if table[0].kind != tokBroadcast || table[0].id != "subteam^S01" || table[0].label != "@team" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_LinkLabeled(t *testing.T) {
	_, table := tokenize("<https://slack.com|Slack>")
	if table[0].kind != tokLink || table[0].id != "https://slack.com" || table[0].label != "Slack" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_LinkBare(t *testing.T) {
	_, table := tokenize("<https://slack.com>")
	if table[0].kind != tokLink || table[0].id != "https://slack.com" || table[0].label != "" {
		t.Errorf("table[0] = %+v", table[0])
	}
}

func TestTokenize_Multiple(t *testing.T) {
	got, table := tokenize("hi <@U1> and <@U2>")
	want := "hi \uE000" + "0" + "\uE001 and \uE000" + "1" + "\uE001"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if len(table) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(table))
	}
}

func TestSentinelRoundTrip(t *testing.T) {
	in := "**bold** <@U1> and `code` and <https://x.com|x>"
	tokenized, table := tokenize(in)
	got := detokenizeText(tokenized, table)
	if got != in {
		t.Errorf("round trip changed text:\n  in:  %q\n  out: %q", in, got)
	}
}

func TestParseSentinel(t *testing.T) {
	// Layout: "hello " (6 bytes) + \uE000 (3) + "7" (1) + \uE001 (3) + " world" (6).
	// Sentinel begins at byte 6 and ends at byte 13.
	s := "hello \uE0007\uE001 world"
	idx, end, ok := parseSentinel(s, 6)
	if !ok {
		t.Fatal("expected to parse sentinel at byte 6")
	}
	if idx != 7 {
		t.Errorf("idx = %d, want 7", idx)
	}
	if end != 13 {
		t.Errorf("end = %d, want 13", end)
	}
}

func TestParseSentinel_NotASentinel(t *testing.T) {
	_, _, ok := parseSentinel("hello", 0)
	if ok {
		t.Fatal("expected ok=false for plain text")
	}
}
