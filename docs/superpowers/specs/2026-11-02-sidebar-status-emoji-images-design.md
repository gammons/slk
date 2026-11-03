# Sidebar and status emoji as images, section headers on one line

Status: approved scope, 2026-11-02. One PR for gammons/slk#344. It also
takes over the abandoned gammons/slk#315 and fixes gammons/slk#301.

This note replaces the three fork notes it was built from
(`2026-10-20-sidebar-emoji-images-design.md`,
`2026-10-20-sidebar-header-fit-design.md`,
`2026-10-20-status-emoji-images-design.md`). Only this one ships.

## Where things stand

### #344 (our proposal)

The emoji-as-images work (`2026-05-27-emoji-as-images-design.md`) left
out "channel list emoji prefixes" and "user status emoji". The fork has
them on three branches (`feat/sidebar-emoji-images`,
`fix/sidebar-header-fit`, `feat/status-emoji-images`), 10 commits on
top of the WezTerm work, which this PR does not need (#343 is separate).

### #315 (cshirley, opened 2026-10-13, stale since)

What it does, all in `renderSectionHeaderLabel`:

1. Truncates the section name with `truncate.StringWithTail` so the
   header fits `width - 2` and lipgloss does not wrap it into a row
   counted as one line.
2. Truncates an over-long emoji prefix too.
3. Strips skin tones and gates the glyph with `ShouldRenderUnicode`
   (the #301 fix).
4. For a multi-codepoint glyph, tries each ZWJ segment (VS16 removed)
   and keeps the first single-codepoint one, else `:code:` text.
5. One test: a long name at width 20 stays one line and ends in "…".

What review found (Riley for gammons, Greptile), all still open:

| Finding | Status in #315 | In this PR |
|---|---|---|
| R1: a truncated emoji prefix gets `" "` appended after it was cut to the budget, one column too wide, so the header still wraps (any long custom shortcode in a collapsed section at width 20) | bug | Not possible: the emoji prefix is never cut. It is kept whole or dropped whole (`fitSectionHeader`) |
| R2 / Greptile P1: `budget` floored at 3 lets the name overflow when the aggregates leave less | bug | No floor. A budget under 1 drops the name, then the emoji, then the aggregates |
| R3 / Greptile P2: no provider-backed tests; emoji paths untested | missing | Provider-backed tests at width 20, normal and selected variants: skin-tone code, VS16 code (#301), ZWJ code, long custom shortcode in a collapsed section with unreads and a mention badge |
| R4: a glyph prefix has two trailing spaces (`Sprint` appends one), the ZWJ path one | pre-existing | One space for every prefix (see "Text mode" below) |
| R5: the ZWJ-segment rule is a new display rule, unlike reactions and message bodies (`:mountain:` would be `⛰` here, `:mountain:` in a pill; `rainbow-flag` reads as `🏳`); #301 left the choice to gammons | open question | Not taken. Same rule as reactions: single-codepoint glyph, else `:code:` text. In image mode the question goes away: the header draws the real image |
| R6: a truncated prefix drops the name, so a long custom emoji leaves only `:partypar…` | design | The `:code:` text gives way first, the name stays |
| R7: doc comment starts "Fixed: previously…"; history belongs in the commit; conventional commit titles | style | Comment describes current behaviour; `fix(sidebar): …` titles |
| Riley: add `Fixes #301` | missing | In the PR text |

Already fixed upstream since #315: nothing in this area.
`renderSectionHeaderLabel` on `upstream/main` still has the "Known,
pre-existing, NOT introduced here" note and calls `Sprint` with no
gate.

### #301 (kirahowe)

VS16 emoji (`:hammer_and_wrench:` is `U+1F6E0 U+FE0F`, `:mountain:`)
in a section header put the row one cell off. Messages and reactions
already gate with `ShouldRenderUnicode`; the header does not. The issue
offers two fixes: fall back to `:code:` text, or drop the emoji. This
PR takes the first, which is what message bodies and reaction pills do
(`render_reactions.go`). Peer status glyphs do not gate yet (see "Left
out").

### Upstream changes the port must absorb

`upstream/main` moved the sidebar a lot since the fork branches
(#407 Slack style, #401 hidden unread DMs, #404/#405 mention pills, the
row indent option, `0d01095b` hedge cell for custom glyphs):

- `renderSectionHeaderLabel` returns the section's mention count too.
- `slackStyle`: the header glyph is `" "` when expanded, and the header
  style is `Bold(false)`. The bold fix below must not add bold there.
- DM rows: the name budget now also pays for the indent and a custom
  `private_glyph`; the status emoji's charge is unchanged, so the image
  path still swaps `1 + GlyphWidth` for `1 + cells`.
- The cursor row can be a pill (`pillRow`) when the theme sets one.
  Headers do not use it (`buildCache` renders them through
  `headerStyle.Width(width - 2)`).
- Author headers now go through `WordWrap` (`render_message.go`), and
  `wrapLine` reflows an overlong line with `strings.Fields`, which eats
  runs of spaces. That matters for cold image reservations (see "DM
  header" below).

## Scope

| Surface | Before | After |
|---|---|---|
| Section header emoji, image mode | glyph; custom emoji and skin tones as `:code:` | image (standard, custom, aliases, skin tones); glyph, then `:code:` as fallback |
| Section header emoji, text mode | any resolved glyph, VS16 and ZWJ included (#301) | single-codepoint glyph, else `:code:` text; skin tone stripped first, as reaction pills do |
| Section header longer than the sidebar | wraps into a row counted as one line | fits one line; see "Fit" |
| Selected section header | bold lost after the cursor | bold kept (not in Slack style, which is never bold) |
| DM row status emoji | glyph; custom status is `💬` | image in image mode (custom included); glyph as fallback |
| Message author header status, both panes | glyph | image; glyph as fallback |
| Channel finder DM rows | glyph | image; glyph as fallback |
| DM header topic (`Summary`) | glyphs | huddle and status emoji as images; glyph as fallback |

With image mode off (or no fetcher), every surface except the section
header is byte-identical to today; the goldens prove it. The section
header changes in text mode only for headers that do not fit, that are
selected, or whose emoji is not a single codepoint, plus the one-space
prefix.

## Design

### Shared pieces

- `emoji.PlaceShortcode(ctx, name, customs, cells)`: `URLForShortcode`
  then `Place`, gated on image mode and a fetcher. Every new surface
  uses it.
- `peerstatus.Status.GlyphShortcode(now)` / `StatusShortcode(now)`: the
  shortcode the glyph stands for (`headphones`, the status emoji,
  `speech_balloon` for text only). `SummaryWith(now, layout, emoji)` is
  `Summary` with each emoji drawn by a callback; `Summary` calls it with
  the glyph.
- `bubbles/ansi.ForegroundSGR(color)`: re-asserts a foreground after a
  placement, whose trailing `\x1b[39m` `ReapplyAfterResets` does not
  catch. It replaces the inline expression in
  `messages.ReactionPillText`. Own refactor commit.
- Measure, then place: a budget is charged `1 + cells` for an image,
  the text is truncated, and the placement is appended after. No lossy
  truncation or wrap may split a placement or drop the spaces a cold
  one reserves. (Measuring a finished row that holds one is fine:
  lipgloss counts a placement as its cells.)
- Uploads fire during `View` against `imgpkg.KittyOutput`. Each surface
  drops its cache on `HandleEmojiImageReady` (the App's debounced
  emoji tick) and `SetEmojiCustoms` (`App.SetCustomEmoji`). The sidebar
  rebuilds on the tick only while it holds a cold reservation: any
  surface's fetch ends there, and a warm image cannot change until new
  custom emoji arrive.

### Text mode section emoji (#301)

```
name := StripSkinTone(code)
glyph := CodeMap()[":"+name+":"]
prefix := glyph + " "      if ShouldRenderUnicode(glyph)
prefix := ":" + name + ": " otherwise (unknown, VS16, ZWJ, flags)
```

The `:code:` text is the stripped name, as the reaction pill's
`legacyName` is: shorter in a tight header, and the tone has no glyph
to show in text anyway. One trailing space for every prefix (R4), as
its own commit so it can be dropped if gammons prefers the old
spacing.

### Fit

For the widths the sidebar allows (`SetWidth` clamps to 20 and up; at
5 even the cursor and triangle do not fit), the header label has
`width - 4` columns (`Width(width-2)` minus
`SectionHeader`'s `Padding(0, 1)`). Both variants share one budget,
taken from the selected one:

| Part | Charged |
|---|---|
| cursor `▌` / space | 2 (`▌` is East Asian Ambiguous, hedged as channel rows do) |
| triangle and its space | 2 |
| image | `cells + 1` |
| glyph or `:code:` text | measured |
| aggregates (`•N`, mention badge) | measured, never cut |
| name | the rest |

"Measured" is `max(lipgloss.Width, emoji.Width)`. When it does not fit,
in order: drop a `:code:` text prefix if the name does not fit whole
next to it; truncate the name per grapheme cluster with "…"; drop the
name; drop the emoji; drop the aggregates. The triangle always stays.
A header that fits is built exactly as before.

Before measuring, a section's display name has tabs and line breaks
turned into spaces (display only, the section key is untouched).
lipgloss expands a tab to four cells after we measured it as one, and
a newline makes two lines whatever the budget: at width 20,
`"A\tBBBBBBBBBBB"` measures 12 and renders 16, and wraps.

The guarantee is "one line by lipgloss's count, with the `▌` hedge".
It is not a promise about every ambiguous-width character a name may
hold; the channel rows make the same trade.

`truncate.StringWithTail` (what #315 used) measures with runewidth and
can split a cluster, so the sidebar keeps its small unexported
`truncateHeaderName` and `headerCells` (already in AGENTS.md as the
shared-later candidate).

### Bold

The selected variant's reapply payload is `bgAnsi + "\x1b[1m"`, as the
channel rows pass bold in theirs, unless `slackStyle`. Not on the
normal variant: it would also land inside the mention badge padding.

### Images on each surface

As in the fork notes, unchanged in substance: the sidebar collects
flushes in `buildCache` and fires them in `View`; author headers put
the flush in `RenderedMessage.BodyFlushes`; the finder fires inline
(value receiver, no cache); the DM header keeps the `peerstatus.Status`
through `messages.Model.SetDMTopic` and builds the topic in `View`.

Two changes from the fork:

- **Thread parent.** `thread.Model.View` used to discard the parent's
  flushes, so a custom status only the parent shows would stay blank.
  Upstream (#418) now fires them every frame, so this PR needs no change
  there.
- **DM header wrap.** A long status text makes the topic wrap, and
  `wrapLine`'s `strings.Fields` drops a cold reservation's spaces, so
  the text moves (or the header changes height) when the image lands.
  The topic is wrapped with a stand-in for each image, a run of
  `U+10EEED` as wide as the image (same private plane as the kitty
  placeholder, so it measures the same), and the stand-ins are replaced
  by the placements or the cold spaces after the wrap.
- **Author header wrap.** Upstream now wraps an author header that does
  not fit. A status image wraps as its own unit, like the timestamp,
  instead of with the name's prose, so a cold reservation survives. A
  glyph reflows with the name as before, so text mode is unchanged. The
  header takes `authorStatusEmoji`, the half of upstream's
  `AuthorStatusSuffix` (#418) that returns the bare emoji and whether
  it is an image; its upload goes through the body's collector.

## Commits

Fixes first, so they stand without the feature:

1. `fix(sidebar): fit section headers on one line` (name truncation,
   give-way order, tab and newline normalized; text-only tests and
   helpers; #315's long-name test, adapted; credit in the PR text)
2. `fix(sidebar): keep the selected section header bold`
3. `fix(sidebar): draw a section emoji as a glyph only when it is one codepoint` (#301)
4. `fix(sidebar): one space after a section emoji` (R4, droppable;
   commit 3 keeps today's two spaces so this is a real, separate change)
5. `refactor(ansi): ForegroundSGR for the foreground after an image placement`
6. `feat(sidebar): section header and DM status emoji as images` (and
   the 1-cell fallback glyph charge, see below)
7. `feat(messages): author status emoji as an image` (with the thread
   parent's uploads and the header wrap; the merge with upstream main
   replaced its helper and parent uploads with #418's)
8. `feat(channelfinder): DM status emoji as an image`
9. `feat(messages): DM header status emoji as images` (with the topic
   wrap)
10. `docs(spec): sidebar and status emoji as images` (this note)

## Left out, and why

- WezTerm emulated placement: #343, separate PR. Without it, WezTerm
  falls back to glyphs exactly as message bodies do today.
- #315's ZWJ-segment rule (R5): a new display rule gammons has not
  chosen. Image mode shows the real emoji anyway.
- Dropping the emoji instead of `:code:` text for #301: possible, but
  the shortcode is consistent with reactions and messages, and it gives
  way to the name when space is short.
- The DM row's 5-cell name floor (`maxNameLen := max(budget-indent, 5)`)
  can overflow the row at width 20 with DND, a status emoji and a
  `99+` badge. Pre-existing and the same with a glyph; the image does
  not change the charge at the default 2 cells. Follow-up issue.
  (Fixed here, since the feature touches it: with `emoji_cells = 1`, a
  status glyph the row falls back to was charged 1 cell by
  `GlyphWidth`, which goes through `emoji.Width`; it is now charged the
  wider of that and lipgloss's count.)
- The same VS16 problem in a DM row's status glyph in text mode (a
  `:spiral_calendar_pad:` status is `U+1F5D3 U+FE0F`):
  `peerstatus.shortcodeGlyph` does not gate. Fixing it changes every
  status surface's text mode and has no issue yet; follow-up.
- Raw emoji in DM display names: inside the truncated name budget, needs
  token-aware truncation. Follow-up.
- Profile dialog status line, the DND `🔕` marker: not asked for; the
  marker is slk's, not a user's emoji.

## Tests

From the fork branches, ported, plus:

- The `:code:` fallback text checked at a generous width (it shows),
  then at width 20 (it gives way to the name), so the fallback is
  actually exercised.
- A section name with a tab and with a newline stays one line.
- A thread whose parent alone shows a custom status: placement and
  upload. A DM header with a long status text: same height cold and
  warm.
- Provider-backed headers at width 20, normal and selected, badges 1 /
  99 / 99+, image cells 1 and 2, failed fetch: skin-tone
  code, VS16 code (no `U+FE0F` in the output, `:code:` instead), ZWJ
  code, long custom shortcode in a collapsed section with unreads and a
  mention badge (R1's case). Every line at most `width`, line count
  equal to `height`.
- Slack style: selected header not bold; expanded glyph `" "` charged
  the same.
- Goldens expected unchanged: their sidebar uses config sections
  ("Channels", "DMs"), so no header has an emoji, and none selects a
  header. If one does change, it is re-blessed only where the header
  changed on purpose.

## Terminals

kitty and Ghostty resolve placeholder cells natively. Under a modal,
`overlay.DimmedOverlay` on `upstream/main` already blanks placeholder
cells of the dimmed background (`internal/ui/overlay/overlay.go`), so a
sidebar image is hidden, not drawn over the dim; this does not depend
on the WezTerm work.
