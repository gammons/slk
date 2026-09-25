// internal/ui/typing.go
//
// Typing indicator state — both inbound (other users typing in a
// channel) and outbound (our own typing send throttle).
//
// Phase 2d of the SOLID refactor of internal/ui/app.go: extracts the
// five typing-related fields (typingUsers, typingTickerOn,
// typingEnabled, typingSendFn, lastTypingSent) and eight methods
// (addTypingUser, expireTypingUsers, getTypingUsers,
// getTypingUsersFiltered, typingIndicatorText, shouldSendTyping,
// maybeSendTyping, plus the rendering helper renderTypingLine which
// stays on App because it pulls in messagepane name-resolution and
// styles).
//
// Two cohesive types live here:
//
//	typingTracker      Inbound state: per-channel "who is typing now"
//	                   map, the global enabled flag, and the ticker-
//	                   alive bookkeeping (one tea.Tick chain at a time
//	                   regardless of how many UserTypingMsg's arrive).
//
//	typingBroadcaster  Outbound throttle: holds the TypingSendFunc and
//	                   the wall clock of the last self-emit. Refuses
//	                   to send more than once per 3 seconds.
//
// The broadcaster holds a *typingTracker reference so it can consult
// the shared Enabled() flag without forcing the App to re-check it at
// every call site.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// typingExpiry is how long a typing indicator from a remote user
// stays visible after the most recent UserTypingMsg.
const typingExpiry = 5 * time.Second

// typingThrottle is the minimum interval between successive
// chat.typing emissions for the local user. Slack itself only
// re-publishes typing pings every few seconds.
const typingThrottle = 3 * time.Second

// typingTracker owns inbound typing-indicator state.
type typingTracker struct {
	// users maps channelID -> userID -> expiresAt.
	users map[string]map[string]time.Time
	// tickerOn is true while a TypingExpiredMsg tea.Tick chain is
	// known to be alive. Guards against UserTypingMsg-burst-driven
	// duplicate chains.
	tickerOn bool
	// enabled is the global feature switch (cfg.Animations.TypingIndicators).
	// When false, Add is a no-op and renderTypingLine returns "".
	enabled bool
}

func newTypingTracker() *typingTracker {
	return &typingTracker{
		users: make(map[string]map[string]time.Time),
	}
}

// Enabled returns the global typing-indicator on/off flag.
func (t *typingTracker) Enabled() bool { return t.enabled }

// SetEnabled toggles the global typing-indicator flag.
func (t *typingTracker) SetEnabled(b bool) { t.enabled = b }

// TickerOn reports whether a TypingExpiredMsg tick chain is alive.
func (t *typingTracker) TickerOn() bool { return t.tickerOn }

// MarkTickerOn records that the caller has just scheduled a
// TypingExpiredMsg tick chain. Subsequent Adds for any channel skip
// scheduling another until Expire() reports the chain should die.
func (t *typingTracker) MarkTickerOn() { t.tickerOn = true }

// Add records that userID is typing in channelID. The indicator
// persists for typingExpiry from now. Does NOT gate on Enabled() —
// the Update arm checks Enabled() before calling Add (and tests use
// Add to seed state without flipping the global feature switch).
func (t *typingTracker) Add(channelID, userID string) {
	if t.users[channelID] == nil {
		t.users[channelID] = make(map[string]time.Time)
	}
	t.users[channelID][userID] = time.Now().Add(typingExpiry)
}

// Expire removes any per-user entries whose expiresAt has passed,
// and drops any channel that ends up empty. Returns true if any
// typers remain after the sweep (i.e. the caller should reschedule
// another TypingExpiredMsg tick); false otherwise, and tickerOn is
// flipped to false so a future Add can start a new chain.
func (t *typingTracker) Expire() bool {
	now := time.Now()
	for ch, users := range t.users {
		for uid, expires := range users {
			if now.After(expires) {
				delete(users, uid)
			}
		}
		if len(users) == 0 {
			delete(t.users, ch)
		}
	}
	hasTypers := len(t.users) > 0
	t.tickerOn = hasTypers
	return hasTypers
}

// Users returns the user IDs currently typing in channelID. Filters
// out any entries whose expiresAt has already passed (defensive — a
// late Expire-tick can leave momentarily stale entries).
func (t *typingTracker) Users(channelID string) []string {
	users := t.users[channelID]
	if len(users) == 0 {
		return nil
	}
	now := time.Now()
	var result []string
	for uid, expires := range users {
		if now.Before(expires) {
			result = append(result, uid)
		}
	}
	return result
}

// UsersExcluding returns the user IDs currently typing in channelID,
// skipping the excluded userID. Used by the renderer to hide the local
// user's own self-typing echo.
func (t *typingTracker) UsersExcluding(channelID, exclude string) []string {
	all := t.Users(channelID)
	if len(all) == 0 {
		return nil
	}
	out := make([]string, 0, len(all))
	for _, uid := range all {
		if uid != exclude {
			out = append(out, uid)
		}
	}
	return out
}

// typingBroadcaster owns the outbound typing-emit throttle.
type typingBroadcaster struct {
	tracker  *typingTracker // for Enabled() check
	sendFn   TypingSendFunc
	lastSent time.Time
}

func newTypingBroadcaster(tracker *typingTracker) *typingBroadcaster {
	return &typingBroadcaster{tracker: tracker}
}

// SetSender wires the function used to emit a typing event for a
// channel. nil disables outbound sends.
func (b *typingBroadcaster) SetSender(fn TypingSendFunc) { b.sendFn = fn }

// ResetThrottle clears the last-sent wall clock so the next MaybeSend
// for any channel can fire immediately. Called when the user switches
// channels: throttle is per-app, not per-channel, but the user-visible
// "I just started typing here" deserves an immediate ping in the new
// channel rather than waiting for the residual throttle window.
func (b *typingBroadcaster) ResetThrottle() { b.lastSent = time.Time{} }

// CanSend reports whether the broadcaster would dispatch right now.
// Used by the test suite; production code calls MaybeSend directly.
func (b *typingBroadcaster) CanSend() bool {
	if !b.tracker.Enabled() {
		return false
	}
	return time.Since(b.lastSent) >= typingThrottle
}

// MaybeSend dispatches a typing event for channelID via the configured
// TypingSendFunc, but only if enabled, throttled-clear, and the
// function has been wired. Returns true if it actually dispatched.
// The dispatch itself happens on a goroutine so the Update loop is
// never blocked by the HTTP call.
func (b *typingBroadcaster) MaybeSend(channelID string) bool {
	if b.sendFn == nil || !b.CanSend() {
		return false
	}
	b.lastSent = time.Now()
	fn := b.sendFn
	go fn(channelID)
	return true
}

// typingExpiredTickCmd schedules the next TypingExpiredMsg one
// second out. Used by both the UserTypingMsg arm (kick the chain
// alive) and TypingExpiredMsg arm (reschedule while typers remain).
func typingExpiredTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return TypingExpiredMsg{}
	})
}

// Handle is the typing-family reducer for App.Update (Phase 4d).
// Owns UserTypingMsg (record a remote typer + ensure the expiry
// tick chain is alive) and TypingExpiredMsg (sweep expired entries
// + maybe reschedule). Returns (nil, false) for any other message
// type.
//
// The outbound throttle (typingBroadcaster.MaybeSend) is invoked
// from compose/insert-mode handlers, not from Update, so it's not
// part of this reducer.
func (t *typingTracker) Handle(a *App, msg tea.Msg) (tea.Cmd, bool) {
	_ = a
	switch m := msg.(type) {
	case UserTypingMsg:
		if !t.Enabled() {
			return nil, true
		}
		t.Add(m.ChannelID, m.UserID)
		if t.TickerOn() {
			return nil, true
		}
		t.MarkTickerOn()
		return typingExpiredTickCmd(), true

	case TypingExpiredMsg:
		_ = m
		// Continue ticking if there are still active typers.
		if !t.Expire() {
			return nil, true
		}
		return typingExpiredTickCmd(), true
	}
	return nil, false
}

// typingIndicatorText formats the user-facing line for the supplied
// display names. Pure: no state, no styling, no name resolution.
func typingIndicatorText(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is typing..."
	case 2:
		return names[0] + " and " + names[1] + " are typing..."
	default:
		return "Several people are typing..."
	}
}
