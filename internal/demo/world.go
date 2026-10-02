package demo

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/sidebar"
)

// TimestampFormat is the clock format every demo message is stamped with.
const TimestampFormat = "3:04 PM"

// neverRead is Slack's last_read sentinel for a conversation the user has
// never read.
const neverRead = "0000000000.000000"

// Fixture specs: the input newWorld builds a World from. Times are
// offsets back from "now" so the day separators read Today and Yesterday
// whenever the demo runs.
type (
	teamSpec struct {
		id, name, domain, theme string
		selfID                  string
		responder               string // who answers the user outside DMs
		firstChannel            string // opened at startup
		users                   []user
		channels                []channelSpec
	}
	channelSpec struct {
		id, name, typ, section string
		sectionOrder           int
		dmUserID               string
		unread                 int // trailing top-level messages not yet read
		mentions               int
		msgs                   []msgSpec
	}
	msgSpec struct {
		user        string
		ago         time.Duration
		text        string
		edited      bool
		reactions   []reactSpec
		replies     []msgSpec
		legacy      []blocks.LegacyAttachment
		attachments []core.Attachment
	}
	reactSpec struct {
		emoji string
		users []string
	}
	user struct {
		id, name string
		presence string // "active" or "away"; shown on DM rows
		status   peerstatus.Status
		title    string // shown in the profile dialog
		pronouns string
		tz       string // IANA name, e.g. "America/Los_Angeles"
		tzAbbrev string // "PDT"
		tzOffset int    // seconds east of UTC
	}
)

type channel struct {
	id, name, typ, section string
	sectionOrder           int
	dmUserID               string
	teamID                 string
	messages               []core.MessageItem            // top level, ascending TS
	replies                map[string][]core.MessageItem // parent TS -> replies, ascending TS
	lastRead               string
	mentions               int
}

type team struct {
	id, name, domain, theme string
	selfID, responder       string
	firstChannel            string
	users                   []user
	channels                []*channel
}

// World is the demo's in-memory Slack. The fake services read and write
// it from Bubble Tea's command goroutines and the director from its own,
// so every method takes the lock, and every message handed out is a deep
// copy the caller owns.
type World struct {
	mu     sync.Mutex
	clock  func() time.Time
	seq    int
	teams  []*team
	byID   map[string]*channel
	active string
}

func newWorld(specs []teamSpec, now time.Time, clock func() time.Time) *World {
	w := &World{clock: clock, byID: map[string]*channel{}}
	for _, ts := range specs {
		t := &team{id: ts.id, name: ts.name, domain: ts.domain, theme: ts.theme,
			selfID: ts.selfID, responder: ts.responder, firstChannel: ts.firstChannel, users: ts.users}
		for _, cs := range ts.channels {
			c := &channel{id: cs.id, name: cs.name, typ: cs.typ, section: cs.section,
				sectionOrder: cs.sectionOrder, dmUserID: cs.dmUserID, teamID: t.id,
				replies: map[string][]core.MessageItem{}, mentions: cs.mentions}
			for _, ms := range cs.msgs {
				parent := w.fixtureItem(t, ms, now)
				for _, rs := range ms.replies {
					r := w.fixtureItem(t, rs, now)
					r.ThreadTS = parent.TS
					c.replies[parent.TS] = append(c.replies[parent.TS], r)
				}
				if n := len(ms.replies); n > 0 {
					parent.ThreadTS = parent.TS
					parent.ReplyCount = n
				}
				c.messages = append(c.messages, parent)
			}
			c.lastRead = initialLastRead(c.messages, cs.unread)
			t.channels = append(t.channels, c)
			w.byID[c.id] = c
		}
		w.teams = append(w.teams, t)
	}
	if len(w.teams) > 0 {
		w.active = w.teams[0].id
	}
	return w
}

func initialLastRead(msgs []core.MessageItem, unread int) string {
	i := len(msgs) - unread - 1
	switch {
	case len(msgs) == 0:
		return ""
	case i < 0:
		return neverRead
	default:
		return msgs[i].TS
	}
}

// stamp returns a Slack-shaped timestamp for at. The seconds part has ten
// digits for any date this demo can run on, so timestamps compare
// correctly as strings; seq keeps them unique within a second.
func (w *World) stamp(at time.Time) string {
	w.seq++
	return fmt.Sprintf("%d.%06d", at.Unix(), w.seq)
}

func (w *World) newItem(t *team, userID, txt string, at time.Time) core.MessageItem {
	return core.MessageItem{
		TS:        w.stamp(at),
		UserID:    userID,
		UserName:  t.userName(userID),
		Text:      txt,
		Timestamp: at.Format(TimestampFormat),
		DateStr:   at.Format("2006-01-02"),
	}
}

func (w *World) fixtureItem(t *team, ms msgSpec, now time.Time) core.MessageItem {
	m := w.newItem(t, ms.user, ms.text, now.Add(-ms.ago))
	m.IsEdited = ms.edited
	m.LegacyAttachments = ms.legacy
	m.Attachments = ms.attachments
	for _, r := range ms.reactions {
		m.Reactions = append(m.Reactions, core.ReactionItem{
			Emoji:      r.emoji,
			Count:      len(r.users),
			UserIDs:    slices.Clone(r.users),
			HasReacted: slices.Contains(r.users, t.selfID),
		})
	}
	return m
}

func (t *team) userName(id string) string {
	for _, u := range t.users {
		if u.id == id {
			return u.name
		}
	}
	return id
}

// hasUser reports whether id is one of the team's users.
func (t *team) hasUser(id string) bool {
	return slices.ContainsFunc(t.users, func(u user) bool { return u.id == id })
}

func (c *channel) latestTS() string {
	if len(c.messages) == 0 {
		return ""
	}
	return c.messages[len(c.messages)-1].TS
}

func (c *channel) hasUnread() bool {
	l := c.latestTS()
	return l != "" && l > c.lastRead
}

func (c *channel) readState() core.ReadState {
	rs := core.ReadState{LastReadTS: c.lastRead, HasUnread: c.hasUnread()}
	if rs.HasUnread {
		rs.MentionCount = c.mentions
	}
	return rs
}

func (c *channel) findTop(ts string) *core.MessageItem {
	for i := range c.messages {
		if c.messages[i].TS == ts {
			return &c.messages[i]
		}
	}
	return nil
}

func (c *channel) find(ts string) *core.MessageItem {
	if m := c.findTop(ts); m != nil {
		return m
	}
	for _, rs := range c.replies {
		for i := range rs {
			if rs[i].TS == ts {
				return &rs[i]
			}
		}
	}
	return nil
}

func cloneItem(m core.MessageItem) core.MessageItem {
	m.Reactions = slices.Clone(m.Reactions)
	for i := range m.Reactions {
		m.Reactions[i].UserIDs = slices.Clone(m.Reactions[i].UserIDs)
	}
	m.Attachments = slices.Clone(m.Attachments)
	m.LegacyAttachments = slices.Clone(m.LegacyAttachments)
	return m
}

func cloneItems(ms []core.MessageItem) []core.MessageItem {
	out := make([]core.MessageItem, len(ms))
	for i, m := range ms {
		out[i] = cloneItem(m)
	}
	return out
}

// profile looks up userID's profile in team teamID, built from the
// fixture's title/pronouns/timezone fields. It errors for an unknown
// team or user, as the real Slack API would.
func (w *World) profile(teamID, userID string) (core.UserProfile, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	t := w.teamByID(teamID)
	if t == nil {
		return core.UserProfile{}, fmt.Errorf("demo: unknown team %q", teamID)
	}
	for _, u := range t.users {
		if u.id != userID {
			continue
		}
		handle := strings.ToLower(strings.ReplaceAll(u.name, " ", ""))
		return core.UserProfile{
			UserID:      u.id,
			TeamID:      t.id,
			Handle:      handle,
			RealName:    u.name,
			DisplayName: u.name,
			Title:       u.title,
			Pronouns:    u.pronouns,
			Email:       handle + "@example.com",
			TZ:          u.tz,
			TZAbbrev:    u.tzAbbrev,
			TZOffset:    u.tzOffset,
			IsBot:       u.id == bDeploy,
		}, nil
	}
	return core.UserProfile{}, fmt.Errorf("demo: unknown user %q in team %q", userID, teamID)
}
func (w *World) teamByID(id string) *team {
	for _, t := range w.teams {
		if t.id == id {
			return t
		}
	}
	return nil
}

func (w *World) teamOf(channelID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c := w.byID[channelID]; c != nil {
		return c.teamID
	}
	return ""
}

// selfIn is the user's own ID in the workspace that owns channelID.
func (w *World) selfIn(channelID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c := w.byID[channelID]; c != nil {
		return w.teamByID(c.teamID).selfID
	}
	return ""
}

// responder is who answers the user in channelID: the peer in a DM, the
// workspace's designated responder anywhere else.
func (w *World) responder(channelID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	switch {
	case c == nil:
		return ""
	case c.dmUserID != "":
		return c.dmUserID
	default:
		return w.teamByID(c.teamID).responder
	}
}

func (w *World) activeTeam() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active
}

func (w *World) setActive(teamID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.teamByID(teamID) == nil {
		return false
	}
	w.active = teamID
	return true
}

// messages returns the channel's feed and its read cursor.
func (w *World) messages(channelID string) ([]core.MessageItem, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil {
		return nil, ""
	}
	return cloneItems(c.messages), c.lastRead
}

func (w *World) replies(channelID, threadTS string) []core.MessageItem {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil {
		return nil
	}
	return cloneItems(c.replies[threadTS])
}

func (w *World) latestTS(channelID string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c := w.byID[channelID]; c != nil {
		return c.latestTS()
	}
	return ""
}

// markRead advances the read cursor to ts. It never moves it backwards:
// the App's mark flush is debounced and can land after a newer mark.
func (w *World) markRead(channelID, ts string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil {
		return
	}
	if ts > c.lastRead {
		c.lastRead = ts
	}
	if !c.hasUnread() {
		c.mentions = 0
	}
}

// setLastRead moves the cursor anywhere, backwards included: it is the
// user's explicit "mark unread".
func (w *World) setLastRead(channelID, ts string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c := w.byID[channelID]; c != nil {
		c.lastRead = ts
	}
}

// post adds a message by userID, now. A non-empty threadTS makes it a
// reply to that parent; broadcast also puts it in the channel feed.
// Messages the user sends leave the channel read; other people's leave it
// unread, and count as a mention when they name the user. atts are attached
// as given.
func (w *World) post(channelID, threadTS, userID, txt string, broadcast bool, atts ...core.Attachment) (core.MessageItem, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil {
		return core.MessageItem{}, false
	}
	var parent *core.MessageItem
	if threadTS != "" {
		if parent = c.findTop(threadTS); parent == nil {
			return core.MessageItem{}, false
		}
	}
	t := w.teamByID(c.teamID)
	if !t.hasUser(userID) {
		return core.MessageItem{}, false
	}
	m := w.newItem(t, userID, txt, w.clock())
	m.Attachments = slices.Clone(atts)
	if parent != nil {
		parent.ThreadTS = threadTS
		parent.ReplyCount++
		m.ThreadTS = threadTS
		if broadcast {
			m.Subtype = "thread_broadcast"
		}
		c.replies[threadTS] = append(c.replies[threadTS], m)
	}
	if parent == nil || broadcast {
		c.messages = append(c.messages, m)
		switch {
		case userID == t.selfID:
			c.lastRead = m.TS
			c.mentions = 0
		case strings.Contains(txt, "<@"+t.selfID+">"):
			c.mentions++
		}
	}
	return cloneItem(m), true
}

// react adds (or, with remove, takes away) userID's emoji reaction on the
// message ts, top level or reply. Idempotent per (emoji, user).
func (w *World) react(channelID, ts, userID, emoji string, remove bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil {
		return false
	}
	m := c.find(ts)
	if m == nil {
		return false
	}
	m.Reactions = applyReaction(m.Reactions, emoji, userID, w.teamByID(c.teamID).selfID, remove)
	return true
}

func applyReaction(rs []core.ReactionItem, emoji, userID, selfID string, remove bool) []core.ReactionItem {
	i := slices.IndexFunc(rs, func(r core.ReactionItem) bool { return r.Emoji == emoji })
	if i < 0 {
		if remove {
			return rs
		}
		rs = append(rs, core.ReactionItem{Emoji: emoji})
		i = len(rs) - 1
	}
	r := &rs[i]
	has := slices.Contains(r.UserIDs, userID)
	switch {
	case remove && has:
		r.UserIDs = slices.DeleteFunc(slices.Clone(r.UserIDs), func(u string) bool { return u == userID })
	case !remove && !has:
		r.UserIDs = append(slices.Clone(r.UserIDs), userID)
	default:
		return rs
	}
	r.Count = len(r.UserIDs)
	r.HasReacted = slices.Contains(r.UserIDs, selfID)
	if r.Count == 0 {
		rs = slices.Delete(rs, i, i+1)
	}
	return rs
}

// readStates is the active workspace's per-channel read state.
func (w *World) readStates() map[string]core.ReadState {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]core.ReadState{}
	if t := w.teamByID(w.active); t != nil {
		for _, c := range t.channels {
			out[c.id] = c.readState()
		}
	}
	return out
}

// unreadTeams lists, in rail order, every workspace with an unread channel.
func (w *World) unreadTeams() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, t := range w.teams {
		if slices.ContainsFunc(t.channels, (*channel).hasUnread) {
			out = append(out, t.id)
		}
	}
	return out
}

// lookup answers for the active workspace only, as the port requires.
func (w *World) lookup(channelID string) (string, string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil || c.teamID != w.active {
		return "", "", false
	}
	return c.name, c.typ, true
}

// search returns the TSes of the channel's messages containing query,
// case- and accent-insensitively, newest first.
func (w *World) search(channelID, query string) []string {
	q := text.Fold(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c := w.byID[channelID]
	if c == nil {
		return nil
	}
	var out []string
	for i := len(c.messages) - 1; i >= 0; i-- {
		if strings.Contains(text.Fold(c.messages[i].Text), q) {
			out = append(out, c.messages[i].TS)
		}
	}
	return out
}

// threadSummaries lists the threads the user started or replied to.
func (w *World) threadSummaries(teamID string) []core.ThreadSummary {
	w.mu.Lock()
	defer w.mu.Unlock()
	t := w.teamByID(teamID)
	if t == nil {
		return nil
	}
	var out []core.ThreadSummary
	for _, c := range t.channels {
		for _, p := range c.messages {
			rs := c.replies[p.TS]
			if len(rs) == 0 {
				continue
			}
			mine := func(m core.MessageItem) bool { return m.UserID == t.selfID }
			if !mine(p) && !slices.ContainsFunc(rs, mine) {
				continue
			}
			last := rs[len(rs)-1]
			out = append(out, core.ThreadSummary{
				ChannelID: c.id, ChannelName: c.name, ChannelType: c.typ,
				ThreadTS: p.TS, ParentUserID: p.UserID, ParentText: p.Text, ParentTS: p.TS,
				ReplyCount: len(rs), LastReplyTS: last.TS, LastReplyBy: last.UserID,
			})
		}
	}
	return out
}

// teamSnapshot is everything the App's workspace-ready and
// workspace-switched messages need about one team.
type teamSnapshot struct {
	id, name, domain, theme string
	selfID, firstChannel    string
	channels                []sidebar.ChannelItem
	finder                  []core.ChannelFinderItem
	userNames               map[string]string
	statuses                map[string]peerstatus.Status
}

func (t *team) snapshot() teamSnapshot {
	s := teamSnapshot{
		id: t.id, name: t.name, domain: t.domain, theme: t.theme,
		selfID: t.selfID, firstChannel: t.firstChannel,
		userNames: map[string]string{}, statuses: map[string]peerstatus.Status{},
	}
	presence := map[string]string{}
	for _, u := range t.users {
		s.userNames[u.id] = u.name
		presence[u.id] = u.presence
		if u.status != (peerstatus.Status{}) {
			s.statuses[u.id] = u.status
		}
	}
	for _, c := range t.channels {
		s.channels = append(s.channels, sidebar.ChannelItem{
			ID: c.id, Name: c.name, Type: c.typ, Section: c.section, SectionOrder: c.sectionOrder,
			DMUserID: c.dmUserID, Presence: presence[c.dmUserID],
		})
		s.finder = append(s.finder, core.ChannelFinderItem{
			ID: c.id, Name: c.name, Type: c.typ, Presence: presence[c.dmUserID], Joined: true,
		})
	}
	return s
}

// snapshots returns every team, in rail order.
func (w *World) snapshots() []teamSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]teamSnapshot, 0, len(w.teams))
	for _, t := range w.teams {
		out = append(out, t.snapshot())
	}
	return out
}

func (w *World) snapshot(teamID string) (teamSnapshot, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t := w.teamByID(teamID); t != nil {
		return t.snapshot(), true
	}
	return teamSnapshot{}, false
}
