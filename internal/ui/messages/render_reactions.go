package messages

import (
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/styles"
)

// pillSpec is one real (non-"+") reaction pill in rendering order, with its
// line index and column extents relative to the reaction-line block. It is
// turned into an absolute entrylist.Hit once the row offset of the block is
// known (reactionHits).
type pillSpec struct {
	lineIdx  int
	colStart int
	colEnd   int
	emoji    string
}

// renderReactionPills renders msg's reaction pills, wrapped to contentWidth.
// It returns the block (with a leading "\n", empty when there are no
// reactions), its row count and the per-pill hit specs, and appends any
// image-emoji flushes to out.ReactionFlushes. Shared by the chat-message and
// system-notice branches of RenderMessage so both draw (and hit-test)
// reactions the same way.
func renderReactionPills(msg MessageItem, rc *RenderContext, opt RenderOptions, contentWidth int, out *RenderedMessage) (block string, lineCount int, specs []pillSpec) {
	if len(msg.Reactions) == 0 {
		return "", 0, nil
	}
	stats := opt.stats
	var reactT0 time.Time
	if stats != nil {
		reactT0 = time.Now()
	}
	var pills []string
	// pillEmojis parallels pills; empty string marks the trailing "+" pill
	// that appears only in reaction nav mode (no toggle target).
	var pillEmojis []string
	// Image-aware emoji-as-image path is active only when the process-global
	// ImageMode is on AND a fetcher has been installed via SetEmojiContext.
	// Otherwise the legacy glyph/shortcode-text branch renders.
	imageOK := emojiutil.ImageModeActive() && rc.Emoji.PlaceCtx.Fetcher != nil
	pillCells := rc.Emoji.Cells
	if pillCells <= 0 {
		pillCells = 2
	}
	for i, r := range msg.Reactions {
		// Image path: pass r.Emoji directly (including any skin-tone
		// suffix) -- URLForShortcode composes the per-tone CDN URL natively.
		//
		// Legacy path: still strip the suffix because the glyph renderer
		// suffers a width inconsistency for multi-codepoint ZWJ / skin-tone
		// sequences. See internal/emoji/shouldrender.go for the rule.
		var emojiStr string
		var placedFlush func(io.Writer) error
		if imageOK {
			if url, ok := emojiutil.URLForShortcode(r.Emoji, rc.Emoji.Customs); ok {
				if placement, flush, ok := emojiutil.Place(rc.Emoji.PlaceCtx, url, pillCells); ok {
					emojiStr = placement
					placedFlush = flush
				}
			}
		}
		if emojiStr == "" {
			legacyName := emojiutil.StripSkinTone(r.Emoji)
			resolved := emojiutil.Sprint(":" + legacyName + ":")
			if emojiutil.ShouldRenderUnicode(resolved) {
				emojiStr = resolved
			} else {
				emojiStr = ":" + legacyName + ":"
			}
		}
		var style lipgloss.Style
		if opt.IsSelected && rc.Nav.Active && i == rc.Nav.Index {
			style = styles.ReactionPillSelected
		} else if r.HasReacted {
			style = styles.ReactionPillOwn
		} else {
			style = styles.ReactionPillOther
		}
		pillText := ReactionPillText(emojiStr, r.Count, style, placedFlush != nil)
		pills = append(pills, style.Render(pillText))
		pillEmojis = append(pillEmojis, r.Emoji)
		// Image emoji in reaction pills upload once per visible frame, like
		// body text and inline image attachments.
		if placedFlush != nil {
			out.ReactionFlushes = append(out.ReactionFlushes, placedFlush)
		}
	}
	if opt.IsSelected && rc.Nav.Active {
		plusStyle := styles.ReactionPillPlus
		if rc.Nav.Index >= len(msg.Reactions) {
			plusStyle = styles.ReactionPillSelected
		}
		pills = append(pills, plusStyle.Render("+"))
		pillEmojis = append(pillEmojis, "")
	}
	// Join pills with wrapping. emojiutil.Width() consults the
	// terminal-probed width cache so wrapping decisions match what the
	// user's terminal will actually render.
	bgSpace := lipgloss.NewStyle().Background(styles.Background).Render(" ")
	var reactionLines []string
	currentLine := ""
	lineIdx := 0
	for i, pill := range pills {
		candidate := currentLine
		sepWidth := 0
		if i > 0 {
			candidate += bgSpace
			sepWidth = 1
		}
		candidate += pill
		pillW := emojiutil.Width(pill)
		if emojiutil.Width(candidate) > contentWidth && currentLine != "" {
			reactionLines = append(reactionLines, currentLine)
			lineIdx++
			currentLine = pill
			// After wrap the pill starts at col 0 of the new line.
			if pillEmojis[i] != "" {
				specs = append(specs, pillSpec{lineIdx: lineIdx, colStart: 0, colEnd: pillW, emoji: pillEmojis[i]})
			}
		} else {
			colStart := emojiutil.Width(currentLine) + sepWidth
			if pillEmojis[i] != "" {
				specs = append(specs, pillSpec{lineIdx: lineIdx, colStart: colStart, colEnd: colStart + pillW, emoji: pillEmojis[i]})
			}
			currentLine = candidate
		}
	}
	if currentLine != "" {
		reactionLines = append(reactionLines, currentLine)
	}
	if stats != nil {
		stats.reactionsTotal += time.Since(reactT0)
		stats.reactionsCount++
	}
	return "\n" + strings.Join(reactionLines, "\n"), len(reactionLines), specs
}

// reactionHits converts pill specs into entry-relative HitReaction rects.
// rowBase is the row (within the entry's lines) where the first reaction
// line lands; colBase is the display column where the block's column 0 sits.
func reactionHits(specs []pillSpec, rowBase, colBase int) []entrylist.Hit {
	hits := make([]entrylist.Hit, 0, len(specs))
	for _, ps := range specs {
		row := rowBase + ps.lineIdx
		hits = append(hits, entrylist.Hit{
			RowStart: row, RowEnd: row + 1,
			ColStart: colBase + ps.colStart,
			ColEnd:   colBase + ps.colEnd,
			Kind:     entrylist.HitReaction, Key: ps.emoji,
		})
	}
	return hits
}
