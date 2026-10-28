# Sidebar DM pane

Status: implemented (opt-in, `[sidebar] dm_pane = true`).

## Problem

With many sections, Direct Messages sit somewhere in the middle of one long sidebar list, often scrolled out of view. Unread DMs are what people most want to see at a glance.

## Behaviour

- `[sidebar] dm_pane = true` (default `false`) draws the Direct Messages section in its own bordered box at the bottom of the sidebar column, always visible. Everything else stays in the box above, unchanged.
- The DM section is the config-mode "Direct Messages" section, or the Slack-mode section whose type is `direct_messages`. DMs a `[sections.*]` glob or a star puts elsewhere stay where they are.
- The DM box fits its rows: at least 3, at most two fifths of the column. Below 8 usable rows the sidebar stays one box.
- The DM box is a focus stop of its own: Tab / `l` cycle channel box → DM box → messages → thread, and Shift+Tab / `h` go back. Each box remembers the row it last held. `j`/`k`, `gg`/`G` and page keys stay inside the box holding the cursor. The DM section is the last section, so `a`/`A` (next/previous unread) see DMs last. The box holding the cursor gets the focused border.
- Each box scrolls on its own: the wheel acts on the box under the pointer, PgUp/PgDn and ctrl+u/d on the box holding the cursor, and selection snapping stays inside that box.
- Clicks in the DM box select like any sidebar row.

## Design

One `sidebar.Model`, one `nav` and one `cacheRows`. Nothing outside the sidebar render path changes how it talks to the sidebar.

- `modelOrderedSections` moves the DM section key (`dmSectionKey`) to the end when `dmPane` is set. Filtering, nav and `buildCache` all use that one ordering.
- `buildCache` records `dmStart`, the first DM row, and drops the blank separator before it.
- `View`'s viewport logic became `window(lo, hi, off, height)`. `View` calls it over every row; `ViewSplit(mainHeight, dmHeight, width)` calls it over `[0, dmStart)` with `yOffset` and over `[dmStart, len)` with a separate `dmYOffset`. A `split` flag records which one rendered last, so `ClickAt` / `pageBy` only treat DM rows apart when they are drawn apart.
- New entry points: `SetDMPane`, `DMPaneRows`, `CursorInDMPane`, `ClickAtDM`, `ScrollDMUp/ScrollDMDown`. `pageBy` and `nearestNavigable` take region bounds.
- `rebuildNav` records `dmNavStart`, the DM header's nav index. With the split drawn (`SplitActive`), `navBounds` limits `MoveUp/MoveDown/GoToTop/GoToBottom` to the box holding the cursor. `FocusDMPane` / `FocusChannelPane` switch boxes, restoring each box's last row through `navIndexForKey`, which was split out of `rebuildNavWithCursor`. `App.FocusNext/FocusPrev` call them, so the DM box is a stop between the sidebar's channel box and the messages pane.
- App: `sidebarSplit(width, contentHeight)` computes both heights. `renderSidebar` and the mouse reducer both use it, so drawing and hit-testing agree. The column still comes out exactly `ContentHeight` rows.

## Tests

- `internal/ui/sidebar/dm_pane_test.go`: off by default, split contents, movement bounded to a box, focus switching that remembers each box, no DM focus without the split drawn, `ClickAtDM` vs `ClickAt`, independent DM scrolling.
- `internal/ui/sidebar_dm_pane_test.go`: two boxes filling the column, the Tab / Shift+Tab cycle through the DM box, a click in the DM box opening the DM.
- `internal/config/config_test.go`: `TestConfig_SidebarDMPane`.
- Goldens are unchanged (option off).

## Not done

- No per-workspace override or configurable DM box height.
- Starred DMs stay in Starred (the top box).
