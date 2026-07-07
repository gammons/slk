// internal/ui/mode_normal.go
//
// Normal-mode key handler (Phase 5k).
//
// The bulk of slk's keybinding surface lives here:
//   - mode entry: i (insert), Ctrl-T (channel finder), : (command
//     prompt), Ctrl-Y (theme switcher), ? (help),
//     S (presence menu), R (reaction picker)
//   - navigation: j/k (selection), Ctrl-D/U (half-page), C-f/b
//     (page), G (bottom), Tab/h/l (focus next/prev), Ctrl-o/i
//     (nav back/forward through visited channels)
//   - layout toggles: s (sidebar), t (thread)
//   - message ops: y (copy permalink), E (edit), D (delete),
//     M (mark unread), O (open image preview)
//   - reaction nav sub-state: r enters; arrows + Enter select
//     (delegated to handleReactionNav / handleThreadReactionNav)
//   - window commands: Ctrl-W prefix arms a pending sub-state; the
//     next key is a window chord (s/v split, h/j/k/l focus, w cycle,
//     q/c close, o only — delegated to handleWindowChord)
//   - workspace switch: 1-9 number keys (handled in default arm)
//   - quit confirm: q (close thread if visible, else no-op),
//     Q (quit confirm)
//
// Reaction-nav sub-state is intercepted FIRST: while in it, only
// a narrow set of keys (arrows / Enter / r / Esc) is handled,
// everything else falls back to normal key handling.
package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/help"
	"github.com/gammons/slk/internal/ui/themeswitcher"
)

func handleNormalMode(a *App, msg tea.KeyMsg) tea.Cmd {
	// ctrl+w pending sub-state: the next key is a window command
	// (intercepted FIRST, like the reaction-nav sub-states below).
	if a.pendingWinCmd {
		a.pendingWinCmd = false
		a.statusbar.SetHelpHint(a.defaultHelpHint())
		return a.handleWindowChord(msg)
	}

	// Reaction-nav sub-state (intercept before normal keys).
	if a.focusedPanel == PanelMessages && a.messagepane.ReactionNavActive() {
		return a.handleReactionNav(msg)
	}
	if a.focusedPanel == PanelThread && a.threadPanel.ReactionNavActive() {
		return a.handleThreadReactionNav(msg)
	}

	switch {
	case key.Matches(msg, a.keys.InsertMode):
		a.SetMode(ModeInsert)
		// In the Threads view there is no main compose box -- the
		// only way to type is into the right-side thread panel's
		// compose. Force focus there even when the threads list
		// itself was the focused panel.
		if a.focusedPanel == PanelThread || (a.view == ViewThreads && a.threadVisible) {
			a.focusedPanel = PanelThread
			return a.threadCompose.Focus()
		}
		a.focusedPanel = PanelMessages
		return a.compose.Focus()

	case key.Matches(msg, a.keys.CommandMode):
		a.enterCommandMode()

	case key.Matches(msg, a.keys.Escape):
		// An active `/` search absorbs the first Esc: clear it and
		// stop, leaving thread panels / edits untouched. v1
		// limitation: Esc during the in-flight window doesn't cancel
		// a local channel search; acceptable because local FTS is
		// ms-fast — workspace search cancels via modal close instead.
		// The statusbar check covers the no-active-state case where
		// only the "/foo  no matches" segment lingers.
		if a.search != nil || a.statusbar.Search() != "" {
			a.clearActiveSearch()
			return nil
		}
		a.cancelEdit()
		a.SetMode(ModeNormal)
		a.compose.Blur()
		if a.threadVisible {
			a.CloseThread()
		}

	case key.Matches(msg, a.keys.WindowPrefix):
		a.pendingWinCmd = true
		a.statusbar.SetHelpHint("ctrl+w …")
		return nil

	case key.Matches(msg, a.keys.SearchMode):
		// Spec scopes `/` to the channel message pane in v1: no-op
		// while the thread panel has focus.
		if a.focusedPanel == PanelThread {
			return nil
		}
		a.searchInput = ""
		a.statusbar.SetSearch("/")
		a.SetMode(ModeSearch)
		return nil

	// n/N match navigation: like `/`, scoped to the channel message
	// pane in v1 — a no-op while the thread panel has focus.
	case key.Matches(msg, a.keys.SearchNext) && a.search != nil && a.focusedPanel != PanelThread:
		return a.searchStep(1)

	case key.Matches(msg, a.keys.SearchPrev) && a.search != nil && a.focusedPanel != PanelThread:
		return a.searchStep(-1)

	case key.Matches(msg, a.keys.WorkspaceSearch):
		a.searchResults.Open()
		a.SetMode(ModeWorkspaceSearch)
		return nil

	case key.Matches(msg, a.keys.Tab):
		a.FocusNext()

	case key.Matches(msg, a.keys.ShiftTab):
		a.FocusPrev()

	case key.Matches(msg, a.keys.ToggleSidebar):
		a.ToggleSidebar()

	case key.Matches(msg, a.keys.ToggleWorkspaces):
		a.ToggleWorkspaces()

	case key.Matches(msg, a.keys.SidebarGrow):
		a.sidebar.GrowWidth()
		if a.widthSaveFn != nil {
			a.widthSaveFn(a.sidebar.Width())
		}

	case key.Matches(msg, a.keys.SidebarShrink):
		a.sidebar.ShrinkWidth()
		if a.widthSaveFn != nil {
			a.widthSaveFn(a.sidebar.Width())
		}

	case key.Matches(msg, a.keys.ToggleThread):
		a.ToggleThread()

	case key.Matches(msg, a.keys.NavBack):
		if cmd := a.navigateBack(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.NavForward):
		if cmd := a.navigateForward(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.Down):
		if cmd := a.handleDown(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.Up):
		if cmd := a.handleUp(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.Left):
		a.FocusPrev()

	case key.Matches(msg, a.keys.Right):
		a.FocusNext()

	case key.Matches(msg, a.keys.Enter):
		return a.handleEnter()

	case key.Matches(msg, a.keys.ToggleSection):
		// Space on a sidebar section header toggles its collapsed
		// state; elsewhere it falls through to whatever the focused
		// panel does with a literal space (typically nothing in
		// normal mode).
		if a.focusedPanel == PanelSidebar {
			if a.sidebar.ToggleCollapseSelected() {
				return nil
			}
		}

	case key.Matches(msg, a.keys.Bottom):
		if cmd := a.handleGoToBottom(); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.PageUp):
		if cmd := a.scrollFocusedPanel(-a.pageSize()); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.PageDown):
		if cmd := a.scrollFocusedPanel(a.pageSize()); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.HalfPageUp):
		if cmd := a.scrollFocusedPanel(-a.halfPageSize()); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.HalfPageDown):
		if cmd := a.scrollFocusedPanel(a.halfPageSize()); cmd != nil {
			return cmd
		}

	case key.Matches(msg, a.keys.Help):
		a.help.SetEntries(help.FromKeyMap(a.keys))
		a.help.Open()
		a.SetMode(ModeHelp)

	case key.Matches(msg, a.keys.ThemeSwitcher):
		// Per-workspace scope. Header text shows the current
		// workspace name.
		header := "Theme for " + a.activeTeamName()
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeWorkspace, header)
		a.SetMode(ModeThemeSwitcher)
		return nil
	case key.Matches(msg, a.keys.ThemeSwitcherGlobal):
		a.themeSwitcher.OpenWithScope(themeswitcher.ScopeGlobal, "Default theme for new workspaces")
		a.SetMode(ModeThemeSwitcher)
		return nil

	case key.Matches(msg, a.keys.PresenceMenu):
		header := a.workspaceNameForActive()
		pres, dndEnabled, dndEnd, _ := a.presence.Status(a.activeTeamID)
		a.presenceMenu.OpenWith(header, pres, dndEnabled, dndEnd)
		a.SetMode(ModePresenceMenu)

	case key.Matches(msg, a.keys.FuzzyFinder) || key.Matches(msg, a.keys.FuzzyFinderAlt):
		a.channelFinder.Open()
		a.SetMode(ModeChannelFinder)

	case key.Matches(msg, a.keys.NewMessage):
		return func() tea.Msg { return EnterNewMessageMsg{} }

	case key.Matches(msg, a.keys.Reaction):
		if a.focusedPanel == PanelMessages {
			return a.openPickerFromMessage()
		} else if a.focusedPanel == PanelThread {
			return a.openPickerFromThread()
		}

	case key.Matches(msg, a.keys.ReactionNav):
		if a.focusedPanel == PanelMessages {
			a.messagepane.EnterReactionNav()
		} else if a.focusedPanel == PanelThread {
			a.threadPanel.EnterReactionNav()
		}

	case key.Matches(msg, a.keys.ListReactions):
		return a.openReactionsView()

	case key.Matches(msg, a.keys.SaveThread):
		return a.saveThreadToFile()

	case key.Matches(msg, a.keys.CopyPermalink):
		return a.copyPermalinkOfSelected()

	case key.Matches(msg, a.keys.Edit):
		return a.beginEditOfSelected()

	case key.Matches(msg, a.keys.Delete):
		return a.beginDeleteOfSelected()

	case key.Matches(msg, a.keys.OpenPreview):
		return a.openImagePreviewOfSelected()

	case key.Matches(msg, a.keys.OpenLink):
		return a.openLinksOfSelected()

	case key.Matches(msg, a.keys.MarkUnread):
		return a.markUnreadOfSelected()

	case key.Matches(msg, a.keys.CloseThreadView):
		// Lowercase q is "close thread view" when one is open; if
		// no thread panel is visible it's a no-op (Q and Ctrl+C
		// are the quit keys). The vim-style pairing: q closes the
		// transient pane, Q closes the whole app.
		if a.threadVisible {
			a.CloseThread()
		}
		return nil

	case key.Matches(msg, a.keys.QuitConfirm):
		a.openQuitConfirm()
		return nil

	default:
		// Number keys 1-9 switch workspaces.
		keyStr := msg.String()
		if len(keyStr) == 1 && keyStr[0] >= '1' && keyStr[0] <= '9' {
			idx := int(keyStr[0] - '1') // 0-indexed
			if idx < len(a.workspaceItems) && a.workspaceSwitcher != nil {
				if a.workspaceItems[idx].ID != a.workspaceRail.SelectedID() {
					switcher := a.workspaceSwitcher
					teamID := a.workspaceItems[idx].ID
					return func() tea.Msg {
						return switcher(teamID)
					}
				}
			}
		}
	}
	return nil
}
