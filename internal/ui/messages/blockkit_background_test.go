// internal/ui/messages/blockkit_background_test.go
//
// Block Kit lines must carry the theme background on every run of
// visible text, not only on the trailing pad.
//
// The inline styles (bold, italic, link, mention) deliberately omit
// .Background() and rely on an outer style supplying it. Body text has
// styles.MessageText for that; Block Kit lines do not — they are a
// gutter prefix plus rendered content, and the prefix's closing reset
// clears the background for the rest of the line. The visible symptom
// was a selected message whose tint stopped at the glyphs.
package messages

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/styles"
)

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// runsWithoutBackground reports the visible text runs in s that render
// with no background set — i.e. text reached after a reset with no
// background re-established before it.
func runsWithoutBackground(s string) []string {
	var bare []string
	for _, line := range strings.Split(s, "\n") {
		bgActive := false
		idx := 0
		for _, loc := range sgrRe.FindAllStringIndex(line, -1) {
			if text := line[idx:loc[0]]; strings.TrimSpace(text) != "" && !bgActive {
				bare = append(bare, text)
			}
			seq := line[loc[0]:loc[1]]
			switch {
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

func blockLinesFor(t *testing.T, blocks []blockkit.Block) string {
	t.Helper()
	styles.Apply("nord", config.Theme{})
	res := blockkit.Render(blocks, blockkit.Context{
		RenderText: func(s string, un map[string]string) string {
			return RenderSlackMarkdownWith(s, RenderSlackMarkdownOpts{UserNames: un})
		},
		WrapText: WordWrap,
	}, 60)
	// Mirror the host's ORDER exactly: the background treatment runs
	// over the Block Kit lines first, and placeAvatarBeside prepends
	// the gutter afterwards. Getting this backwards hides the bug —
	// the gutter's closing reset is what strips the background, and if
	// it is inside the string being treated, the treatment patches it
	// and the test passes against broken code.
	treated := WithBackground(res.Lines, BgANSI())
	gutter := styles.MessageText.Render("     ")
	var lines []string
	for _, l := range strings.Split(treated, "\n") {
		lines = append(lines, gutter+l)
	}
	return strings.Join(lines, "\n")
}

func TestBlockKitLines_EveryTextRunKeepsABackground(t *testing.T) {
	out := blockLinesFor(t, []blockkit.Block{
		blockkit.SectionBlock{Text: "*Out Today*\nAlex Rivera, _back in 3 days_"},
	})
	if bare := runsWithoutBackground(out); len(bare) > 0 {
		t.Errorf("these runs render with no background set: %q", bare)
	}
}

// The plain run BEFORE a styled span is the one the old code lost: it
// sat between the gutter's reset and the span's own SGR.
func TestBlockKitLines_PlainRunBeforeStyledSpan(t *testing.T) {
	out := blockLinesFor(t, []blockkit.Block{
		blockkit.SectionBlock{Text: "leading plain text _then italic_"},
	})
	for _, run := range runsWithoutBackground(out) {
		if strings.Contains(run, "leading plain text") {
			t.Errorf("the run before the styled span lost its background: %q", run)
		}
	}
}

func TestBlockKitLines_HeaderAndContextKeepBackground(t *testing.T) {
	out := blockLinesFor(t, []blockkit.Block{
		blockkit.HeaderBlock{Text: "Pull Request opened"},
		blockkit.ContextBlock{Elements: []blockkit.ContextElement{{Text: "opened by _someone_"}}},
	})
	if bare := runsWithoutBackground(out); len(bare) > 0 {
		t.Errorf("these runs render with no background set: %q", bare)
	}
}

// Guard the detector itself: without the fix the bare runs ARE found,
// so a passing test above means something.
func TestRunsWithoutBackground_DetectsTheDefect(t *testing.T) {
	styles.Apply("nord", config.Theme{})
	broken := styles.MessageText.Render("     ") + "unstyled text\x1b[3mitalic\x1b[m"
	if bare := runsWithoutBackground(broken); len(bare) == 0 {
		t.Error("detector found nothing in a deliberately broken line")
	}
}

// WithBackground must do BOTH halves: prefix each line, and patch after
// each reset inside it. The v1 fix did only the second and left the
// run at the start of every line bare.
func TestWithBackground_PrefixesAndPatches(t *testing.T) {
	styles.Apply("nord", config.Theme{})
	bg := BgANSI()

	got := WithBackground([]string{"plain\x1b[mmiddle\x1b[0mtail"}, bg)
	if !strings.HasPrefix(got, bg) {
		t.Errorf("line not prefixed with the background: %q", got)
	}
	for _, reset := range []string{"\x1b[m", "\x1b[0m"} {
		if !strings.Contains(got, reset+bg) {
			t.Errorf("background not re-applied after %q: %q", reset, got)
		}
	}
}

func TestWithBackground_EmptyBackgroundIsPassthrough(t *testing.T) {
	got := WithBackground([]string{"a", "b"}, "")
	if got != "a\nb" {
		t.Errorf("got %q, want the lines joined untouched", got)
	}
}

// The tests above build their input by calling WithBackground directly,
// so they pin the helper's behaviour and the detector's. None of them
// fails if renderMessagePlain stops calling it — which is the whole
// defect, not a detail of it. This one goes through buildCache, so the
// wiring is what is under test.
func TestBuildCache_BlockKitRunsKeepBackground(t *testing.T) {
	styles.Apply("nord", config.Theme{})
	msg := MessageItem{
		TS:        "1700000000.000000",
		UserName:  "github",
		UserID:    "U-BOT",
		Text:      "PR opened",
		Timestamp: "1:23 PM",
		Blocks: []blockkit.Block{
			// The plain run before a styled span is the one the old
			// code lost: it sits between the gutter's closing reset
			// and the span's own SGR.
			blockkit.SectionBlock{Text: "leading plain text _then italic_"},
		},
	}
	m := New([]MessageItem{msg}, "general")
	// The defect needs the avatar gutter. placeAvatarBeside prepends it
	// to every line AFTER the Block Kit lines are composed, and it is
	// the gutter's closing reset that clears the background for the
	// rest of the line — with no avatar there is no such reset and the
	// bug cannot appear. Only the avatar's content is stood in for
	// here; the composition order is the renderer's own.
	m.SetAvatarFunc(func(string) string { return styles.MessageText.Render("  ") })
	m.buildCache(100)

	var lines []string
	for _, e := range m.cache {
		if e.msgIdx == 0 {
			lines = e.linesSelected
			break
		}
	}
	if lines == nil {
		t.Fatal("no entry with msgIdx 0 in cache")
	}
	out := strings.Join(lines, "\n")
	if !strings.Contains(sgrRe.ReplaceAllString(out, ""), "leading plain text") {
		t.Fatalf("the Block Kit section never reached the render: %q", out)
	}
	for _, run := range runsWithoutBackground(out) {
		if strings.Contains(run, "leading plain text") {
			t.Errorf("Block Kit run rendered with no background: %q", run)
		}
	}
}
