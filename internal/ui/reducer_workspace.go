// internal/ui/reducer_workspace.go
//
// Workspace-lifecycle reducer for App.Update (Phase 4k).
//
// Owns the nine Update arms that drive workspace activation,
// per-workspace data resolution echoes, and the sidebar / read-state
// refresh paths:
//
//	WorkspaceReadyMsg       - one of the configured workspaces
//	                          finished its initial connect. If
//	                          InitialActive (claimed exactly once),
//	                          activate it: apply theme, push
//	                          channels/users/emoji, open the first
//	                          channel. Always fires an initial
//	                          threads-list fetch.
//	WorkspaceSwitchedMsg    - user picked a different workspace.
//	                          Tear down per-workspace transient
//	                          state (threads view, edit, selection,
//	                          compose draft), push new channels/users/
//	                          emoji, apply theme, restore the last
//	                          channel viewed for this workspace.
//	ConversationOpenedMsg   - WS event: a DM/MPIM just opened.
//	                          Upsert into the active sidebar.
//	SectionsRefreshedMsg    - cache notice: sidebar sections were
//	                          reorganized. Re-push channel items
//	                          for the active workspace.
//	DMNameResolvedMsg       - DM display-name resolved (im.open
//	                          roundtrip): patch the sidebar entry
//	                          and re-render.
//	UserResolvedMsg         - per-user display-name resolved:
//	                          patch any in-history references in
//	                          both messages pane and thread panel.
//	UserExternalMsg         - per-user external/internal flag
//	                          resolved: update the cache + re-push
//	                          SetUserNames so styling refreshes.
//	ReadStateChangedMsg     - persistent read-state changed in the
//	                          cache: refresh sidebar + workspace rail.
//	CustomEmojisLoadedMsg   - per-workspace emoji list arrived.
//	                          Apply only to the active workspace.
//
// Free reducer (not controller-absorbed): these arms cooperate on
// the sidebar, messagepane, threadPanel, statusbar, channelFinder,
// workspaceRail, compose, threadCompose, threadsView, presence,
// bootstrap, themes, channel-state, and the threads service. No
// single existing controller owns this cross-section.
//
// WorkspaceReadyMsg and WorkspaceSwitchedMsg are extracted into
// reduceWorkspaceReady / reduceWorkspaceSwitched helpers to keep
// the dispatch switch readable. The two arms share substantial
// activation logic (theme apply, channels push, threads fetch);
// deduplication is deliberately NOT attempted in this commit
// (Phase 4 is behavior-preserving refactoring, not algorithmic
// changes). A follow-up could extract a shared
// activateWorkspace helper.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/styles"
)

var reduceWorkspace reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case WorkspaceReadyMsg:
		return reduceWorkspaceReady(a, m), true

	case WorkspaceSwitchedMsg:
		return reduceWorkspaceSwitched(a, m), true

	case ConversationOpenedMsg:
		if m.TeamID == a.activeTeamID {
			a.sidebar.UpsertItem(m.Item)
		}
		// Inactive-workspace events update WorkspaceContext.Channels
		// from the rtmEventHandler in cmd/slk/main.go (Task 6);
		// App.Update only mutates the active sidebar.
		return nil, true

	case SectionsRefreshedMsg:
		if m.TeamID == a.activeTeamID {
			a.SetChannels(m.Channels)
		}
		// Inactive-workspace events have already updated the
		// WorkspaceContext.Channels in cmd/slk; App.Update only
		// mutates the active sidebar.
		return nil, true

	case DMNameResolvedMsg:
		items := a.sidebar.Items()
		for i := range items {
			if items[i].ID != m.ChannelID {
				continue
			}
			items[i].Name = m.DisplayName
			if m.IsBot && items[i].Type == "dm" {
				items[i].Type = "app"
			}
			break
		}
		a.SetChannels(items)
		return nil, true

	case UserResolvedMsg:
		if m.TeamID != a.activeTeamID {
			return nil, true
		}
		for _, mp := range a.allWinModels() {
			mp.PatchUserName(m.UserID, m.DisplayName)
		}
		a.threadPanel.PatchUserName(m.UserID, m.DisplayName)
		// IsBot affects DM channel-type classification, but that's
		// orchestrated by DMNameResolvedMsg; this handler is only
		// the in-history name patch. IsBot is carried for forward
		// compatibility but not consumed here.
		return nil, true

	case UserExternalMsg:
		if a.externalUsers == nil {
			a.externalUsers = map[string]bool{}
		}
		if m.IsExternal {
			a.externalUsers[m.UserID] = true
		} else {
			delete(a.externalUsers, m.UserID)
		}
		if len(a.userNames) > 0 {
			a.SetUserNames(a.userNames)
		}
		return nil, true

	case ReadStateChangedMsg:
		_ = m
		// Persistent read state changed in the cache. Invalidate
		// the sidebar and refresh the workspace rail so both
		// re-read from the DB.
		a.notifyReadStateChanged()
		return nil, true

	case CustomEmojisLoadedMsg:
		if m.TeamID == a.activeTeamID {
			a.SetCustomEmoji(m.CustomEmoji)
		}
		return nil, true

	case UserGroupsLoadedMsg:
		if m.TeamID == a.activeTeamID {
			a.SetUserGroups(m.UserGroups)
		}
		return nil, true
	}
	return nil, false
}

// reduceWorkspaceReady handles WorkspaceReadyMsg. Extracted from
// the reduceWorkspace dispatch switch because the arm is ~60 lines
// (InitialActive activation branch alone is ~50).
func reduceWorkspaceReady(a *App, m WorkspaceReadyMsg) tea.Cmd {
	a.bootstrap.MarkReady(m.TeamName)
	if m.Domain != "" {
		a.workspaceDomains[m.TeamID] = m.Domain
	}
	var batch []tea.Cmd
	// Only the workspace flagged InitialActive auto-claims active
	// state. main.go computes this deterministically
	// (default_workspace match, else first to connect) so two
	// simultaneous WorkspaceReadyMsgs can no longer race on
	// (activeChannelID == "") and both claim. ClaimInitialActive
	// is a defensive one-shot guard against any future bug that
	// delivers InitialActive=true twice.
	if m.InitialActive && a.bootstrap.ClaimInitialActive() {
		a.view = ViewChannels
		a.sidebar.SetThreadsActive(false)
		a.threadsView.SetSummaries(nil)
		a.sidebar.SetThreadsUnreadCount(0)
		a.sidebar.SetActivityActive(false)
		a.activityView.SetItems(nil)
		a.activityView.SetBodies(nil)
		a.sidebar.SetActivityUnreadCount(0)
		a.activityNextCursor = ""
		a.lastOpenedChannelID = ""
		a.lastOpenedThreadTS = ""
		// Apply the resolved theme for the initial active
		// workspace. Without this, per-workspace themes silently
		// revert to the global default on startup until the user
		// manually switches workspaces.
		if m.Theme != "" {
			styles.Apply(m.Theme, a.themeOverrides)
			a.invalidateAllWinModelCaches()
			a.threadPanel.InvalidateCache()
			a.sidebar.InvalidateCache()
			a.compose.RefreshStyles()
			a.threadCompose.RefreshStyles()
		}
		if m.SidebarWidth != 0 {
			a.sidebar.SetWidth(m.SidebarWidth)
		}
		a.sidebar.SetSectionsProvider(m.SectionsProvider)
		a.SetChannels(m.Channels)
		a.channelFinder.SetItems(m.FinderItems)
		// SetExternalUsers re-pushes user-names; calling
		// SetUserNames last is the canonical state.
		a.SetExternalUsers(m.ExternalUsers)
		a.SetUserNames(m.UserNames)
		a.SetCustomEmoji(m.CustomEmoji)
		a.SetUserGroups(m.UserGroups)
		// Route through the setter so messagepane/threadPanel also learn
		// the current user — production never calls SetCurrentUserID
		// otherwise, which would leave live self-reactions unstyled.
		a.SetCurrentUserID(m.UserID)
		a.activeTeamID = m.TeamID
		pres, dndEnabled, dndEnd, _ := a.presence.Status(a.activeTeamID)
		a.statusbar.SetStatus(pres, dndEnabled, dndEnd)
		a.workspaceRail.SelectByID(m.TeamID)
		if len(m.Channels) > 0 {
			// Restore the last-visited channel across restarts (persisted in
			// channel_visits). Falls back to the first sidebar entry when
			// there's no recorded visit or the channel no longer exists.
			target := m.Channels[0]
			if m.LastChannelID != "" {
				for _, ch := range m.Channels {
					if ch.ID == m.LastChannelID {
						target = ch
						break
					}
				}
			}
			a.sidebar.SelectByID(target.ID)
			a.messagepane.SetLoading(true)
			a.messagepane.SetMessages(nil)
			batch = append(batch, spinnerTickCmd())
			batch = append(batch, func() tea.Msg {
				return ChannelSelectedMsg{ID: target.ID, Name: target.Name, Type: target.Type}
			})
		}
		// Prime the sidebar Activity badge on connect (the feed is a
		// network call scoped to the active workspace, so — unlike the
		// per-workspace threads fetch below — fire it only for the one
		// workspace that just became active).
		activity := a.activity
		team := ids.TeamID(m.TeamID)
		unreadOnly := a.activityView.UnreadOnly()
		batch = append(batch, func() tea.Msg {
			return activity.Fetch(team, activityFeedPageLimit, "", unreadOnly)
		})
	}
	// Initial threads-list fetch fires for every workspace as it
	// becomes ready; the result is gated by ThreadsListLoadedMsg's
	// TeamID == activeTeamID check, so background fetches are
	// dropped without affecting the active sidebar.
	threads := a.threads
	team := ids.TeamID(m.TeamID)
	batch = append(batch, func() tea.Msg { return threads.ListFetch(team) })
	return tea.Batch(batch...)
}

// reduceWorkspaceSwitched handles WorkspaceSwitchedMsg. Extracted
// from the reduceWorkspace dispatch switch because the arm is ~87
// lines (tears down per-workspace transient state, applies new
// data, restores last-viewed channel).
func reduceWorkspaceSwitched(a *App, m WorkspaceSwitchedMsg) tea.Cmd {
	if a.compose.Uploading() || a.threadCompose.Uploading() {
		return a.uploadToastCmd("Upload in progress", 2*time.Second)
	}
	if m.Domain != "" {
		a.workspaceDomains[m.TeamID] = m.Domain
	}
	// Remember which channel we were on in the workspace we're
	// leaving so that switching back lands the user on the same
	// channel rather than always snapping to the sidebar's first
	// entry.
	if a.activeTeamID != "" && a.activeChannelID != "" && a.activeTeamID != m.TeamID {
		a.lastChannelByTeam[a.activeTeamID] = a.activeChannelID
	}
	a.cancelEdit()
	// Always land in ViewChannels and drop any per-workspace
	// threads-view state so stale summaries / unread badges from
	// the previous workspace can't leak in. The sidebar cursor is
	// moved to the restored channel below (after SetChannels);
	// only fall back to the Threads row when the new workspace
	// has no channels at all.
	a.view = ViewChannels
	a.sidebar.SetThreadsActive(false)
	a.sidebar.SetActivityActive(false)
	a.threadsView.SetSummaries(nil)
	a.sidebar.SetThreadsUnreadCount(0)
	a.activityView.SetItems(nil)
	a.activityView.SetBodies(nil)
	a.sidebar.SetActivityUnreadCount(0)
	a.activityNextCursor = ""
	a.lastOpenedChannelID = ""
	a.lastOpenedThreadTS = ""
	a.CloseThread()
	a.clearSelections()
	// The window tree + per-window models are per-workspace state:
	// collapse to a single empty window so no leaf carries a
	// cross-workspace channel. The queued ChannelSelectedMsg for
	// this workspace re-populates it.
	a.resetWindowTree()
	a.compose.Reset()
	a.statusbar.SetSyncing(false) // defensive: don't carry stale sync state across workspaces
	// resetWindowTree replaced the pane with a fresh empty model;
	// the queued ChannelSelectedMsg below paints it via the
	// three-tier dispatch. For empty workspaces (no Channels) the
	// pane's empty state is confirmed in the else branch below.
	a.SetMode(ModeNormal)
	a.compose.Blur()
	a.sidebar.SetSectionsProvider(m.SectionsProvider)
	// Forget the previous workspace's live DM presence before loading
	// the new one, so its cache-seeded item presence isn't overwritten
	// by a stale peer from the workspace we're leaving. The new
	// workspace's own presence_change events repopulate it.
	a.sidebar.ResetPresence()
	a.SetChannels(m.Channels)
	a.channelFinder.SetItems(m.FinderItems)
	// SetExternalUsers re-pushes user-names; calling SetUserNames
	// last is the canonical state.
	a.SetExternalUsers(m.ExternalUsers)
	a.SetUserNames(m.UserNames)
	a.SetCustomEmoji(m.CustomEmoji)
	a.SetUserGroups(m.UserGroups)
	// Route through the setter so messagepane/threadPanel also learn the
	// current user (see WorkspaceReadyMsg above).
	a.SetCurrentUserID(m.UserID)
	a.activeTeamID = m.TeamID
	pres, dndEnabled, dndEnd, _ := a.presence.Status(a.activeTeamID)
	a.statusbar.SetStatus(pres, dndEnabled, dndEnd)
	// Apply per-workspace theme. Must run on Update goroutine so
	// the component cache invalidations and compose-style refreshes
	// below take effect on the next render.
	if m.Theme != "" {
		styles.Apply(m.Theme, a.themeOverrides)
		a.invalidateAllWinModelCaches()
		a.threadPanel.InvalidateCache()
		a.sidebar.InvalidateCache()
		a.compose.RefreshStyles()
		a.threadCompose.RefreshStyles()
	}
	if m.SidebarWidth != 0 {
		a.sidebar.SetWidth(m.SidebarWidth)
	}
	a.workspaceRail.SelectByID(m.TeamID)

	var batch []tea.Cmd
	// Restore the last-viewed channel for this workspace if we
	// have one and it still exists; otherwise fall back to the
	// first channel in the sidebar. Move the sidebar cursor to
	// that channel as well so the highlight matches the messages
	// pane.
	if len(m.Channels) > 0 {
		target := m.Channels[0]
		if savedID, ok := a.lastChannelByTeam[m.TeamID]; ok && savedID != "" {
			for _, ch := range m.Channels {
				if ch.ID == savedID {
					target = ch
					break
				}
			}
		}
		a.sidebar.SelectByID(target.ID)
		batch = append(batch, func() tea.Msg {
			return ChannelSelectedMsg{ID: target.ID, Name: target.Name, Type: target.Type}
		})
	} else {
		a.sidebar.SelectThreadsRow()
		a.messagepane.SetLoading(false)
		a.messagepane.SetMessages(nil)
	}
	// Kick off an initial threads-list fetch so the sidebar
	// Threads row badge populates before the user opens the view.
	threads := a.threads
	team := ids.TeamID(m.TeamID)
	batch = append(batch, func() tea.Msg { return threads.ListFetch(team) })
	// Refresh the Activity badge for the newly-active workspace too.
	activity := a.activity
	unreadOnly := a.activityView.UnreadOnly()
	batch = append(batch, func() tea.Msg {
		return activity.Fetch(team, activityFeedPageLimit, "", unreadOnly)
	})
	return tea.Batch(batch...)
}
