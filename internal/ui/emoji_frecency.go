// internal/ui/emoji_frecency.go
//
// The user's emoji history: what gets recorded (a reaction added from
// the picker or by toggling a pill on, and the :shortcode:s of a sent
// message, thread reply or upload caption) and handing the ranking to
// every emoji picker (the reaction picker and both compose
// autocompletes), which list frecent emoji first. The history lives
// behind core.ReactionService.LoadFrecent / RecordFrecent.
package ui

import (
	"github.com/gammons/slk/internal/emoji"
)

// frecentEmojiLimit is how many of the user's most frecent emoji the
// pickers rank ahead of the rest.
const frecentEmojiLimit = 30

// refreshEmojiFrecency reads the user's emoji history and hands it to
// the reaction picker and both composers.
func (a *App) refreshEmojiFrecency() {
	entries := a.reactions.LoadFrecent(frecentEmojiLimit)
	if a.reactionPicker != nil {
		a.reactionPicker.SetFrecentEmoji(entries)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	f := emoji.NewFrecency(names)
	a.compose.SetEmojiFrecency(f)
	a.threadCompose.SetEmojiFrecency(f)
}

// recordEmojiUse adds names (Slack's canonical short names) to the
// user's emoji history and refreshes the pickers' ranking.
func (a *App) recordEmojiUse(names ...string) {
	if len(names) == 0 {
		return
	}
	for _, n := range names {
		a.reactions.RecordFrecent(n)
	}
	a.refreshEmojiFrecency()
}

// recordEmojiInText records the emoji a message the user sends uses as
// :shortcode:s.
func (a *App) recordEmojiInText(text string) {
	a.recordEmojiUse(emoji.UsedInText(text, a.emojiCustoms)...)
}
