// internal/ui/reducer_load_images.go
//
// The reducer for the `I` load prompt's answer. It lives here rather
// than beside the prompt in load_images.go because AGENTS.md puts
// reducer behavior in a reducer_*.go file (compare
// reducer_remove_preview.go for `x`). The prompt itself, its choice and
// the policy update stay in load_images.go.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// reduceLoadImages applies an imagesAllowedMsg: the URLs and hosts the
// user allowed at the load prompt go into the session policy, and the
// render caches are dropped so the panes fetch what is now permitted.
//
// A prompt a preview raised carries that request, which is replayed
// here rather than opened directly: the open path (imagePreviewController
// .Handle) re-checks the policy, builds the loading overlay and starts
// the fetch, so approving ends where the keypress would have without the
// prompt. The re-check is also the backstop for an image the policy
// still refuses, which cannot ask twice.
var reduceLoadImages reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	m, ok := msg.(imagesAllowedMsg)
	if !ok {
		return nil, false
	}
	a.allowImages(m)
	if m.Preview == nil {
		return nil, true
	}
	// A dialog that queued behind this prompt (an ephemeral's buttons,
	// say) is opened by afterConfirmPrompt one step before this replay
	// lands, so the preview would go up over it -- and the preview
	// swallows every key (the pre-switch interception in App.Update),
	// leaving that dialog visible but unanswerable. The approval is
	// already recorded either way, so the panes draw the images inline;
	// only the full-screen preview waits for the user to ask again.
	if a.mode.IsModalOverlay() {
		return toastWithClear(a, "Images loaded · O to preview", 3*time.Second), true
	}
	req := *m.Preview
	return func() tea.Msg { return req }, true
}
