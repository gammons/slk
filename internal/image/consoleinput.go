package image

import (
	"unicode/utf16"
	"unicode/utf8"
)

// Console input records: how a Windows console hands over input, and so
// how a terminal's reply to a query reaches pollProbe there
// (probe_windows.go). Kept free of build tags so the decoding is tested
// on every platform.

// consoleKeyEvent is KEY_EVENT in an INPUT_RECORD's EventType.
const consoleKeyEvent = 0x0001

// consoleInputRecord is INPUT_RECORD with its union read as the
// KEY_EVENT_RECORD, the only event a reply arrives as: 20 bytes, the
// union 4-aligned after the 2-byte EventType.
type consoleInputRecord struct {
	EventType      uint16
	_              uint16
	KeyDown        int32
	RepeatCount    uint16
	VirtualKeyCode uint16
	VirtualScan    uint16
	UnicodeChar    uint16
	ControlKeys    uint32
}

// appendConsoleChars appends to buf, as UTF-8, the characters the
// key-down events in recs carry. A terminal's reply to a query arrives as
// one key-down per character; key-ups repeat them, and focus, mouse and
// resize events carry none, so all of those are skipped. pending holds a
// high surrogate whose low half is in the next record; it is returned
// updated.
func appendConsoleChars(buf []byte, recs []consoleInputRecord, pending uint16) ([]byte, uint16) {
	for _, r := range recs {
		if r.EventType != consoleKeyEvent || r.KeyDown == 0 || r.UnicodeChar == 0 {
			continue
		}
		c := r.UnicodeChar
		var ch rune
		switch {
		case utf16.IsSurrogate(rune(c)) && c < 0xDC00:
			pending = c
			continue
		case utf16.IsSurrogate(rune(c)):
			ch = utf16.DecodeRune(rune(pending), rune(c))
			pending = 0
		default:
			ch = rune(c)
		}
		n := int(r.RepeatCount)
		if n < 1 {
			n = 1
		}
		for i := 0; i < n; i++ {
			buf = utf8.AppendRune(buf, ch)
		}
	}
	return buf, pending
}
