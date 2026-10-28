package reactionpicker

import (
	"io"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/muesli/reflow/truncate"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/core"
	slkemoji "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/overlay"
)

// ReactionResult is returned when the user selects an emoji.
type ReactionResult struct {
	Emoji  string // emoji name without colons
	Remove bool   // true if toggling off an existing reaction
}

// Model is the reaction picker overlay.
type Model struct {
	allEmoji          []core.EmojiEntry
	frecency          slkemoji.Frecency
	filtered          []core.EmojiEntry
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
// messages.EmojiContext, which both content panes share.
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

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// New creates a new reaction picker with the full emoji list.
func New() *Model {
	m := &Model{}
	m.buildEmojiList()
	return m
}

func (m *Model) buildEmojiList() {
	codeMap := slkemoji.CodeMap()
	seen := make(map[string]bool)
	m.allEmoji = make([]core.EmojiEntry, 0, len(codeMap))

	for code, unicode := range codeMap {
		name := strings.Trim(code, ":")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		m.allEmoji = append(m.allEmoji, core.EmojiEntry{Name: name, Unicode: strings.TrimRight(unicode, " ")})
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
	m.allEmoji = make([]core.EmojiEntry, 0, len(entries))
	for _, e := range entries {
		m.allEmoji = append(m.allEmoji, core.EmojiEntry{
			Name:    e.Name,
			Unicode: e.Display,
		})
	}
	// Re-run the active filter so visible results reflect the new list.
	if m.visible {
		m.filter()
	}
}

// Open shows the picker for a specific message.
func (m *Model) Open(channelID, messageTS string, existingReactions []string) {
	m.channelID = channelID
	m.messageTS = messageTS
	m.existingReactions = existingReactions
	m.query = ""
	m.visible = true
	m.filter()
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

// SetFrecentEmoji sets the user's frequently/recently used emoji, most
// frecent first. They lead the list, both with no query and among the
// matches for one; only their names are read.
func (m *Model) SetFrecentEmoji(entries []core.EmojiEntry) {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	m.frecency = slkemoji.NewFrecency(names)
	if m.visible {
		m.filter()
	}
}

// ChannelID returns the target channel.
func (m *Model) ChannelID() string {
	return m.channelID
}

// MessageTS returns the target message timestamp.
func (m *Model) MessageTS() string {
	return m.messageTS
}

// listTopOffset is the box-local row of the first list row.
func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), true) }

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

// BoxSize returns the rendered modal box's outer dimensions. termHeight is
// accepted for interface symmetry; this modal's height depends only on its
// row count.
func (m *Model) BoxSize(termWidth, termHeight int) (int, int) {
	start, end := chrome.Window(m.selected, len(m.displayedList()), maxVisibleRows)
	nRows := end - start
	if nRows < 1 {
		nRows = 1
	}
	return boxWidth(termWidth), nRows + 7
}

// ClickRow moves the selection to the list row at box-local localY and
// returns true when the click lands on a visible row.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	if row < 0 {
		return false
	}
	start, end := chrome.Window(m.selected, len(m.displayedList()), maxVisibleRows)
	if row >= end-start {
		return false
	}
	m.selected = start + row
	return true
}

// displayedList returns the list currently shown.
func (m *Model) displayedList() []core.EmojiEntry {
	return m.filtered
}

// maxResults caps the list, with or without a query.
const maxResults = 50

// filter lists the emoji matching the query (every emoji when it is
// empty), the user's frecent ones first: see slkemoji.Match.
func (m *Model) filter() {
	m.filtered = slkemoji.Match(m.allEmoji, func(e core.EmojiEntry) string { return e.Name }, m.query,
		slkemoji.MatchOptions{
			Substring: true,
			Limit:     maxResults,
			Frecency:  m.frecency,
			Customs:   m.emojiCtx.Customs,
		})
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
			_, n := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-n]
			m.filter()
		}
		return nil

	default:
		if r := []rune(keyStr); len(r) == 1 && unicode.IsPrint(r[0]) {
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
	s := messages.ChromeStyles()
	overlayWidth := boxWidth(termWidth)
	innerWidth := overlayWidth - 4 // border + padding

	list := m.displayedList()
	start, end := chrome.Window(m.selected, len(m.displayedList()), maxVisibleRows)
	win := chrome.List{Start: start, End: end, Total: len(list)}
	width := win.ContentWidth(innerWidth)

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

	rows := make([]string, 0, end-start)
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

		// Truncated one column wider than the row and clipped by
		// MaxWidth: preserved as found (Phase 4 spec §8.1).
		if lipgloss.Width(line) > width+1 {
			line = truncate.StringWithTail(line, uint(width+1), "…")
		}
		style := s.Row
		if i == m.selected {
			style = s.RowSelected
		}
		rows = append(rows, style.Width(width).MaxWidth(width).Render(line))
	}

	f := chrome.Frame{
		Title: "Add Reaction",
		Input: chrome.FilterInput(s, m.query, "Type to filter..."),
		Body:  win.Rows(s, m.selected, rows),
	}
	if m.query != "" {
		f.Empty = "No matching emoji"
	}
	box := f.Render(s, overlayWidth)

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
