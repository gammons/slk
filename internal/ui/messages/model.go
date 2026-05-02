package messages

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/scrollbar"
	"github.com/gammons/slk/internal/ui/selection"
	"github.com/gammons/slk/internal/ui/styles"
	emoji "github.com/kyokomi/emoji/v2"
)

type MessageItem struct {
	TS          string
	UserName    string
	UserID      string
	Text        string
	Timestamp   string // formatted display time (e.g. "3:04 PM")
	DateStr     string // date string for grouping (e.g. "2026-04-23")
	ThreadTS    string
	ReplyCount  int
	Reactions   []ReactionItem
	Attachments []Attachment
	IsEdited    bool
	// Subtype mirrors Slack's `subtype` field on a message event.
	// Currently we only act on "thread_broadcast" (a thread reply that
	// was also sent to the channel) so we can render a label above it.
	Subtype string

	// Blocks holds parsed Slack Block Kit blocks. Rendered between
	// the body Text and the file Attachments by Phase 5.
	Blocks []blockkit.Block

	// LegacyAttachments holds parsed entries from the legacy
	// `attachments` field (color stripe + title + fields style bot
	// cards). Rendered after Blocks.
	LegacyAttachments []blockkit.LegacyAttachment
}

// Attachment represents a file or image attached to a message.
// Kind is "image" for image/* mimetypes, "file" otherwise.
// URL is the user-facing permalink (preferred) or fallback to url_private.
type Attachment struct {
	Kind string // "image" or "file"
	Name string // display filename / title
	URL  string // permalink (preferred) or url_private

	// Populated only for Kind == "image":
	FileID string      // Slack file ID for cache key
	Mime   string      // e.g. "image/png"
	Thumbs []ThumbSpec // sorted ascending; empty for non-image
}

// ThumbSpec is one Slack thumbnail variant.
//
// This is intentionally distinct from image.ThumbSpec in the internal/image
// package to avoid coupling the messages UI package to the image package's
// internal type. A converter helper bridges the two where needed.
type ThumbSpec struct {
	URL string
	W   int
	H   int
}

// AvatarFunc returns the rendered half-block avatar for a user ID, or empty string.
type AvatarFunc func(userID string) string

type ReactionItem struct {
	Emoji      string // emoji name without colons, e.g. "thumbsup"
	Count      int
	HasReacted bool // whether the current user has reacted with this emoji
}

// viewEntry is a pre-rendered row in the message list (message or date separator).
//
// For messages we pre-render BOTH the selected and unselected bordered variants
// during buildCache so that selection movement (j/k) is a near-O(1) operation
// in View(): no lipgloss calls per keypress, just string concatenation.
//
// For separators (msgIdx == -1) only `content` is populated.
//
// linesPlain is a column-aligned, ANSI-stripped mirror of linesNormal used
// by the selection layer to extract clipboard text and check column
// membership. Each entry pairs the line's plain text with a column→byte
// index (preserving multi-rune grapheme clusters intact); see plainLine
// and sliceColumns in render.go for the slicing contract.
type viewEntry struct {
	// linesNormal / linesSelected hold the entry's rendered lines pre-split on
	// "\n" so View() can append them directly into the visible window without
	// any string scanning, splitting, or width measurement at render time.
	// For separator entries (msgIdx == -1) the two slices are identical.
	linesNormal   []string
	linesSelected []string
	linesPlain    []plainLine // column-aligned mirror of CONTENT (sans border)
	// contentColOffset is the number of display columns at the START of each
	// linesNormal[i] that belong to chrome (e.g. the thick left border ▌ on
	// message entries) and should be skipped when mapping mouse columns to
	// columns in linesPlain. Plain lines DO NOT include these chrome columns,
	// so a mouse column of N maps to a plain column of N - contentColOffset.
	// Default 0 (separators have no border); message entries set 1.
	contentColOffset int
	height           int // == len(linesNormal); cached for scroll math
	msgIdx           int // index into messages, or -1 for separator

	// flushes are per-frame side effects (kitty image uploads). Phase 6
	// invokes these from Model.View() once-per-frame, deduplicated by
	// image ID via the package-level kitty registry. nil for entries
	// with no inline image attachments.
	flushes []func(io.Writer) error

	// sixelRows maps absolute row index within linesNormal to the sixel
	// byte stream for an image whose top-left cell sits on that row.
	// Phase 6 emits the bytes via the active output writer only when the
	// image's full vertical extent is on-screen; otherwise it substitutes
	// the entry's halfblock fallback. nil for entries with no sixel
	// attachments.
	sixelRows map[int]sixelEntry

	// imageHits records the column extents of inline image attachments
	// rendered in this entry, keyed by row-start within linesNormal.
	// View() translates these to viewport-absolute coordinates and
	// appends to Model.lastHits per frame so the app-level mouse handler
	// can route clicks to the click-to-preview overlay (Phase 7).
	imageHits []entryHit
}

// entryHit is one inline-image hit-rect, expressed in coordinates
// relative to a single viewEntry's linesNormal. rowStart/rowEnd are
// row indices into linesNormal; colStart/colEnd are display columns
// within those lines (i.e., they ALREADY include the avatar gutter and
// the thick-left-border column added by buildCache, so they map
// directly to mouse columns at View() time after viewport translation).
type entryHit struct {
	rowStartInEntry int
	rowEndInEntry   int // exclusive
	colStart        int
	colEnd          int // exclusive
	fileID          string
	attIdx          int // index into MessageItem.Attachments
}

// hitRect is one clickable image footprint in the messages pane,
// expressed in viewport-absolute coordinates. rowStart/rowEnd are
// rows within the message-content area (0 = first row below the
// channel-header chrome). colStart/colEnd are display columns within
// the messages pane. msgIdx and attIdx identify the message and
// attachment so the mouse handler can construct an OpenImagePreviewMsg.
type hitRect struct {
	rowStart int
	rowEnd   int // exclusive
	colStart int
	colEnd   int // exclusive
	fileID   string
	msgIdx   int
	attIdx   int
}

// sixelEntry holds the pre-computed sixel bytes for one inline image,
// plus the halfblock fallback used when the image is only partially
// visible (Phase 6 cannot emit a half-image with sixel).
type sixelEntry struct {
	bytes    []byte
	fallback []string // halfblock-equivalent text for partial-visibility frames
	height   int      // image height in rows
}

// ImageContext holds the dependencies the messages model needs for image
// rendering. Configured at startup via Model.SetImageContext. SendMsg is
// optional; when nil, image fetches still complete but no re-render is
// triggered when bytes arrive (set by Task 5.6 once the tea.Program is
// available).
type ImageContext struct {
	Protocol    imgpkg.Protocol
	Fetcher     *imgpkg.Fetcher
	KittyRender *imgpkg.KittyRenderer
	CellPixels  image.Point
	// MaxRows caps the height of an inline image in terminal rows.
	MaxRows int
	// MaxCols caps the width of an inline image in terminal columns.
	// The image is also bounded by the available message-pane width
	// when narrower. 0 disables the column cap (width-only bounded by
	// the message pane).
	MaxCols int
	SendMsg func(tea.Msg)
}

// ImageReadyMsg is dispatched by the prefetcher when an image attachment
// has finished downloading and decoding into the cache. The model
// invalidates the cache for the affected message and re-renders.
// Key is the cache key whose fetch just succeeded; the model uses it
// to clear ONLY that entry from its in-flight tracking set, avoiding
// the fetch-stampede that occurs if every successful arrival cleared
// the whole set (which would re-spawn fetches for every still-failing
// sibling image, each one re-running the slow multi-auth retry path).
type ImageReadyMsg struct {
	Channel string
	TS      string
	Key     string
}

// ImageFailedMsg is dispatched by the prefetcher when all auth attempts
// for an image have failed. Carries the cache key only; the model uses
// it to clear the in-flight bit for that key so a future cache
// invalidation doesn't try the same image again. It does NOT trigger a
// re-render (the placeholder is already on screen and the bytes still
// aren't available).
type ImageFailedMsg struct {
	Key string
}

// OpenImagePreviewMsg requests opening the full-screen preview overlay
// for a specific message attachment. Dispatched by the messages model
// on click (Phase 7.4) or `O` keybind (Phase 7.3). The App's Update
// handler fetches the largest available thumb and constructs an
// image.Preview to render over the messages+thread region.
type OpenImagePreviewMsg struct {
	Channel string
	TS      string
	AttIdx  int
}

type Model struct {
	messages     []MessageItem
	selected     int
	channelName  string
	channelTopic string
	channelType  string // "channel", "private", "dm", "group_dm" -- drives header glyph
	loading      bool
	avatarFn     AvatarFunc        // optional: returns half-block avatar for a userID
	userNames    map[string]string // user ID -> display name for mention resolution
	channelNames map[string]string // channel ID -> name for bare <#CID> resolution

	// Render cache -- invalidated when messages or width change.
	// Each entry holds pre-bordered variants so selection movement does not
	// re-invoke lipgloss per keypress.
	cache       []viewEntry
	cacheWidth  int
	cacheMsgLen int
	cacheSpacer       string // pre-rendered blank spacer line (1 row, full width, themed background)
	cacheLoadingHint  string // pre-rendered "Loading older messages..." line
	cacheMoreBelow    string // pre-rendered "-- more below --" line

	// Chrome cache: header line(s). Depends on width, channelName, and
	// channelTopic only -- never on selection or scroll position.
	chromeCache       string
	chromeHeight      int
	chromeWidth       int
	chromeChannel     string
	chromeTopic       string
	chromeChannelType string
	chromeCacheValid  bool

	// Cumulative line offsets, computed in buildCache (only when content
	// changes). entryOffsets[i] is the line index where entry i starts in the
	// flattened content; totalLines is the total line count.
	entryOffsets []int
	totalLines   int

	// Custom scroll state -- replaces bubbles/viewport for the scrolling case
	// where we already know our content's line count and width. The bubbles
	// viewport calls ansi.StringWidth on every line of content per SetContent
	// (~55% of CPU on j/k); we skip that entirely.
	yOffset int

	// snappedSelection tracks the last selection index that View() snapped
	// yOffset to. While snappedSelection == selected, View() leaves yOffset
	// alone -- this allows the mouse wheel (or programmatic ScrollUp/Down)
	// to scroll freely without the next render yanking the viewport back to
	// the selected message.
	snappedSelection int
	hasSnapped       bool

	// Tracks the start / end line of the currently-selected entry so View()
	// can adjust yOffset to keep it on screen.
	selectedStartLine int
	selectedEndLine   int

	reactionNavActive bool
	reactionNavIndex  int

	lastReadTS string

	// version increments on every state change that could alter rendered
	// View() output. The App layer caches the WRAPPED panel output (border +
	// exactSize + ReapplyBgAfterResets) keyed on this counter, so on compose
	// keystrokes (where version is unchanged) we reuse the previous wrap.
	version int64

	// Mouse selection state. selRange is the user's drag selection.
	// messageIDToEntryIdx maps Slack TS -> entry index in m.cache for
	// O(1) anchor resolution; rebuilt on every buildCache. lastViewHeight
	// is captured during View() so ScrollHintForDrag knows the pane
	// bounds without needing the App to plumb them through.
	selRange            selection.Range
	hasSelection        bool
	messageIDToEntryIdx map[string]int
	lastViewHeight      int
	// countedReplies tracks reply TSes already counted into a parent's
	// ReplyCount, so optimistic + WS-echo paths don't double-increment.
	countedReplies map[string]map[string]struct{}

	// imgCtx is the inline-image rendering context: protocol, fetcher,
	// cell metrics, and tea.Program send-fn. Zero-valued ImageContext is
	// equivalent to ProtoOff (no inline rendering, attachments fall back
	// to the legacy single-line OSC 8 hyperlink form).
	imgCtx ImageContext

	// fetchingImages tracks the set of cache keys for which a placeholder
	// path has already spawned a fetch goroutine. Prevents per-frame
	// goroutine pile-up during scroll/resize when an image is in flight.
	// Cleared per-key when the corresponding ImageReadyMsg or
	// ImageFailedMsg lands.
	//
	// Concurrency: only mutated from Update-driven render paths
	// (Model is single-threaded from bubbletea's perspective). Goroutines
	// never write to this map directly.
	fetchingImages map[string]struct{}

	// staleEntries is the set of message TSes whose render-cache slot
	// must be rebuilt before the next View() commit, while sibling
	// slots are reused verbatim. Populated by HandleImageReady when an
	// inline image's bytes arrive: instead of nilling the entire cache
	// (which forces buildCache to walk every message in the channel),
	// we mark the affected TS and let the View()-time guard dispatch a
	// targeted partial rebuild via partialRebuild. Cleared at the end
	// of partialRebuild and by full buildCache invocations. nil is
	// equivalent to "nothing stale".
	staleEntries map[string]struct{}

	// failedImages remembers cache keys whose fetch hit a permanent
	// failure (e.g. all auths exhausted on a Slack Connect channel the
	// user isn't actually a member of). renderAttachmentBlock skips
	// re-spawning fetches for these keys until the channel is switched.
	// Cleared by SetChannel.
	failedImages map[string]struct{}

	// lastHits holds the inline-image hit rects captured during the most
	// recent View() call, in viewport-absolute coordinates relative to
	// the message-content area (excludes the channel-header chrome).
	// Cleared (length 0, capacity preserved) at the start of each View;
	// consumed by the app-level mouse handler via HitTest. See Phase 7
	// (click-to-preview) for the consumer.
	lastHits []hitRect

	// focused tracks whether this panel currently has user focus. When
	// false, the selected-message "▌" border dims from Accent to
	// TextMuted (via styles.SelectionBorderColor) so the unfocused
	// selection doesn't compete visually with the focused panel. The
	// color is baked into viewEntry.linesSelected during buildCache, so
	// SetFocused must invalidate the cache.
	focused bool
}

// SetFocused records whether the messages pane currently holds user focus
// and invalidates the render cache so the selected-message border re-renders
// with the appropriate color (Accent when focused, TextMuted when not). The
// cache is dropped only when the value actually flips, to avoid spurious
// rebuilds on every render.
func (m *Model) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	m.cache = nil
	m.dirty()
}

// HandleImageReady is invoked by the host (App.Update) when an
// ImageReadyMsg lands. It marks the affected message's render-cache
// entry as stale (so the next View() rebuilds only that slot, not the
// whole channel) and clears ONLY the specified key from the in-flight
// fetch set so other in-flight images keep their dedup bit (avoids
// fetch stampedes — see ImageReadyMsg's doc comment).
//
// This per-entry invalidation is the perf invariant for image bursts:
// when a channel switch or scroll-up surfaces N attachments
// back-to-back, the prior whole-cache nil forced N full walks across
// every message in the loaded history on the bubbletea Update
// goroutine. Now N arrivals trigger N pointed rebuilds of one entry
// each, with siblings reused verbatim.
//
// Messages for a non-active channel are ignored (the cache is
// per-channel; switching channels rebuilds it).
//
// key may be empty for legacy callers; in that case the entire cache
// is invalidated (old behavior) for safety, since we don't know which
// entry to mark stale.
func (m *Model) HandleImageReady(channel, ts, key string) {
	if channel != m.channelName {
		return
	}
	if key == "" {
		// Legacy path: no per-key bookkeeping available, so we fall
		// back to the wholesale invalidation that the new fast path
		// is meant to avoid. Used by tests that drive transitions
		// synchronously without a real fetch key.
		m.cache = nil
		m.fetchingImages = nil
		m.dirty()
		return
	}
	if m.fetchingImages != nil {
		delete(m.fetchingImages, key)
	}
	if ts != "" && m.cache != nil {
		if m.staleEntries == nil {
			m.staleEntries = make(map[string]struct{})
		}
		m.staleEntries[ts] = struct{}{}
	} else {
		// No TS to target (or no cache to mutate): fall back to a
		// full rebuild on the next View().
		m.cache = nil
	}
	m.dirty()
}

// HandleImageFailed clears the in-flight bit for a specific key and
// records the key as permanently failed. renderAttachmentBlock checks
// failedImages before spawning a fetch, so a permanently-failed image
// (e.g. all auths exhausted on a Slack Connect channel the user isn't
// actually a member of) won't be re-fetched on every cache
// invalidation. The failure record is cleared on channel switch so
// the user can retry by switching away and back.
//
// Does NOT invalidate the render cache: the placeholder is already on
// screen and we have no new bytes to show.
func (m *Model) HandleImageFailed(key string) {
	if key == "" {
		return
	}
	if m.fetchingImages != nil {
		delete(m.fetchingImages, key)
	}
	if m.failedImages == nil {
		m.failedImages = make(map[string]struct{})
	}
	m.failedImages[key] = struct{}{}
}

// Version returns a counter that increments every time the View() output
// could change.
func (m *Model) Version() int64 { return m.version }

// dirty bumps the render-version counter.
func (m *Model) dirty() { m.version++ }

func New(msgs []MessageItem, channelName string) Model {
	selected := 0
	if len(msgs) > 0 {
		selected = len(msgs) - 1
	}
	return Model{
		messages:    msgs,
		selected:    selected,
		channelName: channelName,
	}
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.chromeCacheValid = false
	m.dirty()
}

func (m *Model) SetChannel(name, topic string) {
	if m.channelName != name || m.channelTopic != topic {
		m.chromeCacheValid = false
		m.dirty()
		// Different channel = different attachment set; clear the
		// permanently-failed-image record so the user can retry by
		// re-entering this channel later.
		if m.channelName != name {
			m.failedImages = nil
			m.fetchingImages = nil
		}
	}
	m.channelName = name
	m.channelTopic = topic
}

// SetChannelType sets the channel type used to pick the header glyph
// (# for public, \u25c6 for private, \u25cf for dm/group_dm).
// Pass an empty string or "channel" for the default `#` prefix.
func (m *Model) SetChannelType(chType string) {
	if m.channelType != chType {
		m.chromeCacheValid = false
		m.dirty()
	}
	m.channelType = chType
}

// channelGlyph returns the prefix glyph to render before the channel
// name in the header. Mirrors the sidebar's type-to-glyph mapping.
func channelGlyph(chType string) string {
	switch chType {
	case "private":
		return "\u25c6" // ◆
	case "dm", "group_dm":
		return "\u25cf" // ●
	default:
		return "#"
	}
}

func (m *Model) SetMessages(msgs []MessageItem) {
	m.messages = msgs
	m.ClearSelection()
	m.cache = nil // invalidate cache
	// Force the next View() to re-snap yOffset to the new selection -- without
	// this, switching to a channel that happens to have the same selected
	// index as the previous channel would leave yOffset at its old value.
	m.hasSnapped = false
	m.dirty()

	if len(msgs) == 0 {
		m.selected = 0
		return
	}
	// Start at the bottom -- newest messages visible
	m.selected = len(msgs) - 1
}

func (m *Model) AppendMessage(msg MessageItem) {
	// Idempotent on TS. Self-sent messages take an optimistic path
	// (MessageSentMsg from the chat.postMessage HTTP response) AND
	// arrive again as a WebSocket echo (NewMessageMsg). The order of
	// those two events isn't deterministic -- the WS echo can arrive
	// before the HTTP response returns -- so a TS-keyed dedup map at the
	// caller can race. Dedup here, at the model boundary, defends
	// against duplicates regardless of arrival order.
	if msg.TS != "" {
		// Scan from the back: dupes, when they happen, are recent.
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].TS == msg.TS {
				return
			}
		}
	}
	m.messages = append(m.messages, msg)
	m.cache = nil // invalidate cache
	m.dirty()

	// Always scroll to the newest message. Advancing `selected` to the
	// last index forces View() to re-snap yOffset to the bottom on the
	// next render (because snappedSelection != selected).
	m.selected = len(m.messages) - 1
}

// UpsertSelfSent is the optimistic-add variant of AppendMessage for
// messages we just sent ourselves. If a message with the same TS
// already exists (e.g. a WS echo arrived faster than the HTTP
// response and AppendMessage stored its version first), this method
// REPLACES that entry's contents with msg. Otherwise it appends.
//
// The replace-on-duplicate behaviour ensures the optimistic-display
// text — which carries the locally-converted mrkdwn from the slk
// compose box — always wins over Slack's WS-echo text. Slack may
// normalise the wire-form text (e.g. flatten paragraph breaks for
// rich_text_block messages), and our renderer only consults the
// Text field, so without this fix multi-line composed messages
// render horizontally when the WS echo races ahead.
//
// Replacement also invalidates the snap-to-selected anchor so View()
// re-pins yOffset to the bottom: when the optimistic version's text
// expands the message from a 1-line WS-echo placeholder to multiple
// lines, the previously-selected last index now extends below the
// fold and would otherwise stay clipped.
func (m *Model) UpsertSelfSent(msg MessageItem) {
	if msg.TS != "" {
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].TS == msg.TS {
				m.messages[i] = msg
				m.cache = nil
				m.dirty()
				// Force the next View() to re-snap yOffset to the
				// updated selected entry's range, even when m.selected
				// hasn't changed.
				m.hasSnapped = false
				return
			}
		}
	}
	m.messages = append(m.messages, msg)
	m.cache = nil
	m.dirty()
	m.selected = len(m.messages) - 1
}

func (m *Model) Messages() []MessageItem {
	return m.messages
}

func (m *Model) SelectedIndex() int {
	return m.selected
}

func (m *Model) SelectedMessage() (MessageItem, bool) {
	if len(m.messages) == 0 {
		return MessageItem{}, false
	}
	return m.messages[m.selected], true
}

// SelectByIndex moves the selection cursor to i. No-op if i is out of
// range. Used by tests that need a deterministic selection state.
func (m *Model) SelectByIndex(i int) {
	if i < 0 || i >= len(m.messages) {
		return
	}
	if m.selected != i {
		m.selected = i
		m.dirty()
	}
}

// ChromeHeight returns the number of rows at the top of the messages
// pane consumed by the channel header / separator chrome. Set during
// View() (so callers must invoke View at least once for a meaningful
// value). The app-level mouse handler subtracts this from a pane-local
// y coordinate before calling HitTest, mirroring the convention used
// internally by ClickAt and BeginSelectionAt.
func (m *Model) ChromeHeight() int {
	return m.chromeHeight
}

// HitRect is a public, immutable snapshot of one inline-image hit
// rect, used exclusively by tests in other packages (the app-level
// mouse-click integration test) that need to query the post-View()
// hit cache without exporting the unexported hitRect type itself.
// Coordinates use the same frame as HitTest's input: rows are within
// the messages-pane content area (chrome already subtracted); cols
// are display columns within the messages pane.
type HitRect struct {
	RowStart, RowEnd int // RowEnd exclusive
	ColStart, ColEnd int // ColEnd exclusive
	FileID           string
	MsgIdx, AttIdx   int
}

// LastHitsForTest returns the inline-image hit rects captured during
// the most recent View() call. Test-only entry point; production code
// must use HitTest. Returns a freshly-allocated slice so callers
// cannot mutate the internal cache.
func (m *Model) LastHitsForTest() []HitRect {
	out := make([]HitRect, 0, len(m.lastHits))
	for _, h := range m.lastHits {
		out = append(out, HitRect{
			RowStart: h.rowStart,
			RowEnd:   h.rowEnd,
			ColStart: h.colStart,
			ColEnd:   h.colEnd,
			FileID:   h.fileID,
			MsgIdx:   h.msgIdx,
			AttIdx:   h.attIdx,
		})
	}
	return out
}

func (m *Model) MoveUp() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

// ScrollUp moves the viewport up by n lines without changing the selected
// message. The selection may scroll off-screen; pressing j/k will snap the
// viewport back to keep the (new) selection visible.
func (m *Model) ScrollUp(n int) {
	if n <= 0 {
		return
	}
	m.yOffset -= n
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	// Mark the current selection as already snapped so View() leaves yOffset
	// alone on the next render.
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

// ScrollDown moves the viewport down by n lines without changing the selected
// message. View() clamps yOffset to the maximum allowed for the current
// content height.
func (m *Model) ScrollDown(n int) {
	if n <= 0 {
		return
	}
	m.yOffset += n
	m.snappedSelection = m.selected
	m.hasSnapped = true
	m.dirty()
}

func (m *Model) MoveDown() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected < len(m.messages)-1 {
		m.selected++
		m.dirty()
	}
}

func (m *Model) IsAtBottom() bool {
	return m.selected >= len(m.messages)-1
}

func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

func (m *Model) GoToBottom() {
	if len(m.messages) > 0 && m.selected != len(m.messages)-1 {
		m.selected = len(m.messages) - 1
		m.dirty()
	}
}

func (m *Model) AtTop() bool {
	return m.selected == 0 && len(m.messages) > 0
}

func (m *Model) PrependMessages(msgs []MessageItem) {
	if len(msgs) == 0 {
		return
	}
	count := len(msgs)
	m.messages = append(msgs, m.messages...)
	m.selected += count
	m.cache = nil // invalidate cache
	m.dirty()
}

func (m *Model) EnterReactionNav() {
	if msg, ok := m.SelectedMessage(); ok && len(msg.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.cache = nil
		m.dirty()
	}
}

func (m *Model) ExitReactionNav() {
	if !m.reactionNavActive && m.reactionNavIndex == 0 {
		return
	}
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.cache = nil
	m.dirty()
}

func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

func (m *Model) ReactionNavLeft() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	total := len(msg.Reactions) + 1 // +1 for [+] pill
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
	m.cache = nil
	m.dirty()
}

func (m *Model) ReactionNavRight() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	total := len(msg.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.cache = nil
	m.dirty()
}

func (m *Model) SelectedReaction() (emoji string, isPlus bool) {
	msg, ok := m.SelectedMessage()
	if !ok {
		return "", false
	}
	if m.reactionNavIndex >= len(msg.Reactions) {
		return "", true
	}
	return msg.Reactions[m.reactionNavIndex].Emoji, false
}

func (m *Model) ClampReactionNav() {
	msg, ok := m.SelectedMessage()
	if !ok || len(msg.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(msg.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
	m.cache = nil
	m.dirty()
}

// IncrementReplyCount finds a message by TS and increments its ReplyCount.
// Idempotent on (parentTS, replyTS): the optimistic ThreadReplySentMsg path
// and the WS echo NewMessageMsg path can both fire for the same reply --
// pass the reply's own TS to dedup. An empty replyTS skips dedup (legacy
// callers that don't yet pass it).
func (m *Model) IncrementReplyCount(parentTS, replyTS string) {
	if replyTS != "" {
		if m.countedReplies == nil {
			m.countedReplies = make(map[string]map[string]struct{})
		}
		set := m.countedReplies[parentTS]
		if set == nil {
			set = make(map[string]struct{})
			m.countedReplies[parentTS] = set
		}
		if _, dup := set[replyTS]; dup {
			return
		}
		set[replyTS] = struct{}{}
	}
	// Same bottom-anchor preservation as UpdateReaction: if the increment
	// is going to add a "[N replies ->]" line under a message that's
	// currently at the bottom of the visible window, advance yOffset by
	// the height delta so the message stays pinned to the bottom instead
	// of being pushed under the fold.
	wasAnchoredAtBottom := false
	oldTotalLines := m.totalLines
	if m.cache != nil && m.lastViewHeight > 0 && m.totalLines > 0 {
		wasAnchoredAtBottom = m.yOffset+m.lastViewHeight >= m.totalLines
	}

	for i, msg := range m.messages {
		if msg.TS == parentTS {
			m.messages[i].ReplyCount++
			m.cache = nil
			m.dirty()
			if wasAnchoredAtBottom && m.cacheWidth > 0 {
				m.buildCache(m.cacheWidth)
				if delta := m.totalLines - oldTotalLines; delta > 0 {
					m.yOffset += delta
					if maxOffset := m.totalLines - m.lastViewHeight; m.yOffset > maxOffset {
						m.yOffset = maxOffset
					}
					if m.yOffset < 0 {
						m.yOffset = 0
					}
					m.snappedSelection = m.selected
					m.hasSnapped = true
				}
			}
			return
		}
	}
}

// UpdateMessageInPlace finds a message by TS, replaces its text, and
// marks it as edited. Returns true if the message was found.
// Invalidates the render cache.
func (m *Model) UpdateMessageInPlace(ts, newText string) bool {
	for i, msg := range m.messages {
		if msg.TS == ts {
			m.messages[i].Text = newText
			m.messages[i].IsEdited = true
			m.cache = nil
			m.dirty()
			return true
		}
	}
	return false
}

// RemoveMessageByTS removes a message with the given TS, adjusting the
// selected index so it remains valid. Returns true if the message was
// found and removed. Invalidates the render cache.
func (m *Model) RemoveMessageByTS(ts string) bool {
	for i, msg := range m.messages {
		if msg.TS == ts {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			if i <= m.selected && m.selected > 0 {
				m.selected--
			}
			if m.selected >= len(m.messages) {
				if len(m.messages) == 0 {
					m.selected = 0
				} else {
					m.selected = len(m.messages) - 1
				}
			}
			m.cache = nil
			m.dirty()
			return true
		}
	}
	return false
}

func (m *Model) UpdateReaction(messageTS, emojiName, userID string, remove bool) {
	// Capture pre-mutation viewport state so we can preserve "anchored at
	// bottom" across the reaction's height change. Without this, adding
	// the first reaction to the last visible message grows totalLines by
	// 1 while yOffset stays put -- the bottom message gets pushed below
	// the fold and a "-- more below --" hint replaces it. We want the
	// reaction to push content *up* (yOffset++) instead, keeping the
	// reacted-to message anchored to the bottom of the pane.
	wasAnchoredAtBottom := false
	oldTotalLines := m.totalLines
	if m.cache != nil && m.lastViewHeight > 0 && m.totalLines > 0 {
		wasAnchoredAtBottom = m.yOffset+m.lastViewHeight >= m.totalLines
	}

	for i, msg := range m.messages {
		if msg.TS == messageTS {
			if remove {
				for j, r := range msg.Reactions {
					if r.Emoji == emojiName {
						r.Count--
						if r.Count <= 0 {
							m.messages[i].Reactions = append(msg.Reactions[:j], msg.Reactions[j+1:]...)
						} else {
							r.HasReacted = false
							m.messages[i].Reactions[j] = r
						}
						break
					}
				}
			} else {
				found := false
				for j, r := range msg.Reactions {
					if r.Emoji == emojiName {
						r.Count++
						r.HasReacted = true
						m.messages[i].Reactions[j] = r
						found = true
						break
					}
				}
				if !found {
					m.messages[i].Reactions = append(m.messages[i].Reactions, ReactionItem{
						Emoji:      emojiName,
						Count:      1,
						HasReacted: true,
					})
				}
			}
			m.cache = nil
			m.dirty()
			if m.reactionNavActive {
				m.ClampReactionNav()
			}
			// Re-anchor the viewport bottom if it was at the bottom before.
			// We need the new totalLines, which only buildCache computes;
			// rebuild now (cheap, and the next View() would do it anyway).
			if wasAnchoredAtBottom && m.cacheWidth > 0 {
				m.buildCache(m.cacheWidth)
				if delta := m.totalLines - oldTotalLines; delta > 0 {
					m.yOffset += delta
					if maxOffset := m.totalLines - m.lastViewHeight; m.yOffset > maxOffset {
						m.yOffset = maxOffset
					}
					if m.yOffset < 0 {
						m.yOffset = 0
					}
					// Mark snapped so View() doesn't re-snap to the
					// (unchanged) selection and undo our adjustment.
					m.snappedSelection = m.selected
					m.hasSnapped = true
				}
			}
			return
		}
	}
}

func (m *Model) SetLoading(loading bool) {
	if m.loading != loading {
		m.loading = loading
		m.dirty()
	}
}

func (m *Model) SetAvatarFunc(fn AvatarFunc) {
	m.avatarFn = fn
}

// ResolveUserName returns the display name for a user ID, or empty string if unknown.
func (m *Model) ResolveUserName(userID string) string {
	if m.userNames == nil {
		return ""
	}
	return m.userNames[userID]
}

// SetUserNames sets the user ID -> display name map used to resolve @mentions.
func (m *Model) SetUserNames(names map[string]string) {
	m.userNames = names
	m.cache = nil // invalidate cache so mentions re-render
	m.dirty()
}

// SetChannelNames sets the channel ID -> name map used to resolve bare
// <#CHANNELID> mentions (Slack-side messages from clients that emit
// channel mentions without the embedded |name).
func (m *Model) SetChannelNames(names map[string]string) {
	m.channelNames = names
	m.cache = nil
	m.dirty()
}

// SetImageContext configures the inline-image rendering pipeline. Should
// be called once at startup, before the first View(). Subsequent calls
// invalidate the render cache. A zero-valued ImageContext (or one with
// Protocol == ProtoOff) disables inline rendering and falls back to the
// legacy single-line "[Image] <url>" form.
func (m *Model) SetImageContext(ctx ImageContext) {
	m.imgCtx = ctx
	if m.imgCtx.Fetcher != nil {
		m.imgCtx.Fetcher.ConfigurePrerender(m.imgCtx.Protocol)
		if m.imgCtx.Protocol == imgpkg.ProtoKitty {
			m.imgCtx.Fetcher.ConfigurePrerenderKitty(m.imgCtx.KittyRender)
		} else {
			m.imgCtx.Fetcher.ConfigurePrerenderKitty(nil)
		}
	}
	m.cache = nil
	m.dirty()
}

// SetLastReadTS sets the timestamp of the last read message.
// Messages with TS > lastReadTS are considered unread.
func (m *Model) SetLastReadTS(ts string) {
	if m.lastReadTS == ts {
		return
	}
	m.lastReadTS = ts
	m.cache = nil // invalidate render cache
	m.dirty()
}

// LastReadTS returns the current "last read" boundary timestamp. Used by
// tests; production code reads the field via SetLastReadTS-driven render.
func (m *Model) LastReadTS() string {
	return m.lastReadTS
}

func (m *Model) OldestTS() string {
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[0].TS
}

// cacheStyles bundles the lipgloss styles and pre-rendered strings that
// buildCache and partialRebuild share. Computing them once per
// (width, theme) avoids re-allocating identical lipgloss styles per
// message during the per-message render loop.
type cacheStyles struct {
	borderFill   lipgloss.Style
	borderInvis  lipgloss.Style
	borderSelect lipgloss.Style
	spacerLines  []string
}

// buildCacheStyles materializes the shared styles for a given width.
// Also writes m.cacheSpacer / m.cacheLoadingHint / m.cacheMoreBelow as
// a side effect so View()'s per-frame chrome rendering can reuse the
// same allocations. partialRebuild reuses the existing m.cacheSpacer
// (since width hasn't changed) and only needs the border styles.
func (m *Model) buildCacheStyles(width int) cacheStyles {
	borderFill := lipgloss.NewStyle().Background(styles.Background)
	borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.Background).BorderBackground(styles.Background)
	borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).BorderForeground(styles.SelectionBorderColor(m.focused)).BorderBackground(styles.Background)
	spacerBg := lipgloss.NewStyle().Background(styles.Background)
	m.cacheSpacer = spacerBg.Width(width).Render("")
	hintStyle := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted)
	m.cacheLoadingHint = hintStyle.Render("  Loading older messages...")
	m.cacheMoreBelow = hintStyle.Render("  -- more below --")
	return cacheStyles{
		borderFill:   borderFill,
		borderInvis:  borderInvis,
		borderSelect: borderSelect,
		spacerLines:  []string{m.cacheSpacer},
	}
}

// renderMessageEntry builds a single viewEntry for m.messages[i] using
// the shared cacheStyles. The returned entry is fully populated with
// both the unselected and selected pre-bordered line slices, the
// plain-text mirror, the per-frame image flushes, and the per-image
// hit rects -- i.e. it is a drop-in replacement for the slot at the
// caller's chosen cache index. The trailing spacer line is appended
// when i is not the last message, matching the layout invariant
// established in the full buildCache path.
//
// Pulled out of buildCache so partialRebuild can re-render a single
// stale slot (when an image lands for one message) without walking
// every other message in the channel. Runs zero string-scanning over
// sibling messages' content -- the perf win this method exists for.
func (m *Model) renderMessageEntry(i int, width int, cs cacheStyles) viewEntry {
	msg := m.messages[i]
	avatarStr := ""
	if m.avatarFn != nil {
		avatarStr = m.avatarFn(msg.UserID)
	}
	rendered, attachFlushes, attachSixel, attachHits := m.renderMessagePlain(msg, width, avatarStr, m.userNames, m.channelNames, i == m.selected)
	filled := cs.borderFill.Width(width - 1).Render(rendered)
	normal := cs.borderInvis.Render(filled)
	selected := cs.borderSelect.Render(filled)

	linesN := strings.Split(normal, "\n")
	linesS := strings.Split(selected, "\n")
	// linesPlain mirrors the UNBORDERED content (filled) so that the
	// thick left-border column is NOT present in plain text and never
	// bleeds into clipboard output via SelectionText. The mouse-column
	// to plain-column mapping happens in anchorAt via contentColOffset.
	linesP := plainLines(filled)
	// Append a trailing spacer line after every message except the last.
	// Both variants share the same spacer (it has no border styling).
	// The plain mirror of the spacer is the empty string -- selection
	// extraction trims trailing whitespace, and no real content lives
	// in the spacer row.
	if i < len(m.messages)-1 {
		linesN = append(linesN, cs.spacerLines...)
		linesS = append(linesS, cs.spacerLines...)
		linesP = append(linesP, plainLine{Text: "", Bytes: []int{0}})
	}
	return viewEntry{
		linesNormal:      linesN,
		linesSelected:    linesS,
		linesPlain:       linesP,
		contentColOffset: 1, // thick left border ▌ occupies column 0 of linesNormal
		height:           len(linesN),
		msgIdx:           i,
		flushes:          attachFlushes,
		sixelRows:        attachSixel,
		imageHits:        attachHits,
	}
}

// recomputeEntryOffsets walks m.cache and rewrites m.entryOffsets +
// m.totalLines from the cached per-entry heights. Cheap (O(N) integer
// adds with no rendering work), called by both buildCache (full) and
// partialRebuild (after a stale slot is replaced -- height may have
// changed when a placeholder was swapped for the real image).
func (m *Model) recomputeEntryOffsets() {
	if cap(m.entryOffsets) < len(m.cache) {
		m.entryOffsets = make([]int, len(m.cache))
	} else {
		m.entryOffsets = m.entryOffsets[:len(m.cache)]
	}
	off := 0
	for i, e := range m.cache {
		m.entryOffsets[i] = off
		off += e.height
	}
	m.totalLines = off
}

// buildCache pre-renders all messages and day separators, splitting each
// rendered string on "\n" so View() can flatten everything into the visible
// window with zero string-scanning per frame. Runs only on width / message-set
// / theme / reaction changes -- never on simple j/k navigation, and never
// on a single-image arrival (HandleImageReady marks one TS stale and the
// View()-time guard dispatches to partialRebuild instead).
func (m *Model) buildCache(width int) {
	m.cache = m.cache[:0]
	m.cacheWidth = width
	m.cacheMsgLen = len(m.messages)
	// A full rebuild supersedes any pending per-entry invalidations.
	for k := range m.staleEntries {
		delete(m.staleEntries, k)
	}

	if m.messageIDToEntryIdx == nil {
		m.messageIDToEntryIdx = make(map[string]int, len(m.messages))
	} else {
		for k := range m.messageIDToEntryIdx {
			delete(m.messageIDToEntryIdx, k)
		}
	}

	cs := m.buildCacheStyles(width)

	if cap(m.cache) < len(m.messages)+8 {
		m.cache = make([]viewEntry, 0, len(m.messages)+8)
	}

	appendSeparator := func(rendered string) {
		lines := strings.Split(rendered, "\n")
		m.cache = append(m.cache, viewEntry{
			linesNormal:   lines,
			linesSelected: lines,
			linesPlain:    plainLines(rendered),
			height:        len(lines),
			msgIdx:        -1,
		})
	}

	var lastDate string
	newMsgLandmarkInserted := false
	for i, msg := range m.messages {
		msgDate := dateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := formatDateSeparator(msgDate)
			sepStr := "── " + label + " ──"
			sep := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Bold(true).
				Width(width).Align(lipgloss.Center).
				Render(sepStr)
			appendSeparator(sep)
			lastDate = msgDate
		}

		// New message landmark: insert before the first unread message
		if m.lastReadTS != "" && !newMsgLandmarkInserted && msg.TS > m.lastReadTS {
			newStr := "── new ──"
			label := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.Error).Bold(true).
				Width(width).Align(lipgloss.Center).
				Render(newStr)
			appendSeparator(label)
			newMsgLandmarkInserted = true
		}

		m.messageIDToEntryIdx[msg.TS] = len(m.cache)
		m.cache = append(m.cache, m.renderMessageEntry(i, width, cs))
	}

	m.recomputeEntryOffsets()
}

// partialRebuild re-renders only the cache slots whose source-message
// TSes are in m.staleEntries, leaving sibling slots (other messages
// AND date / "── new ──" separators) intact. Called by View() when an
// ImageReadyMsg has marked one or more entries stale but neither the
// pane width nor the message-set length has changed since the last
// full buildCache.
//
// Critical perf invariant: this function MUST NOT call
// renderMessagePlain for any message that isn't in m.staleEntries.
// The whole point of per-entry invalidation is to skip the per-message
// lipgloss / wordwrap / blockkit work for siblings during an image
// burst.
//
// Preconditions enforced by the caller (the View()-time guard):
//   - m.cache != nil and m.cacheWidth == width and m.cacheMsgLen == len(m.messages)
//   - len(m.staleEntries) > 0
//
// Postcondition: m.staleEntries is empty.
func (m *Model) partialRebuild(width int) {
	cs := m.buildCacheStyles(width)
	for ts := range m.staleEntries {
		idx, ok := m.messageIDToEntryIdx[ts]
		if !ok {
			// Entry has gone away (message removed since the
			// invalidation was queued); silently drop.
			continue
		}
		if idx < 0 || idx >= len(m.cache) {
			continue
		}
		// Sanity: the slot must still belong to a message (msgIdx >= 0).
		// Separators are not addressable by TS and must never appear here.
		old := m.cache[idx]
		if old.msgIdx < 0 || old.msgIdx >= len(m.messages) {
			continue
		}
		m.cache[idx] = m.renderMessageEntry(old.msgIdx, width, cs)
	}
	for k := range m.staleEntries {
		delete(m.staleEntries, k)
	}
	// Heights may have changed (e.g. placeholder swapped for a real
	// image of the same reserved height -- equal in practice but we
	// don't rely on it), so reset the cumulative offsets from the
	// cached per-entry heights. No render work happens here.
	m.recomputeEntryOffsets()
}

// blockkitContext bundles the blockkit-package dependencies sourced
// from the model's image context, theme, and per-message identity.
// Wired here rather than in the constructor so it picks up runtime
// changes to imgCtx (e.g., when image_protocol is reconfigured).
func (m *Model) blockkitContext(msg MessageItem, userNames, channelNames map[string]string) blockkit.Context {
	send := m.imgCtx.SendMsg
	return blockkit.Context{
		Protocol:    m.imgCtx.Protocol,
		Fetcher:     m.imgCtx.Fetcher,
		KittyRender: m.imgCtx.KittyRender,
		CellPixels:  m.imgCtx.CellPixels,
		MaxRows:     m.imgCtx.MaxRows,
		MaxCols:     m.imgCtx.MaxCols,
		UserNames:   userNames,
		MessageTS:   msg.TS,
		Channel:     m.channelName,
		// Capture channelNames in a closure so blockkit's two-arg
		// RenderText signature stays stable; channel-name resolution
		// is a host concern.
		RenderText: func(s string, un map[string]string) string {
			return RenderSlackMarkdown(s, un, channelNames)
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

// renderMessagePlain renders a message without selection highlight.
//
// Returns the message content (multi-line string), per-frame flushes
// (kitty image upload callbacks), a row-keyed map of sixel bytes, and
// the per-image hit rects for the rendered attachments. Row indices in
// sixelRows AND in the returned hits are relative to linesNormal AFTER
// the surrounding buildCache wraps (avatar+border) are applied — the
// avatar gutter adds columns but no rows, and the border adds 1 column
// (col 0) but no rows, so row indices computed pre-wrap stay valid.
// Column extents in the returned hits are absolute display columns
// within linesNormal, including the border (col 0) and the avatar
// gutter (5 cols when an avatar is present), so View() can translate
// directly to pane-local mouse columns.
func (m *Model) renderMessagePlain(msg MessageItem, width int, avatarStr string, userNames map[string]string, channelNames map[string]string, isSelected bool) (
	content string, flushes []func(io.Writer) error, sixelRows map[int]sixelEntry, hits []entryHit,
) {
	line := styles.Username.Render(msg.UserName) + lipgloss.NewStyle().Background(styles.Background).Render("  ") + styles.Timestamp.Render(msg.Timestamp)

	// If we have an avatar, reserve space on the left for it
	contentWidth := width - 4
	if avatarStr != "" {
		contentWidth = width - 7 // 4 cols avatar + 1 space + 2 padding
	}
	if contentWidth < 20 {
		contentWidth = 20
	}

	text := styles.MessageText.Render(WordWrap(RenderSlackMarkdown(msg.Text, userNames, channelNames), contentWidth))

	var threadLine string
	if msg.ReplyCount > 0 {
		word := "replies"
		if msg.ReplyCount == 1 {
			word = "reply"
		}
		threadLine = "\n" + styles.ThreadIndicator.Render(
			fmt.Sprintf("[%d %s ->]", msg.ReplyCount, word))
	}

	var reactionLine string
	if len(msg.Reactions) > 0 {
		var pills []string
		for i, r := range msg.Reactions {
			// Drop any skin-tone modifier suffix so the pill renders the
			// base emoji at a well-known width. Skin-toned glyphs render
			// inconsistently across terminals and tend to break border
			// alignment regardless of how we measure them.
			emojiStr := emoji.Sprint(":" + emojiutil.StripSkinTone(r.Emoji) + ":")
			pillText := fmt.Sprintf("%s%d", emojiStr, r.Count)
			var style lipgloss.Style
			if isSelected && m.reactionNavActive && i == m.reactionNavIndex {
				style = styles.ReactionPillSelected
			} else if r.HasReacted {
				style = styles.ReactionPillOwn
			} else {
				style = styles.ReactionPillOther
			}
			pills = append(pills, style.Render(pillText))
		}
		if isSelected && m.reactionNavActive {
			plusStyle := styles.ReactionPillPlus
			if m.reactionNavIndex >= len(msg.Reactions) {
				plusStyle = styles.ReactionPillSelected
			}
			pills = append(pills, plusStyle.Render("+"))
		}
		// Join pills with wrapping. emojiutil.Width() consults the
		// terminal-probed width cache so wrapping decisions match what
		// the user's terminal will actually render.
		bgSpace := lipgloss.NewStyle().Background(styles.Background).Render(" ")
		var reactionLines []string
		currentLine := ""
		for i, pill := range pills {
			candidate := currentLine
			if i > 0 {
				candidate += bgSpace
			}
			candidate += pill
			if emojiutil.Width(candidate) > contentWidth && currentLine != "" {
				reactionLines = append(reactionLines, currentLine)
				currentLine = pill
			} else {
				currentLine = candidate
			}
		}
		if currentLine != "" {
			reactionLines = append(reactionLines, currentLine)
		}
		reactionLine = "\n" + strings.Join(reactionLines, "\n")
	}

	var editedMark string
	if msg.IsEdited {
		editedMark = " " + styles.Timestamp.Render("(edited)")
	}

	// Pre-attachment row count, so attachment rows can compute their
	// absolute row index (used as the sixelRows key).
	//
	//   row 0: broadcastLabel (only when subtype=thread_broadcast)
	//   row 0|1: username line + editedMark
	//   row N: wrapped body text (lipgloss.Height of styled `text`)
	//
	// Attachments begin immediately after the body text.
	var broadcastLabel string
	preAttachmentRows := 0
	if msg.Subtype == "thread_broadcast" {
		broadcastLabel = styles.Timestamp.Render("\u21b3 replied to a thread") + "\n"
		preAttachmentRows++ // the broadcast label occupies its own row
	}
	preAttachmentRows++                       // username + ts row
	preAttachmentRows += lipgloss.Height(text) // wrapped body text

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
	// We compute it here (before calling renderAttachmentBlock) and
	// pass it down so the per-image hit rects come back in absolute
	// linesNormal columns, ready for direct viewport translation in
	// View() without any further offset arithmetic.
	contentColBase := 1
	if avatarStr != "" {
		contentColBase += 5
	}

	var attachLineSlices [][]string
	var allFlushes []func(io.Writer) error
	allSixel := map[int]sixelEntry{}

	// Block Kit blocks render between the body text and file attachments.
	bkCtx := m.blockkitContext(msg, userNames, channelNames)
	var bkLines []string
	bkInteractive := false
	if len(msg.Blocks) > 0 {
		startInBk := len(bkLines)
		res := blockkit.Render(msg.Blocks, bkCtx, contentWidth)
		bkLines = append(bkLines, res.Lines...)
		allFlushes = append(allFlushes, res.Flushes...)
		rowOffset := preAttachmentRows + startInBk
		for k, v := range res.SixelRows {
			allSixel[rowOffset+k] = sixelEntry{bytes: v.Bytes, fallback: v.Fallback, height: v.Height}
		}
		// Note: res.Hits is intentionally NOT appended to `hits` in
		// v1. App-level click routing (app.go) currently uses entryHit
		// to look up file attachments by attIdx; routing for "BK-"
		// (Block Kit URL-keyed) hits is deferred. Recording them here
		// without routing would mis-route clicks to msg.Attachments[0].
		// See Phase 7 of the plan for the future wiring.
		_ = res.Hits
		bkInteractive = bkInteractive || res.Interactive
	}
	if len(msg.LegacyAttachments) > 0 {
		startInBk := len(bkLines)
		res := blockkit.RenderLegacy(msg.LegacyAttachments, bkCtx, contentWidth)
		bkLines = append(bkLines, res.Lines...)
		allFlushes = append(allFlushes, res.Flushes...)
		rowOffset := preAttachmentRows + startInBk
		for k, v := range res.SixelRows {
			allSixel[rowOffset+k] = sixelEntry{bytes: v.Bytes, fallback: v.Fallback, height: v.Height}
		}
		// Note: res.Hits is intentionally NOT appended to `hits` in
		// v1. App-level click routing (app.go) currently uses entryHit
		// to look up file attachments by attIdx; routing for "BK-"
		// (Block Kit URL-keyed) hits is deferred. Recording them here
		// without routing would mis-route clicks to msg.Attachments[0].
		// See Phase 7 of the plan for the future wiring.
		_ = res.Hits
		bkInteractive = bkInteractive || res.Interactive
	}
	if bkInteractive {
		hint := styles.Timestamp.Render("↗ open in Slack to interact")
		bkLines = append(bkLines, hint)
	}
	preAttachmentRows += len(bkLines)

	bkBlock := ""
	if len(bkLines) > 0 {
		bkBlock = "\n" + strings.Join(bkLines, "\n")
	}

	if len(msg.Attachments) > 0 {
		rowCursor := preAttachmentRows
		for attIdx, att := range msg.Attachments {
			lines, fls, sxl, h, hit := m.renderAttachmentBlock(att, msg.TS, contentWidth, rowCursor, attIdx, contentColBase)
			attachLineSlices = append(attachLineSlices, lines)
			allFlushes = append(allFlushes, fls...)
			for k, v := range sxl {
				allSixel[k] = v
			}
			// Only record hits for actual image-block renders (the
			// fallback-to-link path returns a zero entryHit, which we
			// skip — clicking a single-line "[Image] <url>" doesn't
			// open the preview overlay; that's a hyperlink.).
			if hit.rowEndInEntry > hit.rowStartInEntry {
				hits = append(hits, hit)
			}
			rowCursor += h
		}
	}
	var attachmentLines string
	if len(attachLineSlices) > 0 {
		flat := make([]string, 0)
		for _, ls := range attachLineSlices {
			flat = append(flat, ls...)
		}
		attachmentLines = "\n" + strings.Join(flat, "\n")
	}

	msgContent := broadcastLabel + line + editedMark + "\n" + text + bkBlock + attachmentLines + threadLine + reactionLine

	// Place avatar next to message content (avatar is side-by-side, no
	// extra rows; row indices for sixelRows remain valid).
	if avatarStr != "" {
		msgContent = placeAvatarBeside(avatarStr, msgContent)
	}

	if len(allSixel) == 0 {
		allSixel = nil
	}
	return msgContent, allFlushes, allSixel, hits
}



// placeAvatarBeside renders the avatar to the left of the message content.
// The avatar is 4 cols wide, 2 rows tall. Message content flows to the right.
func placeAvatarBeside(avatar, content string) string {
	avatarLines := strings.Split(avatar, "\n")
	contentLines := strings.Split(content, "\n")

	// Pad avatar to consistent width (4 visible chars + reset codes)
	avatarWidth := 5 // 4 chars + 1 space gap

	var result []string
	maxLines := len(contentLines)
	if len(avatarLines) > maxLines {
		maxLines = len(avatarLines)
	}

	for i := 0; i < maxLines; i++ {
		var left, right string

		if i < len(avatarLines) {
			left = avatarLines[i] + lipgloss.NewStyle().Background(styles.Background).Render(" ")
		} else {
			// Empty space where avatar was (maintain alignment)
			left = lipgloss.NewStyle().Background(styles.Background).Width(avatarWidth).Render("")
		}

		if i < len(contentLines) {
			right = contentLines[i]
		}

		result = append(result, left+right)
	}

	return strings.Join(result, "\n")
}

// renderAttachmentBlock returns the rendered rows + per-frame flushes +
// sixel sentinel rows + the image-hit footprint (for click-to-preview)
// for one attachment. baseRow is the absolute row index where this
// attachment's first row will land within the containing message's
// rendered output (used as the key for sixelRows AND as the row-base
// for the returned hit rect). attIdx is the index of this attachment
// within its parent MessageItem.Attachments slice; preserved on the
// returned hit so the mouse handler can identify which attachment was
// clicked when a message has multiple. contentColBase is the display
// column at which the message-content area begins within the entry's
// rendered linesNormal — i.e., the offset added by the avatar gutter
// (when present) plus the thick-left-border (1 col, added by
// buildCache). The returned entryHit's colStart/colEnd are absolute
// columns within linesNormal so View() can translate them directly.
//
// Behavior matrix:
//   - Non-image attachment, ProtoOff, missing fetcher, or missing thumbs:
//     fall back to the legacy single-line "[Image|File] <url>" form.
//     No hit rect (hit.rowEndInEntry == hit.rowStartInEntry).
//   - Image with bytes already in the on-disk cache: render via the active
//     protocol's renderer (kitty stable-key path, halfblock inline, or
//     sixel sentinel + bytes). Hit rect spans the full image extent.
//   - Image not yet cached: emit a reserved-height placeholder block of
//     theme-surface-colored spaces with a centered loading indicator,
//     and kick off an async prefetch via go func. The placeholder is
//     ALSO clickable — clicking it before the bytes arrive is a no-op
//     for the preview overlay (Phase 7's open handler guards), but it
//     gives the user immediate feedback that the row is interactive.
func (m *Model) renderAttachmentBlock(att Attachment, ts string, availWidth, baseRow, attIdx, contentColBase int) (
	lines []string, flushes []func(io.Writer) error, sixelRows map[int]sixelEntry, height int, hit entryHit,
) {
	ctx := m.imgCtx
	if att.Kind != "image" || ctx.Protocol == imgpkg.ProtoOff || ctx.Fetcher == nil {
		return []string{renderSingleAttachment(att)}, nil, nil, 1, entryHit{}
	}
	// Defensive: an image attachment without a FileID can't form a
	// stable cache key, so there's nothing the inline pipeline can do
	// with it. Fall back to the single-line link form.
	if att.FileID == "" {
		return []string{renderSingleAttachment(att)}, nil, nil, 1, entryHit{}
	}

	target := computeImageTarget(att, ctx, availWidth)
	if target.X <= 0 || target.Y <= 0 {
		return []string{renderSingleAttachment(att)}, nil, nil, 1, entryHit{}
	}

	pixelTarget := image.Pt(target.X*ctx.CellPixels.X, target.Y*ctx.CellPixels.Y)
	url, suffix := imgpkg.PickThumb(toImgThumbs(att.Thumbs), pixelTarget)
	if url == "" {
		return []string{renderSingleAttachment(att)}, nil, nil, 1, entryHit{}
	}
	key := att.FileID + "-" + suffix

	// The hit rect spans the image's reserved cell extent (target.X cells
	// wide × target.Y rows tall), starting at baseRow within the entry's
	// linesNormal and at contentColBase within each of those lines.
	hit = entryHit{
		rowStartInEntry: baseRow,
		rowEndInEntry:   baseRow + target.Y,
		colStart:        contentColBase,
		colEnd:          contentColBase + target.X,
		fileID:          att.FileID,
		attIdx:          attIdx,
	}

	img, cached := ctx.Fetcher.Cached(key, pixelTarget)
	if !cached {
		// Reserved-height placeholder; kick off async prefetch. The
		// fetcher is concurrency-safe (singleflight dedupes), so it's
		// fine for multiple visible messages to fire prefetches for
		// the same key in parallel — but each goroutine still
		// independently fires ImageReadyMsg, which can pile up under
		// scroll/resize churn. Gate spawn on an in-flight set so each
		// (key) only has one goroutine in flight at a time.
		if m.fetchingImages == nil {
			m.fetchingImages = make(map[string]struct{})
		}
		// Skip re-spawn for previously-failed keys. The failure set
		// clears on SetChannel so the user can retry by switching
		// channels.
		if _, failed := m.failedImages[key]; failed {
			return buildPlaceholder(att.Name, target), nil, nil, target.Y, hit
		}
		if _, inFlight := m.fetchingImages[key]; inFlight {
			return buildPlaceholder(att.Name, target), nil, nil, target.Y, hit
		}
		m.fetchingImages[key] = struct{}{}
		channel := m.channelName
		go func() {
			_, err := ctx.Fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
				Key:        key,
				URL:        url,
				Target:     pixelTarget,
				CellTarget: target,
			})
			// On either success or failure, dispatch a message so the
			// model can clear THIS key's in-flight bit. Without that,
			// failed fetches leave a permanent "fetching" placeholder
			// AND get re-attempted on every successful sibling's
			// arrival (sibling success used to nil the entire in-flight
			// set, causing fetch stampedes that locked up the UI on
			// channels with many failing-to-auth Slack Connect images).
			if ctx.SendMsg == nil {
				return
			}
			if err != nil {
				log.Printf("image fetch failed: key=%s url=%s err=%v", key, url, err)
				ctx.SendMsg(ImageFailedMsg{Key: key})
				return
			}
			ctx.SendMsg(ImageReadyMsg{Channel: channel, TS: ts, Key: key})
		}()
		return buildPlaceholder(att.Name, target), nil, nil, target.Y, hit
	}

	// Fast path: prerendered output baked by the fetch goroutine off
	// the UI thread. This is the hot path post-Fetch — no protocol
	// encoding runs on the bubbletea Update goroutine.
	if pr, ok := ctx.Fetcher.Prerendered(key, target, ctx.Protocol); ok {
		var fl []func(io.Writer) error
		var sxlMap map[int]sixelEntry
		if ctx.Protocol == imgpkg.ProtoSixel && pr.OnFlush != nil {
			var bb bytes.Buffer
			if err := pr.OnFlush(&bb); err == nil {
				sxlMap = map[int]sixelEntry{
					baseRow: {bytes: bb.Bytes(), fallback: pr.Fallback, height: target.Y},
				}
			}
		} else if pr.OnFlush != nil {
			fl = []func(io.Writer) error{pr.OnFlush}
		}
		return pr.Lines, fl, sxlMap, target.Y, hit
	}

	// Slow path: prerender wasn't populated (e.g. protocol changed
	// since fetch, or this is a cache file from a previous session
	// that hasn't been refreshed through Fetch). Fall through to the
	// existing on-thread RenderImage path. Cached() already returned
	// the decoded image as a pure memo lookup (Task 1) so the
	// remaining cost is one protocol encode.
	if ctx.Protocol == imgpkg.ProtoKitty && ctx.KittyRender != nil {
		ckey := "F-" + att.FileID
		ctx.KittyRender.SetSource(ckey, img)
		out := ctx.KittyRender.RenderKey(ckey, target)
		var fl []func(io.Writer) error
		if out.OnFlush != nil {
			fl = []func(io.Writer) error{out.OnFlush}
		}
		return out.Lines, fl, nil, target.Y, hit
	}

	out := imgpkg.RenderImage(ctx.Protocol, img, target)
	var fl []func(io.Writer) error
	var sxlMap map[int]sixelEntry
	if ctx.Protocol == imgpkg.ProtoSixel {
		if out.OnFlush != nil {
			var bb bytes.Buffer
			if err := out.OnFlush(&bb); err == nil {
				sxlMap = map[int]sixelEntry{
					baseRow: {bytes: bb.Bytes(), fallback: out.Fallback, height: target.Y},
				}
			}
		}
	} else if out.OnFlush != nil {
		fl = []func(io.Writer) error{out.OnFlush}
	}
	return out.Lines, fl, sxlMap, target.Y, hit
}

// computeImageTarget chooses (cols, rows) for an inline image render.
// rows is capped at ctx.MaxRows; cols is capped at min(availWidth,
// ctx.MaxCols). Aspect ratio is always preserved. Returns image.Point{}
// when the attachment has no usable thumbnail or the cell metrics are
// zero.
func computeImageTarget(att Attachment, ctx ImageContext, availWidth int) image.Point {
	if len(att.Thumbs) == 0 || ctx.CellPixels.X <= 0 || ctx.CellPixels.Y <= 0 {
		return image.Point{}
	}
	largest := att.Thumbs[len(att.Thumbs)-1]
	if largest.W <= 0 || largest.H <= 0 {
		return image.Point{}
	}
	aspect := float64(largest.W) / float64(largest.H)
	cellRatio := float64(ctx.CellPixels.X) / float64(ctx.CellPixels.Y)

	rows := ctx.MaxRows
	if rows <= 0 {
		rows = 20
	}
	// Width cap is the smaller of the user's MaxCols and the available
	// message-pane width. MaxCols == 0 disables the user cap.
	maxCols := availWidth
	if ctx.MaxCols > 0 && ctx.MaxCols < maxCols {
		maxCols = ctx.MaxCols
	}
	// cols = rows * aspect / cellRatio  (cells are taller than wide,
	// so cellRatio < 1 inflates cols to compensate).
	cols := int(float64(rows) * aspect / cellRatio)
	if cols < 1 {
		// Extreme tall/skinny aspect ratios (e.g., 100×9000 px scrolling
		// screenshots) underflow cols to zero. Clamp to 1 col and let
		// the renderer downscale the source to fit. Without this clamp
		// the attachment falls through to the "[Image] <url>" text
		// fallback, which the user perceives as a missing image.
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
		rows = int(float64(cols) * cellRatio / aspect)
	}
	if rows < 1 {
		rows = 1
	}
	return image.Pt(cols, rows)
}

// toImgThumbs converts the messages-package ThumbSpec slice to the
// image-package's. The two types are intentionally distinct (see
// model.go ThumbSpec doc) so the messages UI doesn't depend on the
// image package's internal type.
func toImgThumbs(ts []ThumbSpec) []imgpkg.ThumbSpec {
	out := make([]imgpkg.ThumbSpec, len(ts))
	for i, t := range ts {
		out[i] = imgpkg.ThumbSpec{URL: t.URL, W: t.W, H: t.H}
	}
	return out
}

// buildPlaceholder produces a target.Y-row block with theme-surface
// background and a centered "⏳ Loading <name>..." indicator on the
// middle row. Used while the image bytes are being fetched.
func buildPlaceholder(name string, target image.Point) []string {
	bg := lipgloss.NewStyle().Background(styles.SurfaceDark)
	pad := strings.Repeat(" ", target.X)
	emptyRow := bg.Render(pad)

	lines := make([]string, target.Y)
	for i := range lines {
		lines[i] = emptyRow
	}

	label := "⏳ Loading " + name + "..."
	labelW := lipgloss.Width(label)
	if labelW > target.X {
		// Truncate to fit. Use rune-safe slicing via a runes round-trip
		// — image labels are user-controlled file names and can contain
		// multi-byte UTF-8.
		if target.X > 1 {
			runes := []rune(label)
			// Conservatively trim to target.X-1 runes plus an ellipsis;
			// some runes are wide so the result may still be slightly
			// over, in which case we re-trim.
			for len(runes) > 0 {
				candidate := string(runes[:len(runes)-1]) + "…"
				if lipgloss.Width(candidate) <= target.X {
					label = candidate
					labelW = lipgloss.Width(label)
					break
				}
				runes = runes[:len(runes)-1]
			}
			if labelW > target.X {
				return lines
			}
		} else {
			return lines
		}
	}
	leftPad := (target.X - labelW) / 2
	rightPad := target.X - labelW - leftPad
	mid := target.Y / 2
	lines[mid] = bg.Render(strings.Repeat(" ", leftPad)) + bg.Render(label) + bg.Render(strings.Repeat(" ", rightPad))
	return lines
}

// ClickAt handles a mouse click at the given y-coordinate (the pane-local
// y returned by App.panelAt, which is measured from the panel's top border —
// so y=0..chromeHeight-1 is the channel header / separator chrome and
// y=chromeHeight onward is message content). Selects the message at that
// position; clicks in the chrome are ignored.
func (m *Model) ClickAt(y int) {
	contentY := y - m.chromeHeight
	if contentY < 0 {
		return // click on chrome (header / separator) — ignore
	}
	absoluteY := contentY + m.yOffset

	// Walk through cached view entries to find which message is at this line
	currentLine := 0
	for _, entry := range m.cache {
		if entry.msgIdx < 0 {
			// Date separator or "new messages" line — skip
			currentLine += entry.height
			continue
		}
		if absoluteY >= currentLine && absoluteY < currentLine+entry.height {
			if m.selected != entry.msgIdx {
				m.selected = entry.msgIdx
				m.dirty()
			}
			return
		}
		currentLine += entry.height
	}
}

// HitTest returns the message index, attachment index, and Slack file
// ID of an inline image rendered at (row, col) within the messages-pane
// content area, or ok=false when no image footprint covers that cell.
//
// Coordinate frame: row=0 is the FIRST row of message content (i.e.,
// just below the channel-header chrome); col=0 is the leftmost column
// of the messages pane. The app-level mouse handler is responsible for
// subtracting the panel border (handled by panelAt) AND the chrome
// height before calling HitTest, mirroring the convention used by
// ClickAt and BeginSelectionAt. Out-of-range coordinates always return
// ok=false; the method does no scrolling, mutation, or side effects.
//
// Hits are populated from the most recent View() call. Callers must
// invoke View at least once between cache invalidations and a HitTest
// query, which is the natural order in a tea.Program: View runs every
// frame, then a click event arrives.
//
// In Phase 7 the returned (msgIdx, attIdx, fileID) tuple is wrapped in
// an OpenImagePreviewMsg and dispatched from the app-level mouse
// handler. Clicking a placeholder (image still loading) is a valid hit
// — the open handler guards against "no bytes yet" and is a no-op for
// the preview overlay in that case.
func (m *Model) HitTest(row, col int) (msgIdx, attIdx int, fileID string, ok bool) {
	for _, h := range m.lastHits {
		if row >= h.rowStart && row < h.rowEnd && col >= h.colStart && col < h.colEnd {
			return h.msgIdx, h.attIdx, h.fileID, true
		}
	}
	return 0, 0, "", false
}

var thickLeftBorder = lipgloss.Border{Left: "▌"}

// absoluteLineAt returns the global line index in the flattened cache for a
// pane-local y coordinate. The incoming viewportY is what App.panelAt
// returns: zero at the top of the panel's content area (just below the
// panel border), so rows 0..chromeHeight-1 are the channel header /
// separator chrome and chromeHeight onward is message content. We strip the
// chrome offset before mapping into cache lines, and clamp negative
// (in-chrome) values to the first content line. The result is clamped to
// [0, totalLines-1] for out-of-range inputs.
func (m *Model) absoluteLineAt(viewportY int) int {
	contentY := viewportY - m.chromeHeight
	if contentY < 0 {
		contentY = 0
	}
	abs := contentY + m.yOffset
	if abs < 0 {
		abs = 0
	}
	if m.totalLines > 0 && abs >= m.totalLines {
		abs = m.totalLines - 1
	}
	return abs
}

// anchorAt converts an absolute line index + display column into an
// Anchor. Returns ok=false when no entry covers the line (empty cache).
// Anchors on separator entries (msgIdx < 0) carry MessageID == "" so
// downstream code can recognize them as line boundaries.
//
// The incoming col is a MOUSE column (relative to linesNormal[i]). The
// stored Anchor.Col is a PLAIN column (relative to linesPlain[i]); the
// two differ by the entry's contentColOffset (e.g. the thick left
// border on message entries occupies one mouse column but no plain
// column). Mouse columns falling inside the chrome (col < offset) clamp
// to plain col 0.
func (m *Model) anchorAt(absLine, col int) (selection.Anchor, bool) {
	for i, e := range m.cache {
		start := m.entryOffsets[i]
		end := start + e.height
		if absLine < start || absLine >= end {
			continue
		}
		lineIdx := absLine - start
		// Translate mouse column -> plain column.
		plainCol := col - e.contentColOffset
		if plainCol < 0 {
			plainCol = 0
		}
		// Clamp plainCol to the plain-line's width so we never anchor
		// past the end of visible content.
		if lineIdx < len(e.linesPlain) {
			if w := displayWidthOfPlain(e.linesPlain[lineIdx]); plainCol > w {
				plainCol = w
			}
		}
		var msgID string
		if e.msgIdx >= 0 && e.msgIdx < len(m.messages) {
			msgID = m.messages[e.msgIdx].TS
		}
		return selection.Anchor{MessageID: msgID, Line: lineIdx, Col: plainCol}, true
	}
	return selection.Anchor{}, false
}

// snapToMessageAnchor takes an Anchor that may sit on a separator entry
// (MessageID == "") and returns an equivalent Anchor pointing at a real
// message. Snaps forward to the next message's first line; if none
// exists forward, snaps backward to the previous message's last content
// line. Returns ok=false when no real-message entry exists in the cache.
// Real-message anchors pass through unchanged.
func (m *Model) snapToMessageAnchor(a selection.Anchor, absLine int) (selection.Anchor, bool) {
	if a.MessageID != "" {
		return a, true
	}
	// Find the entry covering absLine, then walk forward looking for a
	// real message; if none, walk backward.
	startEntry := -1
	for i, e := range m.cache {
		s := m.entryOffsets[i]
		if absLine >= s && absLine < s+e.height {
			startEntry = i
			break
		}
	}
	if startEntry < 0 {
		return selection.Anchor{}, false
	}
	for i := startEntry + 1; i < len(m.cache); i++ {
		e := m.cache[i]
		if e.msgIdx >= 0 && e.msgIdx < len(m.messages) {
			return selection.Anchor{MessageID: m.messages[e.msgIdx].TS, Line: 0, Col: 0}, true
		}
	}
	for i := startEntry - 1; i >= 0; i-- {
		e := m.cache[i]
		if e.msgIdx >= 0 && e.msgIdx < len(m.messages) {
			lastLine := e.height - 1
			col := 0
			if lastLine < len(e.linesPlain) {
				col = displayWidthOfPlain(e.linesPlain[lastLine])
			}
			return selection.Anchor{MessageID: m.messages[e.msgIdx].TS, Line: lastLine, Col: col}, true
		}
	}
	return selection.Anchor{}, false
}

// resolveAnchor returns the absolute line + col for an Anchor, using the
// current cache. Returns ok=false when the message is no longer present
// (deleted, or cache rebuilt for a different channel) or when MessageID
// is empty (separator anchors don't survive cache rebuilds).
func (m *Model) resolveAnchor(a selection.Anchor) (absLine, col int, ok bool) {
	if a.MessageID == "" {
		return 0, 0, false
	}
	idx, found := m.messageIDToEntryIdx[a.MessageID]
	if !found || idx >= len(m.cache) {
		return 0, 0, false
	}
	e := m.cache[idx]
	if a.Line < 0 || a.Line >= e.height {
		return 0, 0, false
	}
	return m.entryOffsets[idx] + a.Line, a.Col, true
}

// BeginSelectionAt anchors a new selection at the given pane-local
// coordinates. The selection becomes Active. Coordinates are clamped to
// the rendered area; out-of-range inputs are silently no-ops. Clicks on
// the chrome (channel header / separator at pane-local y < chromeHeight)
// are ignored — there's no message content there to anchor on. If the
// click lands on a separator entry inside the message area, the anchor
// snaps to the nearest real message.
func (m *Model) BeginSelectionAt(viewportY, x int) {
	if viewportY < m.chromeHeight {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	a, ok = m.snapToMessageAnchor(a, abs)
	if !ok {
		return
	}
	m.selRange = selection.Range{Start: a, End: a, Active: true}
	m.hasSelection = true
	m.dirty()
}

// ExtendSelectionAt updates the End anchor of the active selection.
// No-op if BeginSelectionAt was never called. Separator anchors snap
// to the nearest real message.
func (m *Model) ExtendSelectionAt(viewportY, x int) {
	if !m.hasSelection {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	a, ok = m.snapToMessageAnchor(a, abs)
	if !ok {
		return
	}
	m.selRange.End = a
	m.dirty()
}

// EndSelection finalizes the drag, returning the plain-text contents of
// the selection. Returns ok=false when the selection is empty (a click
// without drag). The selection itself remains visible until ClearSelection
// is called or a new drag begins.
func (m *Model) EndSelection() (string, bool) {
	if !m.hasSelection {
		return "", false
	}
	m.selRange.Active = false
	if m.selRange.IsEmpty() {
		m.hasSelection = false
		m.selRange = selection.Range{}
		m.dirty()
		return "", false
	}
	text := m.SelectionText()
	m.dirty()
	if text == "" {
		return "", false
	}
	return text, true
}

// ClearSelection removes the current selection, if any.
func (m *Model) ClearSelection() {
	if !m.hasSelection {
		return
	}
	m.hasSelection = false
	m.selRange = selection.Range{}
	m.dirty()
}

// HasSelection reports whether a selection is currently active or
// pinned-on-screen post-drag.
func (m *Model) HasSelection() bool {
	return m.hasSelection
}

// ScrollHintForDrag returns -1 if the cursor is within 1 row of the top
// edge of the message-content area, +1 if within 1 row of the bottom, else 0.
// The incoming viewportY is pane-local (0 == top of panel content, just
// below the border); we offset by m.chromeHeight so "top edge" is measured
// against the message content, not the channel header. A cursor sitting on
// the chrome (or above) is treated the same as the top content row, so an
// upward drag continues to auto-scroll toward older messages.
func (m *Model) ScrollHintForDrag(viewportY int) int {
	h := m.lastViewHeight
	if h <= 0 {
		return 0
	}
	contentY := viewportY - m.chromeHeight
	if contentY <= 0 {
		return -1
	}
	if contentY >= h-1 {
		return +1
	}
	return 0
}

// SelectionText extracts the plain-text contents of the current
// selection. Trailing whitespace is trimmed per line; a final trailing
// newline is removed. Multi-rune grapheme clusters (ZWJ, skin-tone
// modifiers, ❤️+VS16) are preserved intact.
func (m *Model) SelectionText() string {
	if !m.hasSelection || m.selRange.IsEmpty() {
		return ""
	}
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 {
		return ""
	}
	if loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return ""
	}

	var b strings.Builder
	for i, e := range m.cache {
		entryStart := m.entryOffsets[i]
		entryEnd := entryStart + e.height
		if entryEnd <= loLine {
			continue
		}
		if entryStart > hiLine {
			break
		}
		for j, plain := range e.linesPlain {
			absLine := entryStart + j
			if absLine < loLine {
				continue
			}
			if absLine > hiLine {
				break
			}
			from := 0
			to := displayWidthOfPlain(plain)
			if absLine == loLine {
				from = loCol
			}
			if absLine == hiLine {
				to = hiCol
			}
			seg := sliceColumns(plain, from, to)
			seg = strings.TrimRight(seg, " ")
			b.WriteString(seg)
			if absLine != hiLine {
				b.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) View(height, width int) string {
	// Chrome (header + separator) is cached; only rebuilt on width / channel
	// name / topic change. This avoids per-keypress strings.Repeat + lipgloss
	// renders that don't depend on the selection.
	if !m.chromeCacheValid || m.chromeWidth != width || m.chromeChannel != m.channelName || m.chromeTopic != m.channelTopic || m.chromeChannelType != m.channelType {
		// Channel title sits in the message pane, immediately inside the
		// panel's top border. Bold + TextPrimary on Background matches
		// the surrounding messages. The panel border itself provides
		// visual separation from the sidebar -- we deliberately do NOT
		// emit a horizontal separator below the title here, because that
		// produced a "border above the channel name and another border-
		// looking line below it" effect that visually separated the
		// header from the messages and made the title look like it sat
		// outside the panel.
		headerStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.TextPrimary).
			Bold(true).
			Padding(0, 1)
		header := headerStyle.Render(fmt.Sprintf("%s %s", channelGlyph(m.channelType), m.channelName))
		if m.channelTopic != "" {
			header += "\n" + styles.Timestamp.Render(WordWrap(m.channelTopic, width))
		}
		m.chromeCache = header
		m.chromeHeight = lipgloss.Height(m.chromeCache)
		m.chromeWidth = width
		m.chromeChannel = m.channelName
		m.chromeTopic = m.channelTopic
		m.chromeChannelType = m.channelType
		m.chromeCacheValid = true
	}
	chrome := m.chromeCache
	chromeHeight := m.chromeHeight

	msgAreaHeight := height - chromeHeight
	if msgAreaHeight < 1 {
		msgAreaHeight = 1
	}
	// If the available height shrank since the last render (e.g. the
	// compose box grew an extra row to fit a soft-wrapped line) and the
	// viewport was anchored at the bottom, advance yOffset by the delta
	// so the bottom of the content stays pinned. Without this, the
	// last message would be pushed below the fold and the
	// "-- more below --" hint would replace it on the next render.
	if m.lastViewHeight > 0 && msgAreaHeight < m.lastViewHeight && m.totalLines > 0 &&
		m.yOffset+m.lastViewHeight >= m.totalLines {
		delta := m.lastViewHeight - msgAreaHeight
		m.yOffset += delta
		// Mark "snapped" so the selection-snap branch below doesn't undo
		// the adjustment if selection is unchanged. The post-snap clamp
		// at the end of View() still bounds yOffset to maxOffset.
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	m.lastViewHeight = msgAreaHeight

	if len(m.messages) == 0 {
		text := "No messages yet"
		if m.loading {
			text = "Loading messages..."
		}
		empty := lipgloss.NewStyle().
			Width(width).
			Height(msgAreaHeight).
			Foreground(styles.TextMuted).
			Background(styles.Background).
			Render(text)
		return chrome + "\n" + empty
	}

	// Rebuild cache if messages or width changed. If only individual
	// message TSes have been marked stale (e.g. by HandleImageReady
	// landing one image at a time), take the targeted partial-rebuild
	// path that re-renders only those slots and reuses every other
	// entry verbatim -- the perf invariant for image bursts.
	switch {
	case m.cache == nil || m.cacheWidth != width || m.cacheMsgLen != len(m.messages):
		m.buildCache(width)
	case len(m.staleEntries) > 0:
		m.partialRebuild(width)
	}

	entries := m.cache

	// Locate selected entry's line range. O(N) scan over entryOffsets; cheap.
	m.selectedStartLine = 0
	m.selectedEndLine = 0
	for i, e := range entries {
		if e.msgIdx == m.selected {
			m.selectedStartLine = m.entryOffsets[i]
			m.selectedEndLine = m.selectedStartLine + e.height
			// The trailing spacer is part of e.height; subtract it from the
			// scroll-to-keep-visible target so we don't push the spacer into
			// view above the selection.
			if i < len(entries)-1 && e.msgIdx >= 0 {
				m.selectedEndLine--
			}
			break
		}
	}

	// Adjust yOffset to keep selection visible -- but only when the selection
	// has actually changed since the last snap. This lets the mouse wheel
	// (or programmatic ScrollUp/Down) move the viewport away from the
	// selected message without the next render yanking it back.
	if !m.hasSnapped || m.snappedSelection != m.selected {
		if m.selectedEndLine > m.yOffset+msgAreaHeight {
			m.yOffset = m.selectedEndLine - msgAreaHeight
		}
		if m.selectedStartLine < m.yOffset {
			m.yOffset = m.selectedStartLine
		}
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}
	if m.yOffset < 0 {
		m.yOffset = 0
	}
	maxOffset := m.totalLines - msgAreaHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.yOffset > maxOffset {
		m.yOffset = maxOffset
	}

	// Build the visible window directly from per-entry pre-split line slices.
	// No lipgloss, no uniseg, no width measurement.
	//
	// Per-frame image side effects:
	//   * Kitty image uploads (entry.flushes) are collected from every
	//     visible entry and rendered into a single byte stream that is
	//     prepended to the first visible line. Repeat invocations of an
	//     upload escape for the same image ID are benign (the terminal
	//     discards them); the kitty registry inside the renderer also
	//     dedupes at first-render-of-id, so most frames have nothing to
	//     emit at all.
	//   * Sixel images (entry.sixelRows) are emitted INLINE as bytes
	//     appended to the first row of the image's sentinel placeholder
	//     — but only when the image's full vertical extent fits in the
	//     visible window. Under partial visibility (image clipped at the
	//     top or bottom edge), the per-row halfblock fallback substitutes
	//     for the visible rows so users always see something. Acknowledged
	//     tradeoff: visible flicker as a sixel image scrolls past an edge,
	//     in exchange for correctness under arbitrary scroll positions.
	// Reset the per-frame hit-rect slice. Capacity is preserved so the
	// common case (a few visible images at most) reuses the underlying
	// array without reallocation.
	m.lastHits = m.lastHits[:0]

	visible := make([]string, 0, msgAreaHeight)
	want := msgAreaHeight
	// kittyFlushBuf collects the APC upload bytes for any kitty images
	// that need uploading this frame. Per spec, the upload escape and
	// placeholder cells must reach the terminal in the same byte stream
	// for kitty to associate the placement with the image. We write
	// these bytes DIRECTLY to os.Stdout rather than embedding them in
	// the View() return string — lipgloss / bubbletea v2's renderer is
	// known to mangle APC sequences embedded in line content. Writing
	// to os.Stdout from this goroutine (bubbletea's Update goroutine)
	// is race-free wrt bubbletea's own writes, and the upload bytes
	// land in stdout BEFORE bubbletea's frame buffer is flushed for
	// this redraw — kitty parses APC sequences out of the stream
	// regardless of position.
	var kittyFlushBuf bytes.Buffer
	// sixelEmissions records, per visible-window row, the action to take
	// for sixel images intersecting that row. Populated below as we walk
	// entries; consumed AFTER the visible slice is fully built so partial
	// visibility decisions know the slice's final extent.
	type sixelAction struct {
		appendBytes []byte // append after visible[row] (full-visibility start row)
		fallback    string // replace visible[row] with this (partial-visibility row)
	}
	var sixelActions map[int]sixelAction

	for i, e := range entries {
		if want == 0 {
			break
		}
		entryStart := m.entryOffsets[i]
		entryEnd := entryStart + e.height
		if entryEnd <= m.yOffset {
			continue
		}
		if entryStart >= m.yOffset+msgAreaHeight {
			break
		}
		var lines []string
		if e.msgIdx == m.selected {
			lines = e.linesSelected
		} else {
			lines = e.linesNormal
		}
		// Slice the portion of this entry that falls within [yOffset, yOffset+height).
		from := 0
		if entryStart < m.yOffset {
			from = m.yOffset - entryStart
		}
		to := len(lines)
		if entryEnd > m.yOffset+msgAreaHeight {
			to = len(lines) - (entryEnd - (m.yOffset + msgAreaHeight))
		}
		// Visible-window row index where this entry's first emitted line lands.
		windowRowBase := len(visible)
		visible = append(visible, lines[from:to]...)
		want = msgAreaHeight - len(visible)

		// Collect kitty per-image upload escapes for this entry.
		for _, fl := range e.flushes {
			if fl != nil {
				_ = fl(&kittyFlushBuf)
			}
		}

		// Translate this entry's per-image hit rects into viewport-
		// absolute coordinates and record them for HitTest. Rows
		// outside the visible window are clipped; an image that
		// straddles the top/bottom edge yields a hit rect for only
		// its visible rows. Coordinate frame: rowStart/rowEnd are
		// rows within the message-content area (chrome rows are NOT
		// included here; the app-level mouse handler subtracts
		// chromeHeight before calling HitTest, mirroring the
		// convention already used by ClickAt and BeginSelectionAt).
		for _, h := range e.imageHits {
			absStart := entryStart + h.rowStartInEntry
			absEnd := entryStart + h.rowEndInEntry
			if absEnd <= m.yOffset || absStart >= m.yOffset+msgAreaHeight {
				continue
			}
			clipStart := absStart - m.yOffset
			if clipStart < 0 {
				clipStart = 0
			}
			clipEnd := absEnd - m.yOffset
			if clipEnd > msgAreaHeight {
				clipEnd = msgAreaHeight
			}
			m.lastHits = append(m.lastHits, hitRect{
				rowStart: clipStart,
				rowEnd:   clipEnd,
				colStart: h.colStart,
				colEnd:   h.colEnd,
				fileID:   h.fileID,
				msgIdx:   e.msgIdx,
				attIdx:   h.attIdx,
			})
		}

		// Schedule sixel emissions for any image whose start row falls
		// inside this entry. Keys in sixelRows are the image's first row
		// within the entry's linesNormal.
		for startRowInEntry, sx := range e.sixelRows {
			absStart := entryStart + startRowInEntry
			absEnd := absStart + sx.height
			fullyVisible := absStart >= m.yOffset && absEnd <= m.yOffset+msgAreaHeight
			if sixelActions == nil {
				sixelActions = make(map[int]sixelAction)
			}
			if fullyVisible {
				windowRow := windowRowBase + (startRowInEntry - from)
				if windowRow >= 0 && windowRow < len(visible) {
					sixelActions[windowRow] = sixelAction{appendBytes: sx.bytes}
				}
				continue
			}
			// Partial visibility: walk every row of the image, emitting
			// fallback for rows that ARE in the visible window.
			for k := 0; k < sx.height; k++ {
				abs := absStart + k
				if abs < m.yOffset || abs >= m.yOffset+msgAreaHeight {
					continue
				}
				windowRow := abs - m.yOffset
				if windowRow >= 0 && windowRow < len(visible) && k < len(sx.fallback) {
					sixelActions[windowRow] = sixelAction{fallback: sx.fallback[k]}
				}
			}
		}
	}

	// Apply sixel substitutions / appends. Done after the slice is built
	// so we know its final length and can safely mutate by index.
	for row, act := range sixelActions {
		if row < 0 || row >= len(visible) {
			continue
		}
		if act.fallback != "" {
			visible[row] = act.fallback
		}
		if len(act.appendBytes) > 0 {
			visible[row] = visible[row] + string(act.appendBytes)
		}
	}

	// Write kitty upload escapes DIRECTLY to the terminal output,
	// bypassing bubbletea's frame buffer entirely. See the
	// kittyFlushBuf declaration above for rationale. We do this
	// before returning the View string so the upload reaches the
	// terminal before the placeholder cells in the bubbletea frame.
	if kittyFlushBuf.Len() > 0 {
		_, _ = imgpkg.KittyOutput.Write(kittyFlushBuf.Bytes())
	}

	// Pad vertically with the themed spacer if content is shorter than the pane.
	for len(visible) < msgAreaHeight {
		visible = append(visible, m.cacheSpacer)
	}

	// Scroll indicators replace the first / last line when applicable.
	// Track which rows were overridden so the selection overlay knows to
	// leave them alone -- otherwise the overlay would re-compose the
	// indicator line using the underlying message's plain text and
	// corrupt the indicator.
	overrodeFirst := false
	overrodeLast := false
	if m.loading && len(visible) > 0 {
		visible[0] = m.cacheLoadingHint
		overrodeFirst = true
	}
	if m.yOffset+msgAreaHeight < m.totalLines && len(visible) > 0 {
		visible[len(visible)-1] = m.cacheMoreBelow
		overrodeLast = true
	}

	if m.hasSelection {
		visible = m.applySelectionOverlay(visible, overrodeFirst, overrodeLast)
	}

	// Overlay a 1-col scrollbar on the right of the message area when content
	// exceeds the visible height. Chrome (header + separator) is left alone
	// since it does not scroll.
	visible = scrollbar.Overlay(visible, width, m.totalLines, m.yOffset, msgAreaHeight,
		styles.Background, styles.Border, styles.Primary)

	return chrome + "\n" + strings.Join(visible, "\n")
}

// applySelectionOverlay re-composes lines that intersect the active
// selection range. linesNormal supplies the original styled prefix and
// suffix; the selected interior is rendered through styles.SelectionStyle
// over the plain-text segment so the highlight is uniform.
//
// visible is mutated in place when possible. The selection's plain
// columns are translated to display columns by adding the entry's
// contentColOffset.
//
// skipFirst / skipLast tell the overlay to leave row 0 / row N-1 alone
// when those rows have been replaced with scroll indicators (loading
// hint, "more below"). Without this guard the overlay would re-compose
// the indicator line from the underlying entry's plain text, corrupting
// the indicator.
func (m *Model) applySelectionOverlay(visible []string, skipFirst, skipLast bool) []string {
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 {
		return visible
	}
	if loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return visible
	}

	selStyle := styles.SelectionStyle()

	for row := 0; row < len(visible); row++ {
		if (row == 0 && skipFirst) || (row == len(visible)-1 && skipLast) {
			continue
		}
		absLine := m.yOffset + row
		if absLine < loLine || absLine > hiLine {
			continue
		}
		// Find the entry covering this absolute line.
		entryIdx := -1
		for i := range m.cache {
			start := m.entryOffsets[i]
			if absLine >= start && absLine < start+m.cache[i].height {
				entryIdx = i
				break
			}
		}
		if entryIdx < 0 {
			continue
		}
		e := m.cache[entryIdx]
		j := absLine - m.entryOffsets[entryIdx]
		if j < 0 || j >= len(e.linesPlain) {
			continue
		}
		plain := e.linesPlain[j]
		styled := visible[row]

		// from / to are PLAIN columns. They become display columns by
		// adding contentColOffset.
		from := 0
		to := displayWidthOfPlain(plain)
		if absLine == loLine {
			from = loCol
		}
		if absLine == hiLine {
			to = hiCol
		}
		if from < 0 {
			from = 0
		}
		if to > displayWidthOfPlain(plain) {
			to = displayWidthOfPlain(plain)
		}
		if from >= to {
			continue
		}
		// Translate to display columns.
		dispFrom := from + e.contentColOffset
		dispTo := to + e.contentColOffset

		styledWidth := ansi.StringWidth(styled)
		if dispFrom >= styledWidth {
			continue
		}
		if dispTo > styledWidth {
			dispTo = styledWidth
		}
		prefix := ansi.Cut(styled, 0, dispFrom)
		suffix := ansi.Cut(styled, dispTo, styledWidth)
		seg := sliceColumns(plain, from, to)
		visible[row] = prefix + selStyle.Render(seg) + suffix
	}
	return visible
}

func dateFromTS(ts string) string {
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) == 0 {
		return ""
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(sec, 0).Format("2006-01-02")
}

func formatDateSeparator(dateStr string) string {
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	diff := today.Sub(d).Hours() / 24

	switch {
	case diff < 1:
		return "Today"
	case diff < 2:
		return "Yesterday"
	case diff < 7:
		return d.Format("Monday")
	default:
		return d.Format("Monday, January 2, 2006")
	}
}
