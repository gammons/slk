package ui

import (
	"bytes"
	goimage "image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/export"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/workspace"
)

// Regression net for the App's collaborator seams: every place the TUI
// hands work to the rest of the application. Each test drives the App the
// way a user would and pins what crosses the seam and what the App does
// with the answer. Wiring lives in seams_wiring_test.go.

type seamStatusCall struct {
	action presencemenu.Action
	mins   int
}

func TestSeam_PresenceMenuSetsStatus(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	var calls []seamStatusCall
	wireStatusSetter(a, func(action presencemenu.Action, mins int) {
		calls = append(calls, seamStatusCall{action, mins})
	})
	pres, dnd, end, _ := a.presence.Status(a.activeTeamID)
	a.presenceMenu.OpenWith(a.workspaceNameForActive(), pres, dnd, end)
	a.SetMode(ModePresenceMenu)

	for _, r := range "away" {
		_ = dispatchModeKey(a, keyPress(r))
	}
	_ = dispatchModeKey(a, keyCode(tea.KeyEnter))

	want := []seamStatusCall{{presencemenu.ActionSetAway, 0}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("status calls = %+v, want %+v", calls, want)
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v, want ModeNormal", a.mode)
	}
}

func TestSeam_CustomSnoozeSetsStatus(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	var calls []seamStatusCall
	wireStatusSetter(a, func(action presencemenu.Action, mins int) {
		calls = append(calls, seamStatusCall{action, mins})
	})
	a.presence.ClearSnoozeBuf()
	a.SetMode(ModePresenceCustomSnooze)

	for _, r := range "45" {
		_ = dispatchModeKey(a, keyPress(r))
	}
	_ = dispatchModeKey(a, keyCode(tea.KeyEnter))

	want := []seamStatusCall{{presencemenu.ActionSnooze, 45}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("status calls = %+v, want %+v", calls, want)
	}
}

func TestSeam_StatusUnwiredStillAppliesLocally(t *testing.T) {
	a := newTestApp(t, withActiveTeam("T1"))
	pres, dnd, end, _ := a.presence.Status(a.activeTeamID)
	a.presenceMenu.OpenWith(a.workspaceNameForActive(), pres, dnd, end)
	a.SetMode(ModePresenceMenu)

	for _, r := range "away" {
		_ = dispatchModeKey(a, keyPress(r))
	}
	_ = dispatchModeKey(a, keyCode(tea.KeyEnter))

	if got, _, _, _ := a.presence.Status("T1"); got != "away" {
		t.Errorf("local presence = %q, want away", got)
	}
}

func TestSeam_TypingSendsForActiveChannel(t *testing.T) {
	a := newTestApp(t, withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}), withActiveChannel("C1"))
	a.SetTypingEnabled(true)
	sent := make(chan string, 4)
	wireTypingSender(a, func(channelID string) { sent <- channelID })
	a.SetMode(ModeInsert)
	a.focusedPanel = PanelMessages
	_ = a.compose.Focus()

	_ = dispatchModeKey(a, keyPress('h'))

	select {
	case got := <-sent:
		if got != "C1" {
			t.Errorf("typing sent for %q, want C1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no typing event sent")
	}
}

func TestSeam_ThemeSwitcherSavesChoice(t *testing.T) {
	t.Cleanup(func() { styles.Apply("dark", config.Theme{}) })
	a := newTestApp(t)
	type save struct {
		name  string
		scope themeswitcher.ThemeScope
	}
	var saves []save
	wireThemeSaver(a, func(name string, scope themeswitcher.ThemeScope) {
		saves = append(saves, save{name, scope})
	})
	a.SetThemeItems([]string{"dracula", "nord"})
	a.themeSwitcher.OpenWithScope(themeswitcher.ScopeWorkspace, "")
	a.SetMode(ModeThemeSwitcher)

	_ = dispatchModeKey(a, keyCode(tea.KeyEnter))

	want := []save{{"dracula", themeswitcher.ScopeWorkspace}}
	if !reflect.DeepEqual(saves, want) {
		t.Errorf("theme saves = %+v, want %+v", saves, want)
	}
}

func TestSeam_SidebarResizeSavesWidth(t *testing.T) {
	a := newTestApp(t)
	var widths []int
	wireWidthSaver(a, func(w int) { widths = append(widths, w) })

	_ = dispatchModeKey(a, keyPress(']'))
	grown := a.sidebar.Width()
	_ = dispatchModeKey(a, keyPress('['))
	shrunk := a.sidebar.Width()

	if want := []int{grown, shrunk}; !reflect.DeepEqual(widths, want) {
		t.Errorf("saved widths = %v, want %v", widths, want)
	}
	if grown <= shrunk {
		t.Errorf("] then [ gave widths %d then %d; want a grow then a shrink", grown, shrunk)
	}
}

type seamUpload struct {
	channelID, threadTS, caption string
	atts                         []compose.PendingAttachment
}

type seamUploadDone struct{}

func TestSeam_UploadFromChannelCompose(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	var got []seamUpload
	wireUploader(a, func(channelID, threadTS, caption string, atts []compose.PendingAttachment) tea.Cmd {
		got = append(got, seamUpload{channelID, threadTS, caption, atts})
		return func() tea.Msg { return seamUploadDone{} }
	})
	a.focusedPanel = PanelMessages
	a.SetMode(ModeInsert)
	att := compose.PendingAttachment{Filename: "a.png", Bytes: []byte("png"), Mime: "image/png", Size: 3}
	a.compose.AddAttachment(att)
	a.compose.SetValue("look")

	cmd := a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	want := []seamUpload{{"C1", "", "look", []compose.PendingAttachment{att}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uploads = %+v, want %+v", got, want)
	}
	if !containsMsg(drainSkippingTimers(cmd), seamUploadDone{}) {
		t.Error("the uploader's cmd was not part of the returned batch")
	}
}

// drainSkippingTimers is drainCmd for batches that carry a long
// tea.Tick: any cmd still blocked after a moment is skipped.
func drainSkippingTimers(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(200 * time.Millisecond):
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainSkippingTimers(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func TestSeam_UploadFromThreadCompose(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	var got []seamUpload
	wireUploader(a, func(channelID, threadTS, caption string, atts []compose.PendingAttachment) tea.Cmd {
		got = append(got, seamUpload{channelID, threadTS, caption, atts})
		return nil
	})
	a.threadPanel.SetThread(messages.MessageItem{TS: "P1"}, nil, "C1", "P1")
	a.threadVisible = true
	a.focusedPanel = PanelThread
	a.SetMode(ModeInsert)
	att := compose.PendingAttachment{Filename: "b.txt", Path: "/tmp/b.txt", Size: 5}
	a.threadCompose.AddAttachment(att)

	_ = a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})

	want := []seamUpload{{"C1", "P1", "", []compose.PendingAttachment{att}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uploads = %+v, want %+v", got, want)
	}
}

func TestSeam_UploadUnwiredToasts(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	a.focusedPanel = PanelMessages
	a.SetMode(ModeInsert)
	a.compose.AddAttachment(compose.PendingAttachment{Filename: "a.png", Bytes: []byte("png"), Size: 3})

	cmd := a.handleInsertMode(tea.KeyPressMsg{Code: tea.KeyEnter})
	firstBatchCmd(t, cmd)

	if got := statusbarText(a); !strings.Contains(got, "Cannot upload: no active channel") {
		t.Errorf("status bar = %q, want the cannot-upload toast", got)
	}
	if a.compose.Uploading() {
		t.Error("compose marked uploading without an uploader")
	}
}

type seamSwitched struct{ teamID string }

func TestSeam_NumberKeySwitchesWorkspace(t *testing.T) {
	a := newTestApp(t, withWorkspaces(
		workspace.WorkspaceItem{ID: "T1", Name: "One", Initials: "ON"},
		workspace.WorkspaceItem{ID: "T2", Name: "Two", Initials: "TW"},
	))
	a.workspaceRail.SelectByID("T1")
	var calls []string
	wireWorkspaceSwitcher(a, func(teamID string) tea.Msg {
		calls = append(calls, teamID)
		return seamSwitched{teamID}
	})

	if cmd := dispatchModeKey(a, keyPress('1')); cmd != nil {
		t.Errorf("switching to the current workspace returned a cmd")
	}
	cmd := dispatchModeKey(a, keyPress('2'))
	if cmd == nil {
		t.Fatal("2 returned no cmd")
	}
	if len(calls) != 0 {
		t.Errorf("switcher ran before its cmd did: %v", calls)
	}
	if got := cmd(); got != (seamSwitched{"T2"}) {
		t.Errorf("cmd() = %#v, want the switcher's msg for T2", got)
	}
	if !reflect.DeepEqual(calls, []string{"T2"}) {
		t.Errorf("switcher calls = %v, want [T2]", calls)
	}
}

func TestSeam_WorkspaceSwitchUnwiredIsInert(t *testing.T) {
	a := newTestApp(t, withWorkspaces(
		workspace.WorkspaceItem{ID: "T1", Name: "One", Initials: "ON"},
		workspace.WorkspaceItem{ID: "T2", Name: "Two", Initials: "TW"},
	))
	a.workspaceRail.SelectByID("T1")

	if cmd := dispatchModeKey(a, keyPress('2')); cmd != nil {
		t.Errorf("2 with no switcher returned a cmd")
	}
}

func TestSeam_UnreadStateFeedsTitleAndStatusReport(t *testing.T) {
	a := newTestApp(t,
		withSize(0, 0),
		withWorkspaces(
			workspace.WorkspaceItem{ID: "T1", Name: "SWAP", Initials: "SW"},
			workspace.WorkspaceItem{ID: "T2", Name: "Other", Initials: "OT"},
		),
		withChannels(
			sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"},
			sidebar.ChannelItem{ID: "C2", Name: "noisy", Type: "channel", IsMuted: true},
			sidebar.ChannelItem{ID: "C3", Name: "design", Type: "channel"},
		),
	)
	wireUnreadReaders(a,
		func() map[string]cache.ReadState {
			return map[string]cache.ReadState{
				"C1": {HasUnread: true},
				"C2": {HasUnread: true},
				"C3": {HasUnread: false},
			}
		},
		func() []string { return []string{"T1", "T2"} },
	)
	type report struct {
		unread, other int
		ws, title     string
	}
	var reports []report
	wireStatusReporter(a, func(unread, other int, ws, title string) {
		reports = append(reports, report{unread, other, ws, title})
	})
	a.activeTeamID = "T1"

	a.notifyReadStateChanged()

	want := []report{{1, 1, "SWAP", "slk SW (1) +1"}}
	if !reflect.DeepEqual(reports, want) {
		t.Errorf("status reports = %+v, want %+v", reports, want)
	}
	if a.windowTitle != "slk SW (1) +1" {
		t.Errorf("window title = %q", a.windowTitle)
	}
}

func TestSeam_UnreadStateUnwired(t *testing.T) {
	a := newTestApp(t,
		withSize(0, 0),
		withWorkspaces(workspace.WorkspaceItem{ID: "T1", Name: "SWAP", Initials: "SW"}),
		withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}),
	)
	a.activeTeamID = "T1"

	a.notifyReadStateChanged()

	if a.windowTitle != "slk SW" {
		t.Errorf("window title = %q, want %q", a.windowTitle, "slk SW")
	}
}

func TestSeam_AvatarsAskedForEachSender(t *testing.T) {
	a := newTestApp(t, withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}), withActiveChannel("C1"),
		withMessages(
			messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "hi", Timestamp: "1:00 PM"},
			messages.MessageItem{TS: "2.0", UserID: "U2", UserName: "bob", Text: "yo", Timestamp: "1:01 PM"},
		))
	asked := map[string]bool{}
	wireAvatars(a, func(userID string) string {
		asked[userID] = true
		return ""
	})

	_ = a.View()

	if !asked["U1"] || !asked["U2"] {
		t.Errorf("avatars requested for %v, want U1 and U2", asked)
	}
}

// pasteInto runs ctrl+v in insert mode against the channel compose.
func pasteInto(t *testing.T, a *App) {
	t.Helper()
	a.focusedPanel = PanelMessages
	a.SetMode(ModeInsert)
	_ = a.compose.Focus()
	_ = a.handleInsertMode(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
}

func TestSeam_PasteText(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	wireClipboard(a, "hello there", nil)

	pasteInto(t, a)

	if got := a.compose.Value(); got != "hello there" {
		t.Errorf("compose = %q, want the clipboard text", got)
	}
	if n := len(a.compose.Attachments()); n != 0 {
		t.Errorf("attachments = %d, want 0", n)
	}
}

func TestSeam_PasteImage(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	img := []byte("\x89PNG fake")
	wireClipboard(a, "", img)

	pasteInto(t, a)

	atts := a.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("attachments = %d, want 1", len(atts))
	}
	if !bytes.Equal(atts[0].Bytes, img) || atts[0].Mime != "image/png" || atts[0].Size != int64(len(img)) {
		t.Errorf("attachment = %+v, want the clipboard PNG", atts[0])
	}
	if !strings.HasPrefix(atts[0].Filename, "slk-paste-") {
		t.Errorf("filename = %q", atts[0].Filename)
	}
}

func TestSeam_PasteFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, withActiveChannel("C1"))
	wireClipboard(a, path, nil)
	wireFilesystem(a)

	pasteInto(t, a)

	atts := a.compose.Attachments()
	if len(atts) != 1 {
		t.Fatalf("attachments = %d, want 1", len(atts))
	}
	if atts[0].Path != path || atts[0].Size != 5 || atts[0].Filename != "notes.txt" {
		t.Errorf("attachment = %+v, want %s (5 bytes)", atts[0], path)
	}
	if a.compose.Value() != "" {
		t.Errorf("compose = %q, want the path consumed as an attachment", a.compose.Value())
	}
}

func TestSeam_PasteMissingFileFallsBackToText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gone.txt")
	a := newTestApp(t, withActiveChannel("C1"))
	wireClipboard(a, path, nil)
	wireFilesystem(a)

	pasteInto(t, a)

	if n := len(a.compose.Attachments()); n != 0 {
		t.Errorf("attachments = %d, want 0 for a missing file", n)
	}
	if got := a.compose.Value(); got != path {
		t.Errorf("compose = %q, want the path pasted as text", got)
	}
}

func TestSeam_PasteWithoutClipboardIsInert(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))

	pasteInto(t, a)

	if a.compose.Value() != "" || len(a.compose.Attachments()) != 0 {
		t.Errorf("compose changed with no clipboard: %q, %d attachments", a.compose.Value(), len(a.compose.Attachments()))
	}
}

func TestSeam_SaveThreadWritesMarkdown(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	a := newTestApp(t, withActiveChannel("C1"))
	wireThreadExport(a)
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent text"}
	replies := []messages.MessageItem{{TS: "2.0", UserID: "U2", UserName: "bob", Text: "a reply", ThreadTS: "1.0"}}
	a.threadPanel.SetThread(parent, replies, "C1", "1.0")
	a.threadPanel.SetUserNames(map[string]string{"U1": "alice", "U2": "bob"})
	a.threadPanel.SetChannelNames(map[string]string{"C1": "dev team"})
	a.threadVisible = true
	a.focusedPanel = PanelThread

	cmd := a.saveThreadToFile()
	if cmd == nil {
		t.Fatal("no save cmd")
	}
	saved, ok := cmd().(statusbar.ThreadSavedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want ThreadSavedMsg", cmd())
	}

	dir := filepath.Join(dataHome, "slk", "exports")
	if filepath.Dir(saved.Path) != dir {
		t.Errorf("saved to %q, want a file in %q", saved.Path, dir)
	}
	if base := filepath.Base(saved.Path); !strings.HasPrefix(base, "slk-thread-dev-team-") || !strings.HasSuffix(base, ".md") {
		t.Errorf("file name = %q", base)
	}
	got, err := os.ReadFile(saved.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := export.ThreadToMarkdown(parent, replies, a.threadPanel.UserNames(), a.threadPanel.ChannelNames())
	if string(got) != want {
		t.Errorf("exported markdown differs:\n got: %q\nwant: %q", got, want)
	}
}

func TestSeam_SaveThreadReportsWriteFailure(t *testing.T) {
	// A file where the exports directory should be makes MkdirAll fail.
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "slk"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	a := newTestApp(t, withActiveChannel("C1"))
	wireThreadExport(a)
	a.threadPanel.SetThread(messages.MessageItem{TS: "1.0", Text: "p"}, nil, "C1", "1.0")
	a.threadVisible = true
	a.focusedPanel = PanelThread

	msg := a.saveThreadToFile()()
	if failed, ok := msg.(statusbar.ThreadSaveFailedMsg); !ok || failed.Reason == "" {
		t.Errorf("cmd() = %#v, want ThreadSaveFailedMsg with a reason", msg)
	}
}

func TestSeam_DownloadUnwiredToasts(t *testing.T) {
	a := newTestApp(t)

	msg := a.downloadFileCmd(messages.Attachment{Name: "doc.pdf", DownloadURL: "https://files.example/doc.pdf"})()

	if msg != (ToastMsg{Text: "File downloads unavailable"}) {
		t.Errorf("cmd() = %#v, want the unavailable toast", msg)
	}
}

func seamPNG(t *testing.T) []byte {
	t.Helper()
	img := goimage.NewRGBA(goimage.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSeam_ImagePreviewFetchesLargestThumb(t *testing.T) {
	body := seamPNG(t)
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	imgCache, err := imgpkg.NewCache(t.TempDir(), 10)
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp(t, withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1.0", UserID: "U1", Text: "pic",
		Attachments: []messages.Attachment{{
			Kind: "image", Name: "cat.png", FileID: "F1", Mime: "image/png",
			Thumbs: []messages.ThumbSpec{
				{URL: srv.URL + "/small.png", W: 2, H: 2},
				{URL: srv.URL + "/big.png", W: 4, H: 4},
			},
		}},
	}))
	a.SetImageFetcher(imgpkg.NewFetcher(imgCache, srv.Client()))

	cmd := a.openImagePreviewCmd("C1", "1.0", 0)
	if cmd == nil {
		t.Fatal("no preview cmd")
	}
	loaded, ok := cmd().(previewLoadedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want previewLoadedMsg", cmd())
	}
	if loaded.Name != "cat.png" || loaded.FileID != "F1" || loaded.Img == nil {
		t.Errorf("loaded = %+v", loaded)
	}
	if !reflect.DeepEqual(requested, []string{"/big.png"}) {
		t.Errorf("requested %v, want only the largest thumb", requested)
	}
}

func TestSeam_ImagePreviewWithoutFetcher(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"), withMessages(messages.MessageItem{
		TS: "1.0", Attachments: []messages.Attachment{{Kind: "image", FileID: "F1", Thumbs: []messages.ThumbSpec{{URL: "u", W: 1, H: 1}}}},
	}))

	if cmd := a.openImagePreviewCmd("C1", "1.0", 0); cmd != nil {
		t.Error("preview cmd returned with no fetcher wired")
	}
}

type seamFetched struct {
	channelID ids.ChannelID
	name      string
}

func TestSeam_SelectingChannelFetchesHistory(t *testing.T) {
	a := newTestApp(t, withChannels(sidebar.ChannelItem{ID: "C2", Name: "random", Type: "channel"}))
	var calls []seamFetched
	wireChannelFetch(a, func(channelID ids.ChannelID, name string) tea.Msg {
		calls = append(calls, seamFetched{channelID, name})
		return seamFetched{channelID, name}
	})

	_, cmd := a.Update(ChannelSelectedMsg{ID: "C2", Name: "random", Type: "channel"})

	if !containsMsg(drainCmd(cmd), seamFetched{"C2", "random"}) {
		t.Errorf("fetch result not among the returned msgs; calls = %+v", calls)
	}
}

type seamSent struct {
	channelID ids.ChannelID
	text      string
}

// sendVia sends "ship it" to C1 with the message service answering with
// result, and returns what the send cmd produced.
func sendVia(t *testing.T, result tea.Msg) ([]seamSent, []tea.Msg) {
	t.Helper()
	a := newTestApp(t, withChannels(sidebar.ChannelItem{ID: "C1", Name: "general", Type: "channel"}), withActiveChannel("C1"))
	var calls []seamSent
	wireMessageSend(a, func(channelID ids.ChannelID, text string) tea.Msg {
		calls = append(calls, seamSent{channelID, text})
		return result
	})
	_, cmd := a.Update(SendMessageMsg{ChannelID: "C1", Text: "ship it"})
	return calls, drainCmd(cmd)
}

func TestSeam_SendStampsPlaceholderOnSuccess(t *testing.T) {
	calls, msgs := sendVia(t, MessageSentMsg{ChannelID: "C1", Message: messages.MessageItem{TS: "9.0", Text: "ship it"}})

	if !reflect.DeepEqual(calls, []seamSent{{"C1", "ship it"}}) {
		t.Fatalf("send calls = %+v", calls)
	}
	var sent *MessageSentMsg
	for _, m := range msgs {
		if s, ok := m.(MessageSentMsg); ok {
			sent = &s
		}
	}
	if sent == nil {
		t.Fatalf("no MessageSentMsg in %#v", msgs)
	}
	if sent.LocalTS == "" || sent.Message.TS != "9.0" || sent.ChannelID != "C1" {
		t.Errorf("sent = %+v, want the service's message with a placeholder LocalTS", *sent)
	}
}

func TestSeam_SendStampsPlaceholderOnFailure(t *testing.T) {
	_, msgs := sendVia(t, MessageSendFailedMsg{ChannelID: "C1", Reason: "rate_limited"})

	var failed *MessageSendFailedMsg
	for _, m := range msgs {
		if f, ok := m.(MessageSendFailedMsg); ok {
			failed = &f
		}
	}
	if failed == nil {
		t.Fatalf("no MessageSendFailedMsg in %#v", msgs)
	}
	if failed.LocalTS == "" || failed.Reason != "rate_limited" {
		t.Errorf("failed = %+v, want the service's reason with a placeholder LocalTS", *failed)
	}
}

func TestSeam_SendPassesOtherResultsThrough(t *testing.T) {
	_, msgs := sendVia(t, ToastMsg{Text: "odd"})

	if !containsMsg(msgs, ToastMsg{Text: "odd"}) {
		t.Errorf("service result not passed through: %#v", msgs)
	}
}

type seamOpened struct {
	userIDs []string
	reqID   uint64
}

func TestSeam_NewMessageOpensConversation(t *testing.T) {
	a := newTestApp(t)
	wireOpenConversation(a, func(userIDs []string, requestID uint64) tea.Cmd {
		ids := append([]string(nil), userIDs...)
		return func() tea.Msg { return seamOpened{ids, requestID} }
	})
	a.newMessagePicker.SetUsers(newMessageUsers())
	a.newMessagePicker.Open()
	a.SetMode(ModeNewMessage)

	cmd := dispatchModeKey(a, keyCode(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter returned no cmd")
	}
	got, ok := cmd().(seamOpened)
	if !ok || !reflect.DeepEqual(got, seamOpened{[]string{"U1"}, 1}) {
		t.Errorf("cmd() = %#v, want the open for [U1] as request 1", cmd())
	}
}

func TestSeam_NewMessageOpenNilCmd(t *testing.T) {
	a := newTestApp(t)
	wireOpenConversation(a, func([]string, uint64) tea.Cmd { return nil })
	a.newMessagePicker.SetUsers(newMessageUsers())
	a.newMessagePicker.Open()
	a.SetMode(ModeNewMessage)

	if cmd := dispatchModeKey(a, keyCode(tea.KeyEnter)); cmd != nil {
		t.Errorf("a nil open cmd came back non-nil")
	}
}

type seamMark struct {
	channelID ids.ChannelID
	threadTS  ids.ThreadTS
	ts        ids.MessageTS
}

// openThreadForMark shows thread P1 in C1 so a replies load marks it.
func openThreadForMark(a *App) {
	a.threadPanel.SetThread(messages.MessageItem{TS: "P1", Text: "parent"}, nil, "C1", "P1")
	a.threadVisible = true
}

func TestSeam_ThreadRepliesMarkThreadRead(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	var calls []seamMark
	wireThreadMark(a, func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) tea.Cmd {
		calls = append(calls, seamMark{channelID, threadTS, ts})
		return func() tea.Msg { return seamMark{channelID, threadTS, ts} }
	})
	openThreadForMark(a)

	_, cmd := a.Update(ThreadRepliesLoadedMsg{ThreadTS: "P1", Replies: []messages.MessageItem{
		{TS: "P1.1", Text: "r1"}, {TS: "P1.2", Text: "r2"},
	}})

	want := seamMark{"C1", "P1", "P1.2"}
	if !reflect.DeepEqual(calls, []seamMark{want}) {
		t.Fatalf("mark calls = %+v, want [%+v]", calls, want)
	}
	if !containsMsg(drainCmd(cmd), want) {
		t.Error("the mark cmd was not part of the returned batch")
	}
}

func TestSeam_ThreadMarkNilCmd(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	marked := 0
	wireThreadMark(a, func(ids.ChannelID, ids.ThreadTS, ids.MessageTS) tea.Cmd {
		marked++
		return nil
	})
	openThreadForMark(a)

	_, cmd := a.Update(ThreadRepliesLoadedMsg{ThreadTS: "P1", Replies: []messages.MessageItem{{TS: "P1.1", Text: "r1"}}})

	if marked != 1 {
		t.Fatalf("mark called %d times, want 1", marked)
	}
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(seamMark); ok {
			t.Errorf("unexpected mark msg %#v from a nil cmd", m)
		}
	}
}

// Ctrl+E hands the draft to an external editor through a temp file;
// EditorFinishedMsg reads it back.

func editDraft(a *App, draft string) tea.Cmd {
	a.focusedPanel = PanelMessages
	a.SetMode(ModeInsert)
	_ = a.compose.Focus()
	a.compose.SetValue(draft)
	return a.handleKey(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
}

func TestSeam_EditorGetsDraftInTempFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	a := newTestApp(t, withActiveChannel("C1"))
	wireEditor(a, []string{"true"})

	if cmd := editDraft(a, "draft text"); cmd == nil {
		t.Fatal("no editor cmd")
	}

	if !a.compose.EditingExternally() {
		t.Error("compose not locked while the editor is open")
	}
	files, _ := filepath.Glob(filepath.Join(tmp, "slk-compose-*.md"))
	if len(files) != 1 {
		t.Fatalf("temp drafts = %v, want one", files)
	}
	if got, _ := os.ReadFile(files[0]); string(got) != "draft text" {
		t.Errorf("temp draft = %q, want the compose text", got)
	}
}

func TestSeam_EditorUnconfiguredToasts(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	wireEditor(a, nil)

	editDraft(a, "draft text")

	if got := statusbarText(a); !strings.Contains(got, "No editor configured") {
		t.Errorf("status bar = %q, want the no-editor toast", got)
	}
	if a.compose.EditingExternally() || a.compose.Value() != "draft text" {
		t.Errorf("compose changed without an editor: %q", a.compose.Value())
	}
}

func TestSeam_EditorResultReplacesDraft(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		wantToast string
	}{
		{"clean exit", nil, ""},
		{"non-zero exit", exec.Command("false").Run(), ""},
		{"never launched", &exec.Error{Name: "nope", Err: exec.ErrNotFound}, "Could not open editor: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "slk-compose-1.md")
			if err := os.WriteFile(path, []byte("edited\n\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			a := newTestApp(t, withActiveChannel("C1"))
			wireEditor(a, []string{"true"})
			a.compose.SetValue("old")
			a.compose.SetEditingExternally(true)

			a.Update(EditorFinishedMsg{Panel: PanelMessages, Path: path, Err: tc.err})

			if got := a.compose.Value(); got != "edited" {
				t.Errorf("compose = %q, want the edited draft", got)
			}
			if a.compose.EditingExternally() {
				t.Error("compose still locked")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("temp draft left behind: %v", err)
			}
			got := statusbarText(a)
			if tc.wantToast == "" && strings.Contains(got, "editor") {
				t.Errorf("status bar = %q, want no editor toast", got)
			}
			if tc.wantToast != "" && !strings.Contains(got, tc.wantToast) {
				t.Errorf("status bar = %q, want %q", got, tc.wantToast)
			}
		})
	}
}

func TestSeam_EditorUnreadableDraftKeepsCompose(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C1"))
	wireEditor(a, []string{"true"})
	a.compose.SetValue("old")
	a.compose.SetEditingExternally(true)

	a.Update(EditorFinishedMsg{Panel: PanelMessages, Path: filepath.Join(t.TempDir(), "gone.md")})

	if got := a.compose.Value(); got != "old" {
		t.Errorf("compose = %q, want the draft kept", got)
	}
	if got := statusbarText(a); !strings.Contains(got, "Editor: could not read draft back") {
		t.Errorf("status bar = %q, want the read-back toast", got)
	}
}

func containsMsg(msgs []tea.Msg, want tea.Msg) bool {
	for _, m := range msgs {
		if reflect.DeepEqual(m, want) {
			return true
		}
	}
	return false
}
