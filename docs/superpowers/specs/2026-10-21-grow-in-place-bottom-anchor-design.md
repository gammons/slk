# A message that grows in place keeps a bottom-anchored pane at the bottom

## The bug

Send a message with a link while the channel pane shows the newest
message. A second later Slack's unfurl arrives as a `message_changed`
echo, `reduceNewMessage` routes it to `messages.Model.UpdateMessageInPlace`,
and the message grows by the preview's rows. The pane does not follow:
the preview, or the bottom of it, is under the fold, and the last row
says "-- more below --". An edit that adds lines does the same.

`UpdateMessageInPlace` applies the change (`MessageItem.ApplyChange`),
drops the render cache and bumps the version. The next `View` rebuilds
the entries, and `entrylist.Layout` snaps the window only when the
cursor moved since the last snap. It did not, so the offset stays and
the extra rows land below the window.

Before `message_changed` applied blocks and attachments (the previous
commits), an echo only replaced text, so an unfurl never changed the
height and the gap hardly showed.

The thread pane has the same gap: `thread.(*Model).UpdateMessageInPlace`
(a reply grows) and `UpdateParentInPlace` (the parent grows, which
pushes every reply down). So does its `UpdateReaction`: the first
reaction on the last reply adds a pill row under the fold.

The channel pane already handles two in-place growth paths:
`UpdateReaction` and `IncrementReplyCount` each capture "was the window
at the bottom" and the old total, rebuild the cache eagerly, advance
`YOffset` by the height delta and `MarkSnapped`. The two blocks are
copies of each other. A third and fourth copy (plus a thread version)
is what AGENTS.md says not to write.

## The fix

Move the substrate into `bubbles/entrylist`, where the scroll offset
lives, and make both panes use it:

```go
// HoldBottom keeps a window that shows the last line now on the last
// line at the next Layout, however the entries change in between. An
// owner calls it before an entry grows or shrinks in place. A ScrollUp,
// SetYOffset or ResetSnap in between cancels it; a cursor move still
// snaps after it.
func (m *Model) HoldBottom()
```

- `HoldBottom` records `hold = true` when the window is at the bottom
  as last laid out: `total > 0 && height > 0 && yOffset+height >= total`,
  the test both existing copies use. It does nothing while a re-snap is
  pending (`!hasSnapped`, after `ResetSnap`): the entries and offset
  then describe the previous content (a channel switch, say), and the
  next frame is positioned by the cursor anyway.
- `Layout` applies it first: `yOffset = max(total-height, 0)`, clear
  the hold, then the usual snap, clamp and follow. Applying it in
  `Layout` rather than `SetEntries` covers both rebuild paths (full
  `SetEntries`, the channel pane's `ReplaceEntries`) and both panes with
  one line, and uses this frame's height and total.
- `ScrollUp`, `SetYOffset` and `ResetSnap` clear the hold: an explicit
  position or a re-snap wins over "stay at the bottom". `ScrollDown`
  does not: the hold exists only for a window at the bottom, and from
  there scrolling down can only ask for the bottom.
  `Static` (the thread pane's zero-reply frame) clears it too. Only these
  public mutators clear it: the internal `setY` does not, because the
  thread pane's eager `SetEntries` goes through it before `Layout` runs,
  and `SetHeight`'s pin-on-shrink is itself a bottom pin.
- Snap state is left alone. If the cursor did not move, `Layout` does
  not snap and the pin stands. If it moved (a new reply selected, `k`),
  `Layout` snaps to it from the bottom, which is what a fresh frame
  would do.

Pinning instead of "advance by the delta" is the same thing: for a
window at the bottom, `min(yOffset+delta, newTotal-height)` is
`newTotal-height` whenever `delta > 0`. For a shrink (a preview removed
with `x`) the pin keeps the bottom where the lazy clamp would have put
it, so it is harmless and needs no special case.

Callers, each calling `m.list.HoldBottom()` before the mutation, only
when the target message is found:

- `messages.Model`: `UpdateMessageInPlace` (new), `UpdateReaction` and
  `IncrementReplyCount` (their eager rebuild and offset arithmetic are
  replaced by the call).
- `thread.Model`: `UpdateMessageInPlace`, `UpdateParentInPlace`,
  `UpdateReaction`, and the replace branches of `SwapLocalSentReply` and
  `UpsertSelfSentReply` (all new). The last two are the thread side of
  "a reply I just sent grows": the authoritative copy replaces the
  optimistic one without moving the cursor. Their channel counterparts
  re-snap to the cursor (`ResetSnap`) instead; a hold is better for a
  reply taller than the pane, where a re-snap shows its top.

One behaviour is new for the thread pane, on purpose: it has no
cursor-follows-scroll, so when the cursor is on a reply above the last
one and the last reply grows past the window, the pin can push the
cursor's reply above the window. The alternative, keeping the cursor on
screen, is the bug this note fixes. The same goes for a thread whose
cursor is on the parent: a parent that grows past a pane that showed
the whole thread now scrolls its top rows off, where before the last
reply went under the fold.

The channel pane did this for reactions and reply counts already, and
now does it for change echoes too: someone else's unfurl or edit on the
last message can pin the window and, when the cursor's message ends up
fully above it, cursor-follows-scroll moves the cursor onto the window.
Before, a change echo never moved the cursor.

Moving `UpdateReaction` and `IncrementReplyCount` from the eager pin to
the deferred one is the same for the next frame, with two differences
between the mutation and that frame (both sub-frame in practice, since
bubbletea renders after every message):

- A `ScrollUp` in between cancels the hold and scrolls from the old
  offset, so it ends `delta` rows further up than the eager version.
- The eager version rebuilt the cache, so clicks and text selection had
  live entries before the frame; now they see none until it
  (`KeepLookups(0)`, as after every other `dropCache`).

The eager version's `MarkSnapped(m.selected)` goes away with the rest
of that block. It marked a cursor that had moved but not yet been laid
out as snapped, which swallowed the move; now the move snaps after the
pin.

## Tests (first, white-box, stdlib)

- `entrylist`: a held window at the bottom ends on the last line after
  the entries grow (lazy and eager lists), also when the entry that
  grew is above the window; a window not at the bottom keeps its
  offset; `ScrollUp` / `SetYOffset` / `ResetSnap` / `Static` cancel the
  hold and `ScrollDown` does not; a pending re-snap wins; a cursor move
  after the hold still snaps to the cursor; one `Layout` spends it.
- `messages`: a pane at the bottom, `UpdateMessageInPlace` on the last
  message adds attachment rows (an unfurl), the next `View` shows the
  last row of the preview and no "-- more below --"; an edit that adds
  lines does the same; the same pane scrolled up keeps its top row; an
  unknown ts changes nothing; the existing reaction and reply-count
  behaviour is covered by a test each (none existed).
- `thread`: a thread at the bottom, a reply grows in place, the parent
  grows, a first reaction lands on the last reply, a just-sent reply is
  replaced by a taller copy (both paths): the next `View` still shows
  the bottom.

## Related reflow, deferred (recorded, not changed)

Not every way a message can change height goes through the paths
above. These are the same mechanism (rows added without a cursor move)
but a different trigger, rarer, and not what the user hit; each could
call `HoldBottom` later:

- Avatar arrival (`HandleAvatarReady`, channel pane): with no avatar
  the body is `width-4` wide, with one `width-7` (`RenderMessage`), so
  an avatar landing can rewrap the whole message.
- Emoji image arrival (`HandleEmojiImageReady`): the cold form is
  spaces, which `WordWrap` can drop at a line end, the warm form is
  placeholder cells that take columns, and a failed fetch falls back to
  the longer `:shortcode:` text. A line can gain a row.
- Name, channel-name and user-group resolution (`PatchUserName`,
  `SetUserNames`, `SetChannelNames`, `SetUserGroups`): a resolved
  mention can be longer than the raw id and rewrap a body; a status
  patch can rewrap a header.
- The unread landmark (`SetLastReadTS`) adds a decoration row.

Image arrival is not one of them: every branch of
`imgrender.RenderBlock` (placeholder, prerendered, cached kitty, cached
slow) and the Block Kit image pipeline reserve the image's target
height, so an image landing does not change the entry height.

Orderings the hold does not fully cover, none of them new:

- A height shrink at the bottom in the same frame as a cursor move:
  `SetHeight`'s pin-on-shrink already marks the new cursor snapped
  (swallowing the move) before this change; after it, the hold also pins
  and the channel pane's follow can move the cursor back.
- Older history prepended while a hold is pending: `PrependMessages`
  renumbers the cursor, so `Layout` snaps to it after the pin; when the
  last message is taller than the pane that shows its top. It takes a
  fetch of older history started at the top that lands after the user
  scrolled back to the bottom, while the last message grew past the
  pane height; without the hold the same snap happens.
- A thread with no replies draws through `Static`, which never snaps;
  the hold is a no-op there (`SetThread` resets the snap and `Static`
  never sets it) and `Static` cancels one. A parent that grows past the
  pane keeps its top row, as it did.

The thread parent renders inside the list (the first entry, built from
`parentRendered`), not in the chrome above it, so a parent that grows
grows `total` and `HoldBottom` sees it.

AGENTS.md gets a row for `HoldBottom` in the UI chrome table, next to
`entrylist`.
