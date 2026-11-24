// internal/ui/app_home.go
//
// An app DM's Home tab: fetching it, switching between the Home and
// Messages tabs, and pressing its Block Kit buttons. The tab itself is
// rendered by internal/ui/apphome; the press results reduce in
// reducer_app_home.go.
package ui

import (
	"io"
	"strconv"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

// appHomeActionPressedMsg reports a Home-tab button press. teamID and
// channelID route the result: the user may have left the conversation
// while the press was in flight.
type appHomeActionPressedMsg struct {
	teamID    string
	channelID string
	label     string
	err       error
}

// fetchAppHomeCmd asks the AppHomeService for channelID's Home tab in
// teamID, and forgets the tab held for the previous conversation. The
// result is AppHomeLoadedMsg; a nil Home means the conversation has no
// Home tab, which is the common case for a bot DM.
//
// autoShow is false when the channel is being opened to land on a
// message (a permalink or a saved position): the tab is still fetched,
// for t, but must not replace the message the navigation is about.
func (a *App) fetchAppHomeCmd(teamID, channelID, channelName string, autoShow bool) tea.Cmd {
	a.appHome.Clear()
	a.appHomeChannelID = channelID
	a.appHomeAutoShow = autoShow
	a.appHome.SetChannelName(channelName)
	svc := a.appHomeSvc
	return func() tea.Msg {
		return svc.Fetch(teamID, ids.ChannelID(channelID))
	}
}

// clearAppHome forgets the Home tab and returns the pane to messages.
func (a *App) clearAppHome() {
	if a.view == ViewAppHome {
		a.view = ViewChannels
	}
	a.appHome.Clear()
	a.appHomeChannelID = ""
	a.appHomeAutoShow = false
}

// appHomeAvailable reports whether the active conversation has a Home
// tab fetched and ready to show.
func (a *App) appHomeAvailable() bool {
	return a.appHome.Home() != nil && a.appHomeChannelID == a.activeChannelID
}

// appHomeRenderContext is the Block Kit host context the Home tab
// renders with: the messages pane's mention names and emoji, and no
// image pipeline. Nothing paints image placements in this pane, and no
// fetch completion repaints it, so an empty renderer context makes
// image blocks draw their fallback links instead of reserving rows
// nothing will fill.
func (a *App) appHomeRenderContext() blockkit.Context {
	rc := a.messagepane.DraftRenderContext()
	rc.Images = imgrender.NewRenderer()
	var flushes []func(io.Writer) error
	return messages.BlockkitContext(messages.MessageItem{}, rc, &flushes)
}

// toggleAppHome switches the messages region between the active app
// DM's Home tab and its messages.
func (a *App) toggleAppHome() tea.Cmd {
	if !a.appHomeAvailable() {
		return nil
	}
	if a.view == ViewAppHome {
		a.view = ViewChannels
		return nil
	}
	a.view = ViewAppHome
	return nil
}

// openAppHomeActions raises the chooser for the Home tab's buttons, one
// per button with number keys 1-9, as openActionPrompt does for a
// message's legacy buttons.
func (a *App) openAppHomeActions() tea.Cmd {
	home := a.appHome.Home()
	if home == nil {
		return nil
	}
	if len(home.Actions) == 0 {
		return toastWithClear(a, "This home tab has no buttons", 2*time.Second)
	}
	choice := &confirmprompt.Choice{
		Title:   "App buttons",
		Body:    "Choose a button from the app's Home tab.",
		Default: -1,
	}
	for i, act := range home.Actions {
		// The view renders the label with emoji expanded; the chooser is
		// plain text, so expand shortcodes the same way.
		label := emoji.ResolveShortcodesInText(emoji.StripSkinToneFromText(act.Label))
		b := confirmprompt.Button{Label: label}
		if i < 9 {
			b.Keys = key.NewBinding(key.WithKeys(strconv.Itoa(i + 1)))
		}
		switch {
		case act.URL != "":
			url := act.URL
			b.Action = func() tea.Msg { return OpenLinkMsg{URL: url} }
		case act.Confirm != nil:
			b.Then = a.appHomeConfirmChoice(act, choice)
		default:
			b.Action = a.pressAppHomeAction(act)
		}
		choice.Buttons = append(choice.Buttons, b)
	}
	a.openConfirmChoice(*choice)
	return nil
}

// appHomeConfirmChoice is Slack's confirmation step for act. Its
// dismiss button returns to back; its OK button presses.
func (a *App) appHomeConfirmChoice(act core.AppHomeAction, back *confirmprompt.Choice) *confirmprompt.Choice {
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
			{Label: ok, Keys: key.NewBinding(key.WithKeys("2")), Action: a.pressAppHomeAction(act)},
		},
	}
}

// appHomeMessageOp reports whether msg acts on the messages pane's
// selection, which is not on screen while a Home tab is shown. Those
// keys are ignored there rather than acting on a hidden message; the
// tab's own keys (b, t, the scroll keys) are handled elsewhere.
func (a *App) appHomeMessageOp(msg tea.KeyMsg) bool {
	k := a.keys
	for _, b := range []key.Binding{
		k.Reaction, k.ReactionNav, k.ListReactions, k.UserProfile,
		k.SaveThread, k.CopyMessage, k.CopyPermalink, k.ForwardMessage,
		k.Edit, k.Delete, k.RemovePreview, k.OpenPreview, k.OpenLink,
		k.DownloadFile, k.MarkUnread, k.MarkSet, k.JumpToParent,
		k.SearchMode, k.SearchNext, k.SearchPrev,
	} {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

// pressAppHomeAction is a button's action: press it through the AppHome
// service, off the UI goroutine, and report the result. The view is
// captured now so the press goes to the view the user saw.
func (a *App) pressAppHomeAction(act core.AppHomeAction) func() tea.Msg {
	home := a.appHome.Home()
	if home == nil {
		return nil
	}
	svc := a.appHomeSvc
	team, channel := a.activeTeamID, a.activeChannelID
	viewID, serviceID := home.ViewID, home.ServiceID
	return func() tea.Msg {
		err := svc.Press(team, ids.ChannelID(channel), viewID, serviceID, act)
		return appHomeActionPressedMsg{
			teamID: team, channelID: channel, label: act.Label, err: err,
		}
	}
}
