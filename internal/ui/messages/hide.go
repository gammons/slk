package messages

// Visible returns msgs without the messages hide reports true for, and
// the oldest TS among those it dropped ("" when none). It never writes
// msgs: callers keep using the list they passed (the mark-read target is
// taken from it, hidden messages included), so when anything is dropped
// the result is a new slice. A nil hide keeps everything.
//
// Both panes call it from every method that takes messages, which is
// what keeps a suppressed author's messages off the screen (see
// docs/superpowers/specs/2026-11-03-suppress-authors-design.md).
func Visible(hide func(MessageItem) bool, msgs []MessageItem) ([]MessageItem, string) {
	if hide == nil {
		return msgs, ""
	}
	first := -1
	for i, msg := range msgs {
		if hide(msg) {
			first = i
			break
		}
	}
	if first < 0 {
		return msgs, ""
	}
	out := make([]MessageItem, first, len(msgs)-1)
	copy(out, msgs[:first])
	oldest := msgs[first].TS
	for _, msg := range msgs[first+1:] {
		if hide(msg) {
			if msg.TS < oldest {
				oldest = msg.TS
			}
			continue
		}
		out = append(out, msg)
	}
	return out, oldest
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
