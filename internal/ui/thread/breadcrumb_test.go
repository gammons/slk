package thread

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/ui/messages"
)

// pad right-pads s with spaces to width cells.
func pad(s string, width int) string {
	return s + strings.Repeat(" ", width-ansi.StringWidth(s))
}

// hinted places the close hint flush right of left at width cells.
func hinted(left string, width int) string {
	return pad(left, width-ansi.StringWidth(breadcrumbHint)) + breadcrumbHint
}

func TestRenderBreadcrumb(t *testing.T) {
	const full = "# general › Thread from alice · 2 replies" // 41 cells
	tests := []struct {
		name    string
		width   int
		channel string
		chType  string
		author  string
		replies int
		want    string
	}{
		{"room for everything", 60, "general", "channel", "alice", 2, hinted(full, 60)},
		{"hint fits exactly with a two-space gap", 52, "general", "channel", "alice", 2, full + "  " + breadcrumbHint},
		{"one short of the hint drops only the hint", 51, "general", "channel", "alice", 2, pad(full, 51)},
		{"full crumb fits exactly", 41, "general", "channel", "alice", 2, full},
		{"author dropped next", 40, "general", "channel", "alice", 2, pad("# general › Thread · 2 replies", 40)},
		{"channel truncated last", 25, "general", "channel", "alice", 2, "# g… › Thread · 2 replies"},
		{"no room for any channel", 22, "general", "channel", "alice", 2, pad("Thread · 2 replies", 22)},
		{"core hard-truncated below its own width", 10, "general", "channel", "alice", 2, "Thread · 2"},
		{"singular reply", 60, "general", "channel", "alice", 1, hinted("# general › Thread from alice · 1 reply", 60)},
		{"no author", 60, "general", "channel", "", 2, hinted("# general › Thread · 2 replies", 60)},
		{"no channel", 60, "", "", "alice", 2, hinted("Thread from alice · 2 replies", 60)},
		{"private glyph", 60, "design", "private", "alice", 2, hinted("◆ design › Thread from alice · 2 replies", 60)},
		{"dm glyph", 60, "bob", "dm", "alice", 2, hinted("● bob › Thread from alice · 2 replies", 60)},
		{"group dm glyph", 60, "bob, carol", "group_dm", "alice", 2, hinted("● bob, carol › Thread from alice · 2 replies", 60)},
		{"emoji in author name", 60, "general", "channel", "al🎉ice", 2, hinted("# general › Thread from al🎉ice · 2 replies", 60)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Guards the table itself: a mistyped expectation must not
			// silently assert a different width than the row claims.
			if w := ansi.StringWidth(tt.want); w != tt.width {
				t.Fatalf("bad row: want is %d cells, width is %d", w, tt.width)
			}
			got := renderBreadcrumb(tt.width, tt.channel, tt.chType, tt.author, tt.replies)
			if w := ansi.StringWidth(got); w != tt.width {
				t.Errorf("width = %d, want %d", w, tt.width)
			}
			if plain := ansi.Strip(got); plain != tt.want {
				t.Errorf("breadcrumb =\n%q\nwant\n%q", plain, tt.want)
			}
		})
	}
}

func breadcrumbTestModel() *Model {
	m := New()
	m.SetThread(
		messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"},
		[]messages.MessageItem{
			{TS: "2.0", UserID: "U2", UserName: "bob", Text: "one"},
			{TS: "3.0", UserID: "U2", UserName: "bob", Text: "two"},
		},
		"C1", "1.0")
	return m
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func TestView_HeaderIsTheBreadcrumb(t *testing.T) {
	m := breadcrumbTestModel()
	m.SetBreadcrumb("general", "channel")
	got := ansi.Strip(firstLine(m.View(20, 60)))
	if want := hinted("# general › Thread from alice · 2 replies", 60); got != want {
		t.Errorf("header =\n%q\nwant\n%q", got, want)
	}
	if m.chromeHeight != 2 {
		t.Errorf("chromeHeight = %d, want 2 (breadcrumb + separator); hit-testing depends on it", m.chromeHeight)
	}
}

func TestView_BreadcrumbChangeInvalidatesChrome(t *testing.T) {
	m := breadcrumbTestModel()
	m.SetBreadcrumb("general", "channel")
	_ = m.View(20, 60)
	v := m.Version()
	m.SetBreadcrumb("design", "private")
	if m.Version() == v {
		t.Error("SetBreadcrumb did not bump Version; the App panel cache would serve the old header")
	}
	if got := ansi.Strip(firstLine(m.View(20, 60))); !strings.HasPrefix(got, "◆ design › Thread") {
		t.Errorf("header after SetBreadcrumb = %q, want the new channel", got)
	}
	v = m.Version()
	m.SetBreadcrumb("design", "private")
	if m.Version() != v {
		t.Error("an unchanged SetBreadcrumb bumped Version")
	}
}

func TestView_BreadcrumbAuthorFallsBackToUserNames(t *testing.T) {
	m := New()
	m.SetThread(messages.MessageItem{TS: "1.0", UserID: "U9", Text: "parent"}, nil, "C1", "1.0")
	m.SetUserNames(map[string]string{"U9": "zed"})
	if got := ansi.Strip(firstLine(m.View(20, 60))); !strings.HasPrefix(got, "Thread from zed · 0 replies") {
		t.Errorf("header = %q, want the author resolved from userNames", got)
	}
}
