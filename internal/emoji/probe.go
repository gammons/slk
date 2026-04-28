package emoji

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

var errProbeTimeout = errors.New("probe timed out waiting for DSR response")

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
		return 0, errors.New("invalid column: " + err.Error())
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
	if width < 0 || width > 4 {
		return 0, errors.New("implausible width: " + strconv.Itoa(width))
	}
	return width, nil
}

// readDSRResponse reads bytes from r until 'R' is seen or timeout elapses.
func readDSRResponse(r io.Reader, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var buf []byte
	one := make([]byte, 1)

	for time.Now().Before(deadline) {
		// Use a goroutine + channel for non-blocking-ish read
		readCh := make(chan struct {
			n   int
			err error
		}, 1)
		go func() {
			n, err := r.Read(one)
			readCh <- struct {
				n   int
				err error
			}{n, err}
		}()

		remaining := time.Until(deadline)
		select {
		case res := <-readCh:
			if res.err != nil && res.n == 0 {
				return buf, res.err
			}
			buf = append(buf, one[0])
			if one[0] == 'R' {
				return buf, nil
			}
		case <-time.After(remaining):
			return buf, errProbeTimeout
		}
	}
	return buf, errProbeTimeout
}
