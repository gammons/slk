package core

import (
	"errors"

	"github.com/gammons/slk/internal/core/blocks"
)

// AppView is a modal an app opened for the user (Slack calls these
// views): a read-only dialog of Block Kit blocks, or a form slk does
// not support filling in yet. Its buttons can be pressed, and Slack
// answers a press by publishing a new version of the view.
type AppView struct {
	// ViewID identifies this view in Slack; presses and refreshes name
	// it.
	ViewID string
	// TeamID is the workspace the view belongs to.
	TeamID string
	// AppID is the app that published the view.
	AppID string
	// ServiceID is the app's bot ID, sent as service_id on a press.
	ServiceID string
	// Title heads the dialog.
	Title string
	// CloseLabel is the dialog's own close-button label ("Close",
	// "Cancel", "Back"). Display only: slk closes the dialog locally.
	CloseLabel string
	// SubmitLabel is non-empty when the view has a form to submit,
	// which slk cannot do yet.
	SubmitLabel string
	// Blocks is the view's content, in the same shape a message
	// carries.
	Blocks []blocks.Block
	// Actions are the view's buttons in render order, pressable through
	// the keyboard chooser.
	Actions []AppHomeAction
}

// AppViewService fetches a modal's current content and presses its
// buttons.
//
// Fetch is blocking (callers run it from a tea.Cmd) and returns
// ui.AppViewLoadedMsg; teamID is the workspace the fetch was issued in,
// because the user may switch while it is in flight.
//
// Press is blocking too, and returns only an error: Slack re-publishes
// the view over the WebSocket, so the UI reloads it to draw the result.
type AppViewService interface {
	Fetch(teamID, viewID string) Msg
	Press(teamID, viewID, serviceID string, action AppHomeAction) error
}

// AppViewServiceFuncs bundles the closures behind an AppViewService.
// A nil func makes its method return errors.ErrUnsupported.
type AppViewServiceFuncs struct {
	Fetch func(teamID, viewID string) Msg
	Press func(teamID, viewID, serviceID string, action AppHomeAction) error
}

// NewAppViewService builds an AppViewService from fns. Used by
// cmd/slk/main.go (production wiring) and tests.
func NewAppViewService(fns AppViewServiceFuncs) AppViewService {
	return appViewAdapter{fns: fns}
}

type appViewAdapter struct{ fns AppViewServiceFuncs }

func (s appViewAdapter) Fetch(teamID, viewID string) Msg {
	if s.fns.Fetch == nil {
		return nil
	}
	return s.fns.Fetch(teamID, viewID)
}

func (s appViewAdapter) Press(teamID, viewID, serviceID string, action AppHomeAction) error {
	if s.fns.Press == nil {
		return errors.ErrUnsupported
	}
	return s.fns.Press(teamID, viewID, serviceID, action)
}
