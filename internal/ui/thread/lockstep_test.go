// internal/ui/thread/lockstep_test.go
//
// Lockstep parity between messages.Model and thread.Model.
//
// thread/model.go:35-36 asks humans to keep this package's viewEntry --
// and, transitively, ~377 lines of scroll, selection and render logic --
// in lockstep with internal/ui/messages by hand. AGENTS.md names that
// comment as the counter-example for its own convention: "two
// implementations that must stay parallel" belong in an interface
// assertion or a lockstep test, not a comment. This file is that test.
//
// Phase 3 of the architecture refactor merges the two models behind a
// shared pane package. When it does, this file is deleted. That
// lifecycle is intended: the file exists to make the merge verifiable,
// not to be maintained forever.
package thread

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// Pane geometry. 80 columns is wide enough that the fixture's long line
// wraps exactly once; 20 rows is tall enough that neither pane scrolls,
// so every fixture row is on screen and the scrollbar gutter stays off.
const (
	lockstepWidth  = 80
	lockstepHeight = 20
)

// lockstepClock is the instant both panes render against: Sunday
// 2026-03-15, midday, in the process's LOCAL zone.
//
// Local, not UTC. This task's brief specified time.UTC; that is wrong
// here for the same reason it was wrong for the goldens
// (internal/ui/golden_test.go:321-343). The day-divider label compares
// DateFromTS(msg.TS), which formats in the local zone
// (messages/model.go:3473), against the calendar fields of nowFunc()
// (messages/model.go:3522). A UTC anchor makes those two sides disagree
// in every zone east of UTC+11: the fixture's parent row silently moves
// onto the same calendar day as the replies, the day-transition divider
// stops rendering, and the assertion that looks for it fails. Anchoring
// the clock to the local zone moves both sides together, so the labels
// are identical in every zone on earth.
//
// Midday, not midnight, so the fixture's derived timestamps stay clear
// of a day boundary across the +-14h spread of real UTC offsets.
//
// Verified by running this package under TZ=UTC, TZ=Pacific/Auckland,
// TZ=Pacific/Kiritimati, TZ=Pacific/Chatham, TZ=America/Anchorage and
// TZ=Asia/Kathmandu.
func lockstepClock() time.Time {
	return time.Date(2026, 3, 15, 12, 0, 0, 0, time.Local)
}

// lockstepTS builds a Slack timestamp offset from lockstepClock.
//
// Derived, never a literal. Literal epochs are how this task's brief
// broke: it paired 2024-epoch TS values with 2026 DateStr strings, and
// the day-divider path reads DateFromTS(msg.TS), never DateStr
// (messages/model.go:1776, thread/model.go:1602). Deriving from a local
// clock also keeps every row on a fixed local calendar day in any zone.
func lockstepTS(offset time.Duration) string {
	return fmt.Sprintf("%d.000100", lockstepClock().Add(offset).Unix())
}

// lockstepItems exercises only behaviour BOTH models claim to share:
// author/timestamp rows, message bodies, a day-transition divider, the
// unread landmark, a reaction pill, and a line long enough to wrap.
//
// Row 0 lands on the day before lockstepClock and row 1/2 on
// lockstepClock itself, so the day-transition divider fires between
// them in both panes. In the thread pane row 0 is the parent and rows
// 1..2 are the replies, which is why the transition -- rather than the
// leading divider -- is the shared property (see divergence 3).
//
// DateStr is inert in the render path and is set only because
// internal/export/markdown.go:33 writes it into exported threads.
func lockstepItems() []messages.MessageItem {
	return []messages.MessageItem{
		{
			TS: lockstepTS(-24 * time.Hour), UserID: "U1", UserName: "alice",
			Text: "first", Timestamp: "9:00 AM", DateStr: "2026-03-14",
		},
		{
			TS: lockstepTS(0), UserID: "U2", UserName: "bob",
			Text: "second", Timestamp: "9:00 AM", DateStr: "2026-03-15",
			Reactions: []messages.ReactionItem{
				{Emoji: "tada", Count: 2, UserIDs: []string{"U1", "U3"}},
			},
		},
		{
			TS: lockstepTS(time.Minute), UserID: "U3", UserName: "carol",
			Text:      "a deliberately long line that has to wrap identically in both panes or the two renderers have drifted apart",
			Timestamp: "9:01 AM", DateStr: "2026-03-15",
		},
	}
}

// lockstepRender builds both panes from the same fixture, in the same
// state, and returns the models alongside their ANSI-stripped rows.
//
// Stripped, not raw: thread.View wraps its whole output in an outer
// Background style that messages.View does not emit (divergence 14), so
// the raw byte streams differ even where the panes are pixel-identical.
// Stripping isolates the rendered glyph grid, which is the thing that
// actually has to stay in lockstep.
//
// The two panes are put into equivalent states, not identical calls:
// each model's last row is selected (messages.New selects the newest
// message; SetThread selects the newest reply), both are focused, and
// both are given an unread boundary at the first item so the
// "── new ──" landmark renders.
func lockstepRender(t *testing.T) (mm *messages.Model, tm *Model, msgPane, thrPane []string) {
	t.Helper()

	// Process-global render inputs. Both are mutated by other tests in
	// this package and by production code, and both change every glyph
	// on screen. Reverting means re-applying the dark theme and the
	// default emoji mode -- styles.Apply has no inverse; re-applying
	// dark is the convention the rest of the tree uses.
	styles.Apply("dark", config.Theme{})
	emojiutil.SetImageMode(false, 2)
	messages.SetNowFunc(lockstepClock)
	t.Cleanup(func() {
		styles.Apply("dark", config.Theme{})
		emojiutil.SetImageMode(false, 2)
		messages.SetNowFunc(nil)
	})

	items := lockstepItems()

	msgModel := messages.New(items, "general")
	mm = &msgModel
	mm.SetFocused(true)
	mm.SetLastReadTS(items[0].TS)

	tm = New()
	tm.SetThread(items[0], items[1:], "C1", items[0].TS)
	tm.SetFocused(true)
	tm.SetUnreadBoundary(items[0].TS)

	msgPane = lockstepStrip(mm.View(lockstepHeight, lockstepWidth))
	thrPane = lockstepStrip(tm.View(lockstepHeight, lockstepWidth))
	return mm, tm, msgPane, thrPane
}

// lockstepStrip splits a rendered pane into ANSI-stripped rows.
func lockstepStrip(view string) []string {
	rows := strings.Split(view, "\n")
	for i, r := range rows {
		rows[i] = ansi.Strip(r)
	}
	return rows
}

// lockstepRow returns the index of the ONLY row in pane containing
// anchor, and fails the test when the count is not exactly one.
//
// The "exactly one" requirement is load-bearing, and is the reason this
// file does not use the shape the brief proposed:
//
//	strings.Contains(a, x) != strings.Contains(b, x)
//
// That assertion passes when BOTH sides are false -- a pane that
// rendered nothing at all satisfies it. Resolving each anchor to a
// concrete row index first means a missing row is a failure, not a
// silent pass, and gives the comparison something to be exact about.
func lockstepRow(t *testing.T, pane []string, paneName, anchor string) int {
	t.Helper()
	idx := -1
	n := 0
	for i, row := range pane {
		if strings.Contains(row, anchor) {
			n++
			idx = i
		}
	}
	if n != 1 {
		t.Fatalf("%s: want exactly 1 row containing %q, found %d\npane:\n%s",
			paneName, anchor, n, strings.Join(pane, "\n"))
	}
	return idx
}

// TestLockstep_SharedRenderBehaviour pins the parity that
// thread/model.go:35-36 currently asks humans to maintain by hand.
//
// It asserts on the SHARED SUBSET only, in three layers:
//
//   - geometry: both panes fill exactly lockstepHeight rows of exactly
//     lockstepWidth display columns;
//   - content: every shared row is byte-identical after ANSI stripping,
//     which pins glyphs, centring, padding and the wrap column;
//   - structure: the row-index gaps WITHIN a message are equal, which
//     pins ordering (day divider above unread landmark above the
//     message) and intra-message spacing.
//
// Every legitimate difference is enumerated in the "Documented
// divergences" comment below. That list is the contract Phase 3's pane
// extraction has to honour.
func TestLockstep_SharedRenderBehaviour(t *testing.T) {
	_, _, msgPane, thrPane := lockstepRender(t)

	// --- geometry -------------------------------------------------
	for _, p := range []struct {
		name string
		rows []string
	}{{"messages", msgPane}, {"thread", thrPane}} {
		if len(p.rows) != lockstepHeight {
			t.Errorf("%s pane: got %d rows, want %d", p.name, len(p.rows), lockstepHeight)
		}
		for i, row := range p.rows {
			if w := ansi.StringWidth(row); w != lockstepWidth {
				t.Errorf("%s pane row %d: width %d, want %d (%q)", p.name, i, w, lockstepWidth, row)
			}
		}
	}

	// --- content --------------------------------------------------
	//
	// Anchors are chosen to be unique within a pane; lockstepRow
	// enforces that. The assertion is full-row equality, so a drift in
	// centring, padding, glyph choice or wrap column fails even though
	// the anchor still resolves.
	shared := []struct{ name, anchor string }{
		{"author row (alice)", "alice  9:00 AM"},
		{"body row (first)", "▌first"},
		{"day-transition divider", "── Today ──"},
		{"unread landmark", "── new ──"},
		{"author row (bob)", "bob  9:00 AM"},
		{"body row (second)", "▌second"},
		{"reaction pill", "🎉 2"},
		{"author row (carol)", "carol  9:01 AM"},
		{"wrapped line 1", "in both panes or the"},
		{"wrapped line 2", "two renderers have drifted apart"},
	}
	for _, s := range shared {
		mi := lockstepRow(t, msgPane, "messages pane/"+s.name, s.anchor)
		ti := lockstepRow(t, thrPane, "thread pane/"+s.name, s.anchor)
		if msgPane[mi] != thrPane[ti] {
			t.Errorf("%s differs between panes:\n messages[%d] = %q\n thread[%d]   = %q",
				s.name, mi, msgPane[mi], ti, thrPane[ti])
		}
	}

	// --- structure ------------------------------------------------
	//
	// Only gaps WITHIN a message (or between a divider and the message
	// it introduces) are compared. Gaps BETWEEN messages legitimately
	// differ: messages separates with a blank row, thread with a
	// full-width rule (divergence 4).
	gaps := []struct{ name, from, to string }{
		{"author -> body (alice)", "alice  9:00 AM", "▌first"},
		{"day divider -> unread landmark", "── Today ──", "── new ──"},
		{"unread landmark -> author (bob)", "── new ──", "bob  9:00 AM"},
		{"author -> body (bob)", "bob  9:00 AM", "▌second"},
		{"body -> reaction pill (bob)", "▌second", "🎉 2"},
		{"author -> wrapped line 1 (carol)", "carol  9:01 AM", "in both panes or the"},
		{"wrapped line 1 -> line 2", "in both panes or the", "two renderers have drifted apart"},
	}
	for _, g := range gaps {
		msgGap := lockstepRow(t, msgPane, "messages pane", g.to) - lockstepRow(t, msgPane, "messages pane", g.from)
		thrGap := lockstepRow(t, thrPane, "thread pane", g.to) - lockstepRow(t, thrPane, "thread pane", g.from)
		if msgGap != thrGap {
			t.Errorf("row gap %q: messages=%d thread=%d", g.name, msgGap, thrGap)
		}
	}
}

// TestLockstep_ReactionHitTestFrames pins divergence 6, the one
// difference between the two models that a merge could break silently:
// the panes agree on WHERE a reaction pill is drawn but disagree on the
// coordinate frame their HitTestReaction accepts.
//
// It is a test rather than a line in the comment below because a
// comment cannot notice when one side changes. AGENTS.md: express a
// cross-model invariant as an assertion, not a comment.
func TestLockstep_ReactionHitTestFrames(t *testing.T) {
	mm, tm, msgPane, thrPane := lockstepRender(t)

	// Each pane's own pane-local pill row. These happen to be equal for
	// this fixture, but that is a coincidence of the two chrome heights
	// and is deliberately NOT asserted -- pane-local row position is not
	// a shared property (divergence 5).
	const pill = "🎉 2"
	msgRow := lockstepRow(t, msgPane, "messages pane", pill)
	thrRow := lockstepRow(t, thrPane, "thread pane", pill)

	// SHARED: the clickable column extent of the pill is identical.
	msgCols := lockstepHitColumns(func(col int) bool {
		_, e, ok := mm.HitTestReaction(msgRow-mm.ChromeHeight(), col)
		return ok && e == "tada"
	})
	thrCols := lockstepHitColumns(func(col int) bool {
		_, e, ok := tm.HitTestReaction(thrRow, col)
		return ok && e == "tada"
	})
	if len(msgCols) == 0 {
		t.Fatalf("messages pane reported no reaction hit on the pill row; assertion would be vacuous")
	}
	if !slices.Equal(msgCols, thrCols) {
		t.Errorf("reaction pill column extent differs: messages=%v thread=%v", msgCols, thrCols)
	}

	// DIVERGENT (divergence 6): messages' row is content-relative -- the
	// app-level mouse handler subtracts ChromeHeight() before calling
	// (messages/model.go:2510-2514). thread's row is pane-local and
	// already includes chromeHeight (thread/model.go:1767-1772). Each
	// model must MISS at the other's row, or the frames would in fact
	// agree and Phase 3 would have nothing to reconcile.
	col := msgCols[0]
	if _, _, ok := mm.HitTestReaction(msgRow, col); ok {
		t.Errorf("messages.HitTestReaction now accepts a pane-local row (%d); "+
			"the frames have converged -- update divergence 6", msgRow)
	}
	if _, _, ok := tm.HitTestReaction(thrRow-tm.chromeHeight, col); ok {
		t.Errorf("thread.HitTestReaction now accepts a content-relative row (%d); "+
			"the frames have converged -- update divergence 6", thrRow-tm.chromeHeight)
	}

	// DIVERGENT (divergence 2): the same visual pill yields a different
	// index, because thread indexes replies (the parent is not a reply)
	// while messages indexes messages.
	msgIdx, _, _ := mm.HitTestReaction(msgRow-mm.ChromeHeight(), col)
	thrIdx, _, _ := tm.HitTestReaction(thrRow, col)
	if msgIdx != 1 || thrIdx != 0 {
		t.Errorf("index bases changed: messages=%d (want 1) thread=%d (want 0); "+
			"update divergence 2", msgIdx, thrIdx)
	}
}

// lockstepHitColumns returns every column in [0, lockstepWidth) for
// which hit reports true.
func lockstepHitColumns(hit func(col int) bool) []int {
	var cols []int
	for c := 0; c < lockstepWidth; c++ {
		if hit(c) {
			cols = append(cols, c)
		}
	}
	return cols
}

// Documented divergences between messages.Model and thread.Model.
//
// This list is the specification for the divergence hooks Phase 3's
// shared pane package must provide. Everything NOT listed here is
// expected to render identically and is asserted above.
//
// Adding an entry is a design decision: it declares "this difference is
// intended". If behaviour drifts and you cannot justify it as intended,
// the fix belongs in the model, not in this list.
//
// Verified against the code at the commit that introduced this file.
// The task brief supplied 7 items from grep; 5 are confirmed below, 1
// (the unread boundary) was wrong about which half diverges and is
// corrected in item 8, and 8 further divergences were found.
//
//  1. Construction. messages.New(msgs, channelName) returns a VALUE
//     Model preloaded with content (messages/model.go:597); thread.New()
//     returns a *Model that is empty until SetThread
//     (thread/model.go:226). Phase 3 must pick one shape; the value
//     return is why callers of messages.New must take an address before
//     calling any method.
//
//  2. Parent row. thread renders m.parent as a pseudo-row above the
//     replies, addressable through the parentSelected = -1 sentinel
//     (thread/model.go:94, 1333-1361); messages has no such row. The
//     consequence is an index-base divergence: for the same on-screen
//     pill, HitTestReaction returns a REPLY index in thread and a
//     MESSAGE index in messages. Asserted by
//     TestLockstep_ReactionHitTestFrames.
//
//  3. Leading day divider. messages seeds lastDate empty
//     (messages/model.go:1773), so the FIRST message always gets a day
//     divider above it. thread seeds lastDate from the parent's day
//     (thread/model.go:1559), so no divider is ever drawn above the
//     parent, and none above the first reply that shares the parent's
//     day. Only the day TRANSITION is shared, which is what the fixture
//     above exercises.
//
//  4. Inter-row separator. messages separates messages with a blank
//     row; thread draws a full-width "─" rule after the parent
//     (thread/model.go:1355-1359) and between replies
//     (thread/model.go:1522). This is why only intra-message row gaps
//     are compared above.
//
//  5. Pane chrome. messages renders " # <channel>" plus an optional
//     wrapped topic (messages/model.go:2840-2855) -- 1 row for the
//     fixture -- and exports ChromeHeight(). thread renders
//     "Thread  N replies" plus a "-" rule (thread/model.go:1296-1307)
//     -- 2 rows -- and keeps chromeHeight unexported.
//
//  6. Reaction hit-test row frame. messages.HitTestReaction takes a
//     CONTENT-relative row; the app-level mouse handler subtracts
//     ChromeHeight() first (messages/model.go:2510-2514).
//     thread.HitTestReaction takes a PANE-local row that still includes
//     chromeHeight (thread/model.go:1767-1772). Column extents are
//     identical. Phase 3 must pick one frame; this is the divergence
//     most likely to break silently, so it is asserted rather than
//     merely listed.
//
//  7. Scroll mechanism. thread drives a bubbles/viewport
//     (thread/model.go:107); messages hand-rolls yOffset specifically to
//     avoid viewport's per-SetContent ansi.StringWidth pass
//     (messages/model.go:279-283). Phase 3 must pick one, and the
//     hand-rolled side exists for a measured reason.
//
//  8. Unread landmark -- API and structure, NOT rendering. The brief
//     listed the landmark as a rendering divergence. It is not: both
//     panes emit a centred bold "── new ──" row that is byte-identical,
//     and the assertions above pin that. What diverges is
//     (a) the setter -- messages.SetLastReadTS(ts) vs
//     thread.SetUnreadBoundary(ts); (b) lifecycle -- thread clears its
//     boundary when the thread identity changes
//     (thread/model.go:310-312), messages never self-clears; and
//     (c) structure -- messages inserts the landmark as a viewEntry with
//     msgIdx -1 (messages/model.go:1762-1771), so it participates in
//     selection anchor resolution, while thread appends a bare row
//     outside any entry (thread/model.go:1616).
//
//  9. Reply-count affordance. messages renders "[N replies ->]" when
//     msg.ReplyCount > 0 (messages/model.go:1983-1989) and exposes
//     IncrementReplyCount; thread renders no such affordance -- it IS
//     the thread. thread.ReplyCount() is unrelated: it is the open
//     thread's own reply total, used only for the header.
//
// 10. Search-term highlighting. messages has SetSearchTerms and calls
//     HighlightSearchTerms in the body render path
//     (messages/model.go:1968-1974); thread has neither.
//
// 11. Loading state. messages has SetLoading / IsLoading /
//     SetSpinnerFrame and renders a braille spinner with
//     "Loading messages..." (messages/model.go:2892-2894); thread has
//     none of them.
//
// 12. Empty state. thread renders "No thread selected" when IsEmpty
//     (thread/model.go:1253-1260) and "No replies yet" for a
//     reply-less thread (thread/model.go:1373); messages has no
//     equivalent -- its empty pane is either the loading spinner or
//     blank.
//
// 13. Selection entry points. messages splits View / ViewBare and
//     offers ApplySelectionToBordered so the App layer can cache a
//     selection-free bordered render and repaint the selection as a
//     cheap post-pass (messages/model.go:3315-3332, 3457); thread has
//     only View, which always paints the selection. ClickAt also
//     diverges in signature: messages.ClickAt(y) returns bool,
//     thread.ClickAt(y) returns nothing.
//
// 14. Outer background wrapper. thread.View wraps its entire output in
//     a Width/Height/MaxHeight/Background style
//     (thread/model.go:1764); messages.View returns
//     chrome + "\n" + rows unwrapped (messages/model.go:3312). The
//     panes are visually identical but not byte-identical in ANSI,
//     which is why the assertions above compare ANSI-stripped rows.
//
// 15. Inline-image click-to-preview. messages has HitTest and
//     HandleImageReady for opening an image preview from a message;
//     thread deliberately discards imgrender's per-block hits
//     (thread/model.go:1788-1790) and has no HitTest. Scoped out in
//     thread/model.go:203-206.
