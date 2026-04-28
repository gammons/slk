# Terminal Emoji Width Probing — Design

## Problem

No width-measurement library can reliably predict how a terminal will render any given emoji. Different terminals (kitty, alacritty, iTerm2, WezTerm, ghostty, etc.) have their own emoji width tables that drift over time and disagree with each other. Heuristics based on Unicode properties — Extended_Pictographic, Emoji_Presentation, VS16, skin-tone modifiers — only approximate terminal behavior.

In the slack-tui app, when a single emoji's measured width disagrees with its rendered width, the panel border alignment breaks for that entire line. The current implementation (strip VS16 from text-default Extended_Pictographic) gets close but still produces visible regressions for some emoji, and we have no closed-loop way to fix new mismatches without screenshot feedback.

## Solution

**Ask the terminal directly.** Use the standard `CSI 6n` (Device Status Report) escape sequence to query the cursor position after rendering each emoji. The response tells us exactly how many cells the terminal advanced — that is the rendered width.

Run this probe once on first launch, cache the results to disk keyed by terminal identity, and use the cached widths for all width measurement going forward. After cache load, the cost is a map lookup per emoji.

## Architecture

### Components

A new package `internal/emoji` (extending the existing one) with:

- **`Probe`** — orchestrates the probe: detects terminal, iterates the kyokomi codemap, captures widths via DSR
- **`Cache`** — JSON file at `$XDG_CACHE_HOME/slk/emoji-widths-<terminalkey>.json`
- **`WidthMap`** — in-memory map from emoji string → width, populated from cache
- **`Width(s)`** — public function: looks up emoji width from map, falls back to `lipgloss.Width()` for non-emoji content
- **`terminal.Identify()`** — returns a stable cache key from environment

### Lifecycle

1. `cmd/slk/main.go` calls `emoji.Init()` before bubbletea starts
2. `Init` checks for cached file matching the terminal key
3. Cache hit → load into `WidthMap`, return
4. Cache miss → print "Calibrating emoji widths for your terminal (one-time, ~1 second)..." → run probe → write cache → return
5. Probe failure → log warning, use lipgloss fallback for all measurement (current behavior)

## Probe Mechanism

The probe uses Device Status Report (`CSI 6n`):

1. Put terminal in raw mode (no echo, no line buffering)
2. Send `\r` to move cursor to column 1
3. Print the emoji (no newline, no styling, no surrounding text)
4. Send `\x1b[6n`
5. Read response from stdin: terminal replies with `\x1b[<row>;<col>R`
6. Width = `col - 1`
7. Send `\r` and clear line for the next probe

For 3,092 emoji, the serial loop runs in ~500ms-1s. We do not parallelize because cursor position is global state.

### Edge cases

- DSR response timeout (200ms): mark emoji as "use lipgloss fallback"
- Invalid response format: same fallback
- Initial sanity check fails (probe a known-width ASCII character first): abort probe entirely, log warning, use lipgloss for everything
- Width > 4 or < 0: treat as invalid, fallback

### Why this works reliably

`CSI 6n` is supported by every terminal that supports cursor positioning. It is the same mechanism `tput`, `bash` `\[\]`, and dozens of TUI libraries use for capability detection. There is no terminal that supports rich emoji rendering but not DSR.

## Cache Format and Identity

### Cache file location

`$XDG_CACHE_HOME/slk/emoji-widths-<key>.json`, falling back to `~/.cache/slk/...`.

### Terminal identity key

A stable string built from environment, in order of preference:

1. `$TERM_PROGRAM` + `_` + `$TERM_PROGRAM_VERSION` (set by iTerm2, Apple Terminal, vscode, ghostty, kitty, etc.)
2. `$KITTY_WINDOW_ID` present → `kitty_$KITTY_VERSION`
3. `$ALACRITTY_LOG` present → `alacritty`
4. `$WEZTERM_PANE` present → `wezterm_$WEZTERM_VERSION`
5. Fallback: `$TERM` value (e.g., `xterm-256color`)

Examples: `ghostty_1.0.0.json`, `iTerm.app_3.5.0.json`, `kitty_0.32.0.json`

### File format

```json
{
  "version": 1,
  "terminal": "ghostty_1.0.0",
  "probed_at": "2026-04-27T16:00:00Z",
  "codemap_hash": "a3b2...",
  "widths": {
    "❤️": 1,
    "❤": 1,
    "👍": 2,
    "🕵🏻‍♂️": 2,
    "1️⃣": 2
  }
}
```

### Re-probe triggers

- File missing
- `version` mismatch (schema bump)
- `codemap_hash` mismatch — we hash the kyokomi codemap; if kyokomi is upgraded and adds new emoji, we re-probe

### Storage size

~3,092 emoji × ~30 bytes ≈ 100KB. Trivial.

## Width API and Integration

### Public API (`internal/emoji`)

```go
// Init must be called once at startup, before bubbletea begins.
// Loads cache or runs probe. Returns nil on success, error on
// fatal probe failure (in which case Width falls back to lipgloss).
func Init() error

// Width returns the rendered cell width of s. For emoji in our
// probed cache, returns the cached value. For everything else,
// delegates to lipgloss.Width().
func Width(s string) int

// IsCalibrated reports whether the probe succeeded and we have
// terminal-specific widths. False means we're using lipgloss fallback.
func IsCalibrated() bool
```

### Width routing

1. If string contains no Extended_Pictographic codepoints → `lipgloss.Width(s)` directly (fast path)
2. Otherwise, segment the string into grapheme clusters (using uniseg), look up each cluster in cache. Sum widths. Cache miss for any cluster falls back to `lipgloss.Width()` for that cluster.

### Integration points

| File | Current call | New call |
|------|-------------|----------|
| `internal/ui/messages/model.go:457` | `lipgloss.Width(candidate)` | `emoji.Width(candidate)` |
| `internal/ui/thread/model.go:463` | `lipgloss.Width(candidate)` | `emoji.Width(candidate)` |
| `cmd/slk/main.go` (early in `main()`) | — | `emoji.Init()` |

### What we remove

- `NormalizeEmojiPresentation` and `StripTextDefaultVS16` become unnecessary — we no longer need to rewrite emoji to fool the width library
- The `isTextDefaultEP` table (300+ lines) goes away
- `internal/ui/messages/render.go` revert to plain `emoji.Sprint(text)`

### What we keep

- The displaywidth PR (https://github.com/clipperhouse/displaywidth/pull/23) — it's still correct for terminals that DO render skin-tone sequences as 2-wide. The probe confirms or overrides it per-terminal.

## Failure Modes and Fallback

| Scenario | Behavior |
|----------|----------|
| First emoji DSR query times out (200ms) | Abort probe. Cache nothing. Log warning. Width falls back to `lipgloss.Width()` everywhere. |
| Some emoji time out mid-probe | Skip those emoji (no cache entry). Continue probe. Width falls back per-cluster. |
| stdin/stdout not a TTY | Skip probe. Use `lipgloss.Width()`. |
| Cache file corrupt JSON | Delete file, re-probe. |
| Cache file unreadable (permissions) | Probe but skip writing. Re-probe each launch. Log warning. |
| Cache directory creation fails | Probe in memory only. Re-probe each launch. Log warning. |
| `--no-emoji-probe` flag | Skip probe unconditionally. Use `lipgloss.Width()`. |

The key principle: probe failure must never break the app. Fallback is the current behavior, which works (imperfectly). Probing is enhancement, not dependency.

## Diagnostic Command

`slk --probe-emoji` forces a re-probe regardless of cache state. Useful after terminal upgrades or to verify the probe worked. Output: `Probed 3092 emoji in 847ms. Cached to ~/.cache/slk/emoji-widths-ghostty_1.0.0.json`.

## Testing Strategy

### Unit tests (no terminal needed)

- Cache load/save round-trip
- Cache invalidation on version mismatch, hash mismatch, missing file
- Terminal identity key generation from various env var combinations
- DSR response parsing (well-formed, malformed, partial, timeout)
- `Width()` routing logic: ASCII bypass, cache hit, cache miss fallback

### Integration tests (require a fake PTY)

- Probe against a scripted terminal that returns predetermined widths
- Probe timeout handling
- DSR-not-supported handling

### Manual verification

The existing `/tmp/emoji_width_test/terminal_check` diagnostic stays useful as a sanity check that the probed widths match what the terminal actually renders.

## Out of Scope

- **Custom Slack workspace emoji** — these render as `:name:` text (not Unicode), no probe needed
- **Animated/image-based emoji** — same as above
- **Per-row probing** — terminals don't change rendering based on row; probe once per session
- **Probe at custom intervals** — only re-probe on terminal identity change or kyokomi codemap change
- **User configuration of widths** — the probe is the source of truth; manual override is unnecessary if the probe works correctly. Users with broken probes can use `--no-emoji-probe` to fall back.
