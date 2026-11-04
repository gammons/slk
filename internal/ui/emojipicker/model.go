package emojipicker

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

var _ chrome.Dialog = (*Model)(nil)

// MaxVisible caps how many emoji rows are shown in the picker.
// Independent of mentionpicker.MaxVisible.
const MaxVisible = 5

type Model struct {
	entries  []emoji.EmojiEntry
	filtered []emoji.EmojiEntry
	query    string
	selected int
	visible  bool
	emojiCtx EmojiContext
	frecency emoji.Frecency
}

// EmojiContext bundles the emoji-image rendering dependencies for
// the compose autocomplete dropdown. Mirrors the picker's version
// in shape and purpose. Customs resolves custom emoji URLs at View()
// time and tells a custom emoji from a standard one of the same name
// when ranking by frecency (the entries the dropdown searches already
// include workspace customs; see emoji.BuildEntries).
type EmojiContext struct {
	PlaceCtx emoji.PlaceContext
	Cells    int
	Customs  map[string]string
}

// SetEmojiContext configures emoji-image rendering for the autocomplete
// dropdown. Mirrors the same setter on other UI surfaces.
func (m *Model) SetEmojiContext(ctx EmojiContext) {
	if ctx.Cells != 1 && ctx.Cells != 2 {
		ctx.Cells = 2
	}
	m.emojiCtx = ctx
}

// SetEmojiCustoms updates the customs map without changing PlaceCtx
// or Cells. Called from compose.SetEmojiCustoms when the workspace's
// custom emoji list arrives via CustomEmojisLoadedMsg.
//
// Mirrors reactionpicker.Model.SetEmojiCustoms — the picker reads
// m.emojiCtx.Customs at View() time when resolving each row's URL
// (model.go:185 calls URLForShortcode(name, m.emojiCtx.Customs)).
// Without this method the dropdown's customs map stayed at its
// startup-empty value forever, so custom emoji rows fell back to
// the placeholder glyph rendering.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiCtx.Customs = customs
}

// HandleEmojiImageReady is a no-op hook for shape parity with other
// surfaces. The dropdown has no render cache.
func (m *Model) HandleEmojiImageReady(_ string) {}

func New() Model { return Model{} }

// SetEntries replaces the full entry list. If the picker is visible, the
// filtered list and selection are recomputed against the current query.
func (m *Model) SetEntries(entries []emoji.EmojiEntry) {
	m.entries = entries
	if m.visible {
		m.filter()
	}
}

func (m *Model) Open(query string) {
	m.visible = true
	m.query = query
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

func (m *Model) IsVisible() bool { return m.visible }

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m *Model) Query() string { return m.query }

func (m *Model) Filtered() []emoji.EmojiEntry { return m.filtered }

func (m *Model) Selected() int { return m.selected }

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

// SelectedEntry returns the currently highlighted entry. ok=false if the
// filtered list is empty.
func (m *Model) SelectedEntry() (emoji.EmojiEntry, bool) {
	if len(m.filtered) == 0 {
		return emoji.EmojiEntry{}, false
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return emoji.EmojiEntry{}, false
	}
	return m.filtered[m.selected], true
}

// SetFrecency sets the user's frequently/recently used emoji, which
// lead the matches for a query. If the picker is visible, the filtered
// list is recomputed.
func (m *Model) SetFrecency(f emoji.Frecency) {
	m.frecency = f
	if m.visible {
		m.filter()
	}
}

// filter keeps the first MaxVisible entries whose name starts with the
// query, the user's frecent ones first (see emoji.Match). Callers must
// pass alphabetically-sorted entries (emoji.BuildEntries already does);
// the rest keep that order.
func (m *Model) filter() {
	m.filtered = emoji.Match(m.entries, func(e emoji.EmojiEntry) string { return e.Name }, m.query,
		emoji.MatchOptions{Limit: MaxVisible, Frecency: m.frecency, Customs: m.emojiCtx.Customs})
	if m.selected >= len(m.filtered) {
		m.selected = 0
		if len(m.filtered) > 0 {
			m.selected = len(m.filtered) - 1
		}
	}
}

// View renders the bordered dropdown. Returns "" when not visible OR when
// there are no matches (caller already shows the textarea below).
func (m Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}
	s := messages.ChromeStyles()

	// Compute the widest display preview so name columns line up.
	previewWidth := 1
	for _, e := range m.filtered {
		w := lipgloss.Width(e.Display)
		if w > previewWidth {
			previewWidth = w
		}
	}

	// Image-aware emoji-as-image path: active only when the
	// process-global ImageMode is on AND a fetcher has been installed
	// via SetEmojiContext. Otherwise the legacy `e.Display` branch
	// renders (byte-identical to pre-Phase-9).
	imageOK := emoji.ImageModeActive() && m.emojiCtx.PlaceCtx.Fetcher != nil
	cells := m.emojiCtx.Cells
	if cells <= 0 {
		cells = 2
	}
	// Collect any kitty-upload callbacks Place produced into this
	// per-View local slice and fire them against imgpkg.KittyOutput
	// just before returning. Most are no-ops in steady state (the
	// messages-pane already uploaded via the shared Registry); the
	// dropdown still owns the fire to handle the case where it's the
	// first/only surface to reference a given emoji this session.
	var pendingFlushes []func(io.Writer) error

	var rows []string
	for i, e := range m.filtered {
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		if i == m.selected {
			nameStyle = nameStyle.Bold(true)
		}

		var preview string
		if imageOK {
			if url, ok := emoji.URLForShortcode(e.Name, m.emojiCtx.Customs); ok {
				if placement, flush, ok := emoji.Place(m.emojiCtx.PlaceCtx, url, cells); ok {
					preview = placement
					if flush != nil {
						pendingFlushes = append(pendingFlushes, flush)
					}
				}
			}
		}
		if preview == "" {
			preview = e.Display
		}

		// Pad preview cell so all names start at the same column.
		pad := previewWidth - lipgloss.Width(preview)
		if pad < 0 {
			pad = 0
		}
		preview = preview + strings.Repeat(" ", pad)
		row := chrome.PopupMarker(s, i == m.selected) + preview + "  " + nameStyle.Render(":"+e.Name+":")
		rows = append(rows, row)
	}

	box := chrome.Popup(s, rows, width)

	// Fire any kitty image upload callbacks the per-row Place calls
	// produced. Done here (inside View) so the autocomplete dropdown
	// owns kitty uploads independently of any other surface.
	for _, fl := range pendingFlushes {
		_ = fl(imgpkg.KittyOutput)
	}
	return box
}
