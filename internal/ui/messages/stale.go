package messages

import (
	"strconv"
	"strings"
	"time"

	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// partialNameLimit is how many changed ids are still cheaper to
// re-render one by one. More than that — a workspace switch replaces
// the whole map — rebuilds the pane.
const partialNameLimit = 8

func changedStringKeys(old, next map[string]string) []string {
	var out []string
	for k, v := range next {
		if old[k] != v {
			out = append(out, k)
		}
	}
	for k := range old {
		if _, ok := next[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

func changedStatusKeys(old, next map[string]peerstatus.Status) []string {
	var out []string
	for k, v := range next {
		if old[k] != v {
			out = append(out, k)
		}
	}
	for k := range old {
		if _, ok := next[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// markWhere queues every message match reports for partialRebuild.
// It reports whether any message matched, including when the cache is
// already nil (nothing to queue; the next full build sees the new state).
// A matched message with no TS cannot be addressed, so the whole cache
// is dropped instead — partialRebuild finds rows by TS.
func (m *Model) markWhere(reason string, match func(MessageItem) bool) bool {
	found := false
	for i := range m.messages {
		if !match(m.messages[i]) {
			continue
		}
		found = true
		if m.messages[i].TS == "" {
			m.dropCache(reason)
			return true
		}
		m.markStale(m.messages[i].TS)
	}
	return found
}

func (m *Model) markAuthors(reason string, ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return m.markWhere(reason, func(msg MessageItem) bool {
		_, ok := set[msg.UserID]
		return ok
	})
}

// messageShowsUser reports whether a rendered row displays userID: as
// its author, a huddle participant, or an <@id> mention in a lane the
// pane draws.
func messageShowsUser(msg MessageItem, userID string) bool {
	if userID == "" {
		return false
	}
	if msg.UserID == userID || HuddleStarter(msg) == userID {
		return true
	}
	if msg.HuddleRoom != nil {
		for _, id := range msg.HuddleRoom.Participants {
			if id == userID {
				return true
			}
		}
	}
	needle := "<@" + userID
	found := false
	walkMessageText(msg, func(text string) {
		if !found && strings.Contains(text, needle) {
			found = true
		}
	})
	return found
}

func messageMentionsChannel(msg MessageItem, channelID string) bool {
	if channelID == "" {
		return false
	}
	needle := "<#" + channelID
	found := false
	walkMessageText(msg, func(text string) {
		if !found && strings.Contains(text, needle) {
			found = true
		}
	})
	return found
}

// messageUsesEmojiURL reports whether msg would draw url as an emoji
// image: a :shortcode: in its text, a reaction, or the author's status.
func messageUsesEmojiURL(msg MessageItem, statuses map[string]peerstatus.Status, customs map[string]string, url string) bool {
	if url == "" {
		return false
	}
	glyph, haveGlyph := glyphFromStandardEmojiURL(url)
	hit := false
	check := func(name string) {
		if hit || name == "" {
			return
		}
		name = strings.Trim(name, ":")
		if u, ok := emojiutil.URLForShortcode(name, customs); ok && u == url {
			hit = true
		}
	}
	walkMessageText(msg, func(text string) {
		if hit || text == "" {
			return
		}
		for _, name := range emojiutil.UsedInText(text, customs) {
			check(name)
		}
		if haveGlyph && strings.Contains(text, glyph) {
			hit = true
		}
	})
	for _, r := range msg.Reactions {
		check(r.Emoji)
	}
	ids := []string{msg.UserID}
	if id := HuddleStarter(msg); id != "" && id != msg.UserID {
		ids = append(ids, id)
	}
	now := time.Now()
	for _, id := range ids {
		st := statuses[id]
		check(st.Emoji)
		if st.InHuddle(now) {
			check("headphones")
		}
	}
	return hit
}

func glyphFromStandardEmojiURL(url string) (string, bool) {
	rest, ok := strings.CutPrefix(url, emojiutil.CDNBaseURL)
	if !ok || !strings.HasSuffix(rest, ".png") {
		return "", false
	}
	rest = strings.TrimSuffix(rest, ".png")
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	parts := strings.Split(rest, "-")
	runes := make([]rune, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseUint(p, 16, 32)
		if err != nil || n == 0 {
			return "", false
		}
		runes = append(runes, rune(n))
	}
	return string(runes), true
}

func walkMessageText(msg MessageItem, fn func(string)) {
	if t := MessageTextSource(msg); t != "" {
		fn(t)
	}
	walkBlocks(msg.Blocks, fn)
	for _, att := range msg.LegacyAttachments {
		for _, s := range []string{att.Pretext, att.Text, att.Title, att.Footer, att.AuthorName} {
			if s != "" {
				fn(s)
			}
		}
		for _, f := range att.Fields {
			if f.Title != "" {
				fn(f.Title)
			}
			if f.Value != "" {
				fn(f.Value)
			}
		}
		walkBlocks(att.Blocks, fn)
	}
}

func walkBlocks(bs []blockkit.Block, fn func(string)) {
	for _, b := range bs {
		switch v := b.(type) {
		case blockkit.SectionBlock:
			if v.Text != "" {
				fn(v.Text)
			}
			for _, f := range v.Fields {
				if f != "" {
					fn(f)
				}
			}
		case blockkit.HeaderBlock:
			if v.Text != "" {
				fn(v.Text)
			}
		case blockkit.ContextBlock:
			for _, el := range v.Elements {
				if el.Text != "" {
					fn(el.Text)
				}
			}
		case blockkit.RichTextBlock:
			if s := blockkit.RichTextToMrkdwn(v); s != "" {
				fn(s)
			}
		}
	}
}

// unreadLandmarkAt is the index of the first message strictly after ts,
// or -1 when no "── new ──" row would be drawn (no boundary, or every
// message is already read).
func unreadLandmarkAt(msgs []MessageItem, ts string) int {
	if ts == "" {
		return -1
	}
	for i := range msgs {
		if msgs[i].TS > ts {
			return i
		}
	}
	return -1
}
