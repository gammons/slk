# Emoji frecency everywhere — design

SLK-305. Amends the frecency parts of
`2026-04-26-reaction-picker-design.md` (which this does not rewrite).

## Problem

slk already keeps a frecency table (`frecent_emoji`: use count and last
use, scored `use_count / (1 + days since last use)`), but:

- only a reaction added from the reaction picker is recorded. Toggling a
  pill on, and emoji used in a message, are not;
- the history is used only while the picker's query is empty: the top
  ten, and nothing else. If you have no history, the box is empty;
- searching ignores it: the picker lists prefix matches alphabetically
  (and its 50-candidate scan cap could crowd them out entirely, #193),
  and the compose `:` autocomplete lists the first five prefix matches
  alphabetically.

## Behavior

**Recorded** (as Slack's canonical name, `CanonicalSlackName`):

- a reaction added from the picker (as before);
- a reaction pill toggled on, by key or click (removals are not uses);
- every known `:shortcode:` in a sent message, thread reply or upload
  caption, once per message. Unknown shortcodes (`:30:` in `12:30:45`)
  are skipped; a shortcode in a code span counts. Edits are not
  recorded.

**Ranked**, in both the reaction picker and the compose autocomplete,
by one function, `emoji.Match`:

1. used emoji whose name starts with the query, most frecent first;
2. (picker only) used emoji whose name contains it, most frecent first;
3. other names that start with the query, alphabetically;
4. (picker only) other names that contain it, alphabetically.

A used emoji counts under any alias or skin-tone spelling (recorded as
`+1`, it leads the matches for `thumbs` as `thumbsup`) and is listed once
even when several of its aliases match. The pickers list base names
only, so a skin-toned use (`+1::skin-tone-4`) ranks the base emoji.
Only emoji in the current workspace's list are offered, so a custom
emoji used in another workspace does not appear.

With an empty query the picker shows the history first and then
everything else alphabetically (capped at 50 rows, scrollable), instead
of only the history.

The 30 most frecent emoji are ranked (`frecentEmojiLimit`). The history
is reloaded when the picker opens and after every recorded use, and
handed to both composers.

## Not changed

- The table, its score and its scope: it stays global, not per
  workspace (custom emoji from another workspace are filtered out at
  display time instead). No migration.
- The `core.ReactionService` port (`LoadFrecent` / `RecordFrecent`).
  Recording and loading stay synchronous on the Update goroutine, as
  they were for the picker: one small SQLite statement each.

## Code

- `internal/emoji/frecency.go`: `Frecency`, `NewFrecency`, `Match`,
  `UsedInText`.
- `internal/ui/emoji_frecency.go`: `recordEmojiUse`, `recordEmojiInText`,
  `refreshEmojiFrecency`.
- `reactionpicker.Model.filter` and `emojipicker.Model.filter` call
  `emoji.Match`; `emojipicker.Model.SetFrecency` /
  `compose.Model.SetEmojiFrecency` carry the history to the composers.
