package messages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func spacingNotice(ts string, replies int) MessageItem {
	return MessageItem{TS: ts, UserID: "U000", Text: "A huddle started", Timestamp: "10:30 AM",
		Subtype: "huddle_thread", ReplyCount: replies}
}

func spacingMsg(ts, text string) MessageItem {
	return MessageItem{TS: ts, UserID: "U001", UserName: "alice", Text: text, Timestamp: "10:31 AM"}
}

// spacingRows draws the pane and returns its rows as plain text with the
// border and trailing blanks trimmed, the scroll window cut after the
// last non-empty row.
func spacingRows(t *testing.T, msgs []MessageItem, selected int) []string {
	t.Helper()
	m := New(msgs, "general")
	if selected >= 0 {
		m.selected = selected
	}
	rows := strings.Split(ansi.Strip(m.View(24, 60)), "\n")
	for i := range rows {
		rows[i] = strings.TrimSpace(strings.TrimPrefix(rows[i], "▌"))
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows[1:] // drop the channel header
}

func rowIndex(t *testing.T, rows []string, sub string) int {
	t.Helper()
	for i, r := range rows {
		if strings.Contains(r, sub) {
			return i
		}
	}
	t.Fatalf("no row containing %q in:\n%s", sub, strings.Join(rows, "\n"))
	return -1
}

const (
	day1 = "1700000000.0" // 2023-11-14 UTC
	day2 = "1700100000.0" // the next day
)

// A notice first after a date separator gets one blank row between them,
// like a notice anywhere else.
func TestNoticeAfterDateSeparatorHasBlankRow(t *testing.T) {
	for _, sel := range []int{-1, 0, 1} {
		rows := spacingRows(t, []MessageItem{spacingMsg(day1, "one"), spacingNotice(day2, 0), spacingMsg("1700100100.0", "two")}, sel)
		sep := rowIndex(t, rows, "── ")
		first := rowIndex(t, rows[sep+1:], "── ") + sep + 1 // second separator
		n := rowIndex(t, rows, "A huddle started")
		if n != first+2 || rows[first+1] != "" {
			t.Errorf("sel %d: notice at %d, separator at %d, want one blank row between:\n%s", sel, n, first, strings.Join(rows, "\n"))
		}
	}
}

// First message in the pane is a notice: the date separator above it is
// followed by a blank row, and the reply line stays glued to the notice.
func TestNoticeFirstInPaneWithReplies(t *testing.T) {
	rows := spacingRows(t, []MessageItem{spacingNotice(day1, 2), spacingMsg("1700000100.0", "two")}, 0)
	sep := rowIndex(t, rows, "── ")
	n := rowIndex(t, rows, "A huddle started")
	if n != sep+2 || rows[sep+1] != "" {
		t.Errorf("want blank between separator and notice:\n%s", strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[n+1], "[2 replies ->]") {
		t.Errorf("reply line not under notice:\n%s", strings.Join(rows, "\n"))
	}
	if rows[n+2] != "" || !strings.Contains(rows[n+3], "alice") {
		t.Errorf("want one blank row before the next message:\n%s", strings.Join(rows, "\n"))
	}
}

// Spacing that was already right is unchanged: a notice after a message
// on the same day, a message after a notice, two notices in a row, and a
// normal message first after a separator (no blank row).
func TestNoticeSpacingElsewhereUnchanged(t *testing.T) {
	rows := spacingRows(t, []MessageItem{
		spacingMsg(day1, "one"), spacingNotice("1700000100.0", 0), spacingNotice("1700000200.0", 0), spacingMsg("1700000300.0", "two"),
	}, -1)
	if rows[0] == "" || !strings.Contains(rows[0], "── ") || !strings.Contains(rows[1], "alice") {
		t.Errorf("normal message must sit directly under the separator:\n%s", strings.Join(rows, "\n"))
	}
	n1 := rowIndex(t, rows, "A huddle started")
	if rows[n1-1] != "" || rows[n1-2] != "one" {
		t.Errorf("want one blank row before a notice after a message:\n%s", strings.Join(rows, "\n"))
	}
	if rows[n1+1] != "" || !strings.Contains(rows[n1+2], "A huddle started") {
		t.Errorf("want one blank row between two notices:\n%s", strings.Join(rows, "\n"))
	}
	if rows[n1+3] != "" || !strings.Contains(rows[n1+4], "alice") {
		t.Errorf("want one blank row before a message after a notice:\n%s", strings.Join(rows, "\n"))
	}
}

// The blank row is a decoration entry: it is not selectable, so j/k and
// the message indices are unaffected.
func TestNoticeSpacerIsNotSelectable(t *testing.T) {
	m := New([]MessageItem{spacingNotice(day1, 0), spacingMsg("1700000100.0", "two")}, "general")
	m.View(24, 60)
	sel := 0
	for _, e := range m.list.Entries() {
		if e.Selectable {
			sel++
		}
	}
	if sel != 2 {
		t.Errorf("selectable entries = %d, want 2", sel)
	}
}
