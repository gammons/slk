package thread

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/debuglog"
	emojiutil "github.com/gammons/slk/internal/emoji"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/scrollbar"
	"github.com/gammons/slk/internal/ui/selection"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/usergroups"
)

var thickLeftBorder = lipgloss.Border{Left: "▌"}

// viewEntry is a pre-rendered reply. linesNormal/linesSelected hold the FULLY
// BORDERED rendered content split on "\n" so View() can flatten directly into
// the visible window without any per-frame string scanning, lipgloss render,
// or width measurement. linesPlain mirrors the UNBORDERED content for
// selection extraction. contentColOffset is 1 (the thick left border ▌ that
// linesNormal/linesSelected include).
//
// This shape mirrors internal/ui/messages.viewEntry exactly; keeping them in
// lockstep means scroll and selection logic can be kept in sync.
type viewEntry struct {
	linesNormal      []string
	linesSelected    []string
	linesPlain       []messages.PlainLine
	height           int
	replyIdx         int
	contentColOffset int

	// flushes are per-frame side effects (kitty image upload escapes)
	// returned by imgrender.Renderer.RenderBlock for inline image
	// attachments on this reply. Invoked during View() with a per-frame
	// buffer that is then written directly to imgpkg.KittyOutput.
	// Mirrors internal/ui/messages viewEntry.flushes. nil for entries
	// with no inline image attachments.
	//
	// v1 scope: sixel inline-byte injection is deferred — sixel images
	// render as their imgrender placeholder/sentinel only, so no
	// sixelRows field is captured here.
	flushes []func(io.Writer) error

	// reactionHits records the column extents of each rendered
	// reaction pill on this reply, in coordinates relative to
	// linesNormal. View() translates these to pane-local coordinates
	// (including the thread chromeHeight offset) and appends to
	// Model.lastReactionHits per frame so the app-level mouse handler
	// can route clicks to a toggle-reaction command.
	reactionHits []reactionEntryHit
}

// reactionEntryHit is one reaction-pill hit-rect, expressed in
// coordinates relative to a single viewEntry's linesNormal. Mirrors
// internal/ui/messages.reactionEntryHit; kept package-local so the
// thread panel doesn't import unexported types.
type reactionEntryHit struct {
	rowStartInEntry int
	rowEndInEntry   int // exclusive
	colStart        int
	colEnd          int // exclusive
	emoji           string
}

// reactionHitRect is one clickable reaction-pill footprint in the
// thread pane, expressed in pane-local coordinates (rowStart is
// measured from the top of the panel, AFTER the chromeHeight rows).
type reactionHitRect struct {
	rowStart int
	rowEnd   int // exclusive
	colStart int
	colEnd   int // exclusive
	replyIdx int
	emoji    string
}

// Model represents the thread panel UI component.
// It displays a parent message and its replies with cursor navigation.
// parentSelected is the sentinel value of Model.selected meaning the
// cursor sits on the parent message (the row above reply index 0).
const parentSelected = -1

type Model struct {
	parent            messages.MessageItem
	replies           []messages.MessageItem
	channelID         string
	threadTS          string
	selected          int
	focused           bool
	coloredUsernames  bool
	avatarFn          messages.AvatarFunc
	userNames         map[string]string
	channelNames      map[string]string
	vp                viewport.Model
	reactionNavActive bool
	reactionNavIndex  int
	// currentUserID is the authenticated user; UpdateReaction uses it to
	// set HasReacted only for the current user's own reactions.
	currentUserID string

	// Render cache -- pre-rendered reply entries (unbordered content
	// captured per reply; borders are applied later when assembling
	// viewContent).
	cache         []viewEntry
	cacheWidth    int
	cacheReplyLen int

	// entryOffsets / totalLines mirror the FULLY BORDERED viewContent:
	// entryOffsets[i] is the absolute line index inside viewContent where
	// reply i starts. Inter-reply separators occupy a single line that is
	// NOT inside any entry's [start, start+height) range; selection
	// overlay/extraction skips them naturally.
	entryOffsets []int
	totalLines   int

	// parentEntry mirrors the rendered parent message at the start of
	// viewContent. It provides the same plain-text and column metadata as
	// reply entries so mouse selection can address the parent.
	parentEntry viewEntry

	// View-level cache -- bordered content ready for viewport
	viewContent       string
	viewSelected      int
	viewWidth         int
	viewHeight        int
	viewCacheValid    bool
	selectedStartLine int
	selectedEndLine   int

	// chromeCache holds the rendered "header + separator + parent message +
	// separator" prefix that View() prepends to viewContent. Rebuilt only
	// when its inputs (width, replyCount, parent identity, parent text,
	// userNames, channelNames) change. On a plain j/k it is reused as-is.
	chromeCache         string
	chromeCacheValid    bool
	chromeWidth         int
	chromeReplyCount    int
	chromeUserNamesV    uint64 // version of the userNames map at build time
	chromeChannelNamesV uint64 // version of the channelNames map at build time

	// userNamesV / channelNamesV are bumped every time SetUserNames /
	// SetChannelNames replaces the map. Used by chromeCache (and any other
	// cache that depends on these maps) to detect changes without hashing.
	userNamesV    uint64
	channelNamesV uint64
	userGroups    map[string]string

	// Mouse selection state. selRange is the user's drag selection.
	// replyIDToIdx maps reply TS -> entry index in m.cache for O(1)
	// anchor resolution; rebuilt on every cache build. lastViewHeight is
	// captured during View() so ScrollHintForDrag knows the reply-area
	// bounds without needing the App to plumb them through.
	selRange       selection.Range
	hasSelection   bool
	replyIDToIdx   map[string]int
	lastViewHeight int
	// chromeHeight is the number of visual rows occupied by the thread's
	// chrome (header + separator + parent message + separator) at the top
	// of View()'s output. It's stored so click / drag handlers can offset
	// pane-local y coordinates into the reply-content coordinate space the
	// rest of the model operates in.
	chromeHeight int

	// lastReactionHits holds the reaction-pill hit rects captured
	// during the most recent View() call, in pane-local coordinates
	// (rowStart is measured from the panel top, AFTER chromeHeight
	// rows). Consumed by HitTestReaction so the app-level mouse
	// handler can toggle a reaction when the user clicks a pill.
	lastReactionHits []reactionHitRect

	// unreadBoundaryTS is the Slack timestamp the user has already read up
	// to in this thread. Replies whose TS > unreadBoundaryTS are considered
	// new; a "── new ──" landmark is inserted between the last read reply
	// and the first new one. Empty string disables the landmark. Set via
	// SetUnreadBoundary, typically with the parent channel's last_read_ts
	// at the moment the thread is opened.
	unreadBoundaryTS string

	// snappedSelection lets View() avoid re-snapping vp.YOffset back to the
	// selected reply on every render. While snappedSelection == selected,
	// mouse-wheel / programmatic scrolls (ScrollUp/ScrollDown) are preserved
	// across renders -- mirrors the same convention used by messages.Model.
	snappedSelection int
	hasSnapped       bool

	// version increments on every state change that could alter View() output.
	version int64

	// imgRenderer is the inline-image rendering pipeline. Configured at
	// startup via Model.SetImageContext (mirrors messages.Model). v1
	// renders images inline (kitty + halfblock fully supported; sixel
	// renders placeholder-only) but does not support click-to-preview
	// from a thread reply.
	imgRenderer *imgrender.Renderer

	// emojiCtx bundles the emoji-image rendering dependencies. Threaded
	// through RenderSlackMarkdownWith for reply body text and consulted
	// directly when building reply reaction pills. Configured at startup
	// via Model.SetEmojiContext (mirrors messages.Model).
	emojiCtx EmojiContext
}

// EmojiContext bundles the emoji-image rendering dependencies for the
// thread pane. Mirrors messages.EmojiContext in shape and purpose —
// see internal/ui/messages/model.go for the architectural rationale.
type EmojiContext struct {
	PlaceCtx emojiutil.PlaceContext
	Cells    int               // 1 or 2; 0 falls back to 2
	Customs  map[string]string // workspace custom emoji map; nil = empty
}

// New creates an empty thread panel.
func New() *Model {
	return &Model{
		imgRenderer: imgrender.NewRenderer(),
	}
}

// SetImageContext configures the inline-image rendering pipeline for
// the thread panel. Triggers a cache rebuild so existing replies
// re-render with the new context. Mirrors messages.Model.SetImageContext.
func (m *Model) SetImageContext(ctx imgrender.ImageContext) {
	if m.imgRenderer == nil {
		m.imgRenderer = imgrender.NewRenderer()
	}
	m.imgRenderer.SetContext(ctx)
	m.InvalidateCache()
}

// SetEmojiContext configures emoji-image rendering for thread replies.
// Should be called once at startup (cmd/slk/main.go) and again after
// CustomEmojisLoadedMsg arrives (via SetEmojiCustoms). Mirrors
// messages.Model.SetEmojiContext.
func (m *Model) SetEmojiContext(ctx EmojiContext) {
	if ctx.Cells != 1 && ctx.Cells != 2 {
		ctx.Cells = 2
	}
	m.emojiCtx = ctx
	m.InvalidateCache()
}

// SetEmojiCustoms updates only the customs map on the active emoji
// context, leaving PlaceCtx and Cells untouched. Invalidates the cache
// so the new map is consulted on the next View(). Mirrors
// messages.Model.SetEmojiCustoms.
func (m *Model) SetEmojiCustoms(customs map[string]string) {
	m.emojiCtx.Customs = customs
	m.InvalidateCache()
}

// HandleEmojiImageReady is invoked by the host (App.Update) when an
// emoji.EmojiImageReadyMsg lands. The same emoji can appear anywhere
// in the open thread (body text or reaction pills), so v1 performs a
// wholesale render-cache invalidation; the next View() rebuilds with
// the now-warm emoji placement. Mirrors messages.Model.HandleEmojiImageReady.
func (m *Model) HandleEmojiImageReady(url string) {
	debuglog.ImgFetch("thread.HandleEmojiImageReady: url=%s wholesale_invalidate", url)
	m.InvalidateCache()
}

// Version returns a counter that increments any time the View() output could
// change. Used by App's panel-output cache.
func (m *Model) Version() int64 { return m.version }

func (m *Model) dirty() { m.version++ }

// InvalidateCache forces the render cache to be rebuilt on next View().
// Call this after theme changes or style updates.
func (m *Model) InvalidateCache() {
	m.cache = nil
	m.viewCacheValid = false
	m.chromeCacheValid = false
	m.dirty()
}

// HandleAvatarReady invalidates the thread render cache so the next
// View() picks up the now-rendered avatar for userID. Mirrors
// messages.Model.HandleAvatarReady. Empty UserID is a no-op.
//
// Workspace-coarse: we don't try to scope by channel/thread identity
// because (a) avatar arrivals are rare and (b) the thread panel only
// renders if it's visible, so an invalidation while hidden is a noop
// at next View() time anyway.
func (m *Model) HandleAvatarReady(userID string) {
	if userID == "" {
		return
	}
	m.InvalidateCache()
}

// SetThread populates the thread panel with a parent message and replies.
// The cursor starts at the bottom (newest reply). When the channel/thread
// identity changes (i.e. the user is opening a different thread, not just
// receiving a refresh of the current one), the unread boundary is cleared
// so a fresh boundary can be set by the caller via SetUnreadBoundary.
func (m *Model) SetThread(parent messages.MessageItem, replies []messages.MessageItem, channelID, threadTS string) {
	if channelID != m.channelID || threadTS != m.threadTS {
		m.unreadBoundaryTS = ""
	}
	m.ClearSelection()
	m.parent = parent
	m.replies = replies
	m.channelID = channelID
	m.threadTS = threadTS
	// Per the doc comment, the cursor starts at the bottom (newest reply)
	// so a long thread opens scrolled to the latest activity rather than
	// jammed up at the parent message. A reply-less thread selects the
	// parent (parentSelected) so message ops still have a target.
	if len(replies) > 0 {
		m.selected = len(replies) - 1
	} else {
		m.selected = parentSelected
	}
	// Force the next View() to re-snap the viewport to the new selection.
	// Without this, opening a thread whose newest-reply index matches the
	// previously-viewed thread's snapped selection skips the snap and leaves
	// the viewport scrolled to the top. Mirrors messages.Model.SetMessages.
	m.hasSnapped = false
	m.InvalidateCache()
}

// SetUnreadBoundary sets the timestamp the user has already read up to in
// this thread. A "── new ──" landmark is rendered between the last reply
// with TS <= boundary and the first reply with TS > boundary. Pass "" to
// clear the boundary. Typically called by the App right after SetThread,
// using the thread's own last-read cursor from thread_subscriptions.
func (m *Model) SetUnreadBoundary(ts string) {
	if m.unreadBoundaryTS == ts {
		return
	}
	m.unreadBoundaryTS = ts
	m.viewCacheValid = false
	m.dirty()
}

// UnreadBoundaryTS returns the current unread-boundary ts. Used by tests.
func (m *Model) UnreadBoundaryTS() string {
	return m.unreadBoundaryTS
}

// AddReply appends a reply to the thread and scrolls to the bottom.
// We always advance `selected` to the new last index so the incoming
// reply is visible regardless of where the user had scrolled.
func (m *Model) AddReply(msg messages.MessageItem) {
	// Idempotent on TS -- same race-defense rationale as
	// messages.Model.AppendMessage: optimistic add (HTTP response) and
	// WS echo can arrive in either order, and a caller-side dedup map
	// can lose the race if the echo lands first.
	if msg.TS != "" {
		for i := len(m.replies) - 1; i >= 0; i-- {
			if m.replies[i].TS == msg.TS {
				return
			}
		}
	}
	m.replies = append(m.replies, msg)
	m.InvalidateCache()
	m.selected = len(m.replies) - 1
}

// SwapLocalSentReply replaces an optimistic placeholder identified
// by localTS (an internal "local:..." id assigned when the user
// pressed Enter on a thread reply, before chat.postMessage returned
// the real Slack TS) with the authoritative msg. Returns true if a
// row matching localTS was found and swapped, false otherwise.
// Mirrors messages.Model.SwapLocalSent.
func (m *Model) SwapLocalSentReply(localTS string, msg messages.MessageItem) bool {
	if localTS == "" {
		return false
	}
	for i := len(m.replies) - 1; i >= 0; i-- {
		if m.replies[i].TS == localTS {
			m.replies[i] = msg
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// RemoveLocalSentReply removes an optimistic placeholder reply
// identified by localTS. Used when the chat.postMessage HTTP call
// fails and we want to roll back the instant-display add. Returns
// true if a row was removed.
func (m *Model) RemoveLocalSentReply(localTS string) bool {
	if localTS == "" {
		return false
	}
	for i := len(m.replies) - 1; i >= 0; i-- {
		if m.replies[i].TS == localTS {
			m.replies = append(m.replies[:i], m.replies[i+1:]...)
			m.InvalidateCache()
			if m.selected >= len(m.replies) && len(m.replies) > 0 {
				m.selected = len(m.replies) - 1
			}
			return true
		}
	}
	return false
}

// UpsertSelfSentReply is the optimistic-add variant of AddReply for
// thread replies the user just sent themselves. If a reply with the
// same TS already exists (e.g. a WS echo arrived faster than the
// HTTP response and AddReply stored its version first), this method
// REPLACES that entry's contents with msg. Otherwise it appends.
//
// Mirrors messages.Model.UpsertSelfSent — see that method for the
// motivating bug (Slack's WS-echo Text may flatten paragraph breaks
// for rich_text_block messages).
func (m *Model) UpsertSelfSentReply(msg messages.MessageItem) {
	if msg.TS != "" {
		for i := len(m.replies) - 1; i >= 0; i-- {
			if m.replies[i].TS == msg.TS {
				m.replies[i] = msg
				m.InvalidateCache()
				return
			}
		}
	}
	m.replies = append(m.replies, msg)
	m.InvalidateCache()
	m.selected = len(m.replies) - 1
}

// Clear resets all thread state.
func (m *Model) Clear() {
	m.ClearSelection()
	m.parent = messages.MessageItem{}
	m.replies = nil
	m.channelID = ""
	m.threadTS = ""
	m.selected = parentSelected
	m.InvalidateCache()
}

// ThreadTS returns the thread timestamp.
func (m *Model) ThreadTS() string {
	return m.threadTS
}

// ChannelID returns the channel ID this thread belongs to.
func (m *Model) ChannelID() string {
	return m.channelID
}

// IsEmpty returns true if no thread is loaded.
func (m *Model) IsEmpty() bool {
	return m.threadTS == ""
}

// HasReply returns true when the open thread contains a reply with the
// given TS. App.Update uses this to decide whether to invalidate the
// thread cache on ImageReadyMsg.
//
// Note: replyIDToIdx is built lazily during View() (see the cache-build
// path), so HasReply may return false for replies whose cache hasn't
// been built yet. That's acceptable for v1 — when the user opens a
// thread, View() runs at least once before any image bytes arrive.
// Returning false when the index is nil is the safe default; the cache
// is rebuilt on the next frame anyway.
func (m *Model) HasReply(ts string) bool {
	if m.replyIDToIdx == nil {
		return false
	}
	_, ok := m.replyIDToIdx[ts]
	return ok
}

// HandleImageFailed clears the in-flight bit for key on the thread's
// renderer and marks it as permanently failed for this session.
// App.Update calls this when an ImageFailedMsg lands so the thread's
// renderer state stays in sync with the messages-pane renderer's.
func (m *Model) HandleImageFailed(key string) {
	if m.imgRenderer == nil {
		debuglog.ImgFetch("thread.HandleImageFailed: thread_ts=%q key=%s SKIP (no renderer)",
			m.threadTS, key)
		return
	}
	tracked := m.imgRenderer.MarkFailed(key)
	debuglog.ImgFetch("thread.HandleImageFailed: thread_ts=%q key=%s was_in_flight=%v",
		m.threadTS, key, tracked)
}

// ReplyCount returns the number of replies.
func (m *Model) ReplyCount() int {
	return len(m.replies)
}

// ParentMsg returns the parent message.
func (m *Model) ParentMsg() messages.MessageItem {
	return m.parent
}

// Replies returns the slice of currently-loaded thread replies. Used by
// the App for cross-pane lookups (e.g. resolving an attachment for the
// preview overlay when the click landed inside the thread panel).
func (m *Model) Replies() []messages.MessageItem {
	return m.replies
}

// UserNames returns the user-ID-to-display-name map used for rendering mentions.
func (m *Model) UserNames() map[string]string { return m.userNames }

// ChannelNames returns the channel-ID-to-name map used for rendering channel mentions.
func (m *Model) ChannelNames() map[string]string { return m.channelNames }

// UpdateMessageInPlace finds a reply by TS and replaces its text,
// marking it edited. Returns true if found.
func (m *Model) UpdateMessageInPlace(ts, newText string) bool {
	for i, r := range m.replies {
		if r.TS == ts {
			m.replies[i].Text = newText
			m.replies[i].IsEdited = true
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// RemoveMessageByTS removes a reply by TS, adjusting the selected
// index so it remains valid. Returns true if found.
func (m *Model) RemoveMessageByTS(ts string) bool {
	for i, r := range m.replies {
		if r.TS == ts {
			m.replies = append(m.replies[:i], m.replies[i+1:]...)
			if i <= m.selected && m.selected > 0 {
				m.selected--
			}
			if m.selected >= len(m.replies) {
				if len(m.replies) == 0 {
					m.selected = parentSelected
				} else {
					m.selected = len(m.replies) - 1
				}
			}
			m.InvalidateCache()
			return true
		}
	}
	return false
}

// UpdateParentInPlace updates the thread parent's text and marks it
// edited if its TS matches. Returns true if updated.
func (m *Model) UpdateParentInPlace(ts, newText string) bool {
	if m.parent.TS != ts {
		return false
	}
	m.parent.Text = newText
	m.parent.IsEdited = true
	m.InvalidateCache()
	return true
}

// SetFocused sets whether the thread panel has focus. When the value flips
// the view-level cache is invalidated so the selected reply's "▌" border
// re-renders with the appropriate color (Accent when focused, TextMuted when
// not — see styles.SelectionBorderColor).
func (m *Model) SetFocused(focused bool) {
	if m.focused != focused {
		m.focused = focused
		m.viewCacheValid = false
		m.dirty()
		debuglog.Perf("thread.SetFocused flip focused=%v replies=%d viewCache=invalidated",
			focused, len(m.replies))
	}
}

// Focused returns whether the thread panel has focus.
func (m *Model) Focused() bool {
	return m.focused
}

// SetAvatarFunc sets the avatar rendering function.
func (m *Model) SetAvatarFunc(fn messages.AvatarFunc) {
	m.avatarFn = fn
}

// SetUserNames sets the user ID -> display name map for mention resolution.
// Bumps userNamesV unconditionally so chromeCache (and any other cache
// keyed by this version counter) sees the change via a simple `!=` check.
func (m *Model) SetUserNames(names map[string]string) {
	m.userNames = names
	m.userNamesV++
	m.InvalidateCache()
}

// SetColoredUsernames enables or disables deterministic per-user coloring
// of usernames. Invalidates the render cache so existing rows re-render
// with the new coloring.
func (m *Model) SetColoredUsernames(enabled bool) {
	m.coloredUsernames = enabled
	m.InvalidateCache()
}

// SetUserGroups sets the workspace-scoped usergroup ID -> handle map used
// to resolve bare <!subteam^SID> mentions in the parent and replies.
// No-op when the new map matches the current one -- App.SetUserGroups
// fires on every workspace switch, and busting the render cache for an
// identical set is pure waste.
func (m *Model) SetUserGroups(groups map[string]string) {
	if usergroups.Equal(m.userGroups, groups) {
		return
	}
	m.userGroups = usergroups.Copy(groups)
	m.InvalidateCache()
}

// SetCurrentUser records the authenticated user ID so UpdateReaction can
// flag the current user's own reactions (HasReacted) correctly.
func (m *Model) SetCurrentUser(userID string) {
	m.currentUserID = userID
}

// PatchUserName updates the in-memory userNames map (used for @mention
// rendering) and overwrites the UserName field on the parent message
// and every cached reply authored by userID. Always invalidates the
// render cache after a map change so mentions of <@userID> in other
// authors' text re-resolve. Idempotent: no-op when the name is
// unchanged.
//
// Mirrors messages.Model.PatchUserName. Used by the async user-
// resolution path: history fetchers stash MessageItem.UserName =
// m.UserID for unknown authors. When the resolution returns
// asynchronously, the App calls PatchUserName to replace the
// placeholders live without re-fetching the thread.
func (m *Model) PatchUserName(userID, displayName string) {
	if userID == "" {
		return
	}
	if m.userNames == nil {
		m.userNames = map[string]string{}
	}
	if m.userNames[userID] == displayName {
		return
	}
	m.userNames[userID] = displayName
	// The render cache stores rows with their mentions already resolved
	// (RenderSlackMarkdown consults userNames at render time), so any
	// cached row that mentioned <@userID> is now stale. Mirror
	// SetUserNames's behavior: invalidate unconditionally on map change.
	// Bump userNamesV so chromeCache (which renders the parent message
	// and consults userNames for its mentions) also rebuilds.
	m.userNamesV++
	m.cache = nil
	m.viewCacheValid = false
	if m.parent.UserID == userID && m.parent.UserName != displayName {
		m.parent.UserName = displayName
	}
	for i := range m.replies {
		if m.replies[i].UserID == userID && m.replies[i].UserName != displayName {
			m.replies[i].UserName = displayName
		}
	}
	m.dirty()
}

// SetChannelNames sets the channel ID -> name map used to resolve bare
// <#CHANNELID> mentions in thread replies. Bumps channelNamesV
// unconditionally; see SetUserNames for the rationale.
func (m *Model) SetChannelNames(names map[string]string) {
	m.channelNames = names
	m.channelNamesV++
	m.InvalidateCache()
}

// SelectedReply returns the currently selected reply, or nil if none.
func (m *Model) SelectedReply() *messages.MessageItem {
	// The parent is a selectable row like any reply — returning it here
	// makes every SelectedReply-driven message op (react, permalink,
	// yank, open links) work on the thread's first message too.
	if m.selected == parentSelected && m.parent.TS != "" {
		return &m.parent
	}
	if m.selected < 0 || m.selected >= len(m.replies) {
		return nil
	}
	return &m.replies[m.selected]
}

// SelectByIndex moves the selection cursor to i (an index into Replies()).
// No-op if i is out of range. Used by tests that need a deterministic
// selection state.
func (m *Model) SelectByIndex(i int) {
	if i < 0 || i >= len(m.replies) {
		return
	}
	if m.selected != i {
		m.selected = i
		m.InvalidateCache()
	}
}

// MoveUp moves the selection cursor up one reply.
// ScrollUp scrolls the thread viewport up by n lines without changing the
// selected reply. Marks the current selection as already-snapped so View()
// won't pull the viewport back to keep selection visible on the next render.
func (m *Model) ScrollUp(n int) {
	if n > 0 {
		m.vp.ScrollUp(n)
		m.snappedSelection = m.selected
		m.hasSnapped = true
		m.dirty()
	}
}

// ScrollDown scrolls the thread viewport down by n lines without changing the
// selected reply. Marks the current selection as already-snapped so View()
// won't pull the viewport back on the next render.
func (m *Model) ScrollDown(n int) {
	if n > 0 {
		m.vp.ScrollDown(n)
		m.snappedSelection = m.selected
		m.hasSnapped = true
		m.dirty()
	}
}

// ViewportAtTop reports whether the thread viewport is scrolled to the top.
// Used by the app layer to detect a wheel-up / PageUp that hit the top of the
// reply stream (cosmetic only -- no thread backfill exists today).
func (m *Model) ViewportAtTop() bool {
	return m.vp.YOffset() == 0
}

func (m *Model) MoveUp() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	// The parent row (parentSelected, index -1) sits above the first
	// reply, so the cursor can move one step past index 0.
	if m.selected > parentSelected {
		m.selected--
		m.dirty()
	}
}

// MoveDown moves the selection cursor down one reply.
func (m *Model) MoveDown() {
	if m.reactionNavActive {
		m.ExitReactionNav()
	}
	if m.selected < len(m.replies)-1 {
		m.selected++
		m.dirty()
	}
}

func (m *Model) IsAtBottom() bool {
	return m.selected >= len(m.replies)-1
}

// GoToTop moves the selection to the first reply.
func (m *Model) GoToTop() {
	if m.selected != 0 {
		m.selected = 0
		m.dirty()
	}
}

// GoToBottom moves the selection to the last reply.
func (m *Model) GoToBottom() {
	if len(m.replies) > 0 && m.selected != len(m.replies)-1 {
		m.selected = len(m.replies) - 1
		m.dirty()
	}
}

// EnterReactionNav activates reaction navigation on the selected reply.
func (m *Model) EnterReactionNav() {
	if reply := m.SelectedReply(); reply != nil && len(reply.Reactions) > 0 {
		m.reactionNavActive = true
		m.reactionNavIndex = 0
		m.InvalidateCache()
	}
}

// ExitReactionNav deactivates reaction navigation.
func (m *Model) ExitReactionNav() {
	m.reactionNavActive = false
	m.reactionNavIndex = 0
	m.InvalidateCache()
}

// ReactionNavActive returns whether reaction navigation is active.
func (m *Model) ReactionNavActive() bool {
	return m.reactionNavActive
}

// ReactionNavLeft moves the reaction cursor left with wrapping.
func (m *Model) ReactionNavLeft() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex - 1 + total) % total
	m.InvalidateCache()
}

// ReactionNavRight moves the reaction cursor right with wrapping.
func (m *Model) ReactionNavRight() {
	reply := m.SelectedReply()
	if reply == nil {
		return
	}
	total := len(reply.Reactions) + 1
	m.reactionNavIndex = (m.reactionNavIndex + 1) % total
	m.InvalidateCache()
}

// SelectedReaction returns the currently highlighted reaction emoji name,
// or isPlus=true if the "+" button is highlighted.
func (m *Model) SelectedReaction() (emojiName string, isPlus bool) {
	reply := m.SelectedReply()
	if reply == nil {
		return "", false
	}
	if m.reactionNavIndex >= len(reply.Reactions) {
		return "", true
	}
	return reply.Reactions[m.reactionNavIndex].Emoji, false
}

// ClampReactionNav ensures the reaction nav index is within bounds.
func (m *Model) ClampReactionNav() {
	reply := m.SelectedReply()
	if reply == nil || len(reply.Reactions) == 0 {
		m.ExitReactionNav()
		return
	}
	total := len(reply.Reactions) + 1
	if m.reactionNavIndex >= total {
		m.reactionNavIndex = total - 1
	}
	m.InvalidateCache()
}

// UpdateReaction updates the reaction state for a specific message in the thread.
func (m *Model) UpdateReaction(messageTS, emojiName, userID string, remove bool) {
	var msg *messages.MessageItem
	if m.parent.TS == messageTS {
		msg = &m.parent
	} else {
		for i := range m.replies {
			if m.replies[i].TS == messageTS {
				msg = &m.replies[i]
				break
			}
		}
	}
	if msg == nil {
		return
	}

	m.updateMessageReaction(msg, emojiName, userID, remove)
	m.InvalidateCache()
	if m.reactionNavActive {
		m.ClampReactionNav()
	}
}

// updateMessageReaction applies one idempotent reaction event to msg.
func (m *Model) updateMessageReaction(msg *messages.MessageItem, emojiName, userID string, remove bool) {
	if remove {
		for j, r := range msg.Reactions {
			if r.Emoji != emojiName {
				continue
			}
			newIDs := messages.RemoveUserID(r.UserIDs, userID)
			if len(newIDs) == len(r.UserIDs) && r.Count <= len(r.UserIDs) {
				return
			}
			r.UserIDs = newIDs
			r.Count--
			if userID == m.currentUserID {
				r.HasReacted = false
			}
			if r.Count <= 0 {
				msg.Reactions = append(msg.Reactions[:j], msg.Reactions[j+1:]...)
			} else {
				msg.Reactions[j] = r
			}
			return
		}
		return
	}

	for j, r := range msg.Reactions {
		if r.Emoji != emojiName {
			continue
		}
		newIDs := messages.AppendUserID(r.UserIDs, userID)
		if len(newIDs) != len(r.UserIDs) {
			r.UserIDs = newIDs
			r.Count++
		}
		if userID == m.currentUserID {
			r.HasReacted = true
		}
		msg.Reactions[j] = r
		return
	}
	msg.Reactions = append(msg.Reactions, messages.ReactionItem{
		Emoji:      emojiName,
		Count:      1,
		HasReacted: userID == m.currentUserID,
		UserIDs:    messages.AppendUserID(nil, userID),
	})
}

// ClickAt handles a mouse click at the given y-coordinate (the pane-local
// y returned by App.panelAt — measured from the panel's top border, so
// y=0..chromeHeight-1 sits inside the chrome (header + separator) and
// y=chromeHeight onward is the scrolling parent+replies content). Clicks
// in the chrome are ignored: absoluteLineAt clamps them to the first
// content line, which would otherwise read as a click on the parent.
func (m *Model) ClickAt(y int) {
	if y < m.chromeHeight {
		return
	}
	entry, _, messageTS, ok := m.selectionEntryAt(m.absoluteLineAt(y))
	if !ok {
		return
	}
	selected := entry.replyIdx
	if messageTS == m.parent.TS {
		selected = parentSelected
	}
	if m.selected != selected {
		m.selected = selected
		m.viewCacheValid = false
		m.dirty()
	}
}

// BeginSelectionAt anchors a new selection at the given pane-local
// coordinates (App.panelAt's coordinate system: 0 == panel content top,
// just below the border). Chrome rows (header + separator at
// y < chromeHeight) are ignored; the scrolling parent+replies content
// begins at chromeHeight. The selection becomes Active. Out-of-range
// inputs that do not land on a message entry are silently no-ops.
func (m *Model) BeginSelectionAt(viewportY, x int) {
	if viewportY < m.chromeHeight {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	m.selRange = selection.Range{Start: a, End: a, Active: true}
	m.hasSelection = true
	m.dirty()
}

// ExtendSelectionAt updates the End anchor of the active selection.
// No-op if BeginSelectionAt was never called or the coordinates fall
// on a non-entry row (inter-reply separator).
func (m *Model) ExtendSelectionAt(viewportY, x int) {
	if !m.hasSelection {
		return
	}
	abs := m.absoluteLineAt(viewportY)
	a, ok := m.anchorAt(abs, x)
	if !ok {
		return
	}
	// Perf: MouseModeCellMotion fires a MouseMotionMsg for every
	// cell traversed while the button is held, and many of those
	// cells resolve (via anchorAt) to the same end anchor. Bumping
	// m.version unconditionally would bust the thread-pane render
	// cache on every motion event. Skip the dirty when nothing
	// actually changed.
	if a == m.selRange.End {
		return
	}
	m.selRange.End = a
	m.dirty()
}

// EndSelection finalizes the drag, returning the plain-text contents
// of the selection. Returns ok=false when the selection is empty
// (a click without drag).
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
func (m *Model) HasSelection() bool { return m.hasSelection }

// ScrollHintForDrag returns -1 if the cursor is within 1 row of the top
// edge of the scrolling content, +1 if within 1 row of the bottom, else 0.
// The incoming viewportY is pane-local (0 == top of panel content, just
// below the border). We offset by m.chromeHeight so the top edge is measured
// below the header and separator. A cursor sitting on chrome is treated as
// the top content row, so an upward drag keeps auto-scrolling toward older
// messages.
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

// absoluteLineAt converts a pane-local y coordinate to an absolute line
// index inside m.viewContent (the bordered content the viewport scrolls
// through). Rows 0..chromeHeight-1 are the thread chrome (header +
// separator); chromeHeight onward is scrolling parent+replies content. We
// strip the chrome offset before mapping into viewContent lines, clamping
// negative (in-chrome) values to the first content line. The result is
// clamped to [0, totalLines-1] for out-of-range inputs.
func (m *Model) absoluteLineAt(viewportY int) int {
	contentY := viewportY - m.chromeHeight
	if contentY < 0 {
		contentY = 0
	}
	abs := contentY + m.vp.YOffset()
	if abs < 0 {
		abs = 0
	}
	if m.totalLines > 0 && abs >= m.totalLines {
		abs = m.totalLines - 1
	}
	return abs
}

// selectionEntryAt returns the rendered message entry containing absLine.
// Parent lines begin at zero; reply lines use parent-inclusive offsets.
func (m *Model) selectionEntryAt(absLine int) (*viewEntry, int, string, bool) {
	if m.parent.TS != "" && absLine >= 0 && absLine < m.parentEntry.height {
		return &m.parentEntry, 0, m.parent.TS, true
	}
	for i := range m.cache {
		start := m.entryOffsets[i]
		if absLine >= start && absLine < start+m.cache[i].height {
			return &m.cache[i], start, m.replies[m.cache[i].replyIdx].TS, true
		}
	}
	return nil, 0, "", false
}

// anchorAt converts an absolute line + display column into an Anchor.
// `col` is the mouse's display column (relative to the reply area's
// content). We subtract contentColOffset to get the plain column, then
// clamp to plain-line width. Returns ok=false when no entry covers the
// line (inter-reply separator) or when the cache is empty.
func (m *Model) anchorAt(absLine, col int) (selection.Anchor, bool) {
	entry, start, messageTS, ok := m.selectionEntryAt(absLine)
	if !ok {
		return selection.Anchor{}, false
	}
	line := absLine - start
	plainCol := col - entry.contentColOffset
	if plainCol < 0 {
		plainCol = 0
	}
	if line < len(entry.linesPlain) {
		if width := messages.DisplayWidthOfPlain(entry.linesPlain[line]); plainCol > width {
			plainCol = width
		}
	}
	return selection.Anchor{MessageID: messageTS, Line: line, Col: plainCol}, true
}

// resolveAnchor returns the absolute line + plain col for an Anchor.
// Returns ok=false when the reply is no longer present.
func (m *Model) resolveAnchor(a selection.Anchor) (absLine, col int, ok bool) {
	if a.MessageID == "" {
		return 0, 0, false
	}
	if a.MessageID == m.parent.TS {
		if a.Line < 0 || a.Line >= m.parentEntry.height {
			return 0, 0, false
		}
		return a.Line, a.Col, true
	}
	idx, found := m.replyIDToIdx[a.MessageID]
	if !found || idx >= len(m.cache) {
		return 0, 0, false
	}
	entry := m.cache[idx]
	if a.Line < 0 || a.Line >= entry.height {
		return 0, 0, false
	}
	return m.entryOffsets[idx] + a.Line, a.Col, true
}

// SelectionText extracts the plain-text contents of the current
// selection. Trailing whitespace is trimmed per line; a final trailing
// newline is removed. Multi-rune grapheme clusters are preserved
// intact.
func (m *Model) SelectionText() string {
	if !m.hasSelection || m.selRange.IsEmpty() {
		return ""
	}
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 || loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return ""
	}
	var b strings.Builder
	appendSelectionText(&b, 0, m.parentEntry, loLine, loCol, hiLine, hiCol)
	for i, entry := range m.cache {
		appendSelectionText(&b, m.entryOffsets[i], entry, loLine, loCol, hiLine, hiCol)
	}
	return strings.TrimRight(b.String(), "\n")
}

func appendSelectionText(b *strings.Builder, entryStart int, entry viewEntry, loLine, loCol, hiLine, hiCol int) {
	entryEnd := entryStart + entry.height
	if entryEnd <= loLine || entryStart > hiLine {
		return
	}
	for line, plain := range entry.linesPlain {
		absLine := entryStart + line
		if absLine < loLine {
			continue
		}
		if absLine > hiLine {
			break
		}
		from := 0
		to := messages.DisplayWidthOfPlain(plain)
		if absLine == loLine {
			from = loCol
		}
		if absLine == hiLine {
			to = hiCol
		}
		b.WriteString(strings.TrimRight(messages.SliceColumns(plain, from, to), " "))
		if absLine != hiLine {
			b.WriteByte('\n')
		}
	}
}

// applySelectionOverlay returns viewContent with selection-style
// applied to the visible columns of the active selection range.
// Operates on the FULLY-BORDERED viewContent because the viewport
// will slice and render it. Plain columns from linesPlain map to
// display columns by adding the entry's contentColOffset (= 1 for
// reply rows, since the thick left border occupies display column 0).
//
// Inter-reply separator lines are NOT inside any cache entry's
// [start, end) range, so they are skipped naturally.
func (m *Model) applySelectionOverlay(content string) string {
	loA, hiA := m.selRange.Normalize()
	loLine, loCol, ok1 := m.resolveAnchor(loA)
	hiLine, hiCol, ok2 := m.resolveAnchor(hiA)
	if !ok1 || !ok2 || loLine > hiLine || (loLine == hiLine && loCol >= hiCol) {
		return content
	}
	selStyle := styles.SelectionStyle()
	lines := strings.Split(content, "\n")
	for absLine := loLine; absLine <= hiLine && absLine < len(lines); absLine++ {
		entry, entryStart, _, ok := m.selectionEntryAt(absLine)
		if !ok {
			continue
		}
		line := absLine - entryStart
		if line < 0 || line >= len(entry.linesPlain) {
			continue
		}
		plain := entry.linesPlain[line]
		from := 0
		to := messages.DisplayWidthOfPlain(plain)
		if absLine == loLine {
			from = loCol
		}
		if absLine == hiLine {
			to = hiCol
		}
		if from < 0 {
			from = 0
		}
		if to > messages.DisplayWidthOfPlain(plain) {
			to = messages.DisplayWidthOfPlain(plain)
		}
		if from >= to {
			continue
		}
		styled := lines[absLine]
		dispFrom := from + entry.contentColOffset
		dispTo := to + entry.contentColOffset
		styledWidth := ansi.StringWidth(styled)
		if dispFrom >= styledWidth {
			continue
		}
		if dispTo > styledWidth {
			dispTo = styledWidth
		}
		prefix := ansi.Cut(styled, 0, dispFrom)
		suffix := ansi.Cut(styled, dispTo, styledWidth)
		lines[absLine] = prefix + selStyle.Render(messages.SliceColumns(plain, from, to)) + suffix
	}
	return strings.Join(lines, "\n")
}

// View renders the thread panel content without a border.
// The parent App is responsible for adding the border.
func (m *Model) View(height, width int) string {
	if m.IsEmpty() {
		return lipgloss.NewStyle().
			Width(width).
			Height(height).
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Render("No thread selected")
	}

	// Chrome: header + separator + parent message + separator. Cached
	// because the parent's full markdown pipeline (RenderSlackMarkdown +
	// WordWrap + reactions + attachments) is expensive and identical
	// frame-to-frame on a plain j/k. Mirrors internal/ui/messages's
	// chromeCache.
	//
	// Invariant: any mutation of m.parent.TS, m.parent.Text, m.userNames
	// (which bumps userNamesV), m.channelNames (which bumps channelNamesV),
	// or len(m.replies) MUST be paired with InvalidateCache() or a bump of
	// the relevant version counter — otherwise the chrome will go stale.
	// Today SetThread, AddReply, UpdateMessageInPlace, UpdateParentInPlace,
	// SetUserNames, and SetChannelNames all honor this. If you add a new
	// visual element to the chrome (e.g. parent reactions, parent edited
	// marker, attachments rendered above the separator), either add its
	// state to the predicate below or invalidate on its mutation path.
	// Adding to the predicate is preferred when the state changes
	// infrequently — chrome is rebuilt rarely, so the comparison cost is
	// negligible and invalidation discipline is easier to get wrong.
	chromeReplyCount := len(m.replies)
	// Chrome no longer contains the parent message -- the parent now lives
	// at the top of m.viewContent so it scrolls with the replies (a long
	// parent must not pin and block the reply area). Chrome is only the
	// header line + a single border separator; everything else moved into
	// the viewport, so parent identity no longer participates in the
	// chrome cache key.
	if !m.chromeCacheValid ||
		m.chromeWidth != width ||
		m.chromeReplyCount != chromeReplyCount ||
		m.chromeUserNamesV != m.userNamesV ||
		m.chromeChannelNamesV != m.channelNamesV {
		replyLabel := "replies"
		if chromeReplyCount == 1 {
			replyLabel = "reply"
		}
		header := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.TextPrimary).
			Bold(true).
			Render(fmt.Sprintf("Thread  %d %s", chromeReplyCount, replyLabel))
		separator := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Border).
			Render(strings.Repeat("-", width))
		m.chromeCache = header + "\n" + separator
		m.chromeHeight = lipgloss.Height(m.chromeCache)
		m.chromeCacheValid = true
		m.chromeWidth = width
		m.chromeReplyCount = chromeReplyCount
		m.chromeUserNamesV = m.userNamesV
		m.chromeChannelNamesV = m.channelNamesV
	}
	chrome := m.chromeCache
	chromeHeight := m.chromeHeight

	// chromeHeight already counts every visual row of `chrome`; joining with
	// a single "\n" between chrome and the viewport produces exactly
	// chromeHeight+vp.Height() lines total. No extra row is consumed.
	replyAreaHeight := height - chromeHeight
	if replyAreaHeight < 1 {
		replyAreaHeight = 1
	}
	m.lastViewHeight = replyAreaHeight

	// Render the parent message once -- it now lives at the top of the
	// scrollable viewContent (chrome only carries the header + separator).
	// We discard the parent's image flushes and reaction-hit rects in v1:
	// parent attachments are rare and threading flushes through the cache
	// lifecycle adds complexity; reply flushes and hit rects ARE captured
	// in the per-reply loop below.
	parentIsSelected := m.selected == parentSelected
	parentContent, _, _ := m.renderThreadMessage(m.parent, width, m.userNames, m.channelNames, parentIsSelected)
	m.parentEntry = viewEntry{
		linesPlain:       messages.PlainLines(parentContent),
		height:           lipgloss.Height(parentContent),
		contentColOffset: 1,
	}
	// Selection border for the parent row mirrors the per-reply
	// cache-build treatment (borderSelect / borderInvis below): thick
	// left border + tint when the cursor sits on the parent, invisible
	// border otherwise so the parent's width matches the reply rows.
	if parentIsSelected {
		parentContent = lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.SelectionBorderColor(m.focused)).
			BorderBackground(styles.SelectionTintColor(m.focused)).
			Background(styles.SelectionTintColor(m.focused)).
			Render(parentContent)
	} else {
		parentContent = lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.Background).BorderBackground(styles.Background).
			Render(parentContent)
	}
	parentSeparator := lipgloss.NewStyle().
		Width(width).
		Background(styles.Background).
		Foreground(styles.Border).
		Render(strings.Repeat("─", width))
	parentBlock := parentContent + "\n" + parentSeparator
	parentBlockHeight := lipgloss.Height(parentBlock)

	if len(m.replies) == 0 {
		emptyHeight := replyAreaHeight - parentBlockHeight
		if emptyHeight < 1 {
			emptyHeight = 1
		}
		empty := lipgloss.NewStyle().
			Width(width).
			Height(emptyHeight).
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Render("No replies yet")
		// Push parent + placeholder through the viewport so a parent
		// taller than the pane still scrolls. The scrollbar overlay
		// reflects the parent's own height.
		emptyContent := parentBlock + "\n" + empty
		emptyTotalLines := lipgloss.Height(emptyContent)
		m.vp.SetWidth(width)
		m.vp.SetHeight(replyAreaHeight)
		m.vp.KeyMap = viewport.KeyMap{}
		m.vp.SetContent(emptyContent)
		visibleLines := strings.Split(m.vp.View(), "\n")
		visibleLines = scrollbar.Overlay(
			visibleLines, width,
			emptyTotalLines, m.vp.YOffset(), replyAreaHeight,
			styles.Background, styles.Border, styles.Primary,
		)
		result := chrome + "\n" + strings.Join(visibleLines, "\n")
		return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
	}

	// Rebuild render cache if replies or width changed
	//
	// Per-reply cache rebuild predicate intentionally excludes m.selected,
	// m.focused, m.reactionNavActive/Index, and theme version. The cache
	// holds renderThreadMessage output, which depends on those fields, so
	// any state that can change rendered output for a given (reply, width)
	// MUST call InvalidateCache() on its mutation path. EnterReactionNav /
	// ExitReactionNav / ReactionNavLeft / ReactionNavRight all do this
	// today; MoveUp/MoveDown call ExitReactionNav before mutating selected.
	// If you add a new visual feature gated on mutable model state, either
	// invalidate on the relevant transition or add the state to this
	// predicate. Adding to the predicate forces a full cache rebuild on the
	// transition, which defeats the j/k speedup — prefer invalidation.
	if m.cache == nil || m.cacheWidth != width || m.cacheReplyLen != len(m.replies) {
		var perfReason string
		var perfStart time.Time
		if debuglog.Enabled() {
			switch {
			case m.cache == nil:
				perfReason = "cache-nil"
			case m.cacheWidth != width:
				perfReason = fmt.Sprintf("width-changed %d->%d", m.cacheWidth, width)
			default:
				perfReason = fmt.Sprintf("len-changed %d->%d", m.cacheReplyLen, len(m.replies))
			}
			perfStart = time.Now()
		}
		m.cache = m.cache[:0]
		if m.replyIDToIdx == nil {
			m.replyIDToIdx = make(map[string]int, len(m.replies))
		} else {
			for k := range m.replyIDToIdx {
				delete(m.replyIDToIdx, k)
			}
		}
		// Pre-build border styles ONCE (don't allocate per reply). Mirrors
		// internal/ui/messages/model.go:1051-1056: the thick left border ▌
		// is now applied at cache-build time so View() can pick a slice
		// (linesNormal vs linesSelected) instead of running lipgloss for
		// every visible reply on every j/k.
		borderFill := lipgloss.NewStyle().Background(styles.Background)
		borderInvis := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.Background).BorderBackground(styles.Background)
		borderSelect := lipgloss.NewStyle().BorderStyle(thickLeftBorder).BorderLeft(true).
			BorderForeground(styles.SelectionBorderColor(m.focused)).
			BorderBackground(styles.SelectionTintColor(m.focused)).
			Background(styles.SelectionTintColor(m.focused))
		for i, reply := range m.replies {
			// renderThreadMessage's last arg ("isSelected") drives reaction-
			// nav pill highlighting (lines 1040, 1049): when reaction nav
			// is active on the selected reply, the navigated pill / "+"
			// button gets a distinct style. We MUST forward i==m.selected
			// here (not a constant false) to preserve that UX. EnterReactionNav
			// / ReactionNavLeft / ReactionNavRight all call InvalidateCache(),
			// so the cache rebuilds whenever the highlighted index changes.
			// This matches the messages-pane convention
			// (internal/ui/messages/model.go:1050).
			rendered, attachFlushes, reactHits := m.renderThreadMessage(reply, width, m.userNames, m.channelNames, i == m.selected)
			// Two filled variants — see internal/ui/messages/model.go for the
			// rationale. Without per-variant fills, the trailing whitespace of
			// every wrapped line shows the wrong bg and the tint stops at the
			// last character of content. linesPlain mirrors the UNTINTED
			// (filledNormal) so clipboard text never carries the tint.
			filledNormal := borderFill.Width(width - 1).Render(rendered)
			// For the selected variant, repaint inner explicit-bg ANSI
			// escapes (Username, Timestamp, MessageText, RenderSlackMarkdown's
			// reset-reapplications) with the tint color so the tint reaches
			// every cell of the row, not just the trailing whitespace.
			renderedTinted := messages.RepaintBgToSelectionTint(rendered, m.focused)
			selectedFill := lipgloss.NewStyle().
				Background(styles.SelectionTintColor(m.focused)).
				Width(width - 1).
				Render(renderedTinted)
			normal := borderInvis.Render(filledNormal)
			selected := borderSelect.Render(selectedFill)
			linesN := strings.Split(normal, "\n")
			linesS := strings.Split(selected, "\n")
			m.cache = append(m.cache, viewEntry{
				linesNormal:   linesN,
				linesSelected: linesS,
				// linesPlain mirrors the UNBORDERED, UNTINTED content (filledNormal)
				// so the thick left-border column is NOT present in plain text and
				// never bleeds into clipboard output via SelectionText. The
				// mouse-column to plain-column mapping uses contentColOffset.
				// Same convention as internal/ui/messages/model.go:1057-1061.
				linesPlain:       messages.PlainLines(filledNormal),
				height:           len(linesN),
				replyIdx:         i,
				contentColOffset: 1,
				flushes:          attachFlushes,
				reactionHits:     reactHits,
			})
			m.replyIDToIdx[reply.TS] = i
		}
		m.cacheWidth = width
		m.cacheReplyLen = len(m.replies)
		m.viewCacheValid = false
		if debuglog.Enabled() {
			debuglog.Perf("thread.buildCache N=%d width=%d took=%s reason=%s",
				len(m.replies), width, time.Since(perfStart), perfReason)
		}
	}

	// Check if view-level cache (bordered content) can be reused
	var viewPerfReason string
	var viewPerfStart time.Time
	viewPerfRebuild := !m.viewCacheValid || m.viewSelected != m.selected || m.viewWidth != width || m.viewHeight != replyAreaHeight
	if viewPerfRebuild && debuglog.Enabled() {
		switch {
		case !m.viewCacheValid:
			viewPerfReason = "invalidated"
		case m.viewSelected != m.selected:
			viewPerfReason = fmt.Sprintf("selected-changed %d->%d", m.viewSelected, m.selected)
		case m.viewWidth != width:
			viewPerfReason = fmt.Sprintf("width-changed %d->%d", m.viewWidth, width)
		default:
			viewPerfReason = fmt.Sprintf("height-changed %d->%d", m.viewHeight, replyAreaHeight)
		}
		viewPerfStart = time.Now()
	}
	if viewPerfRebuild {
		// Visible separator drawn between replies. Uses the panel border color
		// over the themed background so it reads as a divider but doesn't
		// fight with the panel's outer border. Falls through full content
		// width.
		separatorStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.Border)
		replySeparator := separatorStyle.Render(strings.Repeat("─", width))

		// "── new ──" landmark inserted just before the first reply with
		// TS > unreadBoundaryTS. Mirrors the channel pane's new-message
		// line (see the unread-landmark logic in messages/model.go). The
		// landmark line is NOT inside any cache entry, matching the
		// inter-reply separator convention so selection overlay /
		// extraction skip it naturally. Only constructed when a boundary
		// is set — the typical case (no unread boundary) skips the style
		// allocation entirely.
		var newLandmark string
		if m.unreadBoundaryTS != "" {
			landmarkStyle := lipgloss.NewStyle().
				Width(width).
				Background(styles.Background).
				Foreground(styles.Error).
				Bold(true).
				Align(lipgloss.Center)
			newLandmark = landmarkStyle.Render("── new ──")
		}
		landmarkInserted := false

		// Day-divider style for "── Today ──" / "── Yesterday ──" /
		// weekday / fully-qualified date rows inserted between replies on
		// different local days. Mirrors the channel pane's date separator
		// (see internal/ui/messages/model.go: buildCache). Like
		// replySeparator and newLandmark, these rows live OUTSIDE any
		// cache entry so selection overlay and extraction skip them
		// naturally. lastDate is seeded from the parent so the divider
		// only fires when a reply lands on a day different from the
		// parent's day.
		dateSeparatorStyle := lipgloss.NewStyle().
			Width(width).
			Background(styles.Background).
			Foreground(styles.TextMuted).
			Bold(true).
			Align(lipgloss.Center)
		lastDate := messages.DateFromTS(m.parent.TS)

		// viewContent begins with the parent message block so the parent
		// scrolls together with the replies. entryOffsets / selectedStartLine
		// / m.totalLines are built in parent-inclusive coordinates from here
		// on -- the snap-to-selection math and reaction-hit translation
		// both index into viewContent and so naturally account for the
		// parent prefix without further adjustment.
		allRows := []string{parentBlock}
		startLine := 0
		endLine := 0
		if parentIsSelected {
			// Parent row occupies lines 0..parentBlockHeight; the
			// snap-to-selection math below scrolls to the top.
			endLine = parentBlockHeight
		}
		currentLine := parentBlockHeight
		// kittyFlushBuf collects per-image kitty APC upload bytes for
		// every cached entry (the thread cache holds only the open
		// thread's replies — typically a handful — so we don't bother
		// with viewport-visibility scoping). Written directly to
		// imgpkg.KittyOutput AFTER the loop, before viewContent is
		// assembled. Mirrors internal/ui/messages/model.go's
		// kittyFlushBuf handling; APC sequences embedded in line
		// content are known to get mangled by the bubbletea/lipgloss
		// renderer, so we bypass the frame buffer.
		var kittyFlushBuf bytes.Buffer

		// entryOffsets / totalLines mirror the BORDERED viewContent. Each
		// reply takes lipgloss.Height(borderedReply) lines (== e.height,
		// since the thick left border is purely horizontal padding), plus
		// 1 line per inter-reply separator.
		m.entryOffsets = m.entryOffsets[:0]

		for i, e := range m.cache {
			// Insert a centered day-divider row when this reply's local
			// day differs from the previous one (seeded with the parent's
			// day). Sits ABOVE both the unread landmark and the reply
			// itself so users see e.g. "── Yesterday ──" before any
			// replies from yesterday — matching the channel pane's
			// behavior. Falls through silently when the TS can't be
			// parsed.
			if i < len(m.replies) {
				replyDate := messages.DateFromTS(m.replies[i].TS)
				if replyDate != "" && replyDate != lastDate {
					divider := dateSeparatorStyle.Render("── " + messages.FormatDateSeparator(replyDate) + " ──")
					allRows = append(allRows, divider)
					currentLine++
					lastDate = replyDate
				}
			}

			// Insert the new-reply landmark before the first reply whose
			// TS exceeds the unread boundary. We check this BEFORE
			// recording the entry offset so the landmark sits above
			// reply i.
			if !landmarkInserted && newLandmark != "" && i < len(m.replies) && m.replies[i].TS > m.unreadBoundaryTS {
				allRows = append(allRows, newLandmark)
				currentLine++
				landmarkInserted = true
			}

			// linesNormal/linesSelected are pre-bordered at cache-build
			// time (see buildCache above). View() just picks the slice for
			// the cursor row vs everything else; no per-frame lipgloss work.
			var lines []string
			if i == m.selected {
				startLine = currentLine
				lines = e.linesSelected
			} else {
				lines = e.linesNormal
			}
			content := strings.Join(lines, "\n")
			h := e.height
			m.entryOffsets = append(m.entryOffsets, currentLine)
			if i == m.selected {
				endLine = currentLine + h
			}
			allRows = append(allRows, content)
			currentLine += h

			// Collect kitty per-image upload escapes for this entry.
			// We invoke flushes for every cached entry rather than only
			// viewport-visible ones; the thread cache is small and
			// scoping adds complexity. A future optimization can clip
			// to visible entries.
			for _, fl := range e.flushes {
				if fl != nil {
					_ = fl(&kittyFlushBuf)
				}
			}

			// Separator between replies (not after the last). Separator
			// lines are NOT inside any cache entry — selection overlay /
			// extraction skip them naturally because no entry covers
			// them.
			if i < len(m.cache)-1 {
				allRows = append(allRows, replySeparator)
				currentLine++
			}
		}

		// Write kitty upload escapes directly to the terminal output,
		// bypassing bubbletea's frame buffer. Same rationale as
		// internal/ui/messages/model.go's kittyFlushBuf write.
		if kittyFlushBuf.Len() > 0 {
			_, _ = imgpkg.KittyOutput.Write(kittyFlushBuf.Bytes())
		}

		m.viewContent = strings.Join(allRows, "\n")
		m.viewSelected = m.selected
		m.viewWidth = width
		m.viewHeight = replyAreaHeight
		m.selectedStartLine = startLine
		m.selectedEndLine = endLine
		m.totalLines = currentLine
		m.viewCacheValid = true
		if debuglog.Enabled() {
			debuglog.Perf("thread.viewCache rebuild N=%d width=%d took=%s reason=%s",
				len(m.replies), width, time.Since(viewPerfStart), viewPerfReason)
		}
	}

	// Configure viewport
	m.vp.SetWidth(width)
	m.vp.SetHeight(replyAreaHeight)
	m.vp.KeyMap = viewport.KeyMap{}
	m.vp.SetContent(m.viewContent)

	// Scroll to keep selected item visible -- but only when the selection
	// has actually changed since the last snap. This lets the mouse wheel
	// (or programmatic ScrollUp/Down) move the viewport away from the
	// selected reply without the next render yanking it back. Mirrors the
	// same guard used in messages.Model.View().
	if !m.hasSnapped || m.snappedSelection != m.selected {
		if m.selectedEndLine > m.vp.YOffset()+m.vp.Height() {
			m.vp.SetYOffset(m.selectedEndLine - m.vp.Height())
		}
		if m.selectedStartLine < m.vp.YOffset() {
			m.vp.SetYOffset(m.selectedStartLine)
		}
		m.snappedSelection = m.selected
		m.hasSnapped = true
	}

	// Populate the per-frame reaction-hit slice in pane-local
	// coordinates so the app-level mouse handler can route clicks to
	// a toggle-reaction command. Done after the scroll-snap above so
	// YOffset is settled; cleared at the start of every frame so an
	// invisible entry's hits don't survive the next render. Capacity
	// is preserved across frames (typical case: a handful of pills).
	m.lastReactionHits = m.lastReactionHits[:0]
	yOff := m.vp.YOffset()
	for i, e := range m.cache {
		if len(e.reactionHits) == 0 {
			continue
		}
		entryStart := m.entryOffsets[i]
		for _, h := range e.reactionHits {
			absStart := entryStart + h.rowStartInEntry
			absEnd := entryStart + h.rowEndInEntry
			// Clip to the viewport in viewContent coordinates.
			if absEnd <= yOff || absStart >= yOff+replyAreaHeight {
				continue
			}
			clipStart := absStart - yOff
			if clipStart < 0 {
				clipStart = 0
			}
			clipEnd := absEnd - yOff
			if clipEnd > replyAreaHeight {
				clipEnd = replyAreaHeight
			}
			// Pane-local rows include the chromeHeight prefix that
			// View()'s final string prepends, mirroring the convention
			// used by ClickAt (which subtracts chromeHeight from
			// incoming pane-local y).
			m.lastReactionHits = append(m.lastReactionHits, reactionHitRect{
				rowStart: chromeHeight + clipStart,
				rowEnd:   chromeHeight + clipEnd,
				colStart: h.colStart,
				colEnd:   h.colEnd,
				replyIdx: e.replyIdx,
				emoji:    h.emoji,
			})
		}
	}

	// Overlay the active selection on top of viewContent. Done after
	// scroll-snapping so YOffset is settled, then re-apply the overlayed
	// content to the viewport for the final View() render.
	if m.hasSelection {
		m.vp.SetContent(m.applySelectionOverlay(m.viewContent))
	}

	// Scrollbar overlay on the right edge of the scrollable area. Chrome
	// (header + separator) is left alone since it does not scroll. Same
	// pattern as messages.Model.View().
	visibleLines := strings.Split(m.vp.View(), "\n")
	visibleLines = scrollbar.Overlay(
		visibleLines, width,
		m.totalLines, m.vp.YOffset(), replyAreaHeight,
		styles.Background, styles.Border, styles.Primary,
	)
	result := chrome + "\n" + strings.Join(visibleLines, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).MaxHeight(height).Background(styles.Background).Render(result)
}

// HitTestReaction returns the (reply index, reaction emoji name) at
// (row, col) within the thread pane, or ok=false when no reaction
// pill covers that cell. Coordinate frame: row is pane-local and
// includes the chromeHeight rows of header/parent message at the top
// (i.e., the same frame as the app-level mouse handler's panel-local
// y from panelAt). Hits are populated from the most recent View().
func (m *Model) HitTestReaction(row, col int) (replyIdx int, emoji string, ok bool) {
	for _, h := range m.lastReactionHits {
		if row >= h.rowStart && row < h.rowEnd && col >= h.colStart && col < h.colEnd {
			return h.replyIdx, h.emoji, true
		}
	}
	return 0, "", false
}

// renderThreadMessage renders a single message for the thread panel.
// Returns the content string, any per-frame kitty flush callbacks for
// inline image attachments, and the per-pill hit rects for the
// rendered reactions (in coordinates relative to the rendered content
// AFTER buildCache wraps it with the thick left border in column 0).
// The flushes are consumed by View() when the entry is visible
// (mirroring messages.Model). v1: per-block Hit and SixelRows from
// imgrender are discarded — click-to-preview from a thread reply and
// inline sixel emission are out of scope.
// blockkitContext wires the thread pane's host dependencies into a
// blockkit.Context. Mirrors messages.Model.blockkitContext so Block
// Kit blocks and legacy attachments render identically in the thread
// panel and the main message pane.
func (m *Model) blockkitContext(msg messages.MessageItem, userNames, channelNames map[string]string) blockkit.Context {
	var imgCtx imgrender.ImageContext
	if m.imgRenderer != nil {
		imgCtx = m.imgRenderer.Context()
	}
	send := imgCtx.SendMsg
	return blockkit.Context{
		Protocol:    imgCtx.Protocol,
		Fetcher:     imgCtx.Fetcher,
		KittyRender: imgCtx.KittyRender,
		CellPixels:  imgCtx.CellPixels,
		MaxRows:     imgCtx.MaxRows,
		MaxCols:     imgCtx.MaxCols,
		UserNames:   userNames,
		MessageTS:   msg.TS,
		Channel:     m.channelID,
		RenderText: func(s string, un map[string]string) string {
			return messages.RenderSlackMarkdownWith(s, messages.RenderSlackMarkdownOpts{
				UserNames:    un,
				ChannelNames: channelNames,
				UserGroups:   m.userGroups,
				PlaceCtx:     m.emojiCtx.PlaceCtx,
				EmojiCells:   m.emojiCtx.Cells,
				Customs:      m.emojiCtx.Customs,
				EmojiFlushes: nil,
			})
		},
		WrapText: messages.WordWrap,
		SendMsg: func(v any) {
			if send != nil {
				send(v)
			}
		},
	}
}

func (m *Model) renderThreadMessage(msg messages.MessageItem, width int, userNames map[string]string, channelNames map[string]string, isSelected bool) (string, []func(io.Writer) error, []reactionEntryHit) {
	line := styles.Username(msg.UserID, m.coloredUsernames).Render(msg.UserName) + lipgloss.NewStyle().Background(styles.Background).Render("  ") + styles.Timestamp.Render(msg.Timestamp)

	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}

	// flushes aggregates per-frame side effects (kitty image upload
	// escapes) from body-text emoji, reaction-pill emoji, and inline
	// image attachments. Walked by the View() loop for visible entries.
	// Mirrors the messages-pane named-return `flushes` slice.
	var flushes []func(io.Writer) error
	bodyOpts := messages.RenderSlackMarkdownOpts{
		UserNames:    userNames,
		ChannelNames: channelNames,
		UserGroups:   m.userGroups,
		PlaceCtx:     m.emojiCtx.PlaceCtx,
		EmojiCells:   m.emojiCtx.Cells,
		Customs:      m.emojiCtx.Customs,
		EmojiFlushes: &flushes,
	}
	text := styles.MessageText.Render(messages.WordWrap(messages.RenderSlackMarkdownWith(messages.MessageTextSource(msg), bodyOpts), contentWidth))

	// Block Kit blocks + legacy attachments render between the body
	// text and file attachments, mirroring the main message pane's
	// renderMessagePlain. Image flushes are aggregated into the shared
	// `flushes` slice; per-image Hit/SixelRows are discarded (v1 thread
	// rendering, consistent with the file-attachment path below).
	bkCtx := m.blockkitContext(msg, userNames, channelNames)
	var bkLines []string
	bkInteractive := false
	if len(msg.Blocks) > 0 {
		res := blockkit.Render(msg.Blocks, bkCtx, contentWidth)
		bkLines = append(bkLines, res.Lines...)
		flushes = append(flushes, res.Flushes...)
		bkInteractive = bkInteractive || res.Interactive
	}
	if len(msg.LegacyAttachments) > 0 {
		res := blockkit.RenderLegacy(msg.LegacyAttachments, bkCtx, contentWidth)
		bkLines = append(bkLines, res.Lines...)
		flushes = append(flushes, res.Flushes...)
		bkInteractive = bkInteractive || res.Interactive
	}
	if bkInteractive {
		bkLines = append(bkLines, styles.Timestamp.Render("↗ open in Slack to interact"))
	}
	bkBlock := ""
	bkLineCount := len(bkLines)
	if bkLineCount > 0 {
		bkBlock = "\n" + strings.Join(bkLines, "\n")
	}

	var reactionLine string
	// pillSpecs captures one entry per real (non-"+") reaction pill in
	// rendering order, with line index and column extents relative to
	// the reaction-line block. Translated to absolute reactionEntryHit
	// rects below (once we know the row offset of the reaction block).
	type pillSpec struct {
		lineIdx  int
		colStart int
		colEnd   int
		emoji    string
	}
	var pillSpecs []pillSpec
	reactionLineCount := 0
	if len(msg.Reactions) > 0 {
		var pills []string
		var pillEmojis []string
		// Image-aware emoji-as-image path is active only when the
		// process-global ImageMode is on AND a fetcher has been
		// installed via SetEmojiContext. Otherwise the legacy
		// glyph/shortcode-text branch renders. Mirrors the
		// messages-pane reaction-pill loop.
		imageOK := emojiutil.ImageModeActive() && m.emojiCtx.PlaceCtx.Fetcher != nil
		pillCells := m.emojiCtx.Cells
		if pillCells <= 0 {
			pillCells = 2
		}
		for i, r := range msg.Reactions {
			// Image path: pass r.Emoji directly (including any skin-tone
			// suffix) — URLForShortcode composes the per-tone CDN URL
			// natively. Stripping skin tone was a workaround for
			// glyph-rendering width inconsistencies that no longer apply
			// at the kitty-image cell footprint. Mirror of the
			// messages-pane reaction-pill loop.
			//
			// Legacy path: still strip the suffix because the glyph
			// renderer suffers the original width-inconsistency problem
			// (multi-codepoint ZWJ / skin-tone sequences render at
			// terminal-dependent widths and break border alignment).
			var emojiStr string
			var placedFlush func(io.Writer) error
			if imageOK {
				if url, ok := emojiutil.URLForShortcode(r.Emoji, m.emojiCtx.Customs); ok {
					if placement, flush, ok := emojiutil.Place(m.emojiCtx.PlaceCtx, url, pillCells); ok {
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
			if isSelected && m.reactionNavActive && i == m.reactionNavIndex {
				style = styles.ReactionPillSelected
			} else if r.HasReacted {
				style = styles.ReactionPillOwn
			} else {
				style = styles.ReactionPillOther
			}
			pillText := messages.ReactionPillText(emojiStr, r.Count, style, placedFlush != nil)
			pills = append(pills, style.Render(pillText))
			pillEmojis = append(pillEmojis, r.Emoji)
			// Image emoji in reaction pills land in the same per-frame
			// flush list as body text and inline image attachments.
			if placedFlush != nil {
				flushes = append(flushes, placedFlush)
			}
		}
		if isSelected && m.reactionNavActive {
			plusStyle := styles.ReactionPillPlus
			if m.reactionNavIndex >= len(msg.Reactions) {
				plusStyle = styles.ReactionPillSelected
			}
			pills = append(pills, plusStyle.Render("+"))
			pillEmojis = append(pillEmojis, "")
		}
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

	// Attachments: per-attachment inline render via imgrender.Renderer.
	// v1: flushes are aggregated into the unified `flushes` slice for
	// the cache layer to invoke during View(); the per-block Hit and
	// SixelRows are discarded (click-to-preview from threads and inline
	// sixel emission are out of scope for v1; sixel images render their
	// res.Lines placeholder/sentinel only).
	var attachmentLines string
	attachmentLineCount := 0
	if len(msg.Attachments) > 0 {
		if m.imgRenderer == nil {
			m.imgRenderer = imgrender.NewRenderer()
		}
		blocks := make([]string, 0, len(msg.Attachments))
		for attIdx, att := range msg.Attachments {
			imgThumbs := make([]imgrender.ThumbSpec, len(att.Thumbs))
			for i, t := range att.Thumbs {
				imgThumbs[i] = imgrender.ThumbSpec{URL: t.URL, W: t.W, H: t.H}
			}
			res := m.imgRenderer.RenderBlock(imgrender.Block{
				Kind:   att.Kind,
				FileID: att.FileID,
				Name:   att.Name,
				URL:    att.URL,
				Thumbs: imgThumbs,
			}, m.channelID, msg.TS, contentWidth, 0 /* baseRow */, attIdx, 0 /* contentColBase */)
			blocks = append(blocks, strings.Join(res.Lines, "\n"))
			flushes = append(flushes, res.Flushes...)
			attachmentLineCount += len(res.Lines)
		}
		attachmentLines = "\n" + strings.Join(blocks, "\n")
	}

	// Translate per-pill specs into reactionEntryHit rects. Row layout
	// of the reply content (pre-border, pre-tint):
	//   row 0: username + timestamp line
	//   rows [1 .. 1+textRows): wrapped body text
	//   rows [.. +bkLineCount): block kit + legacy attachment content
	//   rows [.. +attachmentLineCount): file attachments
	//   rows [reactionRowBase ..): reaction lines
	// buildCache wraps the content with a thick left border in col 0
	// (so contentColBase = 1); it adds no rows.
	var reactionHits []reactionEntryHit
	if len(pillSpecs) > 0 && reactionLineCount > 0 {
		const contentColBase = 1 // thick left border occupies col 0 of linesNormal
		reactionRowBase := 1 + lipgloss.Height(text) + bkLineCount + attachmentLineCount
		for _, ps := range pillSpecs {
			row := reactionRowBase + ps.lineIdx
			reactionHits = append(reactionHits, reactionEntryHit{
				rowStartInEntry: row,
				rowEndInEntry:   row + 1,
				colStart:        contentColBase + ps.colStart,
				colEnd:          contentColBase + ps.colEnd,
				emoji:           ps.emoji,
			})
		}
	}

	return line + "\n" + text + bkBlock + attachmentLines + reactionLine, flushes, reactionHits
}
