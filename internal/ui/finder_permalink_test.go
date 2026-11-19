package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/sidebar"
)

func finderPermalinkApp(t *testing.T) (*App, *string) {
	t.Helper()
	app := newTestApp(t,
		withActiveTeam("T1"),
		withActiveChannel("C054JFCBN69"),
		withChannelFinderOpen(
			core.ChannelFinderItem{ID: "C054JFCBN69", Name: "general", Type: "channel", Joined: true},
			core.ChannelFinderItem{ID: "C2", Name: "engineering", Type: "channel", Joined: true},
		),
		withMode(ModeChannelFinder),
	)
	app.workspaceDomains["T1"] = "myteam"
	var opened string
	app.browserOpener = func(url string) tea.Cmd {
		opened = url
		return nil
	}
	app.setChannelLookupFuncForTest(func(id ids.ChannelID) (string, string, bool) {
		switch id {
		case "C054JFCBN69":
			return "general", "channel", true
		case "CTARGET01":
			return "random", "channel", true
		default:
			return "", "", false
		}
	})
	return app, &opened
}

func pasteFinder(t *testing.T, app *App, text string) {
	t.Helper()
	app.Update(tea.PasteMsg{Content: text})
}

func TestFinderPermalink_PasteShowsOpenMessage(t *testing.T) {
	app, _ := finderPermalinkApp(t)
	const url = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"
	pasteFinder(t, app, url+"\n")

	items := app.channelFinder.FilteredItems()
	if len(items) != 1 || items[0].Name != "Open message" || items[0].Type != channelfinder.PermalinkType || items[0].ID != url {
		t.Fatalf("filtered = %+v, want one Open message for the pasted permalink", items)
	}
	if app.channelFinder.PermalinkURL() != url {
		t.Fatalf("PermalinkURL = %q, want the trimmed link", app.channelFinder.PermalinkURL())
	}

	// A channel name still filters channels.
	app.channelFinder.Open()
	pasteFinder(t, app, "eng")
	items = app.channelFinder.FilteredItems()
	if len(items) == 0 || items[0].ID != "C2" {
		t.Fatalf("non-permalink paste filtered = %+v, want engineering", items)
	}
}

func TestFinderPermalink_ThreadTSOpensThread(t *testing.T) {
	app, opened := finderPermalinkApp(t)
	var fetchedChannel, fetchedThread string
	app.setThreadFetcherForTest(func(channelID ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
		fetchedChannel, fetchedThread = string(channelID), string(threadTS)
		return nil
	})
	const url = "https://myteam.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100"
	pasteFinder(t, app, url)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	drainCmd(cmd)

	if !app.threadVisible {
		t.Fatal("thread panel not visible")
	}
	if got := app.threadPanel.ThreadTS(); got != "1779284700.000100" {
		t.Errorf("ThreadTS = %q", got)
	}
	if fetchedChannel != "C054JFCBN69" || fetchedThread != "1779284700.000100" {
		t.Errorf("fetch = (%q, %q)", fetchedChannel, fetchedThread)
	}
	if *opened != "" {
		t.Errorf("browser opened %q", *opened)
	}
	if app.mode != ModeNormal || app.channelFinder.IsVisible() {
		t.Errorf("mode=%v finder visible=%v, want normal and closed", app.mode, app.channelFinder.IsVisible())
	}
}

func TestFinderPermalink_CrossWorkspaceAliasSwitchesThenNavigates(t *testing.T) {
	app, opened := finderPermalinkApp(t)
	app.workspaceDomains["T2"] = "beta"
	app.permalinkDomains["T2"] = []string{"beta.enterprise"}
	var switchedTo string
	app.SetWorkspaceService(core.NewWorkspaceService(func(teamID string) core.Msg {
		switchedTo = teamID
		return WorkspaceSwitchedMsg{
			TeamID:           teamID,
			TeamName:         "Beta",
			Domain:           "beta",
			PermalinkDomains: []string{"beta.enterprise"},
			Channels: []sidebar.ChannelItem{
				{ID: "COTHER01", Name: "other", Type: "channel"},
				{ID: "CTARGET01", Name: "random", Type: "channel"},
			},
		}
	}))

	const url = "https://beta.enterprise.slack.com/archives/CTARGET01/p1779284733270139?thread_ts=1779284700.000100"
	pasteFinder(t, app, url)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	msgs := drainCmd(cmd)
	if len(msgs) != 1 {
		t.Fatalf("enter produced %#v, want one workspace switch", msgs)
	}
	sw, ok := msgs[0].(WorkspaceSwitchedMsg)
	if !ok || sw.TeamID != "T2" || switchedTo != "T2" {
		t.Fatalf("switch msg %#v, recorded %q", msgs[0], switchedTo)
	}
	if *opened != "" {
		t.Fatalf("browser opened %q before the switch", *opened)
	}

	_, cmd = app.Update(sw)
	var selected []ChannelSelectedMsg
	for _, m := range drainCmd(cmd) {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			selected = append(selected, cs)
		}
	}
	if len(selected) != 1 || selected[0].ID != "CTARGET01" || selected[0].Name != "random" {
		t.Fatalf("channel selection = %+v, want only CTARGET01", selected)
	}
	if app.activeTeamID != "T2" {
		t.Fatalf("active team = %q, want T2", app.activeTeamID)
	}
	p := app.pendingLinkNav
	if p == nil || string(p.ChannelID) != "CTARGET01" || string(p.MessageTS) != "1779284733.270139" || string(p.ThreadTS) != "1779284700.000100" {
		t.Fatalf("pendingLinkNav = %+v", p)
	}
	if *opened != "" {
		t.Errorf("browser opened %q", *opened)
	}
}

func TestFinderPermalink_SharedChannelStillSelectsAfterSwitch(t *testing.T) {
	// A Slack Connect channel has one ID in both workspaces. The switch
	// resets the pane and detaches the composer, but activeChannelID
	// still names that channel. The jump has to go through
	// ChannelSelectedMsg anyway, or the thread opens against an empty
	// pane and the composer stays detached.
	app, opened := finderPermalinkApp(t)
	app.activeChannelID = "CSHARED01"
	app.compose.SetActiveChannel("CSHARED01")
	app.workspaceDomains["T2"] = "beta"
	app.setChannelLookupFuncForTest(func(id ids.ChannelID) (string, string, bool) {
		if id == "CSHARED01" {
			return "shared", "channel", true
		}
		return "", "", false
	})
	app.setThreadFetcherForTest(func(ids.ChannelID, ids.ThreadTS) core.Msg { return nil })
	app.SetWorkspaceService(core.NewWorkspaceService(func(teamID string) core.Msg {
		return WorkspaceSwitchedMsg{
			TeamID: teamID,
			Domain: "beta",
			Channels: []sidebar.ChannelItem{
				{ID: "CSHARED01", Name: "shared", Type: "channel"},
			},
		}
	}))

	const url = "https://beta.slack.com/archives/CSHARED01/p1779284733270139?thread_ts=1779284700.000100"
	pasteFinder(t, app, url)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	sw, ok := drainCmd(cmd)[0].(WorkspaceSwitchedMsg)
	if !ok {
		t.Fatalf("enter did not switch workspace")
	}
	_, cmd = app.Update(sw)
	var selected []ChannelSelectedMsg
	for _, m := range drainCmd(cmd) {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			selected = append(selected, cs)
		}
	}
	if len(selected) != 1 || selected[0].ID != "CSHARED01" || selected[0].Name != "shared" {
		t.Fatalf("channel selection = %+v, want ChannelSelectedMsg for the shared channel", selected)
	}
	if app.threadVisible {
		t.Fatal("thread opened during the switch, before the channel was selected")
	}
	if len(app.messagepane.Messages()) != 0 {
		t.Fatalf("message pane = %d messages, want it empty until the channel loads", len(app.messagepane.Messages()))
	}
	if *opened != "" {
		t.Fatalf("browser opened %q", *opened)
	}

	app.Update(selected[0])
	if app.activeChannelID != "CSHARED01" || app.activeTeamID != "T2" {
		t.Fatalf("after select: channel %q team %q", app.activeChannelID, app.activeTeamID)
	}
	if app.compose.ActiveChannel() != "CSHARED01" {
		t.Fatalf("composer channel = %q, want it rebound to the shared channel", app.compose.ActiveChannel())
	}
	if !app.threadVisible || app.threadPanel.ThreadTS() != "1779284700.000100" {
		t.Fatalf("thread visible=%v ts=%q", app.threadVisible, app.threadPanel.ThreadTS())
	}
}

func TestFinderPermalink_StaleSwitchKeepsNewerLink(t *testing.T) {
	// Submitting a link for T3 while T2's switch is still in flight must
	// not let T2's result throw away T3's link.
	app, opened := finderPermalinkApp(t)
	app.workspaceDomains["T2"] = "beta"
	app.workspaceDomains["T3"] = "gamma"
	app.setChannelLookupFuncForTest(func(id ids.ChannelID) (string, string, bool) {
		switch id {
		case "CTWOLINK":
			return "from-beta", "channel", true
		case "CTHREELK":
			return "from-gamma", "channel", true
		default:
			return "", "", false
		}
	})
	app.SetWorkspaceService(core.NewWorkspaceService(func(teamID string) core.Msg {
		switch teamID {
		case "T2":
			return WorkspaceSwitchedMsg{
				TeamID: "T2",
				Domain: "beta",
				Channels: []sidebar.ChannelItem{
					{ID: "CTWOONLY", Name: "only-beta", Type: "channel"},
				},
			}
		case "T3":
			return WorkspaceSwitchedMsg{
				TeamID: "T3",
				Domain: "gamma",
				Channels: []sidebar.ChannelItem{
					{ID: "CTHREE01", Name: "only-gamma", Type: "channel"},
				},
			}
		default:
			return nil
		}
	}))

	pasteFinder(t, app, "https://beta.slack.com/archives/CTWOLINK/p1111111111111111")
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	firstMsgs := drainCmd(cmd)
	if len(firstMsgs) != 1 {
		t.Fatalf("first enter produced %#v", firstMsgs)
	}
	first, ok := firstMsgs[0].(WorkspaceSwitchedMsg)
	if !ok || first.TeamID != "T2" {
		t.Fatalf("first switch = %#v", firstMsgs[0])
	}

	app.channelFinder.Open()
	app.SetMode(ModeChannelFinder)
	pasteFinder(t, app, "https://gamma.slack.com/archives/CTHREELK/p1779284733270139")
	_, cmd = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	secondMsgs := drainCmd(cmd)
	if len(secondMsgs) != 1 {
		t.Fatalf("second enter produced %#v", secondMsgs)
	}
	second, ok := secondMsgs[0].(WorkspaceSwitchedMsg)
	if !ok || second.TeamID != "T3" {
		t.Fatalf("second switch = %#v", secondMsgs[0])
	}

	_, cmd = app.Update(first)
	var selected []ChannelSelectedMsg
	for _, m := range drainCmd(cmd) {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			selected = append(selected, cs)
		}
	}
	if len(selected) != 1 || selected[0].ID != "CTWOONLY" {
		t.Fatalf("stale switch selected %+v, want T2's last channel, not the discarded link", selected)
	}
	if app.pendingLinkNav != nil {
		t.Fatalf("stale switch armed a permalink jump: %+v", app.pendingLinkNav)
	}
	if app.pendingFinderPermalink == nil || app.pendingFinderPermalink.teamID != "T3" {
		t.Fatalf("newer link was dropped: %+v", app.pendingFinderPermalink)
	}

	_, cmd = app.Update(second)
	selected = nil
	for _, m := range drainCmd(cmd) {
		if cs, ok := m.(ChannelSelectedMsg); ok {
			selected = append(selected, cs)
		}
	}
	if len(selected) != 1 || selected[0].ID != "CTHREELK" || selected[0].Name != "from-gamma" {
		t.Fatalf("newer switch selected %+v, want CTHREELK", selected)
	}
	if app.pendingLinkNav == nil || string(app.pendingLinkNav.ChannelID) != "CTHREELK" || string(app.pendingLinkNav.MessageTS) != "1779284733.270139" {
		t.Fatalf("pendingLinkNav = %+v", app.pendingLinkNav)
	}
	if *opened != "" {
		t.Fatalf("browser opened %q", *opened)
	}
}

func TestFinderPermalink_UnknownChannelOpensBrowser(t *testing.T) {
	app, opened := finderPermalinkApp(t)
	var switched bool
	app.SetWorkspaceService(core.NewWorkspaceService(func(string) core.Msg {
		switched = true
		return nil
	}))
	const url = "https://myteam.slack.com/archives/CUNKNOWN1/p1779284733270139"
	pasteFinder(t, app, url)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	drainCmd(cmd)

	if *opened != url {
		t.Fatalf("browser opened %q, want %q", *opened, url)
	}
	if switched {
		t.Fatal("unknown channel switched workspace")
	}
	if app.pendingLinkNav != nil {
		t.Fatalf("pending navigation started: %+v", app.pendingLinkNav)
	}
	if app.activeChannelID != "C054JFCBN69" {
		t.Fatalf("active channel = %q, want to stay put", app.activeChannelID)
	}
}

func TestFinderPermalink_UnknownWorkspaceOpensBrowser(t *testing.T) {
	app, opened := finderPermalinkApp(t)
	var switched bool
	app.SetWorkspaceService(core.NewWorkspaceService(func(string) core.Msg {
		switched = true
		return nil
	}))
	const url = "https://nope.slack.com/archives/C054JFCBN69/p1779284733270139"
	pasteFinder(t, app, url)
	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	drainCmd(cmd)

	if *opened != url {
		t.Fatalf("browser opened %q, want %q", *opened, url)
	}
	if switched {
		t.Fatal("unknown workspace switched")
	}
	if app.activeTeamID != "T1" {
		t.Fatalf("active team = %q, want T1", app.activeTeamID)
	}
}
