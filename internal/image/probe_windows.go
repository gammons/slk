//go:build windows

package image

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procReadConsoleInputW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")

// pollProbe is probe_unix.go's pollProbe for a Windows console: fd is the
// console input handle (os.Stdin's), and the reply is read from console
// input records rather than a byte stream. Windows Terminal (1.22 and
// later) answers DA1 and XTWINOPS queries through console input, as key
// events carrying the reply's characters.
//
// It waits on the handle with WaitForSingleObject, then takes what is
// queued with ReadConsoleInputW, which cannot block once the wait says
// input is there. ReadFile could: a focus or mouse event signals the
// handle but gives ReadFile nothing to return, and a read blocked past
// the deadline would eat the first key the user presses afterwards.
//
// Returns (ok, bytesRead, reason) as the unix version does, with
// "wait_err:<err>" and "read_err:<err>" for failed system calls. A
// handle that is not a console (redirected stdin) fails the read and
// reports false, so the caller keeps half-block.
func pollProbe(fd int, timeout time.Duration, scan func([]byte) (bool, bool)) (bool, int, string) {
	h := windows.Handle(fd)
	deadline := time.Now().Add(timeout)
	var collected []byte
	var pending uint16
	var recs [32]consoleInputRecord

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, len(collected), "timeout"
		}
		ms := uint32(remaining / time.Millisecond)
		if ms == 0 {
			ms = 1
		}
		ev, err := windows.WaitForSingleObject(h, ms)
		if err != nil {
			return false, len(collected), "wait_err:" + err.Error()
		}
		if ev == uint32(windows.WAIT_TIMEOUT) {
			return false, len(collected), "timeout"
		}
		var n uint32
		r1, _, err := procReadConsoleInputW.Call(uintptr(h), uintptr(unsafe.Pointer(&recs[0])), uintptr(len(recs)), uintptr(unsafe.Pointer(&n)))
		if r1 == 0 {
			return false, len(collected), "read_err:" + err.Error()
		}
		collected, pending = appendConsoleChars(collected, recs[:n], pending)
		if matched, ok := scan(collected); matched {
			if ok {
				return true, len(collected), "got_ok"
			}
			return false, len(collected), "got_reply_no_ok"
		}
	}
}
