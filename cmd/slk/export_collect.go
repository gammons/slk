// This file gathers the messages behind `slk export`. It walks a
// channel's history to find the conversations that touch the export
// window, fetches their in-window replies, converts the results to
// message items and resolves the user names they reference. It talks to
// Slack only through the small interfaces declared here.

package main

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/export"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/slack-go/slack"
)

// exportSource is the part of the Slack client the collector reads
// channel history through.
type exportSource interface {
	WalkHistory(ctx context.Context, channelID, oldest, latest string, visit func([]slack.Message) error) error
	GetRepliesBetween(ctx context.Context, channelID, threadTS, oldest, latest string) ([]slack.Message, error)
}

// exportProfileSource is the part of the Slack client that resolves a
// user ID the cache does not know.
type exportProfileSource interface {
	GetUserProfile(userID string) (*slack.User, error)
}

// rawConversation is a conversation as Slack returned it, before
// conversion to message items.
type rawConversation struct {
	Parent          slack.Message
	Replies         []slack.Message
	ParentIsContext bool
}

// parentCollector accumulates the top-level messages of the
// conversations to export. Its visit methods are WalkHistory callbacks.
type parentCollector struct {
	win     export.Window
	parents []rawConversation
}

// visitWindow keeps every top-level message inside the window.
func (p *parentCollector) visitWindow(page []slack.Message) error {
	for _, m := range page {
		if isThreadReply(m) || !p.win.Contains(m.Timestamp) {
			continue
		}
		p.parents = append(p.parents, rawConversation{Parent: m})
	}
	return nil
}

// visitOlder keeps the thread parents that predate the window but whose
// thread was still being replied to once the window opened.
func (p *parentCollector) visitOlder(page []slack.Message) error {
	for _, m := range page {
		if isThreadReply(m) || p.win.Contains(m.Timestamp) {
			continue
		}
		if repliesReachWindow(m, p.win) {
			p.parents = append(p.parents, rawConversation{Parent: m, ParentIsContext: true})
		}
	}
	return nil
}

// isThreadReply reports whether m is a reply surfaced in channel
// history (a thread broadcast). Replies are collected through their
// parent, so the history walks skip them.
func isThreadReply(m slack.Message) bool {
	return m.ThreadTimestamp != "" && m.ThreadTimestamp != m.Timestamp
}

// repliesReachWindow reports whether thread parent m may have replies
// at or after the window start, going by its latest_reply. A parent
// that reports replies but no latest_reply is kept, so the reply fetch
// decides instead of the thread being silently dropped.
func repliesReachWindow(m slack.Message, win export.Window) bool {
	if m.ReplyCount == 0 {
		return false
	}
	latest, ok := export.TimeFromTS(m.LatestReply)
	if !ok {
		return true
	}
	return !latest.Before(win.Start)
}

// collectConversations returns every conversation in channelID that has
// at least one message inside win, oldest parent first. That is each
// top-level message in the window with its in-window replies, plus each
// older thread that has in-window replies, its parent kept as context.
//
// Finding those older threads means walking all history before the
// window: Slack has no query for "threads active in this period", and
// a parent of any age can receive a reply. Progress goes to progress.
func collectConversations(ctx context.Context, src exportSource, channelID string, win export.Window, progress io.Writer) ([]rawConversation, error) {
	collector := &parentCollector{win: win}
	fmt.Fprintln(progress, "Fetching messages in range...")
	if err := src.WalkHistory(ctx, channelID, win.OldestTS(), win.LatestTS(), collector.visitWindow); err != nil {
		return nil, err
	}
	fmt.Fprintln(progress, "Scanning earlier history for threads with replies in range...")
	if err := src.WalkHistory(ctx, channelID, "", win.OldestTS(), collector.visitOlder); err != nil {
		return nil, err
	}

	threads := 0
	for _, conv := range collector.parents {
		if conv.Parent.ReplyCount > 0 {
			threads++
		}
	}
	fmt.Fprintf(progress, "Fetching replies for %d threads...\n", threads)

	var out []rawConversation
	for _, conv := range collector.parents {
		if conv.Parent.ReplyCount > 0 {
			msgs, err := src.GetRepliesBetween(ctx, channelID, conv.Parent.Timestamp, win.OldestTS(), win.LatestTS())
			if err != nil {
				return nil, err
			}
			conv.Replies = windowReplies(msgs, conv.Parent.Timestamp, win)
		}
		if conv.ParentIsContext && len(conv.Replies) == 0 {
			continue
		}
		out = append(out, conv)
	}
	slices.SortFunc(out, compareRawConversations)
	return out, nil
}

// compareRawConversations orders conversations by parent timestamp.
func compareRawConversations(a, b rawConversation) int {
	return strings.Compare(a.Parent.Timestamp, b.Parent.Timestamp)
}

// compareMessagesByTS orders Slack messages by timestamp.
func compareMessagesByTS(a, b slack.Message) int {
	return strings.Compare(a.Timestamp, b.Timestamp)
}

// windowReplies reduces a conversations.replies result to the thread's
// in-window replies, oldest first: the parent is dropped, as is anything
// outside win and any message Slack repeated across pages.
func windowReplies(msgs []slack.Message, parentTS string, win export.Window) []slack.Message {
	seen := make(map[string]bool, len(msgs))
	var replies []slack.Message
	for _, m := range msgs {
		if m.Timestamp == parentTS || seen[m.Timestamp] || !win.Contains(m.Timestamp) {
			continue
		}
		seen[m.Timestamp] = true
		replies = append(replies, m)
	}
	slices.SortFunc(replies, compareMessagesByTS)
	return replies
}

// exportConversations converts raw conversations to the export
// package's form. Author names come from names and db where known and
// otherwise stay as the raw ID until resolveExportNames fills them in.
func exportConversations(raw []rawConversation, names map[string]string, db *cache.DB) []export.Conversation {
	convs := make([]export.Conversation, 0, len(raw))
	for _, rc := range raw {
		conv := export.Conversation{
			Parent:          exportMessageItem(rc.Parent, names, db),
			ParentIsContext: rc.ParentIsContext,
		}
		for _, r := range rc.Replies {
			conv.Replies = append(conv.Replies, exportMessageItem(r, names, db))
		}
		convs = append(convs, conv)
	}
	return convs
}

// exportMessageItem converts one Slack message for export. Display
// times are left blank; export.WriteChannel stamps them in the export
// timezone.
func exportMessageItem(m slack.Message, names map[string]string, db *cache.DB) messages.MessageItem {
	authorID, userName := messageAuthor(m, names, db, nil)
	reactions := make([]messages.ReactionItem, 0, len(m.Reactions))
	for _, r := range m.Reactions {
		reactions = append(reactions, messages.ReactionItem{Emoji: r.Name, Count: r.Count, UserIDs: r.Users})
	}
	return messages.MessageItem{
		TS:                m.Timestamp,
		UserID:            authorID,
		UserName:          userName,
		Text:              m.Text,
		ThreadTS:          m.ThreadTimestamp,
		ReplyCount:        m.ReplyCount,
		Subtype:           m.SubType,
		Reactions:         reactions,
		Attachments:       extractAttachments(m.Files),
		Blocks:            extractBlocks(m.Blocks),
		LegacyAttachments: extractLegacyAttachments(m.Attachments),
	}
}

// mentionRecorder collects the user IDs a message body mentions. Its
// record method is passed to messages.FlattenMrkdwn as the user
// resolver, which reuses the renderer's mention parsing instead of
// duplicating its regex here.
type mentionRecorder struct {
	ids map[string]bool
}

// record notes id and reports it unresolved; the flattened text is
// discarded, so what it resolves to does not matter.
func (r *mentionRecorder) record(id string) (string, bool) {
	r.ids[id] = true
	return "", false
}

// unresolvedUserIDs returns, sorted, the user IDs convs need a name
// for that names lacks: authors still shown by raw ID, and mentions.
func unresolvedUserIDs(convs []export.Conversation, names map[string]string) []string {
	rec := &mentionRecorder{ids: make(map[string]bool)}
	for _, conv := range convs {
		for _, msg := range append([]messages.MessageItem{conv.Parent}, conv.Replies...) {
			if msg.UserID != "" && msg.UserName == msg.UserID {
				rec.ids[msg.UserID] = true
			}
			messages.FlattenMrkdwn(messages.MessageTextSource(msg), rec.record, nil)
		}
	}
	var ids []string
	for id := range rec.ids {
		if names[id] == "" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// resolveExportNames looks up every user ID convs still needs, adds
// the results to names, and rewrites authors that were showing a raw
// ID. The cache is consulted before Slack. An ID that cannot be
// resolved keeps its raw form and is reported on warn with the lookup's
// error, so a bot ID or deleted user reads differently from a network
// failure; only a cancelled context is an error.
func resolveExportNames(ctx context.Context, convs []export.Conversation, names map[string]string, db *cache.DB, profiles exportProfileSource, warn io.Writer) error {
	for _, id := range unresolvedUserIDs(convs, names) {
		if _, ok := resolveUserCached(id, names, db); ok {
			continue
		}
		name, err := fetchUserName(ctx, profiles, id)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(warn, "Warning: could not resolve user %s, keeping the ID: %v\n", id, err)
			continue
		}
		if name != "" {
			names[id] = name
		}
	}
	for i := range convs {
		convs[i].Parent = applyAuthorName(convs[i].Parent, names)
		for j := range convs[i].Replies {
			convs[i].Replies[j] = applyAuthorName(convs[i].Replies[j], names)
		}
	}
	return nil
}

// fetchUserName asks Slack for id's display name, waiting out rate
// limits. It returns the lookup's error when Slack cannot resolve the
// ID, and ctx's error when ctx is cancelled during a rate-limit wait.
func fetchUserName(ctx context.Context, profiles exportProfileSource, id string) (string, error) {
	for {
		u, err := profiles.GetUserProfile(id)
		if err == nil {
			return profileDisplayName(u), nil
		}
		limited, waitErr := slackclient.WaitOutRateLimit(ctx, err)
		if waitErr != nil {
			return "", waitErr
		}
		if !limited {
			return "", err
		}
	}
}

// profileDisplayName picks the name slk shows for a user: display
// name, then real name, then handle.
func profileDisplayName(u *slack.User) string {
	for _, name := range []string{u.Profile.DisplayName, u.RealName, u.Name} {
		if name != "" {
			return name
		}
	}
	return ""
}

// applyAuthorName replaces a raw-ID author name with its resolved
// name when names has one.
func applyAuthorName(msg messages.MessageItem, names map[string]string) messages.MessageItem {
	if msg.UserName == msg.UserID {
		if name := names[msg.UserID]; name != "" {
			msg.UserName = name
		}
	}
	return msg
}
