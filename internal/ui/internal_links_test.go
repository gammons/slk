package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// A #channel mention renders as a clickable internal link: the click
// produces OpenLinkMsg, and the App opens the channel in place.
func TestChannelMentionClickOpensChannel(t *testing.T) {
	a := newTestApp(t, withSize(120, 60), withActiveChannel("C0"),
		withMessages(messages.MessageItem{
			TS: "1.0", UserName: "alice",
			Text: "Please open a request in <#C123|help-desk>.",
		}))
	a.channelFinder.SetItems([]core.ChannelFinderItem{
		{ID: "C123", Name: "help-desk", Type: "channel", Joined: true},
	})
	_ = a.View()
	_ = a.View()

	rects := a.messagepane.LastHitsForKindForTest(entrylist.HitLink)
	if len(rects) == 0 {
		t.Fatal("channel mention rendered no link hit rect")
	}
	h := rects[0]
	if !strings.HasPrefix(h.FileID, "slack://channel?") {
		t.Fatalf("mention hit URL = %q, want a slack://channel link", h.FileID)
	}
	cmd := clickAtContent(a, (h.ColStart+h.ColEnd)/2, (h.RowStart+h.RowEnd)/2)
	if cmd == nil {
		t.Fatal("clicking the mention produced no command")
	}
	clicked := cmd()
	open, ok := clicked.(OpenLinkMsg)
	if !ok {
		t.Fatalf("click produced %T, want OpenLinkMsg", clicked)
	}

	_, cmd = a.Update(open)
	var selected *ChannelSelectedMsg
	for _, msg := range drainBatch(cmd) {
		if cs, ok := msg.(ChannelSelectedMsg); ok {
			selected = &cs
		}
	}
	if selected == nil {
		t.Fatal("opening the mention dispatched no ChannelSelectedMsg")
	}
	if selected.ID != "C123" || selected.Name != "help-desk" || selected.Type != "channel" {
		t.Errorf("selected = %+v, want C123/help-desk/channel", selected)
	}
}

// A mention of a channel the user is not in asks for a join, using the
// label the mention carried.
func TestChannelMentionClickJoinsUnknownChannel(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C0"))
	var joined []string
	setChannelFuncsForTest(a, core.ChannelServiceFuncs{
		Lookup: func(ids.ChannelID) (string, string, bool) { return "", "", false },
		Join: func(id ids.ChannelID, name string) core.Msg {
			joined = append(joined, string(id)+"/"+name)
			return ChannelJoinedMsg{ID: string(id), Name: name}
		},
	})

	_, cmd := a.Update(OpenLinkMsg{URL: "slack://channel?id=C999&name=help-scratch"})
	drainBatch(cmd)

	if len(joined) != 1 || joined[0] != "C999/help-scratch" {
		t.Fatalf("joins = %v, want C999/help-scratch", joined)
	}
}

// A slack:// target slk does not know is refused in-app; it never goes
// to the OS launcher.
func TestUnknownSlackLinkIsRefused(t *testing.T) {
	a := newTestApp(t)
	a.Update(OpenLinkMsg{URL: "slack://app?team=T1&id=A1"})
	if got := statusbarText(a); !strings.Contains(got, "can't open this Slack link") {
		t.Errorf("status bar = %q, want the refusal toast", got)
	}
}

// A channel link with no channel id is refused rather than joining "".
func TestChannelMentionWithoutIDIsRefused(t *testing.T) {
	a := newTestApp(t)
	a.Update(OpenLinkMsg{URL: "slack://channel?name=general"})
	if got := statusbarText(a); !strings.Contains(got, "no channel") {
		t.Errorf("status bar = %q, want the missing-channel toast", got)
	}
}

// The keyboard route offers the mention too: with only a mention in the
// message, `o` dispatches its internal link directly.
func TestChannelMentionKeyboardOpen(t *testing.T) {
	a := newTestApp(t, withSize(120, 60), withActiveChannel("C0"),
		withMessages(messages.MessageItem{
			TS: "1.0", UserName: "alice",
			Text: "Please open a request in <#C123|help-desk>.",
		}))
	focusMessages(t, a)
	a.channelFinder.SetItems([]core.ChannelFinderItem{
		{ID: "C123", Name: "help-desk", Type: "channel", Joined: true},
	})

	var selected *ChannelSelectedMsg
	for _, msg := range pressKey(t, a, keyPress('o')) {
		if cs, ok := msg.(ChannelSelectedMsg); ok {
			selected = &cs
		}
	}
	if selected == nil || selected.ID != "C123" {
		t.Fatalf("o on a mention-only message selected %+v, want C123", selected)
	}
}

// A malformed Slack link is refused in-app; it must never be handed to
// the OS browser.
func TestMalformedSlackLinkIsRefused(t *testing.T) {
	a := newTestApp(t)
	opened := ""
	a.browserOpener = func(rawURL string) tea.Cmd {
		opened = rawURL
		return nil
	}

	a.Update(OpenLinkMsg{URL: "slack://app/%zz"})

	if opened != "" {
		t.Errorf("malformed Slack link was handed to the browser: %q", opened)
	}
	if got := statusbarText(a); !strings.Contains(got, "can't open this Slack link") {
		t.Errorf("status bar = %q, want the refusal toast", got)
	}
}

// The link picker labels internal Slack links as in-app.
func TestSlackLinkOpensInApp(t *testing.T) {
	a := newTestApp(t)
	if !a.linkOpensInApp("slack://channel?id=C1&name=general") {
		t.Error("internal channel link labeled external")
	}
	if !a.linkOpensInApp("slack://app/%zz") {
		t.Error("malformed internal link labeled external; routing refuses it in-app")
	}
}

// A finder item marked not-joined asks for a join even though Lookup
// knows the channel (Lookup also holds browseable, unjoined channels).
func TestChannelMentionClickJoinsBrowseableChannel(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C0"))
	var joined []string
	a.channelFinder.SetItems([]core.ChannelFinderItem{
		{ID: "C777", Name: "browseable", Type: "channel", Joined: false},
	})
	setChannelFuncsForTest(a, core.ChannelServiceFuncs{
		Lookup: func(ids.ChannelID) (string, string, bool) { return "browseable", "channel", true },
		Join: func(id ids.ChannelID, name string) core.Msg {
			joined = append(joined, string(id)+"/"+name)
			return ChannelJoinedMsg{ID: string(id), Name: name}
		},
	})

	_, cmd := a.Update(OpenLinkMsg{URL: "slack://channel?id=C777&name=browseable"})
	drainBatch(cmd)

	if len(joined) != 1 || joined[0] != "C777/browseable" {
		t.Fatalf("joins = %v, want C777/browseable", joined)
	}
}

// A refused join is reported: a click that ends in silence reads as a
// broken link.
func TestJoinFailureToasts(t *testing.T) {
	a := newTestApp(t)
	a.Update(ChannelJoinFailedMsg{ID: "C9", Name: "secret", Err: errors.New("not_allowed")})
	if got := statusbarText(a); !strings.Contains(got, "Could not join #secret") || !strings.Contains(got, "not_allowed") {
		t.Errorf("status bar = %q, want the join-failure toast with its reason", got)
	}
}

// A resolved bare mention carries its name in the renderer's map, not in
// the keyboard URL. The join must still find it instead of refusing.
func TestChannelMentionClickUsesResolvedName(t *testing.T) {
	a := newTestApp(t, withActiveChannel("C0"))
	a.channelNames = map[string]string{"C9": "ops"}
	var joined []string
	setChannelFuncsForTest(a, core.ChannelServiceFuncs{
		Lookup: func(ids.ChannelID) (string, string, bool) { return "", "", false },
		Join: func(id ids.ChannelID, name string) core.Msg {
			joined = append(joined, string(id)+"/"+name)
			return ChannelJoinedMsg{ID: string(id), Name: name}
		},
	})

	_, cmd := a.Update(OpenLinkMsg{URL: "slack://channel?id=C9"})
	drainBatch(cmd)

	if len(joined) != 1 || joined[0] != "C9/ops" {
		t.Fatalf("joins = %v, want C9/ops", joined)
	}
}

// A bare mention has no label in its extracted URL; the picker names it
// from the URL's own name or the renderer's resolved map.
func TestInternalLinkLabel(t *testing.T) {
	a := newTestApp(t)
	a.channelNames = map[string]string{"C9": "ops"}
	cases := []struct{ raw, label, want string }{
		{"slack://channel?id=C9", "", "#ops"},
		{"slack://channel?id=C9&name=eng", "", "#eng"},
		{"slack://channel?id=C9&name=eng", "#custom", "#custom"},
		{"slack://channel?id=C404", "", ""},
		{"https://example.com", "", ""},
	}
	for _, tc := range cases {
		if got := a.internalLinkLabel(tc.raw, tc.label); got != tc.want {
			t.Errorf("internalLinkLabel(%q, %q) = %q, want %q", tc.raw, tc.label, got, tc.want)
		}
	}
}
