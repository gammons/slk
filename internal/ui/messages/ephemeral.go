package messages

import (
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
)

// EphemeralLabel is the marker row drawn above the author line of a
// message only the current user can see. Shared by the messages and
// thread panes so the two cannot drift.
func EphemeralLabel() string {
	return styles.Timestamp.Render("Only visible to you")
}

// ButtonAction is one pressable button: a legacy button and the
// attachment it is on, or a Block Kit button (Block set, Attachment and
// Action empty).
type ButtonAction struct {
	Attachment blockkit.LegacyAttachment
	Action     blockkit.LegacyAction

	Block *blockkit.ActionElement
	// BlockID is the Block Kit button's actions block, echoed on press.
	BlockID string
	// Card is the mrkdwn text of the section just above the button's
	// actions block, "" when there is none: what the row of buttons is
	// about, when a message carries several.
	Card string
}

// Label is the button's text.
func (b ButtonAction) Label() string {
	if b.Block != nil {
		return b.Block.Label
	}
	return b.Action.Text
}

// URL is the link the button opens instead of calling the app, if any.
func (b ButtonAction) URL() string {
	if b.Block != nil {
		return b.Block.URL
	}
	return b.Action.URL
}

// Confirm is the button's confirmation step, nil when it has none.
func (b ButtonAction) Confirm() *blockkit.ActionConfirm {
	if b.Block != nil {
		return b.Block.Confirm
	}
	return b.Action.Confirm
}

// ButtonActions returns msg's pressable buttons in order. An ephemeral's
// are its legacy buttons (chat.attachmentAction); any other message's are
// its top-level Block Kit buttons (blocks.actions), which need the app's
// bot ID. Select menus are not buttons and are skipped.
//
// ponytail: Block Kit buttons on ephemerals and in section accessories
// are left out until a capture shows how the web client presses them.
func ButtonActions(msg MessageItem) []ButtonAction {
	var out []ButtonAction
	if msg.Ephemeral {
		for _, att := range msg.LegacyAttachments {
			for _, act := range att.Actions {
				if act.Type == "button" {
					out = append(out, ButtonAction{Attachment: att, Action: act})
				}
			}
		}
		return out
	}
	if msg.BotID == "" {
		return nil
	}
	card := ""
	for _, b := range msg.Blocks {
		switch v := b.(type) {
		case blockkit.SectionBlock:
			card = v.Text
		case blockkit.ActionsBlock:
			for i := range v.Elements {
				if v.Elements[i].Kind == "button" {
					out = append(out, ButtonAction{Block: &v.Elements[i], BlockID: v.BlockID, Card: card})
				}
			}
			card = ""
		}
	}
	return out
}

// ButtonIndex is the index in ButtonActions(msg) of the Block Kit
// button (blockID, actionID), or -1.
func ButtonIndex(msg MessageItem, blockID, actionID string) int {
	for i, ba := range ButtonActions(msg) {
		if ba.Block != nil && ba.BlockID == blockID && ba.Block.ActionID == actionID {
			return i
		}
	}
	return -1
}

// HasButtonActions reports whether msg has a button to press with b.
func HasButtonActions(msg MessageItem) bool {
	return len(ButtonActions(msg)) > 0
}

// InteractiveHint is the line under a message with interactive elements:
// a message with buttons can be answered in slk with b; anything else
// still needs Slack.
func InteractiveHint(msg MessageItem) string {
	if HasButtonActions(msg) {
		return "b to respond"
	}
	return "↗ open in Slack to interact"
}
