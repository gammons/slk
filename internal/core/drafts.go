package core

import (
	"errors"
	"time"
)

// DraftKey names a conversation's composer: a channel or DM, or a thread
// in one when ThreadTS is set. compose.Model keys its slots the same way.
type DraftKey struct {
	TeamID    string
	ChannelID string
	ThreadTS  string
}

// Draft is one of the user's active Slack drafts.
type Draft struct {
	ID string
	// Key is the draft's conversation; ChannelID is "" when the draft
	// has no single recipient (a "New message" draft).
	Key DraftKey
	// Text is the composer markdown, mentions in wire form (<@U…>).
	Text    string
	Updated time.Time
	// Scheduled is when Slack will send it; zero unless scheduled.
	Scheduled time.Time
	FileCount int
	// Saved is Slack's is_from_composer: a draft kept with "Save as
	// draft", or started in "New message". Never a composer's text.
	Saved bool
}

// Writable reports a draft slk restores into a composer and overwrites:
// a conversation's composer text, unscheduled, without files.
func (d Draft) Writable() bool {
	return !d.Saved && d.Key.ChannelID != "" && d.Scheduled.IsZero() && d.FileCount == 0
}

// DraftSaveResult is a save's answer. Replaced reports that a newer
// draft from another device was overwritten.
type DraftSaveResult struct {
	Draft    Draft
	Replaced bool
}

// DraftChange is a slot whose text Slack does not have yet; Text "" means
// the draft is to be discarded. Text is in wire form.
type DraftChange struct {
	Key    DraftKey
	Text   string
	Edited time.Time
}

// ErrDraftHandsOff is Save's answer when the conversation's Slack draft is
// one slk won't overwrite (scheduled, with files, or a deleted draft
// still holding the slot).
var ErrDraftHandsOff = errors.New("slack already has a draft here that slk won't overwrite")

// ErrDraftStale is Save's answer when current (see DraftService.Save)
// reports the save is no longer current: a send or delete for the same
// conversation ran first. No API call was made.
var ErrDraftStale = errors.New("draft save no longer current")
