// internal/ui/reducer_load_images.go
//
// The reducer for the `I` load prompt's answer. It lives here rather
// than beside the prompt in load_images.go because AGENTS.md puts
// reducer behavior in a reducer_*.go file (compare
// reducer_remove_preview.go for `x`). The prompt itself, its choice and
// the policy update stay in load_images.go.
package ui

import (
	tea "charm.land/bubbletea/v2"
)

// reduceLoadImages applies an imagesAllowedMsg: the URLs and hosts the
// user allowed at the load prompt go into the session policy, and the
// render caches are dropped so the panes fetch what is now permitted.
var reduceLoadImages reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(imagesAllowedMsg)
	if !ok {
		return nil, false
	}
	a.allowImages(m)
	return nil, true
}
