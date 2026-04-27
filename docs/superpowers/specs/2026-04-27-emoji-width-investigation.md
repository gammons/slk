# Emoji Width Rendering Investigation

## Problem Statement

When reaction pills contain Unicode emoji characters, the right panel border (`│`) breaks on those lines. Black gaps appear between the message content and the right border. The root cause is a mismatch between how width-calculation libraries measure emoji widths and how the terminal actually renders them.

This issue is most visible on light themes (where the black gap contrasts sharply with the cream/light background) but also affects dark themes (where it's less noticeable).

## Architecture Context

The rendering pipeline for a message with reactions:

```
1. renderMessagePlain()
   → RenderSlackMarkdown() for message text (word-wrapped at contentWidth)
   → Reaction pills rendered with lipgloss styles (Padding, Background, Foreground)
   → Reaction pills joined into lines, wrapped at contentWidth

2. View() border loop
   → borderFill.Width(width-1).Render(content) — pads short lines, does NOT truncate
   → borderInvis/borderSelect.Render(filled) — adds left border character "▌"

3. app.go panel assembly
   → msgBorderStyle.Width(msgWidth).Render(msgInner) — draws panel border on all 4 sides
   → If any line in msgInner exceeds msgWidth, it pushes the right border character
     to the wrong position, creating a gap
```

The critical point: `borderFill.Width(width-1)` only PADS (adds chars to reach the target width). It does NOT truncate lines that are already wider than the target. Lipgloss's `Width()` method never truncates.

## Width Measurement Libraries Tested

### github.com/mattn/go-runewidth v0.0.23 (latest)

This is what lipgloss uses internally for all width calculations. We cannot change lipgloss's dependency.

**Known issues (from GitHub):**
- Issue #59: "Width is 1 when it should be 2" — multi-rune characters (combining marks, variation selectors) miscounted
- Issue #36: "Wrong width reported for some characters" — Tamil, Telugu, Hindi characters miscounted
- Issue #39: "Wrong width for flag symbols"
- Maintainer acknowledges the problem but notes terminal width is "not part of any official specification"

**Test results** (our test in `/tmp/emojicheck.go`):

| Emoji | Char | go-runewidth | Terminal actual | Correct? |
|-------|------|-------------|-----------------|----------|
| thumbsup | 👍 | 2 | 2 | ✓ |
| heart | ❤️ | 1 | 2 | ✗ (variation selector U+FE0F) |
| smile | 😊 | 2 | 2 | ✓ |
| fire | 🔥 | 2 | 2 | ✓ |
| party | 🎉 | 2 | 2 | ✓ |
| pen | 🖊 | 1 | 2 | ✗ |
| pen w/VS | 🖊️ | 1 | 2 | ✗ (variation selector) |
| clap | 👏 | 2 | 2 | ✓ |
| star | ⭐ | 2 | 2 | ✓ |
| check | ✅ | 2 | 2 | ✓ |
| rocket | 🚀 | 2 | 2 | ✓ |
| shamrock | ☘️ | 1 | 2 | ✗ (variation selector) |
| clipboard | 📎 | 2 | 2 | ✓ |
| book | 📖 | 2 | 2 | ✓ |

**Summary:** Most common emoji are correct. Failures are primarily characters with Unicode Variation Selector 16 (U+FE0F) which signals "render as emoji" (2 cells) vs text presentation (1 cell). go-runewidth only looks at the first rune's width, ignoring VS16.

### github.com/rivo/uniseg (via lipgloss's indirect dependency)

More modern Unicode library with grapheme cluster awareness.

**Test results:**

| Emoji | go-runewidth | uniseg | Correct? |
|-------|-------------|--------|----------|
| heart ❤️ | 1 | 2 | ✓ uniseg correct |
| shamrock ☘️ | 1 | 2 | ✓ uniseg correct |
| pen w/VS 🖊️ | 1 | 2 | ✓ uniseg correct |
| pen 🖊 (no VS) | 1 | 1 | Both wrong (terminal renders as 2) |

**Summary:** uniseg handles variation selector emoji correctly. Still fails for some characters where the terminal renders 2-wide but no VS16 is present (terminal-dependent behavior).

### charm.land/lipgloss/v2

Lipgloss's `Width()` function uses go-runewidth internally but with some additional ANSI stripping. It matches or slightly improves on go-runewidth's results (e.g., ❤️ returns 2 from lipgloss but 1 from raw go-runewidth).

## Approaches Tried

### 1. Reaction pill wrapping with lipgloss.Width (first attempt)

**Approach:** Wrap reaction pills to new lines when `lipgloss.Width(accumulatedLine) > contentWidth`.

**Result:** Wrapping occurred but lines still overflowed because `lipgloss.Width` undercounted emoji, allowing too many pills per line.

### 2. Safety margin per pill

**Approach:** Subtract 1 extra column per pill on the line as a dynamic safety margin: `safeWidth = contentWidth - pillsOnLine - 1`.

**Result:** Somewhat helped but was too aggressive on lines without emoji and not aggressive enough on lines with many emoji. Arbitrary heuristic.

### 3. MaxWidth on entire reaction block

**Approach:** Wrap reaction content in `lipgloss.NewStyle().MaxWidth(contentWidth).Render(reactionContent)`.

**Result:** Hard-clipped mid-pill, creating dark rectangles and broken rendering. MaxWidth truncates at the byte/rune level without regard for pill boundaries.

### 4. Per-line truncation with truncate.String

**Approach:** After wrapping, truncate each reaction line to contentWidth using `truncate.String()` from muesli/reflow.

**Result:** Same issue as MaxWidth — truncates mid-pill. Also, `lipgloss.Width()` undercounts the very lines that overflow, so the truncation check (`lipgloss.Width(rl) > contentWidth`) never triggers for the problematic lines.

### 5. MaxWidth on borderFill

**Approach:** Add `MaxWidth(width-1)` to the borderFill style that wraps each message entry before the left border is applied.

**Result:** Truncated normal message text that was actually the correct width. `lipgloss.Width()` occasionally overcounts ANSI-rich text, triggering false truncation.

### 6. ClampLineWidths — per-line width enforcement

**Approach:** Process every line of message content: truncate wide lines with `lipgloss.NewStyle().Width(target).MaxWidth(target).Render(line)`, pad narrow lines with background-colored spaces.

**Result:** Catastrophic regression. The lipgloss re-render added ANSI codes that produced black rectangles on normal text. Width measurement was wrong in BOTH directions: overcounting ANSI-rich text (truncating good lines) and undercounting emoji (missing overflow lines).

### 7. uniseg.StringWidth for reaction wrapping (current solution)

**Approach:** Strip ANSI codes with `charmbracelet/x/ansi.Strip()`, then measure with `uniseg.StringWidth()` for reaction pill wrapping decisions.

**Result:** Significant improvement. Reaction lines wrap more accurately because uniseg handles variation selector emoji correctly. Some edge cases remain where uniseg also miscounts (terminal-dependent rendering).

## Current State

**What works:**
- Reaction pills wrap at approximately the correct width using uniseg
- Most emoji-containing lines stay within the panel border
- Light theme rendering is functional

**What doesn't fully work:**
- Lines with emoji that neither go-runewidth nor uniseg measure correctly (e.g., 🖊 without variation selector) still overflow by 1-2 cells
- lipgloss uses go-runewidth internally for border rendering — we cannot change this
- The `borderFill.Width(w).Render(content)` call pads but never truncates, so overflow propagates to the panel border

## Root Cause

The fundamental issue is a three-way mismatch:

1. **Terminal rendering:** Each terminal (kitty, alacritty, iTerm2, etc.) has its own emoji width table, often based on Unicode's East Asian Width property + emoji presentation rules
2. **go-runewidth:** Uses the East Asian Width property but doesn't correctly handle Variation Selector 16 (U+FE0F) or multi-rune grapheme clusters
3. **lipgloss:** Uses go-runewidth internally. All Width/MaxWidth/Place operations are based on these measurements. There is no way to override the width calculation.

This is an ecosystem-wide problem affecting all Go terminal applications that render emoji. The Charm team is aware (go-runewidth is a transitive dependency of lipgloss, bubbletea, and bubbles).

## Potential Future Fixes

### Upstream fixes
- **go-runewidth:** Add VS16 handling. Issue #59 has been open since Feb 2022 with no resolution.
- **lipgloss:** Allow pluggable width measurement (use uniseg instead of go-runewidth). No issue filed for this yet.

### Application-level workarounds
- **Don't render Unicode emoji in pills:** Show `:thumbsup:5` instead of `👍5`. Avoids width issues entirely but looks worse.
- **Use a custom lipgloss fork:** Patch lipgloss to use uniseg. Maintenance burden.
- **Post-process with terminal queries:** Some terminals support `CSI 6 n` (cursor position report) which could measure actual rendered width. Complex and async.
- **Emoji width lookup table:** Maintain a table of known-problematic emoji and add 1 to their measured width. Fragile and terminal-dependent.

## Test Script

The test script used for width measurement comparison:

```go
package main

import (
	"fmt"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
	"charm.land/lipgloss/v2"
)

func main() {
	emojis := []struct{ name, char string }{
		{"thumbsup", "👍"}, {"heart", "❤️"}, {"smile", "😊"},
		{"fire", "🔥"}, {"party", "🎉"}, {"pen", "🖊"},
		{"clap", "👏"}, {"star", "⭐"}, {"check", "✅"},
		{"rocket", "🚀"}, {"shamrock", "☘️"}, {"clipboard", "📎"},
		{"book", "📖"}, {"pen_fb", "🖊️"}, {"orange_sq", "🟧"},
	}
	fmt.Printf("%-12s %-6s %-10s %-10s %-10s\n", "Name", "Emoji", "runewidth", "uniseg", "lipgloss")
	for _, e := range emojis {
		rw := runewidth.StringWidth(e.char)
		uw := uniseg.StringWidth(e.char)
		lw := lipgloss.Width(e.char)
		fmt.Printf("%-12s %-6s %-10d %-10d %-10d\n", e.name, e.char, rw, uw, lw)
	}
}
```
