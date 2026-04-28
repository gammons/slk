package emoji

import (
	"errors"
	"io"
	"os"
	"time"

	emojilib "github.com/kyokomi/emoji/v2"
	"golang.org/x/term"
)

// InitOptions configures the Init function.
type InitOptions struct {
	// Codemap is the kyokomi-style emoji codemap (":name:" → unicode).
	// Defaults to emojilib.CodeMap() if empty.
	Codemap map[string]string

	// PerProbeTimeout is the timeout for each individual DSR query.
	// Defaults to 200ms.
	PerProbeTimeout time.Duration

	// ProgressFunc, if set, is called periodically during the probe with
	// (current, total) so the caller can render progress.
	ProgressFunc func(current, total int)

	// SkipProbe disables probing entirely. Width() falls back to lipgloss.
	SkipProbe bool

	// ForceProbe runs the probe even if a valid cache exists.
	ForceProbe bool
}

// Init loads the cache or runs a fresh probe. Must be called once at
// startup, before bubbletea begins. After this returns, Width() is safe
// to call from anywhere.
//
// On any error, Width() falls back to lipgloss.Width(). The error is
// returned for logging but does not prevent the app from running.
func Init(opts InitOptions) error {
	// If we'll need to probe, put the terminal in raw mode for the duration.
	// We don't know whether the probe is needed without consulting the cache,
	// but raw mode is harmless if no probe runs (we'll restore on return).
	if !opts.SkipProbe {
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			st, err := term.MakeRaw(fd)
			if err == nil {
				defer term.Restore(fd, st)
			}
		}
	}

	_, _, err := initWithIO(opts, os.Stdout, os.Stdin)
	return err
}

// initWithIO is the testable core. Returns (loadedFromCache, probed, error).
func initWithIO(opts InitOptions, out io.Writer, in io.Reader) (bool, bool, error) {
	if opts.Codemap == nil {
		opts.Codemap = emojilib.CodeMap()
	}
	if opts.PerProbeTimeout == 0 {
		opts.PerProbeTimeout = 200 * time.Millisecond
	}

	terminalKey := IdentifyTerminal()
	cachePath := CachePath(terminalKey)
	wantHash := codemapHash(opts.Codemap)

	// Try cache load first (unless ForceProbe).
	if !opts.ForceProbe {
		if c, err := LoadCache(cachePath); err == nil {
			if c.Version == CacheVersion && c.CodemapHash == wantHash {
				setWidthMap(c.Widths)
				return true, false, nil
			}
		}
	}

	if opts.SkipProbe {
		return false, false, nil
	}

	if out == nil || in == nil {
		return false, false, errors.New("no terminal I/O available; skipping probe")
	}

	// Run probe.
	widths, err := probeAll(out, in, opts.Codemap, opts.PerProbeTimeout)
	if err != nil {
		return false, false, err
	}

	setWidthMap(widths)

	// Write cache (best effort; failure is silently ignored — the probe
	// will simply re-run on next launch).
	c := &Cache{
		Version:     CacheVersion,
		Terminal:    terminalKey,
		ProbedAt:    time.Now().UTC().Format(time.RFC3339),
		CodemapHash: wantHash,
		Widths:      widths,
	}
	_ = SaveCache(cachePath, c)

	return false, true, nil
}
