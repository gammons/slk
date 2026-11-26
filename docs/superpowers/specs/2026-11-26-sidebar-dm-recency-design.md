# Sidebar DM recency ordering

## Problem

Slack's sidebar orders Direct Messages by recent activity. slk sorted every
section alphabetically, so an old DM could sit above the one the user just
read, and the order did not match the web client.

## Ordering

`[sidebar] sort_dms_by_recency` defaults on. Only `dm` and `group_dm` rows in
the same section are reordered:

1. unread rows first;
2. then by `ReadState.LastReadTS`, newest first;
3. rows with no read state last;
4. name as the final tiebreaker.

Channels, private channels and apps keep their alphabetical (or configured
`ChannelOrder`) order. Explicit `ChannelOrder` still wins for every row, so
config sections that pin a position are unaffected.

Read state comes from the map `rebuildFilter` already fetches; `sortFiltered`
now takes that map so a read mark can reorder DMs without a second DB read.

A section can mix DMs with channels or apps (a custom section, or Starred).
Sorting DMs inside the main comparator would then be intransitive: the name
comparisons against the non-DM row contradict the recency comparison between
two DMs. Instead the section is sorted alphabetically first, then
`sortDMsInSections` sorts only the unpinned DM slots by recency and writes them
back into those same slots. Pinned DMs (`ChannelOrder > 0`) keep their existing
order.

## Header control

The Direct Messages section header carries a right-aligned control: `⇅` when
ordered by recency, `A-Z` when alphabetical. Clicking those cells toggles the
order for the session; clicking the rest of the header still collapses the
section, matching Enter. `renderSectionHeaderLabel` reserves the control's
cells before fitting the name and records their row columns in
`renderRow.sortStart` / `sortEnd`; `HitTestSectionSort` answers the click and
`reducer_mouse.go` checks it before `ClickAt`.

## Configuration

`SidebarStyle.SortDMsByRecency` carries `[sidebar] sort_dms_by_recency` from
config to `sidebar.Model.SetSortDMsByRecency`. The default is on, matching
Slack; setting the option to `false` gives the old alphabetical order. The
runtime control is not persisted.

## Tests

- `internal/ui/sidebar/dm_recency_test.go`: recent-first order, unread first,
  alphabetical when off, channels unaffected, DMs reordered around a non-DM
  row in a mixed section, pinned DMs unchanged, the rendered control, its hit
  box, toggling, and a mention pill taking the click instead.
- `internal/ui/sidebar_dm_sort_test.go`: a real frame click on the control
  flips recency to alphabetical, including the sidebar/rail x offset.
- `internal/config/config_test.go`: the default and the explicit `false`.
