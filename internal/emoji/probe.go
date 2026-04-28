package emoji

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

var errProbeTimeout = errors.New("probe timed out waiting for DSR response")

// maxPlausibleEmojiWidth bounds rendered widths we accept from the
// terminal. Real emoji never exceed 2 cells; we allow 4 as a generous
// margin for terminal quirks.
const maxPlausibleEmojiWidth = 4

// parseDSRResponse parses a Device Status Report response of the form
// "\x1b[<row>;<col>R" and returns the column number (1-indexed).
// Returns an error if the response is malformed or column is < 1.
func parseDSRResponse(b []byte) (int, error) {
	// Must start with ESC [
	if len(b) < 4 || b[0] != 0x1B || b[1] != '[' {
		return 0, errors.New("missing CSI prefix")
	}
	// Must end with R
	if b[len(b)-1] != 'R' {
		return 0, errors.New("missing R terminator")
	}

	// Find semicolon
	semi := -1
	for i := 2; i < len(b)-1; i++ {
		if b[i] == ';' {
			semi = i
			break
		}
	}
	if semi == -1 {
		return 0, errors.New("missing semicolon")
	}

	// Parse column (between semi+1 and len-1)
	colStr := string(b[semi+1 : len(b)-1])
	col, err := strconv.Atoi(colStr)
	if err != nil {
		return 0, fmt.Errorf("invalid column: %w", err)
	}
	if col < 1 {
		return 0, errors.New("column must be >= 1")
	}
	return col, nil
}

// probeOne renders a single emoji and queries the terminal for cursor
// position to determine the rendered width.
//
// The caller is responsible for putting the terminal in raw mode.
//
// Procedure:
//  1. Write \r to move cursor to column 1
//  2. Write the emoji
//  3. Write CSI 6n (DSR query)
//  4. Read response with a deadline of timeout
//  5. Parse column from response
//  6. Width = column - 1 (cursor was at column 1 before emoji)
//  7. Write \r\x1b[K to clear the line for the next probe
func probeOne(out io.Writer, in io.Reader, emoji string, timeout time.Duration) (int, error) {
	// Move to column 1, render emoji, query position
	if _, err := fmt.Fprint(out, "\r", emoji, "\x1b[6n"); err != nil {
		return 0, err
	}

	// Read the DSR response. We read byte-by-byte until we see 'R' or hit timeout.
	resp, err := readDSRResponse(in, timeout)
	if err != nil {
		// Always clear the line even on error
		fmt.Fprint(out, "\r\x1b[K")
		return 0, err
	}

	// Clear line for next probe
	fmt.Fprint(out, "\r\x1b[K")

	col, perr := parseDSRResponse(resp)
	if perr != nil {
		return 0, perr
	}
	width := col - 1
	if width < 0 || width > maxPlausibleEmojiWidth {
		return 0, fmt.Errorf("implausible width: %d", width)
	}
	return width, nil
}

// readDSRResponse reads bytes from r until 'R' is seen or timeout elapses.
//
// Concurrency model: each iteration spawns a single-shot reader goroutine
// that reads exactly one byte. On the success path (byte arrives before
// the deadline), the goroutine sends its result and exits — no leaked
// goroutines. On timeout, the inner goroutine is still blocked in
// r.Read; we use a per-iteration done channel to ensure it can exit
// cleanly when its byte eventually arrives instead of writing to a
// stale results channel. Each goroutine owns its own one-byte buffer,
// so a leaked goroutine cannot race with subsequent reads.
//
// Real-terminal caveat: with a raw-mode os.Stdin (no read deadline),
// a leaked goroutine from a timed-out probe will still consume the
// next byte that arrives — which may be a delayed DSR response from
// the prior query. Callers that probe repeatedly should consider
// draining stdin before the next probe.
func readDSRResponse(r io.Reader, timeout time.Duration) ([]byte, error) {
	type readResult struct {
		b   byte
		err error
	}

	deadline := time.Now().Add(timeout)
	var buf []byte

	for time.Now().Before(deadline) {
		done := make(chan struct{})
		results := make(chan readResult, 1)
		go func() {
			one := make([]byte, 1)
			n, err := r.Read(one)
			res := readResult{err: err}
			if n > 0 {
				res.b = one[0]
				res.err = nil
			}
			select {
			case results <- res:
			case <-done:
			}
		}()

		remaining := time.Until(deadline)
		select {
		case res := <-results:
			if res.err != nil {
				close(done)
				return buf, res.err
			}
			buf = append(buf, res.b)
			close(done)
			if res.b == 'R' {
				return buf, nil
			}
		case <-time.After(remaining):
			close(done)
			return buf, errProbeTimeout
		}
	}
	return buf, errProbeTimeout
}
