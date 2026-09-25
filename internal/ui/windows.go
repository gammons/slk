// internal/ui/windows.go
//
// App-side window management (window-management design §1, §4).
// Thin bridge between the wintree package and App state: tree ops,
// focus movement, status-bar toasts, and the focused-window channel
// contract. Phase 3: every window owns a live messages model (see
// internal/ui/winmodels.go); focus changes are a pointer swap plus
// an active-channel context retarget — no channel re-dispatch.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/ui/wintree"
)

// handleWindowChord consumes the key following ctrl+w (vim window
// commands, design §4). Unmapped keys — including Esc — cancel
// silently, matching vim.
func (a *App) handleWindowChord(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "s":
		return a.splitWindow(wintree.SplitStacked)
	case "v":
		return a.splitWindow(wintree.SplitSideBySide)
	case "h", "left":
		return a.navigateWindow(wintree.NavLeft)
	case "j", "down":
		return a.navigateWindow(wintree.NavDown)
	case "k", "up":
		return a.navigateWindow(wintree.NavUp)
	case "l", "right":
		return a.navigateWindow(wintree.NavRight)
	case "w", "ctrl+w":
		return a.cycleWindow()
	case "q", "c":
		return a.closeWindow()
	case "o":
		a.onlyWindow()
		return nil
	}
	return nil
}

// windowBounds returns the messages-region rectangle the window tree
// subdivides. Windows are only drawn when the channel is in front, so
// the rectangle is always the channel-in-front layout, even while a
// stacked thread is in front. A scratch layout keeps the bands View
// stored for mouse hit-testing intact.
func (a *App) windowBounds() wintree.Rect {
	var scratch panelLayout
	frame := scratch.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(),
		a.sidebarVisible, a.threadVisible, false)
	return wintree.Rect{X: 0, Y: 0, W: frame.MsgWidth + frame.MsgBorder, H: frame.ContentHeight}
}

// splitWindow creates a new window (cloning the focused window's
// channel) and focuses it. The new window gets its own model, seeded
// from the source so the clone shows the same history immediately.
// Toasts "Not enough room" on refusal.
func (a *App) splitWindow(dir wintree.Dir) tea.Cmd {
	src := a.messagepane
	srcCh, _ := a.wins.Channel(a.focusedWin)
	id, err := a.wins.Split(a.focusedWin, dir, a.windowBounds())
	if err != nil {
		return toastWithClear(a, "Not enough room", 2*time.Second)
	}
	m := a.newWindowModel(srcCh.Name)
	m.SetChannel(srcCh.Name, a.presence.dmTopicFor(a, srcCh.ID))
	m.SetChannelType(srcCh.Type)
	if src != nil {
		// Messages() exposes the source's internal slice; the seed
		// must be a deep copy (see cloneMessageItems in winmodels.go
		// for the aliasing hazards it covers).
		m.SetMessages(cloneMessageItems(src.Messages()))
		m.SetLastReadTS(src.LastReadTS())
		m.SetLoading(src.IsLoading())
	}
	a.winModels[id] = m
	a.focusedWin = id
	a.messagepane = m
	a.focusedPanel = PanelMessages
	return nil
}

// closeWindow closes the focused window; focus falls to its neighbor.
// Toasts "Cannot close last window" instead of ever quitting.
func (a *App) closeWindow() tea.Cmd {
	next, err := a.wins.Close(a.focusedWin)
	if err != nil {
		return toastWithClear(a, "Cannot close last window", 2*time.Second)
	}
	a.syncWinModels()
	return a.focusWindow(next)
}

// onlyWindow closes every window except the focused one.
func (a *App) onlyWindow() {
	_ = a.wins.Only(a.focusedWin)
	a.syncWinModels()
}

// cycleWindow focuses the next window in tree order (ctrl+w w).
func (a *App) cycleWindow() tea.Cmd {
	return a.focusWindow(a.wins.Cycle(a.focusedWin, 1))
}

// navigateWindow focuses the geometric neighbor (ctrl+w h/j/k/l).
// No neighbor is a silent no-op, like vim.
func (a *App) navigateWindow(nd wintree.NavDir) tea.Cmd {
	if id, ok := a.wins.NavigateDir(a.focusedWin, nd, a.windowBounds()); ok {
		return a.focusWindow(id)
	}
	return nil
}

// focusWindow moves window focus to id: a pointer swap plus an
// active-channel context retarget (compose, statusbar, typing,
// activeChannelID). Per-window models mean no channel re-dispatch.
func (a *App) focusWindow(id wintree.LeafID) tea.Cmd {
	if id == a.focusedWin {
		return nil
	}
	m := a.winModels[id]
	if m == nil {
		return nil // unknown window; invariant breach, ignore
	}
	// In-channel search is focused-pane state (App-level match list +
	// model-level highlights). Clear it BEFORE the pointer swap so the
	// outgoing model's highlights are removed; n/N against a different
	// pane's match list would misnavigate.
	a.clearActiveSearch()
	a.focusedWin = id
	a.messagepane = m
	a.focusedPanel = PanelMessages
	if a.threadVisible {
		a.CloseThread() // spec §7: thread follows focused window
	}
	if ch, ok := a.wins.Channel(id); ok && ch.ID != "" && ch.ID != a.activeChannelID {
		a.retargetActiveChannel(ch.ID, ch.Name, ch.Type)
	}
	return nil
}

// setFocusedWindowChannel records the applied channel selection on
// the focused window. Called from the ChannelSelectedMsg apply path.
//
// The selection applies to whichever window is focused at APPLY time,
// not dispatch time: a queued ChannelSelectedMsg that lands after an
// interleaved ctrl+w focus change records on the newly-focused window.
// Narrow and self-correcting — the next selection on either window
// overwrites it — and the per-window models keep their own content
// regardless; only the tree's channel label can be briefly stale.
func (a *App) setFocusedWindowChannel(id, name, chType string) {
	a.wins.SetChannel(a.focusedWin, wintree.Channel{ID: id, Name: name, Type: chType})
}
