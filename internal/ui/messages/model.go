package messages

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/bubbles/entrylist"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/usergroups"
)

// The message data types live in internal/core so the engine can build
// them without importing the TUI.
type (
	MessageItem  = core.MessageItem
	Attachment   = core.Attachment
	ThumbSpec    = core.ThumbSpec
	ReactionItem = core.ReactionItem
)

// AvatarFunc returns the rendered half-block avatar for a user ID, or empty string.
type AvatarFunc func(userID string) string

// EntryPayload is a content pane's payload for one entry of its list,
// index-aligned with the list's entries: the parts the substrate does
// not need. A nil cache of payloads means the render cache was discarded
// and the next View rebuilds it. Both panes use it.
type EntryPayload struct {
	// ShortSrc is the selected row's content with the short timestamp,
	// set only when the selected variant carries a long one;
	// SelectedShort is built from it on demand (SelectedLines) and
	// shown while a text selection exists, because the drag overlay
	// splices the short-form plain text into the row by column.
	ShortSrc      string
	SelectedShort []string

	// Flushes are per-frame kitty image uploads, invoked once per frame
	// for visible entries. nil when the entry has no inline images.
	Flushes []func(io.Writer) error

	// sixelRows maps a row within the entry to the sixel image whose
	// top-left cell is on it. View paints an image out-of-band when it
	// is fully visible and substitutes its halfblock fallback otherwise.
	// Only the channel pane fills it.
	sixelRows map[int]sixelEntry
}

// SelectedLines returns the rows to draw for a cursor entry e: its
// selected variant, or (while a text selection exists) the short-
// timestamp variant built once from ShortSrc by build. Rows past the
// short variant (the channel pane's spacer) are shared by every variant.
func (p *EntryPayload) SelectedLines(e *entrylist.Entry, hasSelection bool, build func(src string) []string) []string {
	if !hasSelection || p.ShortSrc == "" {
		return e.Selected
	}
	if p.SelectedShort == nil {
		short := build(p.ShortSrc)
		if len(short) > len(e.Selected) {
			return e.Selected // unreachable: variants share a height
		}
		p.SelectedShort = append(short, e.Selected[len(short):]...)
	}
	return p.SelectedShort
}

// sixelEntry holds the pre-computed sixel bytes for one inline image,
// plus the halfblock fallback used when the image is only partially
// visible (Phase 6 cannot emit a half-image with sixel).
type sixelEntry struct {
	key      string // stable image cache key; empty -> payload-hash identity
	bytes    []byte
	fallback []string // halfblock-equivalent text for partial-visibility frames
	height   int      // image height in rows
	width    int      // image width in cells; the erase footprint
	col      int      // display column within the entry's rows where the image starts
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
	// sixelPaints is the placement set produced by the last View, consumed
	// by the App's out-of-band painter. See imgpkg.SixelPaint.
	sixelPaints []imgpkg.SixelPaint

	messages      []MessageItem
	selected      int
	channelName   string
	channelTopic  string
	channelType   string // "channel", "private", "dm", "group_dm" -- drives header glyph
	loading       bool
	spinnerFrame  int                          // braille-spinner frame index for "Loading messages..." animation
	avatarFn      AvatarFunc                   // optional: returns half-block avatar for a userID
	replyAvatarFn AvatarFunc                   // optional: small avatar for the reply row
	userNames     map[string]string            // user ID -> display name for mention resolution
	userStatuses  map[string]peerstatus.Status // user ID -> custom status shown after author names
	channelNames  map[string]string            // channel ID -> name for bare <#CID> resolution
	userGroups    map[string]string            // usergroup ID -> handle for bare subteam resolution

	// searchTerms are folded word-prefix terms of the active in-channel
	// search; non-empty enables highlight rendering. nil = no search.
	searchTerms []string

	// Render cache -- invalidated when messages or width change.
	// Each entry holds pre-bordered variants so selection movement does not
	// re-invoke lipgloss per keypress.
	cache          []EntryPayload
	cacheWidth     int
	cacheMsgLen    int
	cacheSpacer    string // pre-rendered blank spacer line (1 row, full width, themed background)
	cacheMoreBelow string // pre-rendered "-- more below --" line

	// list is the shared substrate: entries, offsets, scroll, the
	// visible window, text selection, hit translation and the scrollbar.
	// Configured for the channel pane's behaviour in New.
	list *entrylist.Model

	// Chrome cache: header line(s). Depends on width, channelName, and
	// channelTopic only -- never on selection or scroll position.
	chromeCache       string
	chromeHeight      int
	chromeWidth       int
	chromeChannel     string
	chromeTopic       string
	chromeChannelType string
	chromeCacheValid  bool

	nav ReactionNav

	// currentUserID is the authenticated user; UpdateReaction uses it to
	// set ReactionItem.HasReacted only for the current user's own
	// reactions (not other users'). Empty until SetCurrentUser is called.
	currentUserID string

	lastReadTS string

	// version increments on every state change that could alter rendered
	// View() output. The App layer caches the WRAPPED panel output (border +
	// exactSize + ReapplyBgAfterResets) keyed on this counter, so on compose
	// keystrokes (where version is unchanged) we reuse the previous wrap.
	version int64

	// countedReplies tracks reply TSes already counted into a parent's
	// ReplyCount, so optimistic + WS-echo paths don't double-increment.
	countedReplies map[string]map[string]struct{}

	// imgRenderer owns the inline-image rendering state (active
	// ImageContext, in-flight fetch keys, permanently-failed keys).
	// Configured at startup via Model.SetImageContext (which forwards
	// to the Renderer). nil is equivalent to ProtoOff (no inline
	// rendering, attachments fall back to the legacy single-line OSC 8
	// hyperlink form).
	imgRenderer *imgrender.Renderer

	// emojiCtx bundles the emoji-image rendering dependencies
	// (PlaceContext, configured cell footprint, workspace customs).
	// Configured at startup via Model.SetEmojiContext from
	// cmd/slk/main.go. A zero value disables the image path; the
	// legacy glyph/shortcode-text branch runs instead.
	emojiCtx EmojiContext

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

	// focused tracks whether this panel currently has user focus. When
	// false, the selected-message "▌" border dims from Accent to
	// TextMuted (via styles.SelectionBorderColor) so the unfocused
	// selection doesn't compete visually with the focused panel. The
	// color is baked into the entry rows during buildCache, so
	// SetFocused must invalidate the cache.
	focused bool

	// coloredUsernames enables deterministic per-user coloring of the
	// username in each message row. Set from config via
	// SetColoredUsernames; invalidates the render cache so existing rows
	// pick up the new coloring.
	coloredUsernames bool
}

// SetFocused records whether the messages pane currently holds user focus
// and arranges for the selected-message border to re-render with the
// appropriate color (Accent when focused, TextMuted when not).
//
// Perf: m.focused only affects the SELECTED entry's cached Selected rows
// (via the focused border colour, the selected fill background,
// RepaintBgToSelectionTint, and RenderMessage's IsSelected option -- see
// buildCacheStyles and renderMessageEntry). Every other entry's Lines are
// focus-independent, and View draws an entry's Selected rows ONLY for the
// entry whose Ref is m.selected.
//
// So instead of dropping the entire cache (which forced a full per-message
// re-render of every loaded message -- ~600ms for 50 msgs in real traces,
// ~150ms for 200 msgs in BenchmarkViewFocusFlip), we mark only the selected
// message's TS as stale and let View()'s partialRebuild path re-render that
// one slot. This collapses the cost of `i` / Left / Right / open-thread
// from O(N) per-message rendering work to O(1).
//
// If there is no current selection (m.selected out of range -- e.g. an
// empty channel), there is nothing to invalidate -- the next render will
// pick up the new m.focused value naturally because no entry draws its
// Selected rows.
func (m *Model) SetFocused(focused bool) {
	if m.focused == focused {
		return
	}
	m.focused = focused
	if m.selected >= 0 && m.selected < len(m.messages) {
		if m.staleEntries == nil {
			m.staleEntries = make(map[string]struct{})
		}
		m.staleEntries[m.messages[m.selected].TS] = struct{}{}
	}
	m.dirty()
	debuglog.Perf("messages.SetFocused flip focused=%v msgs=%d selected-row-marked-stale",
		focused, len(m.messages))
}

// HandleEmojiImageReady is invoked by the host (App.Update) when an
// emoji.EmojiImageReadyMsg lands. The same emoji can appear in any
// visible message, reaction pill, or block-kit element — so the
// cheapest correct invariant is a wholesale render-cache
// invalidation. The next View() rebuilds with the now-warm emoji
// placement (single kitty transmit + N placements per the
// registry's dedup contract).
//
// v1 keeps this coarse; if heavy-emoji channels show measurable
// invalidation churn (multiple emoji arriving in a burst), a future
// follow-up can index per-URL → per-message-TS for targeted
// staleEntries population. Punt for now: kitty image placements are
// cheap, full re-render of a viewport is sub-frame on the workloads
// tested.
func (m *Model) HandleEmojiImageReady(url string) {
	debuglog.ImgFetch("messages.HandleEmojiImageReady: url=%s wholesale_invalidate", url)
	m.dropCache()
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
		debuglog.ImgFetch("messages.HandleImageReady: channel=%q active_channel=%q key=%s SKIP (not active)",
			channel, m.channelName, key)
		return
	}
	if key == "" {
		debuglog.ImgFetch("messages.HandleImageReady: channel=%q ts=%s key=<empty> legacy_path",
			channel, ts)
		// Legacy path: no per-key bookkeeping available, so we fall
		// back to the wholesale invalidation that the new fast path
		// is meant to avoid. Used by tests that drive transitions
		// synchronously without a real fetch key.
		m.dropCache()
		if m.imgRenderer != nil {
			m.imgRenderer.ResetFailed()
		}
		m.dirty()
		return
	}
	if m.imgRenderer != nil {
		cleared := m.imgRenderer.ClearFetching(key)
		debuglog.ImgFetch("messages.HandleImageReady: channel=%q key=%s cleared=%v",
			channel, key, cleared)
	}
	if ts != "" && m.cache != nil {
		if m.staleEntries == nil {
			m.staleEntries = make(map[string]struct{})
		}
		m.staleEntries[ts] = struct{}{}
	} else {
		// No TS to target (or no cache to mutate): fall back to a
		// full rebuild on the next View().
		m.dropCache()
	}
	m.dirty()
}

// AvatarReadyMsg is dispatched by the host (cmd/slk wires it from
// avatar.Cache.SetOnReady) when a lazy avatar fetch completes. The
// messages pane is the consumer: receiving the message means the user
// at UserID now has a non-empty render in the avatar.Cache, so a
// re-render of any message authored by UserID will materialize the
// avatar in its slot.
type AvatarReadyMsg struct {
	UserID string
}

// HandleAvatarReady marks every cached entry authored by userID as
// stale so the next View() rebuilds ONLY those slots via
// partialRebuild. Mirrors HandleImageReady's per-entry invalidation
// fast path -- a busy channel that surfaces N unique authors at once
// (channel switch, scroll up into unseen history) triggers a burst of
// AvatarReadyMsg events as the lazy avatar.Cache fetches complete,
// and the prior whole-cache nil paid an O(messages) full rebuild per
// event on the bubbletea Update goroutine.
//
// No-op when:
//   - userID is empty (defensive against malformed host events).
//   - No cached entry is authored by userID (the user has messages in
//     other channels but not the active one; AvatarReadyMsg is
//     workspace-scoped, the cache is channel-scoped).
//   - The cache has been invalidated already (m.cache == nil) -- a
//     pending full rebuild will pick up the avatar naturally.
func (m *Model) HandleAvatarReady(userID string) {
	if userID == "" || m.cache == nil {
		return
	}
	var marked bool
	for _, msg := range m.messages {
		if msg.UserID != userID && !slices.Contains(msg.ReplyUsers, userID) {
			continue
		}
		if m.staleEntries == nil {
			m.staleEntries = make(map[string]struct{})
		}
		m.staleEntries[msg.TS] = struct{}{}
		marked = true
	}
	if marked {
		m.dirty()
	}
}

// HandleImageFailed clears the in-flight bit for a specific key and
// records the key as permanently failed via the embedded
// imgrender.Renderer. RenderBlock checks the renderer's failed-set
// before spawning a fetch, so a permanently-failed image (e.g. all
// auths exhausted on a Slack Connect channel the user isn't actually
// a member of) won't be re-fetched on every cache invalidation. The
// failure record is cleared on channel switch so the user can retry
// by switching away and back.
//
// Does NOT invalidate the render cache: the placeholder is already on
// screen and we have no new bytes to show.
func (m *Model) HandleImageFailed(key string) {
	if key == "" {
		return
	}
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	tracked := m.imgRenderer.MarkFailed(key)
	debuglog.ImgFetch("messages.HandleImageFailed: channel=%q key=%s was_in_flight=%v",
		m.channelName, key, tracked)
}

// Version returns a counter that increments every time the View() output
// could change.
func (m *Model) Version() int64 { return m.version + m.list.Version() }

// MessageTextSource returns the mrkdwn string that should be rendered
// as the visible message body. For most messages it's just msg.Text,
// but for messages whose body originated as a rich_text block (typical
// of bot apps like GitHub Pending Reviews, PagerDuty, etc.) Slack's
// text fallback collapses standalone "\n" elements into spaces — so
// we reconstruct a newline-faithful mrkdwn from the parsed block when
// one is available. See blockkit.RichTextToMrkdwn.
//
// When no RichTextBlock is present (the overwhelmingly common case
// for user-typed messages) this is a zero-cost passthrough of
// msg.Text.
func MessageTextSource(msg MessageItem) string {
	for _, b := range msg.Blocks {
		if rt, ok := b.(blockkit.RichTextBlock); ok {
			if reconstructed := blockkit.RichTextToMrkdwn(rt); reconstructed != "" {
				return reconstructed
			}
		}
	}
	return msg.Text
}

// BlocksCarryBody reports whether msg's blocks already render its body,
// in which case the host adds no row for msg.Text. A non-empty rich_text
// block is the exception: it renders through MessageTextSource and must
// retain the host body row. This decision affects rendering only;
// copying still uses msg.Text.
func BlocksCarryBody(msg MessageItem) bool {
	for _, b := range msg.Blocks {
		if rt, ok := b.(blockkit.RichTextBlock); ok && blockkit.RichTextToMrkdwn(rt) != "" {
			return false
		}
	}
	return blockkit.RendersBody(msg.Blocks)
}

// dirty bumps the render-version counter.
func (m *Model) dirty() { m.version++ }

// dropCache discards the render cache; the next View rebuilds it. Until
// then the entries on screen are no longer current, so clicks and
// selections find none (the scroll extent is kept).
func (m *Model) dropCache() {
	m.cache = nil
	m.list.KeepLookups(0)
}

// newChannelList configures the substrate for the channel pane (spec
// §5): the cursor follows the scroll (U1), a bottom-anchored pane stays
// pinned when it shrinks (U2), a press on a divider snaps to a message
// (8c), and the bottom test on resize uses the new height (U5). The
// offset clamps lazily (U6) and a drag bumps Version once (U4): the
// defaults.
func newChannelList() *entrylist.Model {
	return entrylist.New(
		entrylist.WithStyles(themeListStyles()),
		entrylist.WithCursorFollowsScroll(),
		entrylist.WithPinBottomOnShrink(),
		entrylist.WithSnapDecorationAnchors(),
		entrylist.WithReanchor(entrylist.ReanchorPolicy{BottomUsesNewHeight: true}),
	)
}

func New(msgs []MessageItem, channelName string) *Model {
	selected := 0
	if len(msgs) > 0 {
		selected = len(msgs) - 1
	}
	return &Model{
		messages:    msgs,
		selected:    selected,
		channelName: channelName,
		imgRenderer: imgrender.NewRenderer(),
		list:        newChannelList(),
	}
}

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.dropCache()
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
		if m.channelName != name && m.imgRenderer != nil {
			m.imgRenderer.ResetFailed()
		}
	}
	m.channelName = name
	m.channelTopic = topic
}

// SetChannelTopic replaces the header's second line without changing
// the channel. A DM uses it for the peer's status and DND.
func (m *Model) SetChannelTopic(topic string) {
	if m.channelTopic == topic {
		return
	}
	m.channelTopic = topic
	m.chromeCacheValid = false
	m.dirty()
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

// ChannelGlyph returns the prefix glyph for a channel type: "◆" for
// "private", "●" for "dm" and "group_dm", "#" otherwise. Used by the
// messages-pane header and the thread breadcrumb.
func ChannelGlyph(chType string) string {
	switch chType {
	case "private":
		return "\u25c6" // ◆
	case "dm", "group_dm":
		return "\u25cf" // ●
	default:
		return "#"
	}
}

// SetSearchTerms sets (or clears, with nil) the folded terms whose
// word-prefix occurrences get highlighted in message text. Invalidates
// the render cache.
func (m *Model) SetSearchTerms(terms []string) {
	// Redundant calls (same terms) must not thrash the render cache;
	// clone so a caller mutating its slice can't alias our state.
	if slices.Equal(terms, m.searchTerms) {
		return
	}
	m.searchTerms = slices.Clone(terms)
	m.InvalidateCache()
	m.dirty()
}

func (m *Model) SetMessages(msgs []MessageItem) {
	if debuglog.Enabled() {
		oldSummary := summarizeMessageItems(m.messages)
		newSummary := summarizeMessageItems(msgs)
		debuglog.Cache("messages.Model.SetMessages: channel=%q before=[%s] after=[%s]",
			m.channelName, oldSummary, newSummary)
	}
	m.messages = msgs
	m.ClearSelection()
	m.dropCache()
	// Force the next View() to re-snap yOffset to the new selection -- without
	// this, switching to a channel that happens to have the same selected
	// index as the previous channel would leave yOffset at its old value.
	m.list.ResetSnap()
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
	m.dropCache()
	m.dirty()

	// Always scroll to the newest message. Advancing `selected` to the
	// last index forces View() to re-snap yOffset to the bottom on the
	// next render (because the snap mark != selected).
	m.selected = len(m.messages) - 1
}

// SwapLocalSent replaces an optimistic placeholder identified by
// localTS (an internal "local:..." id assigned when the user pressed
// Enter, before the chat.postMessage HTTP response carried back the
// authoritative Slack TS) with the authoritative msg. The placeholder
// keeps its position in the list so the rendered order matches what
// the user saw when they hit Enter, but the entry's contents are
// fully replaced (text, real TS, attachments, etc.).
//
// Returns true if a row matching localTS was found and swapped, false
// otherwise. Callers that get false should typically fall back to
// UpsertSelfSent so the message still lands (e.g. the user navigated
// away from the channel between Enter and the HTTP response, so the
// placeholder isn't in the current model).
func (m *Model) SwapLocalSent(localTS string, msg MessageItem) bool {
	if localTS == "" {
		return false
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].TS == localTS {
			m.messages[i] = msg
			m.dropCache()
			m.dirty()
			// Same rationale as UpsertSelfSent's replace branch: the
			// authoritative entry may differ in height (e.g. server-
			// rendered blocks) and the snap anchor needs to refresh.
			m.list.ResetSnap()
			return true
		}
	}
	return false
}

// RemoveLocalSent removes an optimistic placeholder identified by
// localTS. Used when the chat.postMessage HTTP call fails and we want
// to roll back the instant-display add. Returns true if a row was
// removed.
func (m *Model) RemoveLocalSent(localTS string) bool {
	if localTS == "" {
		return false
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].TS == localTS {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			m.dropCache()
			m.dirty()
			m.list.ResetSnap()
			if m.selected >= len(m.messages) && len(m.messages) > 0 {
				m.selected = len(m.messages) - 1
			}
			return true
		}
	}
	return false
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
				m.dropCache()
				m.dirty()
				// Force the next View() to re-snap yOffset to the
				// updated selected entry's range, even when m.selected
				// hasn't changed.
				m.list.ResetSnap()
				return
			}
		}
	}
	m.messages = append(m.messages, msg)
	m.dropCache()
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

// SelectByTS moves the selection cursor to the message with the given
// ts and forces the next View() to re-snap the viewport to it.
// Returns false (selection unchanged) when no message with that ts is
// in the loaded buffer. Used by the permalink-navigation path to land
// on the exact message a Slack archive link points at.
func (m *Model) SelectByTS(ts string) bool {
	if ts == "" {
		return false
	}
	for i := range m.messages {
		if m.messages[i].TS == ts {
			m.selected = i
			m.list.ResetSnap()
			m.dirty()
			return true
		}
	}
	return false
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
	out := []HitRect{}
	for _, h := range m.list.Hits() {
		if h.Kind != entrylist.HitImage {
			continue
		}
		out = append(out, HitRect{
			RowStart: h.RowStart,
			RowEnd:   h.RowEnd,
			ColStart: h.ColStart,
			ColEnd:   h.ColEnd,
			FileID:   h.Key,
			MsgIdx:   h.Ref,
			AttIdx:   h.Aux,
		})
	}
	return out
}

func (m *Model) MoveUp() {
	if m.nav.Active {
		m.ExitReactionNav()
	}
	if m.selected > 0 {
		m.selected--
		m.dirty()
	}
}

// ScrollUp moves the viewport up by n lines. The selected-message cursor
// follows the scroll: if the scroll would push the selection off the bottom
// edge, View() clamps the selection to the bottommost still-visible message
// (see the cursor-clamp step in viewInternal).
func (m *Model) ScrollUp(n int) {
	m.list.ScrollUp(n, m.selected)
}

// ScrollDown moves the viewport down by n lines. View() clamps yOffset to the
// maximum allowed for the current content height, and the selected-message
// cursor follows the scroll: if the scroll would push the selection off the
// top edge, View() clamps it to the topmost still-visible message (see the
// cursor-clamp step in viewInternal).
func (m *Model) ScrollDown(n int) {
	m.list.ScrollDown(n, m.selected)
}

func (m *Model) MoveDown() {
	if m.nav.Active {
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

// ViewportAtTop reports whether the viewport is scrolled to the very top of
// the message stream. Used by the app layer to trigger older-history backfill
// on a wheel-up / PageUp gesture that scrolls the viewport without moving
// selection (selection-based backfill goes through AtTop()).
func (m *Model) ViewportAtTop() bool {
	return m.list.YOffset() == 0 && len(m.messages) > 0
}

// YOffset returns the current viewport scroll offset (first visible line
// index inside the flattened message buffer). Exposed for tests and the
// debug overlay; do not use for navigation -- call ScrollUp/ScrollDown.
func (m *Model) YOffset() int {
	return m.list.YOffset()
}

func (m *Model) PrependMessages(msgs []MessageItem) {
	if len(msgs) == 0 {
		return
	}
	// Boundary guard: a backfill that raced a channel re-select (the
	// re-select replaced the buffer while the backfill was in flight)
	// can deliver items the buffer already contains. Keep only items
	// strictly older than the current head. TS comparison is
	// lexicographic, matching the rest of the model (e.g. buildCache's
	// lastReadTS compare): real Slack TSes are fixed-width
	// "seconds.micros" strings. A "local:" optimistic placeholder at
	// the head compares greater than any numeric TS, so the guard
	// degrades to keep-everything there — never a wrong drop.
	if len(m.messages) > 0 {
		head := m.messages[0].TS
		kept := make([]MessageItem, 0, len(msgs))
		for _, item := range msgs {
			if item.TS < head {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			return
		}
		msgs = kept
	}
	count := len(msgs)
	if debuglog.Enabled() {
		debuglog.Cache("messages.Model.PrependMessages: channel=%q count_before=%d count_added=%d added=[%s]",
			m.channelName, len(m.messages), count, summarizeMessageItems(msgs))
	}
	m.messages = append(msgs, m.messages...)
	m.selected += count
	m.dropCache()
	m.dirty()
}

func (m *Model) EnterReactionNav() {
	if msg, ok := m.SelectedMessage(); ok && len(msg.Reactions) > 0 {
		m.nav.Active = true
		m.nav.Index = 0
		m.dropCache()
		m.dirty()
	}
}

func (m *Model) ExitReactionNav() {
	if !m.nav.Active && m.nav.Index == 0 {
		return
	}
	m.nav.Active = false
	m.nav.Index = 0
	m.dropCache()
	m.dirty()
}

func (m *Model) ReactionNavActive() bool {
	return m.nav.Active
}

func (m *Model) ReactionNavLeft() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	m.nav.Left(len(msg.Reactions))
	m.dropCache()
	m.dirty()
}

func (m *Model) ReactionNavRight() {
	msg, ok := m.SelectedMessage()
	if !ok {
		return
	}
	m.nav.Right(len(msg.Reactions))
	m.dropCache()
	m.dirty()
}

func (m *Model) SelectedReaction() (emoji string, isPlus bool) {
	msg, ok := m.SelectedMessage()
	if !ok {
		return "", false
	}
	return m.nav.Selected(msg.Reactions)
}

func (m *Model) ClampReactionNav() {
	msg, ok := m.SelectedMessage()
	if !ok || len(msg.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	m.nav.Clamp(len(msg.Reactions))
	m.dropCache()
	m.dirty()
}

// IncrementReplyCount finds a message by TS, increments its ReplyCount and
// adds userID to its ReplyUsers.
// Idempotent on (parentTS, replyTS): the optimistic ThreadReplySentMsg path
// and the WS echo NewMessageMsg path can both fire for the same reply --
// pass the reply's own TS to dedup. An empty replyTS skips dedup (legacy
// callers that don't yet pass it).
func (m *Model) IncrementReplyCount(parentTS, replyTS, userID string) {
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
	for i, msg := range m.messages {
		if msg.TS == parentTS {
			// Same as UpdateReaction: the first reply adds a
			// "[N replies ->]" row, which must not push a message at
			// the bottom of the pane under the fold.
			m.list.HoldBottom()
			m.messages[i].ReplyCount++
			if userID != "" && !slices.Contains(msg.ReplyUsers, userID) {
				m.messages[i].ReplyUsers = append(slices.Clip(msg.ReplyUsers), userID)
			}
			m.dropCache()
			m.dirty()
			return
		}
	}
}

// UpdateMessageInPlace finds the message with item's TS and applies a
// message_changed echo to it (MessageItem.ApplyChange: text, edited
// mark, blocks, attachments; reactions and the rest are kept). Returns
// true if the message was found. Invalidates the render cache. A pane
// at the bottom stays there: an unfurl or an edit that adds rows must
// not push them under the fold (see UpdateReaction).
func (m *Model) UpdateMessageInPlace(item MessageItem) bool {
	for i, msg := range m.messages {
		if msg.TS == item.TS {
			m.list.HoldBottom()
			m.messages[i].ApplyChange(item)
			m.dropCache()
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
			m.dropCache()
			m.dirty()
			return true
		}
	}
	return false
}

func (m *Model) UpdateReaction(messageTS, emojiName, userID string, remove bool) {
	for i, msg := range m.messages {
		if msg.TS == messageTS {
			// Keep "anchored at bottom" across the reaction's height
			// change. Without this, adding the first reaction to the
			// last visible message grows the list total by 1 while
			// yOffset stays put -- the bottom message gets pushed below
			// the fold and a "-- more below --" hint replaces it. The
			// reaction must push content *up* instead, keeping the
			// reacted-to message anchored to the bottom of the pane.
			m.list.HoldBottom()
			ApplyReaction(&m.messages[i], emojiName, userID, m.currentUserID, remove)
			m.dropCache()
			m.dirty()
			if m.nav.Active {
				m.ClampReactionNav()
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

// IsLoading reports whether the messagepane is currently displaying its
// loading spinner.
func (m *Model) IsLoading() bool { return m.loading }

// SetSpinnerFrame advances the braille-spinner frame used by the
// "Loading messages..." empty state and the "Loading older messages..."
// hint. Calling with the same value is a no-op; otherwise the cache is
// invalidated so the next render picks up the new glyph.
func (m *Model) SetSpinnerFrame(f int) {
	if m.spinnerFrame != f {
		m.spinnerFrame = f
		m.dirty()
	}
}

func (m *Model) SetAvatarFunc(fn AvatarFunc) {
	m.avatarFn = fn
}

// SetReplyAvatarFunc switches the reply row to Slack's look (repliers'
// avatars, then "N replies"); nil keeps "[N replies ->]".
func (m *Model) SetReplyAvatarFunc(fn AvatarFunc) {
	m.replyAvatarFn = fn
	m.dropCache()
	m.dirty()
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
	// Drop the cache so mentions re-render with the new names.
	m.dropCache()
	m.dirty()
}

// SetColoredUsernames enables or disables deterministic per-user coloring
// of usernames. Invalidates the render cache so existing rows re-render
// with the new coloring.
func (m *Model) SetColoredUsernames(enabled bool) {
	m.coloredUsernames = enabled
	m.dropCache()
	m.dirty()
}

// SetCurrentUser records the authenticated user ID so UpdateReaction can
// flag the current user's own reactions (HasReacted) correctly.
func (m *Model) SetCurrentUser(userID string) {
	m.currentUserID = userID
}

// PatchUserName updates the in-memory userNames map (used for @mention
// rendering) and overwrites the UserName field on every cached message
// authored by userID. Invalidates the render cache so the next View()
// re-renders affected rows.
//
// Used by the async user-resolution path: history fetchers stash
// MessageItem.UserName = m.UserID for unknown authors. When the
// resolution returns asynchronously, the App calls PatchUserName to
// replace the placeholders live without re-fetching history.
//
// Always applies: the map is normally the App's, shared by every pane
// and already holding the name by the time this pane is patched, so
// "already in the map" says nothing about this pane's rows or cache.
// The App drops resolutions that change nothing.
func (m *Model) PatchUserName(userID, displayName string) {
	if userID == "" {
		return
	}
	if m.userNames == nil {
		m.userNames = map[string]string{}
	}
	m.userNames[userID] = displayName
	// The render cache stores rows with their mentions already resolved
	// (RenderSlackMarkdown consults userNames at render time), so any
	// cached row that mentioned <@userID> is now stale. Mirror
	// SetUserNames's behavior: invalidate unconditionally on map change.
	m.dropCache()
	for i := range m.messages {
		if m.messages[i].UserID == userID && m.messages[i].UserName != displayName {
			m.messages[i].UserName = displayName
		}
	}
	m.dirty()
}

// SetUserStatuses replaces the user ID -> custom status map whose emoji
// follows author names. The map is copied; later changes go through
// PatchUserStatus.
func (m *Model) SetUserStatuses(statuses map[string]peerstatus.Status) {
	m.userStatuses = make(map[string]peerstatus.Status, len(statuses))
	for id, st := range statuses {
		m.userStatuses[id] = st
	}
	m.dropCache()
	m.dirty()
}

// PatchUserStatus records one user's status, invalidating the render
// cache only when a message in this pane is theirs. No-op when
// unchanged.
func (m *Model) PatchUserStatus(userID string, st peerstatus.Status) {
	if userID == "" || m.userStatuses[userID] == st {
		return
	}
	if m.userStatuses == nil {
		m.userStatuses = map[string]peerstatus.Status{}
	}
	m.userStatuses[userID] = st
	if m.hasAuthor(userID) {
		m.dropCache()
		m.dirty()
	}
}

// ExpireStatuses drops statuses whose deadline has passed and reports
// whether an author name rendered in this pane changed.
func (m *Model) ExpireStatuses(now time.Time) bool {
	changed := false
	for uid, st := range m.userStatuses {
		if st.Expired(now) {
			m.userStatuses[uid] = st.Clear(now)
			if m.hasAuthor(uid) {
				changed = true
			}
		}
	}
	if changed {
		m.dropCache()
		m.dirty()
	}
	return changed
}

func (m *Model) hasAuthor(userID string) bool {
	for i := range m.messages {
		if m.messages[i].UserID == userID {
			return true
		}
	}
	return false
}

// AuthorStatusSuffix is what follows an author's name in a message
// header: a space and their status emoji on the pane background, or ""
// when they have no live custom status. Shared with the thread pane.
func AuthorStatusSuffix(statuses map[string]peerstatus.Status, userID string, now time.Time) string {
	g := statuses[userID].Glyph(now)
	if g == "" {
		return ""
	}
	return lipgloss.NewStyle().Background(styles.Background).Render(" " + g)
}

// SetChannelNames sets the channel ID -> name map used to resolve bare
// <#CHANNELID> mentions (Slack-side messages from clients that emit
// channel mentions without the embedded |name).
func (m *Model) SetChannelNames(names map[string]string) {
	m.channelNames = names
	m.dropCache()
	m.dirty()
}

// SetUserGroups sets the workspace-scoped usergroup ID -> handle map used
// to resolve bare <!subteam^SID> mentions. No-op when the new map matches
// the current one -- App.SetUserGroups fires on every workspace switch,
// and busting the render cache for an identical set is pure waste.
func (m *Model) SetUserGroups(groups map[string]string) {
	if usergroups.Equal(m.userGroups, groups) {
		return
	}
	m.userGroups = usergroups.Copy(groups)
	m.dropCache()
	m.dirty()
}

// EmojiContext bundles the emoji-image rendering dependencies. Held
// by the Model and threaded through RenderSlackMarkdownWith when
// building each message's body and reaction pills.
type EmojiContext struct {
	PlaceCtx emojiutil.PlaceContext
	Cells    int               // 1 or 2; 0 falls back to 2
	Customs  map[string]string // workspace custom emoji map; nil = empty
}

// SetEmojiContext configures the emoji-image rendering path. Should
// be called once at startup (from cmd/slk/main.go) after the
// PlaceContext, Customs map, and EmojiCells are known. Subsequent
// calls invalidate the render cache so the new context takes effect
// on the next View().
func (m *Model) SetEmojiContext(ctx EmojiContext) {
	if ctx.Cells != 1 && ctx.Cells != 2 {
		ctx.Cells = 2
	}
	m.emojiCtx = ctx
	m.dropCache()
	m.dirty()
}

// SetEmojiCustoms updates only the customs map on the active emoji
// context, leaving PlaceCtx and Cells untouched. Invalidates the
// render cache so the new map is consulted on the next View().
//
// Called from App.SetCustomEmoji when CustomEmojisLoadedMsg arrives
// from the workspace bootstrap.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiCtx.Customs = customs
	m.dropCache()
	m.dirty()
}

// SetImageContext configures the inline-image rendering pipeline. Should
// be called once at startup, before the first View(). Subsequent calls
// invalidate the render cache. A zero-valued ImageContext (or one with
// Protocol == ProtoOff) disables inline rendering and falls back to the
// legacy single-line "[Image] <url>" form.
func (m *Model) SetImageContext(ctx imgrender.ImageContext) {
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	m.imgRenderer.SetContext(ctx)
	if ctx.Fetcher != nil {
		ctx.Fetcher.ConfigurePrerender(ctx.Protocol)
		if ctx.Protocol == imgpkg.ProtoKitty {
			ctx.Fetcher.ConfigurePrerenderKitty(ctx.KittyRender)
		} else {
			ctx.Fetcher.ConfigurePrerenderKitty(nil)
		}
	}
	m.dropCache()
	m.dirty()
}

// SetLastReadTS sets the timestamp of the last read message.
// Messages with TS > lastReadTS are considered unread.
func (m *Model) SetLastReadTS(ts string) {
	if m.lastReadTS == ts {
		return
	}
	m.lastReadTS = ts
	m.dropCache()
	m.dirty()
}

// LastReadTS returns the current "last read" boundary timestamp. The App
// reads it when it seeds a split window from this pane (windows.go).
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
	wrap        WrapStyles
	spacerLines []string
}

// buildCacheStyles materializes the shared styles for a given width.
// Also writes m.cacheSpacer / m.cacheMoreBelow as a side effect so
// View()'s per-frame chrome rendering can reuse the same allocations.
// partialRebuild reuses the existing m.cacheSpacer (since width
// hasn't changed) and only needs the border styles.
//
// The "Loading older messages..." hint is intentionally NOT cached
// here: its spinner glyph must reflect the current m.spinnerFrame,
// which changes far more often than the cache is rebuilt. Caching
// it would freeze the glyph at whatever frame happened to be set
// when the cache last rebuilt. View() composes the hint fresh from
// renderLoadingOlderHint(width) on every frame instead.
func (m *Model) buildCacheStyles(width int) cacheStyles {
	spacerBg := lipgloss.NewStyle().Background(styles.Background)
	m.cacheSpacer = spacerBg.Width(width).Render("")
	hintStyle := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted)
	// NOTE: intentionally NOT width-padded. "more below" only appears when
	// the content overflows the viewport, which is exactly the condition
	// under which scrollbar.Overlay runs and normalizes every row to the
	// full width -- so the app-layer border-assembly width invariant
	// (ui.borderedTopPane) still holds. Padding here would be truncated by
	// the scrollbar pass and break TestSelection_HighlightDoesNotCorrupt...
	m.cacheMoreBelow = hintStyle.Render("  -- more below --")
	return cacheStyles{
		wrap:        NewWrapStyles(m.focused),
		spacerLines: []string{m.cacheSpacer},
	}
}

// renderLoadingOlderHint composes the "Loading older messages..." line
// fresh on every call, using the CURRENT m.spinnerFrame for the
// braille-spinner glyph. Called from View() each frame so the glyph
// animates in lockstep with SetSpinnerFrame's tick. The width arg is
// accepted for symmetry with other render helpers but is unused: the
// hint is left-aligned and the host code (View()) drops it into a
// pre-sized visible[0] slot.
func (m *Model) renderLoadingOlderHint(width int) string {
	// Width(width) pads to the full pane width so this transient top-row
	// hint upholds the "every pane line is exactly width cells" invariant
	// the app-layer border assembly relies on (ui.borderedTopPane).
	hintStyle := lipgloss.NewStyle().Background(styles.Background).Foreground(styles.TextMuted).Width(width)
	frame := styles.SpinnerChars[m.spinnerFrame%len(styles.SpinnerChars)]
	return hintStyle.Render("  " + string(frame) + " Loading older messages...")
}

// entryPerfStats accumulates per-sub-step timing across a single
// buildCache invocation. Populated only when debuglog.Enabled();
// callers pass nil otherwise. renderMessageEntry / RenderMessage
// guard every measurement with `if stats != nil` so the nil path adds
// only a couple of branch-predicted checks per message — negligible
// compared to the lipgloss / regex / blockkit work it surrounds.
//
// Fields measure cumulative time across the loop, paired with a count
// so the [perf] log can report "blockKit n=12 avg=8ms total=96ms"
// (most messages skip blockkit; only ones with msg.Blocks pay).
type entryPerfStats struct {
	count int // number of messages processed

	// RenderMessage sub-steps
	bodyTotal      time.Duration // RenderSlackMarkdown + WordWrap + styles.MessageText.Render
	reactionsTotal time.Duration // pill rendering, emoji width, wrap math
	reactionsCount int
	blockKitTotal  time.Duration // blockkit.Render for msg.Blocks
	blockKitCount  int
	legacyTotal    time.Duration // blockkit.RenderLegacy for msg.LegacyAttachments
	legacyCount    int
	// Sub-breakdown within legacyTotal. Sums across all
	// blockkit.RenderLegacy calls in the loop, attributing the cost
	// across text rendering (renderTextLines for pretext / text /
	// fields / footer), image work (computeBlockImageTarget +
	// fetchOrPlaceholder), and other (stripe styling, title
	// formatting, width-and-truncate math). Populated only when
	// ctx.Perf was non-nil during the call.
	legacyTextTotal  time.Duration
	legacyImageTotal time.Duration
	legacyOtherTotal time.Duration
	legacyAttCount   int // total LegacyAttachment values processed

	// Sub-breakdown within legacyImageTotal, populated by
	// blockkit.fetchOrPlaceholder. Each (total, count) pair lets the
	// host log avg-per-call.
	legacyImgCachedCheckTotal time.Duration
	legacyImgCachedCheckCount int
	legacyImgKittyTotal       time.Duration
	legacyImgKittyCount       int
	legacyImgRenderImageTotal time.Duration
	legacyImgRenderImageCount int
	legacyImgPlaceholderTotal time.Duration
	legacyImgPlaceholderCount int
	attachmentsTotal          time.Duration // imgRenderer.RenderBlock loop over msg.Attachments
	attachmentsCount          int

	// renderMessageEntry sub-steps (post RenderMessage)
	borderWrapTotal time.Duration // WrapRows: border/fill renders + RepaintBgToSelectionTint
	plainLinesTotal time.Duration // ANSI-stripping for the plain-text mirror
}

// renderContext is what RenderMessage reads from the channel pane.
func (m *Model) renderContext() *RenderContext {
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	return &RenderContext{
		ColoredUsernames: m.coloredUsernames,
		UserStatuses:     m.userStatuses,
		UserNames:        m.userNames,
		ChannelNames:     m.channelNames,
		UserGroups:       m.userGroups,
		Emoji:            m.emojiCtx,
		Images:           m.imgRenderer,
		ImageChannel:     m.channelName,
		SearchTerms:      m.searchTerms,
		Nav:              m.nav,
		ReplyAvatar:      m.replyAvatarFn,
	}
}

// renderMessageEntry builds a single entry and its EntryPayload for m.messages[i] using
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
//
// stats may be nil. When non-nil, per-sub-step durations are added to
// it for later aggregation. Callers must NOT increment stats.count --
// renderMessageEntry does that itself so the count always matches the
// number of entries produced.
func (m *Model) renderMessageEntry(i int, width int, cs cacheStyles, stats *entryPerfStats) (entrylist.Entry, EntryPayload) {
	msg := m.messages[i]
	opt := RenderOptions{
		IsSelected:      i == m.selected,
		ReplyAffordance: true,
		EditedMark:      true,
		BroadcastLabel:  true,
		ImageHits:       true,
		SixelRows:       true,
		stats:           stats,
	}
	if m.avatarFn != nil {
		opt.Avatar = m.avatarFn(msg.UserID)
	}
	r := RenderMessage(msg, width, m.renderContext(), opt)
	// The channel pane's kitty upload order: blocks, attachments, then
	// body and reaction emoji.
	flushes := slices.Concat(r.BlockFlushes, r.AttachmentFlushes, r.BodyFlushes, r.ReactionFlushes)
	cs.wrap.stats = stats
	w := WrapRows(msg, r, width, cs.wrap)
	linesN, linesS, linesP := w.Lines, w.Selected, w.Plain
	if stats != nil {
		stats.count++
	}
	// Append a trailing spacer line after every message except the last.
	// Both variants share the same spacer (it has no border styling).
	// The plain mirror of the spacer is the empty string -- selection
	// extraction trims trailing whitespace, and no real content lives
	// in the spacer row.
	trail := 0
	if i < len(m.messages)-1 {
		linesN = append(linesN, cs.spacerLines...)
		linesS = append(linesS, cs.spacerLines...)
		linesP = append(linesP, plainLine{Text: "", Bytes: []int{0}})
		trail = len(cs.spacerLines)
	}
	entry := entrylist.Entry{
		ID:         msg.TS,
		Ref:        i,
		Selectable: true,
		Lines:      linesN,
		Selected:   linesS,
		Plain:      linesP,
		ColOffset:  1, // thick left border ▌ occupies column 0 of Lines
		Trail:      trail,
		Hits:       r.Hits,
	}
	return entry, EntryPayload{ShortSrc: w.ShortSrc, Flushes: flushes, sixelRows: r.SixelRows}
}

// buildCache pre-renders all messages and day separators, splitting each
// rendered string on "\n" so View() can flatten everything into the visible
// window with zero string-scanning per frame. Runs only on width / message-set
// / theme / reaction changes -- never on simple j/k navigation, and never
// on a single-image arrival (HandleImageReady marks one TS stale and the
// View()-time guard dispatches to partialRebuild instead).
func (m *Model) buildCache(width int, keepPosition bool) {
	m.cache = m.cache[:0]
	m.cacheWidth = width
	m.cacheMsgLen = len(m.messages)
	// A full rebuild supersedes any pending per-entry invalidations.
	for k := range m.staleEntries {
		delete(m.staleEntries, k)
	}

	cs := m.buildCacheStyles(width)

	// Perf instrumentation: allocate the per-sub-step stats sink when
	// SLK_DEBUG is on so we can attribute buildCache's wall-clock across
	// body / reactions / blockKit / legacy / attachments / borderWrap /
	// plainLines. Aggregated into a single [perf] line after the loop
	// to keep log volume O(1) per buildCache rather than O(N) per
	// message. nil disables every call site's timing (see
	// renderMessageEntry / RenderMessage).
	var stats *entryPerfStats
	if debuglog.Enabled() {
		stats = &entryPerfStats{}
	}

	if cap(m.cache) < len(m.messages)+8 {
		m.cache = make([]EntryPayload, 0, len(m.messages)+8)
	}
	entries := make([]entrylist.Entry, 0, cap(m.cache))

	appendSeparator := func(rendered string) {
		lines := strings.Split(rendered, "\n")
		entries = append(entries, entrylist.Entry{
			Ref:      -1,
			Lines:    lines,
			Selected: lines,
			Plain:    plainLines(rendered),
		})
		m.cache = append(m.cache, EntryPayload{})
	}

	var lastDate string
	newMsgLandmarkInserted := false
	for i, msg := range m.messages {
		msgDate := DateFromTS(msg.TS)
		if msgDate != "" && msgDate != lastDate {
			label := FormatDateSeparator(msgDate)
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

		e, p := m.renderMessageEntry(i, width, cs, stats)
		entries = append(entries, e)
		m.cache = append(m.cache, p)
	}

	m.list.SetEntries(entries, width, keepPosition)

	if stats != nil {
		debuglog.Perf("messages.buildCache.breakdown N=%d body=%s reactions(n=%d)=%s blockKit(n=%d)=%s legacy(n=%d, atts=%d)=%s [text=%s image=%s other=%s] img[cachedCheck(n=%d)=%s kitty(n=%d)=%s renderImage(n=%d)=%s placeholder(n=%d)=%s] attachments(n=%d)=%s borderWrap=%s plainLines=%s",
			stats.count, stats.bodyTotal,
			stats.reactionsCount, stats.reactionsTotal,
			stats.blockKitCount, stats.blockKitTotal,
			stats.legacyCount, stats.legacyAttCount, stats.legacyTotal,
			stats.legacyTextTotal, stats.legacyImageTotal, stats.legacyOtherTotal,
			stats.legacyImgCachedCheckCount, stats.legacyImgCachedCheckTotal,
			stats.legacyImgKittyCount, stats.legacyImgKittyTotal,
			stats.legacyImgRenderImageCount, stats.legacyImgRenderImageTotal,
			stats.legacyImgPlaceholderCount, stats.legacyImgPlaceholderTotal,
			stats.attachmentsCount, stats.attachmentsTotal,
			stats.borderWrapTotal, stats.plainLinesTotal)
	}
}

// partialRebuild re-renders only the cache slots whose source-message
// TSes are in m.staleEntries, leaving sibling slots (other messages
// AND date / "── new ──" separators) intact. Called by View() when an
// ImageReadyMsg has marked one or more entries stale but neither the
// pane width nor the message-set length has changed since the last
// full buildCache.
//
// Critical perf invariant: this function MUST NOT call
// RenderMessage for any message that isn't in m.staleEntries.
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
	updates := make(map[int]entrylist.Entry, len(m.staleEntries))
	for ts := range m.staleEntries {
		idx, ok := m.list.IndexOf(ts)
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
		old := m.list.Entries()[idx]
		if !old.Selectable || old.Ref >= len(m.messages) {
			continue
		}
		e, p := m.renderMessageEntry(old.Ref, width, cs, nil)
		updates[idx] = e
		m.cache[idx] = p
	}
	// One offset pass for the whole batch: heights may have changed (a
	// placeholder swapped for a real image). No render work for the
	// siblings.
	m.list.ReplaceEntries(updates)
	for k := range m.staleEntries {
		delete(m.staleEntries, k)
	}
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

// convertSixelMap translates imgrender.SixelEntry values into the
// messages-pane's private sixelEntry type, preserving the row keys.
// Returns nil for an empty/nil input so callers don't have to nil-check.
func convertSixelMap(in map[int]imgrender.SixelEntry) map[int]sixelEntry {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]sixelEntry, len(in))
	for k, v := range in {
		out[k] = sixelEntry{key: v.Key, bytes: v.Bytes, fallback: v.Fallback, height: v.Height, width: v.Width}
	}
	return out
}

// ClickAt handles a mouse click at the given y-coordinate (the pane-local
// y returned by App.panelAt, which is measured from the panel's top border —
// so y=0..chromeHeight-1 is the channel header / separator chrome and
// y=chromeHeight onward is message content). Selects the message at that
// position; clicks in the chrome are ignored. Returns true when the click
// landed on a real message row (selection was either updated or already
// matched the hit), false when the click missed -- callers use the bool to
// distinguish "clicked a message" from "clicked empty space" so that
// click-to-open-thread doesn't fire on chrome / dead-space clicks.
//
// Unlike the thread pane, a click below the content misses: the line is
// not clamped to the content.
func (m *Model) ClickAt(y int) bool {
	contentY := y - m.chromeHeight
	if contentY < 0 {
		return false // click on chrome (header / separator) — ignore
	}
	i, _, ok := m.list.EntryAt(contentY + m.list.YOffset())
	if !ok {
		return false
	}
	e := m.list.Entries()[i]
	if !e.Selectable {
		return false // date separator or "new messages" line
	}
	if m.selected != e.Ref {
		m.selected = e.Ref
		m.dirty()
	}
	return true
}

// HitTest returns the message index, attachment index, and Slack file
// ID of an inline image rendered at (row, col) within the messages-pane
// content area, or ok=false when no image footprint covers that cell.
//
// Coordinate frame: row=0 is the FIRST row of message content (i.e.,
// just below the channel-header chrome); col=0 is the leftmost column
// of the messages pane. The app-level mouse handler subtracts the
// chrome height before calling. Hits come from the most recent View().
func (m *Model) HitTest(row, col int) (msgIdx, attIdx int, fileID string, ok bool) {
	h, ok := m.list.HitTest(entrylist.HitImage, row, col)
	if !ok {
		return 0, 0, "", false
	}
	return h.Ref, h.Aux, h.Key, true
}

// HitTestImage is HitTest returning the message's ts instead of its
// index, the form the App's click routing shares with the thread pane.
func (m *Model) HitTestImage(row, col int) (ts string, attIdx int, ok bool) {
	msgIdx, attIdx, fileID, ok := m.HitTest(row, col)
	if !ok || fileID == "" || msgIdx < 0 || msgIdx >= len(m.messages) {
		return "", 0, false
	}
	return m.messages[msgIdx].TS, attIdx, true
}

// HitTestFile returns the ts of the message and the attachment index
// of the "[File]" line at (row, col), or ok=false. Coordinate frame
// mirrors HitTest.
func (m *Model) HitTestFile(row, col int) (ts string, attIdx int, ok bool) {
	h, ok := m.list.HitTest(entrylist.HitFile, row, col)
	if !ok || h.Ref < 0 || h.Ref >= len(m.messages) {
		return "", 0, false
	}
	return m.messages[h.Ref].TS, h.Aux, true
}

// HitTestReaction returns the (message index, reaction emoji name) at
// (row, col) within the messages-pane content area, or ok=false when
// no reaction pill covers that cell. Coordinate frame mirrors HitTest.
func (m *Model) HitTestReaction(row, col int) (msgIdx int, emoji string, ok bool) {
	h, ok := m.list.HitTest(entrylist.HitReaction, row, col)
	if !ok {
		return 0, "", false
	}
	return h.Ref, h.Key, true
}

var thickLeftBorder = lipgloss.Border{Left: "▌"}

// BeginSelectionAt anchors a new selection at the given pane-local
// coordinates. Clicks on the chrome are ignored; a press on a date
// separator snaps to the nearest message (the channel pane's
// WithSnapDecorationAnchors).
func (m *Model) BeginSelectionAt(viewportY, x int) {
	if viewportY < m.chromeHeight {
		return
	}
	m.list.BeginSelection(m.list.AbsLine(viewportY-m.chromeHeight), x)
}

// ExtendSelectionAt updates the End anchor of the active selection.
func (m *Model) ExtendSelectionAt(viewportY, x int) {
	m.list.ExtendSelection(m.list.AbsLine(viewportY-m.chromeHeight), x)
}

// EndSelection finalizes the drag, returning the plain-text contents of
// the selection. Returns ok=false when the selection is empty (a click
// without drag). The selection itself remains visible until ClearSelection
// is called or a new drag begins.
func (m *Model) EndSelection() (string, bool) { return m.list.EndSelection() }

// ClearSelection removes the current selection, if any.
func (m *Model) ClearSelection() { m.list.ClearSelection() }

// HasSelection reports whether a selection is currently active or
// pinned-on-screen post-drag.
func (m *Model) HasSelection() bool { return m.list.HasSelection() }

// SelectionText extracts the plain-text contents of the current
// selection.
func (m *Model) SelectionText() string { return m.list.SelectionText() }

// ScrollHintForDrag returns -1 if the cursor is within 1 row of the top
// edge of the message-content area, +1 if within 1 row of the bottom, else 0.
func (m *Model) ScrollHintForDrag(viewportY int) int {
	return m.list.ScrollHint(viewportY - m.chromeHeight)
}

// selectedLines returns the rows to draw for the selected entry i: the
// long-timestamp variant, or the short one while a text selection exists.
// The drag overlay splices the plain text, which holds the short form,
// into the displayed row by column; the long header would come out
// garbled. The short variant is memoised in m.cache[i].
func (m *Model) selectedLines(i int) []string {
	e := &m.list.Entries()[i]
	return m.cache[i].SelectedLines(e, m.list.HasSelection(), func(src string) []string {
		return NewWrapStyles(m.focused).SelectedVariant(src, m.cacheWidth)
	})
}

// composeFallbackRow grafts a halfblock fallback row onto the
// placeholder row it replaces. The placeholder carries the row's left
// border, avatar gutter and background fill; a bare fallback row
// would shift the image left under the gutter (one column, six with
// an avatar), drop the border, and leave the row short so stale
// pixels stayed visible right of the image on panes without a
// scrollbar pass to re-pad. Keep the placeholder's first col cells,
// then the fallback, then the placeholder's own tail: cut from the
// row being replaced rather than re-filling from a themed spacer, so
// the tail carries whatever background that row had - the selection
// tint included - instead of the unselected background.
func composeFallbackRow(placeholder, fb string, col int) string {
	end := col + ansi.StringWidth(fb)
	return ansi.Cut(placeholder, 0, col) + fb + ansi.Cut(placeholder, end, ansi.StringWidth(placeholder))
}

// viewInternal is the body shared by View and ViewBare. When
// applySelection is true, the selection-highlight overlay is applied
// to the inner content before the scrollbar overlay. When false, the
// overlay is skipped so the App-layer cache can store a selection-free
// bordered output and run ApplySelectionToBordered as a cheap post-pass.
func (m *Model) viewInternal(height, width int, applySelection bool) string {
	m.list.SetStyles(themeListStyles())
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
		header := headerStyle.Render(fmt.Sprintf("%s %s", ChannelGlyph(m.channelType), m.channelName))
		if m.channelTopic != "" {
			// Width(width) pads every wrapped topic line to the full pane
			// width. This keeps the invariant that EVERY chrome line is
			// exactly `width` display cells, which lets the app layer
			// assemble the pane border by concatenation (no per-frame
			// grapheme re-measurement). See ui.borderedTopPane.
			topicStyle := styles.Timestamp.Width(width).Background(styles.Background)
			header += "\n" + topicStyle.Render(WordWrap(m.channelTopic, width))
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
	// A bottom-anchored pane that got shorter (the compose box grew a
	// row) stays pinned to the bottom: WithPinBottomOnShrink.
	m.list.SetHeight(msgAreaHeight, m.selected)

	if len(m.messages) == 0 {
		text := "No messages yet"
		if m.loading {
			frame := styles.SpinnerChars[m.spinnerFrame%len(styles.SpinnerChars)]
			text = string(frame) + " Loading messages..."
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
	// entry verbatim -- the perf invariant for image bursts. A width
	// change with the previous cache intact re-anchors the scroll
	// position (spec §5.2 U5), which SetEntries does.
	switch {
	case m.cache == nil || m.cacheWidth != width || m.cacheMsgLen != len(m.messages):
		// Perf instrumentation: classify which predicate fired so we
		// can attribute focus-flip churn vs width-resize vs new-message
		// rebuilds in slk-debug.log. SLK_DEBUG=1 only; zero cost
		// otherwise (debuglog.Enabled() is an atomic.Bool load).
		var reason string
		if debuglog.Enabled() {
			switch {
			case m.cache == nil:
				reason = "cache-nil"
			case m.cacheWidth != width:
				reason = fmt.Sprintf("width-changed %d->%d", m.cacheWidth, width)
			default:
				reason = fmt.Sprintf("len-changed %d->%d", m.cacheMsgLen, len(m.messages))
			}
		}
		start := time.Now()
		m.buildCache(width, m.cache != nil)
		debuglog.Perf("messages.buildCache N=%d width=%d took=%s reason=%s",
			len(m.messages), width, time.Since(start), reason)
	case len(m.staleEntries) > 0:
		stale := len(m.staleEntries)
		start := time.Now()
		m.partialRebuild(width)
		debuglog.Perf("messages.partialRebuild N=%d stale=%d width=%d took=%s",
			len(m.messages), stale, width, time.Since(start))
	}

	// Snap to the selection if it moved, clamp, let the cursor follow the
	// scroll (WithCursorFollowsScroll), and slice the visible rows.
	frame := m.list.Layout(m.selected, m.selectedLines)
	m.selected = frame.Cursor

	// Per-frame image side effects:
	//   * Kitty image uploads (entry.flushes) are collected from every
	//     visible entry and written straight to the terminal (below).
	//   * Sixel images are painted out-of-band when the image's full
	//     vertical extent is in the window; under partial visibility, or
	//     where an indicator row would cover it, the per-row halfblock
	//     fallback substitutes for the visible rows.
	//
	// kittyFlushBuf collects the APC upload bytes for any kitty images
	// that need uploading this frame. They are written DIRECTLY to the
	// terminal output rather than embedded in the View() string —
	// lipgloss / bubbletea v2's renderer is known to mangle APC sequences
	// embedded in line content.
	var kittyFlushBuf bytes.Buffer
	var sixelFallback map[int]string
	// Placements for this frame, pane-local. Reset every render: the App
	// reconciles the full set against what is currently on screen, so a
	// stale entry here would keep a vanished image alive.
	pendingSixel := make([]imgpkg.SixelPaint, 0, 2)
	yOffset := m.list.YOffset()
	offsets := m.list.Offsets()
	for _, v := range frame.Visible {
		p := &m.cache[v.Index]
		for _, fl := range p.Flushes {
			if fl != nil {
				_ = fl(&kittyFlushBuf)
			}
		}
		entryStart := offsets[v.Index]
		for startRowInEntry, sx := range p.sixelRows {
			absStart := entryStart + startRowInEntry
			absEnd := absStart + sx.height
			fullyVisible := absStart >= yOffset && absEnd <= yOffset+msgAreaHeight
			windowRow := v.Row + (startRowInEntry - v.From)
			// Indicator rows replace window row 0 (loading hint) and
			// row msgAreaHeight-1 (more below). An image whose footprint
			// includes either is not fully paintable: the indicator is
			// authoritative and must stay visible, so the image takes
			// the partial-visibility fallback path instead.
			intersectsLoading := m.loading && windowRow <= 0 && windowRow+sx.height > 0
			intersectsMoreBelow := frame.MoreBelow && windowRow <= msgAreaHeight-1 && windowRow+sx.height > msgAreaHeight-1
			if fullyVisible && !intersectsLoading && !intersectsMoreBelow {
				if windowRow >= 0 && windowRow < len(frame.Rows) {
					// Record the placement instead of appending the bytes
					// to the line; the App paints these out-of-band after
					// the frame is flushed. See SixelPlacements.
					pendingSixel = append(pendingSixel, imgpkg.SixelPaint{
						Key:   imgpkg.PlacementKey(sx.key, sx.bytes, sx.width, sx.height),
						Row:   windowRow,
						Col:   sx.col,
						Rows:  sx.height,
						Cols:  sx.width,
						Bytes: sx.bytes,
					})
				}
				continue
			}
			for k := 0; k < sx.height; k++ {
				abs := absStart + k
				if abs < yOffset || abs >= yOffset+msgAreaHeight {
					continue
				}
				row := abs - yOffset
				if row >= 0 && row < len(frame.Rows) && k < len(sx.fallback) {
					if sixelFallback == nil {
						sixelFallback = make(map[int]string)
					}
					sixelFallback[row] = composeFallbackRow(frame.Rows[row], sx.fallback[k], sx.col)
				}
			}
		}
	}
	for row, fb := range sixelFallback {
		if fb != "" {
			frame.Rows[row] = fb
		}
	}
	if kittyFlushBuf.Len() > 0 {
		_, _ = imgpkg.KittyOutput.Write(kittyFlushBuf.Bytes())
	}

	// Pad to the pane height, put the loading hint on the first row and
	// "-- more below --" on the last (when content continues below),
	// overlay the selection unless the App paints it as a post-pass, and
	// draw the scrollbar. The loading hint is rendered fresh per frame:
	// it MUST reflect the current spinner frame.
	top := ""
	if m.loading {
		top = m.renderLoadingOlderHint(width)
	}
	visible := m.list.Compose(frame, top, m.cacheMoreBelow, applySelection)

	m.sixelPaints = pendingSixel
	return chrome + "\n" + strings.Join(visible, "\n")
}

// View renders the messages pane with the selection-highlight overlay
// applied to the inner content (unbordered). Direct callers (tests,
// callers not going through the App-layer bordered cache) should call
// View; the App layer calls ViewBare instead so the cached bordered
// output stays selection-free and the cheap ApplySelectionToBordered
// post-pass paints the selection at draw time.
func (m *Model) View(height, width int) string {
	return m.viewInternal(height, width, true)
}

// ViewBare renders the messages pane WITHOUT the selection-highlight
// overlay. Used by the App-layer bordered render cache: caching the
// selection-free output and applying the overlay via
// ApplySelectionToBordered as a post-pass means a mouse-drag selection
// no longer invalidates the bordered render every cell of motion.
func (m *Model) ViewBare(height, width int) string {
	return m.viewInternal(height, width, false)
}

// ApplySelectionToBordered overlays the active selection on top of
// a pre-bordered, pre-cached pane output produced by the App layer.
// Splitting the overlay out of View() lets the App layer cache the
// (selection-free) bordered output keyed on messagepane.Version()
// and survive selection-only mutations.
//
// topBorderRows is the number of border rows above the chrome
// (1 for the standard top-bordered panel). leftBorderCols is the
// number of border columns at the start of each row (1 for the
// standard side-bordered panel). Indicator rows are left alone.
func (m *Model) ApplySelectionToBordered(bordered string, topBorderRows, leftBorderCols int) string {
	m.list.SetStyles(themeListStyles())
	return m.list.ApplySelectionToBordered(bordered, topBorderRows+m.chromeHeight, leftBorderCols)
}

// DateFromTS returns the local-day date string ("2006-01-02") for a
// Slack timestamp (e.g. "1700000000.000100"), or "" if the TS can't be
// parsed. Exported so the thread pane can reuse the same day-boundary
// computation that drives the channel pane's date separators.
func DateFromTS(ts string) string {
	t, ok := timeFromTS(ts)
	if !ok {
		return ""
	}
	return t.Format("2006-01-02")
}

// nowFunc is the clock FormatDateSeparator reads. Production leaves it
// as time.Now; tests override it via SetNowFunc so that day-divider
// labels ("Today", "Yesterday", weekday names) are deterministic.
//
// Mirrors the injectable clock sidebar.Model already carries
// (internal/ui/sidebar/model.go:500), but is package-level rather than a
// struct field because FormatDateSeparator is a free function shared by
// the channel and thread panes.
//
// Not guarded by a mutex. The invariant that makes that safe is not
// "this package's tests are serial" — SetNowFunc is exported and is
// called from three test binaries (this package, internal/ui and
// internal/ui/thread), which cannot see each other's writes. It is
// this: no production code path writes it, so every write comes from
// test setup on the test goroutine before any render, and no test in
// the repo calls t.Parallel. A future parallel test, or any production
// caller, would need this to become an atomic.Value or a struct field.
var nowFunc = time.Now

// SetNowFunc injects a clock for tests. Pass nil to revert to time.Now.
func SetNowFunc(fn func() time.Time) {
	if fn == nil {
		nowFunc = time.Now
		return
	}
	nowFunc = fn
}

// FormatShortDate turns a "2006-01-02" date string into a compact
// label for a message header: "Today", "Yesterday", a weekday within
// the last week, "Jan 2" earlier this year, or "Jan 2, 2006" before
// that. The thread pane prefixes the parent's time with it, because
// the parent is the only row there with no day divider above it.
func FormatShortDate(dateStr string) string {
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	if label := FormatDateSeparator(dateStr); !strings.Contains(label, ",") {
		return label
	}
	if d.Year() == nowFunc().Year() {
		return d.Format("Jan 2")
	}
	return d.Format("Jan 2, 2006")
}

// FormatDateSeparator turns a "2006-01-02" date string into the
// human-readable label used in day-divider rows ("Today", "Yesterday",
// a weekday name within the last week, or a fully-qualified date).
// Exported so the thread pane renders identical labels to the channel
// pane.
func FormatDateSeparator(dateStr string) string {
	d, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return dateStr
	}
	// Compare calendar days, not elapsed time: both endpoints are anchored
	// to UTC midnight so the delta is always a whole number of days.
	now := nowFunc()
	// time.Parse already yields UTC midnight; restating it is defensive, not
	// load-bearing.
	dDay := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(dDay).Hours() / 24)

	switch {
	case days <= 0:
		return "Today"
	case days == 1:
		return "Yesterday"
	case days < 7:
		return d.Format("Monday")
	default:
		return d.Format("Monday, January 2, 2006")
	}
}

// summarizeMessageItems collapses a slice into a compact
// "count=N oldest=<ts> newest=<ts>" string for [cache] log lines.
// Empty/nil slices return "count=0". Mirrors summarizeMessages in
// cmd/slk/main.go but lives here to avoid a circular import.
func summarizeMessageItems(items []MessageItem) string {
	if len(items) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(items), items[0].TS, items[len(items)-1].TS)
}

// AppendUserID returns a new slice with userID appended. Empty IDs are
// skipped and duplicates are not added. The input slice is never mutated --
// reaction UserIDs slices are aliased across the message pane and thread
// panel, so in-place mutation would corrupt the other model's view.
func AppendUserID(ids []string, userID string) []string {
	if userID == "" {
		return ids
	}
	for _, id := range ids {
		if id == userID {
			return ids
		}
	}
	out := make([]string, len(ids), len(ids)+1)
	copy(out, ids)
	return append(out, userID)
}

// RemoveUserID returns a new slice without userID (all occurrences). The
// input slice is never mutated (see AppendUserID for why).
func RemoveUserID(ids []string, userID string) []string {
	if userID == "" || len(ids) == 0 {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != userID {
			out = append(out, id)
		}
	}
	return out
}
