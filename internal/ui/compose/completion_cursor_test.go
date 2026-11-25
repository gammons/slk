package compose

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/bubbles/channelpicker"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/mentionpicker"
)

// tabComplete types draft, moves the cursor to at (a byte offset in that
// draft), types query, and accepts the highlighted completion with Tab.
func tabComplete(t *testing.T, m Model, draft string, at int, query string) Model {
	t.Helper()
	m.SetWidth(80)
	_ = m.Focus()
	m = typeText(m, draft)
	m = moveCursorTo(t, m, at)
	m = typeText(m, query)
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	return m
}

// moveCursorTo walks the cursor from the start of the draft to bytePos
// with the same right-arrow path a person uses.
func moveCursorTo(t *testing.T, m Model, bytePos int) Model {
	t.Helper()
	m.MoveCursorToStart()
	val := m.Value()
	if bytePos < 0 || bytePos > len(val) {
		t.Fatalf("cursor target %d out of range for %q", bytePos, val)
	}
	right := tea.KeyPressMsg{Code: tea.KeyRight}
	for range utf8.RuneCountInString(val[:bytePos]) {
		m, _ = m.Update(right)
	}
	if got := m.cursorPosition(); got != bytePos {
		t.Fatalf("setup cursor = %d, want %d in %q", got, bytePos, val)
	}
	return m
}

func assertCursorAt(t *testing.T, m Model, bytePos int) {
	t.Helper()
	val := m.Value()
	if bytePos < 0 || bytePos > len(val) {
		t.Fatalf("want cursor %d outside value %q", bytePos, val)
	}
	if got := m.cursorPosition(); got != bytePos {
		t.Errorf("cursor byte offset = %d, want %d (value %q)", got, bytePos, val)
	}
	prefix := val[:bytePos]
	wantLine := strings.Count(prefix, "\n")
	lineStart := 0
	if i := strings.LastIndexByte(prefix, '\n'); i >= 0 {
		lineStart = i + 1
	}
	wantCol := utf8.RuneCountInString(prefix[lineStart:])
	if got := m.input.Line(); got != wantLine {
		t.Errorf("cursor line = %d, want %d", got, wantLine)
	}
	if got := m.input.Column(); got != wantCol {
		t.Errorf("cursor column = %d, want %d", got, wantCol)
	}
}

func TestTabCompletionLeavesCursorAfterInsertion(t *testing.T) {
	alice := []mentionpicker.User{{ID: "U1", DisplayName: "Alice", Username: "alice"}}
	jose := []mentionpicker.User{{ID: "U2", DisplayName: "José", Username: "jose"}}
	channels := []channelpicker.Channel{{ID: "C1", Name: "engineering", Type: "channel"}}
	emojis := []emoji.EmojiEntry{{Name: "tada", Display: "🎉"}}

	tests := []struct {
		name     string
		draft    string
		at       int
		query    string
		users    []mentionpicker.User
		channels []channelpicker.Channel
		emoji    []emoji.EmojiEntry
		want     string
		cursor   int
	}{
		{
			name:   "mention at start of a multi-line draft",
			draft:  "can you take a look\nat this",
			at:     0,
			query:  "@Al",
			users:  alice,
			want:   "@Alice can you take a look\nat this",
			cursor: len("@Alice "),
		},
		{
			name:   "mention in the middle of a line",
			draft:  "can you take a look at this",
			at:     len("can you "),
			query:  "@Al",
			users:  alice,
			want:   "can you @Alice take a look at this",
			cursor: len("can you @Alice "),
		},
		{
			name:   "mention in the middle of a later line",
			draft:  "first line\nsee this",
			at:     len("first line\nsee "),
			query:  "@Al",
			users:  alice,
			want:   "first line\nsee @Alice this",
			cursor: len("first line\nsee @Alice "),
		},
		{
			name:   "mention with a multi-byte name",
			draft:  "ping the team",
			at:     len("ping "),
			query:  "@Jo",
			users:  jose,
			want:   "ping @José the team",
			cursor: len("ping @José "),
		},
		{
			name:     "channel at the start",
			draft:    "please",
			at:       0,
			query:    "#eng",
			channels: channels,
			want:     "#engineering please",
			cursor:   len("#engineering "),
		},
		{
			name:     "channel in the middle of a later line",
			draft:    "look\nin today",
			at:       len("look\nin "),
			query:    "#eng",
			channels: channels,
			want:     "look\nin #engineering today",
			cursor:   len("look\nin #engineering "),
		},
		{
			name:   "emoji in the middle of a line",
			draft:  "say please",
			at:     len("say "),
			query:  ":tad",
			emoji:  emojis,
			want:   "say :tada: please",
			cursor: len("say :tada: "),
		},
		{
			name:   "emoji at the start of a later line",
			draft:  "wow\nstill here",
			at:     len("wow\n"),
			query:  ":tad",
			emoji:  emojis,
			want:   "wow\n:tada: still here",
			cursor: len("wow\n:tada: "),
		},
		{
			name:   "mention at the end of a multi-line draft",
			draft:  "hello\nthere ",
			at:     len("hello\nthere "),
			query:  "@Al",
			users:  alice,
			want:   "hello\nthere @Alice ",
			cursor: len("hello\nthere @Alice "),
		},
		{
			name:   "mention on the line after a soft-wrapped line",
			draft:  strings.Repeat("a", 80) + "\nsee this",
			at:     80 + len("\nsee "),
			query:  "@Al",
			users:  alice,
			want:   strings.Repeat("a", 80) + "\nsee @Alice this",
			cursor: 80 + len("\nsee @Alice "),
		},
		{
			name:   "mention past the soft wrap on a long line",
			draft:  strings.Repeat("b", 80) + " tail",
			at:     81, // the space after the run of b's
			query:  "@Al",
			users:  alice,
			want:   strings.Repeat("b", 80) + " @Alice tail",
			cursor: 81 + len("@Alice "),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New("general")
			if tt.users != nil {
				m.SetUsers(tt.users)
			}
			if tt.channels != nil {
				m.SetChannels(tt.channels)
			}
			if tt.emoji != nil {
				m.SetEmojiEntries(tt.emoji)
			}
			m = tabComplete(t, m, tt.draft, tt.at, tt.query)
			if m.Value() != tt.want {
				t.Fatalf("value = %q, want %q", m.Value(), tt.want)
			}
			assertCursorAt(t, m, tt.cursor)
		})
	}
}
