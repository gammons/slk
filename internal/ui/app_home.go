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
	"strings"
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
	// Another conversation's press must not keep animating here.
	a.clearAppHomeBusy()
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
	a.clearAppHomeBusy()
}

// appHomeBusyState is the status-bar progress dots for a pressed Home
// button: how many of its refetches are still outstanding, the current
// dot count, the toast identity (a newer message replaces the dots and
// is never cleared by them), and whether a tick chain is armed.
type appHomeBusyState struct {
	// gen numbers presses; a refetch counts toward the dots only when it
	// carries the generation of the press that asked for it.
	gen       uint64
	pending   int
	dots      int
	toastSeq  uint64
	tickArmed bool
}

// appHomeBusyText is the in-flight indicator shown while a pressed
// button's view is being refetched.
func appHomeBusyText(dots int) string {
	return "Updating app view" + strings.Repeat(".", dots)
}

// startAppHomeBusy begins the status-bar dots for a press: the immediate
// refetch and the delayed follow-up are both pending. A second press
// before the first finishes adds its two, so neither press's loads end
// the dots early; only the first press starts the tick chain.
func (a *App) startAppHomeBusy() (tea.Cmd, uint64) {
	a.appHomeBusy.gen++
	a.appHomeBusy.pending = 2
	a.appHomeBusy.dots = 1
	toastUntilReplaced(a, appHomeBusyText(a.appHomeBusy.dots))
	a.appHomeBusy.toastSeq = a.statusbar.ToastSeq()
	seq := a.appHomeBusy.gen
	if a.appHomeBusy.tickArmed {
		return nil, seq
	}
	a.appHomeBusy.tickArmed = true
	return tea.Tick(appHomeSpinnerInterval, func(time.Time) tea.Msg {
		return appHomeSpinnerMsg{}
	}), seq
}

// finishAppHomeFetch records one of the current press's refetches
// landing, ending the indicator when the last one has. A result from an
// earlier press (another generation) belongs to a superseded animation
// and is ignored.
func (a *App) finishAppHomeFetch(seq uint64) {
	if seq == 0 || seq != a.appHomeBusy.gen || a.appHomeBusy.pending == 0 {
		return
	}
	a.appHomeBusy.pending--
	if a.appHomeBusy.pending == 0 {
		a.clearAppHomeBusyToast()
	}
}

// appHomeFetchCmd runs svc.Fetch and tags the resulting AppHomeLoadedMsg
// with the press generation it belongs to.
func appHomeFetchCmd(svc core.AppHomeService, teamID, channelID string, seq uint64) tea.Cmd {
	return func() tea.Msg {
		msg := svc.Fetch(teamID, ids.ChannelID(channelID))
		if loaded, ok := msg.(AppHomeLoadedMsg); ok {
			loaded.Seq = seq
			return loaded
		}
		return msg
	}
}

// clearAppHomeBusy stops the indicator (the press was abandoned or its
// conversation left).
func (a *App) clearAppHomeBusy() {
	a.appHomeBusy.pending = 0
	// tickArmed stays set: an in-flight tick sees pending == 0 and ends
	// the chain itself. Clearing it here would let a press within the
	// tick interval start a second chain and run the dots at double
	// speed.
	a.clearAppHomeBusyToast()
}

// clearAppHomeBusyToast clears the dots if they are still the toast on
// screen; a newer message replaces them and stays.
func (a *App) clearAppHomeBusyToast() {
	if a.appHomeBusy.toastSeq != 0 && a.statusbar.ToastSeq() == a.appHomeBusy.toastSeq {
		a.statusbar.SetToast("")
	}
	a.appHomeBusy.toastSeq = 0
}

// appHomeAvailable reports whether the active conversation has a Home
// tab fetched and ready to show.
func (a *App) appHomeAvailable() bool {
	return a.appHome.Home() != nil && a.appHomeChannelID == a.activeChannelID
}

// appBlocksRenderContext is the Block Kit host context the app-owned
// surfaces render with: an app DM's Home tab, and a modal. It carries
// the messages pane's mention names and emoji, and no image pipeline.
// Nothing paints image placements on those surfaces, and no fetch
// completion repaints them, so an empty renderer context makes image
// blocks draw their fallback links instead of reserving rows nothing
// will fill.
func (a *App) appBlocksRenderContext() blockkit.Context {
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

// clickAppHomeAction is a mouse click on the Home tab's button idx: a
// plain button presses, a URL button opens the link, and a button with
// Slack's confirmation step asks first (its dismiss just closes).
func (a *App) clickAppHomeAction(idx int) tea.Cmd {
	home := a.appHome.Home()
	if home == nil || idx < 0 || idx >= len(home.Actions) {
		return nil
	}
	act := home.Actions[idx]
	switch {
	case act.URL != "":
		url := act.URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	case act.Confirm != nil:
		a.openConfirmChoice(*a.appHomeConfirmChoice(act, nil))
		return nil
	default:
		return a.pressAppHomeAction(act)
	}
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
		k.SearchMode, k.SearchNext, k.SearchPrev, k.OpenInEditor,
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
