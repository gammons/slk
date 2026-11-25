// internal/ui/app_view.go
//
// A modal an app opened (a Slack "view"): showing it, scrolling it,
// dismissing it, and pressing its Block Kit buttons. The dialog itself
// is rendered by internal/ui/appview; results reduce in
// reducer_app_view.go.
package ui

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/overlay"
)

// appViewActionPressedMsg reports a modal button press. teamID and
// viewID route the result: the user may have switched workspaces or
// the app may have pushed another view while the press was in flight.
type appViewActionPressedMsg struct {
	teamID string
	viewID string
	label  string
	err    error
}

// appViewRefreshMsg asks for a modal's content after a press. Slack
// usually publishes the result itself; one bounded fetch covers an app
// that only answers through views.get.
type appViewRefreshMsg struct {
	teamID string
	viewID string
}

// openAppView shows v over the current screen, replacing any modal
// already open (a pushed view included). The view's blocks come from
// Slack; nothing is fetched here.
func (a *App) openAppView(v *core.AppView) {
	if v == nil {
		return
	}
	a.closeAppViewPrompt()
	a.appView.SetView(v)
	a.appViewGen++
	a.SetMode(ModeAppView)
}

// closeAppViewPrompt closes a confirm prompt drawn over the app dialog
// before new content takes the screen: the button chooser (whose
// actions belong to the old view), or any other prompt (a quit prompt
// that arrived while a press was in flight). Left drawn, it would keep
// the screen while the keys moved to the dialog.
func (a *App) closeAppViewPrompt() {
	if a.confirmPrompt.IsVisible() {
		a.confirmPrompt.Close()
	}
	// The prompt may have belonged to an ephemeral's action dialog.
	// Forgetting it here keeps a later delete of that message from
	// treating the app's own chooser as the interrupted dialog.
	a.clearShownPrompt()
	a.appViewBehind = false
}

// closeAppView dismisses the modal at the user's request.
func (a *App) closeAppView() {
	a.appView.Clear()
	if a.mode == ModeAppView {
		a.SetMode(ModeNormal)
	}
}

// appViewOverlay draws the active modal over the dimmed screen, clamped
// to the terminal height so a tall dialog cannot scroll it.
func (a *App) appViewOverlay(background string) string {
	box := a.appView.View(a.width, a.height, a.appBlocksRenderContext())
	if box == "" {
		return background
	}
	out := overlay.DimmedOverlay(a.width, a.height, background, box, 0.5)
	if lines := strings.Split(out, "\n"); len(lines) > a.height {
		return strings.Join(lines[:a.height], "\n")
	}
	return out
}

// clickAppViewAction is a mouse click on the dialog's button idx: a
// button with Slack's confirmation step asks first (its dismiss just
// closes), then a URL button opens the link, and a plain button presses.
func (a *App) clickAppViewAction(idx int) tea.Cmd {
	v := a.appView.Current()
	if v == nil || idx < 0 || idx >= len(v.Actions) {
		return nil
	}
	act := v.Actions[idx]
	switch {
	case act.Confirm != nil:
		a.openConfirmChoiceReturning(*a.appViewConfirmChoice(act, nil), ModeAppView)
		return nil
	case act.URL != "":
		return a.appViewActionMsg(act)
	default:
		return a.pressAppViewAction(act)
	}
}

// openAppViewActions raises the chooser for the dialog's buttons, one
// per button with number keys 1-9, like the Home tab's chooser. The
// dialog stays behind it and comes back when the chooser closes.
func (a *App) openAppViewActions() tea.Cmd {
	v := a.appView.Current()
	if v == nil {
		return nil
	}
	if len(v.Actions) == 0 {
		return toastWithClear(a, "This dialog has no buttons", 2*time.Second)
	}
	choice := &confirmprompt.Choice{
		Title:   "Dialog buttons",
		Body:    "Choose a button from the app's dialog.",
		Default: -1,
	}
	for i, act := range v.Actions {
		label := emoji.ResolveShortcodesInText(emoji.StripSkinToneFromText(act.Label))
		b := confirmprompt.Button{Label: label}
		if i < 9 {
			b.Keys = key.NewBinding(key.WithKeys(strconv.Itoa(i + 1)))
		}
		switch {
		case act.Confirm != nil:
			b.Then = a.appViewConfirmChoice(act, choice)
		case act.URL != "":
			b.Action = a.appViewActionMsg(act)
		default:
			b.Action = a.pressAppViewAction(act)
		}
		choice.Buttons = append(choice.Buttons, b)
	}
	a.openConfirmChoiceReturning(*choice, ModeAppView)
	return nil
}

// appViewActionMsg is what pressing act does once its confirmation step
// (if any) has been accepted: a URL button opens the link, and any
// other button reports the press to the app.
func (a *App) appViewActionMsg(act core.AppHomeAction) func() tea.Msg {
	if act.URL != "" {
		url := act.URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	}
	return a.pressAppViewAction(act)
}

// appViewConfirmChoice is Slack's confirmation step for act. Its
// dismiss button returns to back; its OK button presses.
func (a *App) appViewConfirmChoice(act core.AppHomeAction, back *confirmprompt.Choice) *confirmprompt.Choice {
	c := act.Confirm
	title, ok, dismiss := c.Title, c.OKText, c.DismissText
	if title == "" {
		title = "Are you sure?"
	}
	if ok == "" {
		ok = "Okay"
	}
	if dismiss == "" {
		dismiss = "Cancel"
	}
	return &confirmprompt.Choice{
		Title:   title,
		Body:    c.Text,
		Default: -1,
		Buttons: []confirmprompt.Button{
			{Label: dismiss, Keys: key.NewBinding(key.WithKeys("1")), Then: back},
			{Label: ok, Keys: key.NewBinding(key.WithKeys("2")), Action: a.appViewActionMsg(act)},
		},
	}
}

// pressAppViewAction is a button's action: press it through the app
// view service, off the UI goroutine, and report the result. The view
// is captured now so the press goes to the view the user saw.
func (a *App) pressAppViewAction(act core.AppHomeAction) func() tea.Msg {
	v := a.appView.Current()
	if v == nil {
		return nil
	}
	svc := a.appViewSvc
	team, viewID, serviceID := a.activeTeamID, v.ViewID, v.ServiceID
	return func() tea.Msg {
		err := svc.Press(team, viewID, serviceID, act)
		return appViewActionPressedMsg{
			teamID: team, viewID: viewID, label: act.Label, err: err,
		}
	}
}
