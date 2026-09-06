package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/wintree"
)

// updateGolden re-blesses every golden file this run touches.
//
//	go test ./internal/ui -run TestGolden -update
var updateGolden = flag.Bool("update", false, "rewrite golden files from current output")

// goldenDir is where .ansi goldens live, relative to this package.
const goldenDir = "testdata/golden"

// compareGolden asserts got matches testdata/golden/<name>.ansi byte
// for byte, or rewrites it under -update.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(goldenDir, name+".ansi")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		t.Logf("updated %s (%d bytes, %d lines)", path, len(got), strings.Count(got, "\n")+1)
		return
	}

	wantB, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing or unreadable: %v\n"+
			"bless it with: go test ./internal/ui -run TestGolden -update", path, err)
	}

	if d := styleAwareDiff(string(wantB), got); d != "" {
		t.Errorf("golden %s: %s", path, d)
	}
}

// styleAwareDiff describes how got differs from want, or returns "" when
// they are identical.
//
// The output is deliberately two-tier. Goldens store raw ANSI, so a naive
// diff of a styling-only regression is an unreadable wall of escape
// sequences and gets blessed without being read. When the stripped text
// matches, we say so explicitly and point at the offending byte instead.
//
// Split out from compareGolden so both tiers are directly testable
// without needing to observe a *testing.T failing.
func styleAwareDiff(want, got string) string {
	if want == got {
		return ""
	}
	if d := firstLineDiff(stripANSI(want), stripANSI(got)); d != "" {
		return "rendered text differs\n" + d
	}
	return fmt.Sprintf("content identical, STYLING differs\n%s\n"+
		"A style regression (selection highlight, unread bold, muted dim) "+
		"is the usual cause. Do not bless this without reading it.",
		firstByteDiff(want, got))
}

// firstLineDiff returns a human-readable description of the first
// differing line, or "" when the inputs are equal.
func firstLineDiff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	n := min(len(wl), len(gl))
	for i := 0; i < n; i++ {
		if wl[i] != gl[i] {
			return fmt.Sprintf("first difference at line %d:\n  want: %q\n  got:  %q", i+1, wl[i], gl[i])
		}
	}
	if len(wl) != len(gl) {
		return fmt.Sprintf("line count differs: want %d lines, got %d", len(wl), len(gl))
	}
	return ""
}

// firstByteDiff locates the first differing byte and prints a quoted
// window around it in both inputs, or returns "" when the inputs are
// equal — the same contract as firstLineDiff.
//
// The empty-on-equal case is unreachable through styleAwareDiff, which
// guards on want == got first, but these helpers are called directly by
// tests. Reporting "byte length differs: want 3, got 3" for two equal
// strings is both false and self-contradictory.
func firstByteDiff(want, got string) string {
	n := min(len(want), len(got))
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			lo := max(i-40, 0)
			hiW := min(i+40, len(want))
			hiG := min(i+40, len(got))
			return fmt.Sprintf("first differing byte %d:\n  want: %q\n  got:  %q",
				i, want[lo:hiW], got[lo:hiG])
		}
	}
	// The common prefix ran to the end of the shorter input. Equal
	// lengths at this point means the strings are identical.
	if len(want) == len(got) {
		return ""
	}
	return fmt.Sprintf("byte length differs: want %d, got %d", len(want), len(got))
}

// stripANSI removes SGR/OSC sequences so a text-level diff is readable.
func stripANSI(s string) string { return ansi.Strip(s) }

func TestFirstLineDiff_ReportsFirstDifferingLine(t *testing.T) {
	want := "alpha\nbravo\ncharlie"
	got := "alpha\nBRAVO\ncharlie"
	out := firstLineDiff(want, got)
	if !strings.Contains(out, "line 2") {
		t.Errorf("expected line 2 in %q", out)
	}
	if !strings.Contains(out, "bravo") || !strings.Contains(out, "BRAVO") {
		t.Errorf("expected both values in %q", out)
	}
}

func TestFirstLineDiff_ReportsLineCountMismatch(t *testing.T) {
	out := firstLineDiff("a\nb", "a\nb\nc")
	if !strings.Contains(out, "line count") {
		t.Errorf("expected line-count message in %q", out)
	}
}

func TestFirstLineDiff_EmptyWhenEqual(t *testing.T) {
	if out := firstLineDiff("same", "same"); out != "" {
		t.Errorf("expected empty diff, got %q", out)
	}
}

// TestFirstByteDiff_ReportsOffsetAndHex pins the offset at 9, not 8.
// In "plain \x1b[31m..." the bytes are p,l,a,i,n,space,ESC,[,3,1 — index
// 8 is '3' in both inputs; the first byte that actually differs is the
// '1' vs '2' at index 9. The task brief said 8; it was off by one.
func TestFirstByteDiff_ReportsOffsetAndHex(t *testing.T) {
	want := "plain \x1b[31mred\x1b[0m"
	got := "plain \x1b[32mred\x1b[0m"
	out := firstByteDiff(want, got)
	if !strings.Contains(out, "byte 9") {
		t.Errorf("expected byte offset 9 in %q", out)
	}
	if !strings.Contains(out, `\x1b[31m`) || !strings.Contains(out, `\x1b[32m`) {
		t.Errorf("expected quoted escape windows for both inputs in %q", out)
	}
}

func TestFirstByteDiff_ReportsLengthMismatch(t *testing.T) {
	out := firstByteDiff("abc", "abcdef")
	if !strings.Contains(out, "byte length differs") {
		t.Errorf("expected length message in %q", out)
	}
	if !strings.Contains(out, "want 3") || !strings.Contains(out, "got 6") {
		t.Errorf("expected both lengths in %q", out)
	}
}

// TestFirstByteDiff_EmptyWhenEqual pins the same contract firstLineDiff
// has: equal inputs produce no diff. The empty string must survive the
// zero-length case too, where the loop body never runs.
func TestFirstByteDiff_EmptyWhenEqual(t *testing.T) {
	for _, s := range []string{"", "abc", "hello \x1b[1mworld\x1b[0m"} {
		if out := firstByteDiff(s, s); out != "" {
			t.Errorf("firstByteDiff(%q, %q) = %q, want \"\"", s, s, out)
		}
	}
}

// TestFirstByteDiff_WindowsAreBounded guards the slice arithmetic: a
// difference near either end must not panic and must stay inside both
// inputs.
func TestFirstByteDiff_WindowsAreBounded(t *testing.T) {
	long := strings.Repeat("x", 200)
	if out := firstByteDiff("a"+long, "b"+long); !strings.Contains(out, "byte 0") {
		t.Errorf("expected byte 0 in %q", out)
	}
	if out := firstByteDiff(long+"a", long+"b"); !strings.Contains(out, "byte 200") {
		t.Errorf("expected byte 200 in %q", out)
	}
}

func TestStyleAwareDiff_EmptyWhenEqual(t *testing.T) {
	s := "hello \x1b[1mworld\x1b[0m"
	if out := styleAwareDiff(s, s); out != "" {
		t.Errorf("expected empty diff, got %q", out)
	}
}

// TestStyleAwareDiff_TextDifferenceReportsLineDiff is tier one: the
// visible characters changed, so the reviewer gets a readable text diff
// and no talk of styling.
func TestStyleAwareDiff_TextDifferenceReportsLineDiff(t *testing.T) {
	want := "\x1b[1m#general\x1b[0m\nhello"
	got := "\x1b[1m#random\x1b[0m\nhello"
	out := styleAwareDiff(want, got)
	if !strings.Contains(out, "rendered text differs") {
		t.Errorf("expected text-differs header in %q", out)
	}
	if !strings.Contains(out, "line 1") {
		t.Errorf("expected line 1 in %q", out)
	}
	if strings.Contains(out, "STYLING") {
		t.Errorf("text diff should not mention styling: %q", out)
	}
	// The readable tier must be free of raw escapes.
	if strings.Contains(out, "\x1b") {
		t.Errorf("text diff leaked a raw escape byte: %q", out)
	}
}

// TestStyleAwareDiff_StylingOnlyIsCalledOut is tier two, the branch this
// whole helper exists for: identical characters, different attributes.
func TestStyleAwareDiff_StylingOnlyIsCalledOut(t *testing.T) {
	want := "\x1b[31m#general\x1b[0m"
	got := "\x1b[32m#general\x1b[0m"
	out := styleAwareDiff(want, got)
	if !strings.Contains(out, "content identical, STYLING differs") {
		t.Errorf("expected styling callout in %q", out)
	}
	if !strings.Contains(out, "first differing byte") {
		t.Errorf("expected a byte pointer in %q", out)
	}
	if strings.Contains(out, "rendered text differs") {
		t.Errorf("styling diff should not claim text differs: %q", out)
	}
}

// TestStyleAwareDiff_TrailingStyleOnlyDifference covers a styling change
// that adds bytes rather than substituting them: stripped text is still
// equal, so it must land in the styling tier and not be misreported as a
// line-count mismatch.
func TestStyleAwareDiff_TrailingStyleOnlyDifference(t *testing.T) {
	want := "#general"
	got := "\x1b[1m#general\x1b[0m"
	out := styleAwareDiff(want, got)
	if !strings.Contains(out, "content identical, STYLING differs") {
		t.Errorf("expected styling callout in %q", out)
	}
}

// TestCompareGolden_UpdateThenCompareRoundTrips exercises both modes of
// compareGolden against a scratch working directory, so no artifact is
// left in the repo's testdata.
func TestCompareGolden_UpdateThenCompareRoundTrips(t *testing.T) {
	t.Chdir(t.TempDir())

	content := "\x1b[1m#general\x1b[0m\nline two\n"

	defer func(prev bool) { *updateGolden = prev }(*updateGolden)

	*updateGolden = true
	compareGolden(t, "roundtrip", content)

	path := filepath.Join(goldenDir, "roundtrip.ansi")
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden written by -update: %v", err)
	}
	if string(onDisk) != content {
		t.Errorf("golden not written byte-for-byte:\n  want %q\n  got  %q", content, string(onDisk))
	}

	// Compare mode against the freshly blessed file must be silent.
	*updateGolden = false
	compareGolden(t, "roundtrip", content)
	if t.Failed() {
		t.Fatal("compareGolden reported a mismatch against its own -update output")
	}
}

// TestCompareGolden_UpdateCreatesMissingDir pins the MkdirAll: testdata/
// does not exist in a fresh tree, and a bless run must create it rather
// than fail.
func TestCompareGolden_UpdateCreatesMissingDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if _, err := os.Stat(filepath.Join(dir, goldenDir)); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist, stat err = %v", goldenDir, err)
	}

	defer func(prev bool) { *updateGolden = prev }(*updateGolden)
	*updateGolden = true
	compareGolden(t, "fresh", "content\n")

	if _, err := os.Stat(filepath.Join(dir, goldenDir, "fresh.ansi")); err != nil {
		t.Fatalf("expected golden created under a missing dir: %v", err)
	}
}

// ---------------------------------------------------------------------
// newGoldenApp: the deterministic App every golden scenario is built on.
// ---------------------------------------------------------------------

// goldenClock is the instant every golden anchors to: Sunday
// 2026-03-15, midday, in the process's local zone.
//
// Local, not UTC, and midday, not midnight. Both are load-bearing, and
// neither is obvious:
//
//   - The day-divider label is a comparison between DateFromTS(msg.TS),
//     which formats in the LOCAL zone (messages/model.go:3473), and the
//     calendar fields of nowFunc() (model.go:3521). A UTC-anchored clock
//     makes those two disagree in any zone east of UTC+11: the fixture's
//     "Yesterday" row silently becomes a second "Today" in Auckland
//     (UTC+13 in March), Fiji, and Kiritimati. Anchoring the clock to
//     the local zone makes both sides shift together, so the labels come
//     out identical in every zone on earth. This task's brief specified
//     time.UTC; it renders differently in UTC+12..+14.
//   - Midday keeps the fixture's derived timestamps away from any
//     midnight boundary, so the local calendar day is unambiguous even
//     with the ±14h spread of real UTC offsets.
//
// TestNewGoldenApp_RenderIsTimezoneIndependent pins this.
func goldenClock() time.Time {
	return time.Date(2026, 3, 15, 12, 0, 0, 0, time.Local)
}

// newGoldenApp is newTestApp plus every global the render path reads,
// pinned and reverted. A golden is only meaningful if these are fixed;
// anything not pinned here shows up later as golden flakiness.
//
// What is pinned, and why each one matters:
//
//   - styles: package-global palette, mutated in production by
//     mode_theme_switcher.go and in tests by reducer_search_test.go:437.
//     Every SGR byte in a golden depends on it.
//   - emoji image mode: package-global. When on, emoji render as kitty
//     APC escapes and occupy a different cell width.
//   - messages day-divider clock: decides "Today" / "Yesterday" /
//     weekday / absolute date on every separator row.
//   - sidebar staleness clock: per-Model, decides which channels are
//     filtered out as unread-too-long.
//   - spinner frame, avatar func, image protocol, "now" timestamp
//     formatter: per-App, all animation or environment dependent.
//
// Reverting means "re-apply the dark theme", not "restore the pristine
// package state". styles.Apply is not idempotent with respect to the
// package's var initialisers — the selection and search-highlight colors
// are nil until the first Apply — so there is no un-apply. Re-applying
// dark is the convention every existing test in the tree uses
// (reducer_search_test.go, messages/render_test.go, imgrender_test.go).
func newGoldenApp(t *testing.T, opts ...testOpt) *App {
	t.Helper()

	// Package-level globals, pinned before construction so that any
	// render triggered during construction already sees them.
	styles.Apply("dark", config.Theme{})
	emoji.SetImageMode(false, 2)
	messages.SetNowFunc(goldenClock)
	t.Cleanup(resetRenderGlobals)

	// withRender() renders inside buildTestApp, i.e. BEFORE the per-App
	// pins below get a chance to run. Options only record intent into a
	// testAppCfg and the last one wins, so we replay them into a probe to
	// learn whether a render was asked for, suppress it, and re-issue it
	// ourselves once every pin is installed.
	//
	// This is insurance, not a fix for an observed bug: with the current
	// defaults none of the per-App pins changes anything (they all
	// re-assert what NewApp already set), and the sidebar clock is inert
	// until someone calls SetStaleThreshold. It becomes load-bearing the
	// moment a pin diverges from a NewApp default — which is exactly the
	// kind of change nobody would think to re-check ordering for.
	// TestNewGoldenApp_HonoursWithRender guards the re-issue.
	var probe testAppCfg
	for _, o := range opts {
		o(&probe)
	}
	// Fresh slice: appending to the caller's would let a second call
	// with the same opts slice scribble on its backing array.
	pinned := make([]testOpt, 0, len(opts)+1)
	pinned = append(pinned, opts...)
	pinned = append(pinned, func(c *testAppCfg) { c.render = false })

	a := newTestApp(t, pinned...)

	expandGoldenChannelsSection(a)
	nameGoldenActiveChannel(a)
	wireGoldenReadState(a)

	// Per-Model clock. Reachable despite `sidebar` being a value field:
	// a is a *App, so a.sidebar is addressable and Go takes its address
	// for the pointer-receiver method automatically.
	a.sidebar.SetNowFunc(goldenClock)

	// Per-App nondeterminism. spinnerFrame, avatarFn and imgProtocol all
	// already hold these values after NewApp; assigning them anyway
	// makes the golden contract explicit and survives a change to
	// NewApp's defaults.
	a.spinnerFrame = 0
	a.avatarFn = nil
	// There is no ProtoNone; ProtoOff is the zero value and the one
	// protocol that emits no escape sequences (renderer.go:19).
	a.imgProtocol = imgpkg.ProtoOff
	a.SetNowTimestampFormatter(func() string { return goldenClock().Format("3:04 PM") })

	if probe.render {
		_ = a.View()
	}
	return a
}

// goldenSidebarSection is the sidebar section goldenChannels puts its
// channel rows in. It is the package default name
// (sidebar/model.go:22), which matters: sidebar.New starts exactly this
// section — and "Apps" — collapsed (model.go:565).
const goldenSidebarSection = "Channels"

// expandGoldenChannelsSection un-collapses the default "Channels"
// section so its rows actually render.
//
// This is not cosmetic. sidebar.New collapses "Channels" by default, so
// a golden built straight out of newTestApp shows "▸ Channels" and
// nothing beneath it: IsStarred, IsMuted and plain-channel row
// rendering are pinned by nothing at all, and Task 6 would bless an
// empty section as though it were coverage.
//
// Conditional rather than an unconditional ToggleCollapse: the sidebar
// only offers a flip, so calling it blind would *collapse* the section
// the day someone changes the default. Rendering is asserted by
// TestNewGoldenApp_SidebarRendersEveryFixtureRow.
func expandGoldenChannelsSection(a *App) {
	if a.sidebar.IsCollapsed(goldenSidebarSection) {
		a.sidebar.ToggleCollapse(goldenSidebarSection)
	}
}

// nameGoldenActiveChannel gives the active channel a display name.
//
// withActiveChannel only assigns a.activeChannelID; nothing in the App
// derives a name from an ID, so the messages-pane header renders as a
// bare "#", the statusbar as "#", and the compose placeholder as
// "Message #...". Task 6 would pin that emptiness.
//
// The name is looked up from the sidebar items rather than taken as a
// parameter so it cannot drift from goldenChannels, and it is pushed
// through the same three setters production uses on the initial-channel
// path (App.SetInitialChannel, app.go:2531-2536). No-op when no active
// channel was requested, or when its ID is not in the sidebar.
func nameGoldenActiveChannel(a *App) {
	if a.activeChannelID == "" {
		return
	}
	for _, it := range a.sidebar.Items() {
		if it.ID != a.activeChannelID {
			continue
		}
		a.messagepane.SetChannel(it.Name, "")
		a.compose.SetChannel(it.Name)
		a.statusbar.SetChannel(it.Name)
		return
	}
}

// wireGoldenReadState installs goldenReadState through the same setter
// production uses (App.SetReadStateReader, app.go:2047, which forwards
// to sidebar.SetReadStateReader).
//
// Without a reader the sidebar's readStateReader is nil, every lookup
// returns the zero cache.ReadState, and so every row renders as read
// (sidebar/model.go:1200). That is not a neutral default: it makes the
// entire unread half of the sidebar's row renderer unreachable — the
// "●" dot, the bold attribute, ChannelUnread vs ChannelNormal, and the
// styled-vs-plain prefix fork at model.go:1358-1370. It also renders
// ChannelItem.IsMuted inert, because ChannelMuted and ChannelNormal are
// byte-identical styles and mute is only observable as the SUPPRESSION
// of an unread dot (IsVisiblyUnread = HasUnread && !IsMuted,
// model.go:59).
//
// TestNewGoldenApp_SidebarPinsUnreadAndMuteIndicators asserts both
// halves are actually on screen.
func wireGoldenReadState(a *App) {
	a.SetReadStateReader(goldenReadState)
}

// goldenTS builds a Slack timestamp offset from goldenClock.
//
// Fixture timestamps are derived rather than written as literals for two
// reasons, both learned the hard way from the values this task's brief
// proposed:
//
//  1. The day-divider path reads DateFromTS(msg.TS), NOT msg.DateStr
//     (messages/model.go:1776). The brief's hand-written TS constants
//     were 2024 epochs paired with 2026 DateStr values, so the fixture
//     rendered "Friday, March 15, 2024" instead of "Today".
//  2. DateFromTS formats in the LOCAL zone (model.go:3473), so an
//     absolute epoch literal names a different calendar day in each
//     zone. Deriving from goldenClock — which is itself local midday —
//     makes every row land on a fixed local calendar day everywhere.
func goldenTS(offset time.Duration) string {
	return fmt.Sprintf("%d.000100", goldenClock().Add(offset).Unix())
}

// goldenMessages is the shared fixture for every scenario: one message
// per interesting render branch, so a single content edit re-blesses
// all scenarios consistently instead of letting them drift.
//
// Row 1 lands on the day before goldenClock ("Yesterday"), the rest on
// goldenClock itself ("Today"), so any scenario rendering this fixture
// exercises both relative day labels.
//
// Timestamp is a pre-formatted display string, deliberately NOT derived
// from TS: in production it is the message time rendered in the user's
// local zone, and re-deriving it here would make every golden
// timezone-dependent.
//
// DateStr is inert *here* and only here: the day-divider path reads
// DateFromTS(msg.TS) (messages/model.go:1776), never DateStr. The field
// is not dead in production — internal/export/markdown.go:33 writes it
// into every exported thread — so it is set to a truthful value rather
// than left blank.
func goldenMessages() []messages.MessageItem {
	return []messages.MessageItem{
		{
			TS: goldenTS(-24 * time.Hour), UserID: "U1", UserName: "alice",
			Text: "morning all", Timestamp: "9:00 AM", DateStr: "2026-03-14",
		},
		{
			TS: goldenTS(0), UserID: "U2", UserName: "bob",
			Text: "shipped the thing", Timestamp: "9:00 AM", DateStr: "2026-03-15",
			Reactions: []messages.ReactionItem{
				{Emoji: "tada", Count: 3, UserIDs: []string{"U1", "U3", "U4"}},
				{Emoji: "eyes", Count: 1, HasReacted: true, UserIDs: []string{"U1"}},
			},
		},
		{
			TS: goldenTS(time.Minute), UserID: "U3", UserName: "carol",
			Text: "nice — see thread", Timestamp: "9:01 AM", DateStr: "2026-03-15",
			ThreadTS: goldenTS(time.Minute), ReplyCount: 4,
		},
		// The bot-shaped row. It pins three separate renderer branches
		// on one message:
		//
		//   - the FILE ATTACHMENT branch (msg.Attachments), and
		//   - the LEGACY ATTACHMENT branch (msg.LegacyAttachments →
		//     blockkit.RenderLegacy, model.go:2216), which draws the
		//     colored "█" stripe and the bold title.
		//
		// It also carries msg.Blocks, which routes through
		// blockkit.Render (model.go:2190) — a DIFFERENT splice than
		// RenderLegacy and one that runs before it, between the body
		// text and the file attachment.
		//
		// The name "deploybot" is flavour only; there is no bot branch
		// to hit. messages.MessageItem carries no bot discriminator (the
		// only IsBot in the tree is on ui/msgs.go's wire types) and the
		// renderer's sole subtype branch is "thread_broadcast"
		// (model.go:2156), so a message from a bot renders
		// byte-identically to one from a human. The legacy attachment IS
		// therefore the only way bot-SHAPED output gets pinned at all,
		// which is why it lives here rather than nowhere.
		//
		// The section block's text is mrkdwn, not plain: the `*...*`
		// is consumed by ctx.RenderText on the way through, which is
		// how the golden pins that blockkit's host wiring
		// (Context.RenderText → messages.RenderSlackMarkdown) is
		// connected at all. Rendered output is "rollout: 100% of
		// shards on v2.4.1" with "rollout" bold.
		//
		// The legacy attachment carries Color + Title but no Text.
		// One line each is deliberate: the two paths differ in which
		// renderer they enter, not in how many rows they can produce,
		// so a second row of either buys nothing.
		//
		// Both of these cost vertical space, which is why base is 36
		// rows and not the 30 it started at. Earlier revisions of this
		// fixture treated the height as fixed and dropped coverage to
		// fit; that is backwards. The height is a free parameter — if
		// a future row pushes the "Yesterday" divider off the top of
		// base (TestNewGoldenApp_PinsDateSeparatorClock catches it),
		// raise goldenBaseH rather than delete the row.
		//
		// Deliberately image-free: an ImageURL would route through
		// ctx.Fetcher and an async tea.Cmd, which is exactly the kind of
		// nondeterminism a golden cannot tolerate. Footer/TS are omitted
		// for the same reason in miniature — the footer formats TS as a
		// local-zone time, which would make the golden zone-dependent.
		{
			TS: goldenTS(2 * time.Minute), UserID: "B1", UserName: "deploybot",
			Text: "build #421 green", Timestamp: "9:02 AM", DateStr: "2026-03-15",
			Blocks: []blockkit.Block{
				blockkit.SectionBlock{Text: "*rollout*: 100% of shards on v2.4.1"},
			},
			Attachments: []messages.Attachment{
				{Kind: "file", Name: "build.log", URL: "https://example.invalid/build.log", Size: 20480},
			},
			LegacyAttachments: []blockkit.LegacyAttachment{
				{Color: "good", Title: "deploy #421 succeeded"},
			},
		},
		{
			TS: goldenTS(3 * time.Minute), UserID: "U1", UserName: "alice",
			Text:      "this line is deliberately long enough that it must wrap at every width the golden scenarios exercise, which is what pins the wrapping behaviour",
			Timestamp: "9:03 AM", DateStr: "2026-03-15", IsEdited: true,
		},
	}
}

// goldenChannels is the shared sidebar fixture.
//
// The three channels sit in the package-default "Channels" section,
// which sidebar.New starts COLLAPSED (model.go:565). newGoldenApp
// expands it (expandGoldenChannelsSection) — without that the rows are
// not rendered at all and this fixture pins nothing.
// TestNewGoldenApp_SidebarRendersEveryFixtureRow asserts each row is
// actually on screen.
//
// What the rows pin, and — measured, not assumed — what they do not:
//
//   - Row text, section grouping, section ordering and the DM presence
//     glyphs (● active / ○ away) are pinned.
//
//   - IsStarred changes nothing. sidebar.ChannelItem.IsStarred is a
//     carrier field: nothing in internal/ui reads it at all (only
//     internal/cache does).
//
//   - IsMuted IS live, but only in combination with goldenReadState.
//     It selects styles.ChannelMuted over styles.ChannelNormal
//     (model.go:1438-1446), and those two styles are field-for-field
//     identical (styles.go:83-104 and :451-456: same Background, same
//     TextMuted foreground, same padding, neither bold), so on a READ
//     row the flag is invisible. What mute actually does is suppress
//     the unread treatment: IsVisiblyUnread is HasUnread && !IsMuted
//     (model.go:59), so a muted row never gets the "●" dot, the bold
//     attribute, or the bright foreground. That is only observable on a
//     row the read state marks unread — hence goldenReadState marks C2
//     (unmuted) and C3 (muted) unread, giving one row of each kind.
//
// IsStarred is kept (production data carries it) and pinned as inert by
// TestNewGoldenApp_SidebarRendersEveryFixtureRow's subtest, so wiring it
// up later surfaces as a test failure and a golden diff instead of a
// silent change in what the goldens mean.
//
// Every item carries an explicit Section, which as a side effect exempts
// all of them from the sidebar's staleness filter
// (sidebar/staleness.go:49). That keeps the fixture stable, but it also
// means this fixture cannot exercise the sidebar clock;
// TestNewGoldenApp_PinsSidebarStalenessClock uses its own items for that.
func goldenChannels() []sidebar.ChannelItem {
	return []sidebar.ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", Section: "Channels"},
		{ID: "C2", Name: "engineering", Type: "channel", Section: "Channels", IsStarred: true},
		{ID: "C3", Name: "muted-noise", Type: "channel", Section: "Channels", IsMuted: true},
		{ID: "D1", Name: "bob", Type: "dm", Section: "DMs", Presence: "active", DMUserID: "U2"},
		{ID: "D2", Name: "carol", Type: "dm", Section: "DMs", Presence: "away", DMUserID: "U3"},
	}
}

// goldenReadState is the per-channel read state paired with
// goldenChannels. newGoldenApp installs it (wireGoldenReadState) so
// every scenario shares one set of unread rows.
//
// Two rows are unread, deliberately chosen to straddle the mute
// predicate (sidebar/model.go:59, IsVisiblyUnread):
//
//   - C2 "engineering" — unread and unmuted. Renders the "●" dot, the
//     bold attribute and the bright ChannelUnread foreground.
//   - C3 "muted-noise" — unread and MUTED. The dot, the bold and the
//     bright foreground are all suppressed; the row is styled
//     ChannelMuted. This is the only configuration in which IsMuted
//     changes a single byte of output.
//
// The remaining three rows (C1, D1, D2) have no entry, which a nil-safe
// map lookup reports as the zero ReadState — read.
//
// LastReadTS is set for truthfulness rather than effect: nothing in the
// row renderer reads it, and the one consumer that does (the staleness
// filter) exempts every item carrying an explicit Section
// (sidebar/staleness.go:49), which all of goldenChannels' items do. It
// is derived from goldenClock rather than written as a literal for the
// same reason goldenTS exists.
func goldenReadState() map[string]cache.ReadState {
	return map[string]cache.ReadState{
		"C2": {LastReadTS: goldenTS(-2 * time.Hour), HasUnread: true},
		"C3": {LastReadTS: goldenTS(-2 * time.Hour), HasUnread: true},
	}
}

// goldenBaseW and goldenBaseH are the terminal size of the base
// scenario AND of goldenFixtureOpts, which is why they are a constant
// rather than two independent literals.
//
// The height is 36, not buildTestApp's 120x30 default. At 30 rows the
// fixture's content is one row taller than the messages viewport and
// the "── Yesterday ──" divider scrolls off the top — which base is
// specifically required to show, and which several assertions here
// (TestNewGoldenApp_PinsDateSeparatorClock) depend on. 36 leaves 7
// spare content rows, so the next fixture row is an edit rather than a
// negotiation.
//
// Both must move together: goldenFixtureOpts is what the determinism
// and wiring assertions render, and if it drifted from base's geometry
// those assertions would be checking a frame no golden records.
const (
	goldenBaseW = 120
	goldenBaseH = 36
)

// goldenFixtureOpts is the standard scenario the determinism tests
// render: sidebar, active channel, and one message per interesting
// render branch, at the base scenario's geometry. Shared so every
// determinism assertion exercises the same surface area, and so a
// scenario that stops covering a pinned global fails loudly in one
// place rather than silently everywhere.
func goldenFixtureOpts() []testOpt {
	return []testOpt{
		withSize(goldenBaseW, goldenBaseH),
		withChannels(goldenChannels()...),
		withActiveChannel("C1"),
		withMessages(goldenMessages()...),
		withRender(),
	}
}

// resetRenderGlobals restores every package-level global newGoldenApp
// pins to the value the rest of this package's tests expect. Matches
// newGoldenApp's own cleanup exactly; see the comment there on why
// "restore" means "re-apply dark" and not "restore the pristine
// zero value".
func resetRenderGlobals() {
	styles.Apply("dark", config.Theme{})
	emoji.SetImageMode(false, 2)
	messages.SetNowFunc(nil)
}

func TestNewGoldenApp_IsDeterministic(t *testing.T) {
	render := func() string { return newGoldenApp(t, goldenFixtureOpts()...).View().Content }

	want := render()
	if want == "" {
		t.Fatal("golden app rendered an empty view; the fixture proves nothing")
	}
	// Several repetitions, not one pairwise comparison: a global that
	// only diverges on the third construction (a lazily-initialised
	// cache, a counter feeding a cache key) survives a single a1-vs-a2
	// check.
	for i := 2; i <= 6; i++ {
		if got := render(); got != want {
			t.Fatalf("golden app #%d rendered differently from #1: %s", i, styleAwareDiff(want, got))
		}
	}
}

// TestNewGoldenApp_IsOrderIndependent builds a differently-configured
// golden app in between two identical ones. A pairwise a1/a2 comparison
// cannot see state that leaks from one App's construction into the
// next; this can.
func TestNewGoldenApp_IsOrderIndependent(t *testing.T) {
	first := newGoldenApp(t, goldenFixtureOpts()...).View().Content

	// Deliberately different: other size, other mode, no channels.
	_ = newGoldenApp(t, withSize(60, 20), withMode(ModeInsert), withRender()).View()
	_ = newGoldenApp(t, withMessages(), withChannels(), withRender()).View()

	third := newGoldenApp(t, goldenFixtureOpts()...).View().Content
	if third != first {
		t.Errorf("an intervening differently-configured golden app changed the render: %s",
			styleAwareDiff(first, third))
	}
}

// TestNewGoldenApp_ViewIsIdempotent pins that View() has no observable
// side effect on its own output. Goldens are captured with a single
// View() call, but withRender() already made one; if the second differs
// from the first, every golden is capturing a warm-cache render that a
// fresh one would not reproduce.
func TestNewGoldenApp_ViewIsIdempotent(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)
	first := a.View().Content
	for i := 2; i <= 4; i++ {
		if got := a.View().Content; got != first {
			t.Fatalf("View() call #%d differs from #1: %s", i, styleAwareDiff(first, got))
		}
	}
}

// TestNewGoldenApp_NeutralisesHostileGlobals is the test that proves the
// pins are load-bearing rather than decorative.
//
// For each package-level global newGoldenApp pins, it corrupts the
// global, then asserts two things:
//
//  1. an UNPINNED app (newTestApp) renders differently — otherwise the
//     corruption is invisible and the pin is untested, which the test
//     reports rather than quietly passing;
//  2. a PINNED app (newGoldenApp) renders identically to the baseline.
func TestNewGoldenApp_NeutralisesHostileGlobals(t *testing.T) {
	baseline := newGoldenApp(t, goldenFixtureOpts()...).View().Content

	cases := []struct {
		name    string
		corrupt func()
	}{
		{"styles theme", func() { styles.Apply("light", config.Theme{Primary: "#FF0000"}) }},
		{"emoji image mode", func() { emoji.SetImageMode(true, 1) }},
		{"messages day-divider clock", func() {
			messages.SetNowFunc(func() time.Time { return time.Date(1999, 12, 31, 23, 59, 0, 0, time.UTC) })
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Cleanup(resetRenderGlobals)
			c.corrupt()

			if unpinned := newTestApp(t, goldenFixtureOpts()...).View().Content; unpinned == baseline {
				t.Fatalf("corrupting %s did not change an unpinned render; "+
					"this case proves nothing about the pin", c.name)
			}
			if pinned := newGoldenApp(t, goldenFixtureOpts()...).View().Content; pinned != baseline {
				t.Errorf("newGoldenApp failed to neutralise %s: %s",
					c.name, styleAwareDiff(baseline, pinned))
			}
		})
	}
}

// TestNewGoldenApp_RevertsGlobalsOnCleanup pins the other half of the
// contract: a golden test must not leave the package's globals pinned
// for whatever test runs next. ~90% of this package's tests read App
// state directly and none of them expect a 2026 clock.
//
// The whole-render comparison is the general assertion — it catches any
// global newGoldenApp pins but forgets to revert, including the theme,
// which has no getter to probe. The two explicit probes after it name
// the individual globals so a failure says which one leaked.
func TestNewGoldenApp_RevertsGlobalsOnCleanup(t *testing.T) {
	resetRenderGlobals()
	// Reference render under the package's canonical globals and the real
	// wall clock — exactly the state a golden test must hand back.
	//
	// Not flaky across a local midnight: goldenMessages is anchored six
	// months before any plausible wall clock, so its day dividers are
	// absolute dates ("Sunday, March 15, 2026"), not the relative labels
	// that would flip at midnight.
	want := newTestApp(t, goldenFixtureOpts()...).View().Content

	t.Run("inner", func(t *testing.T) {
		emoji.SetImageMode(true, 1)
		messages.SetNowFunc(func() time.Time { return time.Date(1999, 12, 31, 23, 59, 0, 0, time.UTC) })
		_ = newGoldenApp(t, goldenFixtureOpts()...)
		if !strings.Contains(messages.FormatDateSeparator("2026-03-15"), "Today") {
			t.Fatal("precondition: inside the subtest the clock should be pinned to 2026-03-15")
		}
	})

	if got := newTestApp(t, goldenFixtureOpts()...).View().Content; got != want {
		t.Errorf("a global was left mutated after the golden app's cleanup ran: %s",
			styleAwareDiff(want, got))
	}
	if emoji.ImageModeActive() {
		t.Error("emoji image mode still active after the golden app's cleanup ran")
	}
	if got := messages.FormatDateSeparator(time.Now().Format("2006-01-02")); got != "Today" {
		t.Errorf("day-divider clock not reverted to time.Now: FormatDateSeparator(today) = %q, want \"Today\"", got)
	}
}

// TestNewGoldenApp_PinsDateSeparatorClock checks the pinned clock is
// actually visible in the render: goldenMessages straddles the pinned
// "today" (2026-03-15) and the day before it.
func TestNewGoldenApp_PinsDateSeparatorClock(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)
	plain := stripANSI(a.View().Content)
	for _, want := range []string{"Today", "Yesterday"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected a %q day divider under the pinned clock; view was:\n%s", want, plain)
		}
	}
}

// goldenPanelText returns display columns [from, to) of every line of a
// rendered view, as plain text.
//
// Region-sliced rather than substring-matched over the whole frame,
// because the sidebar and the messages pane occupy the SAME lines: a
// bare strings.Contains(view, "# general") matches the sidebar row and
// the pane header indiscriminately, so it would keep passing with
// either one rendering nothing — the exact failure mode these tests
// exist to catch.
//
// Uses the shared grapheme-correct column slicer (AGENTS.md:
// messages.PlainLines / messages.SliceColumns) rather than a private
// rune-slicing copy; the sidebar contains ● / ○ / ▾ and the pane
// contains emoji reaction pills.
func goldenPanelText(view string, from, to int) string {
	var b strings.Builder
	for _, pl := range messages.PlainLines(view) {
		b.WriteString(messages.SliceColumns(pl, from, to))
		b.WriteByte('\n')
	}
	return b.String()
}

// TestNewGoldenApp_SidebarRendersEveryFixtureRow is the assertion that
// stops goldenChannels from being a fixture that pins nothing.
//
// sidebar.New starts the default "Channels" section collapsed
// (sidebar/model.go:565), so before expandGoldenChannelsSection the
// sidebar rendered "▸ Channels" and no rows at all — a golden blessed
// from that would have recorded an empty section as if it were channel
// coverage.
func TestNewGoldenApp_SidebarRendersEveryFixtureRow(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)
	sb := goldenPanelText(a.View().Content, 0, a.layout.sidebarEnd)

	// Section headers expanded, all three channel rows, both DM rows
	// with their presence glyphs.
	for _, want := range []string{
		"▾ Channels",
		"# general",
		"# engineering",
		"# muted-noise",
		"▾ DMs",
		"● bob",
		"○ carol",
	} {
		if !strings.Contains(sb, want) {
			t.Errorf("sidebar does not render %q; sidebar band was:\n%s", want, sb)
		}
	}

	// Control: the same fixture through newTestApp, which does not
	// expand the section. If the rows showed up here too, the
	// expansion would be redundant and the assertions above vacuous.
	ctrl := newTestApp(t, goldenFixtureOpts()...)
	csb := goldenPanelText(ctrl.View().Content, 0, ctrl.layout.sidebarEnd)
	if !strings.Contains(csb, "▸ Channels") {
		t.Fatalf("control: an unexpanded sidebar should show a collapsed \"▸ Channels\" header; band was:\n%s", csb)
	}
	if strings.Contains(csb, "# engineering") {
		t.Fatalf("control: an unexpanded sidebar already renders channel rows, so "+
			"expandGoldenChannelsSection proves nothing; band was:\n%s", csb)
	}

	// IsStarred is inert: sidebar.ChannelItem.IsStarred is a carrier
	// field that nothing in internal/ui reads (only internal/cache
	// does). Pinned as inert so that wiring it up later surfaces here,
	// and in a golden diff, instead of silently changing what the
	// goldens mean. A failure below is not necessarily a bug: update
	// goldenChannels' doc comment and re-bless.
	//
	// IsMuted used to be listed here too. It no longer is: since
	// newGoldenApp installs goldenReadState, C3 is unread-and-muted and
	// the flag suppresses a dot that would otherwise render. That is the
	// point of the pairing; see
	// TestNewGoldenApp_SidebarPinsUnreadAndMuteIndicators.
	t.Run("starred is inert", func(t *testing.T) {
		render := func(v bool) string {
			items := goldenChannels()
			for i := range items {
				items[i].IsStarred = v
			}
			return newGoldenApp(t, withChannels(items...), withActiveChannel("C1"),
				withMessages(goldenMessages()...), withRender()).View().Content
		}
		if on, off := render(true), render(false); on != off {
			t.Errorf("IsStarred now affects the render, contradicting goldenChannels' doc comment: %s",
				styleAwareDiff(off, on))
		}
	})
}

// goldenSidebarRow returns the single rendered sidebar line containing
// token, or fails the test.
//
// Row-scoped rather than whole-band, because the assertions below are
// about the presence of a "●" glyph and the sidebar has two other
// legitimate sources of one: the DM presence prefix on "bob" and the
// Threads-row unread badge. A band-wide strings.Contains would pass on
// either of those and prove nothing about the channel it names.
//
// token is the full row prefix ("# general"), not the bare name: the
// column band runs the full height of the frame, so it includes the
// status row, and the status row renders the active channel as
// "#general". Matching the bare name found two lines and the uniqueness
// check below turned that into a confusing failure.
func goldenSidebarRow(t *testing.T, a *App, token string) string {
	t.Helper()
	band := goldenPanelText(a.View().Content, 0, a.layout.sidebarEnd)
	var hits []string
	for _, line := range strings.Split(band, "\n") {
		if strings.Contains(line, token) {
			hits = append(hits, line)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("expected exactly one sidebar row containing %q, found %d; band was:\n%s",
			token, len(hits), band)
	}
	return hits[0]
}

// unreadDotGlyph is the sidebar's unread indicator (sidebar/model.go:1236).
const unreadDotGlyph = "●"

// TestNewGoldenApp_SidebarPinsUnreadAndMuteIndicators is the assertion
// that makes goldenReadState and ChannelItem.IsMuted load-bearing rather
// than decorative fixture data.
//
// Before the read state was wired, every sidebar row rendered as read:
// the "●" dot, the bold attribute and the ChannelUnread foreground were
// unreachable, and IsMuted could not change a byte (ChannelMuted and
// ChannelNormal are field-for-field identical, so mute is only visible
// as the ABSENCE of unread treatment). A golden blessed from that state
// would have pinned half of the row renderer as dead code.
func TestNewGoldenApp_SidebarPinsUnreadAndMuteIndicators(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)

	// C2: unread and unmuted — the dot must be there.
	if row := goldenSidebarRow(t, a, "# engineering"); !strings.Contains(row, unreadDotGlyph) {
		t.Errorf("unread channel row has no %q indicator: %q", unreadDotGlyph, row)
	}
	// C3: unread but MUTED — the dot must be suppressed.
	if row := goldenSidebarRow(t, a, "# muted-noise"); strings.Contains(row, unreadDotGlyph) {
		t.Errorf("unread-but-muted channel row shows the %q indicator; mute should suppress it: %q",
			unreadDotGlyph, row)
	}
	// C1: no read-state entry at all — read, so no dot either. Without
	// this the two assertions above are consistent with a renderer that
	// simply never draws a dot for "muted-noise" for some unrelated
	// reason.
	if row := goldenSidebarRow(t, a, "# general"); strings.Contains(row, unreadDotGlyph) {
		t.Errorf("read channel row shows the %q indicator: %q", unreadDotGlyph, row)
	}

	// Control: unmute C3 and the dot appears. This is what proves the
	// suppression above is IsMuted's doing and not a missing read-state
	// entry, a name typo, or a row that is simply scrolled out of view.
	items := goldenChannels()
	for i := range items {
		if items[i].ID == "C3" {
			items[i].IsMuted = false
		}
	}
	unmuted := newGoldenApp(t, withChannels(items...), withActiveChannel("C1"),
		withMessages(goldenMessages()...), withRender())
	if row := goldenSidebarRow(t, unmuted, "# muted-noise"); !strings.Contains(row, unreadDotGlyph) {
		t.Errorf("control: with IsMuted cleared the unread row still has no %q indicator, "+
			"so the mute assertion above proves nothing: %q", unreadDotGlyph, row)
	}

	// Control: with no reader installed nothing is unread, which is the
	// state Task 5 left behind. If the dot showed up here too,
	// wireGoldenReadState would be redundant.
	ctrl := newTestApp(t, goldenFixtureOpts()...)
	ctrlBand := goldenPanelText(ctrl.View().Content, 0, ctrl.layout.sidebarEnd)
	for _, line := range strings.Split(ctrlBand, "\n") {
		if strings.Contains(line, "# engineering") && strings.Contains(line, unreadDotGlyph) {
			t.Fatalf("control: an unwired sidebar already renders an unread dot, so "+
				"wireGoldenReadState proves nothing; row was: %q", line)
		}
	}
}

// TestNewGoldenApp_RendersActiveChannelName pins the fix for a header
// that used to render as a bare "#": withActiveChannel only assigns
// a.activeChannelID, and nothing in the App derives a display name from
// an ID, so every pane that shows the channel name showed an empty one.
func TestNewGoldenApp_RendersActiveChannelName(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)
	view := a.View().Content

	pane := goldenPanelText(view, a.layout.sidebarEnd, a.layout.msgEnd)
	if !strings.Contains(pane, "# general") {
		t.Errorf("messages-pane header has no channel name; pane band was:\n%s", pane)
	}
	// The compose placeholder and the statusbar read the same name from
	// two other setters, so a partial wiring (header only) still fails.
	plain := stripANSI(view)
	if !strings.Contains(plain, "Message #general") {
		t.Errorf("compose placeholder has no channel name; view was:\n%s", plain)
	}
	if !strings.Contains(plain, "NORMAL    #general") {
		t.Errorf("statusbar has no channel name; view was:\n%s", plain)
	}

	// Control: unnamed through newTestApp.
	ctrl := newTestApp(t, goldenFixtureOpts()...)
	cpane := goldenPanelText(ctrl.View().Content, ctrl.layout.sidebarEnd, ctrl.layout.msgEnd)
	if strings.Contains(cpane, "# general") {
		t.Fatalf("control: newTestApp already names the channel, so nameGoldenActiveChannel "+
			"proves nothing; pane band was:\n%s", cpane)
	}
}

// TestNewGoldenApp_RenderIsTimezoneIndependent pins the property that
// makes checked-in goldens portable: the same fixture must render the
// same bytes on a developer's laptop and on CI, whatever TZ each is set
// to.
//
// It swaps time.Local directly rather than shelling out with TZ= so the
// whole offset range is covered in one process, including UTC+12..+14
// where a UTC-anchored clock silently collapses the "Yesterday" divider
// into a second "Today". Safe here because this package's tests never
// run in parallel and nothing else reads time.Local concurrently.
func TestNewGoldenApp_RenderIsTimezoneIndependent(t *testing.T) {
	prev := time.Local
	t.Cleanup(func() { time.Local = prev })

	zones := []struct {
		name    string
		offsetH int
	}{
		{"UTC", 0},
		{"Pacific/Honolulu", -10},
		{"Etc/GMT+12", -12}, // westernmost real offset
		{"Asia/Tokyo", +9},
		{"Pacific/Auckland (NZDT)", +13},
		{"Pacific/Kiritimati", +14}, // easternmost real offset
	}

	var want, wantZone string
	for _, z := range zones {
		time.Local = time.FixedZone(z.name, z.offsetH*3600)
		got := newGoldenApp(t, goldenFixtureOpts()...).View().Content
		if want == "" {
			want, wantZone = got, z.name
			continue
		}
		if got != want {
			t.Errorf("render under %s differs from %s: %s", z.name, wantZone, styleAwareDiff(want, got))
		}
	}
}

// TestNewGoldenApp_PinsSidebarStalenessClock covers the one clock that
// is per-Model rather than package-level. It is invisible in the default
// fixture (no stale threshold is configured, and IsStale exempts every
// item carrying an explicit Section, which goldenChannels all do), so it
// gets its own bespoke items.
func TestNewGoldenApp_PinsSidebarStalenessClock(t *testing.T) {
	// Section deliberately empty: staleness.go:49 exempts sectioned items.
	items := []sidebar.ChannelItem{{ID: "C-old", Name: "old-project", Type: "channel"}}
	// One day before the pinned clock: fresh against goldenClock,
	// long stale against the real one.
	lastRead := fmt.Sprintf("%d.000000", goldenClock().Add(-24*time.Hour).Unix())
	readState := map[string]cache.ReadState{"C-old": {LastReadTS: lastRead}}

	visible := func(a *App) bool {
		a.sidebar.SetReadStateReader(func() map[string]cache.ReadState { return readState })
		a.sidebar.SetStaleThreshold(30 * 24 * time.Hour)
		for _, it := range a.sidebar.VisibleItems() {
			if it.ID == "C-old" {
				return true
			}
		}
		return false
	}

	// Control: without the pin the sidebar reads the wall clock and the
	// channel is months stale. If this ever stops holding the assertion
	// below is vacuous, so it is checked rather than assumed.
	if visible(newTestApp(t, withChannels(items...))) {
		t.Fatalf("control: an unpinned sidebar kept %q visible, so the pinned "+
			"assertion below proves nothing (real now = %s, goldenClock = %s)",
			"C-old", time.Now().Format(time.RFC3339), goldenClock().Format(time.RFC3339))
	}
	if !visible(newGoldenApp(t, withChannels(items...))) {
		t.Error("sidebar staleness clock not pinned: a channel read one day before " +
			"goldenClock was filtered out as stale")
	}
}

// TestNewGoldenApp_HonoursWithRender pins the ordering fix: newGoldenApp
// suppresses newTestApp's own render so that every per-App pin is
// installed BEFORE the first View(), then renders itself. If it stopped
// rendering, layout bands would be empty and Task 6's hit-testing
// scenarios would silently drift.
func TestNewGoldenApp_HonoursWithRender(t *testing.T) {
	if a := newGoldenApp(t, withRender()); a.layout.sidebarEnd == 0 {
		t.Error("withRender() did not populate layout bands through newGoldenApp")
	}
	if a := newGoldenApp(t); a.layout.sidebarEnd != 0 {
		t.Error("newGoldenApp rendered without withRender()")
	}
}

// TestNewGoldenApp_MessagePaneRendersLegacyAttachment pins the Block Kit
// half of goldenMessages.
//
// msg.LegacyAttachments routes through blockkit.RenderLegacy
// (messages/model.go:2216), a branch neither the plain-text rows nor the
// msg.Attachments file row reaches. It is also the only way bot-SHAPED
// output gets pinned at all: MessageItem carries no bot discriminator,
// so a bot's plain message is byte-identical to a human's.
//
// Asserted rather than assumed because the fixture value is silently
// droppable — an empty Title, a zero-width pane, or a future guard in
// the splice site would all leave the field set and the output gone.
func TestNewGoldenApp_MessagePaneRendersLegacyAttachment(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)
	pane := goldenPanelText(a.View().Content, a.layout.sidebarEnd, a.layout.msgEnd)

	// The stripe glyph and the title on the same line: the title alone
	// would also match a plain-text message body, and the stripe alone
	// is a single common character.
	const want = "█ deploy #421 succeeded"
	if !strings.Contains(pane, want) {
		t.Errorf("legacy attachment not rendered; expected a line containing %q. Pane band was:\n%s", want, pane)
	}

	// Control: with the field cleared the stripe disappears, so the
	// assertion above is about LegacyAttachments and not about some
	// other fixture row that happens to contain the same text.
	msgs := goldenMessages()
	for i := range msgs {
		msgs[i].LegacyAttachments = nil
	}
	ctrl := newGoldenApp(t, withChannels(goldenChannels()...), withActiveChannel("C1"),
		withMessages(msgs...), withRender())
	cpane := goldenPanelText(ctrl.View().Content, ctrl.layout.sidebarEnd, ctrl.layout.msgEnd)
	if strings.Contains(cpane, want) {
		t.Fatalf("control: the stripe renders with LegacyAttachments cleared, so the "+
			"assertion above proves nothing. Pane band was:\n%s", cpane)
	}
}

// TestNewGoldenApp_MessagePaneRendersBlockKitSection pins the OTHER
// Block Kit splice: msg.Blocks → blockkit.Render (messages/model.go:2190).
//
// It is a different call site from the LegacyAttachments one
// (RenderLegacy, model.go:2216), runs earlier in the same per-message
// assembly, and takes a different Context field path — the section's
// mrkdwn goes through Context.RenderText, which the legacy title does
// not. A fixture that exercised only the legacy branch left this one
// pinned by nothing.
//
// The asserted text is the mrkdwn-RENDERED form: the fixture's
// "*rollout*: ..." loses its asterisks on the way through
// RenderText, so matching this string also proves the host wired
// Context.RenderText rather than letting raw text fall through.
func TestNewGoldenApp_MessagePaneRendersBlockKitSection(t *testing.T) {
	a := newGoldenApp(t, goldenFixtureOpts()...)
	pane := goldenPanelText(a.View().Content, a.layout.sidebarEnd, a.layout.msgEnd)

	const want = "rollout: 100% of shards on v2.4.1"
	if !strings.Contains(pane, want) {
		t.Errorf("Block Kit section not rendered; expected a line containing %q. Pane band was:\n%s", want, pane)
	}
	// The asterisks must be gone: their survival would mean
	// Context.RenderText was nil and raw mrkdwn reached the screen.
	if strings.Contains(pane, "*rollout*") {
		t.Errorf("Block Kit section text was not run through Context.RenderText; "+
			"raw mrkdwn reached the screen. Pane band was:\n%s", pane)
	}

	// Control: with msg.Blocks cleared the line disappears, so the
	// assertion above is about the Blocks field and not about some
	// other fixture row that happens to contain the same text.
	msgs := goldenMessages()
	for i := range msgs {
		msgs[i].Blocks = nil
	}
	ctrl := newGoldenApp(t, withSize(goldenBaseW, goldenBaseH), withChannels(goldenChannels()...),
		withActiveChannel("C1"), withMessages(msgs...), withRender())
	cpane := goldenPanelText(ctrl.View().Content, ctrl.layout.sidebarEnd, ctrl.layout.msgEnd)
	if strings.Contains(cpane, want) {
		t.Fatalf("control: the section text renders with msg.Blocks cleared, so the "+
			"assertion above proves nothing. Pane band was:\n%s", cpane)
	}
}

// TestGolden_BaseHasVerticalHeadroom pins the reason goldenBaseH is 36
// rather than the 30 it started at.
//
// Without it, base's headroom is an accident that the next fixture edit
// spends without noticing, and the edit after that turns into the same
// "drop the coverage to fit the height" trade this fixture already made
// once and had to undo. Blank content rows are the budget; this asserts
// there is some left.
func TestGolden_BaseHasVerticalHeadroom(t *testing.T) {
	const minSpare = 4

	a := newGoldenApp(t, goldenFixtureOpts()...)
	pane := goldenPanelText(a.View().Content, a.layout.sidebarEnd, a.layout.msgEnd)

	// Count the run of blank content rows immediately above the
	// compose box. Trailing blanks inside the messages viewport are
	// exactly the unspent rows; blanks elsewhere are inter-message
	// gaps and must not be counted, hence the contiguous run rather
	// than a total.
	lines := strings.Split(pane, "\n")
	last := -1
	for i, l := range lines {
		if strings.Contains(l, "wrapping behaviour") {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("last fixture message not found in the pane band:\n%s", pane)
	}
	spare := 0
	for i := last + 1; i < len(lines); i++ {
		// A blank content row is the two vertical borders and
		// nothing but spaces between (and after) them. The cutset
		// deliberately excludes the scrollbar glyphs: a row carrying
		// a scrollbar cell is not spare capacity.
		if strings.Trim(lines[i], " │") != "" {
			break
		}
		spare++
	}
	if spare < minSpare {
		t.Errorf("base has %d spare content rows at %dx%d, want at least %d; "+
			"raise goldenBaseH rather than trimming the fixture. Pane band was:\n%s",
			spare, goldenBaseW, goldenBaseH, minSpare, pane)
	}

	// The headroom is only meaningful if nothing scrolled off the top.
	// The first divider is the one that goes first.
	if !strings.Contains(pane, "── Yesterday ──") {
		t.Errorf("the \"Yesterday\" divider is not on screen at %dx%d; content scrolled "+
			"off the top. Pane band was:\n%s", goldenBaseW, goldenBaseH, pane)
	}
}

// ---------------------------------------------------------------------
// The scenario table and the full-screen goldens.
// ---------------------------------------------------------------------

// goldenScenario is one full-screen render pinned to a file.
//
// Full-screen rather than per-region because composition — panel order,
// width distribution, border placement, overlay compositing — is what
// View() actually does, and is what the later refactor phases threaten.
//
// w and h are recorded on the scenario as well as passed to withSize
// inside build. That is redundant by construction and deliberately so:
// they are what the well-formedness guard measures the blessed file
// against, so a scenario whose build stops honouring its declared size
// fails rather than silently re-blessing at a new geometry.
type goldenScenario struct {
	name  string
	w, h  int
	build func(t *testing.T) *App

	// autoHidesThread marks a scenario that deliberately opens a
	// thread at a width too small to show it, so the golden records
	// the auto-hidden two-pane frame.
	//
	// It is an OPT-OUT from the thread-width guard, not an opt-in to
	// it. Whether a scenario opened a thread at all is derived from
	// the built App (threadPanel.IsEmpty), so every thread scenario is
	// checked by default and a new one cannot escape by not matching a
	// hardcoded name. This flag only exists so the one scenario whose
	// point IS the auto-hide can say so, and
	// TestGolden_ThreadScenariosAreWideEnough refuses to honour it on
	// a scenario wide enough to keep the pane, or on one that never
	// opened a thread — so it cannot be used to silence a real
	// failure.
	autoHidesThread bool
}

// goldenThreadMinWidth is the narrowest terminal width at which
// layout.Compute keeps the thread pane, and the reason the thread_open
// scenario is 140 columns rather than the 120 the task brief specified.
//
// Compute auto-hides the thread pane unless BOTH threadWidth >= 30 and
// the residual messages pane >= 40 (panellayout.go:107-116). With the
// 6-col workspace rail and the 30-col sidebar (+2 border) that every
// golden scenario carries, msgAreaWidth is width-38 and threadWidth is
// 35% of that, so the binding constraint is
//
//	floor((width-38) * 35 / 100) >= 30   →   width >= 124
//
// At 120 threadWidth comes out as 28 and the pane vanishes. A
// thread_open golden blessed at 120 was byte-for-byte IDENTICAL to
// base — which is exactly the failure a golden cannot report on its
// own: the file looks entirely plausible while pinning two panes under
// a name that promises three.
//
// TestGolden_ThreadScenariosAreWideEnough pins this against the real
// Compute so the constant cannot drift away from the layout code.
const goldenThreadMinWidth = 124

// goldenThreadApp builds the shared "a thread is open" App: the
// standard fixture, plus carol's message (goldenMessages()[2], the one
// carrying ThreadTS/ReplyCount) opened as the thread parent with the two
// following rows as its replies.
//
// Returned UNRENDERED. Every thread scenario shares this body, but they
// differ in what they set between the SetThread and the first View() —
// narrow focuses the thread pane so the auto-hide path has a focus to
// fall back from. Since View() is what consumes and mutates that state
// (app.go:2726-2731), the render cannot live in here without either
// forcing a second View() on the callers that need extra setup, or
// pushing every future variation in as another parameter.
//
// Note what is NOT set here: focus stays on PanelMessages. The thread
// pane renders on a.threadVisible && frame.ThreadWidth > 0 alone
// (app.go:2747); focus only picks the border color. Leaving it on the
// messages pane keeps thread_open's chrome comparable to base's.
func goldenThreadApp(t *testing.T, w, h int) *App {
	t.Helper()
	a := newGoldenApp(t,
		withSize(w, h),
		withChannels(goldenChannels()...),
		withMessages(goldenMessages()...),
		withActiveChannel("C1"),
	)
	msgs := goldenMessages()
	// SetThread(parent, replies, channelID, threadTS) — thread/model.go:309.
	// The threadTS is read off the parent rather than written as a
	// literal so it cannot drift from goldenTS.
	a.threadPanel.SetThread(msgs[2], msgs[3:5], "C1", msgs[2].ThreadTS)
	a.threadVisible = true
	return a
}

// goldenThreadScenario is goldenThreadApp plus the render, which is all
// the scenarios that want a visible thread pane need.
//
// Callers must pass a width of at least goldenThreadMinWidth or the pane
// they asked for silently auto-hides;
// TestGolden_ThreadScenariosAreWideEnough enforces that against the
// scenario table — derived from the built App, not from the scenario's
// name, so a new thread scenario cannot escape it.
func goldenThreadScenario(t *testing.T, w, h int) *App {
	t.Helper()
	a := goldenThreadApp(t, w, h)
	_ = a.View()
	return a
}

func goldenScenarios() []goldenScenario {
	return []goldenScenario{
		{
			// 36 rows, not the 30 this started at: see goldenBaseH.
			// goldenFixtureOpts already carries the size, so base is
			// exactly the shared fixture rendered to a file.
			name: "base", w: goldenBaseW, h: goldenBaseH,
			build: func(t *testing.T) *App {
				return newGoldenApp(t, goldenFixtureOpts()...)
			},
		},
		{
			// 140, not the brief's 120. Measured: at 120 the thread
			// pane AUTO-HIDES, so a 120-wide "thread_open" renders
			// byte-for-byte identically to base and pins two panes
			// while claiming to pin three. See goldenThreadMinWidth.
			name: "thread_open", w: 140, h: 30,
			build: func(t *testing.T) *App { return goldenThreadScenario(t, 140, 30) },
		},
		{
			name: "wide", w: 200, h: 50,
			build: func(t *testing.T) *App { return goldenThreadScenario(t, 200, 50) },
		},
		{
			// 80x24 forces layout.Compute to set ThreadAutoHidden,
			// which View() acts on at app.go:2726 by clearing
			// threadVisible and falling focus back to PanelMessages.
			//
			// Verified, not assumed — the arithmetic is in
			// panellayout.go:107-116. The workspace rail is 6 cols and
			// the sidebar 30 (+2 border) at every size here, so
			// msgAreaWidth is 80-6-30-2 = 42 and threadWidth is
			// 42*35/100 = 14, well below the 30-col minimum.
			// TestGolden_NarrowAutoHidesThreadPane pins the consequence
			// rather than leaving the golden as the only record of it.
			name: "narrow", w: 80, h: 24, autoHidesThread: true,
			build: func(t *testing.T) *App {
				// goldenThreadApp, not goldenThreadScenario: the
				// focus has to be on the thread pane BEFORE the
				// first View(), because View() is what drops it
				// back to PanelMessages. Rendering inside the
				// helper and re-focusing afterwards would take a
				// second View() to settle and would not exercise
				// the fallback at all.
				a := goldenThreadApp(t, 80, 24)
				a.focusedPanel = PanelThread
				_ = a.View()
				return a
			},
		},
		{
			// Base's geometry deliberately: the ONLY difference from
			// base is the missing sidebar, so a diff of the two
			// goldens is exactly the sidebar-hidden layout shift.
			name: "no_sidebar", w: goldenBaseW, h: goldenBaseH,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t, goldenFixtureOpts()...)
				// ToggleSidebar, not `a.sidebarVisible = false`: the
				// production path (app.go:1747) also clears any pinned
				// selections and falls focus back to PanelMessages when
				// the sidebar had it. Assigning the field skips both,
				// and the focus fallback is visible — it decides which
				// pane gets the thick focused border.
				a.ToggleSidebar()
				_ = a.View()
				return a
			},
		},
		{
			// The scenario that justifies storing raw ANSI: the
			// selection highlight is a run of SGR bytes laid over
			// text that does not change by a single character.
			//
			// Measured, not assumed — and NOT quite the "identical to
			// base once stripped" the task brief predicted. The press
			// also moves focus to the messages pane
			// (reducer_mouse.go:249), and focus decides which pane
			// gets the THICK border and which the rounded one, so the
			// stripped text differs from base's in the border glyphs
			// as well. The selection itself is still escapes-only;
			// TestGolden_DragSelectionIsActuallySelected proves that
			// against a baseline that holds the focus change fixed,
			// which is the comparison that can actually be exact.
			name: "drag_selection", w: goldenBaseW, h: goldenBaseH,
			build: func(t *testing.T) *App {
				return goldenDragApp(t, true)
			},
		},
		{
			// Pins applyOverlays' compositing (view_overlays.go:39):
			// a centered box drawn over a backdrop whose every cell
			// has been darkened by 50% (overlay/overlay.go:32). The
			// dim is a per-cell color rewrite, so it exists ONLY in
			// the escape sequences — stripped text cannot see it, and
			// a raw-ANSI golden is the only thing that pins it.
			//
			// MEASURED WIDTH ANOMALY, for whoever writes the
			// well-formedness guard: every other golden's rows are
			// w+6 cells wide, because the status row overruns the
			// terminal width by 6 (a known, unfixed bug). This one's
			// rows are exactly w. The difference is
			// maybeWrapFinalScreen (view_overlays.go:108), which
			// re-wraps the whole screen in a Width(a.width) style —
			// but ONLY when an overlay is active, which is exactly
			// this scenario and no other. The wrapper truncates the
			// overrun away, so the golden's last row ends
			// "● Connectin". A guard that asserts w+6 uniformly will
			// fail here, and the right predicate is a.overlayActive(),
			// not the scenario's name.
			name: "overlay_finder", w: goldenBaseW, h: goldenBaseH,
			build: func(t *testing.T) *App {
				a := newGoldenApp(t,
					withSize(goldenBaseW, goldenBaseH),
					withChannels(goldenChannels()...),
					withMessages(goldenMessages()...),
					withActiveChannel("C1"),
					// Seeds the items AND opens the overlay
					// (testapp_test.go:238); the mode is a separate
					// option because buildTestApp applies it after.
					withChannelFinderOpen(goldenFinderItems()...),
					withMode(ModeChannelFinder),
					withRender(),
				)
				return a
			},
		},
		{
			// Pins renderWindowNode's recursion and, more to the
			// point, the focused-vs-unfocused pane chrome: the
			// focused leaf renders through renderMessagesRegion with
			// styles.FocusedBorder (thick), every other leaf through
			// renderUnfocusedWindow with styles.UnfocusedBorder
			// (rounded). TestGolden_WindowSplitRendersTwoDistinctPanes
			// asserts both are actually on screen.
			//
			// 160x40: two side-by-side panes need the width, and the
			// split path is the one place a too-narrow rect degrades
			// silently rather than failing.
			name: "window_split", w: 160, h: 40,
			build: func(t *testing.T) *App {
				return goldenWindowSplitScenario(t, 160, 40)
			},
		},
	}
}

// goldenFinderItems is the channel-finder fixture, derived from
// goldenChannels so the two cannot drift.
//
// LastVisited descends with the index because the finder's empty-query
// order is LastVisited DESC (channelfinder/model.go:41-46). Deriving it
// from the position rather than writing literals means the overlay
// lists the channels in goldenChannels' own order, so a reader can
// check the golden against one fixture instead of two.
//
// One row is deliberately NOT joined. Joined selects a whole separate
// styling branch in the row renderer (channelfinder/model.go:599-609):
// a joined row gets channelPrefix + TextPrimary, a non-joined row gets
// a hardcoded dim grey over BOTH the prefix and the name. With every
// row joined that branch is unreachable and the golden pins half the
// list renderer. TestGolden_OverlayFinderIsCompositedOverBackdrop
// asserts the mixed set survives to the screen.
func goldenFinderItems() []channelfinder.Item {
	src := goldenChannels()
	out := make([]channelfinder.Item, 0, len(src))
	for i, it := range src {
		out = append(out, channelfinder.Item{
			ID:          it.ID,
			Name:        it.Name,
			Type:        it.Type,
			Presence:    it.Presence,
			Joined:      it.ID != "C3",
			LastVisited: int64(len(src) - i),
		})
	}
	return out
}

// goldenDragPressX / goldenDragPressY / goldenDragDX / goldenDragDY
// are the drag drag_selection performs: press on TERMINAL row 7, 11
// columns into the messages band, then drag 25 columns right and 2
// rows down.
//
// The lower bound on the press row is structural: panelAt subtracts
// the 1-row panel border to give a pane-local y, and BeginSelectionAt
// returns early for anything above the pane's chromeHeight (channel
// header + separator = 2 rows, see app_selection_test.go:52-56), so
// terminal y < 4 anchors on nothing.
//
// The specific value is not structural — it was measured against the
// rendered fixture, and the constraint it satisfies is that the whole
// 3-row span lands INSIDE a single message entry (bob's row: the
// author line, the body, and the reaction pills). That matters because
// applySelectionToRows (messages/model.go:3355) resolves each row
// through the cache entry covering it and silently skips any line that
// belongs to none. At terminal y=4 — the value the task brief carried
// over from app_selection_test.go — the span straddles the gap between
// two messages and the "── Today ──" divider, so the highlight lands
// on two disjoint rows with an unhighlighted blank between them: a
// legal selection, but one that pins the skip path instead of the
// contiguous one.
//
// Down-AND-right rather than a same-row drag so the span has all three
// row kinds in it, each a different arm of applySelectionToRows'
// from/to computation (model.go:3407-3414):
//
//   - the FIRST row is partial, from loCol to end of line;
//   - the MIDDLE row is whole, from 0 to end of line;
//   - the LAST row is partial, from 0 to hiCol.
//
// A single-row drag reaches only the both-ends-partial case, and a
// press at the pane's left edge makes the first row whole too — which
// is why goldenDragPressX is 11 and not the "2 columns in" the task
// brief used. 11 = 1 border column + 1 gutter column + 9 content
// columns, which lands the start anchor inside the author line's text
// rather than before it. Measured against the blessed golden: the
// first highlighted row begins mid-timestamp.
//
// The last row is the reaction-pill line, so the 25-column cut also
// runs through sliceColumns' emoji-width handling rather than plain
// ASCII.
//
// TestGolden_DragSelectionSpansMultipleLines re-measures the
// consequence (exactly goldenDragDY+1 highlighted rows, none of them
// blank) rather than trusting this comment.
const (
	goldenDragPressX = 11
	goldenDragPressY = 7
	goldenDragDX     = 25
	goldenDragDY     = 2
)

// goldenDragApp builds the drag_selection scenario's App: the base
// fixture with a mouse drag held across three lines of the messages
// pane.
//
// With drag=false it builds the SAME frame minus the selection, which
// is the baseline the drag assertions compare against. base is not
// usable as that baseline: the press has a second side effect — it
// focuses the messages pane (reducer_mouse.go:249) — and focus decides
// which pane draws the thick border, so a drag-vs-base comparison
// cannot distinguish "the selection rendered" from "the borders
// swapped". The baseline reproduces the focus change and nothing else,
// so the remaining difference is the selection alone.
//
// The motionFlushTickMsg is NOT optional and is the one part of this
// that a reader would omit. MouseMotionMsg only LATCHES the cursor
// position into dragState.pending* and schedules a coalescing tick
// (drag.go:220-246); ExtendSelectionAt runs in the tick's arm
// (drag.go:256-277). Without the tick the selection's Start and End
// stay equal, applySelectionToRows takes its `from >= to` continue
// (model.go:3421), not one cell is styled, and the golden records a
// selection nobody can see.
//
// The tick is constructed and delivered directly rather than waited
// for: tea.Tick's timer never runs in a test, and the message carries
// no payload, so synthesising it is exact rather than approximate.
//
// No MouseReleaseMsg. Release would finalize the drag and copy to the
// clipboard, which is a different (and separately tested) behaviour;
// the held-mid-drag frame is the one that renders the highlight.
func goldenDragApp(t *testing.T, drag bool) *App {
	t.Helper()
	a := newGoldenApp(t, goldenFixtureOpts()...)

	if !drag {
		a.focusedPanel = PanelMessages
		_ = a.View()
		return a
	}

	x := a.layout.sidebarEnd + goldenDragPressX
	y := goldenDragPressY
	_, _ = a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	_, _ = a.Update(tea.MouseMotionMsg{X: x + goldenDragDX, Y: y + goldenDragDY, Button: tea.MouseLeft})
	_, _ = a.Update(motionFlushTickMsg{})

	_ = a.View()
	return a
}

// goldenWindowSplitScenario is the window_split scenario's App: two
// side-by-side windows on different channels, the second focused.
//
// The ChannelSelectedMsg round trip is what makes this a split of two
// DISTINCT windows rather than two views of nothing. splitWindow
// clones the focused window's tree record (windows.go:61-68), and that
// record is only written by the ChannelSelectedMsg apply path
// (windows.go:152). Setting a.messagepane's channel by hand would
// leave both tree records empty and both pane headers blank.
//
// The SetMessages / SetLoading pair after each selection is not
// cosmetic. With no channel service wired, ReadCache returns nothing
// and SyncedAt returns 0, so the reducer takes its tier-3 cold-start
// branch (reducer_channels.go:430-438): it blanks the pane and turns
// on the loading spinner. Left alone, both panes would render a
// spinner and the golden would pin an empty split. Re-seeding is what
// puts renderable rows in front of renderWindowNode.
//
// The two windows get DIFFERENT message counts on purpose: identical
// content in both panes would render identically, and a bug that drew
// the focused pane twice would look correct.
func goldenWindowSplitScenario(t *testing.T, w, h int) *App {
	t.Helper()
	a := newGoldenApp(t,
		withSize(w, h),
		withChannels(goldenChannels()...),
		withActiveChannel("C1"),
	)

	_, _ = a.Update(ChannelSelectedMsg{ID: "C1", Name: "general", Type: "channel"})
	a.messagepane.SetMessages(goldenMessages())
	a.messagepane.SetLoading(false)

	if cmd := a.splitWindow(wintree.SplitSideBySide); cmd != nil {
		// splitWindow returns a toast cmd, and only a toast cmd, when
		// the tree refuses the split for want of room (windows.go:64).
		// A refused split leaves one window and the golden silently
		// becomes a second copy of base at another size.
		t.Fatalf("splitWindow refused at %dx%d; the scenario would pin a single window", w, h)
	}

	_, _ = a.Update(ChannelSelectedMsg{ID: "C2", Name: "engineering", Type: "channel"})
	a.messagepane.SetMessages(goldenMessages()[:2])
	a.messagepane.SetLoading(false)

	_ = a.View()
	return a
}

func TestGolden(t *testing.T) {
	for _, sc := range goldenScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			a := sc.build(t)
			compareGolden(t, sc.name, a.View().Content)
		})
	}
}

// TestGolden_NarrowAutoHidesThreadPane pins the property the narrow
// scenario exists to capture, in terms the golden file cannot express.
//
// The golden records the ABSENCE of a thread pane, and an absence is
// exactly what a golden is worst at: if the scenario stopped opening a
// thread at all, or the sidebar width changed so the auto-hide threshold
// was no longer crossed, narrow.ansi would still look plausible and
// would be re-blessed without comment. This asserts the mechanism —
// ThreadAutoHidden, the threadVisible clear, the focus fallback, and the
// collapsed layout band — instead of its shadow.
func TestGolden_NarrowAutoHidesThreadPane(t *testing.T) {
	var hiders []goldenScenario
	for _, sc := range goldenScenarios() {
		if sc.autoHidesThread {
			hiders = append(hiders, sc)
		}
	}
	if len(hiders) == 0 {
		t.Fatal("no scenario declares autoHidesThread; the auto-hide path is pinned by nothing")
	}

	for _, sc := range hiders {
		t.Run(sc.name, func(t *testing.T) {
			a := sc.build(t)

			// The scenario asked for a thread. View() must have
			// taken it away again (app.go:2726-2731).
			if a.threadPanel.IsEmpty() {
				t.Fatalf("scenario declares autoHidesThread but never opened a thread, "+
					"so %q pins an absence that was never a presence", sc.name)
			}
			if a.threadVisible {
				t.Errorf("threadVisible still set at %dx%d; the thread pane did not auto-hide, "+
					"so the %s golden is not pinning what it claims to", sc.w, sc.h, sc.name)
			}
			if a.focusedPanel == PanelThread {
				t.Error("focus stayed on PanelThread after the pane auto-hid")
			}
			// The thread band collapsed onto the messages band
			// (panellayout.go:131-135), which is what makes PanelAt
			// stop routing clicks into a pane that is not on screen.
			if a.layout.threadEnd != a.layout.msgEnd {
				t.Errorf("thread band did not collapse: threadEnd = %d, msgEnd = %d",
					a.layout.threadEnd, a.layout.msgEnd)
			}
		})
	}

	// Control: the same build at a width above the threshold keeps all
	// three panes. Without it, a narrow golden with no thread pane is
	// equally consistent with a fixture that never opened one.
	wide := goldenThreadScenario(t, 200, 50)
	if !wide.threadVisible {
		t.Fatal("control: the thread pane auto-hid at 200x50 too, so the assertions " +
			"above are not about width at all")
	}
	if wide.layout.threadEnd <= wide.layout.msgEnd {
		t.Fatalf("control: no thread band at 200x50 (threadEnd = %d, msgEnd = %d)",
			wide.layout.threadEnd, wide.layout.msgEnd)
	}
}

// TestGolden_ThreadScenariosAreWideEnough is the counterpart to
// TestGolden_NarrowAutoHidesThreadPane: narrow pins that the pane goes
// away, this pins that it is there at all in the scenarios named for it.
//
// Both are needed. A golden of a three-pane frame and a golden of a
// two-pane frame are equally well-formed files; nothing in the .ansi
// says which one was intended.
//
// Which scenarios are in scope is DERIVED, not listed. The set used to
// be the hardcoded names {thread_open, wide}, which meant a new thread
// scenario added later matched neither and silently escaped the only
// check that has ever caught this defect. Instead every scenario is
// built and asked whether it loaded a thread — threadPanel survives
// View(), unlike threadVisible, which the auto-hide path clears
// (app.go:2726) — and every scenario that did is checked. Opting out
// takes an explicit autoHidesThread on the scenario, and even that is
// refused for a scenario wide enough to keep the pane.
//
// That derivation happens in THIS function body, before any t.Run, and
// deliberately so. It used to happen inside the subtests, with a
// `checked` counter tallying the in-scope ones and an "asserted
// nothing" backstop after the loop. That backstop fired on a clean
// tree: `go test -run 'TestGolden/drag_selection'` selects this test
// too, because -run is an UNANCHORED regex and "TestGolden" is a prefix
// of "TestGolden_ThreadScenariosAreWideEnough". Its subtests then all
// filtered out, leaving the counter at zero and a red failure on
// unmodified code. The parent body always runs when the test is
// selected, whatever the subtest filter says, so computing the scope
// here makes the emptiness check immune to -run — the same reason
// TestGolden_NarrowAutoHidesThreadPane's identical `len(hiders) == 0`
// guard never had the bug. Detecting the filter, renaming the test, or
// gating on testing.Short() would each have voided the guard for
// someone instead.
func TestGolden_ThreadScenariosAreWideEnough(t *testing.T) {
	// Boundary probe: goldenThreadMinWidth must be the exact edge, not
	// merely a width that happens to work. One column narrower has to
	// auto-hide, or the constant's doc comment is fiction.
	probe := func(w int) bool {
		a := newGoldenApp(t, withSize(w, 30), withChannels(goldenChannels()...))
		f := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(),
			a.sidebar.Width(), a.sidebarVisible, true)
		return f.ThreadAutoHidden
	}
	if probe(goldenThreadMinWidth) {
		t.Errorf("thread pane auto-hides at goldenThreadMinWidth (%d)", goldenThreadMinWidth)
	}
	if !probe(goldenThreadMinWidth - 1) {
		t.Errorf("thread pane survives at %d, so goldenThreadMinWidth is not the boundary "+
			"its doc comment claims", goldenThreadMinWidth-1)
	}

	// Scope, computed here rather than inside the subtests. The built
	// App is carried along with the scenario so the assertions below
	// run against the SAME build the scoping decision was made from.
	type threadCase struct {
		sc goldenScenario
		a  *App
	}
	var checked []threadCase
	for _, sc := range goldenScenarios() {
		a := sc.build(t)

		// threadPanel keeps its parent+replies through View(); only
		// threadVisible is cleared by the auto-hide path. So this
		// reads the scenario's REQUEST, after the render that may
		// have denied it.
		if a.threadPanel.IsEmpty() {
			if sc.autoHidesThread {
				t.Fatalf("scenario %q declares autoHidesThread but never opened a thread; "+
					"the flag is silencing a check that has nothing to check (%dx%d)",
					sc.name, sc.w, sc.h)
			}
			continue
		}

		if sc.autoHidesThread {
			// The opt-out is only legitimate below the threshold.
			// Above it the pane would render, so the flag would be
			// hiding a genuine failure rather than describing an
			// intended one.
			if sc.w >= goldenThreadMinWidth {
				t.Fatalf("scenario %q declares autoHidesThread at %d cols, at or above "+
					"goldenThreadMinWidth (%d), where the pane does NOT auto-hide; "+
					"the flag cannot be used to opt a wide thread scenario out of this check",
					sc.name, sc.w, goldenThreadMinWidth)
			}
			// The auto-hide itself is asserted by
			// TestGolden_NarrowAutoHidesThreadPane.
			continue
		}

		checked = append(checked, threadCase{sc: sc, a: a})
	}

	// If every scenario stopped opening a thread, the loop below would
	// pass while asserting nothing at all.
	if len(checked) == 0 {
		t.Fatal("no scenario opens a visible thread pane; this test asserted nothing")
	}

	for _, tc := range checked {
		sc, a := tc.sc, tc.a
		t.Run(sc.name, func(t *testing.T) {
			if sc.w < goldenThreadMinWidth {
				t.Fatalf("scenario opens a thread at %d cols, below goldenThreadMinWidth (%d); "+
					"its thread pane will auto-hide and the golden will pin two panes under a "+
					"name that promises three. Widen it, or declare autoHidesThread if the "+
					"auto-hide is the point", sc.w, goldenThreadMinWidth)
			}
			if !a.threadVisible {
				t.Errorf("threadVisible cleared at %dx%d", sc.w, sc.h)
			}
			if a.layout.threadEnd <= a.layout.msgEnd {
				t.Errorf("no thread band: threadEnd = %d, msgEnd = %d",
					a.layout.threadEnd, a.layout.msgEnd)
			}
		})
	}
}

// goldenScenarioNamed returns the named scenario from the table, or
// fails. Used by the per-scenario assertions below so they exercise the
// SAME build the golden was blessed from — a private near-copy of the
// build func would drift and then assert nothing about the file.
func goldenScenarioNamed(t *testing.T, name string) goldenScenario {
	t.Helper()
	for _, sc := range goldenScenarios() {
		if sc.name == name {
			return sc
		}
	}
	t.Fatalf("no golden scenario named %q", name)
	return goldenScenario{}
}

// TestGolden_NoSidebarActuallyHidesIt pins that no_sidebar records a
// MISSING sidebar and not a narrow one.
//
// Like narrow, this golden records an absence, and an absence is what a
// golden is worst at: a no_sidebar.ansi that still had the channel list
// in it would look entirely plausible and would be re-blessed without
// comment. The band arithmetic is asserted directly (sidebarEnd
// collapses onto railWidth, panellayout.go:129), and separately the
// rows that only ever appear in the sidebar are asserted absent.
func TestGolden_NoSidebarActuallyHidesIt(t *testing.T) {
	a := goldenScenarioNamed(t, "no_sidebar").build(t)

	if a.sidebarVisible {
		t.Error("sidebarVisible still set")
	}
	// Gone, not merely narrow: with sbWidth and sbBorder both zero the
	// sidebar band has zero cells and starts where the rail ends.
	if a.layout.sidebarEnd != a.layout.railWidth {
		t.Errorf("sidebar band is %d cols wide, want 0 (sidebarEnd = %d, railWidth = %d)",
			a.layout.sidebarEnd-a.layout.railWidth, a.layout.sidebarEnd, a.layout.railWidth)
	}
	// The messages pane must have claimed the freed columns rather
	// than leaving them blank.
	if a.layout.msgEnd != a.width {
		t.Errorf("messages band does not reach the right edge: msgEnd = %d, width = %d",
			a.layout.msgEnd, a.width)
	}

	plain := stripANSI(a.View().Content)
	// Tokens unique to the sidebar. Deliberately NOT "# general":
	// that also appears in the messages-pane header and the statusbar,
	// so it would pass with the sidebar fully rendered.
	for _, gone := range []string{"▾ Channels", "▾ DMs", "● bob", "○ carol", "# muted-noise"} {
		if strings.Contains(plain, gone) {
			t.Errorf("sidebar row %q still on screen with the sidebar hidden; view was:\n%s", gone, plain)
		}
	}

	// Control: the same tokens ARE present in base. Without this the
	// assertions above pass equally well against a fixture that never
	// had a sidebar to hide.
	ctrl := stripANSI(goldenScenarioNamed(t, "base").build(t).View().Content)
	for _, want := range []string{"▾ Channels", "▾ DMs", "● bob", "○ carol"} {
		if !strings.Contains(ctrl, want) {
			t.Fatalf("control: base does not render sidebar row %q, so its absence in "+
				"no_sidebar proves nothing; view was:\n%s", want, ctrl)
		}
	}
}

// TestGolden_DragSelectionIsActuallySelected is the assertion that
// stops drag_selection from being a second copy of base.
//
// The scenario's whole value is that its stripped text equals base's
// and the entire difference is escape sequences — which is also
// precisely what makes it easy to get silently wrong. A drag whose
// coordinates missed (chrome, a gap row, an off-by-one in the border
// offset) produces a file that is plausible, readable, and identical
// to base.ansi. Three things are checked, and all three are needed:
//
//  1. the model reports a selection at all;
//  2. the rendered frame is strictly LONGER than the undragged
//     baseline's, i.e. selection SGR bytes actually reached the screen
//     — a selection that exists in the model but renders nothing
//     (Start == End takes applySelectionToRows' `from >= to` continue,
//     model.go:3421) fails here and nowhere else;
//  3. the stripped text is unchanged against that baseline, i.e. what
//     differs is styling and not content. Without this the test would
//     also pass if the drag had scrolled the pane.
//
// The baseline is goldenDragApp(t, false), not base: see that function
// for why base cannot serve. The comparison against base is still
// made, but only for byte length — which is the check the golden FILE
// needs, since drag_selection.ansi being no larger than base.ansi is
// the concrete symptom of a drag that missed.
func TestGolden_DragSelectionIsActuallySelected(t *testing.T) {
	a := goldenScenarioNamed(t, "drag_selection").build(t)
	if !a.messagepane.HasSelection() {
		t.Fatal("no selection on the messages pane after the drag; the coordinates missed " +
			"and drag_selection would be blessed as a frame with no highlight in it")
	}

	drag := a.View().Content
	undragged := goldenDragApp(t, false).View().Content
	base := goldenScenarioNamed(t, "base").build(t).View().Content

	if len(drag) <= len(undragged) {
		t.Errorf("drag_selection renders %d bytes, the same frame without the drag renders %d; "+
			"the highlight emitted no escape sequences", len(drag), len(undragged))
	}
	if got, want := stripANSI(drag), stripANSI(undragged); got != want {
		t.Errorf("drag_selection's stripped text differs from the undragged frame's; the drag "+
			"changed CONTENT, not just styling: %s", firstLineDiff(want, got))
	}
	if len(drag) <= len(base) {
		t.Errorf("drag_selection renders %d bytes, base renders %d; drag_selection.ansi would "+
			"not be larger than base.ansi, which is what a missed drag looks like on disk",
			len(drag), len(base))
	}
}

// TestGolden_DragSelectionSpansMultipleLines pins the shape of the
// selection, which is what decides how many branches of
// applySelectionToRows (messages/model.go:3355) the golden covers.
//
// A same-line selection reaches only the both-ends-partial case. The
// scenario drags down as well as right so the span has a partial first
// line, at least one FULL middle line, and a partial last line.
//
// The highlighted rows are counted by looking for the selection style's
// own SGR prefix, NOT by diffing against an unhighlighted frame. The
// press has a second side effect — messagepane.ClickAt moves the
// selected-MESSAGE cursor (reducer_mouse.go:308), which restyles the
// rows of both the old and the new cursor message — so a diff counts
// six rows where only three carry a selection. Measuring the style
// directly is the only count that means what the name says.
func TestGolden_DragSelectionSpansMultipleLines(t *testing.T) {
	a := goldenScenarioNamed(t, "drag_selection").build(t)

	spans := goldenSelectedSpans(t, a.View().Content)
	if len(spans) != goldenDragDY+1 {
		t.Fatalf("%d rendered rows carry the selection style, want %d; the span is a "+
			"different shape than the %d-row drag implies. Highlighted rows were:\n%s",
			len(spans), goldenDragDY+1, goldenDragDY, goldenFormatSpans(spans))
	}

	// Contiguous: consecutive entry lines, not rows with unhighlighted
	// gaps between them. A gapped span means the selection crossed a
	// message boundary or a divider, which is the shape
	// goldenDragPressY was chosen to avoid.
	for i := 1; i < len(spans); i++ {
		if spans[i].row != spans[i-1].row+1 {
			t.Errorf("highlighted rows %d and %d are not adjacent; the span straddles a "+
				"gap or a divider. Rows were:\n%s",
				spans[i-1].row, spans[i].row, goldenFormatSpans(spans))
		}
	}

	// The three arms of the from/to computation, in the order they
	// occur. Asserted rather than described, because the shape depends
	// entirely on goldenDragPressX and would silently collapse to
	// "whole, whole, partial" if that constant went back to the
	// brief's value.
	first, middle, last := spans[0], spans[1], spans[len(spans)-1]
	if first.start <= middle.start {
		t.Errorf("the first highlighted row starts at column %d and the middle one at %d; "+
			"the first row is not partial, so the from = loCol arm is unpinned. Rows were:\n%s",
			first.start, middle.start, goldenFormatSpans(spans))
	}
	if last.width >= middle.width {
		t.Errorf("the last highlighted row is %d cells wide and the middle one %d; the last "+
			"row is not partial, so the to = hiCol arm is unpinned. Rows were:\n%s",
			last.width, middle.width, goldenFormatSpans(spans))
	}

	// Control: the same frame without the drag has no highlighted rows
	// at all. Without this, a selection style that happened to equal
	// some other style in the frame would satisfy every check above.
	if n := len(goldenSelectedSpans(t, goldenDragApp(t, false).View().Content)); n != 0 {
		t.Fatalf("control: %d rows carry the selection style with no drag performed, so "+
			"the assertions above are not measuring the selection", n)
	}
}

// goldenSelectedSpan is one row's selection highlight: which rendered
// row it is on, the display column it starts at, and how many cells it
// covers.
type goldenSelectedSpan struct {
	row   int
	start int
	width int
	text  string
}

// goldenSelectedSpans locates the selection highlight in a rendered
// frame.
//
// Detection is by the SGR prefix styles.SelectionStyle() emits, derived
// at call time rather than written as a literal so a palette change
// moves this with it. newGoldenApp has already pinned the theme, so the
// prefix is the one the frame was rendered under.
//
// applySelectionToRows emits exactly one selStyle.Render per affected
// row (model.go:3437), so the first occurrence per line is the whole
// highlight and the run ends at the next escape byte.
func goldenSelectedSpans(t *testing.T, view string) []goldenSelectedSpan {
	t.Helper()
	probe := styles.SelectionStyle().Render("x")
	i := strings.Index(probe, "x")
	if i <= 0 {
		t.Fatalf("selection style emits no leading SGR (%q); this detector cannot work", probe)
	}
	prefix := probe[:i]

	var out []goldenSelectedSpan
	for row, line := range strings.Split(view, "\n") {
		at := strings.Index(line, prefix)
		if at < 0 {
			continue
		}
		seg := line[at+len(prefix):]
		if end := strings.IndexByte(seg, 0x1b); end >= 0 {
			seg = seg[:end]
		}
		out = append(out, goldenSelectedSpan{
			row:   row,
			start: ansi.StringWidth(line[:at]),
			width: ansi.StringWidth(seg),
			text:  seg,
		})
	}
	return out
}

func goldenFormatSpans(spans []goldenSelectedSpan) string {
	var b strings.Builder
	for _, s := range spans {
		fmt.Fprintf(&b, "  row %2d col %3d w %3d %q\n", s.row, s.start, s.width, s.text)
	}
	return b.String()
}

// TestGolden_OverlayFinderIsCompositedOverBackdrop pins that
// overlay_finder records a modal composited onto a dimmed screen, and
// not either half on its own.
//
// applyOverlays (view_overlays.go:39) is a chain of conditionals; a
// finder whose IsVisible went false, or a mode that stopped counting as
// a modal overlay, yields a golden that is just base with a different
// name. Both layers are therefore asserted: the box's own text, and the
// background text still legible behind it.
func TestGolden_OverlayFinderIsCompositedOverBackdrop(t *testing.T) {
	a := goldenScenarioNamed(t, "overlay_finder").build(t)

	if !a.channelFinder.IsVisible() {
		t.Fatal("channel finder is not visible; the golden is base under another name")
	}
	if !a.overlayActive() {
		t.Fatal("overlayActive() is false, so applyOverlays composited nothing and " +
			"maybeWrapFinalScreen took its no-op path")
	}

	view := a.View().Content
	plain := stripANSI(view)

	// Layer 1: the box. "Switch Channel" is the finder's own title
	// (channelfinder/model.go:530) and appears nowhere else.
	for _, want := range []string{"Switch Channel", "Type to filter..."} {
		if !strings.Contains(plain, want) {
			t.Errorf("finder box does not render %q; view was:\n%s", want, plain)
		}
	}
	// Layer 2: the backdrop. The status row is outside the box, so its
	// survival is what proves this is a composite and not a
	// full-screen replacement.
	if !strings.Contains(plain, "#general") {
		t.Errorf("no backdrop behind the overlay; view was:\n%s", plain)
	}

	// The box is CENTERED: DimmedOverlay places it at
	// (width-modalW)/2, so the rows carrying its text must start
	// well inside the frame rather than at column 0.
	for _, line := range strings.Split(plain, "\n") {
		if !strings.Contains(line, "Switch Channel") {
			continue
		}
		if lead := len(line) - len(strings.TrimLeft(line, " ")); lead < 4 {
			t.Errorf("the finder box is not centered: %q has %d leading spaces", line, lead)
		}
	}

	// The dim is a per-cell COLOR rewrite (overlay.go:66-71), so it is
	// invisible to stripANSI and only a raw-ANSI golden can pin it.
	// Assert it exists by comparing against the same frame rendered
	// with the overlay closed: identical text, different bytes.
	closed := newGoldenApp(t,
		withSize(goldenBaseW, goldenBaseH),
		withChannels(goldenChannels()...),
		withMessages(goldenMessages()...),
		withActiveChannel("C1"),
		withRender(),
	).View().Content
	if view == closed {
		t.Fatal("the overlay frame is byte-identical to the un-overlaid one")
	}

	// The non-joined row is on screen: without it the dim-grey branch
	// of the row renderer (channelfinder/model.go:606-609) is
	// unreachable and the golden pins only the joined path.
	if !strings.Contains(plain, "muted-noise") {
		t.Errorf("the non-joined finder row is not listed; view was:\n%s", plain)
	}
}

// TestGolden_WindowSplitRendersTwoDistinctPanes pins the property
// window_split exists for.
//
// The golden's value is renderWindowNode's recursion plus the
// focused/unfocused border fork, and neither survives a scenario that
// quietly ended up with one window: a single-window tree short-circuits
// to renderMessagesRegion (view_window_region.go:30) and the file
// becomes base at another size. The borders are the visible signature
// of the fork — FocusedBorder is a THICK box, UnfocusedBorder a ROUNDED
// one (styles/styles.go:439-442) — so both glyphs must be present in
// the messages band, and neither on its own.
func TestGolden_WindowSplitRendersTwoDistinctPanes(t *testing.T) {
	sc := goldenScenarioNamed(t, "window_split")
	a := sc.build(t)

	if got := a.wins.Len(); got != 2 {
		t.Fatalf("window count = %d, want 2; the split did not take and the golden "+
			"records a single pane", got)
	}

	band := goldenPanelText(a.View().Content, a.layout.sidebarEnd, a.layout.msgEnd)
	// Top-left corners, which are unambiguous: "┏" only comes from
	// lipgloss.ThickBorder and "╭" only from RoundedBorder. Sliced to
	// the messages band so the sidebar's own rounded border cannot
	// satisfy the second one.
	if !strings.Contains(band, "┏") {
		t.Errorf("no thick (focused) border in the messages band; the focused pane did not "+
			"render through renderMessagesRegion. Band was:\n%s", band)
	}
	if !strings.Contains(band, "╭") {
		t.Errorf("no rounded (unfocused) border in the messages band; the second pane did not "+
			"render through renderUnfocusedWindow. Band was:\n%s", band)
	}

	// Distinct CONTENT, not just distinct chrome. The two windows are
	// seeded with different message counts, so text present in one and
	// absent from the other proves the panes are not two renders of
	// the same model.
	plain := stripANSI(a.View().Content)
	if !strings.Contains(plain, "# general") {
		t.Errorf("the unfocused window's channel header is missing; view was:\n%s", plain)
	}
	if !strings.Contains(plain, "# engineering") {
		t.Errorf("the focused window's channel header is missing; view was:\n%s", plain)
	}
	// goldenMessages()[:2] stops before carol's row, so this string
	// can only have come from the six-message (unfocused) pane.
	if !strings.Contains(plain, "wrapping behaviour") {
		t.Errorf("the unfocused window is not rendering its own longer history; "+
			"view was:\n%s", plain)
	}

	// Control: the same fixture WITHOUT the split has neither a second
	// pane nor a thick border in the band, so the assertions above are
	// about the split and not about the base chrome.
	ctrl := goldenScenarioNamed(t, "base").build(t)
	cband := goldenPanelText(ctrl.View().Content, ctrl.layout.sidebarEnd, ctrl.layout.msgEnd)
	if strings.Contains(cband, "┏") {
		t.Fatalf("control: the unsplit messages band already carries a thick border, so "+
			"the focused-border assertion proves nothing. Band was:\n%s", cband)
	}
}

// goldenStatusOverflow is how many display columns wider than the
// terminal every golden's rows come out.
//
// IT ENCODES A REAL, UNFIXED BUG, not a rendering convention. The status
// row overruns its budget by six columns:
//
//   - statusbar.Model.render budgets the gap against width-1
//     (statusbar/model.go:347-356), one column short to begin with;
//   - it then emits the filler as up to three separate
//     styles.StatusBar.Render calls (leftPad, hint, rightPad — or the
//     single %*s filler), and styles.StatusBar carries Padding(0, 1)
//     (styles/styles.go:467), so each Render adds two columns of
//     padding that the budget never accounted for;
//   - plus the 3-column rightPad gutter joined on at model.go:355.
//
// Nothing clips the result, so App.View() hands back a frame six cells
// wider than a.width and the terminal wraps it. The goldens record that
// faithfully, which is the point of a golden.
//
// WHEN THAT BUG IS FIXED: change this constant to 0 and re-bless. Do
// not chase the new number — a correct statusbar renders at exactly
// a.width, which is what `want = sc.w` already means for the overlay
// case below.
const goldenStatusOverflow = 6

// goldenMaxWidthReports caps how many wrong-width lines
// TestGoldenFilesAreWellFormed dumps per scenario. See the loop at the
// bottom of that test for why a cap rather than skipping the width
// check when the line count is already wrong: a plain width drift
// leaves the line count correct and still fails every row.
const goldenMaxWidthReports = 3

// TestGoldenFilesAreWellFormed catches the classic snapshot-suite
// death: a golden blessed from a blank, truncated, or half-rendered
// frame. Such a file is still valid ANSI and still compares equal to
// itself forever, so TestGolden alone would keep passing while pinning
// nothing.
//
// Four properties, each defending a different way a golden dies:
//
//   - non-empty bytes: the -update run wrote nothing at all;
//   - line count == sc.h: the frame was truncated, or the scenario's
//     declared height drifted from what its build actually renders;
//   - non-blank after ANSI stripping: every cell is a space, i.e. the
//     render produced chrome-free emptiness (a zero-size App, an
//     un-Init'd model) that looks substantial on disk because of the
//     escape sequences;
//   - uniform display width: a partially-written or hand-edited file.
//
// On the width predicate, two facts that are easy to get wrong:
//
//  1. The expected width is sc.w + goldenStatusOverflow, NOT sc.w — see
//     that constant. But a uniform w+6 is WRONG for overlay scenarios:
//     maybeWrapFinalScreen (view_overlays.go:108-111) re-wraps the
//     whole screen in a Width(a.width) style when an overlay is
//     active, which truncates the overrun away. overlay_finder is
//     therefore exactly 120 while the other seven are w+6. The
//     exception is keyed on a.overlayActive() — derived from the built
//     App — and deliberately NOT on the scenario's name, so a new
//     overlay scenario is handled without an edit here and a scenario
//     that stops opening its overlay fails rather than being excused.
//
//  2. Goldens carry NO trailing newline (compareGolden writes
//     View().Content verbatim), so strings.Split on "\n" yields
//     exactly h elements. A guard written for a trailing newline would
//     compute h+1 and "fix" it by weakening the assertion.
//
// Width is measured with ansi.StringWidth rather than by stripping
// "\x1b[...m" and counting runes: the frames contain OSC-8 hyperlinks
// and wide graphemes, both of which a naive strip miscounts. Every line
// is checked, not just the first — a truncated final row is the most
// likely single-line defect and is exactly what a first-line-only
// check misses.
func TestGoldenFilesAreWellFormed(t *testing.T) {
	for _, sc := range goldenScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			path := filepath.Join(goldenDir, sc.name+".ansi")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v\n"+
					"bless it with: go test ./internal/ui -run TestGolden -update", path, err)
			}
			content := string(b)
			if len(content) == 0 {
				t.Fatalf("%s is empty", path)
			}

			lines := strings.Split(content, "\n")
			if len(lines) != sc.h {
				t.Errorf("%s has %d lines, want %d (scenario height); a golden carries no "+
					"trailing newline, so the split yields exactly h elements",
					path, len(lines), sc.h)
			}
			if strings.TrimSpace(stripANSI(content)) == "" {
				t.Errorf("%s renders as entirely blank once ANSI is stripped; it was blessed "+
					"from an empty frame", path)
			}

			want := sc.w + goldenStatusOverflow // see goldenStatusOverflow: a real bug
			if a := sc.build(t); a.overlayActive() {
				// maybeWrapFinalScreen clamped the frame to a.width.
				want = sc.w
			}
			// Report at most goldenMaxWidthReports mismatching lines.
			// Every failure mode here is systemic — a truncated file,
			// a scenario whose declared width drifted, an overlay
			// that stopped opening — so the rows fail in bulk, and
			// each report carries a %q of a full escape-laden line.
			// Fifty rows of that buries the first one, which is the
			// only one anybody reads. The tally still reports the
			// true total so "3 lines wrong" and "all 50 wrong" stay
			// distinguishable.
			bad := 0
			for i, line := range lines {
				got := ansi.StringWidth(line)
				if got == want {
					continue
				}
				bad++
				if bad > goldenMaxWidthReports {
					continue
				}
				t.Errorf("%s line %d is %d display columns wide, want %d "+
					"(scenario width %d + %d statusbar overrun, or exactly the width "+
					"when an overlay clamps it); line was:\n  %q",
					path, i+1, got, want, sc.w, goldenStatusOverflow, line)
			}
			if bad > goldenMaxWidthReports {
				t.Errorf("%s: %d lines are the wrong display width; %d further reports "+
					"suppressed after the first %d",
					path, bad, bad-goldenMaxWidthReports, goldenMaxWidthReports)
			}
		})
	}
}

// TestGolden_ScenariosArePairwiseDistinct is the cheapest guard against
// the failure mode every scenario above is individually defended
// against: two entries that render the same bytes.
//
// It has already happened once in this file's history — thread_open at
// 120 columns was byte-for-byte base, because the thread pane auto-hid
// (see goldenThreadMinWidth). A per-scenario assertion catches that only
// if someone thought to write one for that scenario; this catches it for
// every scenario, including ones added later.
func TestGolden_ScenariosArePairwiseDistinct(t *testing.T) {
	rendered := map[string]string{}
	for _, sc := range goldenScenarios() {
		rendered[sc.name] = sc.build(t).View().Content
	}
	names := make([]string, 0, len(rendered))
	for n := range rendered {
		names = append(names, n)
	}
	sort.Strings(names)

	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if rendered[names[i]] == rendered[names[j]] {
				t.Errorf("scenarios %q and %q render byte-identical frames; one of the two "+
					"golden files pins nothing the other does not", names[i], names[j])
			}
		}
	}
}
