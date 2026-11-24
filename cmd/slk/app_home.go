package main

import (
	"context"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/slack-go/slack"
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
// for rendering, and the pressable buttons in render order.
func convertAppHome(home *slackclient.AppHome) *core.AppHome {
	out := &core.AppHome{
		AppID:     home.AppID,
		ViewID:    home.ViewID,
		Title:     home.Title,
		ServiceID: home.BotID,
		Blocks:    blockkit.Parse(home.Blocks),
	}
	for _, b := range home.Blocks.BlockSet {
		ab, ok := b.(*slack.ActionBlock)
		if !ok || ab.Elements == nil {
			continue
		}
		for _, e := range ab.Elements.ElementSet {
			btn, ok := e.(*slack.ButtonBlockElement)
			if !ok {
				continue
			}
			act := core.AppHomeAction{
				ActionID: btn.ActionID,
				BlockID:  ab.BlockID,
				Label:    textOf(btn.Text),
				Value:    btn.Value,
				URL:      btn.URL,
				Style:    string(btn.Style),
			}
			if c := btn.Confirm; c != nil {
				act.Confirm = &core.AppHomeConfirm{
					Title:       textOf(c.Title),
					Text:        textOf(c.Text),
					OKText:      textOf(c.Confirm),
					DismissText: textOf(c.Deny),
				}
			}
			out.Actions = append(out.Actions, act)
		}
	}
	return out
}

// textOf returns a Block Kit text object's text, "" when absent.
func textOf(t *slack.TextBlockObject) string {
	if t == nil {
		return ""
	}
	return t.Text
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
