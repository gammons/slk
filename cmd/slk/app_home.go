package main

import (
	"context"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// fetchAppHome loads an app DM's Home tab and converts it for the UI.
// A conversation without one (a plain bot DM, or an app whose home tab
// is disabled) yields a result with no Home, not an error: the pane
// keeps showing messages.
//
// The request is the web client's conversations.info with
// return_app_home=true. boot.ConversationsInfo asks the same question
// for the messaging-capability probe but decodes only app_home, which is
// all that probe consumes; the home_view blocks are decoded here.
func fetchAppHome(ctx context.Context, client *slackclient.Client, teamID, channelID string) core.Msg {
	msg := ui.AppHomeLoadedMsg{TeamID: teamID, ChannelID: channelID}
	home, err := client.GetAppHome(ctx, channelID)
	if err != nil {
		debuglog.General("app home: fetch failed channel=%s team=%s err=%q", channelID, teamID, err)
		msg.Err = err
		return msg
	}
	if home == nil {
		return msg
	}
	msg.Home = convertAppHome(home)
	return msg
}

// convertAppHome turns the client's view into the UI's: parsed blocks
// for rendering, and the pressable buttons in render order. Both come
// from the same parsed blocks, so a rendered button and its action
// cannot drift apart.
func convertAppHome(home *slackclient.AppHome) *core.AppHome {
	parsed := blockkit.Parse(home.Blocks)
	out := &core.AppHome{
		AppID:     home.AppID,
		ViewID:    home.ViewID,
		Title:     home.Title,
		ServiceID: home.BotID,
		Blocks:    parsed,
	}
	for _, b := range parsed {
		ab, ok := b.(blockkit.ActionsBlock)
		if !ok {
			continue
		}
		for _, el := range ab.Elements {
			if el.Kind != "button" {
				continue
			}
			act := core.AppHomeAction{
				ActionID: el.ActionID,
				BlockID:  ab.BlockID,
				Label:    el.Label,
				Value:    el.Value,
				URL:      el.URL,
				Style:    el.Style,
			}
			if c := el.Confirm; c != nil {
				act.Confirm = &core.AppHomeConfirm{
					Title:       c.Title,
					Text:        c.Text,
					OKText:      c.OKText,
					DismissText: c.DismissText,
				}
			}
			out.Actions = append(out.Actions, act)
		}
	}
	return out
}

// pressAppHomeAction presses one Home-tab button on the app's behalf.
// No retry: a press can have side effects, so a replayed request could
// act twice.
func pressAppHomeAction(ctx context.Context, client *slackclient.Client,
	viewID, serviceID string, action core.AppHomeAction) error {
	req := slackclient.BlockActionRequest{
		ViewID:    viewID,
		ServiceID: serviceID,
		ActionID:  action.ActionID,
		BlockID:   action.BlockID,
		Label:     action.Label,
		Value:     action.Value,
		Style:     action.Style,
	}
	if c := action.Confirm; c != nil {
		req.Confirm = &slackclient.BlockActionConfirm{
			Title:       c.Title,
			Text:        c.Text,
			OKText:      c.OKText,
			DismissText: c.DismissText,
		}
	}
	return client.BlockAction(ctx, req)
}
