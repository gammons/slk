package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
)

var errAppViewPress = errors.New("invalid_arguments")

// fakeAppViewService records modal fetches and presses and answers them
// with the test's view.
type fakeAppViewService struct {
	view     *core.AppView
	fetchErr error

	fetched    []string
	pressed    []core.AppHomeAction
	viewIDs    []string
	serviceIDs []string
}

func (f *fakeAppViewService) service() core.AppViewService {
	return core.NewAppViewService(core.AppViewServiceFuncs{
		Fetch: func(teamID, viewID string) core.Msg {
			f.fetched = append(f.fetched, viewID)
			if f.fetchErr != nil {
				return AppViewLoadedMsg{TeamID: teamID, ViewID: viewID, Err: f.fetchErr}
			}
			return AppViewLoadedMsg{TeamID: teamID, ViewID: viewID, View: f.view}
		},
		Press: func(teamID, viewID, serviceID string, action core.AppHomeAction) error {
			f.pressed = append(f.pressed, action)
			f.viewIDs = append(f.viewIDs, viewID)
			f.serviceIDs = append(f.serviceIDs, serviceID)
			return nil
		},
	})
}

func appViewFixture() *core.AppView {
	return &core.AppView{
		ViewID:     "V1",
		TeamID:     "T1",
		AppID:      "A1",
		ServiceID:  "B1",
		Title:      "AI Budget Support",
		CloseLabel: "Close",
		Blocks: []blocks.Block{
			blocks.HeaderBlock{Text: "Your AI Budget"},
			blocks.SectionBlock{Text: "Budget: $400"},
			blocks.ActionsBlock{BlockID: "b1", Elements: []blocks.ActionElement{
				{Kind: "button", Label: "Request budget increase", ActionID: "req_open", Value: "req_open"},
			}},
		},
		Actions: []core.AppHomeAction{
			{ActionID: "req_open", BlockID: "b1", Label: "Request budget increase", Value: "req_open"},
		},
	}
}

func TestAppViewOpenedShowsTheDialog(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	if a.mode != ModeAppView {
		t.Fatalf("mode = %v, want ModeAppView", a.mode)
	}
	plain := ansi.Strip(a.View().Content)
	for _, want := range []string{"AI Budget Support", "Your AI Budget", "Budget: $400", "b buttons", "esc close"} {
		if !strings.Contains(plain, want) {
			t.Errorf("frame missing %q", want)
		}
	}
}

func TestAppViewOpened_OtherWorkspaceIgnored(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.Update(AppViewOpenedMsg{TeamID: "T2", View: appViewFixture()})
	if a.mode != ModeNormal || a.appView.Current() != nil {
		t.Fatalf("mode = %v view = %v, want the dialog ignored", a.mode, a.appView.Current())
	}
}

func TestAppViewEscapeDismisses(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	a.Update(keyCode(tea.KeyEscape))

	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal", a.mode)
	}
	if a.appView.Current() != nil {
		t.Error("the dialog was not cleared")
	}
	if strings.Contains(ansi.Strip(a.View().Content), "AI Budget Support") {
		t.Error("the dialog is still drawn")
	}
}

func TestAppViewScrolling(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(80, 20))
	var all []blocks.Block
	for i := 1; i <= 40; i++ {
		all = append(all, blocks.SectionBlock{Text: "row " + strconv.Itoa(i)})
	}
	v := appViewFixture()
	v.Blocks = all
	a.appView.SetView(v)
	a.SetMode(ModeAppView)
	a.View() // size the dialog

	a.Update(keyPress('j'))
	afterJ := ansi.Strip(a.View().Content)
	if !strings.Contains(afterJ, "row 2") {
		t.Errorf("j did not scroll the dialog; frame:\n%s", afterJ)
	}
	a.Update(keyPress('G'))
	if strings.Contains(ansi.Strip(a.View().Content), "row 1") {
		t.Error("G did not scroll to the end")
	}
	a.Update(keyPress('g'))
	if got := ansi.Strip(a.View().Content); !strings.Contains(got, "row 1") {
		t.Errorf("g did not scroll back to the start; frame:\n%s", got)
	}
}

// Slack normally sends view_updated with the whole view; when it sends
// view:null the App asks the service for it.
func TestAppViewUpdatedWithoutAViewIsFetched(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	fake := &fakeAppViewService{view: appViewFixture()}
	a.SetAppViewService(fake.service())
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	_, cmd := a.Update(AppViewNeedsRefreshMsg{TeamID: "T1", ViewID: "V1"})
	if cmd == nil {
		t.Fatal("no fetch was started")
	}
	a.Update(cmd())
	if len(fake.fetched) != 1 || fake.fetched[0] != "V1" {
		t.Fatalf("fetches = %v, want one for V1", fake.fetched)
	}
	if a.appView.Current() == nil || a.appView.Current().ViewID != "V1" {
		t.Error("the fetched view was not installed")
	}
}

func TestAppViewUpdatedReplacesTheContent(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	next := appViewFixture()
	next.Blocks = []blocks.Block{blocks.SectionBlock{Text: "Request submitted"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: next})
	plain := ansi.Strip(a.View().Content)
	if !strings.Contains(plain, "Request submitted") {
		t.Error("the updated content is not drawn")
	}
}

// A fetch result for a view the user dismissed must not reopen the
// dialog.
func TestAppViewLoadAfterDismissIsIgnored(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	a.Update(keyCode(tea.KeyEscape))

	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: appViewFixture()})
	if a.mode != ModeNormal || a.appView.Current() != nil {
		t.Errorf("mode = %v view = %v, want the result ignored", a.mode, a.appView.Current())
	}
}

func TestAppViewButtonChooserPresses(t *testing.T) {
	shortAppViewRefetch(t)
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	fake := &fakeAppViewService{view: appViewFixture()}
	a.SetAppViewService(fake.service())
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	a.Update(keyPress('b'))
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want ModeConfirm for the chooser", a.mode)
	}
	if !strings.Contains(ansi.Strip(a.View().Content), "Request budget increase") {
		t.Fatal("the chooser does not list the dialog's button")
	}
	if !strings.Contains(ansi.Strip(a.View().Content), "AI Budget S") {
		t.Errorf("the dialog is not drawn behind the chooser; frame:\n%s", ansi.Strip(a.View().Content))
	}

	pressKey(t, a, keyPress('1'))
	if len(fake.pressed) != 1 || fake.pressed[0].ActionID != "req_open" {
		t.Fatalf("presses = %v, want one for req_open", fake.pressed)
	}
	if fake.viewIDs[0] != "V1" || fake.serviceIDs[0] != "B1" {
		t.Errorf("press named view %q service %q, want V1 B1", fake.viewIDs[0], fake.serviceIDs[0])
	}
	if a.mode != ModeAppView {
		t.Errorf("mode = %v after the chooser, want ModeAppView", a.mode)
	}
}

func TestAppViewButtonPressErrorToasts(t *testing.T) {
	shortAppViewRefetch(t)
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.SetAppViewService(core.NewAppViewService(core.AppViewServiceFuncs{
		Press: func(teamID, viewID, serviceID string, action core.AppHomeAction) error {
			return errAppViewPress
		},
	}))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	pressKey(t, a, keyPress('b'))
	pressKey(t, a, keyPress('1'))
	if got := statusbarText(a); !strings.Contains(got, "Request budget increase") {
		t.Errorf("toast = %q, want one naming the button", got)
	}
}

func TestAppViewButtonClickPresses(t *testing.T) {
	shortAppViewRefetch(t)
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	fake := &fakeAppViewService{view: appViewFixture()}
	a.SetAppViewService(fake.service())
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	// The dialog draws its buttons as "[ label ]"; click the label.
	row, col := -1, -1
	for y, line := range strings.Split(ansi.Strip(a.View().Content), "\n") {
		if i := strings.Index(line, "Request budget increase"); i >= 0 {
			row, col = y, ansi.StringWidth(line[:i])
			break
		}
	}
	if row < 0 {
		t.Fatal("the dialog does not draw its button")
	}

	_, cmd := a.Update(tea.MouseClickMsg{X: col, Y: row, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("the click did not press anything")
	}
	a.Update(cmd())
	if len(fake.pressed) != 1 || fake.pressed[0].ActionID != "req_open" {
		t.Fatalf("presses = %v, want one for req_open", fake.pressed)
	}
	if a.mode != ModeAppView {
		t.Errorf("mode = %v after the click, want ModeAppView", a.mode)
	}
}

func TestAppViewOutsideClickDismisses(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	a.View()

	a.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if a.mode != ModeNormal || a.appView.Current() != nil {
		t.Errorf("mode = %v view = %v, want the outside click to dismiss", a.mode, a.appView.Current())
	}
}

// An inline update refreshes the dialog but must not reopen one the
// user has dismissed.
func TestAppViewInlineUpdateAfterDismissIsIgnored(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	a.Update(keyCode(tea.KeyEscape))

	next := appViewFixture()
	next.Blocks = []blocks.Block{blocks.SectionBlock{Text: "Request submitted"}}
	a.Update(AppViewUpdatedMsg{TeamID: "T1", View: next})
	if a.mode != ModeNormal || a.appView.Current() != nil {
		t.Errorf("mode = %v view = %v, want the update ignored", a.mode, a.appView.Current())
	}
}

func TestAppViewInlineUpdateReplacesContent(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	next := appViewFixture()
	next.Blocks = []blocks.Block{blocks.SectionBlock{Text: "Request submitted"}}
	a.Update(AppViewUpdatedMsg{TeamID: "T1", View: next})
	plain := ansi.Strip(a.View().Content)
	if !strings.Contains(plain, "Request submitted") {
		t.Error("the updated content is not drawn")
	}
}

// A view update while the button chooser is open takes the dialog (and
// the keyboard) back instead of leaving the chooser drawn with dead
// keys.
func TestAppViewInlineUpdateTakesBackTheChooser(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	a.Update(keyPress('b'))
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want the chooser", a.mode)
	}

	next := appViewFixture()
	next.Blocks = []blocks.Block{blocks.SectionBlock{Text: "Request submitted"}}
	a.Update(AppViewUpdatedMsg{TeamID: "T1", View: next})
	if a.mode != ModeAppView {
		t.Errorf("mode = %v, want ModeAppView", a.mode)
	}
	if a.confirmPrompt.IsVisible() {
		t.Error("the chooser is still visible")
	}
	if strings.Contains(ansi.Strip(a.View().Content), "Dialog buttons") {
		t.Error("the chooser is still drawn")
	}
}

// A fetch that started before a newer inline update must not overwrite
// the newer content when it lands.
func TestAppViewStaleFetchIsIgnored(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})

	next := appViewFixture()
	next.Blocks = []blocks.Block{blocks.SectionBlock{Text: "fresh content"}}
	a.Update(AppViewUpdatedMsg{TeamID: "T1", View: next})

	stale := appViewFixture()
	stale.Blocks = []blocks.Block{blocks.SectionBlock{Text: "stale content"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: stale, Gen: 1})
	plain := ansi.Strip(a.View().Content)
	if !strings.Contains(plain, "fresh content") || strings.Contains(plain, "stale content") {
		t.Errorf("the stale fetch replaced newer content; frame:\n%s", plain)
	}
}

// Slack's confirmation step runs before the link opens: a URL button
// with a confirm must ask first, and its accept opens the link.
func TestAppViewConfirmsURLButtons(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	fake := &fakeAppViewService{view: appViewFixture()}
	a.SetAppViewService(fake.service())
	act := core.AppHomeAction{
		ActionID: "docs", BlockID: "b1", Label: "Docs", URL: "https://example.com",
		Confirm: &core.AppHomeConfirm{Title: "Open docs?", Text: "Leaves slk.", OKText: "Open", DismissText: "Cancel"},
	}
	a.appView.SetView(&core.AppView{ViewID: "V1", Title: "Dialog", Actions: []core.AppHomeAction{act}})
	a.SetMode(ModeAppView)

	if cmd := a.clickAppViewAction(0); cmd != nil {
		t.Fatal("the click opened the link without asking")
	}
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want the confirmation prompt", a.mode)
	}
	choice := a.appViewConfirmChoice(act, nil)
	msg := choice.Buttons[1].Action()
	if _, ok := msg.(OpenLinkMsg); !ok {
		t.Errorf("confirmation accept returned %T, want OpenLinkMsg", msg)
	}
	if len(fake.pressed) != 0 {
		t.Errorf("pressed %v, want the link opened instead", fake.pressed)
	}
}

// Two fetches may start at the same inline generation. The first result
// must not cancel the second: they are ordered by their own sequence,
// and the later-started fetch wins.
func TestAppViewFetchesAtTheSameGenerationBothApply(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	gen := a.appViewGen

	first := appViewFixture()
	first.Blocks = []blocks.Block{blocks.SectionBlock{Text: "loading"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: first, Gen: gen, Fetch: 1})

	second := appViewFixture()
	second.Blocks = []blocks.Block{blocks.SectionBlock{Text: "done"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: second, Gen: gen, Fetch: 2})
	if plain := ansi.Strip(a.View().Content); !strings.Contains(plain, "done") {
		t.Errorf("the later fetch was dropped; frame:\n%s", plain)
	}
}

func TestAppViewOlderFetchAfterANewerOneIsDropped(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	gen := a.appViewGen

	newer := appViewFixture()
	newer.Blocks = []blocks.Block{blocks.SectionBlock{Text: "newer"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: newer, Gen: gen, Fetch: 2})

	older := appViewFixture()
	older.Blocks = []blocks.Block{blocks.SectionBlock{Text: "older"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: older, Gen: gen, Fetch: 1})
	plain := ansi.Strip(a.View().Content)
	if !strings.Contains(plain, "newer") || strings.Contains(plain, "older") {
		t.Errorf("an older fetch overwrote a newer one; frame:\n%s", plain)
	}
}

// Fetched content replaces the dialog under an open chooser: the
// chooser's actions belong to the old content and must not stay
// pressable.
func TestAppViewFetchedContentClosesTheChooser(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	a.Update(keyPress('b'))
	if a.mode != ModeConfirm {
		t.Fatalf("mode = %v, want the chooser", a.mode)
	}

	next := appViewFixture()
	next.Blocks = []blocks.Block{blocks.SectionBlock{Text: "from views.get"}}
	a.Update(AppViewLoadedMsg{TeamID: "T1", ViewID: "V1", View: next, Gen: a.appViewGen, Fetch: 1})
	if a.mode != ModeAppView || a.confirmPrompt.IsVisible() {
		t.Errorf("mode = %v prompt visible = %v, want the dialog back", a.mode, a.confirmPrompt.IsVisible())
	}
	if plain := ansi.Strip(a.View().Content); !strings.Contains(plain, "from views.get") {
		t.Errorf("the fetched content is not drawn; frame:\n%s", plain)
	}
}

// A view_opened arriving while a confirm prompt is up (the quit prompt,
// say) must close the prompt: it would otherwise stay drawn with the
// keys routed to the dialog underneath.
func TestAppViewOpenedClosesAnExistingPrompt(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.openQuitConfirm()
	if !a.confirmPrompt.IsVisible() {
		t.Fatal("precondition: the quit prompt did not open")
	}

	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	if a.confirmPrompt.IsVisible() {
		t.Error("the prompt is still visible over the dialog")
	}
	if a.mode != ModeAppView {
		t.Errorf("mode = %v, want ModeAppView", a.mode)
	}
	if strings.Contains(ansi.Strip(a.View().Content), "Quit slk?") {
		t.Error("the prompt is still drawn")
	}
}

// The prompt closed for the dialog must also be forgotten as the
// ephemeral's action dialog: otherwise deleting that message would
// find the app's own chooser later and close it.
func TestAppViewOpenedForgetsTheInterruptedPrompt(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"), withSize(100, 30))
	a.openQuitConfirm()
	a.shownPrompt = &actionPromptRef{channelID: "C1", ts: "1.0"}
	a.shownPromptInsert = true

	a.Update(AppViewOpenedMsg{TeamID: "T1", View: appViewFixture()})
	if a.shownPrompt != nil || a.shownPromptInsert {
		t.Errorf("shownPrompt = %+v insert = %v, want the old prompt forgotten",
			a.shownPrompt, a.shownPromptInsert)
	}
}

// shortAppViewRefetch keeps the post-press follow-up fetch from making
// the test wait out its real delay.
func shortAppViewRefetch(t *testing.T) {
	t.Helper()
	old := appViewRefreshDelay
	appViewRefreshDelay = time.Millisecond
	t.Cleanup(func() { appViewRefreshDelay = old })
}
