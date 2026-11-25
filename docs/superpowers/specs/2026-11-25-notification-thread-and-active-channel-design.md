# Notifications: thread replies and the active channel

**Date:** 2026-11-25
**Status:** Implemented

## Goal

Two opt-in notification options:

- `notifications.on_thread_reply`: notify for replies in threads the
  user takes part in.
- `notifications.on_active_channel`: keep notifying for the channel open
  on the active workspace instead of suppressing it.

Both default to `false`, so existing configurations behave as before.

## Which threads count

A thread counts when it has an active row in `thread_subscriptions`.
Slack subscribes the user to a thread when they post in it, are
mentioned in it, or follow it explicitly, so this matches Slack's own
"threads you're in". The table is already kept current by the full
subscription sync, the live `thread_subscribed` / `thread_unsubscribed`
events and `thread_marked`; no new sync is needed.

Unfollowing a thread (a tombstoned row, `active=0`) stops its
notifications, as in Slack.

## Rules

`notify.ShouldNotify` keeps its order:

1. Own messages, DND, muted conversations: never notify.
2. The active channel on the active workspace is suppressed, unless
   `OnActiveChannel` is set. This only lifts the suppression; it does
   not add a trigger.
3. Triggers: DM, subscribed thread reply (`OnThreadReply &&
   InSubscribedThread`), mention, keyword.

`OnMessage` looks the subscription up (`(*cache.DB).IsThreadSubscribed`)
only when `on_thread_reply` is set and the message is a reply
(`threadTS != "" && threadTS != ts`). A lookup error is logged and
treated as not subscribed.
