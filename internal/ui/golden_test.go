package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/styles"
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
// timezone-dependent. DateStr is set for documentation only — nothing in
// the render path reads it.
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
		{
			TS: goldenTS(2 * time.Minute), UserID: "B1", UserName: "deploybot",
			Text: "build #421 green", Timestamp: "9:02 AM", DateStr: "2026-03-15",
			Attachments: []messages.Attachment{
				{Kind: "file", Name: "build.log", URL: "https://example.invalid/build.log", Size: 20480},
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

// goldenFixtureOpts is the standard scenario the determinism tests
// render: sidebar, active channel, and one message per interesting
// render branch. Shared so every determinism assertion exercises the
// same surface area, and so a scenario that stops covering a pinned
// global fails loudly in one place rather than silently everywhere.
func goldenFixtureOpts() []testOpt {
	return []testOpt{
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
