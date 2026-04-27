# Emoji Width Library Analysis — Findings

## Summary

A systematic investigation of the four width-measurement libraries in the dependency chain reveals that **go-runewidth is the only library with significant VS16 bugs**. The spec document's claim that "lipgloss uses go-runewidth internally" was true for lipgloss v1 but is incorrect for lipgloss v2 (v2.0.3), which uses `clipperhouse/displaywidth` — a library that handles VS16 correctly.

## Libraries in the Dependency Chain

### 1. `clipperhouse/displaywidth` v0.11.0 — Correct

This is the **actual** width engine behind `lipgloss.Width()` in v2. The call chain is:

```
lipgloss.Width(str)
  → ansi.StringWidth(line)                    [charmbracelet/x/ansi v0.11.7]
    → stringWidth(GraphemeWidth, s)
      → FirstGraphemeCluster(s, GraphemeWidth)
        → graphemes.FromString(s).First()      [clipperhouse/uax29 v2.7.0]
        → dwOptions.String(cluster)            [clipperhouse/displaywidth v0.11.0]
          → graphemeWidth(cluster, options)
            → lookup(s) via trie + isVS16() check
```

displaywidth handles VS16 correctly via explicit byte-level detection:

```go
// width.go lines 174-183
if prop != _Wide && sz > 0 && len(s) >= sz+3 {
    vs := s[sz : sz+3]
    if isVS16(vs) {
        prop = _Wide    // Forces width to 2
    }
}
```

It uses grapheme cluster segmentation (UAX#29 via `clipperhouse/uax29`), checks if the bytes immediately following the first rune are VS16 (`0xEF 0xB8 0x8F`), and forces width 2 when found. Comprehensive test suite including TR51 conformance tests.

### 2. `rivo/uniseg` v0.4.7 — Mostly Correct

Used by the project for reaction pill wrapping (`messages/model.go:462`, `thread/model.go:463`).

uniseg uses full grapheme cluster segmentation with explicit VS16 handling:

```go
// Inside grapheme cluster iteration loop
if firstProp == prExtendedPictographic {
    if r == vs15 {
        width = 1   // text presentation
    } else if r == vs16 {
        width = 2   // emoji presentation
    }
}
```

**Limitation:** VS16 only modifies width when the base character has `ExtendedPictographic` grapheme property. Characters like digits (`1`, `#`, `*`) are NOT `ExtendedPictographic`, so keycap sequences like `1️⃣` return width 1 instead of 2.

### 3. `mattn/go-runewidth` v0.0.23 — Has Significant Bugs

Used directly by the project in `compose/model.go:124` for line width calculation. Also available as an alternative `WcWidth` mode in the ansi package (but NOT the default path).

go-runewidth's `StringWidth` does use grapheme clustering (via `clipperhouse/uax29`) but the per-cluster width logic is fundamentally flawed:

```go
// runewidth.go lines 224-232
g := graphemes.FromString(s)
for g.Next() {
    var chWidth int
    for _, r := range g.Value() {
        chWidth = c.RuneWidth(r)
        if chWidth > 0 {
            break // Use width of first non-zero-width rune
        }
    }
    width += chWidth
}
```

The algorithm takes the width of the **first non-zero-width rune** in each grapheme cluster. VS16 (U+FE0F) is correctly classified as width 0 (combining), but there is **no logic to promote a text-default base character to width 2 when VS16 is present**. The VS16 is simply skipped because the `break` fires on the base character's width first.

For `"❤️"` (U+2764 + U+FE0F):
1. Grapheme segmenter groups them as one cluster: `[❤, FE0F]`
2. U+2764 → `RuneWidth` returns 1 (not in doublewidth table)
3. `break` — FE0F is never checked
4. Result: width 1 (WRONG — should be 2)

The library's own test suite acknowledges this as "expected" behavior:
```go
{"🏳️\u200d🌈", 1},   // Rainbow flag returns 1 — the test codifies the bug
```

### 4. `charm.land/lipgloss/v2` v2.0.3 — Correct (via displaywidth)

`lipgloss.Width()` splits by newline, measures each line via `ansi.StringWidth()`, and returns the maximum. Since `ansi.StringWidth()` uses `GraphemeWidth` mode (displaywidth) by default, lipgloss inherits displaywidth's correct behavior.

go-runewidth is present as an indirect dependency but is NOT used in the default `Width()` path. It is only used if you explicitly call `ansi.StringWidthWc()`.

## Width Measurement Comparison

Test program compared all 4 libraries against the same emoji set. Emoji were selected from the spec plus additional edge cases.

### VS16 Emoji (text-default base + U+FE0F)

| Emoji | Name | go-runewidth | uniseg | displaywidth | lipgloss |
|-------|------|:---:|:---:|:---:|:---:|
| ❤️ | heart | **1** | 2 | 2 | 2 |
| ☘️ | shamrock | **1** | 2 | 2 | 2 |
| 🖊️ | pen | **1** | 2 | 2 | 2 |
| ☺️ | smiley | **1** | 2 | 2 | 2 |
| ✂️ | scissors | **1** | 2 | 2 | 2 |
| ❄️ | snowflake | **1** | 2 | 2 | 2 |
| ⚠️ | warning | **1** | 2 | 2 | 2 |
| ☁️ | cloud | **1** | 2 | 2 | 2 |
| ☀️ | sun | **1** | 2 | 2 | 2 |

go-runewidth returns 1 for ALL of these. The other three libraries correctly return 2.

### Complex Sequences

| Emoji | Name | go-runewidth | uniseg | displaywidth | lipgloss |
|-------|------|:---:|:---:|:---:|:---:|
| 🏳️‍🌈 | rainbow flag | **1** | 2 | 2 | 2 |
| 🇺🇸 | US flag | **1** | 2 | 2 | 2 |
| 🇩🇪 | DE flag | **1** | 2 | 2 | 2 |
| 1️⃣ | keycap 1 | **1** | **1** | 2 | 2 |
| #️⃣ | keycap hash | **1** | **1** | 2 | 2 |
| 👨‍👩‍👧 | family | 2 | 2 | 2 | 2 |
| 👍🏽 | skin tone | 2 | 2 | 2 | 2 |

go-runewidth fails on flags and rainbow flag. Both go-runewidth AND uniseg fail on keycap sequences.

### Already-Wide Emoji (Emoji_Presentation=Yes)

| Emoji | Name | go-runewidth | uniseg | displaywidth | lipgloss |
|-------|------|:---:|:---:|:---:|:---:|
| 👍 | thumbsup | 2 | 2 | 2 | 2 |
| 😊 | smile | 2 | 2 | 2 | 2 |
| 🔥 | fire | 2 | 2 | 2 | 2 |
| 🎉 | party | 2 | 2 | 2 | 2 |
| 🚀 | rocket | 2 | 2 | 2 | 2 |
| ✅ | check | 2 | 2 | 2 | 2 |
| ✨ | sparkles | 2 | 2 | 2 | 2 |
| ⭐ | star | 2 | 2 | 2 | 2 |

All libraries agree on these.

### Text-Default (No VS16)

| Emoji | Name | go-runewidth | uniseg | displaywidth | lipgloss |
|-------|------|:---:|:---:|:---:|:---:|
| ❤ | heart | 1 | 1 | 1 | 1 |
| ☘ | shamrock | 1 | 1 | 1 | 1 |
| 🖊 | pen | 1 | 1 | 1 | 1 |
| ☺ | smiley | 1 | 1 | 1 | 1 |

All libraries agree on these. Whether the terminal renders these as width 1 or 2 is terminal-dependent.

### Reaction Pill Simulation

| Pill Content | go-runewidth | uniseg | displaywidth | lipgloss |
|-------------|:---:|:---:|:---:|:---:|
| `❤️5` | **2** | 3 | 3 | 3 |
| `☘️3` | **2** | 3 | 3 | 3 |
| `🖊3` (no VS16) | 2 | 2 | 2 | 2 |

go-runewidth undercounts by 1 for VS16 pill content, which directly causes the border overflow described in the spec.

## Where Each Library Is Used in the Project

| Library | Usage Location | Purpose | Correctness |
|---------|---------------|---------|-------------|
| lipgloss v2 (displaywidth) | All rendering, borders, styles | Panel layout, border padding | Correct |
| uniseg | `messages/model.go:462`, `thread/model.go:463` | Reaction pill line wrapping | Mostly correct (fails keycaps) |
| go-runewidth | `compose/model.go:124` | Compose textarea line width | Has VS16 bugs |

## Root Cause of the go-runewidth Bug

The `StringWidth` function's grapheme cluster loop (`runewidth.go:224-232`) iterates runes within each cluster and takes the width of the **first rune with non-zero width**. Since:

1. VS16 (U+FE0F) is correctly classified as combining (width 0) — it's in the `combining` table range `{0xFE00, 0xFE0F}`
2. The base character (e.g., U+2764 HEAVY BLACK HEART) has width 1 in the `doublewidth` table
3. The loop `break`s on the first non-zero width

...VS16 never gets a chance to modify the width. The fix needs to detect VS16 within the grapheme cluster loop and promote the cluster width to 2 when present after an emoji-capable base character.

For flags (Regional Indicator pairs like 🇺🇸), the issue is different: each Regional Indicator rune (e.g., U+1F1FA) has `RuneWidth` of 1 in non-East-Asian mode (they are NOT in the `doublewidth` table). The grapheme cluster loop picks up width 1 from the first RI rune and stops. The correct width for an RI pair is 2.

## Root Cause of the uniseg Keycap Bug

uniseg's VS16 handling only applies when `firstProp == prExtendedPictographic`. Digits (0-9) and `#`/`*` have grapheme property `prAny`, not `prExtendedPictographic`, so VS16 does not trigger the width override. The keycap combining mark (U+20E3) then adds 0 (as `prExtend`), resulting in width 1 instead of 2.

## Fix Candidates

### go-runewidth — Highest Impact

**Bug:** VS16, flag, and keycap sequences all return wrong widths.

**Fix location:** `runewidth.go:224-232`, the grapheme cluster width loop.

**Approach:** After determining the cluster width from the first non-zero-width rune, check if the cluster contains VS16 (U+FE0F) and the base rune is emoji-capable. If so, promote width to 2. For Regional Indicator pairs, check if the cluster contains 2+ RI runes and return width 2.

**Impact:** go-runewidth is used transitively by a very large portion of the Go TUI ecosystem. Fixing it improves width measurement for all downstream consumers.

**Known issue:** Issue #59 ("Width is 1 when it should be 2") has been open since Feb 2022. Issue #76 (variation selectors returning width 1) was fixed in PR #90, but only to make VS16 itself return width 0 — not to make it promote the preceding character.

### uniseg — Medium Impact

**Bug:** Keycap sequences (1️⃣, #️⃣) return width 1 instead of 2.

**Fix location:** The VS16 check in `FirstGraphemeClusterInString()` and `Step()`/`StepString()`. Currently guarded by `firstProp == prExtendedPictographic`.

**Approach:** Extend the VS16 check to also apply when the cluster contains U+20E3 (COMBINING ENCLOSING KEYCAP), or more generally, when VS16 is present after any emoji-capable base (not just `ExtendedPictographic`).

**Impact:** uniseg is used by lipgloss for border grapheme iteration (not width), and by this project for reaction pill wrapping. The keycap bug is an edge case for most applications.

## Spec Corrections

The original investigation spec (`2026-04-27-emoji-width-investigation.md`) contains these inaccuracies given the current lipgloss v2 dependency:

1. **"lipgloss uses go-runewidth internally"** — Incorrect for v2. lipgloss v2 uses `clipperhouse/displaywidth` via `charmbracelet/x/ansi`.
2. **"There is no way to override the width calculation"** — In v2, the default `GraphemeWidth` mode already uses displaywidth, which is correct. The `WcWidth` mode (go-runewidth) is opt-in.
3. **Spec table shows go-runewidth returning 2 for ⭐ and ✅** — These ARE correct (they are in the doublewidth table with `Emoji_Presentation=Yes`). The issue is specifically with text-default emoji that need VS16 to trigger emoji presentation.

## Test Script

The comparison test program is at `/tmp/emoji_width_test/main.go`. It tests all 4 libraries against 40+ emoji including VS16 variants, complex sequences, already-wide emoji, and text-default emoji. Run with:

```bash
cd /tmp/emoji_width_test && go run main.go
```
