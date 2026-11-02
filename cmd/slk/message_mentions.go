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

// resolveMessageMentions hydrates names from local caches and queues misses
// without waiting for profiles. Follow MessageLinks' visible mrkdwn lanes:
// hidden fallback text, plain labels and attachment rich text are not rendered
// as mentions. The existing store/resolver notifications refresh the UI.
func resolveMessageMentions(msg core.MessageItem, names *userNameStore, db *cache.DB, resolver *userResolver) {
	var seen map[string]struct{}
	resolveText := func(text string) {
		if text == "" {
			return
		}
		for _, id := range slackfmt.MentionedUserIDs(text) {
			if _, ok := seen[id]; ok {
				continue
			}
			if seen == nil {
				seen = make(map[string]struct{})
			}
			seen[id] = struct{}{}
			if _, ok := resolveUserCached(id, names, db); !ok && resolver != nil {
				resolver.Request(id)
			}
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
