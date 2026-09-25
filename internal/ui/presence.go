// internal/ui/presence.go
//
// Per-workspace presence + DND state, plus the custom-snooze numeric
// input buffer.
//
// Phase 2f of the SOLID refactor of internal/ui/app.go: extracts the
// workspaceStatus type, the statusByTeam map, dndTickerOn guard, and
// the presenceCustomBuf input buffer out of App. The four mode/event
// orchestrators (handlePresenceMenuMode, handlePresenceCustomSnoozeMode,
// StatusChangeMsg arm, DNDTickMsg arm) stay on App because they touch
// statusbar / mode FSM / tea.Cmd scheduling, but they now route every
// state read and mutation through this controller.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// peerStatusTickMsg re-checks other users' status and DND deadlines so
// an expired status stops rendering without waiting for Slack to report
// the change.
type peerStatusTickMsg struct{}

const peerStatusTickInterval = time.Minute

// dmTopicLayout formats a DND end time in the DM header.
const dmTopicLayout = "15:04"

func peerStatusTick() tea.Cmd {
	return tea.Tick(peerStatusTickInterval, func(time.Time) tea.Msg {
		return peerStatusTickMsg{}
	})
}

// hasDeadline reports whether st holds a status or DND that will end.
func hasDeadline(st peerstatus.Status) bool {
	return st.HasDeadline()
}

// workspaceStatus caches the latest StatusChangeMsg per team so the
// status bar can refresh on workspace switch without round-tripping.
type workspaceStatus struct {
	Presence   string
	DNDEnabled bool
	DNDEndTS   time.Time
}

// presenceController owns presence/DND state caching + the custom
// snooze input buffer + the DND-tick guard.
type presenceController struct {
	byTeam      map[string]workspaceStatus
	dndTickerOn bool
	customBuf   string

	// peers is other users' custom status and DND, pushed to the message
	// panes and channel finder. The sidebar keeps its own copy, with
	// rules for surviving rebuilds, and is the source for DM headers.
	peers        map[string]peerstatus.Status
	peerTickerOn bool
}

func newPresenceController() *presenceController {
	return &presenceController{
		byTeam: make(map[string]workspaceStatus),
	}
}

// Status returns the cached presence/DND for teamID. Zero values when
// the team has no cached entry; the third bool reports whether an
// entry exists (callers that distinguish "no entry yet" from "entry
// happens to be all zeros" should check it).
func (p *presenceController) Status(teamID string) (presence string, dndEnabled bool, dndEnd time.Time, ok bool) {
	st, exists := p.byTeam[teamID]
	if !exists {
		return "", false, time.Time{}, false
	}
	return st.Presence, st.DNDEnabled, st.DNDEndTS, true
}

// Set records the status for teamID and returns the resulting struct
// so the caller (typically StatusChangeMsg arm) can push to the
// statusbar without a second lookup.
func (p *presenceController) Set(teamID string, presence string, dndEnabled bool, dndEnd time.Time) workspaceStatus {
	st := workspaceStatus{
		Presence:   presence,
		DNDEnabled: dndEnabled,
		DNDEndTS:   dndEnd,
	}
	p.byTeam[teamID] = st
	return st
}

// ClearDNDFor clears the DND fields on teamID's cached entry,
// preserving Presence. Used by the DNDTickMsg arm when DND expires
// locally. Returns the updated struct.
func (p *presenceController) ClearDNDFor(teamID string) workspaceStatus {
	st := p.byTeam[teamID]
	st.DNDEnabled = false
	st.DNDEndTS = time.Time{}
	p.byTeam[teamID] = st
	return st
}

// Apply mutates teamID's cached status based on the chosen presencemenu
// action. SetActive/SetAway touch only Presence; Snooze sets DND fields
// and leaves Presence alone; EndDND clears DND fields and leaves
// Presence alone. Returns the resulting struct.
func (p *presenceController) Apply(teamID string, action presencemenu.Action, snoozeMinutes int) workspaceStatus {
	st := p.byTeam[teamID]
	switch action {
	case presencemenu.ActionSetActive:
		st.Presence = "active"
	case presencemenu.ActionSetAway:
		st.Presence = "away"
	case presencemenu.ActionSnooze:
		st.DNDEnabled = true
		st.DNDEndTS = time.Now().Add(time.Duration(snoozeMinutes) * time.Minute)
	case presencemenu.ActionEndDND:
		st.DNDEnabled = false
		st.DNDEndTS = time.Time{}
	}
	p.byTeam[teamID] = st
	return st
}

// ClaimTicker returns true exactly once until ClearTicker is called,
// guarding against parallel DNDTickMsg tea.Tick chains accumulating
// when multiple StatusChangeMsgs arrive in rapid succession.
func (p *presenceController) ClaimTicker() bool {
	if p.dndTickerOn {
		return false
	}
	p.dndTickerOn = true
	return true
}

// ClearTicker resets the ticker-claim flag. Called from the DNDTickMsg
// arm when DND has expired or the active workspace no longer has DND.
func (p *presenceController) ClearTicker() {
	p.dndTickerOn = false
}

// SnoozeBuf returns the current custom-snooze input buffer.
func (p *presenceController) SnoozeBuf() string { return p.customBuf }

// AppendSnoozeDigit appends a single decimal digit to the snooze
// buffer (capped at 6 chars so absurd minute counts can't overflow).
// No-op for non-digit input.
func (p *presenceController) AppendSnoozeDigit(r string) {
	if len(r) != 1 || r[0] < '0' || r[0] > '9' {
		return
	}
	if len(p.customBuf) >= 6 {
		return
	}
	p.customBuf += r
}

// BackspaceSnooze drops the last digit from the snooze buffer.
// No-op when empty.
func (p *presenceController) BackspaceSnooze() {
	if len(p.customBuf) == 0 {
		return
	}
	p.customBuf = p.customBuf[:len(p.customBuf)-1]
}

// ClearSnoozeBuf empties the snooze buffer.
func (p *presenceController) ClearSnoozeBuf() { p.customBuf = "" }

// Handle is the presence-family reducer for App.Update (Phase 4a).
// Owns PresenceChangeMsg (per-user sidebar dot), StatusChangeMsg
// (per-team cache + statusbar refresh + DND tick scheduling) and
// statusbar.DNDTickMsg (countdown refresh + DND-expiry detection).
// Returns (nil, false) for any other message type.
//
// All three arms previously lived in the monolithic Update switch
// (app.go lines ~1879-1920 pre-Phase-4). State and behavior are now
// co-located on this controller — App still owns the sidebar /
// statusbar / activeTeamID references that the reducer needs, so
// those are passed via `a`.
func (p *presenceController) Handle(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case PresenceChangeMsg:
		// Per-user presence dot in the DM list; not workspace-scoped.
		a.sidebar.UpdatePresenceByUser(m.UserID, m.Presence)
		return nil, true

	case UserStatusChangeMsg:
		if m.TeamID != "" && m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.sidebar.UpdateStatusByUser(m.UserID, m.Emoji, m.Text, m.Expires)
		a.sidebar.UpdateHuddleByUser(m.UserID, m.Huddle, m.HuddleExpires)
		st := p.peers[m.UserID].WithStatus(m.Emoji, m.Text, m.Expires).WithHuddle(m.Huddle, m.HuddleExpires)
		return p.applyPeer(a, m.UserID, st), true

	case UserDNDChangeMsg:
		if m.TeamID != "" && m.TeamID != a.activeTeamID {
			return nil, true
		}
		a.sidebar.UpdateDNDByUser(m.UserID, m.Enabled, m.EndTS)
		return p.applyPeer(a, m.UserID, p.peers[m.UserID].WithDND(m.Enabled, m.EndTS)), true

	case peerStatusTickMsg:
		return p.expirePeers(a, time.Now()), true

	case StatusChangeMsg:
		st := p.Set(m.TeamID, m.Presence, m.DNDEnabled, m.DNDEndTS)
		if m.TeamID != a.activeTeamID {
			// Non-active workspace: cache only. The status bar
			// will pick up `st` from the cache on workspace switch.
			return nil, true
		}
		a.statusbar.SetStatus(st.Presence, st.DNDEnabled, st.DNDEndTS)
		// Start the once-a-minute countdown tick if DND is active
		// and not already expired. ClaimTicker returns true exactly
		// once until ClearTicker is called, guarding against parallel
		// tick chains accumulating when multiple StatusChangeMsgs
		// arrive in rapid succession.
		if !(st.DNDEnabled && !st.DNDEndTS.IsZero() && time.Now().Before(st.DNDEndTS) && p.ClaimTicker()) {
			return nil, true
		}
		return tea.Tick(time.Minute, func(time.Time) tea.Msg {
			return statusbar.DNDTickMsg{}
		}), true

	case statusbar.DNDTickMsg:
		pres, dndEnabled, dndEnd, ok := p.Status(a.activeTeamID)
		if !ok {
			// Active workspace has no cached entry (e.g. switched
			// away before a tick fired). Stop the chain.
			p.ClearTicker()
			return nil, true
		}
		if dndEnabled && !dndEnd.IsZero() && !time.Now().Before(dndEnd) {
			// DND expired locally — flip the cached flag so the
			// statusbar segment falls back to presence, and stop
			// the chain.
			st := p.ClearDNDFor(a.activeTeamID)
			a.statusbar.SetStatus(st.Presence, false, time.Time{})
			p.ClearTicker()
			return nil, true
		}
		a.statusbar.SetStatus(pres, dndEnabled, dndEnd)
		if dndEnabled && !dndEnd.IsZero() {
			// Still in DND — reschedule (dndTickerOn stays true).
			return tea.Tick(time.Minute, func(time.Time) tea.Msg {
				return statusbar.DNDTickMsg{}
			}), true
		}
		// Active workspace no longer in DND (e.g. user switched to
		// a non-DND'd workspace between ticks). Stop the chain.
		p.ClearTicker()
		return nil, true
	}
	return nil, false
}

// applyPeer records st for userID and pushes it to the message panes,
// the thread panel, the channel finder and, when that DM is open, the
// header. Returns the expiry tick when st will end and none is running.
func (p *presenceController) applyPeer(a *App, userID string, st peerstatus.Status) tea.Cmd {
	if userID == "" {
		return nil
	}
	if p.peers == nil {
		p.peers = make(map[string]peerstatus.Status)
	}
	p.peers[userID] = st
	for _, mp := range a.allWinModels() {
		mp.PatchUserStatus(userID, st)
	}
	a.threadPanel.PatchUserStatus(userID, st)
	p.pushDMStatus(a, userID, time.Now())
	return p.claimPeerTick(st)
}

// SetPeers replaces every known peer status, for a workspace becoming
// active with statuses seeded from the cache. Returns the expiry tick
// when any of them will end.
func (p *presenceController) SetPeers(a *App, statuses map[string]peerstatus.Status) tea.Cmd {
	p.peers = make(map[string]peerstatus.Status, len(statuses))
	var cmd tea.Cmd
	for id, st := range statuses {
		p.peers[id] = st
		if c := p.claimPeerTick(st); c != nil {
			cmd = c
		}
	}
	for _, mp := range a.allWinModels() {
		mp.SetUserStatuses(statuses)
	}
	a.threadPanel.SetUserStatuses(statuses)
	// The finder takes DM statuses from the sidebar, which both callers
	// populate (SetChannels) before calling this.
	for _, item := range a.sidebar.Items() {
		if item.DMUserID != "" {
			a.channelFinder.SetStatus(item.ID, item.Status)
		}
	}
	return cmd
}

func (p *presenceController) claimPeerTick(st peerstatus.Status) tea.Cmd {
	if !hasDeadline(st) || p.peerTickerOn {
		return nil
	}
	p.peerTickerOn = true
	return peerStatusTick()
}

// pushDMStatus copies the sidebar's status for userID's DM onto the
// channel finder row and every window (focused or not) currently
// showing that DM's header.
func (p *presenceController) pushDMStatus(a *App, userID string, now time.Time) {
	for _, item := range a.sidebar.Items() {
		if item.DMUserID != userID {
			continue
		}
		a.channelFinder.SetStatus(item.ID, item.Status)
		topic := item.Status.Summary(now, dmTopicLayout)
		for _, mp := range a.modelsForChannel(item.ID) {
			mp.SetChannelTopic(topic)
		}
	}
}

// dmTopicFor is the header topic for channelID: its DM peer's status
// summary, or "" for a channel that is not a DM.
func (p *presenceController) dmTopicFor(a *App, channelID string) string {
	for _, item := range a.sidebar.Items() {
		if item.ID == channelID && item.DMUserID != "" {
			return item.Status.Summary(time.Now(), dmTopicLayout)
		}
	}
	return ""
}

// expirePeers is the peerStatusTickMsg arm: it clears every status and
// DND whose deadline has passed from each surface, and keeps ticking
// while any remaining one still has a deadline.
func (p *presenceController) expirePeers(a *App, now time.Time) tea.Cmd {
	a.sidebar.ExpireStatuses(now)
	for _, mp := range a.allWinModels() {
		mp.ExpireStatuses(now)
	}
	a.threadPanel.ExpireStatuses(now)
	pending := false
	for uid, st := range p.peers {
		if st.Expired(now) {
			st = st.Clear(now)
			p.peers[uid] = st
			p.pushDMStatus(a, uid, now)
		}
		if hasDeadline(st) {
			pending = true
		}
	}
	for _, item := range a.sidebar.Items() {
		if hasDeadline(item.Status) {
			pending = true
		}
	}
	if !pending {
		p.peerTickerOn = false
		return nil
	}
	return peerStatusTick()
}
