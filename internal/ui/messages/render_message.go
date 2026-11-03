package messages

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
)

// IsSystemNoticeSubtype reports whether subtype is a system message --
// join/leave, a huddle starting (huddle_thread), a canvas update
// (tabbed_canvas_updated) -- as opposed to an authored chat message.
// Shared with cmd/slk (unread/mention/notification gating) so the
// subtype list stays in one place; both panes render it through
// RenderMessage. slackclient.IsNoticeSubtype (live delivery) must list
// the same subtypes; a test in cmd/slk keeps them in step.
func IsSystemNoticeSubtype(subtype string) bool {
	switch subtype {
	case "channel_join", "channel_leave", "group_join", "group_leave",
		"huddle_thread", "tabbed_canvas_updated":
		return true
	}
	return false
}

// CanOpenThread reports whether msg may be opened as a thread (and so
// replied to). A system notice with no replies cannot: there is nothing
// to reply to. A notice that already has replies (a huddle whose notes
// live in its thread) stays openable. Every path that opens the thread
// panel on a message goes through this.
func CanOpenThread(msg MessageItem) bool {
	return !IsSystemNoticeSubtype(msg.Subtype) || msg.ReplyCount > 0
}

// noticeFallbackText is the notice body for a system message whose text
// and blocks yield nothing readable. Only fixed labels: who started a
// huddle, or for how long, is not on this data path.
func noticeFallbackText(subtype string) string {
	switch subtype {
	case "huddle_thread":
		return "A huddle started"
	case "tabbed_canvas_updated":
		return "A canvas was updated"
	}
	return ""
}

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
	ReplyAvatar      AvatarFunc  // Slack-style reply row when non-nil
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
	Header       string // the wrapped author header (with "(edited)"), for SelectedHeader
	HeaderBudget int
	Hits         []entrylist.Hit // image and file hits (when ImageHits), then reaction pills
	SixelRows    map[int]sixelEntry
	// Kitty uploads by source: each pane keeps its own upload order.
	// BodyFlushes also carries the author-status emoji's upload.
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
	textOpts := RenderSlackMarkdownOpts{
		UserNames:    rc.UserNames,
		ChannelNames: rc.ChannelNames,
		UserGroups:   rc.UserGroups,
		PlaceCtx:     rc.Emoji.PlaceCtx,
		EmojiCells:   rc.Emoji.Cells,
		Customs:      rc.Emoji.Customs,
		EmojiFlushes: emojiFlushes,
	}
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
		// Mention resolution belongs to the host; plain control labels
		// share its emoji rendering without interpreting mrkdwn.
		RenderText: func(s string, un map[string]string) string {
			opts := textOpts
			opts.UserNames = un
			return RenderSlackMarkdownWith(s, opts)
		},
		RenderPlainText: func(s string) string {
			return renderEmojiWith(s, textOpts)
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
	// Render as a muted system notice, not a chat bubble -- Slack's
	// author for these is a system bot, indistinguishable otherwise.
	// Both panes go through here, so the thread pane matches.
	if IsSystemNoticeSubtype(msg.Subtype) {
		noticeWidth := width - 4
		if noticeWidth < 20 {
			noticeWidth = 20
		}
		body := RenderSlackMarkdownWith(MessageTextSource(msg), RenderSlackMarkdownOpts{
			UserNames:    rc.UserNames,
			ChannelNames: rc.ChannelNames,
			UserGroups:   rc.UserGroups,
			Width:        noticeWidth,
		})
		// Stripped to plain text before styling: RenderSlackMarkdownWith
		// re-applies TextPrimary after a mention/link span, and every
		// join/leave message starts with one -- left unstripped, only
		// the timestamp would actually render muted.
		// The body's own wrap is undone so the line wraps once, with the
		// timestamp, instead of breaking twice.
		// Hyperlinks (OSC 8) are kept: WrapRows reads the notice's mouse
		// link rectangles off the rendered lines.
		plain := strings.Join(strings.Fields(stripKeepingLinks(body)), " ")
		if text := huddleNoticeText(msg, rc.UserNames); text != "" {
			plain = text
		}
		if plain == "" {
			plain = noticeFallbackText(msg.Subtype)
		}
		// Channel search still matches a notice's text, so highlight it
		// the way a message body is (the timestamp is left alone).
		if len(rc.SearchTerms) > 0 {
			// The close re-asserts the notice's own style (bold, muted,
			// background), not the body's.
			if hlStart, hlEnd, ok := searchHighlightSGRRestoring(noticeRestoreSGR()); ok {
				plain = HighlightSearchTerms(plain, rc.SearchTerms, hlStart, hlEnd)
			}
		}
		// Centred and bold like a day divider (DividerStyle). The row is
		// width-1 wide: WrapRows adds the one-column border after it.
		rowWidth := width - 1
		out.Content = DividerStyle(rowWidth).Render(WordWrap(msg.Timestamp+"  "+plain, noticeWidth))
		// A huddle message is a thread parent: keep the reply count line
		// (the channel pane's affordance) so the thread stays reachable.
		if opt.ReplyAffordance && msg.ReplyCount > 0 {
			word := "replies"
			if msg.ReplyCount == 1 {
				word = "reply"
			}
			out.Content += "\n" + lipgloss.NewStyle().Background(styles.Background).
				Width(rowWidth).Align(lipgloss.Center).Render(styles.ThreadIndicator.Render(
				fmt.Sprintf("[%d %s ->]", msg.ReplyCount, word)))
		}
		// Reactions on a notice (people do react to "joined the channel"):
		// the same pills and hit rects as a chat message, left-aligned
		// under the notice, whose rows start at column 1 (after the border).
		if block, n, specs := renderReactionPills(msg, rc, opt, noticeWidth, &out); n > 0 {
			rowBase := lipgloss.Height(out.Content)
			out.Content += block
			out.Hits = reactionHits(specs, rowBase, 1)
		}
		return out
	}
	avatarStr := opt.Avatar
	stats := opt.stats

	// If we have an avatar, reserve space on the left for it
	contentWidth := width - 4
	if avatarStr != "" {
		contentWidth = width - 7 // 4 cols avatar + 1 space + 2 padding
	}
	if contentWidth < 20 {
		contentWidth = 20
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
	headerTimestamp := styles.Timestamp.Render(msg.Timestamp)
	headerGap := lipgloss.NewStyle().Background(styles.Background).Render("  ")
	// The status image's upload joins the body's (bodyOpts.EmojiFlushes):
	// both panes fire BodyFlushes for visible entries.
	headerBg := lipgloss.NewStyle().Background(styles.Background)
	statusEmoji, statusImage := authorStatusEmoji(rc.UserStatuses, msg.UserID, time.Now(), &bodyOpts)
	statusSuffix := ""
	if statusEmoji != "" {
		statusSuffix = headerBg.Render(" " + statusEmoji)
	}
	authorName := styles.Username(msg.UserID, rc.ColoredUsernames).Render(msg.UserName)
	line := authorName + statusSuffix + headerGap + headerTimestamp

	var bodyT0 time.Time
	if stats != nil {
		bodyT0 = time.Now()
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
		if rc.ReplyAvatar != nil {
			label := styles.ThreadIndicator.Italic(false).Bold(true).Render(fmt.Sprintf("%d %s", msg.ReplyCount, word))
			threadLine = "\n" + replyAvatars(msg.ReplyUsers, rc.ReplyAvatar, contentWidth-lipgloss.Width(label)) + label
		} else {
			threadLine = "\n" + styles.ThreadIndicator.Render(
				fmt.Sprintf("[%d %s ->]", msg.ReplyCount, word))
		}
	}

	// reactionLineCount is the number of rendered reaction rows below the
	// body / attachments; pillSpecs become absolute hit rects below once
	// the row layout is known.
	reactionLine, reactionLineCount, pillSpecs := renderReactionPills(msg, rc, opt, contentWidth, &out)

	var editedMark string
	if opt.EditedMark && msg.IsEdited {
		editedMark = " " + styles.Timestamp.Render("(edited)")
	}

	// Pre-attachment row count, so attachment rows can compute their
	// absolute row index (used as the sixelRows key).
	//
	//   rows 0..: label rows (ephemeral, then thread_broadcast), each optional
	//   next rows: wrapped author header + editedMark
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
	// Wrap before adding the avatar's fixed two-row gutter. Each wrapped
	// line must restore its own styles after that gutter resets the pen.
	headerBudget := max(1, min(contentWidth, width-contentColBase))
	header := line + editedMark
	if lipgloss.Width(header) > headerBudget {
		// A status glyph reflows with the name. A status image is its
		// own unit, like the timestamp: reflowing it with the name would
		// drop the spaces a cold image reserves, and the header would
		// change shape when the image lands.
		author := authorName + statusSuffix
		statusPart := ""
		if statusImage {
			author = authorName
			statusPart = headerBg.Render(statusEmoji)
		}
		author = WordWrap(author, headerBudget)
		var wrapped strings.Builder
		wrapped.WriteString(author)
		lastWidth := lipgloss.Width(author[strings.LastIndexByte(author, '\n')+1:])
		// Timestamp spaces belong to one metadata unit, not the name's prose.
		// Wrap within a unit only when it cannot fit on a header line by itself.
		for _, part := range [3]struct{ text, gap string }{
			{statusPart, headerBg.Render(" ")},
			{headerTimestamp, headerGap},
			{strings.TrimPrefix(editedMark, " "), " "},
		} {
			if part.text == "" {
				continue
			}
			partWidth, gapWidth := lipgloss.Width(part.text), lipgloss.Width(part.gap)
			if wrapped.Len() > 0 && lastWidth+gapWidth+partWidth <= headerBudget {
				wrapped.WriteString(part.gap)
				wrapped.WriteString(part.text)
				lastWidth += gapWidth + partWidth
				continue
			}
			if wrapped.Len() > 0 {
				wrapped.WriteByte('\n')
			}
			if partWidth > headerBudget {
				part.text = WordWrap(part.text, headerBudget)
				partWidth = lipgloss.Width(part.text[strings.LastIndexByte(part.text, '\n')+1:])
			}
			wrapped.WriteString(part.text)
			lastWidth = partWidth
		}
		var buf strings.Builder
		writer := lipgloss.NewWrapWriter(&buf)
		_, _ = io.WriteString(writer, wrapped.String())
		_ = writer.Close()
		header = buf.String()
	}
	preAttachmentRows += lipgloss.Height(header)

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
		// The gutter's reset clears the text colour too; an image sets its own.
		fg := FgANSI()
		lines := make([]string, len(bkLines))
		for i, l := range bkLines {
			lines[i] = fg + l
		}
		bkBlock = "\n" + WithBackground(lines, BgANSI())
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
				// skip for images — clicking a single-line "[Image]
				// <url>" doesn't open the preview overlay; that's a
				// hyperlink.). File lines are not registered here:
				// their rows can still wrap in WrapRows, so their hit
				// rects are classified from the final rows there, by
				// URL, via HitsFromSpans.
				if h := res.Hit; h.RowEndInEntry > h.RowStartInEntry {
					imageHits = append(imageHits, entrylist.Hit{
						RowStart: h.RowStartInEntry, RowEnd: h.RowEndInEntry,
						ColStart: h.ColStart, ColEnd: h.ColEnd,
						Kind: entrylist.HitImage, Key: h.FileID, Aux: h.AttIdx,
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
	msgContent := labelRows + header + bodyRow + bkBlock + attachmentLines + threadLine + reactionLine

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
		out.Hits = append(out.Hits, reactionHits(pillSpecs, reactionRowBase, contentColBase)...)
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
	// Timestamp substitution may lengthen a header line, but must not
	// introduce another row after the avatar and hit rects are composed.
	out.Content = msgContent
	out.Header = header
	out.HeaderBudget = headerBudget
	out.SixelRows = allSixel
	return out
}

// maxReplyAvatars caps the repliers drawn on a reply row.
const maxReplyAvatars = 5

// replyAvatars draws up to maxReplyAvatars repliers' small avatars, each
// followed by a space, as Slack does before "N replies", as many as fit
// in room cells: the reply row must stay one row. Repliers whose avatar
// isn't ready are skipped.
func replyAvatars(userIDs []string, avatar AvatarFunc, room int) string {
	if avatar == nil {
		return ""
	}
	gap := lipgloss.NewStyle().Background(styles.Background).Render(" ")
	var b strings.Builder
	drawn, used := 0, 0
	for _, id := range userIDs {
		if drawn == maxReplyAvatars {
			break
		}
		av := avatar(id)
		if av == "" || strings.Contains(av, "\n") {
			continue
		}
		w := lipgloss.Width(av) + 1
		if used+w > room {
			break
		}
		b.WriteString(av + gap)
		used += w
		drawn++
	}
	return b.String()
}

// stripKeepingLinks is ansi.Strip that leaves OSC 8 hyperlink markers in
// place, so a link in a stripped body stays a clickable span.
func stripKeepingLinks(s string) string {
	parts := strings.Split(s, osc8Open)
	var b strings.Builder
	b.WriteString(ansi.Strip(parts[0]))
	for _, p := range parts[1:] {
		st := strings.Index(p, "\x1b\\")
		if st < 0 {
			// Malformed marker: drop it, as ansi.Strip would.
			b.WriteString(ansi.Strip(p))
			continue
		}
		b.WriteString(osc8Open + p[:st+2])
		b.WriteString(ansi.Strip(p[st+2:]))
	}
	return b.String()
}
