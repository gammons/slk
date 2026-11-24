package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/wintree"
)

var errAppHomePress = errors.New("invalid_arguments")

// shortAppHomeRefetch keeps the post-press follow-up fetch from making
// the test wait out its real delay.
func shortAppHomeRefetch(t *testing.T) {
	t.Helper()
	old := appHomeRefetchDelay
	appHomeRefetchDelay = time.Millisecond
	t.Cleanup(func() { appHomeRefetchDelay = old })
}

// fakeAppHomeService records app-home fetches and presses and answers
// them with the test's view.
type fakeAppHomeService struct {
	home *core.AppHome

	fetched    []string
	pressed    []core.AppHomeAction
	viewIDs    []string
	serviceIDs []string
}

func (f *fakeAppHomeService) service() core.AppHomeService {
	return core.NewAppHomeService(core.AppHomeServiceFuncs{
		Fetch: func(teamID string, channelID ids.ChannelID) core.Msg {
			f.fetched = append(f.fetched, string(channelID))
			return AppHomeLoadedMsg{TeamID: teamID, ChannelID: string(channelID), Home: f.home}
		},
		Press: func(teamID string, channelID ids.ChannelID, viewID, serviceID string, action core.AppHomeAction) error {
			f.pressed = append(f.pressed, action)
			f.viewIDs = append(f.viewIDs, viewID)
			f.serviceIDs = append(f.serviceIDs, serviceID)
			return nil
		},
	})
}

func appHomeFixture() *core.AppHome {
	return &core.AppHome{
		AppID:     "A1",
		ViewID:    "V1",
		ServiceID: "B1",
		Blocks: []blocks.Block{
			blocks.HeaderBlock{Text: "Your AI Budget"},
			blocks.SectionBlock{Text: "Spent so far: $335"},
			blocks.ActionsBlock{Elements: []blocks.ActionElement{
				{Kind: "button", Label: "Refresh"},
			}},
		},
		Actions: []core.AppHomeAction{
			{ActionID: "home_refresh", BlockID: "b1", Label: "Refresh", Value: "home_refresh"},
			{ActionID: "email_creds", BlockID: "b1", Label: "Email credentials", Value: "email_creds",
				Confirm: &core.AppHomeConfirm{
					Title: "Email credentials?", Text: "A fresh link will be emailed.",
					OKText: "Send email", DismissText: "Cancel",
				}},
		},
	}
}

// feedOne runs a command's messages through Update and executes one more
// level of the commands they produce, discarding those results. One extra
// level is what the channel switch needs (the app-home fetch, then the
// view load); feeding a self-rescheduling tick forever would hang.
func feedOne(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	for _, m := range drainBatch(cmd) {
		if m == nil {
			continue
		}
		_, next := a.Update(m)
		drainBatch(next)
	}
}

// openAppChannel selects an app DM through the real reducer chain.
func openAppChannel(t *testing.T, a *App, id, name string) {
	t.Helper()
	_, cmd := a.Update(ChannelSelectedMsg{ID: id, Name: name, Type: "app"})
	feedOne(t, a, cmd)
}

func TestAppHomeShownWhenAppChannelOpens(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())

	openAppChannel(t, a, "D1", "AI Budget Help")

	if len(fake.fetched) != 1 || fake.fetched[0] != "D1" {
		t.Fatalf("fetches = %v, want one for D1", fake.fetched)
	}
	if a.view != ViewAppHome {
		t.Fatalf("view = %v, want ViewAppHome", a.view)
	}
	if a.appHome.Home() == nil {
		t.Fatal("the fetched home tab was not installed")
	}
	plain := a.View().Content
	for _, want := range []string{"AI Budget Help", "Home", "Messages", "Your AI Budget", "Refresh"} {
		if !strings.Contains(plain, want) {
			t.Errorf("frame missing %q", want)
		}
	}
}

func TestAppHomeTogglesWithMessages(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	pressKey(t, a, keyPress('t'))
	if a.view != ViewChannels {
		t.Fatalf("after t: view = %v, want ViewChannels", a.view)
	}
	pressKey(t, a, keyPress('t'))
	if a.view != ViewAppHome {
		t.Fatalf("after second t: view = %v, want ViewAppHome", a.view)
	}
}

func TestAppHomeStaysOnMessagesWithoutATab(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{} // no home
	a.SetAppHomeService(fake.service())

	openAppChannel(t, a, "D1", "Some Bot")

	if a.view != ViewChannels {
		t.Errorf("view = %v, want ViewChannels for an app without a home tab", a.view)
	}
	if a.appHome.Home() != nil {
		t.Error("a home tab was installed from a nil result")
	}
}

func TestAppHomeLeaveChannelDropsTheTab(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	_, cmd := a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	feedOne(t, a, cmd)

	if a.view != ViewChannels {
		t.Errorf("view = %v, want ViewChannels after picking a channel", a.view)
	}
	if a.appHome.Home() != nil {
		t.Error("the previous app's home tab survived the channel switch")
	}
	if a.appHomeChannelID != "" {
		t.Errorf("appHomeChannelID = %q, want empty", a.appHomeChannelID)
	}
}

func TestAppHomeButtonsOpenAndPress(t *testing.T) {
	shortAppHomeRefetch(t)
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	pressKey(t, a, keyPress('b'))
	if a.mode != ModeConfirm || !dialogShows(a, "[1] Refresh") || !dialogShows(a, "[2] Email credentials") {
		t.Fatalf("mode = %v; the button chooser should open with both buttons", a.mode)
	}

	pressKey(t, a, keyPress('1'))
	if len(fake.pressed) != 1 {
		t.Fatalf("presses = %d, want 1", len(fake.pressed))
	}
	if got := fake.pressed[0].ActionID; got != "home_refresh" {
		t.Errorf("pressed action = %q, want home_refresh", got)
	}
	if got := fake.viewIDs[0]; got != "V1" {
		t.Errorf("view id = %q, want V1", got)
	}
	if got := fake.serviceIDs[0]; got != "B1" {
		t.Errorf("service id = %q, want B1", got)
	}
	// A successful press refetches the view the app re-published.
	if len(fake.fetched) != 2 {
		t.Errorf("fetches = %d, want the initial load plus a refetch", len(fake.fetched))
	}
	if a.view != ViewAppHome {
		t.Errorf("view = %v, want the refetched home tab to stay up", a.view)
	}
}

func TestAppHomeConfirmingButtonNeedsConfirmation(t *testing.T) {
	shortAppHomeRefetch(t)
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	pressKey(t, a, keyPress('b'))
	pressKey(t, a, keyPress('2')) // Email credentials
	if !dialogShows(a, "Email credentials?") || !dialogShows(a, "[2] Send email") {
		t.Fatalf("the confirmation step did not open (mode %v)", a.mode)
	}
	if len(fake.pressed) != 0 {
		t.Fatal("the button was pressed before the confirmation")
	}
	pressKey(t, a, keyPress('2')) // Send email
	if len(fake.pressed) != 1 || fake.pressed[0].ActionID != "email_creds" {
		t.Fatalf("pressed = %v, want one email_creds press", fake.pressed)
	}
}

func TestAppHomePressFailureToasts(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	// Feed the failure arm directly: the real press command would also
	// sleep out the toast's clear timer.
	a.Update(appHomeActionPressedMsg{
		teamID: "T1", channelID: "D1", label: "Refresh", err: errAppHomePress,
	})
	if got := statusbarText(a); !strings.Contains(got, "Could not press Refresh") {
		t.Errorf("status bar = %q, want a press failure toast", got)
	}
	if len(fake.fetched) != 1 {
		t.Errorf("fetches = %d, want no refetch after a failed press", len(fake.fetched))
	}
}

func TestAppHomeIgnoresMessageOperations(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withMessages(testMessageItems(3)...))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	a.Update(keyPress('m'))
	if a.pendingMark {
		t.Error("m started a mark chord on the hidden message list")
	}
	a.Update(keyPress('i'))
	if got := statusbarText(a); !strings.Contains(got, "Press t for the messages tab") {
		t.Errorf("status bar = %q, want the messages-tab hint", got)
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want insert mode refused", a.mode)
	}
}

func TestAppHomeRefusesInsertFromSidebar(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	a.focusedPanel = PanelSidebar
	a.Update(keyPress('i'))
	if a.mode != ModeNormal {
		t.Errorf("mode = %v, insert mode must not start for a hidden composer", a.mode)
	}
	if got := statusbarText(a); !strings.Contains(got, "Press t for the messages tab") {
		t.Errorf("status bar = %q, want the messages-tab hint", got)
	}
}

func TestAppHomeClearsReactionNavigation(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.activeChannelID = "D1"
	a.appHomeChannelID = "D1"
	a.appHomeAutoShow = true
	a.messagepane.EnterReactionNav()

	a.Update(AppHomeLoadedMsg{TeamID: "T1", ChannelID: "D1", Home: appHomeFixture()})

	if a.view != ViewAppHome {
		t.Fatalf("view = %v, want ViewAppHome", a.view)
	}
	if a.messagepane.ReactionNavActive() {
		t.Error("reaction navigation stayed active on the hidden message list")
	}
}

func TestAppHomeDoesNotHideALinkedMessage(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	a.pendingLinkNav = &pendingLinkNav{Location: Location{
		TeamID: "T1", ChannelID: "D1", MessageTS: "1.0",
	}}

	openAppChannel(t, a, "D1", "AI Budget Help")

	if a.view != ViewChannels {
		t.Errorf("view = %v, want the linked message on screen", a.view)
	}
	if a.appHome.Home() == nil {
		t.Fatal("the tab should still be fetched for t")
	}
	pressKey(t, a, keyPress('t'))
	if a.view != ViewAppHome {
		t.Errorf("view = %v, want t to reach the fetched tab", a.view)
	}
}

func TestAppHomeRefetchAfterPress(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	_, cmd := a.Update(appHomeRefetchMsg{teamID: "T1", channelID: "D1"})
	feedOne(t, a, cmd)

	if len(fake.fetched) != 2 {
		t.Errorf("fetches = %d, want the channel open plus the follow-up", len(fake.fetched))
	}
}

func TestAppHomeFollowsWindowFocus(t *testing.T) {
	a := newWideTestApp(t)
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())

	_, cmd := a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	feedOne(t, a, cmd)
	first := a.focusedWin
	_ = a.splitWindow(wintree.SplitSideBySide)
	openAppChannel(t, a, "D1", "AI Budget Help")
	second := a.focusedWin
	if a.view != ViewAppHome {
		t.Fatalf("precondition: view = %v, want ViewAppHome", a.view)
	}

	feedOne(t, a, a.focusWindow(first))
	if a.view != ViewChannels {
		t.Errorf("view = %v, want ViewChannels after focusing another window's channel", a.view)
	}
	if a.appHome.Home() != nil {
		t.Error("the app's Home tab stayed installed after the window focus moved away")
	}

	feedOne(t, a, a.focusWindow(second))
	if a.view != ViewAppHome {
		t.Errorf("view = %v, want the app window's Home tab after focusing it back", a.view)
	}
	if a.appHome.Home() == nil {
		t.Error("the app window's Home tab was not loaded on focus")
	}
}

func TestAppHomeRenderContextDisablesImages(t *testing.T) {
	a := newTestApp(t)
	ctx := a.appHomeRenderContext()
	if ctx.Fetcher != nil {
		t.Error("the Home tab's render context carries an image fetcher")
	}
	if ctx.Protocol != imgpkg.ProtoOff {
		t.Errorf("protocol = %v, want image rendering off", ctx.Protocol)
	}
	if ctx.RenderText == nil || ctx.WrapText == nil {
		t.Error("the Home tab lost mention rendering or wrapping")
	}
}

func TestAppHomeFetchErrorKeepsTheTab(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")

	a.Update(AppHomeLoadedMsg{TeamID: "T1", ChannelID: "D1", Err: errAppHomePress})

	if a.appHome.Home() == nil {
		t.Fatal("a failed refetch removed the working Home tab")
	}
	if a.appHomeChannelID != "D1" {
		t.Errorf("appHomeChannelID = %q, want D1", a.appHomeChannelID)
	}
	if a.view != ViewAppHome {
		t.Errorf("view = %v, want the tab still on screen", a.view)
	}
}

func TestAppHomeKeepsNewMessagesUnread(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	fake := &fakeAppHomeService{home: appHomeFixture()}
	a.SetAppHomeService(fake.service())
	openAppChannel(t, a, "D1", "AI Budget Help")
	a.terminalFocused = true

	a.Update(NewMessageMsg{ChannelID: "D1", Message: messages.MessageItem{
		TS: "9.9", UserID: "U1", Text: "hello",
	}})

	if a.pendingChannelMark.ts != "" {
		t.Errorf("a message in the hidden conversation staged a read mark at %q", a.pendingChannelMark.ts)
	}
}
