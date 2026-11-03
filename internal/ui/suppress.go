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

// reapplySuppression brings every open pane in line with the rules
// after a change. fromCache reloads each pane's conversation from the
// cache first, so a removed rule's messages come back; otherwise the
// panes are filtered as they stand. Selection stays on the same message
// when it is still shown.
func (a *App) reapplySuppression(fromCache bool) {
	for _, id := range a.wins.Leaves() {
		m := a.winModels[id]
		chID := a.leafChannel(id)
		if m == nil || chID == "" {
			continue
		}
		shown := m.Messages()
		list := cloneMessageItems(shown)
		if fromCache {
			if cached := a.channels.ReadCache(ids.ChannelID(chID)); len(cached) > 0 {
				list = keepEphemerals(cached, shown)
			}
		}
		sel, hadSel := m.SelectedMessage()
		m.SetMessages(list)
		if hadSel {
			m.SelectByTS(sel.TS)
		}
	}
	if !a.threadVisible {
		return
	}
	tp := a.threadPanel
	replies := cloneMessageItems(tp.Replies())
	if fromCache {
		if cached := a.threads.CacheRead(ids.ChannelID(tp.ChannelID()), ids.ThreadTS(tp.ThreadTS())); len(cached) > 1 {
			replies = keepEphemerals(cached[1:], tp.Replies())
		}
	}
	sel, hadSel := tp.SelectedMessage()
	tp.SetThread(tp.ParentMsg(), replies, tp.ChannelID(), tp.ThreadTS())
	if hadSel {
		for i, r := range tp.Replies() {
			if r.TS == sel.TS {
				tp.SelectByIndex(i)
				break
			}
		}
	}
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
		a.reapplySuppression(false)
	}
	where := "everywhere"
	if m.Rule.ChannelID != "" {
		where = a.suppressPlace(m.Rule.ChannelID)
	}
	return toastWithClear(a, "Hiding "+m.Name+" "+where, 3*time.Second)
}

var reduceSuppress reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	if m, ok := msg.(SuppressAuthorMsg); ok {
		return a.addSuppression(m), true
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
	a.reapplySuppression(true)
	return nil
}
