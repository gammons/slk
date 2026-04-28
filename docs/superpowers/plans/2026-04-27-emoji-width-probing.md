# Terminal Emoji Width Probing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace heuristic emoji width measurement with actual terminal probes via `CSI 6n` Device Status Reports, cached per terminal identity.

**Architecture:** Add a probe subsystem to `internal/emoji` that runs once on first launch (or when terminal identity changes), queries the terminal for the rendered width of every emoji in the kyokomi codemap, and caches the results to disk. A new `Width()` function uses the cache for emoji and falls back to `lipgloss.Width()` for everything else.

**Tech Stack:** Go, `golang.org/x/term` for raw mode, `github.com/rivo/uniseg` for grapheme clustering, `charm.land/lipgloss/v2` for fallback width measurement.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/emoji/terminal.go` | Detect terminal identity from environment; produce stable cache key |
| `internal/emoji/terminal_test.go` | Tests for identity detection across various env var combinations |
| `internal/emoji/probe.go` | Run DSR probe loop; serial query of each emoji; raw mode handling |
| `internal/emoji/probe_test.go` | Tests for probe response parsing using fake stdin/stdout |
| `internal/emoji/cache.go` | Load/save JSON cache; codemap hash; XDG path resolution |
| `internal/emoji/cache_test.go` | Tests for cache round-trip, invalidation, error handling |
| `internal/emoji/width.go` | Public `Width()` function; routing logic between cache and lipgloss |
| `internal/emoji/width_test.go` | Tests for ASCII bypass, cache hit, cache miss fallback |
| `internal/emoji/init.go` | `Init()` orchestrator: load cache → run probe → write cache |
| `internal/emoji/init_test.go` | Tests for init with cached, missing, corrupt cache scenarios |
| `internal/emoji/normalize.go` | (Removed in Task 11 — replaced by probed widths) |
| `internal/emoji/normalize_test.go` | (Removed in Task 11) |
| `cmd/slk/main.go` | Call `emoji.Init()` early in `main()`; handle `--probe-emoji` and `--no-emoji-probe` flags |
| `internal/ui/messages/model.go` | Replace `lipgloss.Width(candidate)` with `emoji.Width(candidate)` at line 585 |
| `internal/ui/thread/model.go` | Same replacement at the equivalent call site |
| `internal/ui/messages/render.go` | Remove `StripTextDefaultVS16` call; revert to plain `emoji.Sprint(text)` |

---

## Task 1: Terminal identity detection

**Files:**
- Create: `internal/emoji/terminal.go`
- Test: `internal/emoji/terminal_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/terminal_test.go`:

```go
package emoji

import (
	"testing"
)

func TestIdentifyTerminal(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "iTerm with version",
			env:  map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.0"},
			want: "iTerm.app_3.5.0",
		},
		{
			name: "ghostty",
			env:  map[string]string{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.0.0"},
			want: "ghostty_1.0.0",
		},
		{
			name: "TERM_PROGRAM without version",
			env:  map[string]string{"TERM_PROGRAM": "vscode"},
			want: "vscode",
		},
		{
			name: "kitty via env",
			env:  map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"},
			want: "kitty",
		},
		{
			name: "alacritty via env",
			env:  map[string]string{"ALACRITTY_LOG": "/tmp/alacritty.log", "TERM": "alacritty"},
			want: "alacritty",
		},
		{
			name: "wezterm with version",
			env:  map[string]string{"WEZTERM_PANE": "0", "WEZTERM_VERSION": "20240127"},
			want: "wezterm_20240127",
		},
		{
			name: "wezterm no version",
			env:  map[string]string{"WEZTERM_PANE": "0"},
			want: "wezterm",
		},
		{
			name: "fallback to TERM",
			env:  map[string]string{"TERM": "xterm-256color"},
			want: "xterm-256color",
		},
		{
			name: "no env vars",
			env:  map[string]string{},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := identifyTerminalFromEnv(tt.env)
			if got != tt.want {
				t.Errorf("identifyTerminalFromEnv(%v) = %q, want %q", tt.env, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestIdentifyTerminal -v`
Expected: FAIL with "undefined: identifyTerminalFromEnv"

- [ ] **Step 3: Implement terminal identity**

Create `internal/emoji/terminal.go`:

```go
package emoji

import "os"

// IdentifyTerminal returns a stable cache key for the current terminal
// based on environment variables.
func IdentifyTerminal() string {
	env := map[string]string{
		"TERM_PROGRAM":         os.Getenv("TERM_PROGRAM"),
		"TERM_PROGRAM_VERSION": os.Getenv("TERM_PROGRAM_VERSION"),
		"KITTY_WINDOW_ID":      os.Getenv("KITTY_WINDOW_ID"),
		"KITTY_VERSION":        os.Getenv("KITTY_VERSION"),
		"ALACRITTY_LOG":        os.Getenv("ALACRITTY_LOG"),
		"WEZTERM_PANE":         os.Getenv("WEZTERM_PANE"),
		"WEZTERM_VERSION":      os.Getenv("WEZTERM_VERSION"),
		"TERM":                 os.Getenv("TERM"),
	}
	return identifyTerminalFromEnv(env)
}

func identifyTerminalFromEnv(env map[string]string) string {
	if prog := env["TERM_PROGRAM"]; prog != "" {
		if ver := env["TERM_PROGRAM_VERSION"]; ver != "" {
			return prog + "_" + ver
		}
		return prog
	}
	if env["KITTY_WINDOW_ID"] != "" {
		if ver := env["KITTY_VERSION"]; ver != "" {
			return "kitty_" + ver
		}
		return "kitty"
	}
	if env["ALACRITTY_LOG"] != "" {
		return "alacritty"
	}
	if env["WEZTERM_PANE"] != "" {
		if ver := env["WEZTERM_VERSION"]; ver != "" {
			return "wezterm_" + ver
		}
		return "wezterm"
	}
	if term := env["TERM"]; term != "" {
		return term
	}
	return "unknown"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestIdentifyTerminal -v`
Expected: PASS for all 9 cases

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/terminal.go internal/emoji/terminal_test.go
git commit -m "feat(emoji): add terminal identity detection for cache keying"
```

---

## Task 2: DSR response parser

**Files:**
- Create: `internal/emoji/probe.go`
- Test: `internal/emoji/probe_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/probe_test.go`:

```go
package emoji

import (
	"testing"
)

func TestParseDSRResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantCol int
		wantErr bool
	}{
		{"valid response", []byte("\x1b[1;3R"), 3, false},
		{"valid with extra row digits", []byte("\x1b[42;5R"), 5, false},
		{"valid col=1", []byte("\x1b[1;1R"), 1, false},
		{"valid wide", []byte("\x1b[1;200R"), 200, false},
		{"empty", []byte(""), 0, true},
		{"missing terminator", []byte("\x1b[1;3"), 0, true},
		{"missing semicolon", []byte("\x1b[13R"), 0, true},
		{"missing CSI", []byte("1;3R"), 0, true},
		{"col not a number", []byte("\x1b[1;xR"), 0, true},
		{"col zero", []byte("\x1b[1;0R"), 0, true},
		{"col negative", []byte("\x1b[1;-1R"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := parseDSRResponse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDSRResponse(%q) err = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if col != tt.wantCol {
				t.Errorf("parseDSRResponse(%q) col = %d, want %d", tt.input, col, tt.wantCol)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run TestParseDSRResponse -v`
Expected: FAIL with "undefined: parseDSRResponse"

- [ ] **Step 3: Implement DSR parser**

Create `internal/emoji/probe.go`:

```go
package emoji

import (
	"errors"
	"strconv"
)

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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run TestParseDSRResponse -v`
Expected: PASS for all 11 cases

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/probe.go internal/emoji/probe_test.go
git commit -m "feat(emoji): add DSR response parser for CSI 6n cursor reports"
```

---

## Task 3: Probe a single emoji

**Files:**
- Modify: `internal/emoji/probe.go`
- Modify: `internal/emoji/probe_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/probe_test.go`:

```go
import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeTerminal simulates a terminal that responds to CSI 6n queries
// with a configurable column number.
type fakeTerminal struct {
	out      *bytes.Buffer        // stdin from probe's perspective (we read from this)
	in       *bytes.Buffer        // stdout from probe's perspective (we write to this)
	respCol  map[string]int       // emoji string → column to report after rendering
	current  string               // current emoji being measured
	timeout  bool                 // if true, never respond to DSR
}

func newFakeTerminal(widths map[string]int) *fakeTerminal {
	return &fakeTerminal{
		out:     &bytes.Buffer{},
		in:      &bytes.Buffer{},
		respCol: widths,
	}
}

// Write captures probe output. When CSI 6n is seen, queue a response.
func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.in.Write(p)
	s := string(p)
	// Detect emoji by capturing what's written between \r and CSI 6n
	if strings.Contains(s, "\x1b[6n") {
		// Find the most recent \r in our buffer; everything between it and \x1b[6n is the emoji
		buf := f.in.String()
		lastCR := strings.LastIndex(buf[:strings.LastIndex(buf, "\x1b[6n")], "\r")
		if lastCR >= 0 {
			f.current = buf[lastCR+1 : strings.LastIndex(buf, "\x1b[6n")]
		}
		if f.timeout {
			return len(p), nil
		}
		col, ok := f.respCol[f.current]
		if !ok {
			col = 1 // unknown emoji → no advance
		}
		// Column = 1 + width (we start at column 1, render emoji, cursor is at 1+width)
		fmt.Fprintf(f.out, "\x1b[1;%dR", 1+col)
	}
	return len(p), nil
}

func (f *fakeTerminal) Read(p []byte) (int, error) {
	if f.timeout {
		// Block forever (or until test timeout)
		time.Sleep(500 * time.Millisecond)
		return 0, io.EOF
	}
	return f.out.Read(p)
}

func TestProbeOne(t *testing.T) {
	widths := map[string]int{
		"a":  1,
		"中": 2,
		"👍": 2,
		"❤": 1,
	}
	ft := newFakeTerminal(widths)

	for emoji, want := range widths {
		got, err := probeOne(ft, ft, emoji, 200*time.Millisecond)
		if err != nil {
			t.Errorf("probeOne(%q) error: %v", emoji, err)
			continue
		}
		if got != want {
			t.Errorf("probeOne(%q) = %d, want %d", emoji, got, want)
		}
	}
}

func TestProbeOneTimeout(t *testing.T) {
	ft := newFakeTerminal(nil)
	ft.timeout = true

	_, err := probeOne(ft, ft, "👍", 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, errProbeTimeout) {
		t.Errorf("expected errProbeTimeout, got %v", err)
	}
}
```

Add the missing `fmt` import to the test file as needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run "TestProbeOne" -v`
Expected: FAIL with "undefined: probeOne" and "undefined: errProbeTimeout"

- [ ] **Step 3: Implement single-emoji probe**

Append to `internal/emoji/probe.go`:

```go
import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

var errProbeTimeout = errors.New("probe timed out waiting for DSR response")

// probeOne renders a single emoji and queries the terminal for cursor
// position to determine the rendered width.
//
// The caller is responsible for putting the terminal in raw mode.
//
// Procedure:
//   1. Write \r to move cursor to column 1
//   2. Write the emoji
//   3. Write CSI 6n (DSR query)
//   4. Read response with a deadline of timeout
//   5. Parse column from response
//   6. Width = column - 1 (cursor was at column 1 before emoji)
//   7. Write \r\x1b[K to clear the line for the next probe
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run "TestProbeOne" -v -timeout 5s`
Expected: PASS for both `TestProbeOne` and `TestProbeOneTimeout`

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/probe.go internal/emoji/probe_test.go
git commit -m "feat(emoji): probe single emoji width via CSI 6n"
```

---

## Task 4: Cache file I/O

**Files:**
- Create: `internal/emoji/cache.go`
- Test: `internal/emoji/cache_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/cache_test.go`:

```go
package emoji

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "emoji-widths-test.json")

	original := &Cache{
		Version:      1,
		Terminal:     "ghostty_1.0.0",
		ProbedAt:     "2026-04-27T16:00:00Z",
		CodemapHash:  "abc123",
		Widths: map[string]int{
			"❤️": 1,
			"👍": 2,
			"🕵🏻‍♂️": 2,
		},
	}

	if err := SaveCache(path, original); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	loaded, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", loaded.Version, original.Version)
	}
	if loaded.Terminal != original.Terminal {
		t.Errorf("Terminal: got %q, want %q", loaded.Terminal, original.Terminal)
	}
	if loaded.CodemapHash != original.CodemapHash {
		t.Errorf("CodemapHash: got %q, want %q", loaded.CodemapHash, original.CodemapHash)
	}
	if len(loaded.Widths) != len(original.Widths) {
		t.Errorf("Widths length: got %d, want %d", len(loaded.Widths), len(original.Widths))
	}
	for k, v := range original.Widths {
		if loaded.Widths[k] != v {
			t.Errorf("Widths[%q]: got %d, want %d", k, loaded.Widths[k], v)
		}
	}
}

func TestLoadCacheMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadCache(filepath.Join(dir, "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got %v", err)
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadCache(path)
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
}

func TestCachePath(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/test-cache")
	got := CachePath("ghostty_1.0.0")
	want := "/tmp/test-cache/slk/emoji-widths-ghostty_1.0.0.json"
	if got != want {
		t.Errorf("CachePath: got %q, want %q", got, want)
	}
}

func TestCachePathFallback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/test")
	got := CachePath("kitty")
	want := "/home/test/.cache/slk/emoji-widths-kitty.json"
	if got != want {
		t.Errorf("CachePath fallback: got %q, want %q", got, want)
	}
}

func TestCodemapHashStable(t *testing.T) {
	// Same input must produce same hash.
	m1 := map[string]string{":a:": "A", ":b:": "B"}
	m2 := map[string]string{":b:": "B", ":a:": "A"} // different iteration order
	h1 := codemapHash(m1)
	h2 := codemapHash(m2)
	if h1 != h2 {
		t.Errorf("codemapHash not stable across map iteration: %q vs %q", h1, h2)
	}

	// Different input must produce different hash.
	m3 := map[string]string{":a:": "A", ":c:": "C"}
	h3 := codemapHash(m3)
	if h1 == h3 {
		t.Errorf("codemapHash collision: %q", h1)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run "TestCache|TestCodemapHash" -v`
Expected: FAIL with "undefined: Cache, SaveCache, LoadCache, CachePath, codemapHash"

- [ ] **Step 3: Implement cache I/O**

Create `internal/emoji/cache.go`:

```go
package emoji

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// CacheVersion is the on-disk schema version. Bump when format changes.
const CacheVersion = 1

// Cache is the on-disk format for probed emoji widths.
type Cache struct {
	Version     int            `json:"version"`
	Terminal    string         `json:"terminal"`
	ProbedAt    string         `json:"probed_at"`
	CodemapHash string         `json:"codemap_hash"`
	Widths      map[string]int `json:"widths"`
}

// CachePath returns the absolute path to the cache file for the given
// terminal key. Honors XDG_CACHE_HOME, falls back to ~/.cache.
func CachePath(terminalKey string) string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home := os.Getenv("HOME")
		cacheHome = filepath.Join(home, ".cache")
	}
	return filepath.Join(cacheHome, "slk", "emoji-widths-"+terminalKey+".json")
}

// SaveCache writes the cache as JSON, creating directories as needed.
func SaveCache(path string, c *Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadCache reads a cache file. Returns os.IsNotExist error if absent.
func LoadCache(path string) (*Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// codemapHash produces a stable hex hash of a name→unicode emoji map.
// The hash is order-independent so it's stable across Go map iteration.
func codemapHash(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(m[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run "TestCache|TestCodemapHash" -v`
Expected: PASS for all cache tests

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/cache.go internal/emoji/cache_test.go
git commit -m "feat(emoji): JSON cache for probed widths with stable codemap hashing"
```

---

## Task 5: Width function with cache lookup and fallback

**Files:**
- Create: `internal/emoji/width.go`
- Test: `internal/emoji/width_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/width_test.go`:

```go
package emoji

import (
	"testing"
)

func TestWidthASCIIBypass(t *testing.T) {
	resetWidthMap()
	// Even with empty cache, ASCII should work via lipgloss fallback
	if got := Width("hello"); got != 5 {
		t.Errorf("Width(\"hello\") = %d, want 5", got)
	}
	if got := Width(""); got != 0 {
		t.Errorf("Width(\"\") = %d, want 0", got)
	}
	if got := Width("a"); got != 1 {
		t.Errorf("Width(\"a\") = %d, want 1", got)
	}
}

func TestWidthCacheHit(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"❤️": 1,
		"👍":  2,
	})

	if got := Width("❤️"); got != 1 {
		t.Errorf("Width(❤️) = %d, want 1 (cache hit)", got)
	}
	if got := Width("👍"); got != 2 {
		t.Errorf("Width(👍) = %d, want 2 (cache hit)", got)
	}
}

func TestWidthCacheMissFallback(t *testing.T) {
	resetWidthMap()
	// Empty cache; emoji not present → fall back to lipgloss
	got := Width("👍")
	if got < 1 || got > 2 {
		t.Errorf("Width(👍) fallback = %d, want 1 or 2", got)
	}
}

func TestWidthMixedContent(t *testing.T) {
	resetWidthMap()
	setWidthMap(map[string]int{
		"❤️": 1,
	})

	// "abc❤️def" → 3 + 1 + 3 = 7
	if got := Width("abc❤️def"); got != 7 {
		t.Errorf("Width(\"abc❤️def\") = %d, want 7", got)
	}
}

func TestIsCalibrated(t *testing.T) {
	resetWidthMap()
	if IsCalibrated() {
		t.Error("IsCalibrated should be false with empty map")
	}

	setWidthMap(map[string]int{"👍": 2})
	if !IsCalibrated() {
		t.Error("IsCalibrated should be true after setWidthMap")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run "TestWidth|TestIsCalibrated" -v`
Expected: FAIL with "undefined: Width, IsCalibrated, resetWidthMap, setWidthMap"

- [ ] **Step 3: Implement Width function**

Create `internal/emoji/width.go`:

```go
package emoji

import (
	"sync"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

var (
	widthMu  sync.RWMutex
	widthMap map[string]int // emoji grapheme cluster → width
)

// Width returns the rendered cell width of s.
//
// For grapheme clusters present in the probed cache, returns the cached
// width. For pure ASCII or content with no emoji, delegates directly to
// lipgloss.Width(). For mixed content, segments by grapheme cluster and
// sums per-cluster widths (cache hit or lipgloss fallback per cluster).
func Width(s string) int {
	if !containsNonASCII(s) {
		return lipgloss.Width(s)
	}

	widthMu.RLock()
	cached := widthMap
	widthMu.RUnlock()

	if len(cached) == 0 {
		return lipgloss.Width(s)
	}

	// Segment by grapheme cluster, look up each.
	total := 0
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		cluster := gr.Str()
		if w, ok := cached[cluster]; ok {
			total += w
		} else {
			total += lipgloss.Width(cluster)
		}
	}
	return total
}

// IsCalibrated reports whether the probe succeeded and we have a
// terminal-specific width map loaded.
func IsCalibrated() bool {
	widthMu.RLock()
	defer widthMu.RUnlock()
	return len(widthMap) > 0
}

// setWidthMap installs a new width map. Used by Init() and tests.
func setWidthMap(m map[string]int) {
	widthMu.Lock()
	defer widthMu.Unlock()
	widthMap = m
}

// resetWidthMap clears the width map. Used by tests.
func resetWidthMap() {
	widthMu.Lock()
	defer widthMu.Unlock()
	widthMap = nil
}

// containsNonASCII returns true if s has any byte ≥ 0x80.
func containsNonASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return true
		}
	}
	return false
}

// Suppress unused import warning when test file doesn't reference utf8.
var _ = utf8.RuneError
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run "TestWidth|TestIsCalibrated" -v`
Expected: PASS for all 5 tests

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/width.go internal/emoji/width_test.go
git commit -m "feat(emoji): Width() function with cache lookup and lipgloss fallback"
```

---

## Task 6: Probe loop over the kyokomi codemap

**Files:**
- Modify: `internal/emoji/probe.go`
- Modify: `internal/emoji/probe_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/emoji/probe_test.go`:

```go
func TestProbeAll(t *testing.T) {
	codemap := map[string]string{
		":a:":  "a",
		":cn:": "中",
		":up:": "👍",
	}
	widths := map[string]int{
		"a":  1,
		"中": 2,
		"👍": 2,
	}
	ft := newFakeTerminal(widths)

	result, err := probeAll(ft, ft, codemap, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("probeAll: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}
	for _, emoji := range []string{"a", "中", "👍"} {
		if got, ok := result[emoji]; !ok {
			t.Errorf("missing entry for %q", emoji)
		} else if got != widths[emoji] {
			t.Errorf("Width(%q) = %d, want %d", emoji, got, widths[emoji])
		}
	}
}

func TestProbeAllSkipsTimeouts(t *testing.T) {
	codemap := map[string]string{
		":a:":  "a",
		":cn:": "中",
	}
	// "a" succeeds, "中" times out
	ft := newFakeTerminal(map[string]int{"a": 1})
	// Custom: middle of probe, set timeout=true after first response
	// For simplicity: this test just verifies missing entries are skipped, not timeout reaction
	result, _ := probeAll(ft, ft, codemap, 100*time.Millisecond)
	// "a" should be present; "中" may or may not be depending on fake's response for unknown
	if _, ok := result["a"]; !ok {
		t.Error("expected 'a' in result")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run "TestProbeAll" -v -timeout 10s`
Expected: FAIL with "undefined: probeAll"

- [ ] **Step 3: Implement probe loop**

Append to `internal/emoji/probe.go`:

```go
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
	result := make(map[string]int, len(codemap))
	seen := make(map[string]bool)

	// Sanity check: probe a known-1-wide ASCII char first.
	w, err := probeOne(out, in, "a", perProbeTimeout)
	if err != nil {
		return nil, errors.New("sanity probe failed: " + err.Error())
	}
	if w != 1 {
		return nil, errors.New("sanity probe returned width " + strconv.Itoa(w) + " for 'a'; terminal does not support DSR correctly")
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

		width, err := probeOne(out, in, emoji, perProbeTimeout)
		if err != nil {
			// Skip this emoji; it'll fall back to lipgloss.Width.
			continue
		}
		result[emoji] = width
	}
	return result, nil
}
```

Add `"strconv"` to the imports if not present.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run "TestProbeAll" -v -timeout 10s`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/probe.go internal/emoji/probe_test.go
git commit -m "feat(emoji): probe entire codemap with per-emoji error tolerance"
```

---

## Task 7: Init orchestrator

**Files:**
- Create: `internal/emoji/init.go`
- Test: `internal/emoji/init_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/emoji/init_test.go`:

```go
package emoji

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitWithValidCache(t *testing.T) {
	resetWidthMap()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("TERM_PROGRAM", "test_terminal")
	t.Setenv("TERM_PROGRAM_VERSION", "1.0")

	// Pre-write a valid cache
	codemap := map[string]string{":a:": "a "}
	cachePath := CachePath("test_terminal_1.0")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	c := &Cache{
		Version:     CacheVersion,
		Terminal:    "test_terminal_1.0",
		ProbedAt:    "2026-04-27T16:00:00Z",
		CodemapHash: codemapHash(codemap),
		Widths:      map[string]int{"a": 1, "👍": 2},
	}
	if err := SaveCache(cachePath, c); err != nil {
		t.Fatal(err)
	}

	// Init with cache hit
	opts := InitOptions{
		Codemap:           codemap,
		PerProbeTimeout:   100,
		ProgressFunc:      nil,
		SkipProbe:         false,
		ForceProbe:        false,
	}
	loaded, probed, err := initWithIO(opts, nil, nil)
	if err != nil {
		t.Fatalf("initWithIO: %v", err)
	}
	if !loaded {
		t.Error("expected cache to be loaded")
	}
	if probed {
		t.Error("expected probe to be skipped (cache hit)")
	}
	if !IsCalibrated() {
		t.Error("expected IsCalibrated to be true")
	}
}

func TestInitWithStaleCache(t *testing.T) {
	resetWidthMap()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("TERM_PROGRAM", "test_terminal")
	t.Setenv("TERM_PROGRAM_VERSION", "1.0")

	// Write cache with WRONG codemap hash
	cachePath := CachePath("test_terminal_1.0")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	c := &Cache{
		Version:     CacheVersion,
		Terminal:    "test_terminal_1.0",
		ProbedAt:    "2026-04-27T16:00:00Z",
		CodemapHash: "stale-hash",
		Widths:      map[string]int{"a": 1},
	}
	if err := SaveCache(cachePath, c); err != nil {
		t.Fatal(err)
	}

	// Init with stale hash → should detect mismatch and not use cache.
	// Without a fake terminal, probe will fail; we expect SkipProbe behavior.
	opts := InitOptions{
		Codemap:         map[string]string{":a:": "a "},
		PerProbeTimeout: 100,
		SkipProbe:       true, // skip probe; just verify cache was rejected
	}
	loaded, probed, _ := initWithIO(opts, nil, nil)
	if loaded {
		t.Error("expected stale cache to be rejected")
	}
	if probed {
		t.Error("expected probe to be skipped (SkipProbe=true)")
	}
}

func TestInitSkipProbe(t *testing.T) {
	resetWidthMap()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)

	opts := InitOptions{
		Codemap:         map[string]string{":a:": "a "},
		PerProbeTimeout: 100,
		SkipProbe:       true,
	}
	loaded, probed, err := initWithIO(opts, nil, nil)
	if err != nil {
		t.Fatalf("expected no error with SkipProbe, got %v", err)
	}
	if loaded || probed {
		t.Error("expected neither loaded nor probed with SkipProbe and no cache")
	}
	if IsCalibrated() {
		t.Error("expected IsCalibrated to be false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/emoji/ -run "TestInit" -v`
Expected: FAIL with "undefined: InitOptions, initWithIO"

- [ ] **Step 3: Implement Init**

Create `internal/emoji/init.go`:

```go
package emoji

import (
	"errors"
	"io"
	"os"
	"time"

	emojilib "github.com/kyokomi/emoji/v2"
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

	// Write cache (best effort; failure is logged but not fatal).
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/emoji/ -run "TestInit" -v`
Expected: PASS for all 3 tests

- [ ] **Step 5: Commit**

```bash
git add internal/emoji/init.go internal/emoji/init_test.go
git commit -m "feat(emoji): Init() orchestrator for cache load and probe"
```

---

## Task 8: Raw mode wrapper for probe

**Files:**
- Modify: `internal/emoji/init.go`

The probe needs the terminal in raw mode (no echo, no line buffering) so DSR responses don't get mangled. Add a wrapper.

- [ ] **Step 1: Read existing imports**

Open `internal/emoji/init.go` to see current imports.

- [ ] **Step 2: Add raw-mode wrapper**

Modify `internal/emoji/init.go`. Add to imports:

```go
import (
	"errors"
	"io"
	"os"
	"time"

	emojilib "github.com/kyokomi/emoji/v2"
	"golang.org/x/term"
)
```

Replace the body of `Init` with:

```go
func Init(opts InitOptions) error {
	// If we'll need to probe, put the terminal in raw mode for the duration.
	// Determine that by checking cache first.
	var rawState *term.State
	if !opts.SkipProbe {
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			st, err := term.MakeRaw(fd)
			if err == nil {
				rawState = st
				defer term.Restore(fd, rawState)
			}
		}
	}

	_, _, err := initWithIO(opts, os.Stdout, os.Stdin)
	return err
}
```

- [ ] **Step 3: Add the dependency**

Run: `go get golang.org/x/term && go mod tidy`

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Run all emoji tests**

Run: `go test ./internal/emoji/ -count=1 -v -timeout 30s`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add internal/emoji/init.go go.mod go.sum
git commit -m "feat(emoji): wrap probe in terminal raw mode"
```

---

## Task 9: Wire Init into main.go with progress message and flags

**Files:**
- Modify: `cmd/slk/main.go`

- [ ] **Step 1: Find the main function**

Open `cmd/slk/main.go` and locate `func main()` (around line 56).

- [ ] **Step 2: Add the import**

In the import block (lines 13-30), add:

```go
	emojiwidth "github.com/gammons/slk/internal/emoji"
```

- [ ] **Step 3: Add flag handling and Init call**

Immediately after the `--add-workspace` early-return block (around line 65), add:

```go
	// Emoji width probing: parse flags and call Init before bubbletea starts.
	skipProbe := false
	forceProbe := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--no-emoji-probe":
			skipProbe = true
		case "--probe-emoji":
			forceProbe = true
		}
	}

	probedNow := false
	probeStart := time.Now()
	if !skipProbe {
		// Check whether we'll need to probe (so we can print the message first).
		// We do a dry-run cache check by looking for the cache file.
		terminalKey := emojiwidth.IdentifyTerminal()
		cachePath := emojiwidth.CachePath(terminalKey)
		if _, err := os.Stat(cachePath); err != nil || forceProbe {
			fmt.Fprintln(os.Stderr, "Calibrating emoji widths for your terminal (one-time, ~1 second)...")
			probedNow = true
		}
	}

	if err := emojiwidth.Init(emojiwidth.InitOptions{
		SkipProbe:  skipProbe,
		ForceProbe: forceProbe,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: emoji width calibration failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "Falling back to library defaults; some emoji may render with incorrect width.")
	}

	if probedNow && emojiwidth.IsCalibrated() {
		fmt.Fprintf(os.Stderr, "Done in %dms.\n", time.Since(probeStart).Milliseconds())
	}

	if forceProbe {
		// --probe-emoji is a diagnostic flag: probe and exit.
		fmt.Fprintln(os.Stderr, "Probe complete. Exiting.")
		os.Exit(0)
	}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 5: Verify --no-emoji-probe works**

Run: `./bin/slk --no-emoji-probe --help 2>&1 | head -3` (or equivalent dry run)
Expected: no probe runs, no calibration message printed

- [ ] **Step 6: Commit**

```bash
git add cmd/slk/main.go
git commit -m "feat(slk): integrate emoji width probing on startup"
```

---

## Task 10: Replace lipgloss.Width call sites

**Files:**
- Modify: `internal/ui/messages/model.go`
- Modify: `internal/ui/thread/model.go`

- [ ] **Step 1: Update messages/model.go**

Open `internal/ui/messages/model.go`. The import for `emojiutil` already exists (added in previous work).

At line 585, change:

```go
			if lipgloss.Width(candidate) > contentWidth && currentLine != "" {
```

To:

```go
			if emojiutil.Width(candidate) > contentWidth && currentLine != "" {
```

- [ ] **Step 2: Update thread/model.go**

Open `internal/ui/thread/model.go` and find the equivalent call (search for `lipgloss.Width(candidate)`). Change it the same way.

Run: `grep -n "lipgloss.Width(candidate)" internal/ui/thread/model.go`
Expected: one match. Replace it with `emojiutil.Width(candidate)`.

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: clean build

- [ ] **Step 4: Verify tests still pass**

Run: `go test ./... -count=1`
Expected: all tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/thread/model.go
git commit -m "feat(ui): use probed emoji widths for reaction pill wrapping"
```

---

## Task 11: Remove the heuristic VS16 stripping

**Files:**
- Delete: `internal/emoji/normalize.go`
- Delete: `internal/emoji/normalize_test.go`
- Modify: `internal/ui/messages/model.go`
- Modify: `internal/ui/thread/model.go`
- Modify: `internal/ui/messages/render.go`

The probe gives us ground truth, so the normalization heuristics become unnecessary. Remove them — but keep the call sites readable by replacing them with plain `emoji.Sprint(...)`.

- [ ] **Step 1: Update messages/model.go**

Open `internal/ui/messages/model.go`. Find:

```go
			emojiStr := emojiutil.NormalizeEmojiPresentation(emoji.Sprint(":" + r.Emoji + ":"))
```

Replace with:

```go
			emojiStr := emoji.Sprint(":" + r.Emoji + ":")
```

- [ ] **Step 2: Update thread/model.go**

Open `internal/ui/thread/model.go`. Find the same line and apply the same replacement.

- [ ] **Step 3: Update render.go**

Open `internal/ui/messages/render.go`. Find:

```go
	// Strip VS16 from text-default characters so width measurement
	// matches terminal rendering (many terminals render these as 1-wide
	// regardless of VS16).
	text = emojiutil.StripTextDefaultVS16(emoji.Sprint(text))
```

Replace with:

```go
	text = emoji.Sprint(text)
```

If `emojiutil` is no longer used in this file, remove its import.

- [ ] **Step 4: Delete the old normalize files**

Run:
```bash
rm internal/emoji/normalize.go internal/emoji/normalize_test.go
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./...`
Expected: clean build. If there are unused-import errors, remove the dead `emojiutil` imports from any files that no longer use it.

- [ ] **Step 6: Verify all tests pass**

Run: `go test ./... -count=1`
Expected: all tests pass (the deleted normalize tests are gone too)

- [ ] **Step 7: Commit**

```bash
git add -A internal/emoji internal/ui/messages internal/ui/thread
git commit -m "refactor(emoji): remove VS16 heuristic normalization (replaced by probed widths)"
```

---

## Task 12: Manual end-to-end verification

**No new files. Verification only.**

- [ ] **Step 1: Build the binary**

Run: `make build`
Expected: clean build, fresh binary at `bin/slk`

- [ ] **Step 2: Delete any existing cache to force a fresh probe**

Run:
```bash
rm -f ~/.cache/slk/emoji-widths-*.json
```

- [ ] **Step 3: Launch slk and observe probe**

Run: `./bin/slk`

Expected output before TUI loads:
```
Calibrating emoji widths for your terminal (one-time, ~1 second)...
Done in NNNms.
```

Then the TUI starts normally.

- [ ] **Step 4: Verify cache was written**

Run: `ls -la ~/.cache/slk/emoji-widths-*.json`
Expected: one file matching your terminal identity, ~50-200KB.

- [ ] **Step 5: Inspect a few widths**

Run: `cat ~/.cache/slk/emoji-widths-*.json | python3 -c "import json,sys; d=json.load(sys.stdin); print('total:', len(d['widths'])); print('heart_VS16:', d['widths'].get('❤️')); print('thumbsup:', d['widths'].get('👍'))"`

Expected: ~3000 entries; heart and thumbsup show plausible widths for your terminal.

- [ ] **Step 6: Re-run slk and verify cache is used**

Run: `./bin/slk`
Expected: NO calibration message (cache hit). TUI starts immediately.

- [ ] **Step 7: Test --probe-emoji**

Run: `./bin/slk --probe-emoji`
Expected: probe runs, message printed, exits cleanly without starting TUI.

- [ ] **Step 8: Test --no-emoji-probe**

Run:
```bash
rm -f ~/.cache/slk/emoji-widths-*.json
./bin/slk --no-emoji-probe
```
Expected: no probe runs, no calibration message, TUI starts. Width measurement falls back to lipgloss (current behavior).

- [ ] **Step 9: Visual verification**

Open a Slack channel with diverse reactions (emoji of various kinds). Verify panel borders are aligned correctly. The pill borders should match the panel border. No left or right shifts.

- [ ] **Step 10: Commit verification notes if needed**

If you find any regressions, file them as a separate issue. The plan ends here.
