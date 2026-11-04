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
	"sort"
	"strings"
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

// botStatusExpiry is how long an app's "working" status lasts without
// a clear: Slack's own timeout for assistant.threads.setStatus.
const botStatusExpiry = 2 * time.Minute

// botStatusKey names one app's status on one thread.
type botStatusKey struct {
	threadTS  string
	botUserID string
}

// botStatus is an app's "working" status text and when it lapses.
type botStatus struct {
	text      string
	expiresAt time.Time
}

// typingTracker owns inbound typing-indicator state: people typing and
// apps working (ai_assistant_status), which share the typing row, the
// feature switch and the expiry tick.
type typingTracker struct {
	// users maps channelID -> userID -> expiresAt.
	users map[string]map[string]time.Time
	// bots maps channelID -> (thread, app) -> status.
	bots map[string]map[botStatusKey]botStatus
	// tickerOn is true while a TypingExpiredMsg tea.Tick chain is
	// known to be alive. Guards against UserTypingMsg-burst-driven
	// duplicate chains.
	tickerOn bool
	// enabled is the global feature switch (cfg.Animations.TypingIndicators).
	// When false, Add is a no-op and renderTypingLine returns "".
	enabled bool
	// now is the tracker's clock: time.Now, except in tests.
	now func() time.Time
}

func newTypingTracker() *typingTracker {
	return &typingTracker{
		users: make(map[string]map[string]time.Time),
		bots:  make(map[string]map[botStatusKey]botStatus),
		now:   time.Now,
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
	t.users[channelID][userID] = t.now().Add(typingExpiry)
}

// Expire removes any per-user entries whose expiresAt has passed,
// and drops any channel that ends up empty. Returns true if any
// typers remain after the sweep (i.e. the caller should reschedule
// another TypingExpiredMsg tick); false otherwise, and tickerOn is
// flipped to false so a future Add can start a new chain.
func (t *typingTracker) Expire() bool {
	now := t.now()
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
	for ch, statuses := range t.bots {
		for k, s := range statuses {
			if now.After(s.expiresAt) {
				delete(statuses, k)
			}
		}
		if len(statuses) == 0 {
			delete(t.bots, ch)
		}
	}
	hasTypers := len(t.users) > 0 || len(t.bots) > 0
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
	now := t.now()
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

// SetBotStatus records an app's status on a thread; an empty status
// clears it. Like Add, it does not gate on Enabled().
func (t *typingTracker) SetBotStatus(channelID, threadTS, botUserID, status string) {
	k := botStatusKey{threadTS: threadTS, botUserID: botUserID}
	if status == "" {
		t.dropBotStatus(channelID, func(key botStatusKey) bool { return key == k })
		return
	}
	if t.bots[channelID] == nil {
		t.bots[channelID] = make(map[botStatusKey]botStatus)
	}
	t.bots[channelID][k] = botStatus{text: status, expiresAt: t.now().Add(botStatusExpiry)}
}

// ClearBot drops every status botUserID holds in channelID: its reply
// there ends its work, whichever thread the status named.
func (t *typingTracker) ClearBot(channelID, botUserID string) {
	t.dropBotStatus(channelID, func(key botStatusKey) bool { return key.botUserID == botUserID })
}

func (t *typingTracker) dropBotStatus(channelID string, match func(botStatusKey) bool) {
	statuses := t.bots[channelID]
	for k := range statuses {
		if match(k) {
			delete(statuses, k)
		}
	}
	if len(statuses) == 0 {
		delete(t.bots, channelID)
	}
}

// WorkingBots returns the apps with a live status anywhere in
// channelID, each once, sorted by ID.
func (t *typingTracker) WorkingBots(channelID string) []string {
	now := t.now()
	seen := make(map[string]bool)
	var out []string
	for k, s := range t.bots[channelID] {
		if now.Before(s.expiresAt) && !seen[k.botUserID] {
			seen[k.botUserID] = true
			out = append(out, k.botUserID)
		}
	}
	sort.Strings(out)
	return out
}

// threadBotStatus is one app's live status on a thread.
type threadBotStatus struct {
	botUserID string
	text      string
}

// ThreadBotStatuses returns the live statuses on one thread, sorted by
// app ID.
func (t *typingTracker) ThreadBotStatuses(channelID, threadTS string) []threadBotStatus {
	now := t.now()
	var out []threadBotStatus
	for k, s := range t.bots[channelID] {
		if k.threadTS == threadTS && now.Before(s.expiresAt) {
			out = append(out, threadBotStatus{botUserID: k.botUserID, text: s.text})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].botUserID < out[j].botUserID })
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
		return t.ensureTicker(), true

	case BotStatusMsg:
		if !t.Enabled() {
			return nil, true
		}
		t.SetBotStatus(m.ChannelID, m.ThreadTS, m.BotUserID, m.Status)
		if m.Status == "" {
			return nil, true
		}
		return t.ensureTicker(), true

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

// ensureTicker starts the TypingExpiredMsg chain unless one is alive.
func (t *typingTracker) ensureTicker() tea.Cmd {
	if t.TickerOn() {
		return nil
	}
	t.MarkTickerOn()
	return typingExpiredTickCmd()
}

// typingRowText is the channel pane's typing row: who is typing, then
// which apps are working. Pure, like typingIndicatorText.
func typingRowText(typers, bots []string) string {
	parts := make([]string, 0, 2)
	if s := typingIndicatorText(typers); s != "" {
		parts = append(parts, s)
	}
	if s := botWorkingText(bots); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, " · ")
}

// botWorkingText names the apps working in a channel. The channel pane
// says only that they are working, as Slack's client does; their own
// status text is drawn in the thread it belongs to.
func botWorkingText(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " is working..."
	case 2:
		return names[0] + " and " + names[1] + " are working..."
	default:
		return "Several apps are working..."
	}
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
