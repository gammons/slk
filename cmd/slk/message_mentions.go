package main

import (
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/slackfmt"
	"github.com/gammons/slk/internal/ui/messages"
)

// messageUserResolver uses the message's source workspace, never the workspace
// that happens to be active when a background fetch or cache read completes.
func messageUserResolver(router *workspaceRouter, teamID string) *userResolver {
	if router != nil {
		if wctx := router.ByID(teamID); wctx != nil {
			return wctx.UserResolver
		}
	}
	return nil
}

// messageChannelResolver uses the message's source workspace, never the workspace
// that happens to be active when a background fetch or cache read completes.
func messageChannelResolver(router *workspaceRouter, teamID string) *channelResolver {
	if router != nil {
		if wctx := router.ByID(teamID); wctx != nil {
			return wctx.ChannelResolver
		}
	}
	return nil
}

// resolveMessageMentions hydrates names from local caches and queues misses
// without waiting for profiles or channel lookups. Follow MessageLinks' visible
// mrkdwn lanes: hidden fallback text, plain labels and attachment rich text are
// not rendered as mentions. Store/resolver notifications refresh the UI.
func resolveMessageMentions(msg core.MessageItem, names *userNameStore, db *cache.DB, resolver *userResolver, channels *channelResolver) {
	var seenUsers, seenChannels map[string]struct{}
	resolveText := func(text string) {
		if text == "" {
			return
		}
		for _, id := range slackfmt.MentionedUserIDs(text) {
			if _, ok := seenUsers[id]; ok {
				continue
			}
			if seenUsers == nil {
				seenUsers = make(map[string]struct{})
			}
			seenUsers[id] = struct{}{}
			if _, ok := resolveUserCached(id, names, db); !ok && resolver != nil {
				resolver.Request(id)
			}
		}
		if channels == nil {
			return
		}
		for _, mention := range slackfmt.MentionedChannels(text) {
			if mention.Label != "" {
				continue
			}
			if _, ok := seenChannels[mention.ID]; ok {
				continue
			}
			if seenChannels == nil {
				seenChannels = make(map[string]struct{})
			}
			seenChannels[mention.ID] = struct{}{}
			channels.Request(mention.ID)
		}
	}
	resolveBlocks := func(bs []blocks.Block) {
		for _, b := range bs {
			switch v := b.(type) {
			case blocks.SectionBlock:
				resolveText(v.Text)
				for _, field := range v.Fields {
					resolveText(field)
				}
			case blocks.ContextBlock:
				for _, element := range v.Elements {
					resolveText(element.Text)
				}
			}
		}
	}
	if !messages.BlocksCarryBody(msg) {
		resolveText(messages.MessageTextSource(msg))
	}
	resolveBlocks(msg.Blocks)
	for _, att := range msg.LegacyAttachments {
		resolveText(att.Pretext)
		resolveText(att.Text)
		for _, field := range att.Fields {
			resolveText(field.Value)
		}
		resolveBlocks(att.Blocks)
	}
}
