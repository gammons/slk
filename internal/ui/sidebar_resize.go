package ui

import tea "charm.land/bubbletea/v2"

// reduceSidebarResize drags the sidebar's right border when
// [sidebar] mouse_resize is on: a left press on the border starts the
// drag, motion moves the border to the pointer, release saves the width
// like the [ and ] keys do when it changed. It claims mouse messages only
// while it owns the gesture, so clicks elsewhere and text-selection drags
// pass on. A lost release (button let go outside the window) ends the
// drag at the next motion without the button or the next press.
var reduceSidebarResize reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.MouseClickMsg:
		a.sidebarResizeFrom = 0
		if !a.sidebarMouseResize || !a.sidebarVisible || m.Button != tea.MouseLeft ||
			a.mode.IsModalOverlay() || a.bootstrap.IsLoading() {
			return nil, false
		}
		if m.X != a.layout.SidebarEnd()-1 || m.Y >= a.height-1 {
			return nil, false
		}
		a.sidebarResizeFrom = a.sidebar.Width()
		return nil, true
	case tea.MouseMotionMsg:
		if a.sidebarResizeFrom == 0 {
			return nil, false
		}
		if m.Button != tea.MouseLeft {
			a.sidebarResizeFrom = 0
			return nil, false
		}
		a.sidebar.SetWidth(m.X - a.layout.RailWidth() - 1)
		return nil, true
	case tea.MouseReleaseMsg:
		from := a.sidebarResizeFrom
		if from == 0 {
			return nil, false
		}
		a.sidebarResizeFrom = 0
		if a.settings != nil && a.sidebar.Width() != from {
			a.settings.SaveSidebarWidth(a.sidebar.Width())
		}
		return nil, true
	}
	return nil, false
}
