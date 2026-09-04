package reactionpicker

import (
	"io"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	slkemoji "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
	"github.com/gammons/slk/internal/ui/styles"
)

// EmojiEntry represents an emoji with its name and Unicode character.
type EmojiEntry struct {
	Name    string // e.g. "thumbsup"
	Unicode string // e.g. "\U0001f44d"
}

// ReactionResult is returned when the user selects an emoji.
type ReactionResult struct {
	Emoji  string // emoji name without colons
	Remove bool   // true if toggling off an existing reaction
}

// Model is the reaction picker overlay.
type Model struct {
	allEmoji          []EmojiEntry
	frecent           []EmojiEntry
	filtered          []EmojiEntry
	query             string
	selected          int
	visible           bool
	messageTS         string
	channelID         string
	existingReactions []string
	emojiCtx          EmojiContext
}

// EmojiContext bundles the emoji-image rendering dependencies for the
// reaction picker. Set once at startup; updated again when the
// CustomEmojisLoadedMsg arrives via SetEmojiCustoms. Mirrors
// messages.EmojiContext / thread.EmojiContext.
type EmojiContext struct {
	PlaceCtx slkemoji.PlaceContext
	Cells    int               // 1 or 2; 0 falls back to 2
	Customs  map[string]string // workspace custom emoji map; nil = empty
}

// SetEmojiContext configures emoji-image rendering for the picker.
// Mirrors messages.Model.SetEmojiContext.
func (m *Model) SetEmojiContext(ctx EmojiContext) {
	if ctx.Cells != 1 && ctx.Cells != 2 {
		ctx.Cells = 2
	}
	m.emojiCtx = ctx
}

// SetEmojiCustoms updates the customs map without changing PlaceCtx
// or Cells. Called from App.SetCustomEmoji when the workspace's
// custom emoji list arrives. Mirrors messages.Model.SetEmojiCustoms.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiCtx.Customs = customs
}

// HandleEmojiImageReady forces the next View() to re-render so any
// emoji whose cold-cache fetch just completed picks up the warm
// placement. Picker has no render cache; this is currently a no-op
// hook for shape parity with messages/thread/autocomplete. Documented
// so future caching can drop in without changing the reducer arm.
func (m *Model) HandleEmojiImageReady(url string) {
	// no-op; picker re-evaluates Place on every View().
	_ = url
}

// New creates a new reaction picker with the full emoji list.
func New() *Model {
	m := &Model{}
	m.buildEmojiList()
	return m
}

func (m *Model) buildEmojiList() {
	codeMap := slkemoji.CodeMap()
	seen := make(map[string]bool)
	m.allEmoji = make([]EmojiEntry, 0, len(codeMap))

	for code, unicode := range codeMap {
		name := strings.Trim(code, ":")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		m.allEmoji = append(m.allEmoji, EmojiEntry{Name: name, Unicode: strings.TrimRight(unicode, " ")})
	}

	sort.Slice(m.allEmoji, func(i, j int) bool {
		return m.allEmoji[i].Name < m.allEmoji[j].Name
	})
}

// SetCustomEmoji rebuilds the searchable emoji list from built-ins plus
// the active workspace's custom emoji map (as returned by Slack's
// emoji.list, name -> URL or "alias:target"). Customs shadow built-ins
// of the same name. Pass nil to reset to built-ins only.
func (m *Model) SetCustomEmoji(customs map[string]string) {
	entries := slkemoji.BuildEntries(customs)
	m.allEmoji = make([]EmojiEntry, 0, len(entries))
	for _, e := range entries {
		m.allEmoji = append(m.allEmoji, EmojiEntry{
			Name:    e.Name,
			Unicode: e.Display,
		})
	}
	// Re-run the active filter so visible results reflect the new list.
	if m.visible && m.query != "" {
		m.filter()
	}
}

// Open shows the picker for a specific message.
func (m *Model) Open(channelID, messageTS string, existingReactions []string) {
	m.channelID = channelID
	m.messageTS = messageTS
	m.existingReactions = existingReactions
	m.query = ""
	m.selected = 0
	m.filtered = nil
	m.visible = true
}

// Close hides the picker and resets state.
func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

// IsVisible returns whether the picker is showing.
func (m *Model) IsVisible() bool {
	return m.visible
}

// SetFrecentEmoji sets the frequently/recently used emoji list.
func (m *Model) SetFrecentEmoji(entries []EmojiEntry) {
	m.frecent = entries
}

// ChannelID returns the target channel.
func (m *Model) ChannelID() string {
	return m.channelID
}

// MessageTS returns the target message timestamp.
func (m *Model) MessageTS() string {
	return m.messageTS
}

// listTopOffset is the box-local row of the first list row: top border
// (1) + top padding (1) + title (1) + input (1) + blank separator (1).
const listTopOffset = 5

// maxVisibleRows is the height of the results scroll window.
const maxVisibleRows = 10

// boxWidth returns the modal's outer width for a given terminal width.
func boxWidth(termWidth int) int {
	w := termWidth * 30 / 100
	if w < 35 {
		w = 35
	}
	if w > 50 {
		w = 50
	}
	return w
}

// visibleWindow returns the [start, end) slice of the displayed list
// currently shown, using the same scroll math as the renderer.
func (m *Model) visibleWindow() (int, int) {
	total := len(m.displayedList())
	maxVisible := maxVisibleRows
	if maxVisible > total {
		maxVisible = total
	}
	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}
	end := start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// BoxSize returns the rendered modal box's outer dimensions. termHeight is
// accepted for interface symmetry; this modal's height depends only on its
// row count.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := m.visibleWindow()
	nRows := end - start
	if nRows < 1 {
		nRows = 1
	}
	return boxWidth(termWidth), nRows + 7
}

// ClickRow moves the selection to the list row at box-local localY and
// returns true when the click lands on a visible row.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset
	if row < 0 {
		return false
	}
	start, end := m.visibleWindow()
	if row >= end-start {
		return false
	}
	m.selected = start + row
	return true
}

// displayedList returns the list currently shown (frecent or filtered).
func (m *Model) displayedList() []EmojiEntry {
	if m.query == "" {
		return m.frecent
	}
	return m.filtered
}

func (m *Model) filter() {
	if m.query == "" {
		m.filtered = nil
		m.selected = 0
		return
	}

	q := text.Fold(m.query)
	m.filtered = m.filtered[:0]

	var substringMatches []EmojiEntry
	for _, e := range m.allEmoji {
		name := text.Fold(e.Name)
		if strings.HasPrefix(name, q) {
			m.filtered = append(m.filtered, e)
		} else if strings.Contains(name, q) {
			substringMatches = append(substringMatches, e)
		}
		if len(m.filtered)+len(substringMatches) >= 50 {
			break
		}
	}
	m.filtered = append(m.filtered, substringMatches...)
	m.selected = 0
}

func (m *Model) isExistingReaction(emojiName string) bool {
	for _, r := range m.existingReactions {
		if r == emojiName {
			return true
		}
	}
	return false
}

// HandleKey processes a key event and returns a result if an emoji was selected.
func (m *Model) HandleKey(keyStr string) *ReactionResult {
	switch keyStr {
	case "esc", "escape":
		m.Close()
		return nil

	case "enter":
		list := m.displayedList()
		if len(list) == 0 || m.selected >= len(list) {
			return nil
		}
		selected := list[m.selected]
		// Canonicalize to the primary short_name Slack records: the picker
		// offers iamcal aliases (e.g. both "+1" and "thumbsup" for 👍), and
		// existing reactions are stored under the canonical name, so
		// canonicalizing here keeps the wire name and add/remove detection
		// consistent. Workspace custom emoji shadow standard names and pass
		// through unchanged.
		name := slkemoji.CanonicalSlackName(selected.Name, m.emojiCtx.Customs)
		return &ReactionResult{
			Emoji:  name,
			Remove: m.isExistingReaction(name),
		}

	case "up":
		if m.selected > 0 {
			m.selected--
		}
		return nil

	case "down":
		list := m.displayedList()
		if m.selected < len(list)-1 {
			m.selected++
		}
		return nil

	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.filter()
		}
		return nil

	default:
		if len(keyStr) == 1 && keyStr[0] >= 32 && keyStr[0] <= 126 {
			m.query += keyStr
			m.filter()
		}
		return nil
	}
}

// View renders the picker box content.
func (m *Model) View(termWidth int) string {
	return m.renderBox(termWidth)
}

// ViewOverlay composites the picker on top of the background.
func (m *Model) ViewOverlay(termWidth, termHeight int, background string) string {
	if !m.visible {
		return background
	}

	box := m.renderBox(termWidth)
	if box == "" {
		return background
	}

	result := overlay.DimmedOverlay(termWidth, termHeight, background, box, 0.5)
	// Clamp to exactly termHeight lines to prevent terminal scrolling.
	// Emoji with unpredictable terminal widths can cause lipgloss to wrap
	// lines, producing output taller than expected.
	lines := strings.Split(result, "\n")
	if len(lines) > termHeight {
		lines = lines[:termHeight]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderBox(termWidth int) string {
	if !m.visible {
		return ""
	}

	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4 // border + padding

	// All inner spans share the modal bg so the dimmed app behind the
	// overlay doesn't bleed through where individual styled fragments end.
	bg := styles.Background

	// Title
	title := lipgloss.NewStyle().
		Background(bg).
		Foreground(styles.Primary).
		Bold(true).
		Render("Add Reaction")

	// Query input with blue left border
	var inputText string
	if m.query == "" {
		placeholder := lipgloss.NewStyle().Background(bg).Foreground(styles.TextMuted).Render("Type to filter...")
		inputText = "█ " + placeholder
	} else {
		inputText = m.query + "█"
	}
	input := lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{Left: "▌"}).
		BorderLeft(true).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		PaddingLeft(1).
		Background(bg).
		Foreground(styles.TextPrimary).
		Render(inputText)

	// Results (max 10). Scroll window shared with ClickRow hit-testing.
	list := m.displayedList()
	total := len(list)
	start, end := m.visibleWindow()
	maxVisible := end - start

	// Scrollbar gutter on the right when the list overflows. Same pattern
	// as channelfinder/themeswitcher/workspacefinder.
	showScrollbar := total > maxVisible
	rowWidth := innerWidth - 1
	if !showScrollbar {
		rowWidth = innerWidth
	}

	var thumbStart, thumbEnd int
	if showScrollbar {
		thumbHeight := maxVisible * maxVisible / total
		if thumbHeight < 1 {
			thumbHeight = 1
		}
		denom := total - maxVisible
		if denom < 1 {
			denom = 1
		}
		thumbStart = start * (maxVisible - thumbHeight) / denom
		if thumbStart < 0 {
			thumbStart = 0
		}
		if thumbStart > maxVisible-thumbHeight {
			thumbStart = maxVisible - thumbHeight
		}
		thumbEnd = thumbStart + thumbHeight
	}
	thumbStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Primary)
	trackStyle := lipgloss.NewStyle().Background(bg).Foreground(styles.Border)

	// Image-aware emoji-as-image path: active only when the
	// process-global ImageMode is on AND a fetcher has been installed
	// via SetEmojiContext. Otherwise the legacy ShouldRenderUnicode /
	// shortcode-text branch renders (byte-identical to pre-Phase-8).
	imageOK := slkemoji.ImageModeActive() && m.emojiCtx.PlaceCtx.Fetcher != nil
	cells := m.emojiCtx.Cells
	if cells <= 0 {
		cells = 2
	}
	// Unlike messages.View, the picker has no per-frame Flushes slice
	// walked by the host renderer. We collect any kitty-upload
	// callbacks Place produced into this per-View local slice and
	// fire them against imgpkg.KittyOutput just before returning.
	// Most are no-ops in steady state (the messages-pane already
	// uploaded via the shared Registry); the picker still owns the
	// fire to handle the case where it's the first/only surface to
	// reference a given emoji this session.
	var pendingFlushes []func(io.Writer) error

	var resultRows []string
	for i := start; i < end; i++ {
		entry := list[i]
		// Display Unicode emoji when the resolved form is composition-
		// safe (single base codepoint, optionally + VS16). Multi-
		// codepoint sequences (ZWJ, regional-indicator flags, skin
		// tones) render as broken glyphs in many terminal fonts and
		// corrupt the picker's width arithmetic. See
		// internal/emoji/shouldrender.go.
		var preview string
		if imageOK {
			if url, ok := slkemoji.URLForShortcode(entry.Name, m.emojiCtx.Customs); ok {
				if placement, flush, ok := slkemoji.Place(m.emojiCtx.PlaceCtx, url, cells); ok {
					preview = placement
					if flush != nil {
						pendingFlushes = append(pendingFlushes, flush)
					}
				}
			}
		}
		if preview == "" {
			// Legacy fallback path (image mode off, no URL, or
			// Place returned false).
			if slkemoji.ShouldRenderUnicode(entry.Unicode) {
				preview = entry.Unicode
			} else {
				preview = ":" + entry.Name + ":"
			}
		}
		line := preview + " " + entry.Name

		if m.isExistingReaction(entry.Name) {
			line += " ✓"
		}

		if lipgloss.Width(line) > rowWidth {
			line = truncate.StringWithTail(line, uint(rowWidth), "…")
		}

		var row string
		if i == m.selected {
			indicator := lipgloss.NewStyle().Background(bg).Foreground(styles.Accent).Render("▌")
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.Primary).
				Bold(true).
				Width(rowWidth - 1).
				MaxWidth(rowWidth - 1).
				Render(line)
			row = indicator + label
		} else {
			label := lipgloss.NewStyle().
				Background(bg).
				Foreground(styles.TextPrimary).
				Width(rowWidth - 1).
				MaxWidth(rowWidth - 1).
				Render(line)
			row = " " + label
		}

		if showScrollbar {
			rel := i - start
			if rel >= thumbStart && rel < thumbEnd {
				row += thumbStyle.Render("\u2588")
			} else {
				row += trackStyle.Render("\u2502")
			}
		}
		resultRows = append(resultRows, row)
	}

	if len(list) == 0 && m.query != "" {
		noResults := lipgloss.NewStyle().
			Background(bg).
			Foreground(styles.TextMuted).
			Italic(true).
			Render("No matching emoji")
		resultRows = append(resultRows, noResults)
	}

	// Compose content
	content := title + "\n" + input + "\n\n" + strings.Join(resultRows, "\n")

	// Re-paint modal bg+fg after every ANSI reset (see channelfinder).
	content = messages.ReapplyBgAfterResets(content, messages.BgANSI()+messages.FgANSI())

	// Wrap in bordered box
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		BorderBackground(bg).
		Background(bg).
		Padding(1, 1).
		Width(overlayWidth).
		Render(content)

	// Fire any kitty image upload callbacks the per-row Place calls
	// produced. Most are no-ops (the messages pane already triggered
	// the upload via the shared Registry); the picker still owns the
	// fire to handle the case where it's the first/only surface to
	// reference a given emoji this session. Done here (inside
	// renderBox) so both View() and ViewOverlay() benefit without
	// duplication.
	for _, fl := range pendingFlushes {
		_ = fl(imgpkg.KittyOutput)
	}

	return box
}
