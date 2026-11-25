package core

// SuppressRule hides one author's messages: in one conversation, or,
// with ChannelID empty, everywhere in the workspace the rule belongs to.
// AuthorID is the ID a message is attributed to (MessageItem.UserID: a
// user, or a bot's bot_id for a bot message with no user). Note is a
// label for people reading config.toml (the author's name, the
// channel's); it is never matched.
type SuppressRule struct {
	AuthorID  string
	ChannelID string
	Note      string
}

// suppressKey identifies a rule; two rules with the same key are one.
type suppressKey struct{ author, channel string }

func (r SuppressRule) key() suppressKey { return suppressKey{r.AuthorID, r.ChannelID} }

// Suppression is one workspace's rule set. It is immutable: With and
// Without return a new set. Its maps are built in NewSuppression and
// never written again, so a set may be read from any goroutine. The nil
// *Suppression is valid and hides nothing.
type Suppression struct {
	rules []SuppressRule
	index map[suppressKey]bool
}

// NewSuppression builds a set from rules, in order, dropping rules
// without an author and later duplicates (same author and channel).
func NewSuppression(rules []SuppressRule) *Suppression {
	s := &Suppression{index: make(map[suppressKey]bool, len(rules))}
	for _, r := range rules {
		if r.AuthorID == "" || s.index[r.key()] {
			continue
		}
		s.index[r.key()] = true
		s.rules = append(s.rules, r)
	}
	return s
}

// Hides reports whether authorID's messages are hidden in channelID: a
// rule for that conversation or a workspace-wide rule. An empty
// authorID is never hidden.
func (s *Suppression) Hides(channelID, authorID string) bool {
	if s == nil || authorID == "" {
		return false
	}
	return s.index[suppressKey{authorID, ""}] ||
		(channelID != "" && s.index[suppressKey{authorID, channelID}])
}

// HidesMessage reports whether msg is hidden in channelID, by its author
// (UserID: the user, or the bot ID of a bot message with no user, the
// same ID cmd/slk attributes a live message to).
func (s *Suppression) HidesMessage(channelID string, msg MessageItem) bool {
	return s.Hides(channelID, msg.UserID)
}

// Has reports whether the set holds a rule with r's author and channel.
func (s *Suppression) Has(r SuppressRule) bool {
	return s != nil && s.index[r.key()]
}

// Rules returns a copy of the rules, in the order they were added.
func (s *Suppression) Rules() []SuppressRule {
	if s == nil {
		return nil
	}
	return append([]SuppressRule(nil), s.rules...)
}

// Len is the number of rules.
func (s *Suppression) Len() int {
	if s == nil {
		return 0
	}
	return len(s.rules)
}

// With returns a set with r added at the end (s itself when it already
// holds r).
func (s *Suppression) With(r SuppressRule) *Suppression {
	if s.Has(r) {
		return s
	}
	return NewSuppression(append(s.Rules(), r))
}

// Without returns a set without the rule matching r's author and
// channel.
func (s *Suppression) Without(r SuppressRule) *Suppression {
	kept := make([]SuppressRule, 0, s.Len())
	for _, x := range s.Rules() {
		if x.key() != r.key() {
			kept = append(kept, x)
		}
	}
	return NewSuppression(kept)
}
