# Emoji (and kitty images) on WezTerm: emulated unicode placeholders

Status: implemented (issue #343). The design below is
the one that was reviewed; "Implementation notes" at the end lists
where the code differs and why.

## Problem

PR #49 (merged) renders every emoji, including workspace custom emoji, as a
PNG through the kitty graphics protocol with **unicode placeholders**: the
image is uploaded with a virtual placement (`a=T,U=1,c,r`,
`internal/image/kitty.go` `emitKittyUpload`) and each cell is a U+10EEEE
rune whose truecolor foreground is the image ID and whose diacritics are the
row and column (`buildPlaceholderLines`). The terminal itself turns those
cells into pixels, so the UI never needs to know where on screen an image
ends up.

Image mode is on only when the final protocol is `ProtoKitty`
(`cmd/slk/main.go`, the two `emojiwidth.SetImageMode` calls). `Detect`
(`internal/image/capability.go`) sends `TERM_PROGRAM=WezTerm` to sixel,
because WezTerm has no unicode placeholders. So on WezTerm today:

- multi-codepoint emoji (VS16 `❤️`, ZWJ, flags, skin tones) render as
  `:name:` text (`internal/emoji/shouldrender.go`);
- workspace custom emoji always render as `:name:` text;
- avatars are half-block; inline images are sixel through the frame painter.

## What WezTerm actually does

Tested on WezTerm nightly `20261010-083911-e2a98e1b` with throwaway probe
scripts and window screenshots, and read in WezTerm's source at the same
commit. "Probe" means seen on screen, "source" means read in the code.

| Behaviour | Result | Evidence |
|---|---|---|
| Unicode placeholders (`U=1` + U+10EEEE) | Not supported. `U` is an unknown key and is silently ignored, so slk's `a=T,U=1` upload is a normal display at the cursor, and U+10EEEE prints as tofu. | probe T5; source `wezterm-escape-parser/src/apc.rs:626-645` |
| Upstream status | PR wezterm/wezterm#7924 adds placeholders. Open, no reviews. Issue #986 still has it unchecked. The `a=q` reply is the same either way, so placeholder support cannot be probed. | source / GitHub |
| `a=t` then `a=p,i,c=2,r=1,C=1` at a CUP position | Works: exact 2x1 cells, alpha over the cell background, hides the glyph underneath, cursor stays put. `x,y,w,h` source crop works. | probe T7, T9; source `image.rs:121-155` |
| `a=p,i=N` without `p=` | Removes every other placement of N, so only one copy per image ID can be on screen. Probe 1 saw one exception (T8 survived T9 after an SU moved it), which is moot under the always-set-`p` invariant. | probe 2; source `kitty.rs:103-105, 318-330` |
| `a=p,i=N,p=P` | Replaces only placement (N,P). Many copies of one image work with distinct `p`. | probe Q1, Q2 |
| Text or ECH over a placement **with** `p=` | Image **stays**, drawn over the new text. | probe S1, S2; source `wezterm-surface/src/line/vecstorage.rs:23-34` |
| EL / ED over a placement | Image removed. | probe S3, S4, Q5 |
| `a=d,d=i,i=N,p=P` | Deletes exactly that placement, leaves the cell background alone. `d=i` without `p` deletes all placements of N and keeps the data. | probe R1, R3, S6 |
| Re-transmit `a=t,i=N` over live placements | Live placements survive and keep the old pixels. | probe R4, R5 |
| slk's current upload prefix `a=d,d=I` | Wipes every live placement of that ID. | probe R6 |
| Hard scroll (SU in a scroll region) | Images are attached to cells and move with their line. | probe T8; source |
| iTerm2 OSC 1337 `width=2;height=1;preserveAspectRatio=0;doNotMoveCursor=1` | Works; no IDs, repeats fine, cleared by any overwrite. Re-sends the PNG every paint. | probe Q6-Q8; source |
| Kitty query | Answers `OK`. No reply at all when `enable_kitty_graphics=false` (default true on nightly). Images are refused when the cell pixel size is 0 (mux, some ssh/tmux setups). | source |
| Throughput | 372 2x1 placements, every row rewritten and every slot re-placed at 30 fps (22 KB/frame): drawn correctly, writer never blocked, WezTerm GUI at ~20% of one core vs ~10% for the same text without images. | stress probe |
| Dedup | Identical image bytes share one GPU atlas sprite (SHA-256 keyed). Kitty PNGs decode on the parser thread, once per upload. | source |

The kitty side of WezTerm is therefore good enough. The one thing missing
is the part that lets the terminal find the images by itself.

## Options considered

1. **Flip WezTerm to `ProtoKitty`.** Broken: stray images at the cursor and
   tofu where the emoji should be (probe T5).
2. **Emoji as images, inline images stay on sixel.** The fetcher prerenders
   for one protocol (`Fetcher.ConfigurePrerender`, `maybePrerender`) and
   `emoji.Place` looks up `ProtoKitty`, so this needs two image pipelines in
   one process or a second prerender path. It also brings back the risk of
   issue #50 (prerender misses looping). Rejected.
3. **iTerm2 OSC 1337 per paint.** Works without IDs but sends the full PNG on
   every repaint, and scrolling repaints every slot. Fine for a few emoji,
   wasteful for inline images. Kept as the follow-up for iTerm2, not for
   WezTerm.
4. **Sixel per emoji.** Kilobytes per emoji per repaint and 6-pixel band
   height quantization. Rejected.
5. **Better glyphs only.** Can never show custom emoji. Rejected as the main
   fix. (Widths still matter for text, see below.)
6. **Emulate unicode placeholders inside slk.** Chosen. Details below.

## Decision

Run slk on WezTerm in kitty mode exactly as on kitty and Ghostty, so every
surface (messages, threads, reactions, pickers, avatars, inline images,
preview) keeps emitting placeholder cells unchanged. Add a final step that
does in slk what the terminal would have done: find the placeholder cells
in the finished frame, turn them into spaces, and paint real kitty
placements over them through the existing frame-correlated painter.

kitty and Ghostty keep native placeholders and see no change.

## Design

### 1. Mode flag, not a new protocol

`Protocol` stays `ProtoKitty`. A separate process-global flag (same pattern
as `emoji.SetImageMode`) records the placement mode:

```go
type KittyPlacement int
const (
    KittyPlaceholders KittyPlacement = iota // terminal resolves U+10EEEE (kitty, Ghostty)
    KittyEmulated                           // slk resolves them (WezTerm)
)
```

Every existing `== ProtoKitty` check (`main.go` emoji image mode, avatar
cache, `messages/model.go` prerender config, the fetcher) stays as it is.
Only the upload (section 3) and the frame exit (sections 4 and 5) look at
the flag. A new `Protocol` value would force every one of those sites to
change and is how issue #50 happened.

### 2. Detection

`Detect` returns `ProtoKitty` + `KittyEmulated` for `TERM_PROGRAM=WezTerm`
when:

- not under tmux or zellij (see section 8);
- `image_protocol` is `auto` or `kitty`;
- the existing kitty probe in `main.go` answers.

If the probe fails (stable WezTerm with `enable_kitty_graphics=false`), the
chain already in `main.go` takes over: kitty probe fails, halfblock, DA1
probe, sixel. That is today's behaviour plus the probe's 200 ms. The emoji
image mode reconcile (`reconciledImageMode`) needs no change because the
protocol is still `ProtoKitty`.

Also require a *measured* cell pixel size (`CSI 16 t` or `TIOCGWINSZ`),
since WezTerm refuses images without one (and stable can crash, see Risks);
otherwise stay on the sixel/halfblock chain. The gate must read the raw
`cellPxW` / `cellPxH` atomics in `internal/image/sixel.go` (or a new
"measured" flag), not `measuredCellPixels()`: that helper substitutes the
8x16 defaults when nothing was measured, so a gate on its return value
would never fire.

Escape hatch: `[appearance] kitty_placement = "auto" | "native" |
"emulated"`, so a user on a WezTerm that merges #7924 (or any other
terminal) can pick. Name to be settled; it only matters when
`image_protocol` resolves to kitty.

### 3. Uploads

In emulated mode `emitKittyUpload` sends `a=t,f=100,t=d,i=N,q=2` (plus the
chunk continuations) and nothing else:

- no `U=1`, no `c`/`r` (there is no virtual placement to size);
- no `a=d,d=I` prefix. That prefix exists for Ghostty's virtual-placement
  sizing across processes; on WezTerm it deletes live placements the
  painter believes are on screen (probe R6). Re-transmitting replaces the
  data without touching placements (probe R4). The window is real, not
  theoretical: `Registry.Lookup` keeps returning `fresh=true` until
  `MarkUploaded`, and each `RenderKey` call mints its own `OnFlush`
  closure guarded only by its own `fired` flag, so the channel pane's and
  the thread pane's closures for the same ID can both upload.

Uploads stay on the side channel during `View()`, which reaches the
terminal before the frame write that will place them, so a placement never
arrives before its data.

The upload size should be recorded per image ID (pixel width and height,
cell columns and rows) so the resolver can compute source crops without
assuming the current cell size still matches the one at upload time (font
zoom).

### 4. The resolver (pure, in `internal/image`)

```go
// ResolvePlaceholders returns screen with every kitty placeholder cell
// replaced by a space of the same style minus the image-ID foreground,
// and one placement per visible image rectangle.
func ResolvePlaceholders(screen string, geom func(id uint32) (Geometry, bool)) (string, []SixelPlacement)
```

- Parse the frame with a real cell parser (ultraviolet or `x/ansi`), not a
  regex: lipgloss canvas and `DimmedOverlay` re-encode SGR (PR #49 hit the
  `;1` bundling bug), and the colon form of truecolor must work. The rule
  is parity with kitty: decode what kitty would decode from the same
  bytes, so accept the ID as `38;2;R;G;B` and `38;5;N`, with other
  attributes in the same SGR. Read row and column from the diacritics
  (`kittyDiacritics` gives the inverse table).
- Group cells of one ID into rectangles whose row and column indices are
  contiguous, and break a run when the column index restarts: two adjacent
  `:+1:` share one image ID and decode as columns 0,1,0,1, which is two
  2x1 images, not one 4x1. A 2x1 emoji is one rectangle; an inline image
  scrolled half out of the pane is one rectangle starting at row index k,
  which becomes a source crop. An emoji cut to one cell by truncation
  becomes a 1x1 crop of its left half. Set a crop only for partial
  rectangles.
- Replace only each placeholder grapheme's bytes with one space (both are
  one cell wide) and keep every SGR, so layout and widths stay identical
  and the background under a transparent emoji (reaction pill, selection
  tint) is still drawn.
- Return the row hashes the guards need from the same pass (section 5).
- Skip lines without U+10EEEE with a byte search. `finalizeSixelView` runs
  even when the screen memo hits (`app.go`, the composite memo before it),
  so cache the resolver's result (clean screen, placements, row hashes)
  next to `lastScreen` and reuse it while the screen string is unchanged.

The resolver runs in `finalizeSixelView` (renamed to cover both protocols),
the single exit point of every `App.View` path. It has to run there, before
bubbletea sees the content, because bubbletea must draw spaces, not
placeholders. Everything that composes the screen (splits, overlays, the
preview, the screen memo) already happens before this point, and
`DimmedOverlay` already blanks placeholders under a dimmed backdrop and
splices the modal box's own rows back byte for byte, so images hidden
behind a modal are not found and get deleted, while emoji inside the
reaction picker survive. Resolved placements must therefore **not** go
through the sixel gate in `collectSixelPlacements` that withholds every
placement while `overlayActive()` (`app.go`, "A modal is composited into
the frame's text"): that rule exists because sixel cannot be clipped, and
applying it here would blank the picker's emoji.

Each placement carries:

- `Key`: image ID + crop, plus the footprint, as today;
- `Bytes`: `ESC_G a=p,i=N,p=P,c=C,r=R[,x,y,w,h],C=1,q=2 ESC\`;
- `Erase`: `ESC_G a=d,d=i,i=N,p=P,q=2 ESC\` (new field, section 5);
- `P` derived from the slot, for example `row<<12 | col`, never 0. The hard
  invariant is **every placement has an explicit `p`**: a placement
  without one removes all other copies of that image (probe 2).

### 5. Painter changes (`internal/image/sixelpaint.go`, `sixelframe.go`)

The painter's model (absolute slots, plan then commit after a full write,
paint after the text diff, guard repaint when the rows under an image
change) carries over. Three changes:

1. **Erase is protocol specific.** A placement with a non-nil `Erase` is
   removed with those bytes instead of ECH. This is required, not a
   nicety: on WezTerm a `p=` image survives text and ECH (probe S1, S2), so
   only an explicit delete removes it. It also avoids ECH painting the
   pane background over reaction pills and selection tints that sit under
   emoji. Sixel keeps ECH.
2. **Re-place instead of erase + paint when the slot is unchanged.** A
   guard change means the row was rewritten. If the rewrite was EL the
   image is gone; if it was cell writes the image is still there. Sending
   the same `a=p,i,p` again is correct in both cases (it replaces (N,P)).
   (Not implemented: see Implementation notes, item 3.)
3. **Force on full redraws and hard scrolls, detected from the bytes.**
   Bubbletea enables ultraviolet's scroll optimization on Linux
   (`cursed_renderer.go:606`, `SetScrollOptim`), and WezTerm moves
   cell-attached images with the scrolled lines. Today only a resize sets
   `forceSixelRepaint` (`app.go` `WindowSizeMsg`); the external editor
   return, theme changes and other full repaints are invisible to the
   painter. `FrameOutput` already holds the exact diff bytes, so it can
   force the plan when the diff contains ED (`CSI J`, `CSI 2J`), DECSTBM, SU
   or SD, IL or DL. For kitty placements, forcing means re-placing every
   wanted placement (cheap, about 50 bytes each) and deleting the ones no
   longer wanted; a moved image is moved back by its own `a=p,i,p`.

Also compute the guards once per frame: `frameGuard` splits the whole
screen per placement, which is O(placements x screen) with hundreds of
emoji. Split once, hash each row once, stamp all placements.

### 6. What else moves to kitty on WezTerm

Because the protocol is `ProtoKitty`, avatars become real pixels (today
half-block) and inline images, Block Kit images and the preview use kitty
instead of sixel. The thread pane gets real images too: under sixel it
draws only the placeholder (`internal/ui/thread/model.go`, "The thread
pane captures no sixel rows"). Bandwidth drops a lot: a sixel inline image is re-sent on
every repaint; a kitty placement is one short APC after a one-time upload.
The resolver's crop handling is what makes partially scrolled inline
images work, so it is in scope for v1, not a follow-up.

### 7. Widths

Image emoji are a fixed 2 cells of spaces after resolution, so WezTerm's
width tables no longer matter for them. That removes the alignment drift
the image work was meant to fix. Text that keeps glyphs (compose input,
`emoji_images = "off"`) still uses WezTerm's rules: `unicode_version`
defaults to 9, where VS16 does not widen `❤️`. Out of scope here; worth a
separate note in the wiki (`unicode_version = 14`).

### 8. tmux

Emulated placements are cursor-positioned, and under tmux the outer
cursor is wherever tmux's last redraw left it, so they cannot be placed
reliably. Emulated mode is off under `$TMUX` and `$ZELLIJ`.

Related fix, landed separately (#342): `Detect`
used to map a tmux client termname containing `wezterm` to `ProtoKitty`
with native placeholders, which draws tofu and stray images on WezTerm
(probe T5). It only triggered with `term = "wezterm"` set in WezTerm; the
default `xterm-256color` already fell to half-block. WezTerm under tmux
now gets half-block like any unknown outer terminal.

A second existing bug, also separate: the sixel painter on WezTerm can be
fooled by ultraviolet's hard scroll. WezTerm moves sixel images with
their lines, and when two identical rows (same image, same column, same
text beside it) scroll by one, the painter's slot and guard are both
unchanged while the image has moved. Rare in practice; the byte-level
force in section 5 would fix it for both paths if it is not gated to
kitty, at the cost of re-sending sixel on every scrolled frame.

## Universal follow-ups

The resolver's output is protocol neutral: a list of absolute rectangles
with an image ID and a crop. Only the paint and erase bytes depend on the
terminal, so later:

- **iTerm2**: paint with OSC 1337 (`preserveAspectRatio=0`, cell width and
  height, the PNG from the kitty payload cache), erase with ECH. Fine for
  emoji and avatars; heavy for big inline images.
- **sixel-only terminals** (foot, Windows Terminal, xterm): possible with
  the same model, but kilobytes per emoji per repaint. Only if asked.
- **Native placeholders appear upstream** (WezTerm #7924 or others): switch
  that terminal to `KittyPlaceholders`, by version or by the config key.
  Emulated mode keeps working meanwhile because direct placement stays
  supported.
- kitty and Ghostty: nothing to do; they already work with PR #49. Probe 1
  on Ghostty 1.3.1 draws the `U=1` placeholder image correctly. Its direct
  placements follow kitty's model, not WezTerm's: they are not attached to
  cells, text and ECH do not clear them, and they stay put on a hard
  scroll. So emulated mode must never be enabled for kitty-model
  terminals; its erase and force rules assume WezTerm's cell-attached
  images.

## Risks and open questions

- WezTerm stable `20240203-110809-5046fc22` (the newest release tag) has
  everything this relies on, read in its source: per-`(i,p)` replace
  (`kitty.rs:104, 312-313`), keep-on-overwrite for `p=` images
  (`vecstorage.rs:19-23`, since 2021), `a=d,d=i` with `p` (`kitty.rs:243-258`),
  `C=1`, and exact `c`/`r` scaling (commit 500f8461, 2024-01-24). No version
  gate is needed. One difference: stable lacks the divide-by-zero guards
  added later (d6e18597, wezterm#7872), so with an unknown cell pixel size it
  can crash where the nightly only refuses the image. That makes the
  "known cell pixel size" condition in section 2 mandatory. Stable and
  nightly builds cannot be told apart from the environment (both report
  `YYYYMMDD-HHMMSS-hash`), and slk does not need to.
- Resolver cost: a full-frame cell parse on frames that contain
  placeholders. Expected small next to bubbletea's own parse of the same
  frame; measure with the existing benchmarks (`makeBenchApp`).
- Placement table growth in WezTerm: every `(N,P)` that was ever placed and
  later deleted is removed; placements wiped by EL leave a record until
  replaced. Bounded by the number of slots.
- Selection and copy: unchanged, because the panes' plain-text mirrors
  never contain placeholders; the resolver only touches the outgoing frame.
- Image store limit: WezTerm drops image data past its store quota
  (320 MB) and keeps only images that still have a placement. Emulated
  uploads are transmit-only and the painter deletes placements as images
  leave the screen, so most uploads can be pruned, and slk does not
  upload an id again after `MarkUploaded`. A pruned image would then
  draw as blank cells. Native kitty has its own quota, so this is the
  existing design's limit, newly reachable on WezTerm.
- Auto trusts `TERM_PROGRAM=WezTerm`. A terminal started from a WezTerm
  shell inherits it; kitty and Ghostty are told apart by the `TERM` and
  `TERM_PROGRAM` they set, but one that sets neither and answers the
  kitty `a=q` query would be treated as WezTerm. Asking the terminal
  (XTVERSION) would close that.

## Testing

- Resolver unit tests: emoji, two adjacent identical emoji (two
  rectangles), multi-row image fully visible, scrolled partly out at the
  top, cut on the right, image under `DimmedOverlay` (must vanish),
  reaction picker box over a dimmed backdrop (its emoji must survive),
  `38;5` and colon-form SGR, frames re-encoded by the lipgloss canvas.
- Parity test: run the existing emoji goldens through the resolver and
  assert no U+10EEEE is left and every rectangle matches the decoded
  diacritics.
- Painter tests for the three changes: `Erase` bytes used instead of ECH,
  same-slot guard change re-places without deleting, ED / SU / IL in the
  diff forces a re-place.
- `Detect` table cases: WezTerm outside and inside tmux, `image_protocol`
  overrides, the config key.
- Upload test: emulated mode emits `a=t` without `U=1` and without the
  `a=d,d=I` prefix.
- Golden: one emulated-mode frame of the standard fixture with emoji,
  reactions and an avatar.
- Manual on WezTerm: messages, reactions, thread pane, reaction picker,
  emoji autocomplete, custom emoji, skin tones, scrolling fast through an
  emoji-heavy channel, opening and closing modals, `ctrl+e` and back,
  resize, font zoom, theme switch.

The probe scripts used for the table above are in
`docs/superpowers/specs/2026-10-20-wezterm-emoji-images-probes/`. Run
them inside WezTerm with `python3 probeN.py <seconds>`.

## Implementation notes

Where the code differs from the design above, found while building and
testing it on WezTerm nightly `20261010-e2a98e1b`:

1. **Probe.** WezTerm does not answer `ProbeKittyGraphics`' `a=T,i=…`
   upload at all: it replies to a transmit only when it carries an image
   number (`I=`), and never to a successful placement
   (`kitty_img_transmit`, `kitty_img_place`). Measured: no reply on
   WezTerm, `OK` on Ghostty. The comment in `capability.go` that said
   WezTerm "answers the probe" was wrong. Emulated mode probes with a
   query instead (`ProbeKittyQuery`, `a=q`), which WezTerm answers;
   native mode keeps the old probe, so kitty and Ghostty see no change.
2. **Erase is a delete, and that is enough.** `SixelPlacement.Erase`
   carries `a=d,d=i,i=N,p=P`; probe R1 showed it removes exactly one
   placement and leaves the cell background alone, so no per-slot
   background SGR was needed.
3. **No "re-place without erase" on a guard change.** WezTerm finds a
   placement to delete or replace through the rows it recorded when the
   image was placed (`kitty_remove_placement_from_model` walks
   `info.first_row..+rows`), not by searching the screen. So the painter
   keeps its sixel order: erases go before the frame's text diff, while
   those rows are still right, paints after it. A frame whose own diff
   moves lines or clears the screen (SU, SD, IL, DL, DECSTBM, ED, an
   alternate-screen switch; `diffMovesLines`) is forced: everything is
   erased before the diff and painted again after it
   (`SixelFrame.RepaintOnScroll`). Without that, a hard scroll would
   carry an image away from the rows WezTerm recorded and no later delete
   could reach it. A write that moves lines with no frame plan applied
   (bubbletea leaving and re-entering the alternate screen around the
   external editor, or a flush whose frame was already taken) forces the
   next frame instead (`FrameOutput.missedMove`). Not detected: a
   one-line scroll of the whole screen, which ultraviolet sends as a bare
   newline or `ESC M`; slk's frames keep a fixed top row and status
   row, so it never scrolls the whole screen.
4. **Resolver.** It parses the whole frame with ultraviolet, the parser
   bubbletea lays the frame out with, using bubbletea's width method:
   grapheme widths once the terminal reports mode 2027, tracked by
   `reduceTerminalModes`, otherwise wcwidth. The cell buffer is reused
   between frames (`PlaceholderResolver`) and the App memoizes the last
   frame. A 240x60 frame holding 600 emoji takes about 0.85 ms and 200
   KB per changed frame. `go.mod` now lists ultraviolet as a direct
   dependency, at the version bubbletea already pulls in.
5. **Guards** are computed once per frame for both protocols
   (`stampFrameGuards`).
6. **Config and fallbacks.** The key is `[appearance] kitty_placement`
   (`auto`, `native`, `emulated`). Without a measured cell size,
   emulated mode is not used and auto falls back to sixel, which is what
   WezTerm got before. WezTerm under zellij also stays on sixel.
   "Measured" is `startupCellPixels` (`cmd/slk/cellpixels.go`) saying
   so: the env override, `TIOCGWINSZ`, or the terminal's answer to
   `CSI 16 t`, not the 8x16 fallback. In emulated mode it runs before
   the kitty probe, and its result is kept for image encoding, so the
   terminal is asked once.
7. **Auto and inherited environment.** Auto picks emulated for
   `TERM_PROGRAM=WezTerm` unless `TERM=xterm-kitty`: kitty started from
   a WezTerm shell inherits `TERM_PROGRAM` but sets its own `TERM`, and
   must stay native.

### Verified on WezTerm

With the demo world, kitty emulated placement and emoji served as test
images (a local harness, not committed): message bodies, two identical
emoji side by side, reaction pills on their background, the selection
tint, the thread pane (real images, where sixel drew none), the reaction
picker over a dimmed backdrop (backdrop images removed, the picker's own
kept, everything restored on close), switching channels (nothing left
behind), a window resize, a burst of 40 selection moves, and a live
message arriving. Not verified by hand: a frame that really triggers
ultraviolet's hard scroll (covered by `FrameOutput` tests), font zoom,
and real Slack emoji assets.
