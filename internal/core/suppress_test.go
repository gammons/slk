package core

import (
	"slices"
	"testing"
)

func TestSuppressionHides(t *testing.T) {
	s := NewSuppression([]SuppressRule{
		{AuthorID: "B1"},                  // everywhere
		{AuthorID: "U2", ChannelID: "C1"}, // one channel
	})
	cases := []struct {
		channel, author string
		want            bool
	}{
		{"C1", "B1", true},
		{"C9", "B1", true},
		{"", "B1", true},
		{"C1", "U2", true},
		{"C2", "U2", false},
		{"", "U2", false},
		{"C1", "U3", false},
		{"C1", "", false},
	}
	for _, c := range cases {
		if got := s.Hides(c.channel, c.author); got != c.want {
			t.Errorf("Hides(%q, %q) = %v, want %v", c.channel, c.author, got, c.want)
		}
	}
}

func TestSuppressionHidesMessageByAuthor(t *testing.T) {
	s := NewSuppression([]SuppressRule{{AuthorID: "B1"}})
	if !s.HidesMessage("C1", MessageItem{UserID: "B1"}) {
		t.Error("a bot message attributed to a suppressed bot ID should be hidden")
	}
	// The author is the ID slk attributes the message to: a bot posting
	// as a user is that user, wherever the message came from.
	if s.HidesMessage("C1", MessageItem{UserID: "U9", BotID: "B1"}) {
		t.Error("only the author ID is matched")
	}
}

func TestNilSuppressionHidesNothing(t *testing.T) {
	var s *Suppression
	if s.Hides("C1", "U1") || s.Len() != 0 || s.Rules() != nil || s.Has(SuppressRule{AuthorID: "U1"}) {
		t.Error("nil set should be empty")
	}
	if got := s.With(SuppressRule{AuthorID: "U1"}); !got.Hides("C1", "U1") {
		t.Error("With on nil should build a set")
	}
}

func TestSuppressionWithWithoutAreCopies(t *testing.T) {
	a := NewSuppression([]SuppressRule{{AuthorID: "U1"}, {AuthorID: "U1"}, {AuthorID: ""}})
	if a.Len() != 1 {
		t.Fatalf("duplicates and author-less rules should be dropped, got %d", a.Len())
	}
	b := a.With(SuppressRule{AuthorID: "U2", ChannelID: "C1", Note: "x"})
	if a.Len() != 1 || b.Len() != 2 {
		t.Fatalf("With must not change the receiver: a=%d b=%d", a.Len(), b.Len())
	}
	if a.With(SuppressRule{AuthorID: "U1", Note: "other note"}) != a {
		t.Error("adding a rule already held should return the same set")
	}
	c := b.Without(SuppressRule{AuthorID: "U1"})
	if b.Len() != 2 || c.Len() != 1 || c.Hides("C5", "U1") {
		t.Fatalf("Without: b=%d c=%d", b.Len(), c.Len())
	}
	want := []SuppressRule{{AuthorID: "U2", ChannelID: "C1", Note: "x"}}
	if !slices.Equal(c.Rules(), want) {
		t.Errorf("Rules = %v, want %v", c.Rules(), want)
	}
}
