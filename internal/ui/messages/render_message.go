package messages

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
)

// RenderContext is what rendering a message reads besides the message.
// Each pane builds one from its own fields when it rebuilds its cache.
type RenderContext struct {
	ColoredUsernames bool
	UserStatuses     map[string]peerstatus.Status
	UserNames        map[string]string
	ChannelNames     map[string]string
	UserGroups       map[string]string
	Emoji            EmojiContext
	Images           *imgrender.Renderer // non-nil; the pane creates it
	ImageChannel     string              // the key image fetches and ImageReadyMsg use (P3)
	SearchTerms      []string
	Nav              ReactionNav // drawn only when RenderOptions.IsSelected
}

// RenderOptions are the per-pane differences in how a message renders:
// spec §5.1 items 9, 10 and 15, and plan decision P3.
type RenderOptions struct {
	Avatar          string // the author's avatar block; "" for none (always, in thread)
	IsSelected      bool   // the cursor is on this message (reaction-nav highlight)
	ReplyAffordance bool   // "[N replies ->]" under a parent (channel pane)
	EditedMark      bool   // "(edited)" after the timestamp (channel pane)
	BroadcastLabel  bool   // "↳ replied to a thread" label (channel pane)
	ImageHits       bool   // record image and file hit rects (both panes)
	SixelRows       bool   // record sixel rows (channel pane)
	stats           *entryPerfStats
}

// RenderedMessage is one message, rendered, before the pane's border and fill.
type RenderedMessage struct {
	Content      string
	Header       string // the author line (with "(edited)"), for SelectedHeader
	HeaderBudget int
	Hits         []entrylist.Hit // image and file hits (when ImageHits), then reaction pills
	SixelRows    map[int]sixelEntry
	// Kitty uploads by source: each pane keeps its own upload order.
	BodyFlushes, BlockFlushes, ReactionFlushes, AttachmentFlushes []func(io.Writer) error
}

// BlockkitContext bundles the blockkit-package dependencies sourced
// from the pane's image context, theme, and per-message identity.
// Built per message rather than once so it picks up runtime changes to
// the image context (e.g., when image_protocol is reconfigured).
// Warm-path emoji uploads in block text are appended to emojiFlushes.
func BlockkitContext(msg MessageItem, rc *RenderContext, emojiFlushes *[]func(io.Writer) error) blockkit.Context {
	imgCtx := rc.Images.Context()
	send := imgCtx.SendMsg
	channelNames := rc.ChannelNames
	return blockkit.Context{
		Protocol:    imgCtx.Protocol,
		Fetcher:     imgCtx.Fetcher,
		KittyRender: imgCtx.KittyRender,
		CellPixels:  imgCtx.CellPixels,
		MaxRows:     imgCtx.MaxRows,
		MaxCols:     imgCtx.MaxCols,
		UserNames:   rc.UserNames,
		MessageTS:   msg.TS,
		Channel:     rc.ImageChannel,
		// Capture channelNames in a closure so blockkit's two-arg
		// RenderText signature stays stable; channel-name resolution
		// is a host concern.
		RenderText: func(s string, un map[string]string) string {
			return RenderSlackMarkdownWith(s, RenderSlackMarkdownOpts{
				UserNames:    un,
				ChannelNames: channelNames,
				UserGroups:   rc.UserGroups,
				PlaceCtx:     rc.Emoji.PlaceCtx,
				EmojiCells:   rc.Emoji.Cells,
				Customs:      rc.Emoji.Customs,
				EmojiFlushes: emojiFlushes,
			})
		},
		WrapText: WordWrap,
		SendMsg: func(v any) {
			// tea.Msg is interface{}, so any non-nil v satisfies the inner
			// send signature. The nil-guard is the only meaningful check.
			if send != nil {
				send(v)
			}
		},
	}
}

// RenderMessage renders one message for either content pane, without
// the pane's border, fill or selection tint. opt carries what the panes
// render differently; with every option off it is the thread pane's
// rendering.
//
// Row indices in SixelRows and Hits are relative to the entry's lines
// AFTER the pane wraps the content (avatar+border): the avatar gutter
// adds columns but no rows, and the border adds 1 column (col 0) but
// no rows, so row indices computed here stay valid. Column extents in
// Hits are absolute display columns within those lines, including the
// border (col 0) and the avatar gutter (5 cols when an avatar is
// present), so the pane can translate directly to pane-local mouse
// columns.
func RenderMessage(msg MessageItem, width int, rc *RenderContext, opt RenderOptions) RenderedMessage {
	var out RenderedMessage
	avatarStr := opt.Avatar
	stats := opt.stats
	line := styles.Username(msg.UserID, rc.ColoredUsernames).Render(msg.UserName) + AuthorStatusSuffix(rc.UserStatuses, msg.UserID, time.Now()) + lipgloss.NewStyle().Background(styles.Background).Render("  ") + styles.Timestamp.Render(msg.Timestamp)

	// If we have an avatar, reserve space on the left for it
	contentWidth := width - 4
	if avatarStr != "" {
		contentWidth = width - 7 // 4 cols avatar + 1 space + 2 padding
	}
	if contentWidth < 20 {
		contentWidth = 20
	}

	var bodyT0 time.Time
	if stats != nil {
		bodyT0 = time.Now()
	}
	// Emoji-image opts pass through the active EmojiContext; when
	// image mode is off (or PlaceCtx.Fetcher is nil) RenderSlackMarkdownWith
	// falls through to the legacy glyph/shortcode-text path. Warm-path
	// emoji flushes land in out.BodyFlushes.
	bodyOpts := RenderSlackMarkdownOpts{
		UserNames:    rc.UserNames,
		ChannelNames: rc.ChannelNames,
		UserGroups:   rc.UserGroups,
		PlaceCtx:     rc.Emoji.PlaceCtx,
		EmojiCells:   rc.Emoji.Cells,
		Customs:      rc.Emoji.Customs,
		EmojiFlushes: &out.BodyFlushes,
		Width:        contentWidth,
	}
	// Blocks that render the body suppress Slack's notification-fallback
	// text and its row. See BlocksCarryBody.
	hasBody := !BlocksCarryBody(msg)
	bodySrc := MessageTextSource(msg)
	if !hasBody {
		bodySrc = ""
	}
	rendered := RenderSlackMarkdownWith(bodySrc, bodyOpts)
	if len(rc.SearchTerms) > 0 {
		// SearchHighlightSGR's close sequence restores the theme bg/fg
		// after the highlight's reset so plain body text doesn't bleed
		// terminal-default colors. One derivation per RenderMessage
		// call (i.e. per cache miss), not per line.
		if hlStart, hlEnd, ok := SearchHighlightSGR(); ok {
			rendered = HighlightSearchTerms(rendered, rc.SearchTerms, hlStart, hlEnd)
		}
	}
	bodyStyle := styles.MessageText
	if msg.Subtype == "me_message" {
		// A /me line, italic as in Slack's own clients.
		bodyStyle = bodyStyle.Italic(true)
	}
	text := bodyStyle.Render(WordWrap(rendered, contentWidth))
	if stats != nil {
		stats.bodyTotal += time.Since(bodyT0)
	}

	var threadLine string
	if opt.ReplyAffordance && msg.ReplyCount > 0 {
		word := "replies"
		if msg.ReplyCount == 1 {
			word = "reply"
		}
		threadLine = "\n" + styles.ThreadIndicator.Render(
			fmt.Sprintf("[%d %s ->]", msg.ReplyCount, word))
	}

	var reactionLine string
	// reactionLineCount is the number of rendered reaction rows below
	// the body / attachments. Used downstream (after the row layout is
	// known) to translate per-pill specs into absolute row indices.
	reactionLineCount := 0
	// pillSpecs captures one entry per real (non-"+") reaction pill in
	// rendering order, with line index and column extents relative to
	// the reaction-line block. Converted into absolute entrylist.Hit
	// rects below (once we know the row offset of the reaction block).
	type pillSpec struct {
		lineIdx  int
		colStart int
		colEnd   int
		emoji    string
	}
	var pillSpecs []pillSpec
	var reactT0 time.Time
	if stats != nil && len(msg.Reactions) > 0 {
		reactT0 = time.Now()
	}
	if len(msg.Reactions) > 0 {
		var pills []string
		// pillEmojis parallels pills; empty string marks the trailing
		// "+" pill that appears only in reaction nav mode (no toggle
		// target). Tracked separately because the index in pills can
		// exceed len(msg.Reactions).
		var pillEmojis []string
		// Image-aware emoji-as-image path is active only when the
		// process-global ImageMode is on AND a fetcher has been
		// installed via SetEmojiContext. Otherwise the legacy
		// glyph/shortcode-text branch renders.
		imageOK := emojiutil.ImageModeActive() && rc.Emoji.PlaceCtx.Fetcher != nil
		pillCells := rc.Emoji.Cells
		if pillCells <= 0 {
			pillCells = 2
		}
		for i, r := range msg.Reactions {
			// Image path: pass r.Emoji directly (including any skin-tone
			// suffix) — URLForShortcode composes the per-tone CDN URL
			// natively. Stripping skin tone was a workaround for
			// glyph-rendering width inconsistencies that no longer apply
			// at the kitty-image cell footprint.
			//
			// Legacy path: still strip the suffix because the glyph
			// renderer suffers the original width-inconsistency problem
			// (multi-codepoint ZWJ / skin-tone sequences render at
			// terminal-dependent widths and break border alignment).
			// See internal/emoji/shouldrender.go for the rule.
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
				// Legacy fallback path (image mode off, no URL, or
				// Place returned false). Strip skin tone here only —
				// the glyph renderer still needs the workaround.
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
			// Image emoji in reaction pills upload once per visible
			// frame, like body text and inline image attachments.
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
		// terminal-probed width cache so wrapping decisions match what
		// the user's terminal will actually render.
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
					pillSpecs = append(pillSpecs, pillSpec{
						lineIdx:  lineIdx,
						colStart: 0,
						colEnd:   pillW,
						emoji:    pillEmojis[i],
					})
				}
			} else {
				colStart := emojiutil.Width(currentLine) + sepWidth
				if pillEmojis[i] != "" {
					pillSpecs = append(pillSpecs, pillSpec{
						lineIdx:  lineIdx,
						colStart: colStart,
						colEnd:   colStart + pillW,
						emoji:    pillEmojis[i],
					})
				}
				currentLine = candidate
			}
		}
		if currentLine != "" {
			reactionLines = append(reactionLines, currentLine)
		}
		reactionLine = "\n" + strings.Join(reactionLines, "\n")
		reactionLineCount = len(reactionLines)
	}
	if stats != nil && len(msg.Reactions) > 0 {
		stats.reactionsTotal += time.Since(reactT0)
		stats.reactionsCount++
	}

	var editedMark string
	if opt.EditedMark && msg.IsEdited {
		editedMark = " " + styles.Timestamp.Render("(edited)")
	}

	// Pre-attachment row count, so attachment rows can compute their
	// absolute row index (used as the sixelRows key).
	//
	//   rows 0..: label rows (ephemeral, then thread_broadcast), each optional
	//   next row: username line + editedMark
	//   row N: wrapped body text (lipgloss.Height of styled `text`);
	//          absent when BlocksCarryBody
	//
	// Attachments begin immediately after the body text.
	var labelRows string
	preAttachmentRows := 0
	if msg.Ephemeral {
		labelRows += EphemeralLabel() + "\n"
		preAttachmentRows++ // the ephemeral label occupies its own row
	}
	if opt.BroadcastLabel && msg.Subtype == "thread_broadcast" {
		labelRows += styles.Timestamp.Render("\u21b3 replied to a thread") + "\n"
		preAttachmentRows++ // the broadcast label occupies its own row
	}
	preAttachmentRows++ // username + ts row
	if hasBody {
		preAttachmentRows += lipgloss.Height(text) // wrapped body text
	}

	// contentColBase is the display column at which message content
	// begins inside the cached entry's linesNormal. buildCache wraps
	// the rendered content with a thick-left-border (▌, 1 col) at
	// column 0; placeAvatarBeside (when an avatar is present) prepends
	// 4 cols of avatar + 1 col of spacing in front of every content
	// row. So the in-content column 0 lands at:
	//
	//   col 1 + 5 = 6   when an avatar is present
	//   col 1           when no avatar
	//
	// We compute it here (before calling imgRenderer.RenderBlock) and
	// pass it down so the per-image hit rects come back in absolute
	// linesNormal columns, ready for direct viewport translation in
	// View() without any further offset arithmetic.
	contentColBase := 1
	if avatarStr != "" {
		contentColBase += 5
	}

	var attachLineSlices [][]string
	var imageHits []entrylist.Hit
	allSixel := map[int]sixelEntry{}

	// Block Kit blocks render between the body text and file attachments.
	bkCtx := BlockkitContext(msg, rc, &out.BlockFlushes)
	var bkLines []string
	bkInteractive := false
	if len(msg.Blocks) > 0 {
		var bkT0 time.Time
		if stats != nil {
			bkT0 = time.Now()
		}
		startInBk := len(bkLines)
		res := blockkit.Render(msg.Blocks, bkCtx, contentWidth)
		if stats != nil {
			stats.blockKitTotal += time.Since(bkT0)
			stats.blockKitCount++
		}
		if !opt.SixelRows {
			// A pane without the sixel placement pipeline (the thread
			// pane, plan decision P3) must not keep the renderer's
			// blank placeholder rows for a block image: splice the
			// halfblock fallback into the lines so the image stays
			// visible as text instead of vanishing.
			blockkit.SpliceSixelFallbacks(res.Lines, res.SixelRows)
		}
		bkLines = append(bkLines, res.Lines...)
		out.BlockFlushes = append(out.BlockFlushes, res.Flushes...)
		if opt.SixelRows {
			rowOffset := preAttachmentRows + startInBk
			for k, v := range res.SixelRows {
				allSixel[rowOffset+k] = sixelEntry{key: v.Key, bytes: v.Bytes, fallback: v.Fallback, height: v.Height, width: v.Width, col: v.Col}
			}
		}
		// Note: res.Hits is intentionally NOT appended to out.Hits in
		// v1. App-level click routing (app.go) currently uses image
		// hits to look up file attachments by attIdx; routing for "BK-"
		// (Block Kit URL-keyed) hits is deferred. Recording them here
		// without routing would mis-route clicks to msg.Attachments[0].
		// See Phase 7 of the plan for the future wiring.
		_ = res.Hits
		bkInteractive = bkInteractive || res.Interactive
	}
	if len(msg.LegacyAttachments) > 0 {
		var lgT0 time.Time
		var lgPerf *blockkit.LegacyPerf
		legacyCtx := bkCtx
		if stats != nil {
			lgT0 = time.Now()
			lgPerf = &blockkit.LegacyPerf{}
			legacyCtx.Perf = lgPerf
		}
		startInBk := len(bkLines)
		res := blockkit.RenderLegacy(msg.LegacyAttachments, legacyCtx, contentWidth)
		if stats != nil {
			stats.legacyTotal += time.Since(lgT0)
			stats.legacyCount++
			stats.legacyTextTotal += lgPerf.TextTotal()
			stats.legacyImageTotal += lgPerf.ImageTotal()
			stats.legacyOtherTotal += lgPerf.OtherTotal()
			stats.legacyAttCount += lgPerf.AttachmentCount()
			ccTotal, ccCount := lgPerf.ImgCachedCheck()
			stats.legacyImgCachedCheckTotal += ccTotal
			stats.legacyImgCachedCheckCount += ccCount
			kTotal, kCount := lgPerf.ImgKitty()
			stats.legacyImgKittyTotal += kTotal
			stats.legacyImgKittyCount += kCount
			riTotal, riCount := lgPerf.ImgRenderImage()
			stats.legacyImgRenderImageTotal += riTotal
			stats.legacyImgRenderImageCount += riCount
			phTotal, phCount := lgPerf.ImgPlaceholder()
			stats.legacyImgPlaceholderTotal += phTotal
			stats.legacyImgPlaceholderCount += phCount
		}
		if !opt.SixelRows {
			// Same as the block branch above: keep legacy-attachment
			// images visible as halfblock text in panes that cannot
			// paint sixel placements.
			blockkit.SpliceSixelFallbacks(res.Lines, res.SixelRows)
		}
		bkLines = append(bkLines, res.Lines...)
		out.BlockFlushes = append(out.BlockFlushes, res.Flushes...)
		if opt.SixelRows {
			rowOffset := preAttachmentRows + startInBk
			for k, v := range res.SixelRows {
				allSixel[rowOffset+k] = sixelEntry{key: v.Key, bytes: v.Bytes, fallback: v.Fallback, height: v.Height, width: v.Width, col: v.Col}
			}
		}
		// Note: res.Hits is intentionally NOT appended to out.Hits in
		// v1. App-level click routing (app.go) currently uses image
		// hits to look up file attachments by attIdx; routing for "BK-"
		// (Block Kit URL-keyed) hits is deferred. Recording them here
		// without routing would mis-route clicks to msg.Attachments[0].
		// See Phase 7 of the plan for the future wiring.
		_ = res.Hits
		bkInteractive = bkInteractive || res.Interactive
	}
	if bkInteractive {
		hint := styles.Timestamp.Render(InteractiveHint(msg))
		bkLines = append(bkLines, hint)
	}
	preAttachmentRows += len(bkLines)

	bkBlock := ""
	if len(bkLines) > 0 {
		// Re-apply the background after every reset, the same treatment
		// the body text gets from styles.MessageText.
		//
		// The inline styles (bold, italic, link, mention) deliberately
		// omit .Background() and rely on an outer style providing it —
		// see the comment on boldStyle in render.go. Body text has that
		// outer style; Block Kit lines do not. They are composed from a
		// gutter prefix plus rendered content, and the prefix's closing
		// reset clears the background for everything after it on that
		// line, so styled and plain runs alike drew on the terminal's
		// default background while only the trailing pad kept the
		// theme's. On a selected message that reads as the selection
		// tint stopping at the glyphs.
		//
		// Background only, no foreground: kitty image placeholders
		// encode their image ID in the cell foreground (see
		// image.PlaceholderRune) and must not be repainted.
		//
		// Each line is also PREFIXED with the background, not just
		// patched after its own resets. The reset that strips the
		// background is not in these lines at all — it closes the
		// avatar gutter that placeAvatarBeside prepends to every line
		// afterwards, so the run at the start of the content has no
		// preceding reset here to attach a background to.
		bkBlock = "\n" + WithBackground(bkLines, BgANSI())
	}

	if len(msg.Attachments) > 0 {
		var attT0 time.Time
		if stats != nil {
			attT0 = time.Now()
		}
		rowCursor := preAttachmentRows
		for attIdx, att := range msg.Attachments {
			imgThumbs := make([]imgrender.ThumbSpec, len(att.Thumbs))
			for i, t := range att.Thumbs {
				imgThumbs[i] = imgrender.ThumbSpec{URL: t.URL, W: t.W, H: t.H}
			}
			res := rc.Images.RenderBlock(imgrender.Block{
				Kind:   att.Kind,
				FileID: att.FileID,
				Name:   att.Name,
				URL:    att.URL,
				Thumbs: imgThumbs,
			}, rc.ImageChannel, msg.TS, contentWidth, rowCursor, attIdx, contentColBase)
			if !opt.SixelRows {
				// Same treatment as the block branches above: a pane
				// without the sixel pipeline keeps the image visible
				// as halfblock text. Attachment SixelRows keys are
				// entry-relative (RenderBlock stamps them with the
				// rowCursor it was passed); shift them to be relative
				// to res.Lines, which starts at that same row. Splice
				// into a COPY: on RenderBlock's prerendered fast path
				// res.Lines shares its backing array with the
				// fetcher's cached Render, and writing through it
				// would replace the cache's blank reserved rows with
				// halfblock for every later render, including the
				// channel pane's.
				shifted := make(map[int]blockkit.SixelEntry, len(res.SixelRows))
				for k, v := range res.SixelRows {
					shifted[k-rowCursor] = blockkit.SixelEntry{
						Key: v.Key, Bytes: v.Bytes, Fallback: v.Fallback,
						Height: v.Height, Width: v.Width,
					}
				}
				lines := slices.Clone(res.Lines)
				blockkit.SpliceSixelFallbacks(lines, shifted)
				res.Lines = lines
			}
			attachLineSlices = append(attachLineSlices, res.Lines)
			out.AttachmentFlushes = append(out.AttachmentFlushes, res.Flushes...)
			if opt.SixelRows {
				for k, v := range convertSixelMap(res.SixelRows) {
					allSixel[k] = v
				}
			}
			if opt.ImageHits {
				// Only record hits for actual image-block renders (the
				// fallback-to-link path returns a zero Hit, which we
				// skip — clicking a single-line "[Image] <url>" doesn't
				// open the preview overlay; that's a hyperlink.).
				if h := res.Hit; h.RowEndInEntry > h.RowStartInEntry {
					imageHits = append(imageHits, entrylist.Hit{
						RowStart: h.RowStartInEntry, RowEnd: h.RowEndInEntry,
						ColStart: h.ColStart, ColEnd: h.ColEnd,
						Kind: entrylist.HitImage, Key: h.FileID, Aux: h.AttIdx,
					})
				} else if att.Kind == "file" && att.DownloadURL != "" && len(res.Lines) > 0 {
					// A "[File] <name>" line: clicking it downloads
					// the file, like `d`.
					imageHits = append(imageHits, entrylist.Hit{
						RowStart: rowCursor, RowEnd: rowCursor + res.Height,
						ColStart: contentColBase,
						ColEnd:   contentColBase + lipgloss.Width(res.Lines[0]),
						Kind:     entrylist.HitFile, Aux: attIdx,
					})
				}
			}
			rowCursor += res.Height
		}
		if stats != nil {
			stats.attachmentsTotal += time.Since(attT0)
			stats.attachmentsCount++
		}
	}
	var attachmentLines string
	attachmentLineCount := 0
	if len(attachLineSlices) > 0 {
		flat := make([]string, 0)
		for _, ls := range attachLineSlices {
			flat = append(flat, ls...)
		}
		attachmentLines = "\n" + strings.Join(flat, "\n")
		attachmentLineCount = len(flat)
	}

	bodyRow := ""
	if hasBody {
		bodyRow = "\n" + text
	}
	msgContent := labelRows + line + editedMark + bodyRow + bkBlock + attachmentLines + threadLine + reactionLine

	// Translate per-pill specs into entry-relative reaction hit rects.
	// reactionRowBase is the row index (within linesNormal) where the
	// first reaction line lands. Row layout:
	//   preAttachmentRows = label rows (ephemeral and/or thread_broadcast)
	//   + username + body + bk
	//   + attachmentLineCount (each attachment line is 1 row)
	//   + 1 if threadLine is present
	// (placeAvatarBeside does not change row counts.)
	out.Hits = make([]entrylist.Hit, 0, len(imageHits)+len(pillSpecs))
	out.Hits = append(out.Hits, imageHits...)
	if len(pillSpecs) > 0 && reactionLineCount > 0 {
		reactionRowBase := preAttachmentRows + attachmentLineCount
		if threadLine != "" {
			reactionRowBase++
		}
		for _, ps := range pillSpecs {
			row := reactionRowBase + ps.lineIdx
			out.Hits = append(out.Hits, entrylist.Hit{
				RowStart: row, RowEnd: row + 1,
				ColStart: contentColBase + ps.colStart,
				ColEnd:   contentColBase + ps.colEnd,
				Kind:     entrylist.HitReaction, Key: ps.emoji,
			})
		}
	}

	// Place avatar next to message content (avatar is side-by-side, no
	// extra rows; row indices for sixelRows remain valid).
	if avatarStr != "" {
		msgContent = placeAvatarBeside(avatarStr, msgContent)
	}

	if len(allSixel) == 0 {
		allSixel = nil
	} else {
		// Stamp every entry with the message's content column plus the
		// image's own column offset within the rendered lines. Block
		// kit, legacy, and attachment entries all start at
		// contentColBase; section accessories sit right of their body
		// text and legacy attachment images right of the color stripe,
		// both of which carry Col offsets from their renderers. Without
		// this the painter would place the image at the pane's
		// content-left edge (col 0) instead of its placeholder,
		// shifting it left under the avatar / border.
		for k, v := range allSixel {
			v.col = contentColBase + v.col
			allSixel[k] = v
		}
	}
	// The header row is the only row SelectedHeader may lengthen. It
	// must fit both the width the body wraps to and the columns the row
	// really has: contentWidth is floored at 20, which at narrow widths
	// exceeds width-1 (the fill) minus the avatar gutter.
	out.Content = msgContent
	out.Header = line + editedMark
	out.HeaderBudget = min(contentWidth, width-1-(contentColBase-1))
	out.SixelRows = allSixel
	return out
}
