// internal/ui/thread/blockkit_background_test.go
//
// The thread pane composes Block Kit lines the same way the messages
// pane does and needs the same background treatment; see the matching
// test in internal/ui/messages. Without it the run after the avatar
// gutter's closing reset draws on the terminal default instead of the
// theme's, and a selected message's tint stops at the glyphs.
package thread

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
)

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// bareRuns reports the visible text runs in s reached after a reset
// with no background re-established before them. Deliberately a local
// copy of the messages-side detector: it is twenty lines, and sharing
// it would mean exporting a test-only helper from another package.
func bareRuns(s string) []string {
	var bare []string
	for _, line := range strings.Split(s, "\n") {
		bgActive := false
		idx := 0
		for _, loc := range sgrRe.FindAllStringIndex(line, -1) {
			if text := line[idx:loc[0]]; strings.TrimSpace(text) != "" && !bgActive {
				bare = append(bare, text)
			}
			switch seq := line[loc[0]:loc[1]]; {
			case seq == "\x1b[m" || seq == "\x1b[0m":
				bgActive = false
			case strings.Contains(seq, "48;"):
				bgActive = true
			}
			idx = loc[1]
		}
		if tail := line[idx:]; strings.TrimSpace(tail) != "" && !bgActive {
			bare = append(bare, tail)
		}
	}
	return bare
}

func TestRenderThreadMessage_BlockKitRunsKeepBackground(t *testing.T) {
	styles.Apply("nord", config.Theme{})
	m := New()
	// The defect needs the avatar gutter: it is prepended to every line
	// after the Block Kit lines are composed, and its closing reset is
	// what clears the background for the rest of the line.
	m.SetAvatarFunc(func(string) string { return styles.MessageText.Render("  ") })

	msg := messages.MessageItem{
		TS:        "1700000001.000000",
		UserName:  "github",
		UserID:    "U-BOT",
		Text:      "PR opened",
		Timestamp: "10:30 AM",
		Blocks: []blockkit.Block{
			blockkit.SectionBlock{Text: "leading plain text _then italic_"},
		},
	}
	got, _, _ := m.renderThreadMessage(msg, 80, nil, nil, true)
	if !strings.Contains(sgrRe.ReplaceAllString(got, ""), "leading plain text") {
		t.Fatalf("the Block Kit section never reached the render: %q", got)
	}
	for _, run := range bareRuns(got) {
		if strings.Contains(run, "leading plain text") {
			t.Errorf("Block Kit run rendered with no background: %q", run)
		}
	}
}
