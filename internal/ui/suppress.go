// internal/ui/suppress.go
//
// Hiding authors' messages (docs/superpowers/specs/2026-11-03-suppress-authors-design.md):
// the hide predicates the App gives each pane, the :suppress prompt, the
// :suppressed list, and refiltering open panes after a rule changes.
// The rules themselves are the SuppressionService's (cmd/slk keeps them
// in config.toml); the App reads them whenever a pane takes messages.
package ui

import (
	"errors"
	"log"
	"sort"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/suppresslist"
	"github.com/gammons/slk/internal/ui/wintree"
)

var noopSuppressionService = core.NewSuppressionService(nil, nil, nil)

// SetSuppressionService wires the suppression rules.
func (a *App) SetSuppressionService(s core.SuppressionService) {
	if s == nil {
		s = noopSuppressionService
	}
	a.suppression = s
}

// hidesIn reports whether msg is hidden in channelID of the active
// workspace. The user's own messages and optimistic placeholders never
// are, whatever the rules say.
func (a *App) hidesIn(channelID string, msg messages.MessageItem) bool {
	if a.isOwnMessage(msg) {
		return false
	}
	return a.suppression.Rules(a.activeTeamID).HidesMessage(channelID, msg)
}

// installChannelHide gives a channel-pane model its hide predicate. The
// model's channel is looked up when a message arrives, by the routing
// modelsForChannel uses, so a model keeps the right rules as its window
// changes channel.
func (a *App) installChannelHide(m *messages.Model) {
	m.SetHide(func(msg messages.MessageItem) bool { return a.hidesIn(a.channelOfModel(m), msg) })
}

// installThreadHide gives the thread pane its predicate, keyed on the
// pane's own channel.
func (a *App) installThreadHide() {
	a.threadPanel.SetHide(func(msg messages.MessageItem) bool {
		return a.hidesIn(a.threadPanel.ChannelID(), msg)
	})
}

// leafChannel is the channel window id shows: the focused window's is
// activeChannelID (see modelsForChannel), the others' their tree record.
func (a *App) leafChannel(id wintree.LeafID) string {
	if id == a.focusedWin {
		return a.activeChannelID
	}
	if ch, ok := a.wins.Channel(id); ok {
		return ch.ID
	}
	return ""
}

// channelOfModel is the channel model shows, "" when it is no window's.
func (a *App) channelOfModel(m *messages.Model) string {
	for _, id := range a.wins.Leaves() {
		if a.winModels[id] == m {
			return a.leafChannel(id)
		}
	}
	return ""
}

// reapplySuppression filters every open pane against the rules as
// they stand, after a rule was added. Selection stays on the same
// message when it is still shown. A removed rule's messages are not in
// any pane, so a removal reloads from the cache instead
// (reloadAfterRuleRemoval).
func (a *App) reapplySuppression() {
	for _, id := range a.wins.Leaves() {
		if m := a.winModels[id]; m != nil && a.leafChannel(id) != "" {
			setMessagesKeepSelection(m, cloneMessageItems(m.Messages()))
		}
	}
	if a.threadVisible {
		tp := a.threadPanel
		a.setThreadRepliesKeepSelection(cloneMessageItems(tp.Replies()))
	}
}

// setMessagesKeepSelection refills m (messages.Model.Refill, which keeps
// the range of history it has loaded), keeping the selected message
// selected when it is still shown.
func setMessagesKeepSelection(m *messages.Model, list []messages.MessageItem) {
	sel, hadSel := m.SelectedMessage()
	m.Refill(list)
	if hadSel {
		m.SelectByTS(sel.TS)
	}
}

// setThreadRepliesKeepSelection replaces the thread pane's replies,
// keeping the selected message selected (the parent included) when it
// is still shown.
func (a *App) setThreadRepliesKeepSelection(replies []messages.MessageItem) {
	tp := a.threadPanel
	sel, hadSel := tp.SelectedMessage()
	parentSel := hadSel && sel.TS == tp.ParentMsg().TS
	tp.SetThread(tp.ParentMsg(), replies, tp.ChannelID(), tp.ThreadTS())
	switch {
	case parentSel:
		// SetThread selects the newest reply; the parent is not one
		// of the replies, so it is restored on its own.
		tp.SelectParent()
	case hadSel:
		for i, r := range tp.Replies() {
			if r.TS == sel.TS {
				tp.SelectByIndex(i)
				break
			}
		}
	}
}

// suppressionReloadedMsg carries the cache reads reloadAfterRuleRemoval
// made off the Update goroutine. seq names the reload; only the latest
// one is applied.
type suppressionReloadedMsg struct {
	seq     uint64
	windows []windowReload
	thread  *threadReload
}

// suppressReloadState is the reload in flight: its seq, and the messages
// deleted while it reads. A read can capture a message just before its
// delete arrives, and adding that copy back would resurrect it.
type suppressReloadState struct {
	seq     uint64
	deleted map[string]map[string]bool // channel ID -> deleted TSes
}

// noteDeleted records a delete that arrived while a reload is in
// flight. Nil-safe: no reload, nothing to record.
func (s *suppressReloadState) noteDeleted(channelID, ts string) {
	if s == nil {
		return
	}
	if s.deleted[channelID] == nil {
		s.deleted[channelID] = make(map[string]bool)
	}
	s.deleted[channelID][ts] = true
}

// withoutDeleted returns cached without the messages deleted in
// channelID while the reload ran.
func (s *suppressReloadState) withoutDeleted(channelID string, cached []messages.MessageItem) []messages.MessageItem {
	gone := s.deleted[channelID]
	if len(gone) == 0 {
		return cached
	}
	out := make([]messages.MessageItem, 0, len(cached))
	for _, m := range cached {
		if !gone[m.TS] {
			out = append(out, m)
		}
	}
	return out
}

// windowReload is one window's cached history from since to until, the
// oldest and newest messages it held (hidden ones included) when the
// reload was dispatched; buffer is the window's messages.Model.Buffer
// then.
type windowReload struct {
	id           wintree.LeafID
	chID         string
	since, until string
	buffer       uint64
	cached       []messages.MessageItem
}

// threadReload is the open thread's cached parent and replies.
type threadReload struct {
	chID, threadTS string
	cached         []messages.MessageItem
}

// reloadAfterRuleRemoval brings back a removed rule's messages. Each
// window is reloaded from the cache across exactly the range it has
// loaded, its oldest to its newest message (hidden ones included), so
// history the user paged back to, or opened through a message link,
// stays, and a window opened at an old message reads only that window,
// not everything since; a window holding nothing reads the newest
// cached page. The reads (one cache query and per-message
// enrichment, proportional to what the window has loaded) run in a Cmd,
// not on the Update goroutine; applySuppressionReload merges the result.
func (a *App) reloadAfterRuleRemoval() tea.Cmd {
	var wins []windowReload
	for _, id := range a.wins.Leaves() {
		m := a.winModels[id]
		chID := a.leafChannel(id)
		if m == nil || chID == "" {
			continue
		}
		wins = append(wins, windowReload{id: id, chID: chID, since: m.OldestTS(), until: m.NewestTS(), buffer: m.Buffer()})
	}
	var thr *threadReload
	if a.threadVisible {
		thr = &threadReload{chID: a.threadPanel.ChannelID(), threadTS: a.threadPanel.ThreadTS()}
	}
	if len(wins) == 0 && thr == nil {
		return nil
	}
	seq := uint64(1)
	if a.suppressReload != nil {
		seq = a.suppressReload.seq + 1
	}
	next := &suppressReloadState{seq: seq, deleted: make(map[string]map[string]bool)}
	if prev := a.suppressReload; prev != nil {
		// The superseded reload's result will be ignored, but the
		// deletes it saw may predate this read's snapshot too.
		for ch, tss := range prev.deleted {
			for ts := range tss {
				next.noteDeleted(ch, ts)
			}
		}
	}
	a.suppressReload = next
	channels, threads := a.channels, a.threads
	return func() tea.Msg {
		for i := range wins {
			w := &wins[i]
			if w.since != "" {
				w.cached = channels.ReadCacheRange(ids.ChannelID(w.chID), ids.MessageTS(w.since), ids.MessageTS(w.until))
			}
			if len(w.cached) == 0 {
				w.cached = channels.ReadCache(ids.ChannelID(w.chID))
			}
		}
		if thr != nil {
			thr.cached = threads.CacheRead(ids.ChannelID(thr.chID), ids.ThreadTS(thr.threadTS))
		}
		return suppressionReloadedMsg{seq: seq, windows: wins, thread: thr}
	}
}

// applySuppressionReload adds the cached messages a reload found to each
// pane whose list is still the one it was read for. It only adds:
// everything a pane shows stays as it is now (it may have changed since
// the read), messages deleted meanwhile are left out, and the panes'
// predicates drop whatever is still hidden. A reload superseded by a
// later one is ignored.
func (a *App) applySuppressionReload(r suppressionReloadedMsg) {
	st := a.suppressReload
	if st == nil || st.seq != r.seq {
		return
	}
	a.suppressReload = nil
	for _, w := range r.windows {
		m := a.winModels[w.id]
		if m == nil || a.leafChannel(w.id) != w.chID || len(w.cached) == 0 {
			continue
		}
		// The window's list must be the one the read was for: a
		// channel load or a jump replaces it and the read no longer
		// fits. Paging older, live arrivals and deletes change the
		// list in place and do not.
		if m.Buffer() != w.buffer {
			continue
		}
		setMessagesKeepSelection(m, addMissing(m.Messages(), st.withoutDeleted(w.chID, w.cached)))
	}
	if t := r.thread; t != nil && a.threadVisible && len(t.cached) > 1 &&
		a.threadPanel.ChannelID() == t.chID && a.threadPanel.ThreadTS() == t.threadTS {
		a.setThreadRepliesKeepSelection(addMissing(a.threadPanel.Replies(), st.withoutDeleted(t.chID, t.cached[1:])))
	}
}

// addMissing returns shown plus the messages of cached whose TS shown
// lacks, in TS order. shown wins for a TS in both: a pane's copy is kept
// current by live edits, a cache read may be older.
func addMissing(shown, cached []messages.MessageItem) []messages.MessageItem {
	have := make(map[string]bool, len(shown))
	for _, m := range shown {
		have[m.TS] = true
	}
	out := cloneMessageItems(shown)
	for _, m := range cached {
		if !have[m.TS] {
			out = append(out, m)
		}
	}
	// Slack timestamps are decimal strings of equal shape, so string
	// order is time order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out
}

// suppressPlace is how a prompt or the list names where a rule applies.
func (a *App) suppressPlace(channelID string) string {
	if channelID == "" {
		return "everywhere"
	}
	for _, it := range a.sidebar.Items() {
		if it.ID == channelID {
			if it.Type == "dm" || it.Type == "group_dm" || it.Type == "app" {
				return "in this conversation"
			}
			if it.Name != "" {
				return "in #" + it.Name
			}
		}
	}
	if name, ok := a.channelNames[channelID]; ok && name != "" {
		return "in #" + name
	}
	return "in " + channelID
}

// SuppressAuthorMsg asks the App to add a rule (the :suppress prompt's
// answer).
type SuppressAuthorMsg struct {
	TeamID string
	Rule   core.SuppressRule
	Name   string // the author's name, for the toast
}

// cmdSuppress (:suppress) offers to hide the selected message's author
// in its conversation or in the whole workspace.
func cmdSuppress(a *App, _ commandArgs) tea.Cmd {
	p, channelID, focused := a.focusedContentPane()
	if !focused {
		return toastWithClear(a, "Select a message to hide its author", 3*time.Second)
	}
	msg, ok := p.SelectedMessage()
	if !ok {
		return toastWithClear(a, "Select a message to hide its author", 3*time.Second)
	}
	switch {
	case msg.UserID == "":
		return toastWithClear(a, "This message has no author to hide", 3*time.Second)
	case a.isOwnMessage(msg):
		return toastWithClear(a, "You can't hide your own messages", 3*time.Second)
	}
	name := msg.UserName
	if name == "" {
		name = a.userNameFor(msg.UserID)
	}
	teamID, place := a.activeTeamID, a.suppressPlace(channelID)
	ask := func(chID, note string) func() tea.Msg {
		m := SuppressAuthorMsg{TeamID: teamID, Name: name,
			Rule: core.SuppressRule{AuthorID: msg.UserID, ChannelID: chID, Note: note}}
		return func() tea.Msg { return m }
	}
	a.openConfirmChoice(confirmprompt.Choice{
		Title:   "Hide messages from " + name + "?",
		Body:    "Their messages stop showing, notifying and marking channels unread. :suppressed lists who is hidden.",
		Default: 0,
		Buttons: []confirmprompt.Button{
			{Label: "Only " + place, Keys: key.NewBinding(key.WithKeys("1")), Action: ask(channelID, name+" "+place)},
			{Label: "Everywhere in " + a.activeTeamName(), Keys: key.NewBinding(key.WithKeys("2")), Action: ask("", name)},
			{Label: "Cancel"},
		},
	})
	return nil
}

// addSuppression applies a :suppress answer.
func (a *App) addSuppression(m SuppressAuthorMsg) tea.Cmd {
	if err := a.suppression.Add(m.TeamID, m.Rule); err != nil {
		return toastWithClear(a, "Could not hide "+m.Name+": "+err.Error(), 4*time.Second)
	}
	if m.TeamID == a.activeTeamID {
		a.reapplySuppression()
	}
	where := "everywhere"
	if m.Rule.ChannelID != "" {
		where = a.suppressPlace(m.Rule.ChannelID)
	}
	return toastWithClear(a, "Hiding "+m.Name+" "+where, 3*time.Second)
}

var reduceSuppress reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case SuppressAuthorMsg:
		return a.addSuppression(m), true
	case suppressionReloadedMsg:
		a.applySuppressionReload(m)
		return nil, true
	}
	return nil, false
}

// cmdSuppressed (:suppressed) lists the active workspace's rules.
func cmdSuppressed(a *App, _ commandArgs) tea.Cmd {
	a.refreshSuppressList()
	a.suppressList.Open()
	a.SetMode(ModeSuppressed)
	return nil
}

// refreshSuppressList fills the list from the active workspace's rules;
// a row's Key is the rule's index in Rules().
func (a *App) refreshSuppressList() []core.SuppressRule {
	rules := a.suppression.Rules(a.activeTeamID).Rules()
	rows := make([]suppresslist.Row, 0, len(rules))
	for i, r := range rules {
		who := a.userNameFor(r.AuthorID)
		if who == r.AuthorID && r.Note != "" && r.ChannelID == "" {
			who = r.Note + " (" + r.AuthorID + ")"
		}
		rows = append(rows, suppresslist.Row{Key: i, Who: who, Where: a.suppressPlace(r.ChannelID)})
	}
	a.suppressList.SetRows(rows)
	return rules
}

func handleSuppressedMode(a *App, msg tea.KeyMsg) tea.Cmd {
	res := a.suppressList.HandleKey(normalizeFinderKey(msg))
	if !a.suppressList.IsVisible() {
		a.SetMode(ModeNormal)
		return nil
	}
	if res == nil {
		return nil
	}
	rules := a.suppression.Rules(a.activeTeamID).Rules()
	if res.Remove < 0 || res.Remove >= len(rules) {
		return nil
	}
	if err := a.suppression.Remove(a.activeTeamID, rules[res.Remove]); err != nil {
		log.Printf("suppress: remove %+v: %v", rules[res.Remove], err)
		note := "Could not remove the rule (see the log)"
		switch {
		case errors.Is(err, core.ErrSuppressRuleNotEditable):
			note = "Remove this rule from config.toml by hand"
		case errors.Is(err, core.ErrSuppressionUnsupported):
			note = "Rules cannot be changed here"
		}
		a.suppressList.SetNote(note)
		return nil
	}
	a.refreshSuppressList()
	a.suppressList.SetNote("")
	return a.reloadAfterRuleRemoval()
}
