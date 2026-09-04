// internal/ui/app.go
package ui

import (
	"context"
	"fmt"
	"image"
	"log"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/export"
	"github.com/gammons/slk/internal/filedl"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/channelpicker"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/confirmprompt"
	"github.com/gammons/slk/internal/ui/emojipicker"
	"github.com/gammons/slk/internal/ui/help"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/linkpicker"
	"github.com/gammons/slk/internal/ui/mentionpicker"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/newmessagepicker"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/reactionsview"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/thread"
	"github.com/gammons/slk/internal/ui/threadsview"
	"github.com/gammons/slk/internal/ui/wintree"
	"github.com/gammons/slk/internal/ui/workspace"
	"github.com/gammons/slk/internal/ui/workspacefinder"
	"github.com/gammons/slk/internal/usergroups"
	"golang.design/x/clipboard"
)

type Panel int

const (
	PanelWorkspace Panel = iota
	PanelSidebar
	PanelMessages
	PanelThread
)

// View identifies which "page" the message pane is displaying. The default
// is ViewChannels (a channel's message history); ViewThreads swaps the
// pane's contents for the involved-threads list.
type View int

const (
	ViewChannels View = iota
	ViewThreads
)

const (
	// cacheFreshThreshold: cache rendered as-is, no network fetch, no
	// syncing indicator. Channel was visited or WS-updated within this
	// window so the SQLite snapshot is provably recent.
	cacheFreshThreshold = 30 * time.Second
)

// openThreadDebounceDelay is how long openSelectedThreadCmd waits after a
// j/k key event before firing the conversations.replies HTTP call. Held-key
// bursts coalesce into a single fetch against whichever row the cursor
// finally lands on.
const openThreadDebounceDelay = 200 * time.Millisecond

// channelSearchDebounceDelay is how long the channel finder waits for
// typing to stop before asking the server. ~300 ms is the figure the
// Phase 2b design fixed on from the captures, where a four-second
// typing session produced two channels/search requests — about one per
// pause, never one per keystroke.
const channelSearchDebounceDelay = 300 * time.Millisecond

// defaultMarkFlushDebounce is the App.markFlushDebounce NewApp starts
// with, and the fallback scheduleMarkFlush uses for a zero field.
//
// conversations.mark is a Tier 3 method — Slack budgets those at
// roughly 50 requests a minute. This interval is the ceiling on how
// often the flush spends one: 60/5 = 12 a minute, with room left for
// the entry marks and mark-unread presses that call it on other paths. One second, the value this started at, put a
// channel receiving about a message a second at ~60 a minute — over
// the tier, and an over-tier mark is dropped rather than retried (see
// flushPendingMarks), which is the cross-client divergence the
// auto-marking exists to prevent.
//
// Five seconds is not felt by the user: nothing on screen changes when
// the flush fires. It moves Slack's server-side cursor, and the local
// divider deliberately stays put (again, see flushPendingMarks).
const defaultMarkFlushDebounce = 5 * time.Second

// defaultThreadsDirtyDebounce is the App.threadsDirtyDebounce NewApp
// starts with, and the fallback for a zero field in scheduleThreadsDirty
// and in reduceThreads' ThreadsListDirtyMsg arm. It only delays a query
// against local SQLite, so it is two orders of magnitude below the mark
// flush above.
const defaultThreadsDirtyDebounce = 150 * time.Millisecond

type App struct {
	// Sub-models
	workspaceRail    workspace.Model
	sidebar          sidebar.Model
	messagepane      *messages.Model
	compose          compose.Model
	statusbar        statusbar.Model
	channelFinder    channelfinder.Model
	searchResults    searchresults.Model
	newMessagePicker newmessagepicker.Model
	workspaceFinder  workspacefinder.Model
	themeSwitcher    themeswitcher.Model
	presenceMenu     presencemenu.Model
	help             help.Model
	threadPanel      *thread.Model
	threadCompose    compose.Model
	threadsView      threadsview.Model

	// State
	mode           Mode
	focusedPanel   Panel
	sidebarVisible bool
	threadVisible  bool
	view           View
	width          int
	height         int
	keys           KeyMap

	// cmdline accumulates the text typed at the vi-style ':' prompt
	// while in ModeCommand. Owned by mode_command.go; always "" in
	// every other mode.
	cmdline string

	// wins is the vim-style window tree subdividing the messages
	// region; focusedWin is the active window within it. Phase 2:
	// the focused window renders the single live messages model,
	// unfocused windows render placeholders.
	wins       *wintree.Tree
	focusedWin wintree.LeafID

	// winModels holds one live messages.Model per window (Phase 3).
	// INVARIANT: messagepane == winModels[focusedWin] always; keys
	// exactly match wins.Leaves(). Maintained by newWindowModel /
	// focusWindow / closeWindow / onlyWindow / resetWindowTree.
	winModels map[wintree.LeafID]*messages.Model

	// pendingWinCmd is true between a ctrl+w press and its chord key
	// (vim window-command prefix). Esc or an unmapped key cancels;
	// any mode change disarms (see SetMode).
	pendingWinCmd bool

	// layout owns the per-frame layout geometry (horizontal bands for
	// mouse hit-testing + per-pane content heights for pageSize). See
	// internal/ui/panellayout.go.
	layout *panelLayout

	// renderCache aggregates the six per-panel render caches. See
	// internal/ui/panelcache.go.
	renderCache *panelRenderCache

	// Current context
	activeChannelID string
	activeTeamID    string // workspace whose data is currently loaded into the side panels

	// windowTitle is the cached terminal-window-title string, recomputed
	// by notifyReadStateChanged on every read-state mutation and read by
	// View() into tea.View.WindowTitle. Bubbletea's renderer emits OSC 2
	// only when the value changes between renders -- no per-frame work.
	// See docs/superpowers/specs/2026-05-21-tab-title-unread-indicator-design.md.
	windowTitle string
	// Callbacks
	// channels is the App's ChannelService collaborator (Slack
	// channels API + local cache + session bookkeeping). See
	// internal/ui/services.go. Defaulted to a no-op adapter in
	// NewApp so call sites can dispatch without nil-checks.
	channels ChannelService
	// messages is the App's MessageService collaborator (send / edit /
	// delete / mark-unread / permalink). See internal/ui/services.go.
	// Defaulted to a no-op adapter in NewApp so call sites can dispatch
	// without nil-checks.
	messageSvc MessageService

	uploader UploadFunc

	// statusReport mirrors slk's unread state onto an external surface,
	// invoked by notifyReadStateChanged on every read-state change. Nil unless
	// a status_command is configured (config: notifications.status_command).
	statusReport StatusReportFunc

	// clipboardAvailable is set from the native clipboard reader's startup
	// result. It gates Ctrl+V smart-paste only; OSC 52 writes do not depend on
	// native clipboard initialization.
	clipboardAvailable bool

	// clipboardRead is the function used by smartPaste to read OS clipboard
	// contents. Tests inject fakes via SetClipboardReader.
	clipboardRead clipboardReader

	// clipboardWrite creates OSC 52 write commands for permalink and drag-copy
	// actions. Tests inject fakes via SetClipboardWriter.
	clipboardWrite clipboardWriter

	// threads is the App's ThreadService collaborator (fetch / mark /
	// reply / list-fetch + parent-channel last-read lookup for the
	// unread boundary). See internal/ui/services.go. Defaulted to a
	// no-op adapter in NewApp so call sites can dispatch without
	// nil-checks.
	threads ThreadService

	threadsDirtyDebounce time.Duration

	// threadsListFetchScheduled is set while a threads-list refresh is
	// waiting out threadsDirtyDebounce, and cleared when the
	// threadsListFetchMsg that ends the wait arrives. Further
	// ThreadsListDirtyMsg deliveries are dropped while it is set: the
	// refresh they would ask for is already scheduled, and it reads
	// the cache when it runs rather than when it was scheduled.
	//
	// That holds except across a workspace switch, where a dirty for
	// the newly-active team can be dropped into a window opened for
	// the old one, whose fetch is then discarded by the team check in
	// the threadsListFetchMsg arm — so neither refreshes. Benign:
	// reduceWorkspaceSwitched dispatches its own ListFetch for the new
	// team, and the window reopens for the next dirty either way.
	//
	// The coalescing lives here, on the receiving side, because the
	// message's senders — the thread_marked and
	// thread_subscription_changed WS handlers, the subscription
	// sync's completion callback, and scheduleThreadsDirty's two
	// callers — do not coordinate.
	threadsListFetchScheduled bool

	// terminalFocused tracks whether the terminal running slk currently
	// has focus. Maintained by reduceFocus from tea.FocusMsg /
	// tea.BlurMsg, which the terminal only emits once View sets
	// ReportFocus.
	//
	// Terminals report focus *transitions* only — enabling focus
	// reporting elicits no current-state report — so NewApp starts this
	// true. A terminal with no focus-event support sends nothing at
	// all, and there the flag simply stays true for the whole session.
	// tmux forwards focus events only when `set -g focus-events on`, so
	// tmux users without that setting are in the same position; inside
	// tmux, autoMarkArmed is what keeps that from silently advancing
	// the read cursor. See wiki/Terminal-Compatibility.md, "Focus
	// reporting and read state".
	terminalFocused bool

	// inTmux records whether slk is running inside tmux, captured once
	// in NewApp from $TMUX. It exists because tmux swallows focus
	// events unless `set -g focus-events on`, which defaults off — so
	// inside tmux the assume-focused default is not safe on its own.
	// See autoMarkArmed.
	inTmux bool

	// focusEverReported goes true on the first focus event of any kind.
	// A blur is as much proof that reporting works as a focus is.
	focusEverReported bool

	// pendingChannelMark / pendingThreadMark stage a read-cursor
	// advance for a message that arrived in what is currently on
	// screen — the selected channel, or the open thread panel —
	// whether or not the user is looking at it. Staging is not
	// focus-gated; only the flush is, so a slot staged while blurred
	// waits for the next FocusMsg. A slot staged while FOCUSED waits
	// too if auto-marking is not armed — inside a tmux session that
	// has never reported a focus event that is the whole session, and
	// the slot is only ever issued if one arrives. See autoMarkArmed.
	//
	// Single-slot and newest-wins: a burst coalesces into one request,
	// and a slot can never issue a ts older than one it already holds
	// FOR THE SAME TARGET. Switching target (another channel, another
	// thread) replaces the slot outright, so a staged advance the
	// flush has not yet issued is dropped. Accepted: that advance is
	// reconciled by the target's next entry mark or by reconnect sync.
	pendingChannelMark pendingChannelMarkState
	pendingThreadMark  pendingThreadMarkState

	// selfMarks and selfThreadMarks record the channel and thread marks
	// slk issued itself so their Slack-broadcast echoes can be told
	// apart from a mark made in another client. Two sets rather than
	// one so a burst on either side cannot evict the other's records.
	// See selfMarkDedup.
	selfMarks       selfMarkDedup
	selfThreadMarks selfMarkDedup

	// markFlushScheduled caps the conversations.mark RATE at one per
	// markFlushDebounce per target, not merely the number of live
	// timers: without it a stream arriving faster than the debounce
	// would interleave ticks with arrivals, and each tick would find
	// the slot already refilled and issue a request. See
	// scheduleMarkFlush.
	markFlushScheduled bool

	// markFlushDebounce is that interval. What sets its size is
	// conversations.mark's rate tier, not merely that it is a network
	// write; see defaultMarkFlushDebounce before changing it. Tests
	// shorten it.
	markFlushDebounce time.Duration

	// fetchingOlder tracks in-flight older-history backfills per
	// channel ID (Phase 3: a global bool would let one window's
	// backfill block another channel's, and a fetch completing for
	// a no-longer-viewed channel must still clear its own flag).
	fetchingOlder map[string]bool

	// Cached user-id -> display-name map (mirror of what SetUserNames
	// last received). Used by openSelectedThreadCmd to populate the
	// thread panel parent's UserName without round-tripping through any
	// sub-component's API.
	userNames map[string]string

	// Per-window model config retention (Phase 3): the most recent
	// values pushed through the App-level Set* forwarders, kept so
	// newWindowModel can configure late-created window models
	// identically to the root model. See internal/ui/winmodels.go.
	avatarFn     messages.AvatarFunc
	imageCtx     imgrender.ImageContext
	emojiCtx     messages.EmojiContext
	emojiCustoms map[string]string
	channelNames map[string]string
	userGroups   map[string]string

	// externalUsers tracks which user IDs are Slack Connect / shared-channel
	// guests. Populated by main.go via SetExternalUsers as users are
	// resolved. Read by SetUserNames when building the mention-picker User
	// slice so IsExternal is set on each entry.
	externalUsers map[string]bool
	// Last (channelID, threadTS) auto-opened from the threads view.
	// openSelectedThreadCmd compares against these to dedup repeat calls
	// (j/k keystrokes and ThreadsListLoadedMsg refreshes both fire
	// openSelectedThreadCmd; without dedup we'd hammer the Slack API and
	// clobber the right thread panel mid-read). Cleared whenever the
	// user leaves the threads view (ChannelSelectedMsg, CloseThread,
	// workspace switch).
	lastOpenedChannelID string
	lastOpenedThreadTS  string

	// pendingThreadFetchGen is bumped by every debounced openSelectedThreadCmd
	// call (j/k path). The threadFetchDebounceMsg handler only runs the network
	// fetch when its `gen` matches; older ticks are dropped so a held j produces
	// exactly one fetch (for the row the user finally lands on). Non-debounced
	// callers (activation, list reload, G jump) do NOT bump this — bumping there
	// would needlessly invalidate any in-flight debounced fetch about to land.
	pendingThreadFetchGen uint64

	// pendingChannelSearchGen is bumped by every channel-finder
	// keystroke that changes the query, including the one that empties
	// it. channelSearchDebounceMsg runs the search only when its gen
	// still matches, so a burst of keystrokes issues one request for
	// the query the user stopped on — and deleting back to empty
	// cancels the request that was about to go out.
	pendingChannelSearchGen uint64

	// channelSearchDebounce is the finder's debounce window. A field
	// rather than the constant so tests can collapse it.
	channelSearchDebounce time.Duration

	// emojiInvalidatePending guards against scheduling multiple tick
	// callbacks when many EmojiImageReadyMsg arrive in rapid succession
	// (e.g., a fresh channel with 50+ cold-cache emoji whose fetches all
	// complete in a burst). Coalesces every arrival within the debounce
	// window into a single cache invalidation when the emojiInvalidateMsg
	// tick fires. See reducer_io.go's EmojiImageReadyMsg arm.
	emojiInvalidatePending bool

	// linkPicker is the open-link choice modal (issue #62).
	linkPicker *linkpicker.Model

	// fileDownloader downloads file attachments for the `d`
	// keybinding. Nil in tests; downloadFileCmd toasts when unset.
	fileDownloader *filedl.Downloader

	// pickerKind records what the linkpicker modal is choosing:
	// "links" (Enter dispatches OpenLinkMsg) or "files" (Enter
	// dispatches DownloadFileMsg from pickerFiles).
	pickerKind  string
	pickerFiles []messages.Attachment

	// Reaction picker
	reactionPicker *reactionpicker.Model
	reactionsView  *reactionsview.Model
	confirmPrompt  *confirmprompt.Model
	// reactions is the App's ReactionService collaborator (add/remove
	// reactions on Slack + load/record frecent emoji history). See
	// internal/ui/services.go. Defaulted to a no-op adapter in NewApp
	// so call sites can dispatch without nil-checks.
	reactions     ReactionService
	currentUserID string

	// editing tracks in-progress message edit state. See
	// internal/ui/edit.go.
	editing *editController

	// newMessageInFlightID is the monotonic counter for in-flight
	// OpenConversation requests dispatched from the new-message
	// picker. 0 means no submit is in flight. The reducer drops late
	// results whose RequestID doesn't match.
	newMessageInFlightID uint64
	// newMessageCancelled is set to true when the user Escs the
	// modal while a submit is in flight. A subsequent
	// NewMessageOpenedMsg with the matching RequestID is dropped
	// rather than switching channels behind the user's back.
	newMessageCancelled bool

	// Workspace switching
	workspaceSwitcher SwitchWorkspaceFunc
	workspaceItems    []workspace.WorkspaceItem // cached for lookup
	// lastChannelByTeam remembers the active channel ID per workspace so
	// that switching back to a workspace returns to the same channel the
	// user was last viewing there. Saved at the start of every workspace
	// switch (the workspace being left), consulted when applying the
	// switch (the workspace being entered). Falls back to the first
	// channel in the sidebar when there is no saved entry or the saved
	// channel is no longer in the list.
	lastChannelByTeam map[string]string

	// workspaceDomains maps teamID -> slack.com subdomain, recorded
	// from WorkspaceReadyMsg / WorkspaceSwitchedMsg. Read by the link
	// router to match permalink hosts against the active workspace.
	workspaceDomains map[string]string

	// pendingLinkNav tracks an in-flight permalink navigation: the
	// channel was (or is being) opened and the message-select /
	// thread-open completes when that channel's messages land. See
	// reducer_links.go.
	pendingLinkNav *pendingLinkNav

	// search is the active in-channel search (nil = none).
	// searchInput is the prompt buffer while in ModeSearch.
	// searchGen is a monotonic generation counter: bumped on every
	// dispatch and clear, echoed through ChannelSearchResultsMsg.Gen
	// so stale in-flight results are dropped (see reducer_search.go).
	search      *activeSearch
	searchInput string
	searchGen   uint64
	searchSvc   SearchService

	// browserOpener launches a URL in the OS browser. Defaults to
	// openURLCmd; tests inject fakes.
	browserOpener func(url string) tea.Cmd

	// navHistory owns the per-workspace ctrl+h / ctrl+k browser-style
	// jump list. See internal/ui/navhistory.go. Lazy-initialized on
	// first push for each team. Cleared only when slk exits — the
	// stacks are session-only by design.
	navHistory *navHistoryStore

	// Theme switching
	themeSaveFn    func(name string, scope themeswitcher.ThemeScope)
	themeOverrides config.Theme

	// Sidebar width persistence
	widthSaveFn func(width int)

	// presence owns per-workspace presence/DND cache, the DND-tick
	// guard, and the custom-snooze numeric input buffer. See
	// internal/ui/presence.go.
	presence *presenceController
	// setStatusFn is the callback invoked when the user picks a presence-
	// menu action; it runs the Slack API call for the active workspace.
	// Wired by cmd/slk/main.go via SetStatusSetter.
	setStatusFn func(action presencemenu.Action, snoozeMinutes int)

	// typing owns both inbound typing-indicator state (other users
	// typing in channels) and outbound typing-send throttle. See
	// internal/ui/typing.go.
	typing    *typingTracker
	typingOut *typingBroadcaster

	// mouseWheelLines is the number of lines the viewport scrolls per
	// mouse-wheel notch. Plumbed from [appearance].mouse_wheel_lines.
	// Falls back to 3 when unset (matches the pre-config behavior).
	mouseWheelLines int

	// selfSend tracks slk-originated message sends so WS echoes can
	// be suppressed (preventing the visible flicker between Slack's
	// normalised echo text and the optimistic instant-display text).
	// See internal/ui/selfsend.go. Used for both channel-level
	// in-flight suppression (before chat.postMessage's response
	// returns) and TS-level exact-match suppression (after).
	selfSend *selfSendDedup

	// nowTimestampFormatter renders "now" using the same format used
	// for message timestamps elsewhere (configured via
	// cfg.Appearance.TimestampFormat in main.go). Used by the optimistic
	// instant-display path to populate the Timestamp field of a
	// placeholder MessageItem so it renders identically to messages
	// that arrived via the normal HTTP-response / WS-echo paths.
	// Falls back to time.Now().Format("3:04 PM") when unset.
	nowTimestampFormatter func() string

	// bootstrap owns the multi-workspace startup overlay (loading flag,
	// per-workspace status entries, initial-active claim guard, and the
	// overlay's render). See internal/ui/bootstrap.go.
	bootstrap *workspaceBootstrap

	// spinnerFrame is the global tick counter for the loading spinner
	// glyph. Shared by the bootstrap overlay and the messages pane's
	// in-channel load spinner; advanced by SpinnerTickMsg.
	spinnerFrame int

	// drag owns the mouse-drag selection FSM (set by MouseClickMsg,
	// advanced by MouseMotionMsg, drained by MouseReleaseMsg). See
	// internal/ui/drag.go.
	drag *dragState

	// imageFetcher is the inline-image fetcher shared with the messages
	// pane; the App uses it to load the larger thumb when the user
	// opens the full-screen preview overlay. Wired via SetImageFetcher
	// from main.go, after Detect / cache construction.
	imageFetcher *imgpkg.Fetcher

	// imgProtocol is the active terminal image protocol detected at
	// startup. Used to render the full-screen preview overlay.
	imgProtocol imgpkg.Protocol

	// preview owns the full-screen image preview overlay state
	// (overlay + the channel/ts/attIdx triple that lets h/l/arrow
	// cycling locate sibling attachments). See internal/ui/imagepreview.go.
	preview *imagePreviewController

	// sixelFrames is the shared frame store correlating every sixel
	// App.View with the Bubble Tea frame that actually flushes. The App
	// publishes immutable placement snapshots here; FrameOutput (wired
	// in main) takes the exact flushed frame and paints it after the
	// text diff. nil until SetSixelFrameStore runs.
	sixelFrames *imgpkg.SixelFrameStore

	// forceSixelRepaint marks the next published frame for a full
	// erase/repaint. Set on a real terminal resize (width or height
	// actually changed), consumed by finalizeSixelView after the next
	// publication.
	forceSixelRepaint bool

	// Compositor memo (Stage A perf). bubbletea v2 calls View() after
	// EVERY message -- every keystroke, key-repeat, mouse-motion, and
	// tick -- synchronously in the event loop (tea.go render-on-update).
	// The 60fps cap only throttles the terminal flush, not View(). So
	// the final JoinHorizontal/JoinVertical compositing (~3.6ms on a
	// large screen) otherwise re-runs even when every cached panel is
	// byte-identical to the previous frame (the dominant cost during a
	// >100Hz mouse-motion drag-select, where selection extension is
	// already coalesced to 60Hz but View() still fires per raw event).
	//
	// When no overlay/preview is active and the panel strings + status
	// row match the last frame, View() reuses lastScreen and skips the
	// re-join entirely. lastPanels holds references to the (immutable,
	// mostly cache-shared) per-panel strings from the previous frame;
	// equality against the freshly-rendered panels is O(1) per panel on
	// a cache hit because the strings share a backing array.
	lastScreen      string
	lastPanels      []string
	lastStatus      string
	lastScreenW     int
	lastScreenH     int
	lastScreenValid bool

	// Held-key scroll coalescing (Stage C perf). bubbletea v2 runs
	// View() after every message, and each j/k selection move bumps the
	// messages/thread pane Version -> the Stage A compositor memo misses
	// -> a full (~17-40ms at ultrawide sizes) render. A fast key-repeat
	// then queues keypresses faster than they render, so a held key
	// builds a backlog that keeps scrolling after release.
	//
	// Coalescing applies the FIRST move in a burst immediately (instant
	// single-tap feedback) and accumulates subsequent moves into
	// scrollPending without bumping any Version -- so those frames are
	// cheap Stage A memo hits and the input queue drains. A single
	// scrollFlushMsg tick applies the accumulated batch. scrollPanel
	// records which pane the pending moves target (focus cannot change
	// mid-burst without a non-Up/Down key, which force-flushes first).
	scrollPending        int
	scrollPanel          Panel
	scrollFlushScheduled bool
}

// pendingChannelMarkState is App.pendingChannelMark's slot: the newest
// channel read-cursor advance staged but not yet issued. A zero ts
// means "nothing staged".
type pendingChannelMarkState struct {
	channelID string
	ts        string
}

// pendingThreadMarkState is App.pendingThreadMark's slot: the newest
// thread read-cursor advance staged but not yet issued. A zero ts
// means "nothing staged".
type pendingThreadMarkState struct {
	channelID string
	threadTS  string
	ts        string
}

// selfMarkLimit caps the number of DISTINCT keys ONE selfMarkDedup
// holds (App keeps two: one for channel marks, one for thread marks).
// slk issues at most one mark per channel or thread entry and one per
// markFlushDebounce, and each echo consumes its entry within a round
// trip, so a live set is normally one or two. The cap only exists so a
// session whose echoes never arrive — a WebSocket that dropped and
// reconnected past them, say — cannot grow a set for the life of the
// process. Oldest entries are evicted first.
const selfMarkLimit = 64

// selfMarkKey identifies one read mark slk issued. threadTS is empty
// for a channel mark (conversations.mark) and set for a thread mark
// (subscriptions.thread.mark); ts is the cursor the mark carried, which
// is what the matching echo reports back.
type selfMarkKey struct {
	channelID string
	threadTS  string
	ts        string
}

// selfMarkDedup records the read marks slk issued itself so the echo
// helpers can tell slk's own channel_marked / thread_marked echo from
// one raised by another Slack client.
//
// Slack broadcasts both events to every connected client including the
// issuer, so each mark slk sends comes back a round trip later as a
// ChannelMarkedRemoteMsg or ThreadMarkedRemoteMsg carrying the ts slk
// just set. Applying that echo would drag the on-screen "── new ──"
// divider to the newest message — deleting the user's place moments
// after they looked at it. A mark from another client carries no such
// record and still moves the divider, which is correct: it means the
// user really did read the channel or thread elsewhere.
//
// Entries are counted, not merely present, and one echo consumes one
// count: N issued marks at the same key suppress N echoes, and the next
// one is treated as foreign.
//
// Recording races the echo in one direction only: an echo that arrives
// BEFORE its record is applied, not suppressed. Four of the five
// recording sites record before the mark is issued and so cannot lose
// that race: reduceChannelSelected's tier-1 entry mark, both legs of
// flushPendingMarks, and reduceThreads' ThreadRepliesLoadedMsg
// mark-on-open. What makes them safe is that each writes its record
// before the tea.Cmd carrying the mark is handed to Bubble Tea — not a
// shared service shape, because the two families differ. The thread
// sites call ThreadService.Mark, which only BUILDS a tea.Cmd. The
// channel sites call ChannelService.MarkRead, which returns a tea.Msg
// and issues the mark itself; they wrap that call in a closure, and
// the closure has not run yet. Either way nothing has reached Slack
// when the record lands.
//
// The one exposed site is MessagesLoadedMsg.MarkedTS: ChannelService's
// fetcher issues that mark itself on a cmd goroutine and reports the ts
// back afterwards, so its record cannot be written until the HTTP
// response has already returned — and the WebSocket broadcast of the
// same mark is independent of that response. Losing the race there
// costs a divider move, not the correctness of the read state.
//
// No site records after a mark COMPLETES. Recording on completion (the
// ThreadMarkedLocalMsg arm, say) would expose every thread mark to the
// race, including the mark-on-open whose echo is exactly what this
// dedup exists to suppress. The cost of recording early instead is that
// a mark Slack rejects leaves a record with no echo to consume it; it
// is evicted by selfMarkLimit, and until then it can hold a landmark
// still, never move one wrongly.
//
// Not goroutine-safe. Every caller runs on the Bubble Tea Update
// goroutine.
type selfMarkDedup struct {
	// counts holds the number of issued-but-unechoed marks per key.
	// A key is deleted once its count reaches zero.
	counts map[selfMarkKey]int
	// order lists the distinct keys in counts by first-record time,
	// oldest first. It is the eviction queue for selfMarkLimit.
	order []selfMarkKey
}

// record notes that slk has issued the mark k. A key missing the
// channel or the cursor identifies nothing and is dropped: callers
// record unconditionally from paths that may not have issued a mark at
// all (see MessagesLoadedMsg.MarkedTS).
func (d *selfMarkDedup) record(k selfMarkKey) {
	if k.channelID == "" || k.ts == "" {
		return
	}
	if d.counts == nil {
		d.counts = make(map[selfMarkKey]int)
	}
	if _, known := d.counts[k]; !known {
		d.order = append(d.order, k)
	}
	d.counts[k]++
	for len(d.order) > selfMarkLimit {
		delete(d.counts, d.order[0])
		d.order = d.order[1:]
	}
}

// consume reports whether k matches a mark slk issued and has not yet
// seen echoed, decrementing that mark's count if so.
func (d *selfMarkDedup) consume(k selfMarkKey) bool {
	n, ok := d.counts[k]
	if !ok {
		return false
	}
	if n > 1 {
		d.counts[k] = n - 1
		return true
	}
	delete(d.counts, k)
	for i, e := range d.order {
		if e == k {
			d.order = append(d.order[:i], d.order[i+1:]...)
			break
		}
	}
	return true
}

// len returns the number of distinct keys held; used to pin the bound.
func (d *selfMarkDedup) len() int { return len(d.counts) }

func NewApp() *App {
	// The window tree starts as a single window with no channel; the
	// first ChannelSelectedMsg apply records the channel on it.
	wins, rootWin := wintree.New(wintree.Channel{})
	app := &App{
		workspaceRail:         workspace.New(nil, 0),
		sidebar:               sidebar.New(nil),
		compose:               compose.New(""),
		statusbar:             statusbar.New(),
		channelFinder:         channelfinder.New(),
		searchResults:         searchresults.New(),
		newMessagePicker:      newmessagepicker.New(),
		workspaceFinder:       workspacefinder.New(),
		themeSwitcher:         themeswitcher.New(),
		presenceMenu:          presencemenu.New(),
		help:                  help.New(),
		threadPanel:           thread.New(),
		threadCompose:         compose.New("thread"),
		threadsView:           threadsview.New(nil, ""),
		linkPicker:            linkpicker.New(),
		reactionPicker:        reactionpicker.New(),
		reactionsView:         reactionsview.New(),
		confirmPrompt:         confirmprompt.New(),
		mode:                  ModeNormal,
		focusedPanel:          PanelSidebar,
		wins:                  wins,
		focusedWin:            rootWin,
		sidebarVisible:        true,
		view:                  ViewChannels,
		keys:                  DefaultKeyMap(),
		selfSend:              newSelfSendDedup(),
		bootstrap:             newWorkspaceBootstrap(),
		windowTitle:           "slk",
		threadsDirtyDebounce:  defaultThreadsDirtyDebounce,
		terminalFocused:       true,
		inTmux:                os.Getenv("TMUX") != "",
		markFlushDebounce:     defaultMarkFlushDebounce,
		channelSearchDebounce: channelSearchDebounceDelay,
		fetchingOlder:         map[string]bool{},
		mouseWheelLines:       3,
		userNames:             map[string]string{},
		externalUsers:         map[string]bool{},
		presence:              newPresenceController(),
		renderCache:           newPanelRenderCache(),
		drag:                  newDragState(),
		preview:               newImagePreviewController(),
		layout:                newPanelLayout(),
		reactions:             noopReactionService,
		threads:               noopThreadService,
		messageSvc:            noopMessageService,
		channels:              noopChannelService,
		searchSvc:             noopSearchService,
		lastChannelByTeam:     map[string]string{},
		workspaceDomains:      map[string]string{},
		browserOpener:         openURLCmd,
		navHistory:            newNavHistoryStore(),
		clipboardRead:         defaultClipboardReader,
		clipboardWrite:        defaultClipboardWriter,
	}
	// Root model deliberately bypasses newWindowModel: the config
	// retention fields (avatarFn, userNames, emojiCtx, ...) are still
	// empty at construction — main.go's Set* forwarders run later and
	// reach the root model through the messagepane pointer directly.
	rootModel := messages.New(nil, "")
	app.winModels = map[wintree.LeafID]*messages.Model{rootWin: &rootModel}
	app.messagepane = app.winModels[rootWin]
	app.editing = newEditController()
	// typing tracker is referenced by typingOut so it must exist first;
	// construct outside the literal because struct literals can't
	// reference sibling fields.
	app.typing = newTypingTracker()
	app.typingOut = newTypingBroadcaster(app.typing)
	// Seed the picker with built-in emojis so the autocomplete works even
	// before the first workspace finishes loading customs.
	app.compose.SetEmojiEntries(emoji.BuildEntries(nil))
	app.threadCompose.SetEmojiEntries(emoji.BuildEntries(nil))
	// Seed the channel finder with the "Threads" view shortcut so users
	// can jump to the threads-list view from the same overlay they use
	// to switch channels (ctrl+t / ctrl+p). Selecting this row dispatches
	// ThreadsViewActivatedMsg in handleChannelFinderMode below.
	app.channelFinder.SetSyntheticItems([]channelfinder.Item{{
		ID:     channelfinder.ThreadsViewID,
		Name:   "Threads",
		Type:   "threads",
		Joined: true,
	}})
	// Seed the statusbar hint with the configured help key label so it
	// stays accurate if the binding is ever changed.
	app.statusbar.SetHelpHint(app.defaultHelpHint())
	return app
}

// defaultHelpHint is the resting statusbar hint ("? for keybindings").
// Seeded at construction and restored when a transient hint (e.g. the
// ctrl+w chord prefix) ends — clearing to "" would erase it for good.
func (a *App) defaultHelpHint() string {
	if helpKey := a.keys.Help.Help().Key; helpKey != "" {
		return helpKey + " for keybindings"
	}
	return ""
}

func (a *App) Init() tea.Cmd {
	if a.bootstrap.IsLoading() {
		return tea.Batch(
			tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
				return SpinnerTickMsg{}
			}),
			tea.Tick(15*time.Second, func(time.Time) tea.Msg {
				return LoadingTimeoutMsg{}
			}),
		)
	}
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Phase 4 reducer chain (extension point — see internal/ui/reducers.go).
	// Each reducer owns a cohesive family of message types and either
	// claims the message (returning its cmd, true) or passes (nil,
	// false). The first claimant short-circuits the rest of Update;
	// unclaimed messages fall through to the residual switch below.
	// Reducers are added to this chain one family at a time as the
	// switch shrinks. Order doesn't matter semantically — message
	// types are disjoint across reducers — but stable order keeps
	// the trace predictable.
	if cmd, handled := dispatchReducers(a, msg,
		a.presence,
		a.preview,
		a.drag,
		a.typing,
		a.bootstrap,
		reduceReactions,
		reduceThreads,
		reduceFocus,
		reduceSend,
		reduceChannels,
		reduceLinks,
		reduceFiles,
		reduceSearch,
		reduceWorkspace,
		reduceNewMessagePicker,
		reduceIO,
		reduceMouse,
	); handled {
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)
	}

	// While the full-screen image preview overlay is open, route close
	// and cycle keys directly:
	//   - Esc/q: dismiss
	//   - Enter: dismiss + launch OS image viewer
	//   - h or left arrow: previous sibling image (wraps)
	//   - l or right arrow: next sibling image (wraps)
	// All other keys are swallowed so navigation in the messages pane
	// doesn't leak through. Resize / mouse / async messages still flow
	// normally so the rest of the UI keeps ticking — including the
	// previewLoadedMsg arm that swaps the cycled image into place.
	if a.preview.Active() {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				a.preview.Close()
				return a, nil
			case "enter":
				path := a.preview.Overlay().Path()
				a.preview.Close()
				return a, openInSystemViewerCmd(path)
			case "h", "left":
				if a.preview.Overlay().SiblingCount() > 1 {
					return a, a.cycleImagePreviewCmd(a.preview.Channel(), a.preview.TS(), a.preview.AttIdx(), -1)
				}
				return a, nil
			case "l", "right":
				if a.preview.Overlay().SiblingCount() > 1 {
					return a, a.cycleImagePreviewCmd(a.preview.Channel(), a.preview.TS(), a.preview.AttIdx(), +1)
				}
				return a, nil
			}
			return a, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Only a real size change forces a sixel repaint; a duplicate
		// size message (same dimensions) must not erase and repaint
		// every image pointlessly.
		changed := a.width != msg.Width || a.height != msg.Height
		a.width = msg.Width
		a.height = msg.Height
		if changed {
			a.forceSixelRepaint = true
		}
		return a, nil

	case scrollFlushMsg:
		if cmd := a.applyScrollFlush(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)

	case tea.KeyMsg:
		cmd := a.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		// All non-WindowSize, non-Key message types are owned by Phase 4
		// reducers; see the dispatch chain at the top of Update and the
		// per-family reducer_*.go files / controller Handle methods.
	}

	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Ctrl+C is intercepted globally and routed through the same
	// confirm prompt as lowercase `q`, so an accidental Ctrl+C while
	// reading or typing doesn't yank the whole app out from under the
	// user. `Q` (capital) remains the no-prompt force-quit, and an
	// already-open quit prompt isn't reopened (Enter confirms, Esc
	// cancels via the existing confirm-mode handler).
	if key.Matches(msg, a.keys.Quit) {
		if a.mode != ModeConfirm {
			a.openQuitConfirm()
		}
		return nil
	}

	if a.bootstrap.IsLoading() {
		return nil
	}

	// Held-key scroll coalescing guard: any key other than j/k (Up/Down)
	// must act on the up-to-date selection, so drain accumulated scroll
	// moves before dispatching it. Up/Down themselves feed the coalescer
	// (see coalesceContentScroll) and must not flush.
	var pre tea.Cmd
	if a.scrollPending != 0 && !key.Matches(msg, a.keys.Down) && !key.Matches(msg, a.keys.Up) {
		pre = a.flushScrollCoalesce()
	}

	// Mode-specific handling. Dispatch table lives in
	// mode_handlers.go; unmapped modes fall back to Normal
	// (mirrors the pre-Phase-5 `default:` arm).
	cmd := dispatchModeKey(a, msg)
	if pre != nil {
		return tea.Batch(pre, cmd)
	}
	return cmd
}

// navigateBack walks the per-workspace history stack one step
// backward, skipping any channel IDs that no longer resolve via
// channelLookup (and dropping them from the stack). Returns a tea.Cmd
// that synthesizes a ChannelSelectedMsg{FromHistory: true} for the
// new target, or nil if there's no valid earlier entry.
func (a *App) navigateBack() tea.Cmd {
	return a.walkNavCmd(-1)
}

// navigateForward is the symmetric opposite of navigateBack.
func (a *App) navigateForward() tea.Cmd {
	return a.walkNavCmd(+1)
}

// walkNavCmd is the shared wrapper that turns navHistoryStore.Walk's
// pure result into the ChannelSelectedMsg{FromHistory: true} tea.Cmd
// the App emits. step must be -1 or +1.
func (a *App) walkNavCmd(step int) tea.Cmd {
	id, name, ctype, ok := a.navHistory.Walk(a.activeTeamID, step, a.channels.Lookup)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		return ChannelSelectedMsg{ID: id, Name: name, Type: ctype, FromHistory: true}
	}
}

// handleNormalMode moved to mode_normal.go (Phase 5k).

// handleInsertMode moved to mode_insert.go (Phase 5l).

// handleCommandMode moved to mode_command.go (Phase 5b).

// handleChannelFinderMode moved to mode_channel_finder.go (Phase 5h).

// handleWorkspaceFinderMode moved to mode_workspace_finder.go (Phase 5e).

// handleThemeSwitcherMode moved to mode_theme_switcher.go (Phase 5g).

// handleHelpMode moved to mode_help.go (Phase 5c).

// handlePresenceMenuMode moved to mode_presence_menu.go (Phase 5f).

// handlePresenceCustomSnoozeMode moved to mode_presence_snooze.go (Phase 5d).

// handleReactionPickerMode moved to mode_reaction_picker.go (Phase 5i).

// handleConfirmMode moved to mode_confirm.go (Phase 5j).

func (a *App) updateReactionOnMessage(channelID, messageTS, emojiName, userID string, remove bool) {
	// Pane writes fan out to every window viewing the channel
	// (per-window models deep-copy Reactions at clone/load time, so
	// per-model in-place updates can't cross-leak). The thread panel
	// is a singleton following the focused window; its write is
	// unconditional as before — UpdateReaction no-ops on a missing TS.
	for _, mm := range a.modelsForChannel(channelID) {
		mm.UpdateReaction(messageTS, emojiName, userID, remove)
	}
	a.threadPanel.UpdateReaction(messageTS, emojiName, userID, remove)
}

func (a *App) handleReactionNav(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.Left):
		a.messagepane.ReactionNavLeft()
	case key.Matches(msg, a.keys.Right):
		a.messagepane.ReactionNavRight()
	case key.Matches(msg, a.keys.Enter):
		emojiName, isPlus := a.messagepane.SelectedReaction()
		if isPlus {
			return a.openPickerFromMessage()
		}
		return a.toggleReactionOnSelectedMessage(emojiName)
	case key.Matches(msg, a.keys.Reaction):
		return a.openPickerFromMessage()
	case key.Matches(msg, a.keys.Escape):
		a.messagepane.ExitReactionNav()
	}
	return nil
}

func (a *App) handleThreadReactionNav(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.Left):
		a.threadPanel.ReactionNavLeft()
	case key.Matches(msg, a.keys.Right):
		a.threadPanel.ReactionNavRight()
	case key.Matches(msg, a.keys.Enter):
		emojiName, isPlus := a.threadPanel.SelectedReaction()
		if isPlus {
			return a.openPickerFromThread()
		}
		return a.toggleReactionOnSelectedThread(emojiName)
	case key.Matches(msg, a.keys.Reaction):
		return a.openPickerFromThread()
	case key.Matches(msg, a.keys.Escape):
		a.threadPanel.ExitReactionNav()
	}
	return nil
}

func (a *App) openPickerFromMessage() tea.Cmd {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return nil
	}
	var existing []string
	for _, r := range msg.Reactions {
		if r.HasReacted {
			existing = append(existing, r.Emoji)
		}
	}
	a.messagepane.ExitReactionNav()
	a.reactionPicker.SetFrecentEmoji(a.reactions.LoadFrecent(10))
	a.reactionPicker.Open(a.activeChannelID, msg.TS, existing)
	a.SetMode(ModeReactionPicker)
	return nil
}

func (a *App) openPickerFromThread() tea.Cmd {
	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		return nil
	}
	var existing []string
	for _, r := range reply.Reactions {
		if r.HasReacted {
			existing = append(existing, r.Emoji)
		}
	}
	a.threadPanel.ExitReactionNav()
	a.reactionPicker.SetFrecentEmoji(a.reactions.LoadFrecent(10))
	a.reactionPicker.Open(a.threadPanel.ChannelID(), reply.TS, existing)
	a.SetMode(ModeReactionPicker)
	return nil
}

// openReactionsView opens the read-only reactions list for the selected
// message (main pane) or selected reply (thread pane). It is a no-op when
// nothing is selected or the target has no reactions.
func (a *App) openReactionsView() tea.Cmd {
	var reactions []messages.ReactionItem
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		reactions = msg.Reactions
		a.messagepane.ExitReactionNav()
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		reactions = reply.Reactions
		a.threadPanel.ExitReactionNav()
	default:
		return nil
	}
	if len(reactions) == 0 {
		return nil
	}
	a.reactionsView.Open(a.buildReactionGroups(reactions))
	a.SetMode(ModeReactionsView)
	return nil
}

// buildReactionGroups resolves each reaction's user IDs to display names,
// marking the current user with a "(you)" suffix.
func (a *App) buildReactionGroups(reactions []messages.ReactionItem) []reactionsview.ReactionGroup {
	groups := make([]reactionsview.ReactionGroup, 0, len(reactions))
	for _, r := range reactions {
		users := make([]string, 0, len(r.UserIDs))
		for _, uid := range r.UserIDs {
			name := a.userNameFor(uid)
			if uid == a.currentUserID {
				name += " (you)"
			}
			users = append(users, name)
		}
		groups = append(groups, reactionsview.ReactionGroup{Emoji: r.Emoji, Users: users, Count: r.Count})
	}
	return groups
}

func (a *App) toggleReactionOnSelectedMessage(emojiName string) tea.Cmd {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return nil
	}
	remove := false
	for _, r := range msg.Reactions {
		if r.Emoji == emojiName && r.HasReacted {
			remove = true
			break
		}
	}
	a.updateReactionOnMessage(a.activeChannelID, msg.TS, emojiName, a.currentUserID, remove)
	channelID := ids.ChannelID(a.activeChannelID)
	ts := ids.MessageTS(msg.TS)
	sent := ReactionSentMsg{ChannelID: a.activeChannelID, MessageTS: msg.TS, Emoji: emojiName, UserID: a.currentUserID, Remove: remove}
	if remove {
		return func() tea.Msg {
			sent.Err = a.reactions.Remove(channelID, ts, emojiName)
			return sent
		}
	}
	return func() tea.Msg {
		sent.Err = a.reactions.Add(channelID, ts, emojiName)
		return sent
	}
}

func (a *App) toggleReactionOnSelectedThread(emojiName string) tea.Cmd {
	reply := a.threadPanel.SelectedReply()
	if reply == nil {
		return nil
	}
	remove := false
	for _, r := range reply.Reactions {
		if r.Emoji == emojiName && r.HasReacted {
			remove = true
			break
		}
	}
	threadChannelID := a.threadPanel.ChannelID()
	a.updateReactionOnMessage(threadChannelID, reply.TS, emojiName, a.currentUserID, remove)
	channelID := ids.ChannelID(threadChannelID)
	ts := ids.MessageTS(reply.TS)
	sent := ReactionSentMsg{ChannelID: threadChannelID, MessageTS: reply.TS, Emoji: emojiName, UserID: a.currentUserID, Remove: remove}
	if remove {
		return func() tea.Msg {
			sent.Err = a.reactions.Remove(channelID, ts, emojiName)
			return sent
		}
	}
	return func() tea.Msg {
		sent.Err = a.reactions.Add(channelID, ts, emojiName)
		return sent
	}
}

// toggleReactionOnMessageItem toggles the current user's reaction on
// an arbitrary message (not necessarily the selected one). Used by the
// click-on-pill path, which identifies the target message by its
// rendered hit rect rather than by selection. Behavior matches
// toggleReactionOnSelectedMessage: optimistic in-memory update + an
// async tea.Cmd that issues the Slack add/remove call.
func (a *App) toggleReactionOnMessageItem(channelIDStr string, msg messages.MessageItem, emojiName string) tea.Cmd {
	remove := false
	for _, r := range msg.Reactions {
		if r.Emoji == emojiName && r.HasReacted {
			remove = true
			break
		}
	}
	a.updateReactionOnMessage(channelIDStr, msg.TS, emojiName, a.currentUserID, remove)
	channelID := ids.ChannelID(channelIDStr)
	ts := ids.MessageTS(msg.TS)
	sent := ReactionSentMsg{ChannelID: channelIDStr, MessageTS: msg.TS, Emoji: emojiName, UserID: a.currentUserID, Remove: remove}
	if remove {
		return func() tea.Msg {
			sent.Err = a.reactions.Remove(channelID, ts, emojiName)
			return sent
		}
	}
	return func() tea.Msg {
		sent.Err = a.reactions.Add(channelID, ts, emojiName)
		return sent
	}
}

// copyPermalinkOfSelected resolves the currently-selected message or thread
// reply, calls the permalink fetcher, and returns a tea.Cmd that writes the
// URL to the clipboard and emits a status-bar toast.
func (a *App) copyPermalinkOfSelected() tea.Cmd {
	var channelID, ts string
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		channelID = a.activeChannelID
		ts = msg.TS
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		channelID = a.threadPanel.ChannelID()
		ts = reply.TS
	default:
		return nil
	}
	if channelID == "" || ts == "" {
		return nil
	}
	messageSvc := a.messageSvc
	cID := ids.ChannelID(channelID)
	mTS := ids.MessageTS(ts)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		url, err := messageSvc.Permalink(ctx, cID, mTS)
		if err != nil {
			log.Printf("copy permalink: %v", err)
			return statusbar.PermalinkCopyFailedMsg{}
		}
		if url == "" {
			// No permalink wired (noop service) or Slack returned an
			// empty URL. Silent no-op rather than copying "" with a
			// false success toast.
			return nil
		}
		return tea.Batch(
			a.clipboardWrite(url),
			func() tea.Msg { return statusbar.PermalinkCopiedMsg{} },
		)()
	}
}

// openLinksOfSelected implements the `o` keybinding: collect the
// links in the selected message (messages pane or thread panel).
// 0 links -> toast; 1 link -> dispatch OpenLinkMsg directly; 2+ ->
// open the link picker modal. All opens converge on OpenLinkMsg,
// the single routing point in reducer_links.go.
func (a *App) openLinksOfSelected() tea.Cmd {
	var text string
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		text = msg.Text
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		text = reply.Text
	default:
		return nil
	}
	links := messages.ExtractLinks(text)
	switch len(links) {
	case 0:
		return func() tea.Msg { return ToastMsg{Text: "No links in message"} }
	case 1:
		url := links[0].URL
		return func() tea.Msg { return OpenLinkMsg{URL: url} }
	default:
		items := make([]linkpicker.Item, len(links))
		for i, l := range links {
			items[i] = linkpicker.Item{URL: l.URL, Label: l.Label, InApp: a.linkOpensInApp(l.URL)}
		}
		a.pickerKind = "links"
		a.linkPicker.Open("Open link", items)
		a.SetMode(ModeLinkPicker)
		return nil
	}
}

// downloadFilesOfSelected implements the `d` keybinding: collect the
// downloadable (non-image) file attachments of the selected message
// (messages pane or thread panel). 0 files -> toast; 1 file ->
// dispatch DownloadFileMsg directly; 2+ -> open the picker modal in
// "files" mode. Mirrors openLinksOfSelected. Images are excluded: they
// already have the preview flow (O/v).
func (a *App) downloadFilesOfSelected() tea.Cmd {
	var atts []messages.Attachment
	switch a.focusedPanel {
	case PanelMessages:
		msg, ok := a.messagepane.SelectedMessage()
		if !ok {
			return nil
		}
		atts = msg.Attachments
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return nil
		}
		atts = reply.Attachments
	default:
		return nil
	}
	files := make([]messages.Attachment, 0, len(atts))
	for _, att := range atts {
		if att.Kind == "file" && att.DownloadURL != "" {
			files = append(files, att)
		}
	}
	switch len(files) {
	case 0:
		return func() tea.Msg { return ToastMsg{Text: "No files in message"} }
	case 1:
		att := files[0]
		return func() tea.Msg { return DownloadFileMsg{Attachment: att} }
	default:
		items := make([]linkpicker.Item, len(files))
		for i, f := range files {
			var detail string
			if f.Size > 0 {
				detail = humanSize(f.Size)
			}
			items[i] = linkpicker.Item{Label: f.Name, Detail: detail}
		}
		a.pickerKind = "files"
		a.pickerFiles = files
		a.linkPicker.Open("Download file", items)
		a.SetMode(ModeLinkPicker)
		return nil
	}
}

// linkOpensInApp reports whether routeLink would navigate this URL
// inside slk (used for the picker's "[slk]" badge). Mirrors the
// guards at the top of routeLink.
func (a *App) linkOpensInApp(rawURL string) bool {
	pl, ok := slackurl.Parse(rawURL)
	if !ok {
		return false
	}
	domain := a.activeWorkspaceDomain()
	if domain == "" || pl.Subdomain != domain {
		return false
	}
	_, _, found := a.channels.Lookup(pl.ChannelID)
	return found
}

func (a *App) saveThreadToFile() tea.Cmd {
	if a.focusedPanel != PanelThread {
		return func() tea.Msg { return ToastMsg{Text: "Open a thread first"} }
	}
	if a.threadPanel.IsEmpty() {
		return nil
	}
	parent := a.threadPanel.ParentMsg()
	replies := a.threadPanel.Replies()
	userNames := a.threadPanel.UserNames()
	channelNames := a.threadPanel.ChannelNames()

	channelName := "thread"
	if channelNames != nil {
		if name, ok := channelNames[a.threadPanel.ChannelID()]; ok {
			channelName = name
		}
	}

	return func() tea.Msg {
		content := export.ThreadToMarkdown(parent, replies, userNames, channelNames)

		dir, err := export.ExportDir()
		if err != nil {
			return statusbar.ThreadSaveFailedMsg{Reason: err.Error()}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return statusbar.ThreadSaveFailedMsg{Reason: err.Error()}
		}
		filename := fmt.Sprintf("slk-thread-%s-%s.md", sanitizeForFilename(channelName), time.Now().Format("2006-01-02-150405"))
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return statusbar.ThreadSaveFailedMsg{Reason: err.Error()}
		}
		return statusbar.ThreadSavedMsg{Path: path}
	}
}

func sanitizeForFilename(s string) string {
	var b strings.Builder
	prev := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
			prev = false
		} else if !prev {
			b.WriteByte('-')
			prev = true
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "unknown"
	}
	return result
}

// scrollFlushInterval is the coalescing window for held j/k selection
// moves. 16ms (~60Hz) is short enough that batched moves still feel
// continuous, long enough to collapse a fast key-repeat burst into one
// render per tick. See the scrollPending field doc on App.
const scrollFlushInterval = 16 * time.Millisecond

// scrollFlushMsg drains accumulated held-key scroll moves. Scheduled by
// coalesceContentScroll (at most one in flight via scrollFlushScheduled).
type scrollFlushMsg struct{}

func scrollFlushTickCmd() tea.Cmd {
	return tea.Tick(scrollFlushInterval, func(time.Time) tea.Msg {
		return scrollFlushMsg{}
	})
}

func (a *App) handleDown() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveDown()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.MoveDown()
			// j: held-key burst — debounce the network fetch so we
			// don't fire one conversations.replies call per row.
			return a.openSelectedThreadCmd(true)
		}
		return a.coalesceContentScroll(+1)
	case PanelThread:
		return a.coalesceContentScroll(+1)
	}
	return nil
}

func (a *App) handleUp() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveUp()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.MoveUp()
			// k: same debounce as j — see handleDown.
			return a.openSelectedThreadCmd(true)
		}
		return a.coalesceContentScroll(-1)
	case PanelThread:
		return a.coalesceContentScroll(-1)
	}
	return nil
}

// coalesceContentScroll batches a j/k selection move (delta +1 = down,
// -1 = up) on a tall content pane (channel messages or thread). The
// first move in a burst applies immediately for instant feedback and
// arms the flush tick; subsequent moves within the window only
// accumulate scrollPending (no Version bump -> Stage A memo hit), so a
// held key cannot outpace rendering. See the scrollPending field doc.
func (a *App) coalesceContentScroll(delta int) tea.Cmd {
	if !a.scrollFlushScheduled {
		a.scrollFlushScheduled = true
		a.scrollPanel = a.focusedPanel
		cmd := a.applyScrollMove(a.focusedPanel, delta)
		return tea.Batch(cmd, scrollFlushTickCmd())
	}
	// Focus cannot change mid-burst without a non-Up/Down key, which
	// force-flushes pending first (see handleKey); so scrollPanel stays
	// valid. Guard defensively anyway: a focus mismatch flushes and
	// restarts on the current pane.
	if a.focusedPanel != a.scrollPanel {
		flush := a.flushScrollCoalesce()
		a.scrollPanel = a.focusedPanel
		return tea.Batch(flush, a.applyScrollMove(a.focusedPanel, delta))
	}
	a.scrollPending += delta
	return nil
}

// applyScrollMove applies |delta| MoveUp/MoveDown steps to the given
// content pane and returns any follow-up cmd (older-history backfill
// when an up-scroll lands the channel pane at the top). delta>0 = down.
func (a *App) applyScrollMove(panel Panel, delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	switch panel {
	case PanelMessages:
		if delta > 0 {
			for i := 0; i < delta; i++ {
				a.messagepane.MoveDown()
			}
			return nil
		}
		for i := 0; i < -delta; i++ {
			a.messagepane.MoveUp()
		}
		// Selection reached the top: backfill older history (same UX
		// as the pre-coalescing path and the wheel/PageUp route).
		return a.maybeFetchOlderHistory(a.messagepane.AtTop())
	case PanelThread:
		if delta > 0 {
			for i := 0; i < delta; i++ {
				a.threadPanel.MoveDown()
			}
		} else {
			for i := 0; i < -delta; i++ {
				a.threadPanel.MoveUp()
			}
		}
	}
	return nil
}

// applyScrollFlush drains the accumulated held-key scroll batch. Invoked
// from the scrollFlushMsg tick. While moves keep arriving it applies the
// batch and RESCHEDULES itself (keeping scrollFlushScheduled true) so the
// expensive render stays paced at the flush cadence rather than firing
// once per keypress. When a tick finds nothing pending, the held
// sequence has ended: clear the scheduled flag so the next fresh tap is
// applied immediately again.
func (a *App) applyScrollFlush() tea.Cmd {
	n := a.scrollPending
	a.scrollPending = 0
	if n == 0 {
		a.scrollFlushScheduled = false
		return nil
	}
	cmd := a.applyScrollMove(a.scrollPanel, n)
	return tea.Batch(cmd, scrollFlushTickCmd())
}

// flushScrollCoalesce applies any pending (not-yet-rendered) scroll moves
// immediately so that a subsequent selection-reading action (Enter,
// reaction, click, etc.) sees the correct selected message. Does not
// touch scrollFlushScheduled: a tick already in flight harmlessly
// no-ops when it finds scrollPending == 0.
func (a *App) flushScrollCoalesce() tea.Cmd {
	if a.scrollPending == 0 {
		return nil
	}
	n := a.scrollPending
	a.scrollPending = 0
	return a.applyScrollMove(a.scrollPanel, n)
}

func (a *App) handleGoToBottom() tea.Cmd {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.GoToBottom()
	case PanelMessages:
		if a.view == ViewThreads {
			a.threadsView.GoToBottom()
			// G is a one-shot jump — fire the fetch immediately.
			return a.openSelectedThreadCmd(false)
		}
		a.messagepane.GoToBottom()
	case PanelThread:
		a.threadPanel.GoToBottom()
	}
	return nil
}

// pageSize returns the number of lines to scroll for a full-page jump in the
// currently-focused panel. Falls back to a sensible default if the layout
// hasn't been measured yet (i.e. before the first render).
func (a *App) pageSize() int {
	h := a.layout.PageHeight(a.focusedPanel)
	if h < 4 {
		h = 4
	}
	// Leave one line of context across the page boundary (vim-style).
	return h - 1
}

// halfPageSize returns the half-page scroll distance for ctrl+u / ctrl+d.
func (a *App) halfPageSize() int {
	n := a.pageSize() / 2
	if n < 1 {
		n = 1
	}
	return n
}

// panelAt is a thin wrapper around panelLayout.PanelAt that forwards
// the App's current height + visibility flags.
func (a *App) panelAt(x, y int) (panel Panel, paneX, paneY int, ok bool) {
	return a.layout.PanelAt(x, y, a.height, a.sidebarVisible, a.threadVisible)
}

// scrollFocusedPanel scrolls the focused panel by delta lines (negative = up).
// This is the keyboard equivalent of the mouse wheel:
// PageUp/PageDown/Ctrl+U/Ctrl+D move the viewport. In the messages pane the
// selected-message cursor follows the scroll, clamping to the nearest visible
// message so it never scrolls off-screen (see viewInternal's cursor-clamp
// step). The sidebar/threads panels keep their own scroll/selection coupling.
//
// On a scroll-up that lands the messages-pane viewport at the very top, this
// also triggers a fetch of older channel history -- the same UX the
// selection-based AtTop path provides via handleUp.
func (a *App) scrollFocusedPanel(delta int) tea.Cmd {
	if delta == 0 {
		return nil
	}
	n := delta
	if n < 0 {
		n = -n
	}
	switch a.focusedPanel {
	case PanelSidebar:
		if delta < 0 {
			a.sidebar.ScrollUp(n)
		} else {
			a.sidebar.ScrollDown(n)
		}
	case PanelMessages:
		if a.view == ViewThreads {
			if delta < 0 {
				a.threadsView.ScrollUp(n)
			} else {
				a.threadsView.ScrollDown(n)
			}
		} else {
			if delta < 0 {
				a.messagepane.ScrollUp(n)
				return a.maybeFetchOlderHistory(a.messagepane.ViewportAtTop())
			}
			a.messagepane.ScrollDown(n)
		}
	case PanelThread:
		if delta < 0 {
			a.threadPanel.ScrollUp(n)
		} else {
			a.threadPanel.ScrollDown(n)
		}
	}
	return nil
}

// maybeFetchOlderHistory kicks off a backfill of older channel history when
// `atTop` is true, no fetch is already in flight, and a fetcher is wired up.
// Returns nil otherwise. Centralizes the spinner-tick + fetch-cmd batching
// previously duplicated across handleUp / the mouse-wheel handler / page-up.
func (a *App) maybeFetchOlderHistory(atTop bool) tea.Cmd {
	// Backfill is triggered by focused-window scrolling, so the
	// gate/set are keyed by the focused channel.
	if !atTop || a.fetchingOlder[a.activeChannelID] {
		return nil
	}
	a.fetchingOlder[a.activeChannelID] = true
	a.messagepane.SetLoading(true)
	chID := ids.ChannelID(a.activeChannelID)
	oldestTS := ids.MessageTS(a.messagepane.OldestTS())
	channels := a.channels
	// Kick the spinner tick: if a.loading is already false (workspace
	// fully loaded), no tick is alive and the glyph would freeze on its
	// last frame.
	return tea.Batch(
		tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
			return SpinnerTickMsg{}
		}),
		func() tea.Msg {
			return channels.FetchOlder(chID, oldestTS)
		},
	)
}

// openQuitConfirm raises the centered "Quit slk?" overlay. Called from
// both lowercase `q` and Ctrl+C (the latter intercepted globally so an
// accidental Ctrl+C in any mode never silently kills the app).
func (a *App) openQuitConfirm() {
	a.confirmPrompt.Open(
		"Quit slk?",
		"All workspace connections will close.",
		func() tea.Msg { return tea.Quit() },
	)
	a.SetMode(ModeConfirm)
}

func (a *App) handleEnter() tea.Cmd {
	if a.focusedPanel == PanelSidebar {
		if a.sidebar.IsThreadsSelected() {
			return func() tea.Msg { return ThreadsViewActivatedMsg{} }
		}
		// A section header? Toggle its collapse state and stay in
		// place. Section headers are also navigable via j/k so the
		// user can expand/collapse the firehose Channels section
		// (collapsed by default) without leaving the keyboard.
		if a.sidebar.ToggleCollapseSelected() {
			return nil
		}
		item, ok := a.sidebar.SelectedItem()
		if ok {
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: item.ID, Name: item.Name, Type: item.Type}
			}
		}
	}

	// In the threads-list view, the messages-pane region renders the
	// threadsView model instead of the channel's message history.
	// focusedPanel stays at PanelMessages (we re-use that panel slot),
	// so handleEnter must explicitly route to the highlighted thread
	// here — otherwise the PanelMessages block below falls through to
	// messagepane.SelectedMessage() and opens whatever was highlighted
	// in the underlying channel. Enter also shifts keyboard focus to
	// the thread pane (mirroring the channel-pane Enter semantics:
	// "enter this thread to interact with it"), distinguishing it
	// from the j/k navigation which preserves PanelMessages focus so
	// the user can keep walking the list.
	if a.focusedPanel == PanelMessages && a.view == ViewThreads {
		if _, ok := a.threadsView.SelectedSummary(); !ok {
			return nil
		}
		// Force the open even when openSelectedThreadCmd would
		// otherwise dedup (because j/k or activation already loaded
		// this thread): the user explicitly asked to enter it. We
		// reset the dedup keys so the helper runs its full open
		// path, then re-set focus to the thread pane.
		a.lastOpenedChannelID = ""
		a.lastOpenedThreadTS = ""
		cmd := a.openSelectedThreadCmd(false)
		a.focusedPanel = PanelThread
		return cmd
	}

	if a.focusedPanel == PanelMessages {
		return a.openThreadForSelectedMessage()
	}

	return nil
}

// openThreadForSelectedMessage opens the thread panel for the message
// currently selected in the channel messages pane. Mirrors the Enter
// keypress on PanelMessages; called from both handleEnter and from the
// click-to-open-thread path so the two entry points stay in lockstep.
// Returns nil when there is no selected message or no threadFetcher.
func (a *App) openThreadForSelectedMessage() tea.Cmd {
	msg, ok := a.messagepane.SelectedMessage()
	if !ok {
		return nil
	}
	// Use the message's own TS as the thread parent.
	// If it's already a thread reply, use its ThreadTS instead.
	threadTS := msg.TS
	if msg.ThreadTS != "" && msg.ThreadTS != msg.TS {
		threadTS = msg.ThreadTS
	}
	return a.openThreadPanel(msg, a.activeChannelID, threadTS)
}

// openThreadPanel makes the thread panel visible for (channelID,
// threadTS) with the given parent row, primes replies from the thread
// cache, and returns a cmd that fetches authoritative replies. Shared
// by openThreadForSelectedMessage (parent taken from the pane buffer)
// and openThreadForPermalink (parent reconstructed from cache/stub).
func (a *App) openThreadPanel(parent messages.MessageItem, channelID, threadTS string) tea.Cmd {
	a.threadVisible = true
	a.statusbar.SetInThread(true)
	a.focusedPanel = PanelThread
	a.threadPanel.SetThread(parent, nil, channelID, threadTS)
	a.threadCompose.SetChannel("thread")
	a.applyThreadUnreadBoundary(channelID, threadTS)

	threads := a.threads
	chID := ids.ChannelID(channelID)
	tTS := ids.ThreadTS(threadTS)
	var batch []tea.Cmd
	if cached := threads.CacheRead(chID, tTS); len(cached) > 1 {
		replies := cached[1:] // strip parent; reducer expects replies-only
		batch = append(batch, func() tea.Msg {
			return ThreadRepliesLoadedMsg{ThreadTS: threadTS, Replies: replies}
		})
	}
	batch = append(batch, func() tea.Msg { return threads.Fetch(chID, tTS) })
	return tea.Batch(batch...)
}

func (a *App) SetMode(mode Mode) {
	// A mode change always disarms a pending ctrl+w chord — a global
	// intercept (e.g. ctrl+c quit-confirm) must not strand it armed.
	// The `if` guard scopes the hint restore to chord disarms only, so
	// other helpHint states aren't clobbered by unrelated mode changes.
	if a.pendingWinCmd {
		a.pendingWinCmd = false
		a.statusbar.SetHelpHint(a.defaultHelpHint())
	}
	if mode == ModeInsert {
		a.clearSelections()
	}
	// Leaving command mode by ANY path — including global intercepts
	// (ctrl+c quit-confirm) and async reducers that force a mode —
	// must drop the ':' prompt, or the statusbar keeps rendering a
	// stale command line forever. Keeps the cmdline field's "always
	// empty outside ModeCommand" invariant true; idempotent with
	// exitCommandMode, which clears before calling here.
	if a.mode == ModeCommand && mode != ModeCommand {
		a.cmdline = ""
		a.statusbar.SetCommandLine("")
	}
	a.mode = mode
	a.statusbar.SetMode(mode)
}

// exitInsertAfterSend mirrors the Esc-from-insert handler so that
// submitting a message returns the user to ModeNormal — matching the
// vim convention where finishing an action drops you back to command
// mode. Used by every send branch in handleInsertMode that actually
// dispatches a message (plain-text and attachment, channel and
// thread). Edits intentionally bypass this and instead exit insert
// mode via cancelEdit on MessageEditedMsg, so a transient edit
// failure doesn't strand the user one keystroke away from resuming.
func (a *App) exitInsertAfterSend() {
	a.SetMode(ModeNormal)
	a.compose.Blur()
	a.threadCompose.Blur()
}

// clearSelections drops any active mouse selection from every
// window's message pane and from the thread pane (an unfocused
// window can hold a selection pinned from when it was focused).
// Called from any handler that changes focus, mode,
// or visible content in a way that makes the existing selection
// nonsensical (workspace switch, mode change, focus cycle, etc.).
func (a *App) clearSelections() {
	for _, m := range a.allWinModels() {
		m.ClearSelection()
	}
	a.threadPanel.ClearSelection()
}

func (a *App) FocusNext() {
	a.cancelEdit()
	a.clearSelections()
	if !a.sidebarVisible {
		if a.threadVisible {
			if a.focusedPanel == PanelMessages {
				a.focusedPanel = PanelThread
			} else {
				a.focusedPanel = PanelMessages
			}
		}
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		a.focusedPanel = PanelMessages
	case PanelMessages:
		if a.threadVisible {
			a.focusedPanel = PanelThread
		} else {
			a.focusedPanel = PanelSidebar
		}
	case PanelThread:
		a.focusedPanel = PanelSidebar
	}
}

func (a *App) FocusPrev() {
	a.cancelEdit()
	a.clearSelections()
	if !a.sidebarVisible {
		if a.threadVisible {
			if a.focusedPanel == PanelThread {
				a.focusedPanel = PanelMessages
			} else {
				a.focusedPanel = PanelThread
			}
		}
		return
	}
	switch a.focusedPanel {
	case PanelSidebar:
		if a.threadVisible {
			a.focusedPanel = PanelThread
		} else {
			a.focusedPanel = PanelMessages
		}
	case PanelMessages:
		a.focusedPanel = PanelSidebar
	case PanelThread:
		a.focusedPanel = PanelMessages
	}
}

func (a *App) ToggleSidebar() {
	a.clearSelections()
	a.sidebarVisible = !a.sidebarVisible
	if !a.sidebarVisible && a.focusedPanel == PanelSidebar {
		a.focusedPanel = PanelMessages
	}
}

func (a *App) ToggleThread() {
	a.clearSelections()
	if a.threadVisible {
		a.CloseThread()
	}
	// Don't open on toggle if no thread is loaded -- use Enter for that
}

func (a *App) CloseThread() {
	a.clearSelections()
	a.threadVisible = false
	a.statusbar.SetInThread(false)
	a.threadPanel.Clear()
	a.threadCompose.Blur()
	// Drop dedup state so a future activation re-opens this thread.
	a.lastOpenedChannelID = ""
	a.lastOpenedThreadTS = ""
	if a.focusedPanel == PanelThread {
		a.focusedPanel = PanelMessages
	}
}

// openSelectedThreadCmd updates UI state for whichever row the threadsview
// has highlighted (so the right thread panel shows the parent immediately),
// then schedules the network fetch.
//
// When debounce is true (j/k key handlers), the fetch is delayed by
// openThreadDebounceDelay and coalesced via pendingThreadFetchGen so a
// held-j burst produces exactly one HTTP call. When debounce is false
// (activation, list reload, G jump), the fetch fires immediately so
// thread content lands without artificial latency.
//
// No-op if the list is empty, no thread fetcher is wired, OR the selected
// thread is already the one open in the right panel (dedup: avoids
// hammering the Slack API and clobbering an in-progress read on every j/k
// press or list reload).
func (a *App) openSelectedThreadCmd(debounce bool) tea.Cmd {
	sum, ok := a.threadsView.SelectedSummary()
	if !ok {
		return nil
	}
	if sum.ChannelID == a.lastOpenedChannelID && sum.ThreadTS == a.lastOpenedThreadTS {
		return nil
	}
	a.lastOpenedChannelID = sum.ChannelID
	a.lastOpenedThreadTS = sum.ThreadTS
	a.threadVisible = true
	a.statusbar.SetInThread(true)
	parent := messages.MessageItem{
		TS:       sum.ParentTS,
		UserID:   sum.ParentUserID,
		UserName: a.userNameFor(sum.ParentUserID),
		Text:     sum.ParentText,
		ThreadTS: sum.ThreadTS,
	}
	a.threadPanel.SetThread(parent, nil, sum.ChannelID, sum.ThreadTS)
	a.threadCompose.SetChannel("thread")
	// Snapshot the thread's own last-read cursor BEFORE the local mark-
	// read flips below, so the "── new ──" landmark in the thread panel
	// reflects what the user had actually seen prior to opening this
	// thread.
	a.applyThreadUnreadBoundary(sum.ChannelID, sum.ThreadTS)
	// Local mark-as-read for the threads list: opening a thread should
	// clear its unread flag in the threads-view list and the sidebar
	// badge. This is presentation-only — it does not call Slack's
	// conversations.mark or advance the parent channel's last_read_ts.
	if a.threadsView.MarkSelectedRead() {
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
	}
	threads := a.threads
	chID, threadTS := sum.ChannelID, sum.ThreadTS
	tChID := ids.ChannelID(chID)
	tThreadTS := ids.ThreadTS(threadTS)
	if !debounce {
		var batch []tea.Cmd
		if cached := threads.CacheRead(tChID, tThreadTS); len(cached) > 1 {
			replies := cached[1:] // strip parent; reducer expects replies-only
			batch = append(batch, func() tea.Msg {
				return ThreadRepliesLoadedMsg{ThreadTS: threadTS, Replies: replies}
			})
		}
		batch = append(batch, func() tea.Msg { return threads.Fetch(tChID, tThreadTS) })
		return tea.Batch(batch...)
	}
	a.pendingThreadFetchGen++
	gen := a.pendingThreadFetchGen
	return tea.Tick(openThreadDebounceDelay, func(time.Time) tea.Msg {
		return threadFetchDebounceMsg{channelID: chID, threadTS: threadTS, gen: gen}
	})
}

// applyThreadUnreadBoundary tells the thread panel where the unread
// boundary is for (channelID, threadTS) so it can render a "── new ──"
// landmark before the first reply the user hasn't seen. Sourced from
// the thread's own cursor in thread_subscriptions — the parent
// channel's cursor is stale for threads, since plain replies never
// advance it. No-op when no last-read fetcher is wired (e.g. in tests).
func (a *App) applyThreadUnreadBoundary(channelID, threadTS string) {
	if channelID == "" || threadTS == "" {
		return
	}
	a.threadPanel.SetUnreadBoundary(a.threads.ThreadLastRead(
		ids.ChannelID(channelID), ids.ThreadTS(threadTS)))
}

// scheduleThreadsDirty returns a tea.Cmd that fires a ThreadsListDirtyMsg
// for the active workspace after the configured debounce interval. Returns
// nil when no workspace is active — without an activeTeamID the dirty
// handler would just drop the message anyway.
//
// The tick does not itself coalesce anything: tea.Tick arms a one-shot
// timer per call, so a burst of thread replies arms a timer each and
// sends a dirty message each. Collapsing those into a single
// ListSubscribedThreads query is the receiving arm's job in
// reduceThreads, where the dirty messages the WS handlers send are
// collapsed with them.
//
// Which leaves this tick redundant, and additive with the one the
// receiving arm arms: a reply's list re-query now lands roughly two
// debounce intervals after the reply. Deliberately left in place when
// the receive-side window was added — it predates that window and has
// two callers, so removing it is a separate change. Doing so would
// also improve coalescing slightly, by letting a reply's dirty join a
// window already open instead of arriving after it closed.
func (a *App) scheduleThreadsDirty() tea.Cmd {
	if a.activeTeamID == "" {
		return nil
	}
	team := a.activeTeamID
	d := a.threadsDirtyDebounce
	if d == 0 {
		d = defaultThreadsDirtyDebounce
	}
	return tea.Tick(d, func(time.Time) tea.Msg {
		return ThreadsListDirtyMsg{TeamID: team}
	})
}

// recordChannelMark stages a channel read-cursor advance. Newest-wins:
// an older ts for the same channel is ignored, so out-of-order arrivals
// can never roll the cursor backward. Slack timestamps are decimal
// strings of equal shape, so the string comparison orders them
// correctly without parsing.
func (a *App) recordChannelMark(channelID, ts string) {
	if channelID == "" || ts == "" {
		return
	}
	if a.pendingChannelMark.channelID == channelID && a.pendingChannelMark.ts >= ts {
		return
	}
	a.pendingChannelMark = pendingChannelMarkState{channelID: channelID, ts: ts}
}

// recordThreadMark stages a thread read-cursor advance. Newest-wins for
// the same (channel, thread); a different thread replaces the slot.
func (a *App) recordThreadMark(channelID, threadTS, ts string) {
	if channelID == "" || threadTS == "" || ts == "" {
		return
	}
	if a.pendingThreadMark.channelID == channelID &&
		a.pendingThreadMark.threadTS == threadTS &&
		a.pendingThreadMark.ts >= ts {
		return
	}
	a.pendingThreadMark = pendingThreadMarkState{channelID: channelID, threadTS: threadTS, ts: ts}
}

// autoMarkArmed reports whether arrival-driven read-marking may fire.
// It gates scheduleMarkFlush only: arrivals still stage their slots,
// and reduceFocus's tea.FocusMsg catch-up still flushes them, because
// reaching that arm is itself proof that focus reporting works.
//
// Outside tmux the assume-focused default of terminalFocused stands. A
// terminal that never reports focus is indistinguishable there from one
// that never loses it, and that risk is accepted for the common case.
//
// Inside tmux that default is not safe on its own: tmux forwards focus
// events only when `set -g focus-events on`, which defaults off, so
// such a user never produces a BlurMsg, terminalFocused stays true for
// the whole session, and slk would advance Slack's read cursor while
// the pane sat in the background. Requiring one observed focus event
// proves reporting is wired up. With the setting on, the first focus
// change arms this; without it slk never auto-marks and keeps its
// pre-branch behavior of marking read on channel entry only.
func (a *App) autoMarkArmed() bool {
	return !a.inTmux || a.focusEverReported
}

// scheduleMarkFlush returns a tick that flushes the pending marks after
// the debounce interval, or nil when nothing is staged, a tick is
// already in flight, the terminal is blurred, or auto-marking is not
// yet armed (see autoMarkArmed).
//
// The markFlushScheduled arm is what caps the mark rate: while a tick
// is in flight, further arrivals only update the slots. Drop it and a
// stream arriving faster than the debounce issues one
// conversations.mark per message.
//
// Blurred slots stay staged and flush on the next FocusMsg instead, so
// no timer is armed while the user is away.
func (a *App) scheduleMarkFlush() tea.Cmd {
	if a.markFlushScheduled || !a.terminalFocused || !a.autoMarkArmed() {
		return nil
	}
	if a.pendingChannelMark.ts == "" && a.pendingThreadMark.ts == "" {
		return nil
	}
	a.markFlushScheduled = true
	d := a.markFlushDebounce
	if d == 0 {
		d = defaultMarkFlushDebounce
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return markFlushMsg{} })
}

// flushPendingMarks clears both slots and returns the commands that
// issue their marks. Clearing before issuing is deliberate: a mark
// Slack rejects is not retried here, it is reconciled by the next
// arrival, the next channel entry, or reconnect sync. Retrying a
// rejected mark risks hammering a rate-limited endpoint.
//
// Neither leg of this moves an on-screen "── new ──" divider.
// ChannelService.MarkRead yields ChannelMarkedReadMsg, whose reducer
// arm only calls notifyReadStateChanged and never touches
// SetLastReadTS. Slack then broadcasts the channel mark back as a
// channel_marked event, which DOES reach SetLastReadTS via
// applyChannelMark — hence the selfMarks.record below, which is what
// makes applyChannelMarkEcho recognise and skip its own echo. The
// thread leg is symmetric on both counts: threads.Mark's
// ThreadMarkedLocalMsg arm applies list state only, and the leg records
// into selfThreadMarks so applyThreadMarkEcho skips the thread_marked
// broadcast of the same mark. The divider recomputes on the next
// channel entry.
func (a *App) flushPendingMarks() tea.Cmd {
	var cmds []tea.Cmd
	if pc := a.pendingChannelMark; pc.ts != "" {
		a.pendingChannelMark = pendingChannelMarkState{}
		channels := a.channels
		chID := ids.ChannelID(pc.channelID)
		ts := ids.MessageTS(pc.ts)
		a.selfMarks.record(selfMarkKey{channelID: pc.channelID, ts: pc.ts})
		cmds = append(cmds, func() tea.Msg { return channels.MarkRead(chID, ts) })
	}
	if pt := a.pendingThreadMark; pt.ts != "" {
		a.pendingThreadMark = pendingThreadMarkState{}
		if c := a.threads.Mark(
			ids.ChannelID(pt.channelID),
			ids.ThreadTS(pt.threadTS),
			ids.MessageTS(pt.ts),
		); c != nil {
			// Recorded before the cmd runs, symmetrically with the
			// channel leg above: Mark only builds the cmd, so the mark
			// has not been issued yet and this record cannot lose the
			// race against Slack's thread_marked broadcast. A nil cmd
			// means no mark, hence no echo to suppress.
			a.selfThreadMarks.record(selfMarkKey{
				channelID: pt.channelID, threadTS: pt.threadTS, ts: pt.ts,
			})
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// userNameFor returns the display name for a Slack user ID, falling back
// to the raw ID when the names map has no entry. Returns empty string for
// an empty userID.
func (a *App) userNameFor(userID string) string {
	if userID == "" {
		return ""
	}
	if n, ok := a.userNames[userID]; ok && n != "" {
		return n
	}
	return userID
}

// SetLoadingWorkspaces seeds the startup overlay. Called from
// cmd/slk/main.go at program start. Delegates to workspaceBootstrap;
// kept as an App method for backwards-compatible wiring.
func (a *App) SetLoadingWorkspaces(names []string) {
	a.bootstrap.SetWorkspaces(names)
}

// spinnerGlyph returns the current spinner character for both the
// bootstrap overlay and the messages-pane in-channel spinner. Sourced
// from styles.SpinnerChars indexed by the shared spinnerFrame counter.
func (a *App) spinnerGlyph() string {
	return string(styles.SpinnerChars[a.spinnerFrame])
}

// SetInitialLastReadTS sets the last read timestamp for the initial channel load.
func (a *App) SetInitialLastReadTS(ts string) {
	a.messagepane.SetLastReadTS(ts)
}

// Setters for external use (wiring services)
func (a *App) SetWorkspaces(items []workspace.WorkspaceItem) {
	a.workspaceRail.SetItems(items)
	a.workspaceRail.RefreshUnreads()
	a.workspaceItems = items
	// Update workspace finder items
	var finderItems []workspacefinder.Item
	for _, ws := range items {
		finderItems = append(finderItems, workspacefinder.Item{
			ID:       ws.ID,
			Name:     ws.Name,
			Initials: ws.Initials,
		})
	}
	a.workspaceFinder.SetItems(finderItems)
}

// SetChannels updates the sidebar's channel list, pushes the same set
// into both compose boxes for #-channel autocomplete, and seeds the
// renderers' channel-id -> name map so inbound <#CHANNELID> mentions
// resolve to the user-facing name. Centralizing these updates ensures
// the picker's channel set, the sidebar, and the renderer's resolution
// map never drift from each other (e.g., after a workspace switch, a
// channel join, or a display-name resolution for a DM).
func (a *App) SetChannels(items []sidebar.ChannelItem) {
	a.sidebar.SetItems(items)
	picks := make([]channelpicker.Channel, 0, len(items))
	names := make(map[string]string, len(items))
	for _, ch := range items {
		// Skip entries with empty names (defensive -- they'd never
		// match a typed query and would clutter the empty-query view).
		if ch.Name == "" {
			continue
		}
		picks = append(picks, channelpicker.Channel{
			ID:   ch.ID,
			Name: ch.Name,
			Type: ch.Type,
		})
		names[ch.ID] = ch.Name
	}
	a.compose.SetChannels(picks)
	a.threadCompose.SetChannels(picks)
	a.channelNames = names
	for _, m := range a.allWinModels() {
		m.SetChannelNames(names)
	}
	a.threadPanel.SetChannelNames(names)
	a.threadsView.SetChannelNames(names)
}

// SetChannelService wires the App's ChannelService collaborator
// (Slack channels API + local cache + session bookkeeping). Build
// one via NewChannelService from a ChannelServiceFuncs bundle.
func (a *App) SetChannelService(s ChannelService) {
	if s == nil {
		s = noopChannelService
	}
	a.channels = s
}

// SetSearchService injects the search backend (wired by cmd/slk).
// Build one via NewSearchService from a SearchServiceFuncs bundle.
func (a *App) SetSearchService(s SearchService) {
	if s == nil {
		s = noopSearchService
	}
	a.searchSvc = s
}

// clearActiveSearch removes in-channel search state: highlights,
// status segment, prompt buffer. It also invalidates any in-flight
// query by bumping the generation counter. Always runs unguarded:
// the status segment can be set without active state (e.g. the
// "no matches" text), and the Set* calls below are no-ops when
// already clear.
func (a *App) clearActiveSearch() {
	a.search = nil
	a.searchInput = ""
	a.searchGen++
	a.messagepane.SetSearchTerms(nil)
	a.statusbar.SetSearch("")
}

// SetMessageService wires the App's MessageService collaborator
// (send / edit / delete / mark-unread / permalink). Build one via
// NewMessageService from a MessageServiceFuncs bundle.
func (a *App) SetMessageService(s MessageService) {
	if s == nil {
		s = noopMessageService
	}
	a.messageSvc = s
}

// SetUploader wires the upload callback used by Ctrl+V smart-paste
// when the user submits with attachments.
func (a *App) SetUploader(fn UploadFunc) {
	a.uploader = fn
}

// SetClipboardAvailable reports whether native clipboard reads initialized
// successfully. It controls Ctrl+V smart-paste only; OSC 52 writes remain
// available in headless and CGO-free environments.
func (a *App) SetClipboardAvailable(ok bool) {
	a.clipboardAvailable = ok
}

// SetClipboardReader replaces the clipboard read function. Used by
// tests to inject canned clipboard contents. Pass nil to restore
// the default real clipboard reader.
func (a *App) SetClipboardReader(fn clipboardReader) {
	if fn == nil {
		a.clipboardRead = defaultClipboardReader
		return
	}
	a.clipboardRead = fn
}

// SetClipboardWriter replaces the OSC 52 command factory. Used by tests to
// capture copied text. Pass nil to restore tea.SetClipboard.
func (a *App) SetClipboardWriter(fn clipboardWriter) {
	if fn == nil {
		a.clipboardWrite = defaultClipboardWriter
		return
	}
	a.clipboardWrite = fn
}

// SetThreadService wires the App's ThreadService collaborator
// (fetch / mark / reply / list-fetch + parent-channel last-read).
// Build one via NewThreadService from a ThreadServiceFuncs bundle.
func (a *App) SetThreadService(s ThreadService) {
	if s == nil {
		s = noopThreadService
	}
	a.threads = s
}

// SetReadStateReader installs a callback the sidebar (and any future
// readers) will call at render time to fetch per-channel read state.
// Must be set before the first render for unread dots to appear.
func (a *App) SetReadStateReader(f func() map[string]cache.ReadState) {
	a.sidebar.SetReadStateReader(f)
}

// SetWorkspaceUnreadReader installs the callback the workspace rail
// uses on RefreshUnreads to learn which workspaces have at least one
// channel with has_unread=true.
func (a *App) SetWorkspaceUnreadReader(f func() []string) {
	a.workspaceRail.SetUnreadReader(f)
}

// SetStatusReporter installs the StatusReportFunc invoked on every unread-state
// change to mirror slk's unread state onto an external surface (config:
// notifications.status_command).
func (a *App) SetStatusReporter(fn StatusReportFunc) {
	a.statusReport = fn
}

func (a *App) SetChannelFinderItems(items []channelfinder.Item) {
	a.channelFinder.SetItems(items)
}

// SetAvatarFunc sets the function used to get rendered avatars for messages.
func (a *App) SetAvatarFunc(fn messages.AvatarFunc) {
	a.avatarFn = fn
	for _, m := range a.allWinModels() {
		m.SetAvatarFunc(fn)
	}
	a.threadPanel.SetAvatarFunc(fn)
}

// SetColoredUsernames enables or disables deterministic per-user coloring
// of usernames across all message panes and the thread panel.
func (a *App) SetColoredUsernames(enabled bool) {
	for _, m := range a.allWinModels() {
		m.SetColoredUsernames(enabled)
	}
	a.threadPanel.SetColoredUsernames(enabled)
}

// SetImageContext configures the inline-image rendering pipeline on the
// messages pane. Should be called once at startup, before the first
// View(). Pass a zero-valued ImageContext to disable inline rendering.
func (a *App) SetImageContext(ctx imgrender.ImageContext) {
	a.imageCtx = ctx
	for _, m := range a.allWinModels() {
		m.SetImageContext(ctx)
	}
	a.threadPanel.SetImageContext(ctx)
}

// SetSixelFrameStore wires the shared frame store used to correlate
// every published sixel frame with the Bubble Tea frame that flushes.
// The store is created in main and shared with FrameOutput; without it
// the App still renders but publishes nothing.
func (a *App) SetSixelFrameStore(frames *imgpkg.SixelFrameStore) {
	a.sixelFrames = frames
}

// SetEmojiContext forwards the emoji rendering context to both the
// messages pane and the thread pane. They each hold their own copy
// because they have independent render caches and call paths.
// Subsequent CustomEmojisLoadedMsg dispatches update the customs map
// via App.SetCustomEmoji which calls back into both panes' emojiCtx.
//
// Phase 8 extends this to the picker; Phase 9 to autocomplete.
func (a *App) SetEmojiContext(ctx messages.EmojiContext) {
	a.emojiCtx = ctx
	for _, m := range a.allWinModels() {
		m.SetEmojiContext(ctx)
	}
	a.threadPanel.SetEmojiContext(thread.EmojiContext{
		PlaceCtx: ctx.PlaceCtx,
		Cells:    ctx.Cells,
		Customs:  ctx.Customs,
	})
	a.reactionPicker.SetEmojiContext(reactionpicker.EmojiContext{
		PlaceCtx: ctx.PlaceCtx,
		Cells:    ctx.Cells,
		Customs:  ctx.Customs,
	})
	a.compose.SetEmojiContext(emojipicker.EmojiContext{
		PlaceCtx: ctx.PlaceCtx,
		Cells:    ctx.Cells,
		Customs:  ctx.Customs,
	})
	a.threadCompose.SetEmojiContext(emojipicker.EmojiContext{
		PlaceCtx: ctx.PlaceCtx,
		Cells:    ctx.Cells,
		Customs:  ctx.Customs,
	})
}

// SetImageFetcher records the image fetcher so the preview overlay can
// fetch large thumbs on demand. Called once at startup from main.go.
func (a *App) SetImageFetcher(f *imgpkg.Fetcher) {
	a.imageFetcher = f
}

// SetImageProtocol records the active terminal image protocol detected
// at startup so the preview overlay can render itself with the right
// renderer (kitty / sixel / halfblock / off).
func (a *App) SetImageProtocol(p imgpkg.Protocol) {
	a.imgProtocol = p
}

// SetFileDownloader wires the file attachment downloader used by the
// `d` keybinding.
func (a *App) SetFileDownloader(d *filedl.Downloader) {
	a.fileDownloader = d
}

// openImagePreviewCmd looks up the (channel, ts, attIdx) attachment in
// the active messages pane, picks the largest available thumb, and
// returns a tea.Cmd that asynchronously fetches it; on completion the
// cmd dispatches a previewLoadedMsg (or previewErrorMsg) which Update
// turns into an open Preview overlay. Returns nil for any condition
// that makes the open a no-op (no fetcher, attachment missing, no
// thumbs, mismatched channel, etc.).
func (a *App) openImagePreviewCmd(channel, ts string, attIdx int) tea.Cmd {
	return a.previewFetchCmd(channel, ts, attIdx, false)
}

// cycleImagePreviewCmd loads a sibling image (delta = -1 for prev,
// +1 for next; wraps around) into the existing preview overlay. The
// resulting previewLoadedMsg has isCycle = true so the Update arm
// swaps the image rather than constructing a new overlay. No-op when
// the active preview has only one sibling.
func (a *App) cycleImagePreviewCmd(channel, ts string, currentIdx, delta int) tea.Cmd {
	if a.imageFetcher == nil {
		return nil
	}
	msgItem, ok := a.findMessageInActiveChannel(channel, ts)
	if !ok {
		return nil
	}
	imageIdxs := imageAttachmentIndices(msgItem.Attachments)
	if len(imageIdxs) <= 1 {
		return nil
	}
	// Find currentIdx's position within the image-only list and step.
	pos := -1
	for i, idx := range imageIdxs {
		if idx == currentIdx {
			pos = i
			break
		}
	}
	if pos < 0 {
		// currentIdx isn't an image attachment? Treat as start.
		pos = 0
	}
	pos = (pos + delta + len(imageIdxs)) % len(imageIdxs)
	nextAttIdx := imageIdxs[pos]
	return a.previewFetchCmd(channel, ts, nextAttIdx, true)
}

// previewFetchCmd is the shared helper for opening / cycling. cycle
// determines whether the resulting previewLoadedMsg is treated as a
// fresh open or an in-place image swap.
func (a *App) previewFetchCmd(channel, ts string, attIdx int, cycle bool) tea.Cmd {
	if a.imageFetcher == nil {
		return nil
	}
	msgItem, ok := a.findMessageInActiveChannel(channel, ts)
	if !ok {
		return nil
	}
	if attIdx < 0 || attIdx >= len(msgItem.Attachments) {
		return nil
	}
	att := msgItem.Attachments[attIdx]
	if att.FileID == "" || len(att.Thumbs) == 0 {
		return nil
	}

	// Pick the largest available thumb for preview quality.
	var largest messages.ThumbSpec
	for _, t := range att.Thumbs {
		if max(t.W, t.H) > max(largest.W, largest.H) {
			largest = t
		}
	}
	if largest.URL == "" {
		return nil
	}

	// Compute sibling-count / sibling-index over IMAGE attachments only,
	// so the (i/N) caption ignores non-image siblings (e.g. PDFs).
	imageIdxs := imageAttachmentIndices(msgItem.Attachments)
	sibCount := len(imageIdxs)
	sibIndex := 0
	for i, idx := range imageIdxs {
		if idx == attIdx {
			sibIndex = i
			break
		}
	}

	fetcher := a.imageFetcher
	name := att.Name
	fileID := att.FileID
	url := largest.URL
	target := image.Pt(largest.W, largest.H)
	return func() tea.Msg {
		res, err := fetcher.Fetch(context.Background(), imgpkg.FetchRequest{
			Key:    fileID + "-preview",
			URL:    url,
			Target: target,
		})
		if err != nil {
			return previewErrorMsg{Err: err}
		}
		return previewLoadedMsg{
			Name:         name,
			FileID:       fileID,
			Img:          res.Img,
			Path:         res.Source,
			SiblingCount: sibCount,
			SiblingIndex: sibIndex,
			isCycle:      cycle,
		}
	}
}

// previewMetaForOpen returns the (name, sibCount, sibIndex) needed to
// construct a loading-state Preview for the given (channel, ts, attIdx).
// Used to open the overlay synchronously, before the fetch completes,
// so the user sees immediate feedback.
//
// Returns ("", 1, 0) defaults for any miss; callers won't display the
// loading overlay in that case anyway because openImagePreviewCmd
// returns nil.
func (a *App) previewMetaForOpen(channel, ts string, attIdx int) (name string, sibCount, sibIndex int) {
	msgItem, ok := a.findMessageInActiveChannel(channel, ts)
	if !ok {
		return "", 1, 0
	}
	if attIdx < 0 || attIdx >= len(msgItem.Attachments) {
		return "", 1, 0
	}
	imageIdxs := imageAttachmentIndices(msgItem.Attachments)
	sibCount = len(imageIdxs)
	if sibCount == 0 {
		sibCount = 1
	}
	for i, idx := range imageIdxs {
		if idx == attIdx {
			sibIndex = i
			break
		}
	}
	return msgItem.Attachments[attIdx].Name, sibCount, sibIndex
}

// imageAttachmentIndices returns the indices into atts of attachments
// with Kind == "image" and a non-empty FileID. Order preserved.
func imageAttachmentIndices(atts []messages.Attachment) []int {
	out := make([]int, 0, len(atts))
	for i, a := range atts {
		if a.Kind == "image" && a.FileID != "" {
			out = append(out, i)
		}
	}
	return out
}

// findMessageInActiveChannel returns the MessageItem with the matching TS
// in the messages pane (channel) or thread panel, gated on the supplied
// channel ID matching either pane's active channel. Returns ok=false if
// nothing matches. Used by the preview-open path to resolve the
// attachment metadata for a click / `O` keystroke.
func (a *App) findMessageInActiveChannel(channel, ts string) (messages.MessageItem, bool) {
	if channel == a.activeChannelID {
		for _, m := range a.messagepane.Messages() {
			if m.TS == ts {
				return m, true
			}
		}
	}
	if a.threadVisible && channel == a.threadPanel.ChannelID() {
		if parent := a.threadPanel.ParentMsg(); parent.TS == ts {
			return parent, true
		}
		for _, r := range a.threadPanel.Replies() {
			if r.TS == ts {
				return r, true
			}
		}
	}
	return messages.MessageItem{}, false
}

// openInSystemViewerCmd asynchronously launches the OS-native image
// viewer for path. Uses xdg-open on Linux, open on macOS, and
// rundll32 on Windows. Errors are logged and otherwise silent — the
// overlay is already closed by the time this runs.
func openInSystemViewerCmd(path string) tea.Cmd {
	return func() tea.Msg {
		if path == "" {
			return nil
		}
		if err := launchOS(path); err != nil {
			log.Printf("system viewer launch failed: %v", err)
		}
		return nil
	}
}

// launchOS starts the platform's default handler for target (a URL or
// file path): open (macOS), rundll32 (Windows), xdg-open (Linux).
func launchOS(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// openURLCmd asynchronously launches the OS default browser for url.
// Same launcher matrix as openInSystemViewerCmd (xdg-open / open /
// rundll32). A failed launch surfaces a toast — unlike the image
// viewer, the user otherwise gets no feedback at all.
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if url == "" {
			return nil
		}
		if err := launchOS(url); err != nil {
			log.Printf("browser launch failed: %v", err)
			return ToastMsg{Text: "Failed to open link"}
		}
		return nil
	}
}

// downloadFileCmd downloads att to the temp download dir and opens it
// in the OS default app. Runs async; the user gets a toast either way.
func (a *App) downloadFileCmd(att messages.Attachment) tea.Cmd {
	return func() tea.Msg {
		if a.fileDownloader == nil {
			return ToastMsg{Text: "File downloads unavailable"}
		}
		path, err := a.fileDownloader.Download(context.Background(), att.DownloadURL, att.Name)
		if err != nil {
			log.Printf("file download failed: %v", err)
			return ToastMsg{Text: "Download failed: " + att.Name}
		}
		if err := launchOS(path); err != nil {
			log.Printf("file open failed: %v", err)
			return ToastMsg{Text: "Failed to open " + att.Name}
		}
		return ToastMsg{Text: "Downloaded " + att.Name}
	}
}

// SetUserNames passes the user ID -> display name map to the message pane for mention resolution.
func (a *App) SetUserNames(names map[string]string) {
	a.userNames = names
	a.threadsView.SetUserNames(names)
	for _, m := range a.allWinModels() {
		m.SetUserNames(names)
	}
	a.threadPanel.SetUserNames(names)

	// Build user list for mention picker
	users := make([]mentionpicker.User, 0, len(names))
	for id, displayName := range names {
		users = append(users, mentionpicker.User{
			ID:          id,
			DisplayName: displayName,
			Username:    "",
			IsExternal:  a.externalUsers[id],
		})
	}
	a.compose.SetUsers(users)
	a.threadCompose.SetUsers(users)
}

// SetUserGroups passes the active workspace's usergroup map to every
// surface that renders, previews, or composes usergroup mentions.
func (a *App) SetUserGroups(groups map[string]string) {
	a.userGroups = usergroups.Copy(groups)
	a.threadsView.SetUserGroups(a.userGroups)
	for _, m := range a.allWinModels() {
		m.SetUserGroups(a.userGroups)
	}
	a.threadPanel.SetUserGroups(a.userGroups)
	a.compose.SetUserGroups(a.userGroups)
	a.threadCompose.SetUserGroups(a.userGroups)
}

// seedNewMessagePicker snapshots the current workspace's user list
// into the new-message picker and configures the self-exclusion.
// Called when ModeNewMessage is entered so the modal always sees a
// fresh view of the workspace.
//
// User-list shape:
//   - DisplayName from a.userNames (the workspace display name map).
//   - Username falls back to DisplayName — there is no separate
//     userID->handle map on App today. The picker uses Username
//     only for filter matching, so this fallback degrades
//     gracefully: queries against the display name still hit.
//   - IsExternal from a.externalUsers.
//   - Recency is left at 0 (alphabetical order; recency wiring is a
//     follow-up that needs WorkspaceContext.LastVisitedByChannel
//     plumbed in).
//   - Self (a.currentUserID) is excluded both at slice-build time
//     and via SetCurrentUserID (belt-and-suspenders).
func (a *App) seedNewMessagePicker() {
	users := make([]newmessagepicker.User, 0, len(a.userNames))
	for id, name := range a.userNames {
		if id == a.currentUserID {
			continue
		}
		users = append(users, newmessagepicker.User{
			ID:          id,
			DisplayName: name,
			Username:    name, // see scoping note; replaceable when a handle map lands
			IsExternal:  a.externalUsers[id],
		})
	}

	a.newMessagePicker.SetCurrentUserID(a.currentUserID)
	a.newMessagePicker.SetUsers(users)
}

// SetExternalUsers replaces the set of user IDs known to be Slack
// Connect / shared-channel guests. Sticky: subsequent SetUserNames
// calls consult this map when building mention-picker entries.
// Pass an empty map (or nil) to clear.
func (a *App) SetExternalUsers(externalIDs map[string]bool) {
	if externalIDs == nil {
		externalIDs = map[string]bool{}
	}
	a.externalUsers = externalIDs
	if len(a.userNames) > 0 {
		a.SetUserNames(a.userNames)
	}
}

// SetChannelMembership forwards channel-member IDs to both compose
// pickers. Called by main.go from the membership.Manager.
func (a *App) SetChannelMembership(channelID string, memberIDs []string) {
	a.compose.SetChannelMembership(channelID, memberIDs)
	a.threadCompose.SetChannelMembership(channelID, memberIDs)
}

// SetCustomEmoji rebuilds the emoji entry list (built-ins + the active
// workspace's customs) and pushes it into both compose boxes. Also
// updates the messages pane's emoji-image context so newly-known
// custom emoji URLs become resolvable on the next render.
func (a *App) SetCustomEmoji(customs map[string]string) {
	entries := emoji.BuildEntries(customs)
	a.compose.SetEmojiEntries(entries)
	a.threadCompose.SetEmojiEntries(entries)
	if a.reactionPicker != nil {
		a.reactionPicker.SetCustomEmoji(customs)
	}
	// Update all panes' emoji-image context so newly-known custom
	// emoji URLs become resolvable on the next render.
	a.emojiCustoms = customs
	for _, m := range a.allWinModels() {
		m.SetEmojiCustoms(customs)
	}
	a.threadPanel.SetEmojiCustoms(customs)
	a.reactionPicker.SetEmojiCustoms(customs)
	// Compose autocomplete dropdowns (main + thread) also need the
	// customs map for View()-time URL resolution; without this, custom
	// emoji rows fall back to the placeholder glyph. See
	// emojipicker.Model.SetEmojiCustoms for context.
	a.compose.SetEmojiCustoms(customs)
	a.threadCompose.SetEmojiCustoms(customs)
}

// SetInitialChannel sets the active channel and its messages before the TUI starts.
func (a *App) SetInitialChannel(channelID, channelName string, msgs []messages.MessageItem) {
	a.activeChannelID = channelID
	a.messagepane.SetChannel(channelName, "")
	a.messagepane.SetMessages(msgs)
	a.compose.SetChannel(channelName)
	a.statusbar.SetChannel(channelName)
}

// SetReactionService wires the App's ReactionService collaborator.
// The supplied service handles both reaction add/remove and frecent
// emoji bookkeeping; build one via NewReactionService from
// internal/ui/services.go.
func (a *App) SetReactionService(r ReactionService) {
	if r == nil {
		r = noopReactionService
	}
	a.reactions = r
}

func (a *App) SetCurrentUserID(userID string) {
	a.currentUserID = userID
	a.threadsView.SetSelfUserID(userID)
	a.messagepane.SetCurrentUser(userID)
	a.threadPanel.SetCurrentUser(userID)
}

// SetNowTimestampFormatter wires the formatter used to render the
// Timestamp field of an optimistic instant-display message. main.go
// passes a closure that uses cfg.Appearance.TimestampFormat so the
// placeholder's time renders identically to the surrounding messages.
func (a *App) SetNowTimestampFormatter(fn func() string) {
	a.nowTimestampFormatter = fn
}

// nowFormatted returns the current time rendered via the configured
// formatter, falling back to a sensible default when none is wired
// (e.g. in tests).
func (a *App) nowFormatted() string {
	if a.nowTimestampFormatter != nil {
		return a.nowTimestampFormatter()
	}
	return time.Now().Format("3:04 PM")
}

// ActiveChannelID returns the ID of the currently viewed channel.
func (a *App) ActiveChannelID() string {
	return a.activeChannelID
}

// SetWorkspaceSwitcher sets the callback used to switch workspaces.
func (a *App) SetWorkspaceSwitcher(fn SwitchWorkspaceFunc) {
	a.workspaceSwitcher = fn
}

// SetThemeItems sets the available themes for the switcher.
func (a *App) SetThemeItems(names []string) {
	a.themeSwitcher.SetItems(names)
}

// activeTeamName returns the human-readable name of the active workspace,
// falling back to the team ID if no name is known. Used as a label in the
// theme picker header.
func (a *App) activeTeamName() string {
	for _, w := range a.workspaceItems {
		if w.ID == a.activeTeamID {
			if w.Name != "" {
				return w.Name
			}
			return w.ID
		}
	}
	if a.activeTeamID != "" {
		return a.activeTeamID
	}
	return "this workspace"
}

// activeWorkspaceDomain returns the slack.com subdomain of the active
// workspace, or "" when unknown (link router then falls back to the
// browser for all slack.com permalinks).
func (a *App) activeWorkspaceDomain() string {
	return a.workspaceDomains[a.activeTeamID]
}

// workspaceNameForActive returns the display name of the active workspace
// (empty string if none). Used as the presence menu header.
func (a *App) workspaceNameForActive() string {
	for _, ws := range a.workspaceItems {
		if ws.ID == a.activeTeamID {
			return ws.Name
		}
	}
	return ""
}

// SetThemeSaver sets the callback for saving the theme selection. The
// callback receives the chosen theme name and the scope (workspace vs.
// global) so the implementation can route to the correct save target.
func (a *App) SetThemeSaver(fn func(name string, scope themeswitcher.ThemeScope)) {
	a.themeSaveFn = fn
}

// SetWidthSaver sets the callback for persisting the sidebar width.
// The callback receives the current width after a resize.
func (a *App) SetWidthSaver(fn func(width int)) {
	a.widthSaveFn = fn
}

// SetStatusSetter registers a callback the App invokes when the user picks
// a status action from the presence menu. The callback runs the appropriate
// Slack API call (typically asynchronously) for the active workspace.
func (a *App) SetStatusSetter(fn func(action presencemenu.Action, snoozeMinutes int)) {
	a.setStatusFn = fn
}

// SetThemeOverrides stores the config theme overrides for applying on switch.
func (a *App) SetThemeOverrides(overrides config.Theme) {
	a.themeOverrides = overrides
}

// SetTypingEnabled controls whether typing indicators are shown and sent.
func (a *App) SetTypingEnabled(enabled bool) {
	a.typing.SetEnabled(enabled)
}

// SetHelpFooter sets the attribution line shown at the bottom of the
// help modal (e.g. the version + author credit). Pass "" to clear it.
func (a *App) SetHelpFooter(s string) {
	a.help.SetFooter(s)
}

// SetMouseWheelLines configures the number of lines the viewport scrolls per
// mouse-wheel notch. Values < 1 are coerced to 1 to guarantee scroll progress.
func (a *App) SetMouseWheelLines(n int) {
	if n < 1 {
		n = 1
	}
	a.mouseWheelLines = n
}

// SetSidebarStaleThreshold configures auto-hiding of inactive
// channels in the sidebar. d is the maximum age (since LastReadTS)
// before a channel is hidden; pass 0 to disable.
func (a *App) SetSidebarStaleThreshold(d time.Duration) {
	a.sidebar.SetStaleThreshold(d)
}

// SetTypingSender sets the callback for sending typing indicators.
func (a *App) SetTypingSender(fn TypingSendFunc) {
	a.typingOut.SetSender(fn)
}

// renderTypingLine returns the styled typing indicator for the current
// channel, or an empty string if no one is typing. Stays on App because
// it pulls in messagepane name resolution and styles. State and
// formatting live in internal/ui/typing.go.
func (a *App) renderTypingLine() string {
	if !a.typing.Enabled() {
		return ""
	}
	userIDs := a.typing.UsersExcluding(a.activeChannelID, a.currentUserID)
	if len(userIDs) == 0 {
		return ""
	}
	// Resolve user IDs to display names
	names := make([]string, 0, len(userIDs))
	for _, uid := range userIDs {
		name := a.messagepane.ResolveUserName(uid)
		if name == "" {
			name = uid
		}
		names = append(names, name)
	}
	return styles.TypingIndicator.Render(typingIndicatorText(names))
}

func (a *App) View() tea.View {
	if v, handled := a.renderEarlyFallback(); handled {
		return a.finalizeSixelView(v, nil)
	}

	// Perf instrumentation: wall-clock the main View() path so we can
	// correlate user-visible latency (i / arrow keys / thread open-close)
	// with the cache rebuilds logged from messages.Model.View and
	// thread.Model.View. SLK_DEBUG=1 only; zero cost otherwise.
	var viewPerfStart time.Time
	if debuglog.Enabled() {
		viewPerfStart = time.Now()
	}

	// Resolve per-pane widths/borders. Compute stores horizontal bands
	// for subsequent mouse hit-testing (panelAt) and surfaces a
	// ThreadAutoHidden flag when the available width can't fit the
	// thread pane at its minimum.
	frame := a.layout.Compute(a.width, a.height, a.workspaceRail.Width(), a.sidebar.Width(), a.sidebarVisible, a.threadVisible)
	if frame.ThreadAutoHidden {
		a.threadVisible = false
		if a.focusedPanel == PanelThread {
			a.focusedPanel = PanelMessages
		}
	}
	themeVer := styles.Version()

	// If the full-screen image preview is open, the messages and
	// thread regions are skipped (renderMessagesRegion returns ""
	// and the thread-region gate below short-circuits); the
	// preview panel takes their place as a single overlay
	// spanning the combined width. Rail, sidebar, and status row
	// still render normally so the user can see context.
	previewActive := a.preview.Active()

	var panels []string
	panels = append(panels, a.renderRail(frame.RailWidth, frame.ContentHeight, themeVer))
	if a.sidebarVisible {
		panels = append(panels, a.renderSidebar(frame.SidebarWidth, frame.SidebarBorder, frame.ContentHeight, themeVer))
	}
	if s := a.renderWindowsRegion(frame, themeVer, previewActive); s != "" {
		panels = append(panels, s)
	}
	if a.threadVisible && frame.ThreadWidth > 0 && !previewActive {
		panels = append(panels, a.renderThreadRegion(frame, themeVer))
	}
	if previewActive {
		panels = append(panels, a.renderPreviewPanel(frame))
	}

	status := a.renderStatusRow(frame.RailWidth, a.width-frame.RailWidth, themeVer)

	// Compositor memo (Stage A). Skip the JoinHorizontal/JoinVertical
	// re-composite when no overlay/preview is active and the panel
	// inputs are unchanged from the last frame. See the lastScreen
	// field doc on App for why this matters (View() runs per message).
	// Overlay/preview frames are never memoized: overlay content can
	// change without bumping any base-panel version, and the preview
	// panel is rendered fresh (uncached) each frame.
	canMemo := !previewActive && !a.overlayActive()
	var screen string
	memoHit := false
	if canMemo && a.screenMemoMatches(panels, status, a.width, a.height) {
		screen = a.lastScreen
		memoHit = true
	} else {
		// Composite the side-by-side panels. The panels are uniform-width
		// and all exactly frame.ContentHeight rows, so a row-wise concat
		// is byte-identical to lipgloss's JoinHorizontal but skips its
		// per-line grapheme width measurement (a large share of the
		// remaining scroll-frame cost at wide sizes). Fall back to lipgloss
		// if the fast path declines (e.g. a height-clamped pane).
		content, ok := joinPanelsHorizontal(panels, frame.ContentHeight)
		if !ok {
			content = lipgloss.JoinHorizontal(lipgloss.Top, panels...)
		}
		// Stack content over status without re-measuring every content
		// line (lipgloss.JoinVertical's getLines was the other large share
		// of the composite cost). stackContentStatus reproduces lipgloss's
		// left-align padding byte-for-byte; see its doc.
		screen = stackContentStatus(content, status)
		screen = a.applyOverlays(screen)
		screen = a.maybeWrapFinalScreen(screen)
		if canMemo {
			a.storeScreenMemo(panels, status, a.width, a.height, screen)
		} else {
			// Overlay/preview output is not memoizable; force the next
			// memoizable frame to recompute.
			a.lastScreenValid = false
		}
	}

	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// Ask the terminal to report focus gain/loss so reduceFocus can keep
	// a.terminalFocused current. Bubble Tea diffs this against the
	// previous frame and only emits the enable sequence on change.
	v.ReportFocus = true
	if debuglog.Enabled() {
		// panel: 0=workspace 1=sidebar 2=messages 3=thread
		// view:  0=channels 1=threads
		debuglog.Perf("App.View total=%s memo=%v w=%d h=%d panel=%d view=%d mode=%s thread=%v sidebar=%v preview=%v",
			time.Since(viewPerfStart), memoHit, a.width, a.height,
			int(a.focusedPanel), int(a.view), a.mode.String(),
			a.threadVisible, a.sidebarVisible, previewActive)
	}
	return a.finalizeSixelView(v, a.collectSixelPlacements(frame))
}

// collectSixelPlacements gathers the desired sixel placements for the
// current screen. The preview overlay replaces the messages/thread
// regions entirely when active, so it takes priority; otherwise every
// visible window leaf contributes its own placements, mapped through
// its own rectangle (split windows sit at their own origins, not the
// unsplit messages-pane origin). The thread pane still renders
// halfblock.
func (a *App) collectSixelPlacements(frame panelLayoutFrame) []imgpkg.SixelPlacement {
	if a.preview.Active() {
		want := make([]imgpkg.SixelPlacement, 0, 1)
		if ov := a.preview.Overlay(); ov != nil {
			if sp := ov.SixelPaint(); sp != nil {
				if pl, ok := a.absolutePreviewSixelPlacement(*sp); ok {
					want = append(want, pl)
				}
			}
		}
		return want
	}
	if a.view != ViewChannels {
		return nil
	}
	// The same bounds renderWindowsRegion uses, so leaf rectangles
	// match the panes actually drawn.
	bounds := wintree.Rect{
		X: 0,
		Y: 0,
		W: frame.MsgWidth + frame.MsgBorder,
		H: frame.ContentHeight,
	}
	rects := a.wins.ComputeRects(bounds)
	out := make([]imgpkg.SixelPlacement, 0, 2)
	for _, id := range a.wins.Leaves() {
		rect, ok := rects[id]
		model := a.winModels[id]
		if !ok || model == nil || rect.W < 1 || rect.H < 1 {
			continue
		}
		out = append(out, absoluteWindowSixelPlacements(
			model.SixelPlacements(), model.ChromeHeight(), rect, a.layout.SidebarEnd(),
		)...)
	}
	return out
}

// finalizeSixelView is the single exit point for every App.View path,
// including the early fallback. It:
//
//   - sets the real window title for non-sixel protocols;
//   - publishes an empty frame when no sixel surface is visible, so old
//     pixels erase;
//   - attaches each placement's guard from the complete screen string;
//   - publishes the frame with the themed erase background and the
//     pending resize force, then clears the force flag;
//   - marks the title with the frame ID so FrameOutput can correlate
//     the exact flushed Bubble Tea frame.
//
// It never writes to the terminal; painting happens later in
// FrameOutput after the text diff for the same frame.
func (a *App) finalizeSixelView(v tea.View, placements []imgpkg.SixelPlacement) tea.View {
	if a.imgProtocol != imgpkg.ProtoSixel || a.sixelFrames == nil {
		v.WindowTitle = a.windowTitle
		return v
	}
	for i := range placements {
		placements[i].Guard = frameGuard(v.Content, placements[i].Row-1, placements[i].Rows)
	}
	id := a.sixelFrames.Publish(imgpkg.SixelFrame{
		Placements: placements,
		EraseSGR:   messages.BgANSI(),
		Force:      a.forceSixelRepaint,
	})
	a.forceSixelRepaint = false
	v.WindowTitle = imgpkg.FrameTitle(a.windowTitle, id)
	return v
}

// screenMemoMatches reports whether the freshly-rendered panel strings
// and status row are identical to those that produced a.lastScreen, so
// the previously composited screen can be reused without re-joining.
// On an all-cache-hit frame the panel strings share a backing array
// with the stored ones, so each comparison short-circuits in O(1).
func (a *App) screenMemoMatches(panels []string, status string, w, h int) bool {
	if !a.lastScreenValid || a.lastScreenW != w || a.lastScreenH != h {
		return false
	}
	if a.lastStatus != status || len(panels) != len(a.lastPanels) {
		return false
	}
	for i := range panels {
		if panels[i] != a.lastPanels[i] {
			return false
		}
	}
	return true
}

// storeScreenMemo records the inputs and output of a successful
// (non-overlay, non-preview) composite for reuse by the next frame.
// Panel strings are immutable and mostly cache-shared, so we retain
// references rather than copying their contents; only the slice header
// is copied (the caller reuses its backing array across frames).
func (a *App) storeScreenMemo(panels []string, status string, w, h int, screen string) {
	if cap(a.lastPanels) >= len(panels) {
		a.lastPanels = a.lastPanels[:len(panels)]
	} else {
		a.lastPanels = make([]string, len(panels))
	}
	copy(a.lastPanels, panels)
	a.lastStatus = status
	a.lastScreen = screen
	a.lastScreenW = w
	a.lastScreenH = h
	a.lastScreenValid = true
}

// cancelEdit exits edit mode, restoring the stashed draft to its
// source compose. Safe to call when no edit is active (no-op).
func (a *App) cancelEdit() {
	if !a.editing.IsActive() {
		return
	}
	stashed := a.editing.StashedDraft()
	switch a.editing.Panel() {
	case PanelMessages:
		a.compose.SetValue(stashed)
		a.compose.SetPlaceholderOverride("")
	case PanelThread:
		a.threadCompose.SetValue(stashed)
		a.threadCompose.SetPlaceholderOverride("")
	}
	a.editing.Clear()
	a.SetMode(ModeNormal)
	a.compose.Blur()
	a.threadCompose.Blur()
}

// isOwnMessage returns whether the given message is owned by the
// current user. Bot/system messages and unauthenticated states fail.
func (a *App) isOwnMessage(m messages.MessageItem) bool {
	return a.currentUserID != "" && m.UserID == a.currentUserID
}

// selectedMessageContext returns the channel ID, message TS, text, owner
// user ID, and panel of the currently-selected message in the focused
// pane. Returns ok=false if nothing is selected or the focused panel is
// not a message-bearing pane.
func (a *App) selectedMessageContext() (channelID, ts, text, userID string, panel Panel, ok bool) {
	switch a.focusedPanel {
	case PanelMessages:
		msg, sel := a.messagepane.SelectedMessage()
		if !sel {
			return "", "", "", "", 0, false
		}
		return a.activeChannelID, msg.TS, msg.Text, msg.UserID, PanelMessages, true
	case PanelThread:
		reply := a.threadPanel.SelectedReply()
		if reply == nil {
			return "", "", "", "", 0, false
		}
		return a.threadPanel.ChannelID(), reply.TS, reply.Text, reply.UserID, PanelThread, true
	default:
		return "", "", "", "", 0, false
	}
}

const maxAttachmentSize = 10 * 1024 * 1024 // 10 MB cap

// submitWithAttachments dispatches the pending attachments + caption
// on the given compose to the configured uploader. It refuses if an
// edit is in progress (chat.update doesn't support file attachments)
// or if there's no active channel / no uploader configured. On
// dispatch, the compose's uploading flag is set so the UI can show
// progress; the actual UploadResultMsg arm in Update clears it.
func (a *App) submitWithAttachments(c *compose.Model) tea.Cmd {
	if a.editing.IsActive() {
		return a.uploadToastCmd("Cannot attach files to an edit (send a new message)", 3*time.Second)
	}
	attachments := c.Attachments()
	if len(attachments) == 0 {
		return nil
	}
	caption := strings.TrimSpace(c.Value())

	var channelID, threadTS string
	if c == &a.threadCompose {
		channelID = a.threadPanel.ChannelID()
		threadTS = a.threadPanel.ThreadTS()
	} else {
		channelID = a.activeChannelID
		threadTS = ""
	}
	if channelID == "" || a.uploader == nil {
		return a.uploadToastCmd("Cannot upload: no active channel", 2*time.Second)
	}

	c.SetUploading(true)
	cmds := []tea.Cmd{
		a.uploader(channelID, threadTS, caption, attachments),
		a.uploadToastCmd(fmt.Sprintf("Uploading 0/%d…", len(attachments)), 30*time.Second),
	}
	return tea.Batch(cmds...)
}

// smartPaste inspects the OS clipboard and dispatches:
//  1. PNG image bytes → attach as image with auto-generated filename.
//  2. Single-line file-path text → attach by path.
//  3. Anything else → insert text into the active compose.
//
// Returns a tea.Cmd that emits the appropriate status-bar toast.
// No-op if clipboard.Init() failed at startup.
func (a *App) smartPaste() tea.Cmd {
	if !a.clipboardAvailable {
		return nil
	}

	// Resolve the active compose pointer.
	target := &a.compose
	if a.focusedPanel == PanelThread && a.threadVisible {
		target = &a.threadCompose
	}

	textBytes := a.clipboardRead(clipboard.FmtText)
	if consumed, cmd := a.tryAttachFromClipboard(target, string(textBytes)); consumed {
		return cmd
	}

	// Text fallback — paste verbatim into the active compose.
	if len(textBytes) > 0 {
		target.SetValue(target.Value() + string(textBytes))
	}
	return nil
}

// tryAttachFromClipboard inspects the OS clipboard for an image and the
// supplied text for a file-path reference, attaching the first match
// to the given compose. Returns consumed=true if an attachment (or an
// explicit refusal toast) was produced; false if neither image nor
// path applied — in which case the caller should fall through to its
// own text-paste behavior.
//
// pathCandidate is the text source to test against resolveFilePath.
// For keystroke smart-paste this is the OS clipboard's text; for
// bracketed-paste this is the PasteMsg's payload.
func (a *App) tryAttachFromClipboard(target *compose.Model, pathCandidate string) (bool, tea.Cmd) {
	// 1. Image bytes from the OS clipboard.
	if imgBytes := a.clipboardRead(clipboard.FmtImage); len(imgBytes) > 0 {
		if int64(len(imgBytes)) > maxAttachmentSize {
			return true, a.uploadToastCmd(
				fmt.Sprintf("Image too large (%s > 10 MB limit)", humanSize(int64(len(imgBytes)))),
				3*time.Second,
			)
		}
		filename := "slk-paste-" + time.Now().Format("2006-01-02-15-04-05") + ".png"
		target.AddAttachment(compose.PendingAttachment{
			Filename: filename,
			Bytes:    imgBytes,
			Mime:     "image/png",
			Size:     int64(len(imgBytes)),
		})
		return true, a.uploadToastCmd(
			fmt.Sprintf("Attached: %s (%s)", filename, humanSize(int64(len(imgBytes)))),
			2*time.Second,
		)
	}

	// 2. File-path text.
	if path, ok := resolveFilePath(pathCandidate); ok {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			if info.Size() > maxAttachmentSize {
				return true, a.uploadToastCmd("File too large (>10 MB limit)", 3*time.Second)
			}
			if info.Size() == 0 {
				return true, a.uploadToastCmd("Empty file", 2*time.Second)
			}
			filename := filepath.Base(path)
			target.AddAttachment(compose.PendingAttachment{
				Filename: filename,
				Path:     path,
				Mime:     mime.TypeByExtension(filepath.Ext(path)),
				Size:     info.Size(),
			})
			return true, a.uploadToastCmd(
				fmt.Sprintf("Attached: %s (%s)", filename, humanSize(info.Size())),
				2*time.Second,
			)
		}
	}

	return false, nil
}

// beginEditOfSelected starts editing the currently-selected message
// in the focused pane. Returns a no-op + status toast if not owned;
// returns nil if no message is selected.
func (a *App) beginEditOfSelected() tea.Cmd {
	channelID, ts, text, userID, panel, ok := a.selectedMessageContext()
	if !ok {
		return nil
	}
	// Build a synthetic MessageItem just for the ownership check;
	// avoids fetching the full struct twice.
	if !a.isOwnMessage(messages.MessageItem{UserID: userID}) {
		return func() tea.Msg { return statusbar.EditNotOwnMsg{} }
	}
	if channelID == "" || ts == "" {
		return nil
	}

	var stashed string
	switch panel {
	case PanelMessages:
		stashed = a.compose.Value()
		a.compose.SetValue(text)
		a.compose.SetPlaceholderOverride("Editing message — Enter to save, Esc to cancel")
	case PanelThread:
		stashed = a.threadCompose.Value()
		a.threadCompose.SetValue(text)
		a.threadCompose.SetPlaceholderOverride("Editing message — Enter to save, Esc to cancel")
	}

	a.editing.Begin(channelID, ts, panel, stashed)
	a.SetMode(ModeInsert)
	a.focusedPanel = panel
	if panel == PanelThread {
		return a.threadCompose.Focus()
	}
	return a.compose.Focus()
}

// notifyReadStateChanged invalidates the sidebar render cache,
// refreshes the workspace rail's HasUnread flags, and recomputes the
// cached terminal-window title. Call this whenever per-channel read
// state is mutated via the DB API (i.e., wherever a.sidebar.Invalidate()
// used to suffice). All three operations are downstream consumers of
// the same DB read-state change and MUST stay in lockstep -- e.g., the
// rail going stale while the sidebar updates was an earlier bug class
// this single hook prevents.
func (a *App) notifyReadStateChanged() {
	a.sidebar.Invalidate()
	a.workspaceRail.RefreshUnreads()
	active := a.sidebar.UnreadChannelCount()
	other := a.workspaceRail.OtherUnreadCount(a.activeTeamID)
	name := a.workspaceRail.NameByID(a.activeTeamID)
	a.windowTitle = computeWindowTitle(a.activeTeamID, name, active, other)
	if a.statusReport != nil {
		a.statusReport(active, other, name, a.windowTitle)
	}
}

// applyChannelMark updates local state for a channel-level read-state
// change. channelID is the channel; ts is the new last_read watermark;
// unreadCount is the canonical unread count to show in the sidebar
// badge.
//
// This ALWAYS moves the on-screen cursor. Two callers: the local
// mark-unread press (reducer_send.go's MessageMarkedUnreadMsg arm),
// which is a deliberate user action, and applyChannelMarkEcho, which
// has already decided the inbound channel_marked is not slk's own.
//
// Idempotent: calling twice with the same values is a no-op past the
// first one (the underlying setters short-circuit on equality).
func (a *App) applyChannelMark(channelID, ts string, unreadCount int) {
	debuglog.Cache("applyChannelMark: channel=%s ts=%s unread_count=%d active=%s",
		channelID, ts, unreadCount, a.activeChannelID)
	for _, mm := range a.modelsForChannel(channelID) {
		mm.SetLastReadTS(ts)
	}
	a.notifyReadStateChanged()
}

// applyChannelMarkEcho handles an inbound channel_marked WS event.
//
// Slack broadcasts channel_marked to every connected client INCLUDING
// the one that issued the mark, so this arrives both for marks made in
// another Slack client and for slk's own. Applying slk's own would drag
// the "── new ──" divider to the newest message a round trip after slk
// marked it, deleting the user's place moments after they looked at it.
// Those are suppressed: the read-state notification still runs, so the
// sidebar dot and workspace rail clear, but the cursor is left alone.
// A mark slk did not issue carries no record and still moves the
// cursor, which is correct — the user really did read the channel
// elsewhere. See selfMarkDedup.
//
// The dedup lives here rather than inside applyChannelMark because that
// helper is shared with the local mark-unread press, which must move
// the divider unconditionally. Those two can collide on ts: slk
// auto-marks at the newest message, and "mark this newest message
// unread so I deal with it later" targets that same ts. Consuming there
// would silently swallow the user's explicit action for as long as the
// self-mark record lives — one round trip normally, unbounded if the
// echo never arrives.
func (a *App) applyChannelMarkEcho(channelID, ts string, unreadCount int) {
	if a.selfMarks.consume(selfMarkKey{channelID: channelID, ts: ts}) {
		debuglog.Cache("applyChannelMarkEcho: channel=%s ts=%s self_echo=true (cursor held)",
			channelID, ts)
		a.notifyReadStateChanged()
		return
	}
	a.applyChannelMark(channelID, ts, unreadCount)
}

// applyThreadMark updates local state for a thread read-cursor move.
// Read/unread is derived by comparing the cursor against the thread's
// newest known reply — never from the subscription's `active` flag,
// which means "subscribed".
//
// This applies lastRead to the panel unconditionally whenever the panel
// is open on this thread — there is no self-echo check here. Its sole
// caller is applyThreadMarkEcho, which has already decided the inbound
// thread_marked is not slk's own. Mirrors applyChannelMark; see
// applyThreadMarkEcho for why the dedup does not live here.
func (a *App) applyThreadMark(channelID, threadTS, lastRead string) {
	debuglog.Cache("applyThreadMark: channel=%s thread_ts=%s last_read=%s active=%s",
		channelID, threadTS, lastRead, a.activeChannelID)
	if a.threadVisible &&
		a.threadPanel.ChannelID() == channelID &&
		a.threadPanel.ThreadTS() == threadTS {
		// The panel renders its "── new ──" landmark after the boundary
		// ts, so a fully-caught-up cursor renders no landmark.
		a.threadPanel.SetUnreadBoundary(lastRead)
	}
	a.applyThreadMarkListState(channelID, threadTS, lastRead)
}

// applyThreadMarkListState is applyThreadMark's threads-list half: it
// settles the row's unread flag and the sidebar badge against lastRead
// without touching an open thread panel's landmark.
//
// Three callers. applyThreadMark runs it as its own tail, after moving
// the landmark for a mark slk did not issue. The other two are the two
// ways slk sees its OWN mark: ThreadMarkedLocalMsg, the HTTP call
// reporting success, and applyThreadMarkEcho's suppressed branch,
// Slack's broadcast of that same mark coming back. For those two the
// panel is on screen, the user is reading it, and its boundary was
// deliberately set on open from the pre-open cursor (see
// openSelectedThreadCmd); the mark carries the cursor slk just set —
// the newest reply — so applying it to the panel would erase the
// landmark. The read state itself is real, though, so the list flag and
// the badge must still settle, which is what this does.
func (a *App) applyThreadMarkListState(channelID, threadTS, lastRead string) {
	if a.threadsView.MarkByThreadTSReadAt(channelID, threadTS, lastRead) {
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
	}
}

// applyThreadMarkEcho handles an inbound thread_marked WS event.
//
// Slack broadcasts thread_marked to every connected client INCLUDING
// the one that issued the mark, so ThreadMarkedRemoteMsg does NOT mean
// "another client": it carries slk's own marks back too, chiefly the
// mark-on-open from reducer_threads' ThreadRepliesLoadedMsg arm and the
// auto-mark from flushPendingMarks. Those echoes carry the newest
// reply, and the panel renders no landmark once its boundary reaches
// the newest reply — so applying one wipes the "── new ──" divider a
// round trip after the user opened the thread to look at it. They are
// suppressed: the threads-list flag and sidebar badge still settle, but
// the landmark is left where opening the thread put it. A mark slk did
// not issue carries no record and still moves the landmark, which is
// correct — the user really did read the thread elsewhere. See
// selfMarkDedup.
//
// "Chiefly", not "only": the thread mark-unread press is a third
// self-originated echo. MarkThreadUnread posts to the same
// subscriptions.thread.mark endpoint with read=0, so it broadcasts back
// here exactly as the read marks do. It is deliberately NOT recorded,
// so its echo is treated as foreign and moves the landmark. That is
// correct: Slack rolls the cursor back to just before the ts the user
// picked, so applying it renders the landmark exactly where the press
// asked for it. Do not "complete" the recording sites
// by adding it: that would suppress the press's own effect, the
// thread-side form of the applyChannelMark collision described below.
//
// The dedup lives here rather than inside applyThreadMark so that
// consuming a record stays a property of the echo path alone.
// applyThreadMark has no other caller today — the thread mark-unread
// press goes through applyThreadMarkUnread — but the channel pair
// records why the split is worth keeping: applyChannelMark IS shared
// with the mark-unread press, and folding the dedup into it would
// silently swallow that deliberate action whenever the two collided on
// ts. Anything later reaching applyThreadMark inherits the
// unconditional behaviour rather than the suppression.
func (a *App) applyThreadMarkEcho(channelID, threadTS, lastRead string) {
	if a.selfThreadMarks.consume(selfMarkKey{
		channelID: channelID, threadTS: threadTS, ts: lastRead,
	}) {
		debuglog.Cache("applyThreadMarkEcho: channel=%s thread_ts=%s last_read=%s self_echo=true (landmark held)",
			channelID, threadTS, lastRead)
		a.applyThreadMarkListState(channelID, threadTS, lastRead)
		return
	}
	a.applyThreadMark(channelID, threadTS, lastRead)
}

// applyThreadMarkUnread forces a thread unread from boundaryTS. Used by
// the user-initiated mark-unread flow, which knows its own intent.
// It does not go through applyThreadMark's cursor comparison: Slack sets
// the cursor to just BEFORE boundaryTS, so comparing boundaryTS against
// the newest reply would wrongly report the thread read whenever the
// boundary is that newest reply.
func (a *App) applyThreadMarkUnread(channelID, threadTS, boundaryTS string) {
	debuglog.Cache("applyThreadMarkUnread: channel=%s thread_ts=%s boundary=%s active=%s",
		channelID, threadTS, boundaryTS, a.activeChannelID)
	if a.threadVisible &&
		a.threadPanel.ChannelID() == channelID &&
		a.threadPanel.ThreadTS() == threadTS {
		a.threadPanel.SetUnreadBoundary(boundaryTS)
	}
	if a.threadsView.MarkByThreadTSUnread(channelID, threadTS) {
		a.sidebar.SetThreadsUnreadCount(a.threadsView.UnreadCount())
	}
}

// beginDeleteOfSelected opens the confirmation prompt for deleting the
// currently-selected message in the focused pane. Returns a no-op +
// status toast if not owned, or nil if no message is selected.
func (a *App) beginDeleteOfSelected() tea.Cmd {
	channelID, ts, text, userID, _, ok := a.selectedMessageContext()
	if !ok {
		return nil
	}
	if !a.isOwnMessage(messages.MessageItem{UserID: userID}) {
		return func() tea.Msg { return statusbar.DeleteNotOwnMsg{} }
	}
	if channelID == "" || ts == "" {
		return nil
	}

	preview := strings.ReplaceAll(text, "\n", " ")
	const maxPreview = 80
	if runes := []rune(preview); len(runes) > maxPreview {
		preview = string(runes[:maxPreview]) + "…"
	}

	a.confirmPrompt.Open(
		"Delete message?",
		preview,
		func() tea.Msg {
			return DeleteMessageMsg{ChannelID: channelID, TS: ts}
		},
	)
	a.SetMode(ModeConfirm)
	return nil
}

// markUnreadOfSelected rolls the read watermark backward to the message
// immediately before the currently-selected message in the focused
// pane. Channel pane → emits MarkUnreadMsg with ThreadTS="". Thread
// pane → emits MarkUnreadMsg with ThreadTS=parent ts. Returns nil
// when nothing is selected (silent no-op, matches Edit/Delete).
//
// Boundary semantics:
//   - Channel pane, selection is i-th of N loaded messages →
//     BoundaryTS = messages[i-1].TS (or "0" if i == 0)
//     UnreadCount = N - i
//   - Thread pane, selection is i-th of N replies →
//     BoundaryTS = replies[i-1].TS (or threadTS if i == 0)
//     UnreadCount = 0 (sidebar isn't updated for thread-level)
func (a *App) markUnreadOfSelected() tea.Cmd {
	channelID, ts, _, _, panel, ok := a.selectedMessageContext()
	if !ok || channelID == "" || ts == "" {
		return nil
	}

	switch panel {
	case PanelMessages:
		msgs := a.messagepane.Messages()
		idx := -1
		for i := range msgs {
			if msgs[i].TS == ts {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		boundary := "0"
		if idx > 0 {
			boundary = msgs[idx-1].TS
		}
		unreadCount := len(msgs) - idx
		chID := channelID
		bTS := boundary
		n := unreadCount
		return func() tea.Msg {
			return MarkUnreadMsg{
				ChannelID:   chID,
				ThreadTS:    "",
				BoundaryTS:  bTS,
				UnreadCount: n,
			}
		}

	case PanelThread:
		threadTS := a.threadPanel.ThreadTS()
		replies := a.threadPanel.Replies()
		idx := -1
		for i := range replies {
			if replies[i].TS == ts {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		boundary := threadTS
		if idx > 0 {
			boundary = replies[idx-1].TS
		}
		chID := channelID
		tTS := threadTS
		bTS := boundary
		return func() tea.Msg {
			return MarkUnreadMsg{
				ChannelID:   chID,
				ThreadTS:    tTS,
				BoundaryTS:  bTS,
				UnreadCount: 0,
			}
		}
	}
	return nil
}

// openImagePreviewOfSelected dispatches OpenImagePreviewMsg for the
// first image attachment on the currently-selected message in the
// focused pane (messages or thread). Returns nil if no message is
// selected or the selected message has no image attachment.
func (a *App) openImagePreviewOfSelected() tea.Cmd {
	channelID, ts, _, _, _, ok := a.selectedMessageContext()
	if !ok {
		return nil
	}
	msgItem, found := a.findMessageInActiveChannel(channelID, ts)
	if !found {
		return nil
	}
	for i, att := range msgItem.Attachments {
		if att.Kind == "image" && att.FileID != "" {
			channel := channelID
			messageTS := ts
			idx := i
			return func() tea.Msg {
				return messages.OpenImagePreviewMsg{
					Channel: channel,
					TS:      messageTS,
					AttIdx:  idx,
				}
			}
		}
	}
	return nil
}

// submitEdit emits an EditMessageMsg if the edit text is non-empty.
// Empty text refuses with an inline toast and keeps edit mode open.
func (a *App) submitEdit(rawValue, translated string) tea.Cmd {
	if strings.TrimSpace(rawValue) == "" {
		return func() tea.Msg {
			return editEmptyToastMsg{}
		}
	}
	chID := a.editing.ChannelID()
	ts := a.editing.TS()
	return func() tea.Msg {
		return EditMessageMsg{
			ChannelID: chID,
			TS:        ts,
			NewText:   translated,
		}
	}
}

// truncateReason returns s truncated to max characters with an ellipsis.
// Used for status-bar error toasts.
func truncateReason(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// humanSize formats a byte count as "12 KB", "3.4 MB", or "<1 KB".
func humanSize(size int64) string {
	const kb = 1024
	const mb = 1024 * kb
	switch {
	case size >= mb:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(mb))
	case size >= kb:
		return fmt.Sprintf("%d KB", size/kb)
	default:
		return "<1 KB"
	}
}

// resolveFilePath inspects clipboard text and returns a cleaned,
// absolute file path if it looks like a single-line existing-file
// reference. Returns ok=false on multi-line input, oversized input,
// non-absolute and non-./-relative paths, or paths that don't
// expand. The caller is responsible for the os.Stat / IsRegular
// check and the size check.
func resolveFilePath(text string) (string, bool) {
	s := strings.TrimSpace(text)
	if s == "" || strings.ContainsAny(s, "\n\r") || len(s) > 4096 {
		return "", false
	}
	if strings.HasPrefix(s, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		s = filepath.Join(home, s[2:])
	}
	if !filepath.IsAbs(s) && !strings.HasPrefix(s, "./") {
		return "", false
	}
	return filepath.Clean(s), true
}

// uploadToastCmd builds a tea.Cmd that sets the status bar to the
// given message and schedules a CopiedClearMsg after dur.
func (a *App) uploadToastCmd(text string, dur time.Duration) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			a.statusbar.SetToast(text)
			return nil
		},
		tea.Tick(dur, func(time.Time) tea.Msg {
			return statusbar.CopiedClearMsg{}
		}),
	)
}

// scheduleChannelSearch defers a channels/search for the finder's
// current query.
//
// The generation is bumped on every call, including the ones that
// return nil: emptying the query must cancel the request that the last
// keystroke had already scheduled, and only a bump can do that. An
// empty query issues nothing — edge.ChannelsSearch would return early
// anyway, but queueing the request and dropping it at the far end
// still leaves a timer running against a finder that is on its way
// closed.
func (a *App) scheduleChannelSearch(query string) tea.Cmd {
	a.pendingChannelSearchGen++
	if query == "" {
		return nil
	}
	gen := a.pendingChannelSearchGen
	delay := a.channelSearchDebounce
	if delay <= 0 {
		delay = channelSearchDebounceDelay
	}
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return channelSearchDebounceMsg{query: query, gen: gen}
	})
}
