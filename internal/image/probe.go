package image

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gammons/slk/internal/debuglog"
)

// ProbeKittyGraphics sends a tiny image upload with response requested
// and waits up to timeout for the OK reply. Returns true if the
// terminal acknowledges. Used at startup to downgrade ProtoKitty when
// the terminal claims kitty support but doesn't actually deliver
// (e.g., iTerm2's limited kitty implementation, or zellij / tmux with
// allow-passthrough=off swallowing the probe escape).
//
// Inputs:
//
//	w:       terminal writer (typically os.Stdout)
//	r:       terminal reader (typically os.Stdin in raw mode)
//	timeout: how long to wait for the reply
//
// Implementation note (issue #50): the production path uses pollProbe
// (poll(2) + read(2), see probe_unix.go) so the function is fully
// synchronous and spawns no goroutine. Earlier implementations spawned
// a goroutine that kept reading from r forever after the select-on-
// timeout returned. That leaked goroutine then raced bubbletea's input
// loop for every byte the user typed, discarding ~95% of keystrokes
// (most aren't 0x1b) and making slk unresponsive whenever the probe
// timed out -- which is exactly when the user is in zellij or in tmux
// with allow-passthrough=off, because the multiplexer swallows the
// probe escape and no reply ever arrives.
//
// The poll-based path needs r to be an *os.File (any Go file with a
// real fd works: os.Stdin, os.Pipe). For non-*os.File readers
// (blockingReader in tests), this falls back to the legacy goroutine-
// based probe; that path may leak but tests exit immediately so it
// doesn't matter.
func ProbeKittyGraphics(w io.Writer, r io.Reader, timeout time.Duration) bool {
	header := fmt.Sprintf("a=T,f=100,t=d,i=%d,q=0", kittyProbeID)
	return probeKitty(w, r, timeout, fmt.Sprintf("\x1b_G%s;%s\x1b\\", header, kittyProbePNG))
}

// ProbeKittyQuery is ProbeKittyGraphics with a query (a=q) instead of
// an upload: the terminal checks the image and answers, but stores
// and draws nothing. It is the probe for KittyEmulated. WezTerm
// answers a query, while it stays silent on ProbeKittyGraphics' a=T:
// it answers a transmit only when the transmit carries an image number
// (I=), and never answers a successful placement.
func ProbeKittyQuery(w io.Writer, r io.Reader, timeout time.Duration) bool {
	header := fmt.Sprintf("a=q,f=100,t=d,i=%d", kittyProbeID)
	return probeKitty(w, r, timeout, fmt.Sprintf("\x1b_G%s;%s\x1b\\", header, kittyProbePNG))
}

// kittyProbePNG is a minimal valid 1x1 PNG, base64-encoded.
const kittyProbePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+P+/HgAFhAJ/wlseKgAAAABJRU5ErkJggg=="

// kittyProbeID is the image ID both probes use.
const kittyProbeID = 9999

// probeKitty writes seq and waits up to timeout for a kitty graphics
// reply carrying OK. See ProbeKittyGraphics for why the poll path
// exists.
func probeKitty(w io.Writer, r io.Reader, timeout time.Duration, seq string) bool {
	if err := writeKittySequence(w, seq); err != nil {
		return false
	}

	start := time.Now()

	if f, ok := r.(*os.File); ok {
		ok, bytesRead, reason := pollProbe(int(f.Fd()), timeout, scanForOK)
		debuglog.ImgRender("probe: ok=%v reason=%s bytes_read=%d elapsed_ms=%d",
			ok, reason, bytesRead, time.Since(start).Milliseconds())
		return ok
	}

	// Test fallback for non-*os.File readers.
	return probeViaGoroutine(r, timeout)
}

// ProbeSixel sends a Primary Device Attributes query (CSI c) and reports
// whether the reply advertises sixel graphics — attribute 4 in the
// DA1 response, e.g. `\e[?62;1;4;6;22c` from xterm -ti vt340.
//
// This is the same runtime check img2sixel / chafa use, and it is the
// only reliable one: the set of sixel-capable terminals is open-ended
// (foot, xterm, mlterm, WezTerm, contour, DomTerm, toyterm, …) and
// several of them share a generic TERM value, so a TERM allowlist
// silently downgrades them to the half-block mosaic (issue #116).
//
// Every terminal answers DA1, including ones with no sixel support, so
// unlike the kitty probe a timeout here means "no reply at all" (a
// multiplexer swallowed it, or stdin isn't really the terminal) rather
// than "no sixel". Both outcomes are reported as false.
//
// Inputs match ProbeKittyGraphics: w is the terminal writer, r the
// terminal reader in raw mode, timeout the reply deadline. Must run
// before bubbletea takes over stdin.
func ProbeSixel(w io.Writer, r io.Reader, timeout time.Duration) bool {
	if _, err := io.WriteString(w, "\x1b[c"); err != nil {
		return false
	}

	start := time.Now()

	if f, ok := r.(*os.File); ok {
		ok, bytesRead, reason := pollProbe(int(f.Fd()), timeout, scanForSixelDA)
		debuglog.ImgRender("sixel probe: ok=%v reason=%s bytes_read=%d elapsed_ms=%d",
			ok, reason, bytesRead, time.Since(start).Milliseconds())
		return ok
	}

	// Test fallback for non-*os.File readers.
	return probeViaGoroutineScan(r, timeout, scanForSixelDA)
}

// ProbeCellSize asks the terminal for its cell size in pixels with the
// XTWINOPS query CSI 16 t, answered CSI 6 ; height ; width t. It is how
// the cell size is learnt where TIOCGWINSZ reports none: on Windows,
// which has no such call at all (Windows Terminal answers this query),
// and on a unix terminal that leaves the pixel fields zero. A terminal
// that does not support the query ignores it, so a timeout means "no
// answer" and ok is false.
//
// Inputs and timing match ProbeSixel: run in raw mode before bubbletea
// starts reading.
func ProbeCellSize(w io.Writer, r io.Reader, timeout time.Duration) (pxW, pxH int, ok bool) {
	if _, err := io.WriteString(w, "\x1b[16t"); err != nil {
		return 0, 0, false
	}
	var got []byte
	scan := func(buf []byte) (bool, bool) {
		matched, ok := scanForCellSize(buf)
		if matched {
			got = append(got[:0], buf...)
		}
		return matched, ok
	}

	start := time.Now()
	var replied bool
	if f, isFile := r.(*os.File); isFile {
		var bytesRead int
		var reason string
		replied, bytesRead, reason = pollProbe(int(f.Fd()), timeout, scan)
		debuglog.ImgRender("cell size probe: ok=%v reason=%s bytes_read=%d elapsed_ms=%d",
			replied, reason, bytesRead, time.Since(start).Milliseconds())
	} else {
		// Test fallback for non-*os.File readers.
		replied = probeViaGoroutineScan(r, timeout, scan)
	}
	if !replied {
		return 0, 0, false
	}
	pxW, pxH, ok = parseCellSize(got)
	return pxW, pxH, ok
}

// scanForCellSize returns (matched, ok): matched once a complete reply
// to CSI 16 t (CSI 6 ; H ; W t) is in buf, ok when it carries a usable
// size. Other replies the terminal sends meanwhile are skipped over.
func scanForCellSize(buf []byte) (matched, ok bool) {
	if _, found := findCellSize(buf); !found {
		return false, false
	}
	_, _, ok = parseCellSize(buf)
	return true, ok
}

// parseCellSize extracts (width, height) from the first complete
// CSI 6 ; H ; W t in buf. ok is false when there is none or either value
// is not a positive number of pixels.
func parseCellSize(buf []byte) (pxW, pxH int, ok bool) {
	body, found := findCellSize(buf)
	if !found {
		return 0, 0, false
	}
	parts := bytes.Split(body, []byte(";"))
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, okH := atoi(string(parts[0]))
	w, okW := atoi(string(parts[1]))
	if !okH || !okW {
		return 0, 0, false
	}
	return w, h, true
}

// findCellSize returns the "H;W" between "\x1b[6;" and the next 't'.
func findCellSize(buf []byte) (body []byte, found bool) {
	const prefix = "\x1b[6;"
	i := bytes.Index(buf, []byte(prefix))
	if i < 0 {
		return nil, false
	}
	tail := buf[i+len(prefix):]
	j := bytes.IndexByte(tail, 't')
	if j < 0 {
		return nil, false
	}
	return tail[:j], true
}

// probeViaGoroutineScan is the generic goroutine-based probe used for
// non-*os.File readers (tests only — see probeViaGoroutine). It reads
// byte by byte, re-running scan over everything collected so far, and
// stops at the first complete reply.
func probeViaGoroutineScan(r io.Reader, timeout time.Duration, scan func([]byte) (bool, bool)) bool {
	ch := make(chan bool, 1)
	go func() {
		br := bufio.NewReader(r)
		var collected []byte
		for {
			b, err := br.ReadByte()
			if err != nil {
				ch <- false
				return
			}
			collected = append(collected, b)
			if matched, ok := scan(collected); matched {
				ch <- ok
				return
			}
		}
	}()
	select {
	case ok := <-ch:
		return ok
	case <-time.After(timeout):
		return false
	}
}

// scanForSixelDA returns (matched, ok). matched is true once a complete
// DA1 reply (CSI ? … c) is present in buf; ok is true when that reply
// lists attribute 4 (sixel graphics). Attributes are semicolon-
// separated and must match exactly — "14" or "24" are unrelated
// capabilities, so a substring test would be wrong.
func scanForSixelDA(buf []byte) (matched, ok bool) {
	i := bytes.Index(buf, []byte("\x1b[?"))
	if i < 0 {
		return false, false
	}
	tail := buf[i+3:] // skip past \x1b[?
	j := bytes.IndexByte(tail, 'c')
	if j < 0 {
		return false, false
	}
	for _, attr := range bytes.Split(tail[:j], []byte(";")) {
		if bytes.Equal(attr, []byte("4")) {
			return true, true
		}
	}
	return true, false
}

// probeViaGoroutine is the legacy goroutine-based probe. Retained for
// tests that pass a non-*os.File reader. NOT used in production. The
// goroutine spawned here is intentionally not cleaned up on timeout;
// see the doc comment on ProbeKittyGraphics for why that's acceptable
// in test-only code paths.
func probeViaGoroutine(r io.Reader, timeout time.Duration) bool {
	type result struct{ ok bool }
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
				if err != nil {
					ch <- result{false}
					return
				}
				continue
			}
			next, err = br.ReadByte()
			if err != nil || next != 'G' {
				if err != nil {
					ch <- result{false}
					return
				}
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

// scanForOK returns (matched, ok). matched is true if a complete kitty
// graphics response (\x1b_G ... \x1b\\) is present in buf. ok is true
// when matched is true AND the payload contains ";OK". Used by both
// the poll-based and goroutine-based probe paths.
func scanForOK(buf []byte) (matched, ok bool) {
	i := bytes.Index(buf, []byte("\x1b_G"))
	if i < 0 {
		return false, false
	}
	tail := buf[i+3:] // skip past \x1b_G
	j := bytes.Index(tail, []byte("\x1b\\"))
	if j < 0 {
		return false, false
	}
	return true, bytes.Contains(tail[:j], []byte(";OK"))
}
