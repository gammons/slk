package main

import (
	"context"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/debuglog"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/slack-go/slack"
)

// fetchAppView loads a modal's current content and converts it for the
// UI. Slack sends view_updated with no inline view when the client is
// expected to fetch it, and a pressed button's result can land later
// than the press, so both paths end here.
func fetchAppView(ctx context.Context, client *slackclient.Client, teamID, viewID string) core.Msg {
	msg := ui.AppViewLoadedMsg{TeamID: teamID, ViewID: viewID}
	v, err := client.GetView(ctx, viewID)
	if err != nil {
		debuglog.General("app view: fetch failed view=%s team=%s err=%q", viewID, teamID, err)
		msg.Err = err
		return msg
	}
	msg.View = convertAppView(v)
	return msg
}

// convertAppView turns the client's view into the UI's: parsed blocks
// for rendering, and the pressable buttons in render order. The action
// fields come from the same slack.Blocks the parser reads, so a
// rendered button and its action cannot name different things.
func convertAppView(v *slackclient.View) *core.AppView {
	parsed := blockkit.Parse(v.Blocks)
	out := &core.AppView{
		ViewID:      v.ID,
		TeamID:      v.TeamID,
		AppID:       v.AppID,
		ServiceID:   v.BotID,
		Title:       v.Title,
		CloseLabel:  v.CloseLabel,
		SubmitLabel: v.SubmitLabel,
		Blocks:      renderableViewBlocks(parsed),
	}
	if out.ServiceID == "" {
		// A view published without a bot ID still names its app.
		out.ServiceID = v.AppID
	}
	for _, b := range v.Blocks.BlockSet {
		switch b := b.(type) {
		case *slack.ActionBlock:
			if b.Elements == nil {
				continue
			}
			for _, e := range b.Elements.ElementSet {
				if btn, ok := e.(*slack.ButtonBlockElement); ok {
					out.Actions = append(out.Actions, appViewAction(b.BlockID, btn))
				}
			}
		case *slack.SectionBlock:
			// A button accessory is drawn by the section renderer,
			// which records its hit with the section's block id.
			if b.Accessory != nil && b.Accessory.ButtonElement != nil {
				out.Actions = append(out.Actions, appViewAction(b.BlockID, b.Accessory.ButtonElement))
			}
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

// appViewAction is one of a view's pressable buttons, in the shape a
// press echoes to the app.
func appViewAction(blockID string, btn *slack.ButtonBlockElement) core.AppHomeAction {
	act := core.AppHomeAction{
		ActionID: btn.ActionID,
		BlockID:  blockID,
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
	return act
}

// renderableViewBlocks makes every parsed block draw its own lines: the
// Block Kit renderer leaves rich_text to the message panes, which render
// it through the host's mrkdwn pipeline, but a modal has no such second
// pass. Convert those blocks to section text instead of dropping them.
func renderableViewBlocks(parsed []blocks.Block) []blocks.Block {
	out := make([]blocks.Block, 0, len(parsed))
	for _, b := range parsed {
		rt, ok := b.(blockkit.RichTextBlock)
		if !ok {
			out = append(out, b)
			continue
		}
		if md := blockkit.RichTextToMrkdwn(rt); md != "" {
			out = append(out, blocks.SectionBlock{Text: md})
		}
	}
	return out
}
