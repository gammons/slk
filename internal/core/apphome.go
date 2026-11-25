package core

import (
	"errors"

	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
)

// AppHome is an app's Home tab as Slack serves it for the app's DM
// (conversations.info's home_view). Slack renders the tab from the
// view's Block Kit blocks, so the TUI draws it with the message Block
// Kit renderer and offers the view's buttons through the same
// interactions that a message's buttons use.
type AppHome struct {
	AppID string
	// ViewID identifies this rendering of the view. A press names it in
	// the container so the app knows which view the button belongs to.
	ViewID string
	Title  string
	// ServiceID is the app's bot ID, sent as service_id on a press.
	ServiceID string
	// Blocks is the view's content, in the same shape a message carries.
	Blocks []blocks.Block
	// Actions are the view's buttons in render order. Selects and other
	// interactive elements are not included: only buttons can be pressed
	// from the keyboard chooser.
	Actions []AppHomeAction
}

// AppHomeAction is one Block Kit button of an app's Home tab, in the
// shape a press echoes back to the app.
type AppHomeAction struct {
	ActionID string
	BlockID  string
	Label    string
	Value    string
	// URL makes the button a link: pressing it opens the URL instead of
	// reporting an action to the app.
	URL string
	// Style is Slack's button style ("primary", "danger"), empty for the
	// default style. Display only.
	Style string
	// Confirm is Slack's confirmation step for the button, nil when the
	// button has none.
	Confirm *AppHomeConfirm
}

// AppHomeConfirm is a button's confirmation dialog: what to show, and
// the two button labels.
type AppHomeConfirm struct {
	Title       string
	Text        string
	OKText      string
	DismissText string
}

// AppHomeService fetches an app's Home tab and presses its buttons.
//
// Fetch is blocking (callers run it from a tea.Cmd) and returns
// ui.AppHomeLoadedMsg; teamID is the workspace the fetch was issued in,
// because the user may switch while it is in flight.
//
// Press is blocking too. Slack answers a press by re-publishing the view
// over the WebSocket, so only the error comes back; fetch again to draw
// what the app published.
type AppHomeService interface {
	Fetch(teamID string, channelID ids.ChannelID) Msg
	Press(teamID string, channelID ids.ChannelID, viewID, serviceID string, action AppHomeAction) error
}

// AppHomeServiceFuncs bundles the closures behind an AppHomeService.
// A nil func makes its method return errors.ErrUnsupported.
type AppHomeServiceFuncs struct {
	Fetch func(teamID string, channelID ids.ChannelID) Msg
	Press func(teamID string, channelID ids.ChannelID, viewID, serviceID string, action AppHomeAction) error
}

// NewAppHomeService builds an AppHomeService from fns. Used by
// cmd/slk/main.go (production wiring) and tests.
func NewAppHomeService(fns AppHomeServiceFuncs) AppHomeService {
	return appHomeAdapter{fns: fns}
}

type appHomeAdapter struct{ fns AppHomeServiceFuncs }

func (s appHomeAdapter) Fetch(teamID string, channelID ids.ChannelID) Msg {
	if s.fns.Fetch == nil {
		return nil
	}
	return s.fns.Fetch(teamID, channelID)
}

func (s appHomeAdapter) Press(teamID string, channelID ids.ChannelID, viewID, serviceID string, action AppHomeAction) error {
	if s.fns.Press == nil {
		return errors.ErrUnsupported
	}
	return s.fns.Press(teamID, channelID, viewID, serviceID, action)
}
