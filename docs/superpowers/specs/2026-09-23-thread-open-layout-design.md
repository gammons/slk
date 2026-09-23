# Thread Open Layout Fix

**Issue:** [#244](https://github.com/gammons/slk/issues/244)

## Problem

Opening a thread from the messages pane succeeds initially, but `App.View` can immediately auto-hide it. With the standard 6-column workspace rail and 30-column sidebar, a 120-column terminal leaves 82 columns for messages and thread panes. The proportional split assigns 28 columns to the thread and rejects the layout because the thread minimum is 30, even though assigning 30 columns to the thread still leaves 48 columns for messages.

The render-time auto-hide clears `threadVisible` and moves focus back to messages. It does not clear the status bar's thread indicator, producing the reported state where Enter appears to do nothing while the status bar says `Thread`. The hidden state also causes the replies reducer to discard the in-flight fetch result.

## Design

Preserve the existing pane policy, but base auto-hide on actual fit:

1. Reserve both pane borders.
2. If the remaining content width cannot hold the 40-column messages minimum plus the 30-column thread minimum, auto-hide the thread as before.
3. Otherwise calculate the existing 35% thread width and clamp it upward to the 30-column minimum.
4. When auto-hide is unavoidable, clear the status bar's thread indicator together with visibility and focus so application state remains coherent.

This keeps wide layouts unchanged, makes the default 120-column layout usable, and moves the sidebar-visible threshold from 124 columns to the true combined-minimum boundary of 112 columns.

## Tests

- Table-test layout behavior immediately below and at the combined-minimum boundary, at the default 120-column width, and at a wide width where the proportional split remains unchanged.
- Exercise the production `Enter -> View -> ThreadRepliesLoadedMsg` path at 120 columns and assert that the panel stays visible, focused, and accepts replies.
- Exercise an actually-too-narrow terminal and assert that auto-hide clears visibility, focus, and the thread status indicator.
