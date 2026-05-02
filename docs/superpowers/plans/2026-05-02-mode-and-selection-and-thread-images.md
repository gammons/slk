# Stronger Mode/Selection Visuals + Inline Images in Threads — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make insert-mode and the selected message visually unmistakable via tinted backgrounds, and render inline images in the thread side panel using the same pipeline as the main messages pane.

**Architecture:** Add a tint helper in `internal/ui/styles` that derives `ComposeInsertBG` and `SelectionTintColor(focused)` from the active theme's `Accent`/`TextMuted` and `Background`. Apply those backgrounds to the compose-insert style and to the messages / thread / threadsview selected-row styles. Extract the inline-image rendering pipeline from `internal/ui/messages` into a new shared `internal/ui/imgrender` package with a `Renderer` struct that owns `ImageContext` plus per-pane fetch-tracking maps. Both `messages.Model` and `thread.Model` embed an `*imgrender.Renderer`; `app.go` configures both and forwards `ImageReadyMsg` / `ImageFailedMsg` to both.

**Tech Stack:** Go 1.22+, charm.land/lipgloss/v2, charmbracelet/bubbletea, `github.com/charmbracelet/x/ansi`, the project's existing `internal/image` package.

**Spec:** `docs/superpowers/specs/2026-05-02-mode-and-selection-and-thread-images-design.md`

---

## File structure (lock-in)

**New files**

- `internal/ui/styles/tint.go` — `mixColors`, `SelectionTintColor`, `ComposeInsertBGColor` (derived theme-time).
- `internal/ui/styles/tint_test.go` — unit tests for the tint helpers.
- `internal/ui/imgrender/imgrender.go` — `ImageContext`, `ImageReadyMsg`, `ImageFailedMsg`, `Hit`, `SixelEntry`, `Renderer`, `RenderBlock`, `computeImageTarget`, `buildPlaceholder` (all moved from `internal/ui/messages`).
- `internal/ui/imgrender/imgrender_test.go` — unit tests moved from the messages package's image-rendering tests, adapted to the new package.

**Modified files**

- `internal/ui/styles/styles.go` — `Apply()` populates `ComposeInsertBG`; `ComposeInsert` style gains a Background; theme `colors.compose_insert_bg` / `colors.selection_bg` overrides honored.
- `internal/ui/styles/themes.go` — add `ComposeInsertBG` and `SelectionBg` (focused/unfocused) optional fields on `ThemeColors`.
- `internal/ui/compose/model.go` — inner textarea wrapper paints the same tint as the outer `ComposeInsert`.
- `internal/ui/messages/model.go` — `borderSelect` style gains tint background; `renderMessageEntry` ensures every selected line is right-padded; `Model` embeds `*imgrender.Renderer` instead of holding `imgCtx` / `fetchingImages` / `failedImages`; `renderAttachmentBlock` removed (calls into `Renderer.RenderBlock`).
- `internal/ui/thread/model.go` — `borderSelect` style gains tint background; `Model` gains `imgRenderer *imgrender.Renderer` + `SetImageContext`; `renderThreadMessage` loops over attachments using `imgRenderer.RenderBlock` instead of `messages.RenderAttachments`; cache-build path threads per-block flushes/sixel through the thread viewport.
- `internal/ui/threadsview/model.go` — `borderSelectStyle` / `borderInvisStyle` use `styles.SelectionBorderColor(focused)` and add tint background; `borderSelectStyle` becomes `borderSelectStyle(focused bool)`.
- `internal/ui/app.go` — `SetImageContext` forwards to `a.threadpane`; `ImageReadyMsg` / `ImageFailedMsg` cases also forward to the thread; uses `imgrender.ImageContext` etc.
- `cmd/slk/main.go` — `buildImgCtx` now returns `imgrender.ImageContext`.
- `internal/ui/app_imagepreview_test.go` — uses `imgrender.ImageContext`.
- `README.md` — image-rendering caveats updated.

---

## Task 1: Tint helper + theme-time derivation

**Files:**
- Create: `internal/ui/styles/tint.go`
- Create: `internal/ui/styles/tint_test.go`
- Modify: `internal/ui/styles/themes.go`
- Modify: `internal/ui/styles/styles.go` (rebuild path + override hooks)

- [ ] **Step 1: Write the failing tests for `mixColors` and `SelectionTintColor`**

Create `internal/ui/styles/tint_test.go`:

```go
package styles

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
)

func rgb(c color.Color) (uint8, uint8, uint8) {
	r, g, b, _ := c.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

func TestMixColors_HalfwayBlend(t *testing.T) {
	fg := lipgloss.Color("#FF0000") // red
	bg := lipgloss.Color("#0000FF") // blue
	out := mixColors(fg, bg, 0.5)
	r, g, b := rgb(out)
	// Halfway between #FF0000 and #0000FF is #7F007F (rounding to 0x7F).
	if r != 0x7F || g != 0x00 || b != 0x7F {
		t.Fatalf("expected #7F007F, got #%02X%02X%02X", r, g, b)
	}
}

func TestMixColors_AlphaZeroIsBackground(t *testing.T) {
	out := mixColors(lipgloss.Color("#FFFFFF"), lipgloss.Color("#112233"), 0.0)
	r, g, b := rgb(out)
	if r != 0x11 || g != 0x22 || b != 0x33 {
		t.Fatalf("alpha=0 must equal bg, got #%02X%02X%02X", r, g, b)
	}
}

func TestMixColors_AlphaOneIsForeground(t *testing.T) {
	out := mixColors(lipgloss.Color("#AABBCC"), lipgloss.Color("#000000"), 1.0)
	r, g, b := rgb(out)
	if r != 0xAA || g != 0xBB || b != 0xCC {
		t.Fatalf("alpha=1 must equal fg, got #%02X%02X%02X", r, g, b)
	}
}

func TestSelectionTintColor_FocusedIsAccentMix(t *testing.T) {
	Apply("dark", config.Theme{})
	expected := mixColors(Accent, Background, defaultTintAlpha)
	got := SelectionTintColor(true)
	er, eg, eb := rgb(expected)
	gr, gg, gb := rgb(got)
	if er != gr || eg != gg || eb != gb {
		t.Fatalf("focused tint mismatch: want #%02X%02X%02X got #%02X%02X%02X", er, eg, eb, gr, gg, gb)
	}
}

func TestSelectionTintColor_UnfocusedIsTextMutedMix(t *testing.T) {
	Apply("dark", config.Theme{})
	expected := mixColors(TextMuted, Background, defaultTintAlpha)
	got := SelectionTintColor(false)
	er, eg, eb := rgb(expected)
	gr, gg, gb := rgb(got)
	if er != gr || eg != gg || eb != gb {
		t.Fatalf("unfocused tint mismatch: want #%02X%02X%02X got #%02X%02X%02X", er, eg, eb, gr, gg, gb)
	}
}

func TestComposeInsertBG_DerivedFromAccentAndBackground(t *testing.T) {
	Apply("dark", config.Theme{})
	expected := mixColors(Accent, Background, defaultTintAlpha)
	er, eg, eb := rgb(expected)
	gr, gg, gb := rgb(ComposeInsertBG)
	if er != gr || eg != gg || eb != gb {
		t.Fatalf("ComposeInsertBG mismatch: want #%02X%02X%02X got #%02X%02X%02X", er, eg, eb, gr, gg, gb)
	}
}

func TestComposeInsertBG_OverrideFromThemeColors(t *testing.T) {
	RegisterCustomTheme("tinttest", ThemeColors{
		Primary: "#000000", Accent: "#000000", Warning: "#000000",
		Error: "#000000", Background: "#000000", Surface: "#000000",
		SurfaceDark: "#000000", Text: "#FFFFFF", TextMuted: "#888888",
		Border:         "#222222",
		ComposeInsertBG: "#ABCDEF",
	})
	Apply("tinttest", config.Theme{})
	r, g, b := rgb(ComposeInsertBG)
	if r != 0xAB || g != 0xCD || b != 0xEF {
		t.Fatalf("override not honored: got #%02X%02X%02X", r, g, b)
	}
	Apply("dark", config.Theme{})
}

// Lock the default α for the 12 built-in themes — guarantees the
// derived tints don't drift silently across refactors.
func TestComposeInsertBG_StableAcrossBuiltinThemes(t *testing.T) {
	for _, name := range ThemeNames() {
		Apply(name, config.Theme{})
		// Just assert it's non-nil and distinct from Background;
		// exact RGB values are too brittle to lock per-theme.
		if ComposeInsertBG == nil {
			t.Fatalf("%s: ComposeInsertBG is nil", name)
		}
		br, bg, bb := rgb(Background)
		cr, cg, cb := rgb(ComposeInsertBG)
		if br == cr && bg == cg && bb == cb {
			t.Fatalf("%s: ComposeInsertBG must differ from Background", name)
		}
	}
	Apply("dark", config.Theme{})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ui/styles/ -run 'TestMixColors|TestSelectionTintColor|TestComposeInsertBG' -v`

Expected: FAIL with "undefined: mixColors", "undefined: SelectionTintColor", "undefined: ComposeInsertBG", "undefined: defaultTintAlpha", "ThemeColors has no field ComposeInsertBG".

- [ ] **Step 3: Add the `ComposeInsertBG` and `SelectionBg` fields to `ThemeColors`**

Open `internal/ui/styles/themes.go`. Find the `ThemeColors` struct definition. Add three new optional fields next to the existing `SelectionBackground` / `SelectionForeground` fields:

```go
// ComposeInsertBG, SelectionBgFocused, and SelectionBgUnfocused are
// optional explicit overrides for the tints derived in tint.go. When
// empty, tint.go computes them from Accent/TextMuted+Background.
ComposeInsertBG      string `toml:"compose_insert_bg"`
SelectionBgFocused   string `toml:"selection_bg_focused"`
SelectionBgUnfocused string `toml:"selection_bg_unfocused"`
```

(Search the file for `SelectionBackground` to find the right struct; add these alongside.)

- [ ] **Step 4: Create the tint helper file**

Create `internal/ui/styles/tint.go`:

```go
// internal/ui/styles/tint.go
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// defaultTintAlpha is the share of the foreground color in mixColors when
// deriving compose-insert and selection backgrounds. 0.15 keeps the tint
// readable on every built-in theme without overpowering message text.
const defaultTintAlpha = 0.15

// ComposeInsertBG is the background color used by the compose box when
// focused (insert mode). Apply() populates this either from a theme's
// explicit colors.compose_insert_bg, or by mixing Accent into Background
// at defaultTintAlpha.
var ComposeInsertBG color.Color

// selectionBgFocused / selectionBgUnfocused hold the resolved tint colors
// for the selected-message row. They are populated by Apply() and read
// via SelectionTintColor. Held as package-private vars so callers always
// go through the function (which tolerates Apply() never having run yet
// during init-order edge cases).
var (
	selectionBgFocused   color.Color
	selectionBgUnfocused color.Color
)

// SelectionTintColor returns the background color used to fill the
// selected message row. When the panel has focus we tint with Accent;
// when it doesn't we tint with TextMuted (so the row is still visible
// but no longer competes with the focused panel).
func SelectionTintColor(focused bool) color.Color {
	if focused {
		if selectionBgFocused == nil {
			selectionBgFocused = mixColors(Accent, Background, defaultTintAlpha)
		}
		return selectionBgFocused
	}
	if selectionBgUnfocused == nil {
		selectionBgUnfocused = mixColors(TextMuted, Background, defaultTintAlpha)
	}
	return selectionBgUnfocused
}

// mixColors returns a straight-line RGB interpolation between fg and bg.
// alpha is the share of fg: 0.0 returns bg, 1.0 returns fg.
func mixColors(fg, bg color.Color, alpha float64) color.Color {
	if alpha <= 0 {
		return bg
	}
	if alpha >= 1 {
		return fg
	}
	fr, fg2, fb, _ := fg.RGBA()
	br, bg2, bb, _ := bg.RGBA()
	// RGBA returns 16-bit channels; collapse to 8-bit before mixing.
	fr8, fg8, fb8 := float64(fr>>8), float64(fg2>>8), float64(fb>>8)
	br8, bg8, bb8 := float64(br>>8), float64(bg2>>8), float64(bb>>8)
	r := uint8(fr8*alpha + br8*(1-alpha) + 0.5)
	g := uint8(fg8*alpha + bg8*(1-alpha) + 0.5)
	b := uint8(fb8*alpha + bb8*(1-alpha) + 0.5)
	return lipgloss.Color(rgbHex(r, g, b))
}

func rgbHex(r, g, b uint8) string {
	const hex = "0123456789ABCDEF"
	out := []byte("#000000")
	out[1] = hex[r>>4]
	out[2] = hex[r&0x0F]
	out[3] = hex[g>>4]
	out[4] = hex[g&0x0F]
	out[5] = hex[b>>4]
	out[6] = hex[b&0x0F]
	return string(out)
}

// resetDerivedTints invalidates the cached SelectionTintColor values so
// the next call recomputes from the current Accent/TextMuted/Background.
// Called by Apply() after the palette is rebuilt.
func resetDerivedTints() {
	selectionBgFocused = nil
	selectionBgUnfocused = nil
}
```

- [ ] **Step 5: Wire `Apply()` to populate `ComposeInsertBG` and reset derived tints**

In `internal/ui/styles/styles.go`, locate the `Apply` function (starts at line 234). Find the block that handles `SelectionBackground` / `SelectionForeground` (lines 306-319). Immediately after that block (still inside `Apply`, before the call to `buildStyles()` on line 321), append:

```go
// Compose-insert background: explicit theme override wins, otherwise
// derive from Accent + Background at defaultTintAlpha.
if colors.ComposeInsertBG != "" {
	ComposeInsertBG = lipgloss.Color(colors.ComposeInsertBG)
} else {
	ComposeInsertBG = mixColors(Accent, Background, defaultTintAlpha)
}

// Pre-resolve selection tints (theme overrides take precedence). Caching
// avoids recomputing on every render; resetDerivedTints clears them so
// the next SelectionTintColor() call repopulates from the new theme.
resetDerivedTints()
if colors.SelectionBgFocused != "" {
	selectionBgFocused = lipgloss.Color(colors.SelectionBgFocused)
}
if colors.SelectionBgUnfocused != "" {
	selectionBgUnfocused = lipgloss.Color(colors.SelectionBgUnfocused)
}
```

- [ ] **Step 6: Run the tint tests to verify they pass**

Run: `go test ./internal/ui/styles/ -run 'TestMixColors|TestSelectionTintColor|TestComposeInsertBG' -v`

Expected: PASS for all 8 test cases.

- [ ] **Step 7: Run the full styles package test suite**

Run: `go test ./internal/ui/styles/ -v`

Expected: every existing test still passes (the tint helper additions are additive).

- [ ] **Step 8: Commit**

```bash
git add internal/ui/styles/tint.go internal/ui/styles/tint_test.go internal/ui/styles/themes.go internal/ui/styles/styles.go
git commit -m "Add theme-derived tint helper for selection and insert mode"
```

---

## Task 2: Apply tinted background to the compose-insert style

**Files:**
- Modify: `internal/ui/styles/styles.go` (the `ComposeInsert` style construction in both the `var` block and `buildStyles()`)
- Modify: `internal/ui/compose/model.go:1049-1088` (inner textarea wrapper)
- Test: `internal/ui/compose/model_test.go` (new test confirming the tint reaches the rendered output)

- [ ] **Step 1: Locate or create the compose model test file**

Run: `ls internal/ui/compose/`

If `model_test.go` exists, append the new test function. If it does not, create it with a `package compose` header.

- [ ] **Step 2: Write the failing test for tinted compose output**

Append to `internal/ui/compose/model_test.go`:

```go
package compose

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/styles"
)

// composeInsertTintHex returns the hex-string form of the current
// theme's ComposeInsertBG, formatted the way ansi escape sequences
// emit it (lowercased "rgb:" semantics aren't relevant here — we
// just need a substring that's stable for grep).
func composeInsertTintHex(t *testing.T) (uint8, uint8, uint8) {
	t.Helper()
	r, g, b, _ := styles.ComposeInsertBG.RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

// TestView_FocusedRendersComposeInsertBackground asserts that when the
// compose box is focused, its rendered output contains the
// ComposeInsertBG tint as an ANSI background code. This guards against
// regressions where ComposeInsert loses its Background() call.
func TestView_FocusedRendersComposeInsertBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})

	m := New("Message #channel...", "channel", nil, nil, nil)
	out := m.View(60, true /* focused */)

	r, g, b := composeInsertTintHex(t)
	// The lipgloss/v2 renderer emits bg as "48;2;R;G;B".
	want := []byte{0x1b, '['} // CSI; we just need to know there's at least one bg sequence.
	_ = want
	// Build the substring we expect somewhere in the rendered output.
	expected := fmtRGBBg(r, g, b)
	if !strings.Contains(out, expected) {
		t.Fatalf("focused compose output missing tint bg %q\nplain: %q\nraw: %q",
			expected, ansi.Strip(out), out)
	}
}

// TestView_UnfocusedDoesNotUseComposeInsertBackground asserts that when
// the compose box is NOT focused, the ComposeInsertBG tint is absent.
// (The unfocused box keeps SurfaceDark as its background.)
func TestView_UnfocusedDoesNotUseComposeInsertBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})

	m := New("Message #channel...", "channel", nil, nil, nil)
	out := m.View(60, false /* focused */)

	r, g, b := composeInsertTintHex(t)
	expected := fmtRGBBg(r, g, b)
	if strings.Contains(out, expected) {
		t.Fatalf("unfocused compose output unexpectedly contains tint bg %q\nraw: %q",
			expected, out)
	}
}

func fmtRGBBg(r, g, b uint8) string {
	// lipgloss/v2 uses "48;2;R;G;B" for true-color bg in CSI SGR sequences.
	return "48;2;" + itoa(r) + ";" + itoa(g) + ";" + itoa(b)
}

func itoa(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
```

> If `compose.New`'s constructor signature differs, adapt the call to match the existing pattern from other tests in the same package. The point is to instantiate a Model and call `View(60, true)` / `View(60, false)`.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/ui/compose/ -run 'TestView_FocusedRendersComposeInsertBackground|TestView_UnfocusedDoesNotUseComposeInsertBackground' -v`

Expected: FAIL — focused output does not yet contain the `ComposeInsertBG` color.

- [ ] **Step 4: Update `ComposeInsert` to use the tint background**

In `internal/ui/styles/styles.go`, find both definitions of `ComposeInsert` (the package `var` block at lines 165-172 and the rebuild in `buildStyles()` at lines 380-382). Change `BorderForeground(Primary)` → `BorderForeground(Accent)`, and `Background(SurfaceDark)` → `Background(ComposeInsertBG)`.

The `var`-block definition becomes:

```go
ComposeInsert = lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).
		BorderLeft(true).
		BorderForeground(Accent).
		BorderBackground(ComposeInsertBG).
		Background(ComposeInsertBG).
		Foreground(TextPrimary).
		Padding(1, 1, 1, 1)
```

The `buildStyles()` definition becomes:

```go
ComposeInsert = lipgloss.NewStyle().
	BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(Accent).BorderBackground(ComposeInsertBG).
	Background(ComposeInsertBG).Foreground(TextPrimary).Padding(1, 1, 1, 1)
```

- [ ] **Step 5: Update the inner textarea wrapper in `compose/model.go`**

In `internal/ui/compose/model.go`, lines 1062-1082, change every reference to `styles.SurfaceDark` inside the focused branch to `styles.ComposeInsertBG`. The unfocused branch keeps `SurfaceDark` (unchanged).

Replace lines 1062-1082 with:

```go
var box string
// Pick the inner background to match the outer style: ComposeInsertBG
// when focused, SurfaceDark when not. Without this, the textarea's
// internal Inline(true) styles only paint behind text and the row's
// trailing whitespace shows the WRONG bg.
innerBG := styles.SurfaceDark
if focused {
	innerBG = styles.ComposeInsertBG
}

// If empty and unfocused, render placeholder manually with correct background.
// When focused, show an empty compose box with cursor (no placeholder).
if m.input.Value() == "" && !focused {
	placeholder := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Background(innerBG).
		Width(innerWidth).
		Render(m.input.Placeholder)
	box = style.Render(placeholder)
} else {
	// Wrap textarea output with full-width tinted background.
	// The textarea's internal styles use Inline(true) which only covers text,
	// not the full line width. This wrapper ensures consistent background.
	content := lipgloss.NewStyle().
		Background(innerBG).
		Foreground(styles.TextPrimary).
		Width(innerWidth).
		Render(m.input.View())
	box = style.Render(content)
}
```

- [ ] **Step 6: Run the compose tests to verify they pass**

Run: `go test ./internal/ui/compose/ -v`

Expected: PASS — `TestView_FocusedRendersComposeInsertBackground` finds the tint in focused output; `TestView_UnfocusedDoesNotUseComposeInsertBackground` confirms the tint is absent when unfocused.

- [ ] **Step 7: Sanity-build the binary**

Run: `go build ./...`

Expected: clean exit with no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/styles/styles.go internal/ui/compose/model.go internal/ui/compose/model_test.go
git commit -m "Tint compose box background when focused (insert mode)"
```

---

## Task 3: Tinted background for the selected message in the messages pane

**Files:**
- Modify: `internal/ui/messages/model.go:1041-1126` (cacheStyles construction + per-line padding)
- Test: `internal/ui/messages/model_test.go` (new test confirming selected row contains the tint bg, and the unselected row does not)

- [ ] **Step 1: Write the failing test for selected-row tint**

Append to `internal/ui/messages/model_test.go`:

```go
// TestSelectedRowContainsTintBackground asserts that the rendered
// output for the selected message includes the SelectionTintColor as
// an ANSI background, while a non-selected row does not. Guards
// against regressions in cacheStyles' borderSelect construction.
func TestSelectedRowContainsTintBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})

	m := New(80, 20)
	m.SetMessages([]MessageItem{
		{TS: "1.0", UserID: "U1", UserName: "alice", Text: "first message"},
		{TS: "2.0", UserID: "U2", UserName: "bob", Text: "second message"},
	})
	m.SetFocused(true)
	m.MoveDown() // select index 1 (bob)

	out := m.View()

	r, g, b, _ := styles.SelectionTintColor(true).RGBA()
	want := fmtRGBBg(uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if !strings.Contains(out, want) {
		t.Fatalf("expected selected row to contain tint bg %q\nout=%q",
			want, out)
	}
}

func fmtRGBBg(r, g, b uint8) string {
	return "48;2;" + itoaU8(r) + ";" + itoaU8(g) + ";" + itoaU8(b)
}

func itoaU8(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
```

> If imports for `styles`, `config`, or `strings` aren't already present, add them. If `New(width, height)` / `SetFocused` / `MoveDown` / `View` have different names in the package, adapt to the existing API by inspecting `internal/ui/messages/model_test.go` for the constructor pattern.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/messages/ -run TestSelectedRowContainsTintBackground -v`

Expected: FAIL — `borderSelect` does not yet apply a Background.

- [ ] **Step 3: Add Background() to `borderSelect` in `buildCacheStyles`**

In `internal/ui/messages/model.go`, line 1060, replace:

```go
borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.SelectionBorderColor(m.focused)).BorderBackground(styles.Background)
```

with:

```go
borderSelect := lipgloss.NewStyle().
	BorderStyle(thickLeftBorder).BorderLeft(true).
	BorderForeground(styles.SelectionBorderColor(m.focused)).
	BorderBackground(styles.SelectionTintColor(m.focused)).
	Background(styles.SelectionTintColor(m.focused))
```

- [ ] **Step 4: Ensure every line of the selected entry is right-padded to the full width**

In `renderMessageEntry` (lines 1087-1126), the variable `filled` already widens the rendered content via `cs.borderFill.Width(width-1).Render(rendered)`. That call paints the row with `styles.Background`, which then conflicts with the new tint. Replace lines 1093-1096:

```go
rendered, attachFlushes, attachSixel, attachHits := m.renderMessagePlain(msg, width, avatarStr, m.userNames, m.channelNames, i == m.selected)
filled := cs.borderFill.Width(width - 1).Render(rendered)
normal := cs.borderInvis.Render(filled)
selected := cs.borderSelect.Render(filled)
```

with:

```go
rendered, attachFlushes, attachSixel, attachHits := m.renderMessagePlain(msg, width, avatarStr, m.userNames, m.channelNames, i == m.selected)
// Two filled variants: borderFill (Background) for the unselected
// pre-render, and the SelectionTintColor for the selected pre-render.
// Without per-variant fills, the trailing whitespace of every wrapped
// line shows the WRONG background and the tint stops at the last
// character of content.
filledNormal := cs.borderFill.Width(width - 1).Render(rendered)
selectedFill := lipgloss.NewStyle().Background(styles.SelectionTintColor(m.focused)).Width(width - 1).Render(rendered)
normal := cs.borderInvis.Render(filledNormal)
selected := cs.borderSelect.Render(selectedFill)
```

- [ ] **Step 5: Update `linesPlain` to keep using the unbordered, untinted content**

The next line uses `filled` (now renamed) for the plain mirror:

```go
linesP := plainLines(filled)
```

becomes:

```go
linesP := plainLines(filledNormal)
```

`linesPlain` mirrors clipboard text and must NOT contain the tint background.

- [ ] **Step 6: Run the messages tests to verify selection test passes and nothing else regresses**

Run: `go test ./internal/ui/messages/ -v`

Expected: every existing test passes (including `selection_test.go`'s clipboard tests, which assert plain text does not contain ANSI codes); the new `TestSelectedRowContainsTintBackground` passes.

- [ ] **Step 7: Build the binary**

Run: `go build ./...`

Expected: clean exit.

- [ ] **Step 8: Commit**

```bash
git add internal/ui/messages/model.go internal/ui/messages/model_test.go
git commit -m "Tint selected message row in messages pane"
```

---

## Task 4: Tinted background for the selected reply in the thread panel

**Files:**
- Modify: `internal/ui/thread/model.go:980-1024` (border styles + per-line padding)
- Test: `internal/ui/thread/model_test.go` (new test mirroring Task 3's pattern)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/thread/model_test.go` (or create the file if missing — use `package thread` plus the imports the existing thread tests use):

```go
func TestSelectedReplyContainsTintBackground(t *testing.T) {
	styles.Apply("dark", config.Theme{})

	m := New(60, 20) // adapt to actual constructor signature
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"}
	replies := []messages.MessageItem{
		{TS: "1.001", UserID: "U2", UserName: "bob", Text: "reply one"},
		{TS: "1.002", UserID: "U3", UserName: "carol", Text: "reply two"},
	}
	m.SetParent(parent)
	m.SetReplies(replies)
	m.SetFocused(true)
	m.MoveDown() // select reply index 1

	out := m.View()

	r, g, b, _ := styles.SelectionTintColor(true).RGBA()
	want := "48;2;" + itoaU8(uint8(r>>8)) + ";" + itoaU8(uint8(g>>8)) + ";" + itoaU8(uint8(b>>8))
	if !strings.Contains(out, want) {
		t.Fatalf("expected selected reply to contain tint bg %q\nout=%q", want, out)
	}
}

func itoaU8(v uint8) string {
	if v == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
```

(If the existing thread tests already define an `itoaU8` helper, omit the duplicate.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/thread/ -run TestSelectedReplyContainsTintBackground -v`

Expected: FAIL — `borderSelect` does not yet carry a Background.

- [ ] **Step 3: Update the thread panel border styles**

In `internal/ui/thread/model.go` lines 985-989, replace:

```go
borderFill := lipgloss.NewStyle().Background(styles.Background)
borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
	BorderForeground(styles.Background).BorderBackground(styles.Background)
borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
	BorderForeground(styles.SelectionBorderColor(m.focused)).BorderBackground(styles.Background)
```

with:

```go
borderFill := lipgloss.NewStyle().Background(styles.Background)
borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
	BorderForeground(styles.Background).BorderBackground(styles.Background)
borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
	BorderForeground(styles.SelectionBorderColor(m.focused)).
	BorderBackground(styles.SelectionTintColor(m.focused)).
	Background(styles.SelectionTintColor(m.focused))
```

- [ ] **Step 4: Add a per-variant fill so the tint reaches the right edge**

In the same function, locate the `filled := borderFill.Width(width - 1).Render(rendered)` line (around line 1001). Replace lines 1000-1014 (from `rendered := m.renderThreadMessage(...)` through `m.cache = append(...)`) with:

```go
rendered := m.renderThreadMessage(reply, width, m.userNames, m.channelNames, i == m.selected)
filledNormal := borderFill.Width(width - 1).Render(rendered)
selectedFill := lipgloss.NewStyle().
	Background(styles.SelectionTintColor(m.focused)).
	Width(width - 1).
	Render(rendered)
normal := borderInvis.Render(filledNormal)
selected := borderSelect.Render(selectedFill)
linesN := strings.Split(normal, "\n")
linesS := strings.Split(selected, "\n")
m.cache = append(m.cache, viewEntry{
	linesNormal:      linesN,
	linesSelected:    linesS,
	linesPlain:       messages.PlainLines(filledNormal),
	height:           len(linesN),
	replyIdx:         i,
	contentColOffset: 1,
})
```

- [ ] **Step 5: Run the thread tests**

Run: `go test ./internal/ui/thread/ -v`

Expected: every test passes; new test passes.

- [ ] **Step 6: Build**

Run: `go build ./...`

Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/thread/model.go internal/ui/thread/model_test.go
git commit -m "Tint selected reply row in thread panel"
```

---

## Task 5: Tinted background + focus-aware dim in the threads-list view

**Files:**
- Modify: `internal/ui/threadsview/model.go:50-72` (border style helpers and their call sites)
- Test: `internal/ui/threadsview/model_test.go`

- [ ] **Step 1: Find every call site of `borderSelectStyle()` / `borderInvisStyle()`**

Run: `rg "borderSelectStyle\(|borderInvisStyle\(" internal/ui/threadsview/`

Note all call sites — they need to be updated to pass `focused`.

- [ ] **Step 2: Write the failing test**

Append to `internal/ui/threadsview/model_test.go`:

```go
func TestSelectedCardDimsWhenUnfocused(t *testing.T) {
	styles.Apply("dark", config.Theme{})

	// Construct a model with at least one summary so a selection
	// exists. Adapt to the actual New / setter signature; the existing
	// tests in this file are the reference.
	m := newModelWithSummaries(t, 2)

	m.SetFocused(true)
	m.MoveDown()
	focusedOut := m.View()

	m.SetFocused(false)
	unfocusedOut := m.View()

	// Focused selected row should contain the bright Accent border.
	r, g, b, _ := styles.Accent.RGBA()
	wantAccent := "38;2;" + itoaU8(uint8(r>>8)) + ";" + itoaU8(uint8(g>>8)) + ";" + itoaU8(uint8(b>>8))
	if !strings.Contains(focusedOut, wantAccent) {
		t.Fatalf("focused selected card missing accent fg %q", wantAccent)
	}

	// Unfocused selected row should NOT contain the bright Accent
	// border any more — it should dim to TextMuted (per
	// SelectionBorderColor(false)).
	if strings.Contains(unfocusedOut, wantAccent) {
		t.Fatal("unfocused selected card still contains accent border; should dim to TextMuted")
	}
}
```

> `newModelWithSummaries` is a stand-in. Use whatever helper / direct-construction pattern the existing tests in this file already use.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/ui/threadsview/ -run TestSelectedCardDimsWhenUnfocused -v`

Expected: FAIL — `borderSelectStyle()` hard-codes `styles.Accent`, so the focused/unfocused outputs are identical.

- [ ] **Step 4: Update the border style helpers**

Replace lines 56-72 of `internal/ui/threadsview/model.go` with:

```go
func borderInvisStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.Background).
		BorderBackground(styles.Background)
}

func borderSelectStyle(focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(thickLeftBorder).BorderLeft(true).
		BorderForeground(styles.SelectionBorderColor(focused)).
		BorderBackground(styles.SelectionTintColor(focused)).
		Background(styles.SelectionTintColor(focused))
}

func borderFillStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(styles.Background)
}
```

- [ ] **Step 5: Update all call sites of `borderSelectStyle()` to pass `m.focused`**

For every call site discovered in Step 1, append `m.focused` (or whatever the local "is this panel focused" field is on the receiver). Example:

```go
selStyle := borderSelectStyle(m.focused)
```

If a call site does NOT have access to `m.focused` (e.g. it's in a free function), thread the `focused` boolean down through the function signature.

- [ ] **Step 6: Update the per-variant fill in the threadsview cache so the tint reaches the right edge**

Find where `borderFillStyle().Width(...).Render(...)` is used to back the selected card (search for `borderSelectStyle` callers). For the selected variant, paint with `styles.SelectionTintColor(m.focused)` instead of `styles.Background` — same approach as Tasks 3 and 4. The exact location depends on the threadsview's cache structure; if the file uses a single shared `filled` for both normal and selected variants, split it the same way as Task 3 Step 4.

- [ ] **Step 7: Run the threadsview tests**

Run: `go test ./internal/ui/threadsview/ -v`

Expected: PASS.

- [ ] **Step 8: Build**

Run: `go build ./...`

Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/threadsview/model.go internal/ui/threadsview/model_test.go
git commit -m "Tint selected thread card and dim border when unfocused"
```

---

## Task 6: Create the `imgrender` package skeleton (types + helpers)

**Files:**
- Create: `internal/ui/imgrender/imgrender.go` (initially: just the moved types and the two pure helpers)
- Create: `internal/ui/imgrender/imgrender_test.go` (relocated tests for `computeImageTarget` and `buildPlaceholder`)

This task moves only the **stateless** types and helpers; `Renderer` and the per-block render method come in Task 7. Goal: this commit compiles and existing messages-pane behavior is unchanged. We do NOT keep compatibility re-exports — instead we create the new package file with just the types, and the messages package keeps its original copies in this commit. The two-package duplication is temporary and resolved in Task 7.

- [ ] **Step 1: Create the new package file with the moved types and helpers**

Create `internal/ui/imgrender/imgrender.go`:

```go
// Package imgrender renders inline image attachments for any UI panel
// (messages pane, thread side panel) using the kitty / sixel /
// halfblock pipelines. Two callers — internal/ui/messages and
// internal/ui/thread — embed a Renderer to share the fetch-tracking
// and per-block encode logic.
package imgrender

import (
	"image"

	tea "github.com/charmbracelet/bubbletea/v2"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
)

// ImageContext bundles the dependencies a Renderer needs. Configured
// at startup via Renderer.SetContext. SendMsg is optional; when nil,
// fetches still complete but no re-render is triggered when bytes
// arrive.
type ImageContext struct {
	Protocol    imgpkg.Protocol
	Fetcher     *imgpkg.Fetcher
	KittyRender *imgpkg.KittyRenderer
	CellPixels  image.Point
	MaxRows     int
	MaxCols     int
	SendMsg     func(tea.Msg)
}

// ImageReadyMsg is dispatched by the prefetcher when an image attachment
// has finished downloading and decoding. The host panel uses the Channel
// + TS to identify which entry to invalidate.
type ImageReadyMsg struct {
	Channel string
	TS      string
	Key     string
}

// ImageFailedMsg is dispatched when all auth attempts for an image have
// failed. Carries the cache key only.
type ImageFailedMsg struct {
	Key string
}

// Hit is one inline-image hit-rect, expressed in coordinates relative
// to a single rendered block. The host panel translates these to its
// own per-entry / viewport coordinate system.
type Hit struct {
	RowStartInEntry int
	RowEndInEntry   int // exclusive
	ColStart        int
	ColEnd          int // exclusive
	FileID          string
	AttIdx          int
}

// SixelEntry holds the pre-computed sixel bytes for one inline image,
// plus the halfblock fallback used when the image is only partially
// visible.
type SixelEntry struct {
	Bytes    []byte
	Fallback []string
	Height   int
}

// computeImageTarget returns the rendered cell dimensions for an
// attachment given the available width and the active context's
// cell-pixel size and row/column caps. Pure function — no side
// effects on Renderer state.
func computeImageTarget(att messages.Attachment, ctx ImageContext, availWidth int) image.Point {
	// MOVE THE EXISTING IMPLEMENTATION FROM internal/ui/messages/model.go:1740-1780 HERE VERBATIM,
	// adapting type references (Attachment -> messages.Attachment, ImageContext -> the local type).
	panic("TODO: copied in Step 2")
}

// buildPlaceholder returns a target.Y-row block of theme-surface-colored
// spaces with a centered "⏳ Loading <name>..." string. Used while an
// image fetch is in flight.
func buildPlaceholder(name string, target image.Point) []string {
	// MOVE THE EXISTING IMPLEMENTATION FROM internal/ui/messages/model.go:1797-1839 HERE VERBATIM,
	// adapting any package-private references.
	panic("TODO: copied in Step 2")
}
```

- [ ] **Step 2: Copy the actual `computeImageTarget` and `buildPlaceholder` bodies**

Read `internal/ui/messages/model.go:1740-1839` and replace the panicking stubs with the verbatim function bodies. Adjust any references to package-private identifiers from `messages` so they resolve in `imgrender`. If the implementations reference `styles` or `imgpkg`, add those imports.

- [ ] **Step 3: Verify build**

Run: `go build ./...`

Expected: clean. The new package compiles in isolation; no existing call sites use it yet.

- [ ] **Step 4: Write unit tests for the moved helpers**

Find the existing tests for `computeImageTarget` / `buildPlaceholder` in the messages package:

Run: `rg -l "computeImageTarget|buildPlaceholder" internal/ui/messages/`

For each test that exercises only these two functions (no `Renderer` / `Model` state), copy the test into `internal/ui/imgrender/imgrender_test.go`, change the package declaration to `package imgrender`, and update calls so they reference `imgrender.computeImageTarget` (still package-private — so the test stays inside the package). Delete the original copies from the messages package only if the test does not also exercise messages-pane state.

- [ ] **Step 5: Run the new package's tests**

Run: `go test ./internal/ui/imgrender/ -v`

Expected: PASS.

- [ ] **Step 6: Run the full project tests to confirm nothing broke**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/imgrender/
git commit -m "Add imgrender package skeleton (types + pure helpers)"
```

---

## Task 7: Move the `Renderer` (`renderAttachmentBlock`) into `imgrender`

**Files:**
- Modify: `internal/ui/imgrender/imgrender.go` (add `Renderer` type + `RenderBlock` method)
- Modify: `internal/ui/messages/model.go` (replace `imgCtx`/`fetchingImages`/`failedImages` fields with `*imgrender.Renderer`; delete `renderAttachmentBlock`; update all call sites)
- Modify: `internal/ui/messages/model.go` types: keep `entryHit` and `sixelEntry` private (internal cache types) but import `imgrender.Hit` / `imgrender.SixelEntry` for the renderer's return values. Convert at the call site.
- Modify: `internal/ui/imgrender/imgrender_test.go` (add tests for Renderer)
- Modify: `internal/ui/app.go` (`SetImageContext`, `ImageReadyMsg` / `ImageFailedMsg` cases use `imgrender` types)
- Modify: `cmd/slk/main.go` (`buildImgCtx` returns `imgrender.ImageContext`)
- Modify: `internal/ui/app_imagepreview_test.go` (uses `imgrender.ImageContext`)

This task is the core extraction. After it lands:
- `internal/ui/messages` no longer defines `ImageContext`, `ImageReadyMsg`, `ImageFailedMsg`, `renderAttachmentBlock`, `computeImageTarget`, `buildPlaceholder`, `fetchingImages`, or `failedImages`.
- The messages-pane View output is byte-for-byte identical to before (verified via the existing test suite).

- [ ] **Step 1: Add `Renderer` to `internal/ui/imgrender/imgrender.go`**

Append to the package file:

```go
import (
	"bytes"
	"context"
	"io"
	"log"

	"github.com/charmbracelet/lipgloss"
	"github.com/gammons/slk/internal/ui/messages"
)

// Renderer owns the per-panel inline-image state: fetch-in-flight set,
// permanent-failure set, and the active ImageContext. One instance per
// host panel (one for messages, one for thread).
type Renderer struct {
	ctx      ImageContext
	fetching map[string]struct{}
	failed   map[string]struct{}
}

// NewRenderer returns an empty Renderer. The caller must call
// SetContext before RenderBlock can produce inline output (a zero-
// valued context falls through to the legacy text rendering, which
// is also the safe default during early app startup).
func NewRenderer() *Renderer {
	return &Renderer{
		fetching: map[string]struct{}{},
		failed:   map[string]struct{}{},
	}
}

// SetContext configures the inline-image rendering pipeline. May be
// called multiple times (e.g. when the prefetcher's tea.Program send
// fn becomes available). Resets the fetch-tracking state.
func (r *Renderer) SetContext(ctx ImageContext) {
	r.ctx = ctx
	for k := range r.fetching {
		delete(r.fetching, k)
	}
	for k := range r.failed {
		delete(r.failed, k)
	}
}

// Context returns the current ImageContext (read-only). Callers should
// not mutate the returned struct.
func (r *Renderer) Context() ImageContext { return r.ctx }

// ClearFetching removes a key from the in-flight set. Returns true if
// the key was present (i.e. this Renderer was tracking the fetch).
func (r *Renderer) ClearFetching(key string) bool {
	if _, ok := r.fetching[key]; !ok {
		return false
	}
	delete(r.fetching, key)
	return true
}

// MarkFailed clears the in-flight bit for key and adds it to the
// failed set so RenderBlock won't re-spawn a fetch goroutine.
// Returns true if the key was being tracked here.
func (r *Renderer) MarkFailed(key string) bool {
	tracked := false
	if _, ok := r.fetching[key]; ok {
		delete(r.fetching, key)
		tracked = true
	}
	r.failed[key] = struct{}{}
	return tracked
}

// ResetFailed clears the failure set. Hosts call this on channel /
// thread switch so the user can retry.
func (r *Renderer) ResetFailed() {
	for k := range r.failed {
		delete(r.failed, k)
	}
	for k := range r.fetching {
		delete(r.fetching, k)
	}
}

// BlockResult bundles RenderBlock's return values.
type BlockResult struct {
	Lines     []string
	Flushes   []func(io.Writer) error
	SixelRows map[int]SixelEntry
	Height    int
	Hit       Hit
}

// RenderBlock returns the rendered rows + per-frame flushes + sixel
// sentinel rows + the image-hit footprint for one attachment. channel
// + ts identify the originating message for ImageReadyMsg routing
// when a fetch completes.
//
// Behavior matrix (matches the prior renderAttachmentBlock exactly):
//   - Non-image, ProtoOff, missing fetcher, missing FileID, or no
//     usable thumb -> single-line legacy "[Image|File] <url>" form.
//   - Cached bytes -> render via active protocol.
//   - Not cached -> reserved-height placeholder + async prefetch.
func (r *Renderer) RenderBlock(att messages.Attachment, channel, ts string, availWidth, baseRow, attIdx, contentColBase int) BlockResult {
	// MOVE THE BODY OF (*messages.Model).renderAttachmentBlock VERBATIM HERE,
	// substituting:
	//   m.imgCtx           -> r.ctx
	//   m.fetchingImages   -> r.fetching
	//   m.failedImages     -> r.failed
	//   entryHit{...}      -> Hit{...} (same fields, exported names)
	//   sixelEntry{...}    -> SixelEntry{...} (same fields, exported names)
	//   m.channelName      -> channel (passed in as a parameter)
	//   renderSingleAttachment(att) -> messages.RenderSingleAttachment(att)
	//     — see Step 2 for exporting that helper.
	//
	// The ImageReadyMsg / ImageFailedMsg constructors switch from the
	// old messages.* types to the local imgrender.* types.
	panic("TODO: copy body in Step 3")
}
```

- [ ] **Step 2: Export `renderSingleAttachment` from the messages package**

In `internal/ui/messages/render.go`, find `func renderSingleAttachment(...)`. Rename it to `RenderSingleAttachment` (capital R). If there are internal callers, run `rg -n "renderSingleAttachment"` and update each call.

- [ ] **Step 3: Copy the renderAttachmentBlock body into `RenderBlock`**

Read `internal/ui/messages/model.go:1593-1733`. Paste the body into the `RenderBlock` method, applying the substitutions listed in Step 1.

- [ ] **Step 4: Build to confirm `imgrender` compiles**

Run: `go build ./internal/ui/imgrender/`

Expected: clean. (The messages package still has its own `renderAttachmentBlock` — that's fine, this commit will be amended at the end of the task.)

- [ ] **Step 5: Switch `messages.Model` to embed `*imgrender.Renderer`**

In `internal/ui/messages/model.go`:

5a. Remove the `imgCtx ImageContext`, `fetchingImages map[string]struct{}`, `failedImages map[string]struct{}` fields from the `Model` struct (around lines 319, 330, 348). Replace with:

```go
// imgRenderer owns the inline-image rendering state. Configured at
// startup via Model.SetImageContext (which forwards into the
// Renderer).
imgRenderer *imgrender.Renderer
```

5b. Find `func (m *Model) SetImageContext(ctx ImageContext)` (around line 1003) and replace with:

```go
func (m *Model) SetImageContext(ctx imgrender.ImageContext) {
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	m.imgRenderer.SetContext(ctx)
}
```

5c. Initialize `imgRenderer` in `New(...)`. Find the constructor and add:

```go
m.imgRenderer = imgrender.NewRenderer()
```

before the return.

5d. Delete `func (m *Model) renderAttachmentBlock(...)` (lines 1593-1733). Delete `func computeImageTarget(...)` (lines 1740-1780). Delete `func buildPlaceholder(...)` (lines 1797-1839). Delete the `ImageContext`, `ImageReadyMsg`, `ImageFailedMsg`, `sixelEntry`, and `entryHit` type definitions (search for `type ImageContext struct`, etc.). Note: `OpenImagePreviewMsg` STAYS in the messages package — it's preview-overlay-specific.

5e. Update every call site that referenced the deleted symbols. The major one is `renderMessagePlain` (around line 1488-1512) — replace `m.renderAttachmentBlock(att, msg.TS, contentWidth, rowCursor, attIdx, contentColBase)` with:

```go
res := m.imgRenderer.RenderBlock(att, m.channelName, msg.TS, contentWidth, rowCursor, attIdx, contentColBase)
// Convert imgrender.Hit -> entryHit / imgrender.SixelEntry -> sixelEntry
// for the messages-pane's per-entry cache structures, which keep
// these types private to avoid widening their APIs.
hit := entryHit{
	rowStartInEntry: res.Hit.RowStartInEntry,
	rowEndInEntry:   res.Hit.RowEndInEntry,
	colStart:        res.Hit.ColStart,
	colEnd:          res.Hit.ColEnd,
	fileID:          res.Hit.FileID,
	attIdx:          res.Hit.AttIdx,
}
sxlMap := make(map[int]sixelEntry, len(res.SixelRows))
for k, v := range res.SixelRows {
	sxlMap[k] = sixelEntry{bytes: v.Bytes, fallback: v.Fallback, height: v.Height}
}
return res.Lines, res.Flushes, sxlMap, res.Height, hit
```

(The exact return tuple shape depends on `renderMessagePlain`'s existing inner structure; adapt to match.)

5f. Update `HandleImageReady` and any code that touched `m.fetchingImages` / `m.failedImages` directly. The new equivalent is `m.imgRenderer.ClearFetching(key)` and `m.imgRenderer.MarkFailed(key)`. Run `rg -n "fetchingImages|failedImages" internal/ui/messages/` to find all sites; each site becomes one of those method calls.

5g. Update `Reset` / `SetChannel` paths that previously did `m.fetchingImages = nil` / `m.failedImages = nil`. Replace with `m.imgRenderer.ResetFailed()`.

- [ ] **Step 6: Update `internal/ui/app.go`**

In `internal/ui/app.go`:

- Line 1300, change `case messages.ImageReadyMsg:` to `case imgrender.ImageReadyMsg:`.
- Line 1310, change `case messages.ImageFailedMsg:` to `case imgrender.ImageFailedMsg:`.
- Line 3599, change `func (a *App) SetImageContext(ctx messages.ImageContext)` to `func (a *App) SetImageContext(ctx imgrender.ImageContext)`.
- Add `"github.com/gammons/slk/internal/ui/imgrender"` to the imports.

- [ ] **Step 7: Update `cmd/slk/main.go`**

In `cmd/slk/main.go` lines 389-403, change `messages.ImageContext` to `imgrender.ImageContext`. Add `"github.com/gammons/slk/internal/ui/imgrender"` to the imports. If `messages` is no longer imported anywhere else in this file, remove that import.

- [ ] **Step 8: Update `internal/ui/app_imagepreview_test.go`**

Line 145 (and any sibling tests in this file) — change `messages.ImageContext` to `imgrender.ImageContext`. Add the import.

- [ ] **Step 9: Update tests inside the messages package**

Run: `rg -n "messages\.ImageContext|messages\.ImageReadyMsg|messages\.ImageFailedMsg|m\.fetchingImages|m\.failedImages" internal/ui/messages/`

For each match in a `_test.go` file, update to use `imgrender.ImageContext` / `imgrender.ImageReadyMsg` / `imgrender.ImageFailedMsg`, and replace direct map mutations with `m.imgRenderer.MarkFailed(...)` / `m.imgRenderer.ClearFetching(...)`.

- [ ] **Step 10: Move the relevant unit tests for `RenderBlock` into the imgrender package**

Run: `rg -ln "renderAttachmentBlock|TestRender.*Attachment|TestPlaceholder|TestComputeImageTarget" internal/ui/messages/`

For each test that exercises `renderAttachmentBlock` directly (or only the helpers that have already moved): copy the test into `internal/ui/imgrender/imgrender_test.go` with `package imgrender`. Update the body to construct an `imgrender.Renderer` (e.g. `r := imgrender.NewRenderer(); r.SetContext(...); res := r.RenderBlock(...)`). Delete the originals from the messages package.

Tests that exercise messages-pane state beyond image rendering (e.g. cache invalidation of sibling entries in `TestModel_HandleImageReady_PerEntryInvalidation`) STAY in the messages package — they need a `messages.Model`. Update them to use `imgrender.ImageReadyMsg` etc. but otherwise keep their structure.

- [ ] **Step 11: Build**

Run: `go build ./...`

Expected: clean. If you see "undefined" errors, that's a missed call site — fix and re-run.

- [ ] **Step 12: Run the full test suite**

Run: `go test ./...`

Expected: PASS for every package. Tests that previously verified messages-pane image rendering still pass via either their own messages-pane Model setup (with `m.SetImageContext(imgrender.ImageContext{...})`) or via the moved imgrender unit tests.

- [ ] **Step 13: Commit**

```bash
git add -A
git commit -m "Extract inline-image rendering into internal/ui/imgrender"
```

---

## Task 8: Wire `imgrender.Renderer` into the thread panel

**Files:**
- Modify: `internal/ui/thread/model.go` (add `imgRenderer *imgrender.Renderer` field, `SetImageContext`, change `renderThreadMessage` to loop over attachments via `RenderBlock`, thread per-block flushes/sixel through cache + View)
- Test: `internal/ui/thread/model_test.go`

This is the user-visible win: thread images render inline. v1 is render-only — click-to-preview from a thread is out of scope and the returned `Hit` is discarded.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/thread/model_test.go`:

```go
// TestThreadRendersInlineImagePlaceholder asserts that when a reply
// has an image attachment and the renderer's ImageContext has a
// non-Off Protocol with a fetcher, the thread panel emits a
// reserved-height placeholder block (instead of the legacy single-
// line "[Image] <url>" text). Uses a stub fetcher whose Cached()
// returns false (cache miss) so the placeholder path runs.
func TestThreadRendersInlineImagePlaceholder(t *testing.T) {
	styles.Apply("dark", config.Theme{})

	m := New(60, 20)
	parent := messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice", Text: "parent"}
	reply := messages.MessageItem{
		TS:     "1.001",
		UserID: "U2",
		UserName: "bob",
		Text:   "look at this",
		Attachments: []messages.Attachment{{
			Kind:    "image",
			Name:    "screenshot.png",
			FileID:  "F123",
			Thumbs:  []messages.Thumb{{URL: "https://example.com/t.png", Width: 320, Height: 240}},
		}},
	}
	m.SetParent(parent)
	m.SetReplies([]messages.MessageItem{reply})

	// Wire an imgrender context with a stub Cached-miss fetcher.
	m.SetImageContext(imgrender.ImageContext{
		Protocol:   imgpkg.ProtoHalfblock,
		Fetcher:    stubFetcher{cached: false}, // adapt to the real Fetcher interface
		CellPixels: image.Pt(8, 16),
		MaxRows:    20,
	})

	out := ansi.Strip(m.View())

	// Placeholder text contains the loading hint.
	if !strings.Contains(out, "Loading") {
		t.Fatalf("expected reserved-height placeholder for unfetched image, got:\n%s", out)
	}
	// The legacy text fallback prefix MUST NOT appear when inline
	// rendering is active.
	if strings.Contains(out, "[Image] https://example.com/t.png") {
		t.Fatalf("thread fell back to text rendering; should use inline placeholder")
	}
}
```

> Adapt `stubFetcher` to the existing test pattern. Look at `internal/ui/messages/model_test.go` (the test at line 309 `m.SetImageContext(ImageContext{...})`) for the existing fetcher-stubbing convention; mirror it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/thread/ -run TestThreadRendersInlineImagePlaceholder -v`

Expected: FAIL — `thread.Model` has no `SetImageContext`, and `renderThreadMessage` still emits `[Image] <url>`.

- [ ] **Step 3: Add `imgRenderer` field + setter to `thread.Model`**

In `internal/ui/thread/model.go`, find the `Model` struct definition and add:

```go
imgRenderer *imgrender.Renderer
```

Find the `New(...)` constructor and add `m.imgRenderer = imgrender.NewRenderer()` before returning.

Add the setter near the existing setters (e.g. `SetUserNames`, `SetReplies`):

```go
// SetImageContext configures the inline-image rendering pipeline for
// the thread panel. Mirrors messages.Model.SetImageContext; in v1 the
// thread renders images inline but does not support click-to-preview.
func (m *Model) SetImageContext(ctx imgrender.ImageContext) {
	m.imgRenderer.SetContext(ctx)
	m.InvalidateCache()
	m.dirty()
}
```

Add the import for `"github.com/gammons/slk/internal/ui/imgrender"`.

- [ ] **Step 4: Replace `messages.RenderAttachments` with a per-attachment `RenderBlock` loop**

In `renderThreadMessage` (lines 1143-1209), replace the attachment block at lines 1203-1206:

```go
var attachmentLines string
if rendered := messages.RenderAttachments(msg.Attachments); rendered != "" {
	attachmentLines = "\n" + messages.WordWrap(rendered, contentWidth)
}
```

with:

```go
var attachmentLines string
if len(msg.Attachments) > 0 {
	var attachBlocks []string
	for attIdx, att := range msg.Attachments {
		// channel "" because the thread already routes
		// ImageReadyMsg by TS; a missing channel name is
		// harmless. baseRow / contentColBase are 0 because v1
		// discards the Hit (click-to-preview from threads is
		// out of scope).
		res := m.imgRenderer.RenderBlock(att, "", msg.TS, contentWidth, 0, attIdx, 0)
		attachBlocks = append(attachBlocks, strings.Join(res.Lines, "\n"))
		// v1: discard res.Hit, res.Flushes, res.SixelRows.
		// Per-frame flushes / sixel are addressed in Step 5
		// once the cache structure is updated.
	}
	attachmentLines = "\n" + strings.Join(attachBlocks, "\n")
}
```

> Note: this Step deliberately drops `res.Flushes` and `res.SixelRows`. On terminals that need kitty escape callbacks or sixel sentinels, images render from cache on the second frame instead of the first; Step 5 fixes this.

- [ ] **Step 5: Wire per-block flushes + sixel sentinels through the thread cache**

5a. In the thread `viewEntry` struct (search `type viewEntry struct` around line 18-50 of `internal/ui/thread/model.go`), add fields mirroring the messages package's `flushes` and `sixelRows`:

```go
flushes   []func(io.Writer) error
sixelRows map[int]imgrender.SixelEntry
```

5b. In `renderThreadMessage`, change the signature to also return collected flushes / sixel rows. (Or — simpler — keep the signature and have the function side-effect them onto a struct passed in; pick whichever fits the existing code style.)

Concretely: have `renderThreadMessage` return `(content string, flushes []func(io.Writer) error, sixelRows map[int]imgrender.SixelEntry)` and update the cache-build loop (around line 1000) to capture those values:

```go
rendered, attachFlushes, attachSixel := m.renderThreadMessage(reply, width, m.userNames, m.channelNames, i == m.selected)
// ... existing filling/border work ...
m.cache = append(m.cache, viewEntry{
	linesNormal:      linesN,
	linesSelected:    linesS,
	linesPlain:       messages.PlainLines(filledNormal),
	height:           len(linesN),
	replyIdx:         i,
	contentColOffset: 1,
	flushes:          attachFlushes,
	sixelRows:        attachSixel,
})
```

5c. In the View() path (the section that flattens visible entries — search for the loop that emits `e.linesNormal` / `e.linesSelected` around line 1083 onward), aggregate `e.flushes` and `e.sixelRows` for visible entries the same way `messages.Model.View` does (see `messages/model.go:1433` and surrounding code). The aggregated flushes are returned to the caller via the existing thread-pane output channel; if the thread's View signature does not currently expose flushes, mirror the messages-pane signature and update `app.go`'s thread-output assembly accordingly.

> Step 5c is the largest sub-step of this task. Use `messages/model.go:1430-1480` and the surrounding View() code as the reference for how aggregation is wired. Everything you copy from there has a direct analog in the thread cache.

- [ ] **Step 6: Run the new test**

Run: `go test ./internal/ui/thread/ -run TestThreadRendersInlineImagePlaceholder -v`

Expected: PASS.

- [ ] **Step 7: Run the full thread test suite**

Run: `go test ./internal/ui/thread/ -v`

Expected: PASS. Existing tests that relied on `[Image] <url>` text rendering may need updating — if they expected the legacy text and don't pre-set an `imgrender.ImageContext`, the rendered output now still falls through to `RenderSingleAttachment` (because `ctx.Protocol == ProtoOff` is the zero value and `RenderBlock` falls back to text). They should keep passing as-is.

- [ ] **Step 8: Build**

Run: `go build ./...`

Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/thread/model.go internal/ui/thread/model_test.go
git commit -m "Render inline images in the thread side panel"
```

---

## Task 9: App-level wiring for `SetImageContext` and `ImageReadyMsg` routing to the thread

**Files:**
- Modify: `internal/ui/app.go` (`SetImageContext`, `ImageReadyMsg` / `ImageFailedMsg` cases, thread-cache invalidation)
- Test: `internal/ui/app_thread_image_test.go` (new file) — round-trip test that an `ImageReadyMsg` for a thread reply triggers a thread cache invalidation.

- [ ] **Step 1: Write the failing test**

Create `internal/ui/app_thread_image_test.go`:

```go
package ui

import (
	"testing"

	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
)

// TestImageReadyMsg_RoutesToThread asserts that an ImageReadyMsg
// matching a thread reply's TS triggers the thread panel's cache
// invalidation, not just the messages pane's. Without forwarding,
// thread images stay as placeholders forever even after the bytes
// arrive.
func TestImageReadyMsg_RoutesToThread(t *testing.T) {
	app := newTestApp(t) // adapt to existing test factory
	app.threadpane.SetParent(messages.MessageItem{TS: "1.0", UserID: "U1", UserName: "alice"})
	app.threadpane.SetReplies([]messages.MessageItem{
		{TS: "1.001", UserID: "U2", UserName: "bob", Attachments: []messages.Attachment{
			{Kind: "image", FileID: "F999", Name: "x.png"},
		}},
	})

	versionBefore := app.threadpane.Version()

	app.Update(imgrender.ImageReadyMsg{Channel: "C1", TS: "1.001", Key: "F999-720"})

	if app.threadpane.Version() == versionBefore {
		t.Fatal("ImageReadyMsg for a thread reply did not invalidate the thread cache (Version did not bump)")
	}
}
```

> Adapt `newTestApp` to whatever the existing app-level test helpers are. If none exist, the closest reference is `internal/ui/app_imagepreview_test.go`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ui/ -run TestImageReadyMsg_RoutesToThread -v`

Expected: FAIL — `app.Update(ImageReadyMsg{...})` only invalidates the messages pane.

- [ ] **Step 3: Forward `SetImageContext` to the thread panel**

In `internal/ui/app.go` find `func (a *App) SetImageContext(ctx imgrender.ImageContext)` (the version updated in Task 7). Add a second forward:

```go
func (a *App) SetImageContext(ctx imgrender.ImageContext) {
	a.messagepane.SetImageContext(ctx)
	a.threadpane.SetImageContext(ctx)
}
```

- [ ] **Step 4: Forward `ImageReadyMsg` / `ImageFailedMsg` to the thread**

In the Update loop, around line 1300, change:

```go
case imgrender.ImageReadyMsg:
	a.messagepane.HandleImageReady(msg.Channel, msg.TS, msg.Key)
```

to:

```go
case imgrender.ImageReadyMsg:
	a.messagepane.HandleImageReady(msg.Channel, msg.TS, msg.Key)
	// Thread panel: v1 uses coarse invalidation. If any reply in the
	// open thread has a matching TS, blow the thread cache so
	// renderThreadMessage runs again with the now-cached image bytes.
	if a.threadpane.HasReply(msg.TS) {
		a.threadpane.InvalidateCache()
	}
```

And around line 1310:

```go
case imgrender.ImageFailedMsg:
	// existing messages-pane handling
	// ... plus:
	a.threadpane.HandleImageFailed(msg.Key)
```

- [ ] **Step 5: Implement `HasReply` and `HandleImageFailed` on `thread.Model`**

In `internal/ui/thread/model.go`, add:

```go
// HasReply returns true when the open thread contains a reply with the
// given TS. App.Update uses this to decide whether to invalidate the
// thread cache on ImageReadyMsg.
func (m *Model) HasReply(ts string) bool {
	if m.replyIDToIdx == nil {
		return false
	}
	_, ok := m.replyIDToIdx[ts]
	return ok
}

// HandleImageFailed clears the in-flight bit for key on the thread's
// renderer. Called from App.Update when an ImageFailedMsg lands so the
// failure set stays in sync between the two panels.
func (m *Model) HandleImageFailed(key string) {
	if m.imgRenderer == nil {
		return
	}
	m.imgRenderer.MarkFailed(key)
}
```

- [ ] **Step 6: Run the new test**

Run: `go test ./internal/ui/ -run TestImageReadyMsg_RoutesToThread -v`

Expected: PASS.

- [ ] **Step 7: Run the full app test suite**

Run: `go test ./internal/ui/...`

Expected: PASS.

- [ ] **Step 8: Build**

Run: `go build ./...`

Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/app.go internal/ui/thread/model.go internal/ui/app_thread_image_test.go
git commit -m "Route image ready/failed messages to thread panel"
```

---

## Task 10: README update

**Files:**
- Modify: `README.md` (image-rendering caveat in the Threads / Image rendering caveats sections)

- [ ] **Step 1: Update the threads-side-panel caveat**

In `README.md`, find the line:

```markdown
- Threads side panel renders attachments as text (`[Image] <url>`); inline rendering there is on the roadmap.
```

(Around line 151.) Replace with:

```markdown
- Threads side panel renders images inline using the same pipeline as the main messages pane. Click-to-preview and `O` / `v` from a thread reply are still messages-pane only.
```

- [ ] **Step 2: Verify no other README sections claim threads are text-only**

Run: `rg -i "thread.*image|inline.*thread" README.md`

Update any stale claims to match the new behavior.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "README: document inline image rendering in thread panel"
```

---

## Final verification

- [ ] **Step 1: Run the full project test suite once more**

Run: `go test ./...`

Expected: PASS for every package.

- [ ] **Step 2: Build the binary**

Run: `make build`

Expected: clean build at `bin/slk`.

- [ ] **Step 3: Smoke test (manual, optional)**

Run `bin/slk` against a workspace with a recent image attachment. Verify:

1. The compose box has a tinted-green background when in insert mode.
2. The selected message in the channel has a tinted-green background plus the bright `▌` border.
3. Selecting a thread reply that has an image attachment renders the image inline (or shows the `⏳ Loading` placeholder until bytes arrive).
4. Clicking an image in the messages pane still opens the preview overlay (regression check).
5. Clicking an image in the thread panel does **not** open the preview overlay (v1 non-goal).

---

## Plan self-review

**Spec coverage:**
- §1 Compose-box insert tint — Tasks 1, 2.
- §2 Selected-message tint (messages, thread, threadsview) — Tasks 3, 4, 5.
- §3 Thread inline images — Tasks 6, 7, 8, 9.
- §4 Tint helper — Task 1.
- Testing & rollout — covered per-task plus the Final Verification block.
- Out-of-scope items (click-to-preview from threads, Block Kit in threads, config keys) are explicitly NOT in any task.

**Type consistency:**
- `imgrender.Renderer` / `NewRenderer` / `SetContext` / `RenderBlock` / `ClearFetching` / `MarkFailed` / `ResetFailed` — same names every place they appear.
- `imgrender.ImageContext` / `ImageReadyMsg` / `ImageFailedMsg` / `Hit` / `SixelEntry` / `BlockResult` — same.
- `styles.ComposeInsertBG`, `styles.SelectionTintColor(focused bool)`, `styles.SelectionBorderColor(focused bool)`, `styles.mixColors`, `styles.defaultTintAlpha` — same.
- `thread.Model.SetImageContext`, `.HasReply`, `.HandleImageFailed`, `.imgRenderer` — same.
- `messages.RenderSingleAttachment` (renamed from `renderSingleAttachment`) — used by `imgrender.RenderBlock`.

**Ambiguity check:**
- Task 7 Step 5e shows the conversion between `imgrender.Hit`/`SixelEntry` and the messages-pane's private `entryHit`/`sixelEntry`. Both messages-pane internal types stay private; the conversion is at the call boundary.
- Task 8 Step 5c (per-frame flushes / sixel through the thread View) is the only step that points at "follow the messages-pane reference" rather than spelling out the full code. This is intentional — the messages-pane code at `model.go:1430-1480` is the canonical source and would otherwise be duplicated verbatim. The implementer should read that block and adapt.

**Open known approximations:**
- Test helpers (`newTestApp`, `stubFetcher`, `newModelWithSummaries`) reference patterns from existing test files. The implementer is expected to read the existing tests and use the matching factory / stub. This is preferable to inventing new helpers that diverge from the rest of the test suite.
