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

// bufferedTermReader runs a single goroutine that reads bytes from an
// underlying io.Reader (typically raw-mode stdin) and pushes them to a
// buffered channel. Probe code consumes bytes from the channel with a
// deadline.
//
// This architecture solves a subtle bug in serial DSR probing: when a
// probe times out, the prior probe's delayed response can arrive after
// the next probe is already issued. With a per-call goroutine reading
// directly from stdin, the next probe's reader would consume the prior
// probe's bytes and attribute the wrong width to the wrong emoji,
// cascading errors through the cache.
//
// With a single long-lived reader feeding a channel, each probe can
// explicitly Drain() any leftover bytes from the prior timed-out probe
// before issuing its own query.
type bufferedTermReader struct {
	bytes chan byte
	done  chan struct{}
}

func newBufferedTermReader(r io.Reader) *bufferedTermReader {
	b := &bufferedTermReader{
		// Buffer enough for ~10 typical DSR responses (~6 bytes each)
		bytes: make(chan byte, 64),
		done:  make(chan struct{}),
	}
	go func() {
		one := make([]byte, 1)
		for {
			n, err := r.Read(one)
			if n > 0 {
				select {
				case b.bytes <- one[0]:
				case <-b.done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return b
}

// Close signals the reader goroutine to exit. The goroutine may still
// be blocked in r.Read on a real terminal; this just unblocks any
// pending channel send. The leaked read on stdin is unavoidable without
// fd-level cancellation.
func (b *bufferedTermReader) Close() {
	close(b.done)
}

// Drain consumes any bytes currently in the buffer without blocking.
// It uses a tiny per-byte deadline; if no byte arrives within that
// window, the buffer is considered empty.
func (b *bufferedTermReader) Drain() {
	const drainTimeout = 5 * time.Millisecond
	const maxDrain = 256
	for i := 0; i < maxDrain; i++ {
		select {
		case <-b.bytes:
			// consumed and discarded
		case <-time.After(drainTimeout):
			return
		}
	}
}

// ReadByte returns the next byte from the buffer or errProbeTimeout if
// none arrives before the deadline.
func (b *bufferedTermReader) ReadByte(deadline time.Time) (byte, error) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, errProbeTimeout
	}
	select {
	case c := <-b.bytes:
		return c, nil
	case <-time.After(remaining):
		return 0, errProbeTimeout
	}
}

// probeOne renders a single emoji and queries the terminal for cursor
// position to determine the rendered width.
//
// The caller is responsible for putting the terminal in raw mode and
// for draining the buffer between calls (probeAll handles this).
//
// Procedure:
//  1. Write \r to move cursor to column 1
//  2. Write the emoji
//  3. Write CSI 6n (DSR query)
//  4. Read response with a deadline of timeout
//  5. Parse column from response
//  6. Width = column - 1 (cursor was at column 1 before emoji)
//  7. Write \r\x1b[K to clear the line for the next probe
func probeOne(out io.Writer, in *bufferedTermReader, emoji string, timeout time.Duration) (int, error) {
	// Drain any pending bytes from a previously timed-out probe.
	in.Drain()

	// Move to column 1, render emoji, query position.
	if _, err := fmt.Fprint(out, "\r", emoji, "\x1b[6n"); err != nil {
		return 0, err
	}

	// Read the DSR response, byte-by-byte, until 'R' or timeout.
	deadline := time.Now().Add(timeout)
	var buf []byte
	for {
		c, err := in.ReadByte(deadline)
		if err != nil {
			fmt.Fprint(out, "\r\x1b[K")
			return 0, err
		}
		buf = append(buf, c)
		if c == 'R' {
			break
		}
		// Defensive: don't read forever even if 'R' never comes.
		if len(buf) > 32 {
			fmt.Fprint(out, "\r\x1b[K")
			return 0, errors.New("DSR response too long")
		}
	}

	// Clear line for next probe.
	fmt.Fprint(out, "\r\x1b[K")

	col, perr := parseDSRResponse(buf)
	if perr != nil {
		return 0, perr
	}
	width := col - 1
	if width < 0 || width > maxPlausibleEmojiWidth {
		return 0, fmt.Errorf("implausible width: %d", width)
	}
	return width, nil
}

// probeAll iterates over the kyokomi codemap and probes the rendered
// width of each unique emoji. Returns a map from emoji string → width.
//
// Skips duplicate emoji (same Unicode value mapped to multiple shortcodes).
// On per-emoji timeout or parse error, the emoji is omitted from the
// result (not added to cache). The caller decides whether to abort or
// continue based on the error count.
//
// The codemap must use kyokomi's format: ":name:" → unicode-with-trailing-space.
// We trim the trailing ReplacePadding space before probing.
func probeAll(out io.Writer, in io.Reader, codemap map[string]string, perProbeTimeout time.Duration) (map[string]int, error) {
	br := newBufferedTermReader(in)
	defer br.Close()

	result := make(map[string]int, len(codemap))
	seen := make(map[string]bool)

	// Sanity check: probe a known-1-wide ASCII char first.
	w, err := probeOne(out, br, "a", perProbeTimeout)
	if err != nil {
		return nil, fmt.Errorf("sanity probe failed: %w", err)
	}
	if w != 1 {
		return nil, fmt.Errorf("sanity probe returned width %d for 'a'; terminal does not support DSR correctly", w)
	}

	for _, uni := range codemap {
		// kyokomi adds a trailing space; strip it before probing
		emoji := uni
		if len(emoji) > 0 && emoji[len(emoji)-1] == ' ' {
			emoji = emoji[:len(emoji)-1]
		}
		if emoji == "" || seen[emoji] {
			continue
		}
		seen[emoji] = true

		width, err := probeOne(out, br, emoji, perProbeTimeout)
		if err != nil {
			// Skip this emoji; it'll fall back to lipgloss.Width.
			continue
		}
		result[emoji] = width
	}

	// Also probe a curated list of extra characters that aren't in the
	// kyokomi codemap but are commonly used as reactions or appear in
	// message text (stars, hearts, arrows, misc symbols).
	for _, emoji := range extraProbeChars {
		if seen[emoji] {
			continue
		}
		seen[emoji] = true
		width, err := probeOne(out, br, emoji, perProbeTimeout)
		if err != nil {
			continue
		}
		result[emoji] = width
	}

	return result, nil
}
