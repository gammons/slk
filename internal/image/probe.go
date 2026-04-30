package image

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// ProbeKittyGraphics sends a tiny image upload with response requested and
// waits up to timeout for the OK reply. Returns true if the terminal
// acknowledges. Used at startup to downgrade ProtoKitty when the terminal
// claims kitty support but doesn't actually deliver (e.g., iTerm2's
// limited kitty implementation).
//
// Inputs:
//
//	w:       terminal writer (typically os.Stdout)
//	r:       terminal reader (typically os.Stdin in raw mode)
//	timeout: how long to wait for the reply
func ProbeKittyGraphics(w io.Writer, r io.Reader, timeout time.Duration) bool {
	// Minimal valid 1x1 PNG.
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+P+/HgAFhAJ/wlseKgAAAABJRU5ErkJggg=="
	const probeID = 9999
	header := fmt.Sprintf("a=T,f=100,t=d,i=%d,q=0", probeID)
	if _, err := fmt.Fprintf(w, "\x1b_G%s;%s\x1b\\", header, tinyPNG); err != nil {
		return false
	}

	type result struct {
		ok bool
	}
	ch := make(chan result, 1)
	go func() {
		br := bufio.NewReader(r)
		for {
			b, err := br.ReadByte()
			if err != nil {
				ch <- result{false}
				return
			}
			if b != 0x1b {
				continue
			}
			next, err := br.ReadByte()
			if err != nil || next != '_' {
				continue
			}
			next, err = br.ReadByte()
			if err != nil || next != 'G' {
				continue
			}
			payload, err := br.ReadString(0x1b)
			if err != nil {
				ch <- result{false}
				return
			}
			ch <- result{strings.Contains(payload, ";OK")}
			return
		}
	}()

	select {
	case res := <-ch:
		return res.ok
	case <-time.After(timeout):
		return false
	}
}
