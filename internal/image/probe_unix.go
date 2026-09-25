//go:build unix

package image

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// pollProbe reads from fd up to timeout, handing everything collected
// so far to scan until it reports a complete reply. Synchronous (no
// goroutine) so no leak is possible. Uses poll(2) with a millisecond
// timeout to wait for readable data, then performs ONE read(2) per
// ready cycle into a fixed-size buffer.
//
// scan is scanForOK for the kitty graphics probe and scanForSixelDA for
// the sixel DA1 probe: it returns (matched, ok) where matched means the
// reply is complete and ok is the capability answer.
//
// Returns (ok, bytesRead, reason). reason is a short identifier
// suitable for logging:
//
//	"got_ok"          -- an affirmative reply was seen
//	"got_reply_no_ok" -- a complete reply was seen, but negative
//	"timeout"         -- the deadline elapsed before any reply
//	"poll_err:<err>"  -- poll(2) returned an error
//	"read_err:<err>"  -- read(2) returned an error
//	"read_eof"        -- read returned 0 bytes (fd closed)
//
// Bytes consumed from fd that aren't part of a kitty reply are
// silently discarded -- they were not destined for any other consumer
// at this point in startup (bubbletea hasn't started reading yet).
// The startup window is ~200ms; the rare user keystroke landing in
// that window would also have been eaten by the prior goroutine-based
// implementation, so this is not a regression.
func pollProbe(fd int, timeout time.Duration, scan func([]byte) (bool, bool)) (bool, int, string) {
	deadline := time.Now().Add(timeout)
	var collected []byte
	var buf [256]byte
	bytesRead := 0

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, bytesRead, "timeout"
		}
		ms := int(remaining / time.Millisecond)
		if ms <= 0 {
			ms = 1 // poll(0) is "return immediately", we want at least 1ms
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, ms)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return false, bytesRead, "poll_err:" + err.Error()
		}
		if n == 0 {
			return false, bytesRead, "timeout"
		}
		// Check for hangup / error on the fd.
		if fds[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 && fds[0].Revents&unix.POLLIN == 0 {
			return false, bytesRead, "poll_hup"
		}
		rn, err := unix.Read(fd, buf[:])
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return false, bytesRead, "read_err:" + err.Error()
		}
		if rn == 0 {
			return false, bytesRead, "read_eof"
		}
		bytesRead += rn
		collected = append(collected, buf[:rn]...)
		if matched, ok := scan(collected); matched {
			if ok {
				return true, bytesRead, "got_ok"
			}
			return false, bytesRead, "got_reply_no_ok"
		}
	}
}
