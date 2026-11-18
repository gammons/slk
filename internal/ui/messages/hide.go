package messages

// Visible returns msgs without the messages hide reports true for, and
// the oldest and newest TS among those it dropped ("" when none). It never writes
// msgs: callers keep using the list they passed (the mark-read target is
// taken from it, hidden messages included), so when anything is dropped
// the result is a new slice. A nil hide keeps everything.
//
// Both panes call it from every method that takes messages, which is
// what keeps a suppressed author's messages off the screen (see
// docs/superpowers/specs/2026-11-03-suppress-authors-design.md).
func Visible(hide func(MessageItem) bool, msgs []MessageItem) (out []MessageItem, oldest, newest string) {
	if hide == nil {
		return msgs, "", ""
	}
	first := -1
	for i, msg := range msgs {
		if hide(msg) {
			first = i
			break
		}
	}
	if first < 0 {
		return msgs, "", ""
	}
	out = make([]MessageItem, first, len(msgs)-1)
	copy(out, msgs[:first])
	oldest, newest = msgs[first].TS, msgs[first].TS
	for _, msg := range msgs[first+1:] {
		if hide(msg) {
			oldest, newest = min(oldest, msg.TS), max(newest, msg.TS)
			continue
		}
		out = append(out, msg)
	}
	return out, oldest, newest
}

// SetHide sets the predicate for messages the pane must not show (a
// suppressed author's). It applies to messages given from now on;
// SetMessages(Messages()) applies it to those already shown.
func (m *Model) SetHide(hide func(MessageItem) bool) {
	m.hide = hide
}

// keepOlderHidden records ts as the oldest hidden message the pane was
// given, when it is older than the one recorded.
func (m *Model) keepOlderHidden(ts string) {
	if ts != "" && (m.hiddenOldest == "" || ts < m.hiddenOldest) {
		m.hiddenOldest = ts
	}
}

// keepNewerHidden records ts as the newest hidden message the pane was
// given, when it is newer than the one recorded.
func (m *Model) keepNewerHidden(ts string) {
	if ts > m.hiddenNewest {
		m.hiddenNewest = ts
	}
}

// NewestTS is the newest message the pane holds, or a hidden one it was
// given that is newer still: with OldestTS, the range of history the
// pane has loaded.
func (m *Model) NewestTS() string {
	newest := m.hiddenNewest
	if n := len(m.messages); n > 0 && m.messages[n-1].TS > newest {
		newest = m.messages[n-1].TS
	}
	return newest
}

// onlyHidden reports whether the pane shows nothing because every
// message it was given is hidden. Such a pane is at the top of what it
// holds, so scrolling up must still fetch older history: a page wholly
// from a suppressed author would otherwise block everything before it.
func (m *Model) onlyHidden() bool {
	return len(m.messages) == 0 && m.hiddenOldest != ""
}
