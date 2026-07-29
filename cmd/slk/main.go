package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/debuglog"
	emojiwidth "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/notify"
	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/membership"
	"github.com/gammons/slk/internal/slackdesktop"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gammons/slk/internal/slackurl"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/messages/blockkit"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/searchresults"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/workspace"
	versionpkg "github.com/gammons/slk/internal/version"
	"github.com/gammons/slk/internal/wake"
	"github.com/slack-go/slack"
	"golang.design/x/clipboard"
	"golang.org/x/term"
)

// Build-time version info, injected via -ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// UnresolvedDM tracks a DM channel whose user name wasn't in the initial user list.
type UnresolvedDM struct {
	ChannelID string
	UserID    string
}

// sectionsProviderAdapter adapts *service.SectionStore to the
// sidebar.SectionsProvider interface. Translates SidebarSection into
// the sidebar's view-only SectionMeta shape. The store may be nil;
// the adapter reports Ready()==false in that case so the sidebar
// stays in config-glob mode.
type sectionsProviderAdapter struct {
	store *service.SectionStore
}

func (a sectionsProviderAdapter) Ready() bool {
	return a.store != nil && a.store.Ready()
}

func (a sectionsProviderAdapter) OrderedSlackSections() []sidebar.SectionMeta {
	if a.store == nil {
		return nil
	}
	secs := a.store.OrderedSections()
	out := make([]sidebar.SectionMeta, 0, len(secs))
	for _, s := range secs {
		out = append(out, sidebar.SectionMeta{
			ID:    s.ID,
			Name:  s.Name,
			Emoji: s.Emoji,
			Type:  s.Type,
		})
	}
	return out
}

// WorkspaceContext holds all state for a single connected workspace.
type WorkspaceContext struct {
	Client     *slackclient.Client
	ConnMgr    *slackclient.ConnectionManager
	RTMHandler *rtmEventHandler
	UserNames  map[string]string
	// AvatarURLs maps userID -> avatar image URL. Populated from the
	// local users cache at connect time (synchronous, before any
	// goroutines spin up) and refreshed from the background
	// client.GetUsers fetch and on-demand resolveUser calls. Read by
	// the AvatarFunc closure on the UI goroutine to trigger a lazy
	// avatar Preload when an avatar slot first renders empty.
	//
	// sync.Map (not a plain map) because writes happen from background
	// goroutines (GetUsers fetch, resolveUser) while reads happen on
	// the bubbletea Update goroutine. The lookup-or-trigger pattern
	// (LoadOrStore-style) doesn't apply here — we only call Load — but
	// we still need a concurrent map to avoid Go's "concurrent map
	// writes" detector. Stored values are string (avatar URL).
	AvatarURLs *sync.Map
	// UserNamesByHandle maps a user's handle (the Slack `name` field
	// without an `@`) to a display name. Used to resolve participant
	// handles in mpdm channel names like `mpdm-grant--myles--ray-1`.
	UserNamesByHandle map[string]string
	// BotUserIDs is the set of user IDs known to be Slack apps or bots.
	// Populated from the local cache on startup and refreshed by the
	// background users.list fetch and any on-demand resolveUser calls.
	// Used during channel construction to bucket app DMs into a separate
	// "Apps" sidebar section.
	BotUserIDs map[string]bool
	// SectionStore holds the user's Slack-native sidebar sections for
	// this workspace. Nil when use_slack_sections is disabled, the
	// REST bootstrap failed, or this workspace hasn't connected yet.
	// channelitem.go's resolver and the sectionsProviderAdapter both
	// nil-check it before use.
	SectionStore *service.SectionStore
	// MuteStore tracks which channels the user has muted (Slack stores
	// this in the user prefs blob, not on the channel objects). The
	// sidebar uses it to suppress unread dots and apply a dimmer
	// foreground for muted channels. Nil when the bootstrap fetch
	// failed or hasn't run yet — callers must nil-check before use.
	MuteStore *service.MuteStore
	// ThreadsHasUnreads is the workspace-wide threads-have-any-unread
	// signal returned by client.counts on startup. The local SQLite
	// heuristic for per-thread unread state can produce false positives
	// (the parent channel's last_read_ts is older than a thread reply
	// the user already read in another Slack client). When Slack tells
	// us the workspace has zero unread threads, we trust that and
	// suppress the heuristic-derived flags entirely.
	ThreadsHasUnreads bool
	// SubscriptionsAvailable indicates whether the most recent
	// runSubscriptionPhase attempt succeeded in fetching Slack's
	// authoritative thread-subscription list. true on bootstrap
	// (optimistic — no banner during the brief pre-bootstrap
	// window) and after every successful subscription phase; false
	// after a failed one. The UI uses it to decide whether to draw
	// the "Threads list unavailable" banner.
	SubscriptionsAvailable bool
	Channels               []sidebar.ChannelItem
	// FinderItems is the merged list shown in the Ctrl+T finder. Initially
	// contains only joined channels; the BrowseableChannelsLoadedMsg pipeline
	// extends it with non-joined public channels in the background.
	FinderItems   []channelfinder.Item
	TeamID        string
	TeamName      string
	UserID        string
	UnresolvedDMs []UnresolvedDM
	CustomEmoji   map[string]string // emoji name -> URL or "alias:target"
	// userGroups holds this workspace's usergroup ID -> handle map.
	// Access it via UserGroups/SetUserGroups, never directly.
	//
	// atomic.Pointer (not a plain map) because the write happens on the
	// background usergroups.list fetch goroutine while reads happen on
	// the bubbletea Update/cmd goroutines (workspace switch, search) and
	// on the RTM event loop (notification body stripping). This is where
	// UserGroups differs from the CustomEmoji field it otherwise mirrors:
	// CustomEmoji is only ever consumed through the tea message loop, so
	// its single background write is never read cross-goroutine.
	//
	// The stored map is published once and never mutated afterwards, so
	// readers need no further synchronization.
	userGroups atomic.Pointer[map[string]string]
	// Self presence and DND state for this workspace. Populated on connect
	// and updated by manual_presence_change / dnd_updated WS events plus
	// optimistic writes from the presence menu.
	Presence   string    // "active" or "away"; "" until first fetch
	DNDEnabled bool      // true if either snooze or admin-DND is active
	DNDEndTS   time.Time // unified end timestamp; zero if not in DND
	// LastVisitedByChannel maps channelID -> unix-second timestamp of
	// the user's most recent visit to that channel in this workspace.
	// Populated once at connect from cache.GetChannelVisits and
	// updated on every ChannelSelectedMsg via the visit recorder.
	// Used to populate channelfinder.Item.LastVisited for sort.
	LastVisitedByChannel map[string]int64
	// UserResolver dispatches background users.info lookups for
	// unknown message authors. Set in connectWorkspace once the
	// in-memory UserNames map and the *tea.Program are both available.
	// Hot-path message processors call resolveUserCached first and
	// fall back to UserResolver.Request(userID) to enqueue an async
	// fetch; the goroutine emits ui.UserResolvedMsg back into the
	// program, which patches in-history rows live.
	UserResolver *userResolver
	// Membership owns per-channel member sets for this workspace:
	// SQLite-backed cache + eager fetch on channel switch + live
	// member_joined/left WS deltas + external-user resolution. Set
	// in connectWorkspace alongside UserResolver (it depends on the
	// resolver to trigger external-user lookups for newly-seen IDs).
	Membership *membership.Manager
}

// UserGroups returns this workspace's usergroup ID -> handle map, or an
// empty map before usergroups.list has returned. Safe to call from any
// goroutine; the result must be treated as read-only.
func (w *WorkspaceContext) UserGroups() map[string]string {
	if m := w.userGroups.Load(); m != nil {
		return *m
	}
	return map[string]string{}
}

// SetUserGroups publishes a usergroup ID -> handle map for this
// workspace. The caller must not mutate the map afterwards.
func (w *WorkspaceContext) SetUserGroups(groups map[string]string) {
	w.userGroups.Store(&groups)
}

// workspaceRouter holds the program-wide "active workspace" pointer.
// wireCallbacks(router) is invoked ONCE at startup. Every workspace-
// scoped callback reads router.Active() at invocation time so the
// effective workspace tracks the user's current Ctrl-N selection
// without any closure rebinding.
//
// The `all` map is populated only during the connect-workspaces phase
// (before p.Run); subsequent reads from p.Send-invoked callbacks are
// race-free without a mutex.
type workspaceRouter struct {
	active atomic.Pointer[WorkspaceContext]
	all    map[string]*WorkspaceContext
}

func newWorkspaceRouter() *workspaceRouter {
	return &workspaceRouter{all: map[string]*WorkspaceContext{}}
}

func (r *workspaceRouter) Active() *WorkspaceContext  { return r.active.Load() }
func (r *workspaceRouter) Set(wctx *WorkspaceContext) { r.active.Store(wctx) }
func (r *workspaceRouter) ByID(teamID string) *WorkspaceContext {
	return r.all[teamID]
}

// userResolver dispatches users.info lookups for unknown message
// authors in the background. Deduplicates concurrent requests for
// the same userID; failures are silent (the row stays rendered as
// its user ID). Bound to a single workspace because user IDs are
// workspace-scoped.
type userResolver struct {
	teamID   string
	client   *slackclient.Client
	db       *cache.DB
	avatars  *avatar.Cache
	send     func(tea.Msg)
	inflight sync.Map // userID -> struct{}
}

func newUserResolver(
	teamID string,
	client *slackclient.Client,
	db *cache.DB,
	avatars *avatar.Cache,
	send func(tea.Msg),
) *userResolver {
	return &userResolver{
		teamID:  teamID,
		client:  client,
		db:      db,
		avatars: avatars,
		send:    send,
	}
}

// Request enqueues a users.info fetch for userID. Returns immediately.
// On success, emits a ui.UserResolvedMsg via the resolver's send
// callback so the App can patch in-history display names live.
func (r *userResolver) Request(userID string) {
	if r == nil || userID == "" {
		return
	}
	if _, exists := r.inflight.LoadOrStore(userID, struct{}{}); exists {
		return
	}
	// Skip if already resolved (in cache.User). This is the hot path
	// for membership.Manager which calls Request for every channel
	// member returned by conversations.members; without this, every
	// channel-switch refetches users.info for each member, which is
	// O(channel-size) API calls per switch (a 1000-member shared
	// channel = 1000 calls). Stale-data refresh is the responsibility
	// of an explicit re-resolution path (not implemented here); this
	// gate is for "first time we see this user".
	//
	// Order matters: inflight.LoadOrStore first (claim the slot),
	// THEN the cache check (bail if cached), with inflight.Delete on
	// the bail path. This avoids a race where two concurrent Requests
	// both miss the cache, both then LoadOrStore (one wins, the loser
	// silently returns). Store-first + check-second means at most one
	// goroutine ever passes the cache check.
	if _, err := r.db.GetUser(userID); err == nil {
		r.inflight.Delete(userID)
		return
	}
	go func() {
		defer r.inflight.Delete(userID)
		u, err := r.client.GetUserProfile(userID)
		if err != nil {
			debuglog.Cache("userResolver: GetUserProfile team=%s user=%s err=%v",
				r.teamID, userID, err)
			return
		}
		name := u.Profile.DisplayName
		if name == "" {
			name = u.RealName
		}
		if name == "" {
			name = u.Name
		}
		isBot := u.IsBot || u.IsAppUser
		// r.teamID is the workspace's home TeamID; u.TeamID is the
		// user's home TeamID. If they differ (and u.TeamID is set),
		// the user is a Slack Connect / shared-channel guest. Treat
		// an empty u.TeamID as internal — better to under-detect than
		// to falsely flag.
		isExternal := u.TeamID != "" && u.TeamID != r.teamID
		// Persist to the cache DB (its own goroutine-safe SQLite
		// connection) and the avatar cache (internal RWMutex), but
		// do NOT write r.userNames[userID] from this goroutine —
		// userNames is a plain map shared with the UI goroutine and
		// other code paths, and a direct write here trips Go's
		// "concurrent map writes" detector under load (two parallel
		// Request goroutines for different userIDs is enough). The
		// UserResolvedMsg below is delivered to the bubbletea Update
		// loop, which calls Model.PatchUserName on the UI goroutine
		// — that is the single safe writer for in-history rows.
		// Subsequent resolveUserCached misses fall back to the DB
		// row we just upserted, so we don't re-fetch on every miss
		// in the small window before UserResolvedMsg lands.
		r.avatars.Preload(userID, u.Profile.Image32)
		_ = r.db.UpsertUser(cache.User{
			ID:          userID,
			WorkspaceID: r.teamID,
			Name:        u.Name,
			DisplayName: name,
			AvatarURL:   u.Profile.Image32,
			Presence:    "away",
			IsBot:       isBot,
			IsExternal:  isExternal,
		})
		if r.send != nil {
			r.send(ui.UserResolvedMsg{
				TeamID:      r.teamID,
				UserID:      userID,
				DisplayName: name,
				IsBot:       isBot,
			})
		}
		if isExternal && r.send != nil {
			r.send(ui.UserExternalMsg{UserID: userID, IsExternal: true})
		}
	}()
}

// RequestBot enqueues a bots.info fetch for a bot author (bot_message has
// no `user`, only a `bot_id`). Keyed by botID so the resolved name +
// avatar attach to messages whose UserID was set to the bot_id. username
// is the name carried on the message (used as a fallback / immediate
// value); bots.info supplies the icon (and a name if the message had
// none). Mirrors Request: inflight dedup, cache-skip, async fetch,
// Preload + UpsertUser + UserResolvedMsg (which AvatarReadyMsg follows).
func (r *userResolver) RequestBot(botID, username string) {
	if r == nil || botID == "" {
		return
	}
	if _, exists := r.inflight.LoadOrStore(botID, struct{}{}); exists {
		return
	}
	if _, err := r.db.GetUser(botID); err == nil {
		r.inflight.Delete(botID)
		return
	}
	go func() {
		defer r.inflight.Delete(botID)
		bot, err := r.client.GetBotInfo(context.Background(), botID)
		if err != nil {
			debuglog.Cache("userResolver: GetBotInfo team=%s bot=%s err=%v", r.teamID, botID, err)
			return
		}
		name := username
		if name == "" {
			name = bot.Name
		}
		if name == "" {
			name = botID
		}
		iconURL := bestBotIcon(bot.Icons)
		r.avatars.Preload(botID, iconURL)
		_ = r.db.UpsertUser(cache.User{
			ID:          botID,
			WorkspaceID: r.teamID,
			Name:        name,
			DisplayName: name,
			AvatarURL:   iconURL,
			Presence:    "away",
			IsBot:       true,
		})
		if r.send != nil {
			r.send(ui.UserResolvedMsg{
				TeamID:      r.teamID,
				UserID:      botID,
				DisplayName: name,
				IsBot:       true,
			})
		}
	}()
}

// bestBotIcon returns the best bot icon URL for the avatar grid. The
// avatar fetcher downscales to the avatar cell size, so we prefer the
// 72px icon — large enough to render sharp yet almost always present —
// then fall back through the other sizes by availability rather than
// strictly by pixel size.
func bestBotIcon(ic slack.Icons) string {
	for _, u := range []string{ic.Image72, ic.Image48, ic.Image132, ic.Image36, ic.Image230} {
		if u != "" {
			return u
		}
	}
	return ""
}

func main() {
	// Debug log: when SLK_DEBUG is set, debuglog.Init opens
	// slk-debug.log in cwd (truncating any prior session) and routes
	// both the package-internal logger and the global stdlib log to
	// it. When unset, stdlib log is routed to io.Discard so spurious
	// log.Printf calls don't bleed into the user's altscreen TUI.
	if debugFile, err := debuglog.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "slk: could not open debug log: %v\n", err)
	} else if debugFile != nil {
		// Defer fires only on the clean main() return path; os.Exit
		// in the flag-handling block below skips it. That's fine —
		// the OS reclaims the FD on process exit and stdlib log
		// writes are unbuffered, so no log lines are lost.
		defer debugFile.Close()
		debuglog.General("=== slk debug session started ===")
	}
	// Handle simple flags before anything else
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Printf("slk %s (commit %s, built %s)\n", version, commit, date)
			fmt.Println("Unofficial Slack client. Not affiliated with Slack Technologies, LLC.")
			fmt.Println("Uses Slack's internal browser protocol; may violate Slack's TOS. Use at your own risk.")
			return
		case "--help", "-h", "help":
			printHelp()
			return
		case "--add-workspace":
			if err := addWorkspace(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "--remove-workspace":
			if err := removeWorkspace(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		case "--list-workspaces":
			if err := listWorkspaces(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--dump-sections":
			if err := dumpSections(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		case "--dump-prefs":
			if err := dumpPrefs(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	// Load config early (best-effort) so we can pre-detect the image
	// rendering protocol and decide whether to skip the emoji width
	// probe. run() loads config independently as the source of truth;
	// this duplicate load mirrors the best-effort pattern used at the
	// bottom of this file (search for "best-effort" near config.Load).
	// Plan-deviation note: the original plan assumed cfg was available
	// pre-probe, but config.Load actually lives in run() (line 467).
	// Loading here adds ~1ms and lets the probe-skip decision happen
	// before the ~30s probe runs.
	preCfgPath := filepath.Join(xdgConfig(), "config.toml")
	preCfg, preCfgErr := config.Load(preCfgPath)
	if preCfgErr != nil {
		debuglog.ImgRender("pre-probe config load failed: %v (continuing without image-mode pre-detect)", preCfgErr)
	}

	// Pre-detect the image rendering protocol (env-based; non-interactive)
	// so we can decide whether to skip the emoji width probe entirely.
	// Image mode is active when the user has requested it AND we have
	// reasonable confidence kitty will be the final protocol. The
	// interactive kitty version probe in run() may still downgrade
	// kitty→halfblock; in that rare case the user has already paid the
	// probe-skip and will see lipgloss-fallback widths for non-trivial
	// clusters. Acceptable tradeoff for the common-case startup win.
	if preCfgErr == nil {
		preDetectedProto := imgpkg.Detect(imgpkg.CaptureEnv(), preCfg.Appearance.ImageProtocol)
		imageMode := preCfg.Appearance.EmojiImages == "on" && preDetectedProto == imgpkg.ProtoKitty
		emojiwidth.SetImageMode(imageMode, preCfg.Appearance.EmojiCells)
		if imageMode {
			debuglog.ImgRender("emoji image mode: ON (pre-detected proto=%s, emoji_images=%q)",
				preDetectedProto, preCfg.Appearance.EmojiImages)
		} else {
			debuglog.ImgRender("emoji image mode: OFF (pre-detected proto=%s, emoji_images=%q)",
				preDetectedProto, preCfg.Appearance.EmojiImages)
		}
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`slk %s -- a blazingly fast Slack TUI

Usage:
  slk                    Launch the TUI
  slk --add-workspace     Add a Slack workspace (interactive)
  slk --remove-workspace  Remove a configured workspace (interactive)
  slk --list-workspaces   List configured workspaces (TeamID, Slug, Name)
  slk --dump-sections     Dump raw users.channelSections.list JSON (diagnostic)
  slk --version          Print version and exit
  slk --help             Show this help

Config:  ~/.config/slk/config.toml
Data:    ~/.local/share/slk/
Cache:   ~/.cache/slk/

Docs:    https://github.com/gammons/slk
`, version)
}

func run() error {
	// Resolve XDG paths
	configDir := xdgConfig()
	dataDir := xdgData()
	cacheDir := xdgCache()

	// Load config
	configPath := filepath.Join(configDir, "config.toml")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Load custom themes and apply the active theme
	themesDir := filepath.Join(configDir, "themes")
	styles.LoadCustomThemes(themesDir)
	// At startup we apply the global default. The per-workspace theme
	// for the initial active workspace is then re-applied via
	// WorkspaceReadyMsg.Theme once that workspace finishes connecting,
	// which avoids a flash of the wrong theme without needing to know
	// the active TeamID up front (workspaces connect in goroutines).
	styles.Apply(cfg.Appearance.Theme, cfg.Theme)

	notifier := notify.New(cfg.Notifications.Enabled, cfg.Notifications.NotifyCommand)

	// Initialize the OS clipboard for paste-to-upload.
	//
	// Wayland sessions: golang.design/x/clipboard is X11-only and does
	// not see images placed on the clipboard by Wayland-native apps
	// (even with XWayland), so we shell out to `wl-paste` instead.
	// Requires the `wl-clipboard` package.
	//
	// Otherwise (X11 / macOS / Windows) use the native library.
	clipboardOK := true
	useWaylandClipboard := false
	if ui.IsWayland() {
		if ui.HasWlPaste() {
			useWaylandClipboard = true
		} else {
			log.Printf("Warning: WAYLAND_DISPLAY set but wl-paste not on PATH; install wl-clipboard for paste-to-upload. Ctrl+V image paste disabled.")
			clipboardOK = false
		}
	} else {
		if err := clipboard.Init(); err != nil {
			log.Printf("Warning: clipboard init failed (%v); Ctrl+V image paste disabled", err)
			clipboardOK = false
		}
	}

	// Initialize cache database
	dbPath := filepath.Join(dataDir, "cache.db")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	db, err := cache.New(dbPath)
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}
	defer db.Close()

	// Ensure image cache dir exists
	imgCacheDir := filepath.Join(cacheDir, "images")
	os.MkdirAll(imgCacheDir, 0700)

	// Load tokens
	tokenDir := filepath.Join(dataDir, "tokens")
	tokenStore := slackclient.NewTokenStore(tokenDir)
	tokens, err := tokenStore.List()
	if err != nil || len(tokens) == 0 {
		// No workspaces configured -- launch onboarding automatically
		if err := addWorkspace(); err != nil {
			return err
		}
		// Reload tokens after onboarding
		tokens, err = tokenStore.List()
		if err != nil || len(tokens) == 0 {
			return fmt.Errorf("no workspaces configured after onboarding")
		}
	}

	// Re-mint tokens from the live desktop cookie so every launch starts
	// with fresh xoxc tokens (they expire; the desktop cookie is the source
	// of truth). Falls back to cached tokens when offline / desktop absent.
	tokens = remintTokens(context.Background(), tokens,
		slackdesktop.Cookie,
		slackclient.MintToken,
		tokenStore.Save,
	)

	// Initialize services
	wsMgr := service.NewWorkspaceManager(db)
	msgSvc := service.NewMessageService(db)
	_ = msgSvc // will wire for send/receive

	// Create app
	app := ui.NewApp()
	app.SetHelpFooter(versionpkg.ModalFooter(version))
	app.SetClipboardAvailable(clipboardOK)
	if sr := notify.NewStatusReporter(cfg.Notifications.StatusCommand); sr != nil {
		// Enqueue never blocks a render: it hands the state to the reporter's
		// single worker, which serializes runs and coalesces bursts so the
		// external surface can't end up pinned to a stale count by an
		// out-of-order subprocess.
		app.SetStatusReporter(sr.Enqueue)
	}
	if useWaylandClipboard {
		app.SetClipboardReader(ui.WaylandClipboardReader())
	}

	// Connect to workspaces
	ctx := context.Background()
	tsFormat := cfg.Appearance.TimestampFormat

	// Used by the optimistic instant-display path on send: the App
	// mints a placeholder MessageItem before the chat.postMessage HTTP
	// round-trip and needs a Timestamp string that renders identically
	// to messages arriving through the normal load path.
	app.SetNowTimestampFormatter(func() string {
		return time.Now().Format(tsFormat)
	})

	// Initialize shared image cache (used for avatars and inline images).
	imagesDir := filepath.Join(cacheDir, "images")
	imageCache, err := imgpkg.NewCache(imagesDir, cfg.Cache.MaxImageCacheMB)
	if err != nil {
		log.Fatalf("image cache: %v", err)
	}
	// Slack file thumbnails on files.slack.com require BOTH an
	// `Authorization: Bearer <xoxc-token>` header and the workspace's
	// 'd' cookie. The d cookie alone returns Slack's web login page;
	// the Bearer alone returns 403. Both are per-workspace, since each
	// token file carries its own xoxc + cookie. The URL embeds the
	// team ID, so the fetcher attaches the matching team's auth.
	//
	// Slack Connect / shared channels add a wrinkle: those files are
	// hosted on a partner workspace's team ID that we don't have a
	// token for. The fetcher tries each registered team's auth in
	// order until one succeeds, then caches that mapping so subsequent
	// fetches for the same foreign team go directly to the right auth.
	auths := make([]imgpkg.TeamAuth, 0, len(tokens))
	for _, t := range tokens {
		auths = append(auths, imgpkg.TeamAuth{
			TeamID:  t.TeamID,
			Token:   t.AccessToken,
			DCookie: t.Cookie,
		})
		log.Printf("image fetcher: registered team %q (%s) for file auth", t.TeamName, t.TeamID)
	}
	imageHTTPClient := slackhttp.NewBrowserHTTPClient(nil)
	imageHTTPClient.Timeout = 10 * time.Second
	imageFetcher := imgpkg.NewFetcher(imageCache, imageHTTPClient)
	imageFetcher.SetAuths(auths)

	// Migrate old avatar cache (one-time, idempotent).
	oldAvatarDir := filepath.Join(cacheDir, "avatars")
	if n, err := imgpkg.MigrateAvatars(oldAvatarDir, imagesDir); err != nil {
		log.Printf("avatar migration: %v", err)
	} else if n > 0 {
		log.Printf("migrated %d avatars to %s", n, imagesDir)
	}

	// Detect image rendering protocol BEFORE constructing the avatar
	// cache so the cache can pick the right rendering path (kitty
	// graphics for sharp pixels, halfblock otherwise).
	proto := imgpkg.Detect(imgpkg.CaptureEnv(), cfg.Appearance.ImageProtocol)
	debuglog.ImgRender("image protocol detect: cfg=%q result=%s", cfg.Appearance.ImageProtocol, proto)

	// Optional: run kitty version probe if detected as kitty AND stdin is a TTY.
	// Must happen BEFORE bubbletea takes over the terminal.
	if proto == imgpkg.ProtoKitty && term.IsTerminal(int(os.Stdin.Fd())) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			debuglog.ImgRender("kitty probe skipped: cannot enter raw mode: %v", err)
		} else {
			ok := imgpkg.ProbeKittyGraphics(os.Stdout, os.Stdin, 200*time.Millisecond)
			if rerr := term.Restore(int(os.Stdin.Fd()), state); rerr != nil {
				debuglog.ImgRender("term restore after kitty probe: %v", rerr)
			}
			if !ok {
				debuglog.ImgRender("kitty probe failed, downgrading to halfblock")
				proto = imgpkg.ProtoHalfBlock
			}
		}
	}
	debuglog.ImgRender("image protocol: %s", proto)

	// Reconcile the emoji-as-images flag with the post-probe protocol.
	// The pre-probe block in main() (search for emojiwidth.SetImageMode)
	// turns image mode ON based on env-only detection so the emoji width
	// probe can be skipped at startup. If the interactive kitty probe
	// just above downgraded us to halfblock (zellij; tmux with
	// allow-passthrough=off), image mode must be turned back OFF or:
	//   - every emoji.Place() call misses the prerender memo (it
	//     hard-codes ProtoKitty) and falls into a fetch→ready-msg→
	//     re-render loop that pins the UI thread (see issue #50);
	//   - every emoji.Width() call runs uniseg grapheme segmentation
	//     instead of delegating to lipgloss.Width().
	// Conversely, if we landed on ProtoKitty after the probe AND the
	// user has emoji_images=on, make sure image mode is on — covers the
	// case where pre-probe detection was conservative (e.g. preCfg load
	// failed) but the real detect path succeeded.
	reconciledImageMode := proto == imgpkg.ProtoKitty && cfg.Appearance.EmojiImages == "on"
	emojiwidth.SetImageMode(reconciledImageMode, cfg.Appearance.EmojiCells)
	debuglog.ImgRender("emoji image mode: reconciled=%v (final proto=%s, emoji_images=%q)",
		reconciledImageMode, proto, cfg.Appearance.EmojiImages)

	// Avatars use kitty graphics when available (sharper). Sixel and
	// half-block terminals fall back to half-block — re-emitting sixel
	// per visible avatar per redraw would dominate the bandwidth budget.
	avatarCache := avatar.NewCache(imageFetcher, imgpkg.KittyRendererInstance(), proto == imgpkg.ProtoKitty)

	// Cell pixel metrics for sizing decisions.
	pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
	debuglog.ImgRender("cell pixels: %dx%d", pxW, pxH)

	// Wire the inline-image pipeline into the messages pane. SendMsg
	// stays nil here because tea.NewProgram has not run yet; we re-call
	// SetImageContext after `p` is constructed to populate it (see
	// below). Both calls share buildImgCtx so the only difference is
	// the SendMsg callback.
	buildImgCtx := func(send func(tea.Msg)) imgrender.ImageContext {
		return imgrender.ImageContext{
			Protocol:    proto,
			Fetcher:     imageFetcher,
			KittyRender: imgpkg.KittyRendererInstance(),
			CellPixels:  image.Pt(pxW, pxH),
			MaxRows:     cfg.Appearance.MaxImageRows,
			MaxCols:     cfg.Appearance.MaxImageCols,
			SendMsg:     send,
		}
	}
	// buildPlaceCtx mirrors buildImgCtx for emoji-image placements.
	// The Fetcher is the same instance (one cache, one prerender
	// pipeline). SendMsg dispatches EmojiImageReadyMsg through
	// bubbletea so reducers can invalidate per-surface caches.
	buildPlaceCtx := func(send func(tea.Msg)) emojiwidth.PlaceContext {
		return emojiwidth.PlaceContext{
			Fetcher: imageFetcher,
			SendMsg: func(v any) {
				if send != nil {
					if msg, ok := v.(tea.Msg); ok {
						send(msg)
					}
				}
			},
		}
	}
	app.SetImageContext(buildImgCtx(nil))
	app.SetImageFetcher(imageFetcher)
	app.SetImageProtocol(proto)

	// Emoji-image rendering. Active only on kitty (per ImageMode
	// gate set earlier in startup). When inactive the messages pane
	// uses the legacy glyph/shortcode-text rendering path.
	app.SetEmojiContext(messages.EmojiContext{
		PlaceCtx: buildPlaceCtx(nil), // SendMsg refreshed below once Program exists
		Cells:    cfg.Appearance.EmojiCells,
		Customs:  nil, // populated by CustomEmojisLoadedMsg
	})

	// Apply user-configured workspace ordering to tokens before
	// building the rail. The rail and digit-key (1-9) mapping both
	// follow this order, so a stable sort here is what makes
	// `1` always go to the same workspace across runs.
	//
	// `tokens` remains the authoritative slice for order-insensitive
	// operations (image-auth registration, default_workspace lookup);
	// `orderedTokens` is only for user-facing iteration order.
	orderedTokens := config.OrderTokens(tokens, cfg)

	// Build workspace rail items for all tokens, in configured order.
	var wsItems []workspace.WorkspaceItem
	for _, ot := range orderedTokens {
		wsItems = append(wsItems, workspace.WorkspaceItem{
			ID:       ot.Token.TeamID,
			Name:     ot.Token.TeamName,
			Initials: workspace.WorkspaceInitials(ot.Token.TeamName),
		})
	}

	// Set up loading overlay with workspace names, in the same order
	// so the loading list visually matches the rail.
	var wsNames []string
	for _, ot := range orderedTokens {
		wsNames = append(wsNames, ot.Token.TeamName)
	}
	app.SetLoadingWorkspaces(wsNames)
	app.SetWorkspaces(wsItems)
	app.SetTypingEnabled(cfg.Animations.TypingIndicators)
	app.SetSidebarStaleThreshold(time.Duration(cfg.Sidebar.HideInactiveAfterDays) * 24 * time.Hour)
	app.SetMouseWheelLines(cfg.Appearance.MouseWheelLines)

	// Wire theme switcher
	app.SetThemeItems(styles.ThemeNames())
	app.SetThemeOverrides(cfg.Theme)

	// AvatarFunc is wired below, after `router` is declared, because
	// the lazy-fetch path needs router.Active().AvatarURLs to look up
	// the avatar URL on cache misses.

	// Declare p before wiring callbacks so closures can capture it
	var p *tea.Program
	workspaces := make(map[string]*WorkspaceContext)
	var activeTeamID string

	// router holds the program-wide active workspace pointer. All
	// wireCallbacks-registered callbacks read router.Active() at
	// invocation time so they always see the current workspace.
	router := newWorkspaceRouter()

	// Wire avatar rendering with a lazy-fetch path. AvatarFunc is
	// called by the messages/thread panes on the bubbletea Update
	// goroutine for every message authored row. The fast path is a
	// straight map lookup; on miss, we trigger a background Preload
	// keyed by the workspace's AvatarURLs (populated at connect time
	// from the local user cache and refreshed by the GetUsers fetch).
	// The avatar.Cache's inflight dedup ensures only one Preload runs
	// per userID regardless of how many redraws hit the miss path
	// before completion. On completion, Cache.SetOnReady (wired below
	// once `p` exists) sends an AvatarReadyMsg that invalidates the
	// pane caches so the next View() picks up the rendered avatar.
	//
	// This replaces the prior eager bulk-Preload over every cached
	// user in the workspace, which on large workspaces (tens of
	// thousands of users) wrote ~100MB of kitty graphics APC escape
	// data to stdout at startup and produced a multi-minute hang on
	// terminals that decode kitty graphics (kitty, ghostty).
	app.SetAvatarFunc(func(userID string) string {
		if rendered := avatarCache.Get(userID); rendered != "" {
			return rendered
		}
		// Cache miss: trigger a lazy Preload using the URL the
		// workspace recorded at connect time (or that GetUsers
		// refreshed). No router-active = pre-workspace-ready render;
		// AvatarReadyMsg will invalidate once the avatar lands.
		wctx := router.Active()
		if wctx == nil || wctx.AvatarURLs == nil {
			return ""
		}
		if v, ok := wctx.AvatarURLs.Load(userID); ok {
			if url, ok := v.(string); ok && url != "" {
				avatarCache.Preload(userID, url)
			}
		}
		return ""
	})

	// Wire theme switcher: dispatch to the appropriate saver based on scope.
	app.SetThemeSaver(func(name string, scope themeswitcher.ThemeScope) {
		switch scope {
		case themeswitcher.ScopeWorkspace:
			if activeTeamID == "" {
				return // shouldn't happen, but guard against it
			}
			teamName := activeTeamID
			if wctx, ok := workspaces[activeTeamID]; ok && wctx.TeamName != "" {
				teamName = wctx.TeamName
			}
			// Find the existing TOML key for this workspace, if any.
			// If no block exists yet we use the team ID as the key
			// (legacy default); a future --add-workspace may have
			// already written a slug-keyed block.
			tomlKey := activeTeamID
			for k, w := range cfg.Workspaces {
				if w.TeamID == activeTeamID {
					tomlKey = k
					break
				}
			}
			// Update in-memory config.
			if cfg.Workspaces == nil {
				cfg.Workspaces = make(map[string]config.Workspace)
			}
			ws := cfg.Workspaces[tomlKey]
			ws.TeamID = activeTeamID
			ws.Theme = name
			cfg.Workspaces[tomlKey] = ws
			// Persist.
			if err := saveWorkspaceTheme(configPath, tomlKey, activeTeamID, teamName, name); err != nil {
				log.Printf("save workspace theme: %v", err)
			}
		case themeswitcher.ScopeGlobal:
			cfg.Appearance.Theme = name
			if err := saveGlobalTheme(configPath, name); err != nil {
				log.Printf("save global theme: %v", err)
			}
		}
	})

	// Wire sidebar width saver: always persist to the active workspace.
	app.SetWidthSaver(func(width int) {
		if activeTeamID == "" {
			return
		}
		teamName := activeTeamID
		if wctx, ok := workspaces[activeTeamID]; ok && wctx.TeamName != "" {
			teamName = wctx.TeamName
		}
		tomlKey := activeTeamID
		for k, w := range cfg.Workspaces {
			if w.TeamID == activeTeamID {
				tomlKey = k
				break
			}
		}
		if cfg.Workspaces == nil {
			cfg.Workspaces = make(map[string]config.Workspace)
		}
		ws := cfg.Workspaces[tomlKey]
		ws.TeamID = activeTeamID
		ws.SidebarWidth = width
		cfg.Workspaces[tomlKey] = ws
		if err := saveWorkspaceWidth(configPath, tomlKey, activeTeamID, teamName, width); err != nil {
			log.Printf("save workspace sidebar width: %v", err)
		}
	})

	// Wire presence/DND status setter. Captured workspaces map and
	// activeTeamID by reference so the closure always targets the
	// currently-active workspace context.
	app.SetStatusSetter(func(action presencemenu.Action, snoozeMinutes int) {
		wctx := workspaces[activeTeamID]
		if wctx == nil || wctx.Client == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var err error
			switch action {
			case presencemenu.ActionSetActive:
				err = wctx.Client.SetUserPresence(ctx, "auto")
			case presencemenu.ActionSetAway:
				err = wctx.Client.SetUserPresence(ctx, "away")
			case presencemenu.ActionSnooze:
				_, err = wctx.Client.SetSnooze(ctx, snoozeMinutes)
			case presencemenu.ActionEndDND:
				// End any active manual snooze AND any active scheduled
				// DND session. Either may be a no-op depending on the
				// source of the current DND state; calling both ensures
				// we exit any form of DND the user can dismiss
				// client-side. Slack's dnd.endDnd ends the current DND
				// session for the rest of the day; the user's DND
				// schedule (if any) re-engages on its next window.
				_, snoozeErr := wctx.Client.EndSnooze(ctx)
				dndErr := wctx.Client.EndDND(ctx)
				if dndErr != nil {
					err = dndErr
				} else {
					err = snoozeErr
				}
			}
			if err != nil && p != nil {
				p.Send(ui.ToastMsg{Text: "Status change failed: " + err.Error()})
			}
		}()
	})

	// wireCallbacks installs all App callbacks once at startup. Each
	// callback reads router.Active() at invocation time, so the
	// effective workspace tracks the user's current Ctrl-N selection
	// without any per-switch closure rebinding.
	//
	// Goroutines launched from inside a callback must capture
	// workspace-scoped values (Client, UserNames, ...) into local
	// vars BEFORE the `go func()` so they are not affected by a
	// concurrent router.Set during the goroutine's lifetime.
	wireCallbacks := func(router *workspaceRouter) {
		app.SetReadStateReader(func() map[string]cache.ReadState {
			wctx := router.Active()
			if wctx == nil {
				return nil
			}
			state, err := db.GetWorkspaceReadState(wctx.TeamID)
			if err != nil {
				log.Printf("Warning: GetWorkspaceReadState for %s: %v", wctx.TeamID, err)
				return nil
			}
			return state
		})

		app.SetWorkspaceUnreadReader(func() []string {
			ids, err := db.WorkspacesWithUnreads()
			if err != nil {
				log.Printf("Warning: WorkspacesWithUnreads: %v", err)
				return nil
			}
			return ids
		})

		app.SetChannelService(ui.NewChannelService(ui.ChannelServiceFuncs{
			RecordVisit: func(channelID ids.ChannelID) {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return
				}
				wctx.LastVisitedByChannel[chIDStr] = time.Now().Unix()
				teamID := wctx.TeamID
				go func() {
					if err := db.RecordChannelVisit(teamID, chIDStr); err != nil {
						log.Printf("warning: recording channel visit %s/%s: %v", teamID, chIDStr, err)
					}
				}()
			},
			Lookup: func(channelID ids.ChannelID) (string, string, bool) {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return "", "", false
				}
				// Sidebar (joined channels + Slack-native sections).
				for _, ch := range wctx.Channels {
					if ch.ID == chIDStr {
						return ch.Name, ch.Type, true
					}
				}
				// Finder items (joined + browseable). Covers DMs/group DMs
				// that aren't in the sidebar pre-conversation, and any
				// browseable public channels.
				for _, it := range wctx.FinderItems {
					if it.ID == chIDStr {
						return it.Name, it.Type, true
					}
				}
				return "", "", false
			},
			ReadCache: func(channelID ids.ChannelID) []messages.MessageItem {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				return loadCachedMessages(db, wctx.Client.UserID(), string(channelID), wctx.UserNames, tsFormat, router)
			},
			SyncedAt: func(channelID ids.ChannelID) int64 {
				return db.GetChannelSyncedAt(string(channelID))
			},
			MembershipFetch: func(channelID ids.ChannelID) {
				wctx := router.Active()
				if wctx == nil || wctx.Membership == nil {
					return
				}
				// Note: EnsureFresh synchronously calls pushSnapshot, which
				// invokes p.Send(ChannelMembershipMsg). bubbletea v2's program
				// channel is unbuffered (charm.land/bubbletea/v2 tea.go:598),
				// so p.Send blocks until the Update goroutine receives. The
				// App invokes this fetcher in a goroutine for exactly that
				// reason (see app.go ChannelSelectedMsg handler) — we can
				// call EnsureFresh synchronously here because we're already
				// off the Update goroutine.
				wctx.Membership.EnsureFresh(context.Background(), string(channelID))
			},
			OpenConversation: func(userIDs []string, requestID uint64) tea.Cmd {
				wctx := router.Active()
				if wctx == nil {
					return func() tea.Msg {
						return ui.NewMessageFailedMsg{
							RequestID: requestID,
							Err:       fmt.Errorf("no active workspace"),
						}
					}
				}
				client := wctx.Client
				return func() tea.Msg {
					channelID, alreadyOpen, err := client.OpenConversation(ctx, userIDs)
					if err != nil {
						return ui.NewMessageFailedMsg{
							RequestID: requestID,
							Err:       err,
						}
					}
					return ui.NewMessageOpenedMsg{
						ChannelID:   channelID,
						AlreadyOpen: alreadyOpen,
						UserIDs:     userIDs,
						RequestID:   requestID,
					}
				}
			},
			Fetch: func(channelID ids.ChannelID, channelName string) tea.Msg {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				msgItems := fetchChannelMessages(wctx.Client, chIDStr, db, wctx.UserNames, tsFormat, avatarCache, router)

				state, _ := db.GetChannelReadState(chIDStr)
				lastReadTS := state.LastReadTS

				// Mark channel as read up to the latest message
				if len(msgItems) > 0 {
					latestTS := msgItems[len(msgItems)-1].TS
					markChannelReadAsync(ctx, wctx, db, p, chIDStr, latestTS)
				}

				return ui.MessagesLoadedMsg{
					ChannelID:  chIDStr,
					Messages:   msgItems,
					LastReadTS: lastReadTS,
				}
			},
			MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
				wctx := router.Active()
				markChannelReadAsync(ctx, wctx, db, p, string(channelID), string(ts))
				return nil // ChannelMarkedReadMsg is emitted from inside the goroutine
			},
			FetchOlder: func(channelID ids.ChannelID, oldestTS ids.MessageTS) tea.Msg {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				msgItems := fetchOlderMessages(wctx.Client, chIDStr, string(oldestTS), db, wctx.UserNames, tsFormat, router)
				return ui.OlderMessagesLoadedMsg{
					ChannelID: chIDStr,
					AnchorTS:  string(oldestTS),
					Messages:  msgItems,
				}
			},
			FetchAround: func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				msgItems := fetchMessagesAround(wctx.Client, chIDStr, string(ts), db, wctx.UserNames, tsFormat, router)
				if msgItems == nil {
					return ui.MessagesAroundLoadedMsg{ChannelID: chIDStr, TargetTS: string(ts), Err: errors.New("history fetch failed")}
				}
				return ui.MessagesAroundLoadedMsg{ChannelID: chIDStr, TargetTS: string(ts), Messages: msgItems}
			},
			Join: func(channelID ids.ChannelID, channelName string) tea.Msg {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				ctx := context.Background()
				if err := wctx.Client.JoinChannel(ctx, chIDStr); err != nil {
					return ui.ChannelJoinFailedMsg{ID: chIDStr, Name: channelName, Err: err}
				}
				return ui.ChannelJoinedMsg{ID: chIDStr, Name: channelName}
			},
		}))

		app.SetSearchService(ui.NewSearchService(ui.SearchServiceFuncs{
			SearchChannel: func(channelID ids.ChannelID, query string) tea.Msg {
				wctx := router.Active()
				if wctx == nil {
					// Returning nil would leave the `/query  …` spinner
					// stuck; surface the failure like searchWorkspaceFunc.
					return ui.ChannelSearchResultsMsg{ChannelID: string(channelID), Query: query, Err: errors.New("no active workspace")}
				}
				terms := strings.Fields(query)
				folded := make([]string, 0, len(terms))
				for _, t := range terms {
					folded = append(folded, text.Fold(t))
				}
				tses, err := db.SearchChannelMessages(string(channelID), wctx.Client.TeamID(), query)
				return ui.ChannelSearchResultsMsg{
					ChannelID: string(channelID),
					Query:     query,
					Terms:     folded,
					TSes:      tses,
					Err:       err,
				}
			},
			SearchWorkspace: searchWorkspaceFunc(router, db, tsFormat),
		}))

		app.SetMessageService(ui.NewMessageService(ui.MessageServiceFuncs{
			Send: func(channelID ids.ChannelID, text string) tea.Msg {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil {
					return ui.MessageSendFailedMsg{ChannelID: chIDStr, Reason: "no active workspace"}
				}
				client := wctx.Client
				userNames := wctx.UserNames
				ctx := context.Background()
				ts, sentMrkdwn, err := client.SendMessage(ctx, chIDStr, text)
				if err != nil {
					log.Printf("Warning: failed to send message: %v", err)
					return ui.MessageSendFailedMsg{ChannelID: chIDStr, Reason: err.Error()}
				}
				userName := "you"
				if resolved, ok := userNames[client.UserID()]; ok {
					userName = resolved
				}
				return ui.MessageSentMsg{
					ChannelID: chIDStr,
					Message: messages.MessageItem{
						TS:        ts,
						UserID:    client.UserID(),
						UserName:  userName,
						Text:      sentMrkdwn,
						Timestamp: formatTimestamp(ts, tsFormat),
					},
				}
			},
			Edit: func(channelID ids.ChannelID, ts ids.MessageTS, text string) tea.Msg {
				chIDStr, tsStr := string(channelID), string(ts)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				// EditMessage returns the converted mrkdwn but we ignore
				// it here: the message_changed WS echo updates the local
				// copy with the server-stored text via UpdateMessageInPlace.
				// MessageEditedMsg only carries success/fail status.
				_, err := wctx.Client.EditMessage(ctx, chIDStr, tsStr, text)
				if err != nil {
					log.Printf("Warning: failed to edit message %s/%s: %v", chIDStr, tsStr, err)
				}
				return ui.MessageEditedMsg{ChannelID: chIDStr, TS: tsStr, Err: err}
			},
			Delete: func(channelID ids.ChannelID, ts ids.MessageTS) tea.Msg {
				chIDStr, tsStr := string(channelID), string(ts)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err := wctx.Client.RemoveMessage(ctx, chIDStr, tsStr)
				if err != nil {
					log.Printf("Warning: failed to delete message %s/%s: %v", chIDStr, tsStr, err)
				}
				return ui.MessageDeletedMsg{ChannelID: chIDStr, TS: tsStr, Err: err}
			},
			MarkUnread: func(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) tea.Msg {
				chIDStr := string(channelID)
				threadTSStr := string(threadTS)
				boundaryTSStr := string(boundaryTS)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				client := wctx.Client
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				var err error
				if threadTSStr == "" {
					err = client.MarkChannelUnread(ctx, chIDStr, boundaryTSStr)
					if err == nil {
						if dbErr := db.UpdateChannelReadState(chIDStr, boundaryTSStr, true); dbErr != nil {
							log.Printf("Warning: failed to update read state on mark-unread %s/%s: %v", chIDStr, boundaryTSStr, dbErr)
						}
					} else {
						log.Printf("Warning: failed to mark channel %s as unread (boundary %s): %v", chIDStr, boundaryTSStr, err)
					}
				} else {
					err = client.MarkThreadUnread(ctx, chIDStr, threadTSStr, boundaryTSStr)
					if err != nil {
						log.Printf("Warning: failed to mark thread %s/%s as unread (boundary %s): %v", chIDStr, threadTSStr, boundaryTSStr, err)
					}
					// No SQLite write here for thread-level — the
					// thread_subscriptions row's last_read is the
					// source of truth and gets updated when Slack
					// echoes back a thread_marked event. The UI
					// updates immediately via applyThreadMark; on
					// next refresh cache.ListSubscribedThreads will
					// reconcile from the persisted subscription row.
				}
				return ui.MessageMarkedUnreadMsg{
					ChannelID:   chIDStr,
					ThreadTS:    threadTSStr,
					BoundaryTS:  boundaryTSStr,
					UnreadCount: unreadCount,
					Err:         err,
				}
			},
			Permalink: func(ctx context.Context, channelID ids.ChannelID, ts ids.MessageTS) (string, error) {
				wctx := router.Active()
				if wctx == nil {
					return "", nil
				}
				return wctx.Client.GetPermalink(ctx, string(channelID), string(ts))
			},
		}))

		app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
			return func() tea.Msg {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				client := wctx.Client
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()

				for i, att := range attachments {
					p.Send(ui.UploadProgressMsg{Done: i, Total: len(attachments)})

					var reader io.Reader
					if att.Bytes != nil {
						reader = bytes.NewReader(att.Bytes)
					} else {
						f, err := os.Open(att.Path)
						if err != nil {
							return ui.UploadResultMsg{Err: fmt.Errorf("opening %s: %w", att.Filename, err)}
						}
						defer f.Close()
						reader = f
					}

					currentCaption := ""
					if i == len(attachments)-1 {
						currentCaption = caption
					}

					if _, err := client.UploadFile(ctx, channelID, threadTS, att.Filename, reader, att.Size, currentCaption); err != nil {
						return ui.UploadResultMsg{Err: fmt.Errorf("uploading %s (%d/%d): %w", att.Filename, i+1, len(attachments), err)}
					}
				}
				p.Send(ui.UploadProgressMsg{Done: len(attachments), Total: len(attachments)})
				return ui.UploadResultMsg{Err: nil}
			}
		})

		app.SetThreadService(ui.NewThreadService(ui.ThreadServiceFuncs{
			Fetch: func(channelID ids.ChannelID, threadTS ids.ThreadTS) tea.Msg {
				chIDStr, threadTSStr := string(channelID), string(threadTS)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				replies := fetchThreadReplies(wctx.Client, chIDStr, threadTSStr, db, wctx.UserNames, tsFormat, avatarCache, router)
				return ui.ThreadRepliesLoadedMsg{
					ThreadTS: threadTSStr,
					Replies:  replies,
				}
			},
			CacheRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) []messages.MessageItem {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				return loadCachedThreadReplies(db, wctx.Client.UserID(), string(channelID), string(threadTS), wctx.UserNames, tsFormat, router)
			},
			Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) {
				chIDStr, threadTSStr, tsStr := string(channelID), string(threadTS), string(ts)
				wctx := router.Active()
				if wctx == nil {
					return
				}
				client := wctx.Client
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if err := client.MarkThread(ctx, chIDStr, threadTSStr, tsStr); err != nil {
						log.Printf("Warning: MarkThread(%s, %s): %v", chIDStr, threadTSStr, err)
					}
				}()
			},
			SendReply: func(channelID ids.ChannelID, threadTS ids.ThreadTS, text string) tea.Msg {
				chIDStr, threadTSStr := string(channelID), string(threadTS)
				wctx := router.Active()
				if wctx == nil {
					return ui.ThreadReplySendFailedMsg{ChannelID: chIDStr, ThreadTS: threadTSStr, Reason: "no active workspace"}
				}
				client := wctx.Client
				userNames := wctx.UserNames
				ctx := context.Background()
				ts, sentMrkdwn, err := client.SendReply(ctx, chIDStr, threadTSStr, text)
				if err != nil {
					log.Printf("Warning: failed to send thread reply: %v", err)
					return ui.ThreadReplySendFailedMsg{ChannelID: chIDStr, ThreadTS: threadTSStr, Reason: err.Error()}
				}
				userName := "you"
				if resolved, ok := userNames[client.UserID()]; ok {
					userName = resolved
				}
				return ui.ThreadReplySentMsg{
					ChannelID: chIDStr,
					ThreadTS:  threadTSStr,
					Message: messages.MessageItem{
						TS:        ts,
						UserID:    client.UserID(),
						UserName:  userName,
						Text:      sentMrkdwn,
						Timestamp: formatTimestamp(ts, tsFormat),
						ThreadTS:  threadTSStr,
					},
				}
			},
			ListFetch: func(teamID ids.TeamID) tea.Msg {
				teamIDStr := string(teamID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				summaries, err := db.ListSubscribedThreads(teamIDStr, wctx.Client.UserID())
				if err != nil {
					log.Printf("Warning: ListSubscribedThreads(%s): %v", teamIDStr, err)
					return ui.ThreadsListLoadedMsg{
						TeamID:                 teamIDStr,
						Summaries:              nil,
						SubscriptionsAvailable: wctx.SubscriptionsAvailable,
					}
				}
				// With per-thread last_read in thread_subscriptions, the Unread
				// flag is now authoritative — the old ThreadsHasUnreads
				// suppression heuristic that protected against stale
				// channels.last_read_ts is no longer needed.
				return ui.ThreadsListLoadedMsg{
					TeamID:                 teamIDStr,
					Summaries:              summaries,
					SubscriptionsAvailable: wctx.SubscriptionsAvailable,
				}
			},
			ChannelLastRead: func(channelID ids.ChannelID) string {
				wctx := router.Active()
				if wctx == nil {
					return ""
				}
				chIDStr := string(channelID)
				state, err := db.GetChannelReadState(chIDStr)
				if err != nil {
					log.Printf("Warning: GetChannelReadState for %s: %v", chIDStr, err)
					return ""
				}
				return state.LastReadTS
			},
		}))

		app.SetActivityService(ui.NewActivityService(ui.ActivityServiceFuncs{
			Fetch: func(teamID ids.TeamID, limit int, cursor string, unreadOnly bool) tea.Msg {
				teamIDStr := string(teamID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				result, err := wctx.Client.GetActivityFeed(fetchCtx, limit, cursor, unreadOnly)
				if err != nil {
					log.Printf("Warning: GetActivityFeed(%s): %v", teamIDStr, err)
					// Return nil (no message) on failure so the existing
					// feed + badge are kept rather than blanked. Mirrors
					// the "nil = keep cache" pattern used for message
					// loads; a transient network error must not wipe the
					// Activity view.
					return nil
				}
				return ui.ActivityListLoadedMsg{
					TeamID:     teamIDStr,
					Items:      result.Items,
					NextCursor: result.NextCursor,
				}
			},
			Hydrate: func(teamID ids.TeamID, refs map[string][]string) tea.Msg {
				teamIDStr := string(teamID)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				fetchCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				bodies, err := wctx.Client.GetActivityMessages(fetchCtx, refs)
				if err != nil {
					log.Printf("Warning: GetActivityMessages(%s): %v", teamIDStr, err)
					// nil = keep whatever bodies we already have.
					return nil
				}
				return ui.ActivityBodiesLoadedMsg{TeamID: teamIDStr, Bodies: bodies}
			},
		}))

		app.SetReactionService(ui.NewReactionService(
			func(channelID ids.ChannelID, messageTS ids.MessageTS, emojiName string) error {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				return wctx.Client.AddReaction(ctx, string(channelID), string(messageTS), emojiName)
			},
			func(channelID ids.ChannelID, messageTS ids.MessageTS, emojiName string) error {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				return wctx.Client.RemoveReaction(ctx, string(channelID), string(messageTS), emojiName)
			},
			// LoadFrecent: not workspace-specific, captures only db.
			func(limit int) []reactionpicker.EmojiEntry {
				names, err := db.GetFrecentEmoji(limit)
				if err != nil {
					return nil
				}
				codeMap := emojiwidth.CodeMap()
				var entries []reactionpicker.EmojiEntry
				for _, name := range names {
					unicode := codeMap[":"+name+":"]
					entries = append(entries, reactionpicker.EmojiEntry{
						Name:    name,
						Unicode: unicode,
					})
				}
				return entries
			},
			// RecordFrecent: not workspace-specific, captures only db.
			func(emojiName string) {
				_ = db.RecordEmojiUse(emojiName)
			},
		))

		app.SetTypingSender(func(channelID string) {
			wctx := router.Active()
			if wctx == nil {
				return
			}
			_ = wctx.Client.SendTyping(channelID)
		})

	}

	// Bind all callbacks once. They read router.Active() at invocation.
	wireCallbacks(router)

	// Wire workspace switcher
	app.SetWorkspaceSwitcher(func(teamID string) tea.Msg {
		wctx := router.ByID(teamID)
		if wctx == nil {
			return nil
		}

		// Update active pointer; callbacks read router.Active() at
		// invocation time, so no closure rebinding is needed.
		activeTeamID = teamID
		router.Set(wctx)

		// Build external-user set from cached records so the mention
		// picker reflects Slack Connect / shared-channel guest status
		// for the workspace we're switching into. Best-effort: empty
		// map on error.
		external := map[string]bool{}
		if users, err := db.ListUsers(wctx.TeamID); err == nil {
			for _, u := range users {
				if u.IsExternal {
					external[u.ID] = true
				}
			}
		}

		return ui.WorkspaceSwitchedMsg{
			TeamID:           wctx.TeamID,
			TeamName:         wctx.TeamName,
			Domain:           wctx.Client.TeamSubdomain(),
			Theme:            cfg.ResolveTheme(teamID),
			SidebarWidth:     cfg.ResolveWidth(teamID),
			Channels:         wctx.Channels,
			FinderItems:      wctx.FinderItems,
			UserNames:        wctx.UserNames,
			ExternalUsers:    external,
			UserID:           wctx.UserID,
			CustomEmoji:      wctx.CustomEmoji,
			UserGroups:       wctx.UserGroups(),
			SectionsProvider: sectionsProviderAdapter{store: wctx.SectionStore},
		}
	})

	// Resolve general.default_workspace if set. We honor it only if
	// the matching token is actually configured; otherwise fall back
	// to "first workspace to connect wins" with a warning.
	defaultTeamID, err := cfg.TeamIDForDefaultWorkspace()
	if err != nil {
		log.Printf("Warning: %v; ignoring default_workspace setting", err)
		defaultTeamID = ""
	}
	if defaultTeamID != "" {
		found := false
		for _, t := range tokens {
			if t.TeamID == defaultTeamID {
				found = true
				break
			}
		}
		if !found {
			log.Printf("Warning: default_workspace resolves to team %q but no token is configured for it; ignoring", defaultTeamID)
			defaultTeamID = ""
		}
	}

	// firstReady gates the "first workspace to connect wins" logic when
	// no default_workspace is configured. sync.Once ensures exactly one
	// connect goroutine claims the initial active slot, eliminating the
	// race where two simultaneous WorkspaceReadyMsgs both observed
	// activeTeamID == "" and both set InitialActive=true.
	var firstReady sync.Once

	// Start the TUI immediately (shows loading overlay)
	p = tea.NewProgram(app)

	// Now that `p` exists, re-install the ImageContext with a real
	// SendMsg callback so the prefetcher can dispatch ImageReadyMsg
	// back into the program loop. This must happen before any
	// rendering kicks off prefetches whose completions would otherwise
	// be dropped on the floor.
	app.SetImageContext(buildImgCtx(p.Send))
	// Refresh the emoji PlaceContext with the real SendMsg so cold-
	// path emoji fetches dispatch EmojiImageReadyMsg into the loop
	// and surfaces re-render with the now-warm placement.
	app.SetEmojiContext(messages.EmojiContext{
		PlaceCtx: buildPlaceCtx(p.Send),
		Cells:    cfg.Appearance.EmojiCells,
		Customs:  nil, // CustomEmojisLoadedMsg fills this in
	})

	// Wire avatar-ready callback so the lazy AvatarFunc path's
	// background fetches invalidate the messages/thread caches and
	// re-render with the now-cached avatar. The callback fires from
	// the avatar.Cache worker goroutine; p.Send is safe to call
	// concurrently. Workspace-coarse: a single AvatarReadyMsg per
	// user (the inflight dedup in avatar.Cache ensures this).
	avatarCache.SetOnReady(func(userID string) {
		p.Send(messages.AvatarReadyMsg{UserID: userID})
	})

	// Launch workspace connections in background goroutines
	// Results are sent to the TUI via p.Send()
	for _, ot := range orderedTokens {
		go func(tok slackclient.Token) {
			wctx, err := connectWorkspace(ctx, tok, db, cfg, avatarCache, p)
			if err != nil {
				p.Send(ui.WorkspaceFailedMsg{TeamName: tok.TeamName})
				return
			}

			workspaces[wctx.TeamID] = wctx
			router.all[wctx.TeamID] = wctx
			wsMgr.AddWorkspace(wctx.TeamID, wctx.TeamName, "")

			// Decide whether this workspace becomes the active one.
			// If default_workspace resolved to a team ID, only that
			// workspace claims active. Otherwise the first to connect
			// claims it.
			isInitial := false
			if defaultTeamID != "" {
				if wctx.TeamID == defaultTeamID {
					isInitial = true
					router.Set(wctx)
					activeTeamID = wctx.TeamID
				}
				// else: not the configured default; never claim.
			} else {
				firstReady.Do(func() {
					isInitial = true
					router.Set(wctx)
					activeTeamID = wctx.TeamID
				})
			}

			// Build channel lookup maps for notifications
			channelNames := make(map[string]string, len(wctx.Channels))
			channelTypes := make(map[string]string, len(wctx.Channels))
			for _, ch := range wctx.Channels {
				channelNames[ch.ID] = ch.Name
				channelTypes[ch.ID] = ch.Type
			}

			// Start WebSocket for this workspace
			teamID := wctx.TeamID
			handler := &rtmEventHandler{
				program:         p,
				userNames:       wctx.UserNames,
				tsFormat:        tsFormat,
				db:              db,
				workspaceID:     teamID,
				isActive:        func() bool { return teamID == activeTeamID },
				notifier:        notifier,
				notifyCfg:       cfg.Notifications,
				currentUserID:   wctx.UserID,
				channelNames:    channelNames,
				channelTypes:    channelTypes,
				workspaceName:   wctx.TeamName,
				activeChannelID: func() string { return app.ActiveChannelID() },
				cfg:             cfg,
				wsCtx:           wctx,
				backfillGate:    dedupeGate{window: 30 * time.Second},
			}
			wctx.RTMHandler = handler
			wctx.ConnMgr = slackclient.NewConnectionManager(wctx.Client, handler)
			go wctx.ConnMgr.Run(ctx)

			// Build external-user set from cached records so the
			// mention picker can flag Slack Connect / shared-channel
			// guests on first render, without waiting for fresh
			// userResolver lookups. Best-effort: empty map on error
			// (the picker just won't flag anyone until live resolution
			// fires).
			external := map[string]bool{}
			if users, err := db.ListUsers(wctx.TeamID); err == nil {
				for _, u := range users {
					if u.IsExternal {
						external[u.ID] = true
					}
				}
			}

			p.Send(ui.WorkspaceReadyMsg{
				TeamID:           wctx.TeamID,
				TeamName:         wctx.TeamName,
				Domain:           wctx.Client.TeamSubdomain(),
				Theme:            cfg.ResolveTheme(wctx.TeamID),
				SidebarWidth:     cfg.ResolveWidth(wctx.TeamID),
				Channels:         wctx.Channels,
				FinderItems:      wctx.FinderItems,
				UserNames:        wctx.UserNames,
				ExternalUsers:    external,
				UserID:           wctx.UserID,
				CustomEmoji:      wctx.CustomEmoji,  // empty at this point; filled by the goroutine below
				UserGroups:       wctx.UserGroups(), // empty at this point; filled by the goroutine below
				SectionsProvider: sectionsProviderAdapter{store: wctx.SectionStore},
				InitialActive:    isInitial,
				LastChannelID:    mostRecentlyVisitedChannel(wctx.LastVisitedByChannel),
			})

			// Fetch workspace custom emojis in the background. When done,
			// send a follow-up so the active compose can refresh its
			// emoji picker entries. Best-effort: failure leaves the picker
			// using built-ins only.
			go func(teamID string) {
				emojis, err := wctx.Client.ListCustomEmoji(ctx)
				if err != nil {
					return
				}
				wctx.CustomEmoji = emojis
				p.Send(ui.CustomEmojisLoadedMsg{
					TeamID:      teamID,
					CustomEmoji: emojis,
				})
			}(wctx.TeamID)

			// Fetch workspace usergroups in the background. When done,
			// send a follow-up so render caches and compose pickers can
			// refresh for the active workspace. Best-effort: failure leaves
			// bare subteam mentions rendered as "@group".
			go func(teamID string) {
				groups, err := wctx.Client.GetUserGroups(ctx)
				if err != nil {
					log.Printf("usergroups fetch for %s failed: %v (subteam mentions will render as @group)", wctx.TeamName, err)
					return
				}
				byID := usergroupHandles(groups)
				wctx.SetUserGroups(byID)
				p.Send(ui.UserGroupsLoadedMsg{
					TeamID:     teamID,
					UserGroups: byID,
				})
			}(wctx.TeamID)

			// Background fetch of all public channels so the finder can show
			// channels the user is not yet a member of. Slow on big workspaces;
			// must not block initial workspace readiness.
			go fetchBrowseableChannels(ctx, wctx, p)

			// Resolve unknown DM user names in background
			if len(wctx.UnresolvedDMs) > 0 {
				go func() {
					for _, dm := range wctx.UnresolvedDMs {
						resolved, isBot := resolveUser(wctx.Client, dm.UserID, wctx.UserNames, db, avatarCache)
						if isBot {
							wctx.BotUserIDs[dm.UserID] = true
						}
						if resolved != dm.UserID {
							p.Send(ui.DMNameResolvedMsg{
								ChannelID:   dm.ChannelID,
								DisplayName: resolved,
								IsBot:       isBot,
							})
						}
					}
				}()
			}
		}(ot.Token)
	}

	// Wake-from-sleep detector. The WS read deadline is 60 s; sleeps
	// shorter than that don't tear down the TCP connection, so
	// OnConnect never fires and the read-state catch-up in
	// runChannelPhase doesn't run. The clock-jump heuristic catches
	// these short sleeps and forces a backfill per workspace.
	wakeCtx, wakeCancel := context.WithCancel(context.Background())
	defer wakeCancel()
	go wake.New(10*time.Second, 5*time.Second, func(elapsed time.Duration) {
		debuglog.Backfill("wake detected: elapsed=%v — triggering catch-up across all workspaces", elapsed)
		for _, wctx := range router.all {
			if wctx == nil || wctx.RTMHandler == nil {
				continue
			}
			wctx.RTMHandler.triggerBackfill("wake")
		}
	}).Run(wakeCtx)

	_, err = p.Run()

	// Clean up connection managers
	for _, wctx := range workspaces {
		if wctx.ConnMgr != nil {
			wctx.ConnMgr.Stop()
		}
	}

	return err
}

func usergroupHandles(groups []slack.UserGroup) map[string]string {
	byID := make(map[string]string, len(groups))
	for _, g := range groups {
		handle := g.Handle
		if handle == "" {
			// Fall back to the display name, slugified: a name like
			// "Platform Team" would otherwise become a handle with a
			// space in it, which neither the @-token composer
			// autocomplete nor the word-boundary send translation can
			// round-trip.
			handle = slugifyHandle(g.Name)
		}
		if handle != "" {
			byID[g.ID] = handle
		}
	}
	return byID
}

// slugifyHandle turns a usergroup display name into a mention-safe
// handle: lowercased, with every run of characters Slack handles don't
// use collapsed to a single "-" ("Platform Team" -> "platform-team").
// Returns "" when nothing usable survives, so the caller can skip the
// group entirely rather than register an unmentionable handle.
func slugifyHandle(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	return b.String()
}

func connectWorkspace(ctx context.Context, token slackclient.Token, db *cache.DB, cfg config.Config, avatarCache *avatar.Cache, p *tea.Program) (*WorkspaceContext, error) {
	client := slackclient.NewClient(token.AccessToken, token.Cookie)
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connecting %s: %w", token.TeamName, err)
	}

	wctx := &WorkspaceContext{
		Client:               client,
		TeamID:               client.TeamID(),
		TeamName:             token.TeamName,
		UserID:               client.UserID(),
		UserNames:            make(map[string]string),
		AvatarURLs:           &sync.Map{},
		UserNamesByHandle:    make(map[string]string),
		BotUserIDs:           make(map[string]bool),
		CustomEmoji:          make(map[string]string),
		LastVisitedByChannel: make(map[string]int64),
	}
	wctx.SubscriptionsAvailable = true

	// Seed user names + bot flags from cache (fast, local). The bot
	// flag is what lets channel construction below classify app DMs
	// into "app" vs "dm" without waiting for the network fetch.
	cachedUsers, _ := db.ListUsers(client.TeamID())
	for _, u := range cachedUsers {
		name := u.DisplayName
		if name == "" {
			name = u.Name
		}
		wctx.UserNames[u.ID] = name
		if u.Name != "" {
			wctx.UserNamesByHandle[u.Name] = name
		}
		if u.IsBot {
			wctx.BotUserIDs[u.ID] = true
		}
		// Record the avatar URL for lazy fetch on first render.
		//
		// We intentionally do NOT bulk-Preload every cached user here.
		// A typical Slack workspace has tens of thousands of cached
		// users, virtually none of whom are visible on first paint. The
		// old eager-Preload spawned one goroutine per cached user, and
		// for each one rendered into a kitty graphics APC upload that
		// was synchronously written to os.Stdout. On kitty the terminal
		// applies flow control while decoding the upload PNGs, which
		// blocked the bubbletea View() goroutine's stdout writes and
		// presented as a multi-minute startup hang with idle CPU. The
		// lazy AvatarFunc path (see SetAvatarFunc) triggers a single
		// Preload per userID on first render demand, deduped by
		// avatar.Cache's inflight set.
		if u.AvatarURL != "" {
			wctx.AvatarURLs.Store(u.ID, u.AvatarURL)
		}
	}

	// Construct the per-workspace async user resolver. It writes
	// resolved display names to the cache DB and emits
	// UserResolvedMsg back into the bubbletea program; the UI's
	// Update handler patches the in-memory userNames map on the
	// UI goroutine via Model.PatchUserName (the single safe writer
	// for that shared map). p may be nil in tests, in which case
	// the resolver's send callback is a no-op.
	wctx.UserResolver = newUserResolver(
		wctx.TeamID,
		wctx.Client,
		db,
		avatarCache,
		func(msg tea.Msg) {
			if p != nil {
				p.Send(msg)
			}
		},
	)

	// Per-workspace channel-membership manager. *slackclient.Client
	// structurally satisfies membership.ConversationMemberAPI; the
	// user resolver satisfies membership.UserResolver. The push
	// callback funnels member-set snapshots into the App via
	// ui.ChannelMembershipMsg for picker hydration.
	wctx.Membership = membership.New(
		wctx.TeamID,
		wctx.Client,
		db,
		func(channelID string, memberIDs []string) {
			if p != nil {
				p.Send(ui.ChannelMembershipMsg{ChannelID: channelID, MemberIDs: memberIDs})
			}
		},
		wctx.UserResolver,
	)

	// Seed last-visited timestamps for the channel finder's recency
	// sort. Best-effort: failure is logged and the map stays empty,
	// which means the finder uses its default order until the user
	// starts visiting channels.
	if visits, err := db.GetChannelVisits(client.TeamID()); err != nil {
		log.Printf("warning: loading channel visits for %s: %v", token.TeamName, err)
	} else {
		wctx.LastVisitedByChannel = visits
	}

	// Initialize Slack-native section store if enabled. Bootstrap is
	// best-effort: failure is logged, the field stays nil, and the
	// resolver falls through to config-glob behavior. Doing this
	// before GetChannels means the first pass through buildChannelItem
	// already sees a Ready store.
	if cfg.EffectiveUseSlackSections(client.TeamID()) {
		store := service.NewSectionStore()
		if err := store.Bootstrap(ctx, client); err != nil {
			log.Printf("section store bootstrap for %s failed: %v (falling back to config sections)", token.TeamName, err)
		} else {
			// Slack's users.channelSections.list returns the stars section
			// with an empty channel_ids array (it doesn't populate built-in
			// section types). stars.list is the authoritative source for
			// which channels the user has starred; fetch and inject so the
			// sidebar can render the Starred header. Best-effort: on error
			// the stars section stays empty and includeInSidebar hides it.
			if starIDs, err := client.GetStarredChannels(ctx); err != nil {
				log.Printf("stars.list for %s failed: %v (starred channels will be hidden)", token.TeamName, err)
			} else if len(starIDs) > 0 {
				store.PopulateStars(starIDs)
			}
			wctx.SectionStore = store
			// One-time info log when the user has both Slack sections
			// active AND a non-empty [sections.*] config — the latter
			// is being shadowed.
			hasGlobSections := len(cfg.Sections) > 0
			if ws, ok := cfg.WorkspaceByTeamID(client.TeamID()); ok && len(ws.Sections) > 0 {
				hasGlobSections = true
			}
			if hasGlobSections {
				log.Printf("workspace %s: using Slack-native sections; [sections.*] from config are shadowed (set use_slack_sections=false to disable)", token.TeamName)
			}
		}
	}

	// Initialize the mute store. Best-effort: failure is logged and the
	// field stays nil; the sidebar then renders every channel as
	// unmuted (the conservative default). pref_change WS events for
	// muted_channels can still rebuild the store mid-session via
	// MuteStore.ApplyPrefChange even if this initial fetch failed.
	{
		store := service.NewMuteStore()
		if err := store.Bootstrap(ctx, client); err != nil {
			log.Printf("mute store bootstrap for %s failed: %v (channels will render as unmuted until first pref_change)", token.TeamName, err)
		} else {
			ids := store.MutedChannels()
			log.Printf("mute store bootstrap for %s: %d muted channel(s) loaded: %v", token.TeamName, len(ids), ids)
		}
		// Assign even if not Ready — the pref_change handler can fill
		// it in later, and IsMuted is a safe no-op while not ready.
		wctx.MuteStore = store
	}

	// Background user fetch
	go func() {
		users, err := client.GetUsers(ctx)
		if err != nil {
			return
		}
		for _, u := range users {
			name := u.Profile.DisplayName
			if name == "" {
				name = u.RealName
			}
			if name == "" {
				name = u.Name
			}
			wctx.UserNames[u.ID] = name
			if u.Name != "" {
				wctx.UserNamesByHandle[u.Name] = name
			}
			isBot := u.IsBot || u.IsAppUser
			if isBot {
				wctx.BotUserIDs[u.ID] = true
			}
			// Slack Connect / shared-channel guests have a TeamID
			// that differs from this workspace's home TeamID. Empty
			// TeamID is treated as internal (under-detect rather than
			// falsely flag). Mirrors userResolver.Request semantics.
			isExternal := u.TeamID != "" && u.TeamID != client.TeamID()
			db.UpsertUser(cache.User{
				ID:          u.ID,
				WorkspaceID: client.TeamID(),
				Name:        u.Name,
				DisplayName: name,
				AvatarURL:   u.Profile.Image32,
				Presence:    "away",
				IsBot:       isBot,
				IsExternal:  isExternal,
			})
			// Record the avatar URL for lazy fetch (mirrors the cached-
			// user seed above). The eager Preload was the second wave
			// of the startup avatar burst — equally large on big
			// workspaces — and is replaced by on-demand fetches driven
			// by AvatarFunc.
			if u.Profile.Image32 != "" {
				wctx.AvatarURLs.Store(u.ID, u.Profile.Image32)
			}
		}
	}()

	// Fetch channels
	channels, err := client.GetChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching channels for %s: %w", token.TeamName, err)
	}

	for _, ch := range channels {
		item, finderItem := buildChannelItem(ch, wctx, cfg, client.TeamID())
		upsertChannelInDB(db, ch, item.Type, client.TeamID())

		if ch.IsIM {
			if _, ok := wctx.UserNames[ch.User]; !ok {
				wctx.UnresolvedDMs = append(wctx.UnresolvedDMs, UnresolvedDM{
					ChannelID: ch.ID,
					UserID:    ch.User,
				})
			}
			if cachedUser, err := db.GetUser(ch.User); err == nil && cachedUser.Presence != "" {
				item.Presence = cachedUser.Presence
				finderItem.Presence = cachedUser.Presence
			}
		}
		wctx.Channels = append(wctx.Channels, item)
		finderItem.LastVisited = wctx.LastVisitedByChannel[ch.ID]
		wctx.FinderItems = append(wctx.FinderItems, finderItem)
	}

	// Fetch unread counts
	unreadCounts, threadsAgg, ucErr := client.GetUnreadCounts()
	if ucErr != nil {
		debuglog.Cache("workspace_unread_bootstrap: team=%s GetUnreadCounts failed: %v", token.TeamName, ucErr)
	}
	wctx.ThreadsHasUnreads = threadsAgg.HasUnreads
	// Boot applies an authoritative FULL snapshot: reset every channel
	// in the workspace to read, then set the ones client.counts reports
	// unread. This runs BEFORE the WebSocket goes live (ConnMgr.Run is
	// started by the caller after connectWorkspace returns), so the
	// reset cannot race an inbound *_marked event.
	//
	// Guard on ucErr only (not len>0): a successful call returning zero
	// unreads legitimately means "everything is read" and must clear
	// stale dots carried over from a prior session. A FAILED call must
	// NOT reset — that would wipe every dot with no data to restore.
	if ucErr == nil {
		updates := make([]cache.ChannelReadStateUpdate, 0, len(unreadCounts))
		for _, u := range unreadCounts {
			updates = append(updates, cache.ChannelReadStateUpdate{
				ChannelID:  u.ChannelID,
				LastReadTS: u.LastRead, // may be ""; ReplaceWorkspaceReadState preserves existing in that case
				HasUnread:  u.HasUnread,
			})
		}
		if err := db.ReplaceWorkspaceReadState(client.TeamID(), updates); err != nil {
			log.Printf("Warning: bootstrap ReplaceWorkspaceReadState for team=%s: %v", token.TeamName, err)
		}
	}
	mutedItemCount := 0
	for _, c := range wctx.Channels {
		if c.IsMuted {
			mutedItemCount++
		}
	}
	log.Printf("workspace %s: %d/%d channel items marked IsMuted after build", token.TeamName, mutedItemCount, len(wctx.Channels))

	// Bootstrap-time mute summary so a user can grep
	// `[cache] workspace_unread_bootstrap` after launch and see how
	// many channels are muted in this workspace. The per-channel
	// unread detail log that used to live here was driven by
	// ChannelItem.UnreadCount, which no longer exists -- unread state
	// is now sourced exclusively from the read-state DB (see
	// db.GetChannelReadState) and any equivalent dump would live
	// alongside the DB write path in updateReadStateFromCounts.
	if debuglog.Enabled() {
		var mutedChans int
		for _, ch := range wctx.Channels {
			if ch.IsMuted {
				mutedChans++
			}
		}
		debuglog.Cache("workspace_unread_bootstrap: team=%s total=%d muted=%d threads_has_unreads=%v threads_unread=%d",
			token.TeamName, len(wctx.Channels), mutedChans, threadsAgg.HasUnreads, threadsAgg.UnreadCount)
	}

	// Finder items are built alongside the sidebar items in the loop above
	// (see buildChannelItem). The user is a member of every channel returned
	// by GetChannels (it's backed by users.conversations), so those entries
	// have Joined=true. A separate background fetch surfaces non-joined
	// public channels for browsing -- see startBrowseableChannelsFetch.

	return wctx, nil
}

// fetchBrowseableChannels fetches every public channel in the workspace and
// sends a BrowseableChannelsLoadedMsg to the TUI with the entries the user
// has NOT joined. Joined entries are skipped to avoid duplicates with the
// existing finder list. Runs in a background goroutine; failures are logged
// but otherwise ignored (the finder simply continues to show only joined
// channels).
func fetchBrowseableChannels(ctx context.Context, wctx *WorkspaceContext, p *tea.Program) {
	channels, err := wctx.Client.GetAllPublicChannels(ctx)
	if err != nil {
		log.Printf("warning: fetching browseable channels for %s: %v", wctx.TeamName, err)
		return
	}

	// Build set of joined IDs so we can skip them.
	joined := make(map[string]struct{}, len(wctx.Channels))
	for _, ch := range wctx.Channels {
		joined[ch.ID] = struct{}{}
	}

	browseable := make([]channelfinder.Item, 0, len(channels))
	for _, ch := range channels {
		if _, ok := joined[ch.ID]; ok {
			continue
		}
		browseable = append(browseable, channelfinder.Item{
			ID:          ch.ID,
			Name:        ch.Name,
			Type:        "channel",
			Joined:      false,
			LastVisited: wctx.LastVisitedByChannel[ch.ID],
		})
	}

	// Persist on the workspace context so future workspace switches preserve
	// the merged list.
	wctx.FinderItems = append(wctx.FinderItems, browseable...)

	if p != nil {
		p.Send(ui.BrowseableChannelsLoadedMsg{
			TeamID: wctx.TeamID,
			Items:  browseable,
		})
	}
}

// extractAttachments converts slack-go File entries into the UI's
// Attachment representation.
//
// URL preference depends on the kind:
//   - For images we use an unauthenticated thumbnail URL (files.slack.com/...)
//     when available so the link opens the picture directly in a browser
//     instead of bouncing through Slack's auth flow / launching the desktop
//     client. We pick a reasonably large thumbnail (1024 -> 720 -> 480 ->
//     360 -> 160 -> 80 -> 64) and fall back to PermalinkPublic, Permalink,
//     and finally URLPrivate.
//   - For non-images (PDFs, etc.) we use Permalink, since those files are
//     intentionally gated by Slack auth and opening the workspace UI is the
//     correct flow.
//
// Title is used for the display name when present (Slack lets users set a
// title separate from the original filename); otherwise we fall back to
// the filename. Image mimetypes get the "image" kind so the renderer can
// show [Image]; everything else gets "file" -> [File].
func extractAttachments(files []slack.File) []messages.Attachment {
	if len(files) == 0 {
		return nil
	}
	out := make([]messages.Attachment, 0, len(files))
	for _, f := range files {
		kind := "file"
		if strings.HasPrefix(f.Mimetype, "image/") {
			kind = "image"
		}
		name := f.Title
		if name == "" {
			name = f.Name
		}
		att := messages.Attachment{Kind: kind, Name: name, URL: pickAttachmentURL(f, kind)}
		if kind == "image" {
			att.FileID = f.ID
			att.Mime = f.Mimetype
			att.Thumbs = collectThumbs(f)
		}
		out = append(out, att)
	}
	return out
}

// extractBlocks converts a slack.Blocks value to our typed block
// slice for storage on a MessageItem. Empty input returns nil.
func extractBlocks(b slack.Blocks) []blockkit.Block {
	return blockkit.Parse(b)
}

// extractLegacyAttachments converts slack-go Attachment slice into
// our LegacyAttachment type. Empty input returns nil.
func extractLegacyAttachments(a []slack.Attachment) []blockkit.LegacyAttachment {
	return blockkit.ParseAttachments(a)
}

// collectThumbs builds a slice of ThumbSpec from a slack.File's thumb_*
// fields. Tiers with an empty URL or non-positive dimensions are skipped.
// The slice is ordered smallest-to-largest, matching the order Slack
// returns them in the file metadata.
func collectThumbs(f slack.File) []messages.ThumbSpec {
	var out []messages.ThumbSpec
	add := func(url string, w, h int) {
		if url != "" && w > 0 && h > 0 {
			out = append(out, messages.ThumbSpec{URL: url, W: w, H: h})
		}
	}
	add(f.Thumb360, f.Thumb360W, f.Thumb360H)
	add(f.Thumb480, f.Thumb480W, f.Thumb480H)
	add(f.Thumb720, f.Thumb720W, f.Thumb720H)
	add(f.Thumb960, f.Thumb960W, f.Thumb960H)
	add(f.Thumb1024, f.Thumb1024W, f.Thumb1024H)
	return out
}

// pickAttachmentURL chooses the best URL for a slack.File based on its kind.
// See extractAttachments for the rationale.
func pickAttachmentURL(f slack.File, kind string) string {
	if kind == "image" {
		// Try thumbnails from largest to smallest -- these are direct image
		// bytes hosted at files.slack.com and openable without auth.
		for _, u := range []string{f.Thumb1024, f.Thumb720, f.Thumb480, f.Thumb360, f.Thumb160, f.Thumb80, f.Thumb64} {
			if u != "" {
				return u
			}
		}
		if f.PermalinkPublic != "" {
			return f.PermalinkPublic
		}
	}
	if f.Permalink != "" {
		return f.Permalink
	}
	return f.URLPrivate
}

// lookupUserCached returns the display name for userID using only
// local sources: the in-memory userNames map and the cached users
// table. Never hits the network and NEVER writes to userNames — safe
// to call from goroutines off the UI loop (e.g. the search cmd),
// where a map write would race the UI goroutine (see the
// concurrent-map-writes note on userResolver.Request). Returns
// ("", false) when the user is unknown.
func lookupUserCached(userID string, userNames map[string]string, db *cache.DB) (string, bool) {
	if userID == "" {
		return "", false
	}
	if name, ok := userNames[userID]; ok && name != "" {
		return name, true
	}
	if db != nil {
		if u, err := db.GetUser(userID); err == nil {
			name := u.DisplayName
			if name == "" {
				name = u.Name
			}
			if name != "" {
				return name, true
			}
		}
	}
	return "", false
}

// resolveUserCached is lookupUserCached plus map memoization: a DB hit
// is written back to userNames so subsequent lookups skip SQLite.
// UI-goroutine callers only — the map write is what lookupUserCached
// exists to avoid. Returns ("", false) when the user is unknown —
// caller is expected to fall back to userID-as-name and enqueue an
// async lookup via wctx.UserResolver.Request.
func resolveUserCached(userID string, userNames map[string]string, db *cache.DB) (string, bool) {
	name, ok := lookupUserCached(userID, userNames, db)
	if ok {
		userNames[userID] = name
	}
	return name, ok
}

// resolveUser ensures we have the display name and avatar for a user.
// If the user is unknown, fetches their profile from Slack on demand.
// Returns the resolved display name (or the userID as a fallback) and a
// boolean indicating whether the user is a Slack app or bot. The bool
// is best-effort: if the user was already in the userNames cache and
// the avatar lookup hasn't fired, we don't have a fresh IsBot signal
// and return false. Callers that care (the unresolved-DM goroutine)
// only invoke resolveUser for users not yet in the cache, so the
// fast-path miss is irrelevant for them.
func resolveUser(client *slackclient.Client, userID string, userNames map[string]string, db *cache.DB, avatarCache *avatar.Cache) (string, bool) {
	if name, ok := userNames[userID]; ok {
		// Check if avatar is also cached
		if avatarCache.Get(userID) == "" {
			// Have name but no avatar — try to fetch profile for avatar URL
			if u, err := client.GetUserProfile(userID); err == nil {
				isBot := u.IsBot || u.IsAppUser
				isExternal := u.TeamID != "" && u.TeamID != client.TeamID()
				avatarCache.Preload(userID, u.Profile.Image32)
				db.UpsertUser(cache.User{
					ID:          userID,
					WorkspaceID: client.TeamID(),
					Name:        u.Name,
					DisplayName: name,
					AvatarURL:   u.Profile.Image32,
					Presence:    "away",
					IsBot:       isBot,
					IsExternal:  isExternal,
				})
				return name, isBot
			}
		}
		return name, false
	}
	// Unknown user — fetch profile
	if u, err := client.GetUserProfile(userID); err == nil {
		name := u.Profile.DisplayName
		if name == "" {
			name = u.RealName
		}
		if name == "" {
			name = u.Name
		}
		isBot := u.IsBot || u.IsAppUser
		isExternal := u.TeamID != "" && u.TeamID != client.TeamID()
		userNames[userID] = name
		avatarCache.Preload(userID, u.Profile.Image32)
		db.UpsertUser(cache.User{
			ID:          userID,
			WorkspaceID: client.TeamID(),
			Name:        u.Name,
			DisplayName: name,
			AvatarURL:   u.Profile.Image32,
			Presence:    "away",
			IsBot:       isBot,
			IsExternal:  isExternal,
		})
		return name, isBot
	}
	return userID, false
}

// messageAuthor resolves the display identity for a fetched message.
// Human messages carry a `user` ID, resolved (and lazily fetched) the
// usual way. Bot messages (bot_message) have an empty `user` and only a
// `bot_id` + `username`; those are keyed on the bot_id, use the message's
// username for the name, and enqueue a bots.info lookup for the avatar
// (and a name fallback). The returned userID is what both the cache row
// and the MessageItem are keyed on so the avatar pipeline can attach.
func messageAuthor(m slack.Message, userNames map[string]string, db *cache.DB, router *workspaceRouter) (userID, userName string) {
	if m.User != "" {
		name, ok := resolveUserCached(m.User, userNames, db)
		if !ok {
			name = m.User
			if router != nil {
				if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
					wctx.UserResolver.Request(m.User)
				}
			}
		}
		return m.User, name
	}
	if m.BotID != "" {
		name := m.Username
		if name == "" {
			if cached, ok := resolveUserCached(m.BotID, userNames, db); ok {
				name = cached
			} else {
				name = m.BotID
			}
		}
		if router != nil {
			if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
				wctx.UserResolver.RequestBot(m.BotID, m.Username)
			}
		}
		return m.BotID, name
	}
	return m.User, m.User
}

func fetchOlderMessages(client *slackclient.Client, channelID, latestTS string, db *cache.DB, userNames map[string]string, tsFormat string, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchOlderMessages: channel=%s latest_ts=%s entry", channelID, latestTS)
	start := time.Now()
	history, err := client.GetOlderHistory(ctx, channelID, 50, latestTS)
	if err != nil {
		debuglog.Cache("fetchOlderMessages: GetOlderHistory %s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := convertAndCacheHistory(client, channelID, history, db, userNames, tsFormat, router)

	debuglog.Cache("fetchOlderMessages: channel=%s latest_ts=%s result %s dur_ms=%d (older history backfill)",
		channelID, latestTS, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	return msgItems
}

// fetchMessagesAround fetches a history window centered on targetTS
// for jump-to-message navigation. Mirrors fetchOlderMessages: upserts
// into the cache, converts to MessageItems, returns ascending by TS.
// Returns nil on network failure AND when the fetch succeeds but the
// window is empty — callers cannot distinguish the two from the
// return value alone (the FetchAround closure treats both as
// failure).
func fetchMessagesAround(client *slackclient.Client, channelID, targetTS string, db *cache.DB, userNames map[string]string, tsFormat string, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchMessagesAround: channel=%s target_ts=%s entry", channelID, targetTS)
	start := time.Now()
	history, err := client.GetHistoryAround(ctx, channelID, targetTS, 25)
	if err != nil {
		debuglog.Cache("fetchMessagesAround: GetHistoryAround %s @ %s: %v dur_ms=%d (returning nil)",
			channelID, targetTS, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := convertAndCacheHistory(client, channelID, history, db, userNames, tsFormat, router)

	debuglog.Cache("fetchMessagesAround: channel=%s target_ts=%s result %s dur_ms=%d (jump-to-message window)",
		channelID, targetTS, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	return msgItems
}

// convertAndCacheHistory is the shared tail of fetchOlderMessages and
// fetchMessagesAround: upserts each fetched message (and its
// reactions) into the cache, resolves user names, converts to
// messages.MessageItem, and reverses the slice from Slack's
// newest-first order to the ascending-by-TS convention used
// throughout slk.
func convertAndCacheHistory(client *slackclient.Client, channelID string, history []slack.Message, db *cache.DB, userNames map[string]string, tsFormat string, router *workspaceRouter) []messages.MessageItem {
	var msgItems []messages.MessageItem
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("convertAndCacheHistory: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		authorID, userName := messageAuthor(m, userNames, db, router)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      authorID,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})

		// Convert reactions
		var reactions []messages.ReactionItem
		for _, r := range m.Reactions {
			hasReacted := false
			for _, uid := range r.Users {
				if uid == client.UserID() {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Name,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.Users,
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:                m.Timestamp,
			UserID:            authorID,
			UserName:          userName,
			Text:              m.Text,
			Timestamp:         formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:          m.ThreadTimestamp,
			ReplyCount:        m.ReplyCount,
			Subtype:           m.SubType,
			Reactions:         reactions,
			Attachments:       extractAttachments(m.Files),
			Blocks:            extractBlocks(m.Blocks),
			LegacyAttachments: extractLegacyAttachments(m.Attachments),
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
}

// summarizeMessages collapses a slice of messages.MessageItem into a
// compact "count=N oldest=<ts> newest=<ts>" string for [cache] log
// lines. Empty/nil slices return "count=0" with no ts fields. Assumes
// the slice is sorted ascending by TS (the convention everywhere in
// slk's cache and fetch paths).
func summarizeMessages(items []messages.MessageItem) string {
	if len(items) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(items), items[0].TS, items[len(items)-1].TS)
}

// summarizeCachedRows is summarizeMessages's twin for raw cache.Message
// rows (used by loadCachedMessages / loadCachedThreadReplies).
func summarizeCachedRows(rows []cache.Message) string {
	if len(rows) == 0 {
		return "count=0"
	}
	return fmt.Sprintf("count=%d oldest=%s newest=%s",
		len(rows), rows[0].TS, rows[len(rows)-1].TS)
}

// markChannelReadAsync fires Slack's conversations.mark plus the local
// LastReadTS persistence in a background goroutine. Returns
// immediately. wctx may be nil (returns silently in that case).
func markChannelReadAsync(
	ctx context.Context,
	wctx *WorkspaceContext,
	db *cache.DB,
	p *tea.Program,
	channelID, ts string,
) {
	if wctx == nil || ts == "" {
		return
	}
	client := wctx.Client
	go func() {
		_ = client.MarkChannel(ctx, channelID, ts)
		if err := db.UpdateChannelReadState(channelID, ts, false); err != nil {
			log.Printf("Warning: failed to update read state in markChannelReadAsync %s/%s: %v", channelID, ts, err)
		}
		if p != nil {
			p.Send(ui.ChannelMarkedReadMsg{ChannelID: channelID})
		}
	}()
}

// loadCachedMessages reads up to 50 cached messages for a channel from
// SQLite and reconstructs []messages.MessageItem with the same fidelity
// as fetchChannelMessages — including reactions and (when raw_json is
// present) files / blocks / legacy attachments.
//
// Returns nil on cache miss (no rows for the channel) or any DB error;
// callers treat nil as "fall through to the network fetch path".
//
// selfUserID is used to compute ReactionItem.HasReacted; it is NOT used
// to drive any network call. Cache reads must remain offline-capable —
// unknown user IDs render with their userID as a fallback rather than
// triggering a fresh GetUserProfile RPC. Resolving them on-demand would
// defeat the cache-first goal (and is what fetchChannelMessages already
// does on the network path, populating userNames for next time).
//
// raw_json unmarshal failures on a single row degrade gracefully: that
// row renders as text-only (no attachments / blocks / legacy
// attachments) without aborting the rest of the load.
// enrichPerfStats accumulates per-sub-call timing across one
// loadCachedMessages invocation so the [perf] log can attribute the
// channel-open hot path's cost across the three known N+1 sources.
// Allocated only when debuglog.Enabled(); enrichCachedRow checks for
// nil and skips the time.Now() / time.Since() calls otherwise.
type enrichPerfStats struct {
	getUserCalls   int
	getUserTotal   time.Duration
	getReactCalls  int
	getReactTotal  time.Duration
	unmarshalCalls int
	unmarshalTotal time.Duration
}

func loadCachedMessages(
	db *cache.DB,
	selfUserID string,
	channelID string,
	userNames map[string]string,
	tsFormat string,
	router *workspaceRouter,
) []messages.MessageItem {
	if db == nil {
		debuglog.Cache("loadCachedMessages: channel=%s db=nil", channelID)
		return nil
	}
	debuglog.Cache("loadCachedMessages: channel=%s entry", channelID)

	// Perf instrumentation: wall-clock the whole call and attribute the
	// loop cost across GetReactions / GetUser / json.Unmarshal so we can
	// tell which N+1 dominates the channel-open hot path. Gated on
	// debuglog.Enabled() (atomic.Bool load -> zero cost when SLK_DEBUG
	// is unset). The TODO at the GetReactions callsite below predicts
	// GetReactions as the dominant cost; this trace will confirm or
	// refute it.
	var perfStart time.Time
	var stats *enrichPerfStats
	if debuglog.Enabled() {
		perfStart = time.Now()
		stats = &enrichPerfStats{}
	}

	getMsgsStart := time.Now()
	rows, err := db.GetMessages(channelID, 50, "")
	getMsgsDur := time.Since(getMsgsStart)
	if err != nil {
		debuglog.Cache("loadCachedMessages: GetMessages %s: %v", channelID, err)
		return nil
	}
	if len(rows) == 0 {
		debuglog.Cache("loadCachedMessages: channel=%s result count=0 (no cached rows)", channelID)
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedMessages", router, stats))
	}
	debuglog.Cache("loadCachedMessages: channel=%s result %s", channelID, summarizeMessages(out))
	if stats != nil {
		debuglog.Perf("loadCachedMessages channel=%s N=%d total=%s GetMessages=%s GetReactions(n=%d)=%s GetUser(n=%d)=%s json.Unmarshal(n=%d)=%s",
			channelID, len(rows), time.Since(perfStart), getMsgsDur,
			stats.getReactCalls, stats.getReactTotal,
			stats.getUserCalls, stats.getUserTotal,
			stats.unmarshalCalls, stats.unmarshalTotal)
	}
	return out
}

// enrichCachedRow reconstructs a single messages.MessageItem from a
// cache.Message row using the same fidelity as the network fetchers:
// 3-tier username fallback, per-row reactions, and raw_json
// reconstruction of files / blocks / legacy attachments.
//
// userNames may be nil — username resolution still works via the
// cached users table or the userID fallback, but no memoization
// occurs.
//
// raw_json unmarshal failures degrade the row to text-only without
// failing the caller. logPrefix tags the per-row log lines so callers
// (loadCachedMessages vs loadCachedThreadReplies) remain
// distinguishable in logs.
func enrichCachedRow(
	db *cache.DB,
	selfUserID string,
	channelID string,
	m cache.Message,
	userNames map[string]string,
	tsFormat string,
	logPrefix string,
	router *workspaceRouter,
	stats *enrichPerfStats,
) messages.MessageItem {
	// Bot rows cached before the bot-identity fix have an empty UserID but
	// carry bot_id + username in raw_json. Re-key them on the bot_id and
	// resolve the avatar/name via bots.info so cached bot messages render
	// like freshly-fetched ones (without this they stay blank until a
	// live re-fetch, which never reaches messages older than the latest 50).
	effUserID := m.UserID
	botUsername := ""
	if effUserID == "" && m.RawJSON != "" {
		var rawBot struct {
			BotID    string `json:"bot_id"`
			Username string `json:"username"`
		}
		if json.Unmarshal([]byte(m.RawJSON), &rawBot) == nil && rawBot.BotID != "" {
			effUserID = rawBot.BotID
			botUsername = rawBot.Username
			if router != nil {
				if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
					wctx.UserResolver.RequestBot(rawBot.BotID, rawBot.Username)
				}
			}
		}
	}

	// Resolve username from the in-memory map first; fall back to
	// the cached users table; finally fall back to the bot username /
	// user ID so the row still renders something readable.
	var userName string
	if userNames != nil {
		userName = userNames[effUserID]
	}
	if userName == "" && effUserID != "" {
		var t0 time.Time
		if stats != nil {
			t0 = time.Now()
		}
		u, err := db.GetUser(effUserID)
		if stats != nil {
			stats.getUserCalls++
			stats.getUserTotal += time.Since(t0)
		}
		if err == nil {
			if u.DisplayName != "" {
				userName = u.DisplayName
			} else if u.Name != "" {
				userName = u.Name
			}
			if userName != "" && userNames != nil {
				userNames[effUserID] = userName
			}
		}
	}
	if userName == "" {
		if botUsername != "" {
			userName = botUsername
		} else {
			userName = effUserID
			// Cache had no entry for this user. Enqueue an async resolver
			// fetch so the next render after UserResolvedMsg lands shows
			// the real display name instead of the raw user ID. Guarded on
			// m.UserID (the original) so bot rows — already handled via
			// RequestBot above — don't hit the users.info path.
			if router != nil && m.UserID != "" {
				if wctx := router.Active(); wctx != nil && wctx.UserResolver != nil {
					wctx.UserResolver.Request(m.UserID)
				}
			}
		}
	}

	// Reactions for this message.
	var reactions []messages.ReactionItem
	// TODO(perf): N+1 query — for 50 messages this is 50 SQLite calls on the
	// channel-open hot path. If this becomes a bottleneck, add a batched
	// db.GetReactionsForMessages([]ts) map[ts][]ReactionRow to the cache layer.
	var reactT0 time.Time
	if stats != nil {
		reactT0 = time.Now()
	}
	rs, reactErr := db.GetReactions(m.TS, channelID)
	if stats != nil {
		stats.getReactCalls++
		stats.getReactTotal += time.Since(reactT0)
	}
	if reactErr == nil {
		for _, r := range rs {
			hasReacted := false
			for _, uid := range r.UserIDs {
				if uid == selfUserID {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Emoji,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.UserIDs,
			})
		}
	} else {
		debuglog.Cache("%s: GetReactions %s/%s: %v", logPrefix, channelID, m.TS, reactErr)
	}

	// Attachments / blocks / legacy attachments come from
	// raw_json. Pre-Task-2 rows have an empty raw_json; for
	// those we render text-only.
	var attachments []messages.Attachment
	var blocks []blockkit.Block
	var legacy []blockkit.LegacyAttachment
	if m.RawJSON != "" {
		var raw slack.Message
		var unmarshalT0 time.Time
		if stats != nil {
			unmarshalT0 = time.Now()
		}
		err := json.Unmarshal([]byte(m.RawJSON), &raw)
		if stats != nil {
			stats.unmarshalCalls++
			stats.unmarshalTotal += time.Since(unmarshalT0)
		}
		if err != nil {
			debuglog.Cache("%s: raw_json unmarshal for %s/%s: %v",
				logPrefix, channelID, m.TS, err)
		} else {
			attachments = extractAttachments(raw.Files)
			blocks = extractBlocks(raw.Blocks)
			legacy = extractLegacyAttachments(raw.Attachments)
		}
	}

	return messages.MessageItem{
		TS:                m.TS,
		UserID:            effUserID,
		UserName:          userName,
		Text:              m.Text,
		Timestamp:         formatTimestamp(m.TS, tsFormat),
		ThreadTS:          m.ThreadTS,
		ReplyCount:        m.ReplyCount,
		Subtype:           m.Subtype,
		Reactions:         reactions,
		Attachments:       attachments,
		Blocks:            blocks,
		LegacyAttachments: legacy,
	}
}

// loadCachedThreadReplies reads cached parent + replies for a thread
// from SQLite and reconstructs []messages.MessageItem with the same
// fidelity as fetchThreadReplies. Offline-pure (no network).
//
// The returned slice includes the parent message at index 0 followed
// by replies in chronological order, matching db.GetThreadReplies'
// ordering. Callers that pass the slice into
// ui.ThreadRepliesLoadedMsg.Replies must strip the parent
// (slice[1:]) since the reducer expects replies-only.
//
// Returns nil when no rows are cached or on DB error.
func loadCachedThreadReplies(
	db *cache.DB,
	selfUserID string,
	channelID, threadTS string,
	userNames map[string]string,
	tsFormat string,
	router *workspaceRouter,
) []messages.MessageItem {
	if db == nil {
		debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s db=nil", channelID, threadTS)
		return nil
	}
	debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s entry", channelID, threadTS)
	rows, err := db.GetThreadReplies(channelID, threadTS)
	if err != nil {
		debuglog.Cache("loadCachedThreadReplies: GetThreadReplies %s/%s: %v", channelID, threadTS, err)
		return nil
	}
	if len(rows) == 0 {
		debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s result count=0", channelID, threadTS)
		return nil
	}

	out := make([]messages.MessageItem, 0, len(rows))
	for _, m := range rows {
		out = append(out, enrichCachedRow(db, selfUserID, channelID, m, userNames, tsFormat, "loadCachedThreadReplies", router, nil))
	}
	debuglog.Cache("loadCachedThreadReplies: channel=%s thread_ts=%s result %s",
		channelID, threadTS, summarizeMessages(out))
	return out
}

// fetchChannelMessages returns the channel's recent messages from the
// network, with cache write-through. The return-value contract:
//
//	nil   - the network call FAILED (transient error, auth issue, etc.)
//	[]    - the channel is genuinely empty
//	[...] - normal case
//
// The MessagesLoadedMsg handler distinguishes nil from empty so a
// failed background refresh doesn't wipe a successfully-rendered
// cache view. Do NOT change nil to mean "empty channel".
func fetchChannelMessages(client *slackclient.Client, channelID string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchChannelMessages: channel=%s entry", channelID)
	start := time.Now()
	history, err := client.GetHistory(ctx, channelID, 50, "")
	if err != nil {
		debuglog.Cache("fetchChannelMessages: GetHistory %s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("fetchChannelMessages: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		authorID, userName := messageAuthor(m, userNames, db, router)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      authorID,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})

		// Convert reactions
		var reactions []messages.ReactionItem
		for _, r := range m.Reactions {
			hasReacted := false
			for _, uid := range r.Users {
				if uid == client.UserID() {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Name,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.Users,
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:                m.Timestamp,
			UserID:            authorID,
			UserName:          userName,
			Text:              m.Text,
			Timestamp:         formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:          m.ThreadTimestamp,
			ReplyCount:        m.ReplyCount,
			Subtype:           m.SubType,
			Reactions:         reactions,
			Attachments:       extractAttachments(m.Files),
			Blocks:            extractBlocks(m.Blocks),
			LegacyAttachments: extractLegacyAttachments(m.Attachments),
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	debuglog.Cache("fetchChannelMessages: channel=%s result %s dur_ms=%d (authoritative replace)",
		channelID, summarizeMessages(msgItems), time.Since(start).Milliseconds())
	if err := db.SetChannelSyncedAt(channelID, time.Now().Unix()); err != nil {
		debuglog.Cache("fetchChannelMessages: SetChannelSyncedAt %s: %v", channelID, err)
	}
	return msgItems
}

// fetchThreadReplies returns network thread replies (parent stripped),
// with cache write-through. Same nil-vs-empty contract as
// fetchChannelMessages: nil signals failure, [] signals "no replies",
// so the ThreadRepliesLoadedMsg consumer can decide whether to clobber
// an already-rendered cached view.
func fetchThreadReplies(client *slackclient.Client, channelID, threadTS string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache, router *workspaceRouter) []messages.MessageItem {
	ctx := context.Background()
	debuglog.Cache("fetchThreadReplies: channel=%s thread_ts=%s entry", channelID, threadTS)
	start := time.Now()
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		debuglog.Cache("fetchThreadReplies: GetReplies %s/%s: %v dur_ms=%d (returning nil → keep cache)",
			channelID, threadTS, err, time.Since(start).Milliseconds())
		return nil
	}

	msgItems := make([]messages.MessageItem, 0, len(history))
	for _, m := range history {
		rawBytes, _ := json.Marshal(m)
		debuglog.Cache("fetchThreadReplies: upsert channel=%s ts=%s subtype=%q reply_count=%d files=%d",
			channelID, m.Timestamp, m.SubType, m.ReplyCount, len(m.Files))
		authorID, userName := messageAuthor(m, userNames, db, router)
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      authorID,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})

		// Convert reactions
		var reactions []messages.ReactionItem
		for _, r := range m.Reactions {
			hasReacted := false
			for _, uid := range r.Users {
				if uid == client.UserID() {
					hasReacted = true
					break
				}
			}
			reactions = append(reactions, messages.ReactionItem{
				Emoji:      r.Name,
				Count:      r.Count,
				HasReacted: hasReacted,
				UserIDs:    r.Users,
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:                m.Timestamp,
			UserID:            authorID,
			UserName:          userName,
			Text:              m.Text,
			Timestamp:         formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:          m.ThreadTimestamp,
			ReplyCount:        m.ReplyCount,
			Subtype:           m.SubType,
			Reactions:         reactions,
			Attachments:       extractAttachments(m.Files),
			Blocks:            extractBlocks(m.Blocks),
			LegacyAttachments: extractLegacyAttachments(m.Attachments),
		})
	}

	// First message from GetConversationReplies is the parent -- skip it for the replies list.
	// Return non-nil empty on success-no-replies so the consumer can distinguish from the
	// error path (which returns nil above).
	var out []messages.MessageItem
	if len(msgItems) > 1 {
		out = msgItems[1:]
	} else {
		out = []messages.MessageItem{}
	}
	debuglog.Cache("fetchThreadReplies: channel=%s thread_ts=%s result %s dur_ms=%d (authoritative replace)",
		channelID, threadTS, summarizeMessages(out), time.Since(start).Milliseconds())
	return out
}

// searchWorkspaceFunc builds the SearchService.SearchWorkspace
// closure: a server-side search.messages query against the active
// workspace. Always returns a WorkspaceSearchResultsMsg — a nil msg
// would leave the ctrl+f modal spinner stuck (the reducer only exits
// the loading state on a results msg).
func searchWorkspaceFunc(router *workspaceRouter, db *cache.DB, tsFormat string) func(query string) tea.Msg {
	return func(query string) tea.Msg {
		wctx := router.Active()
		if wctx == nil {
			return ui.WorkspaceSearchResultsMsg{Query: query, Err: errors.New("no active workspace")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := wctx.Client.SearchMessages(ctx, query, 50)
		if err != nil {
			return ui.WorkspaceSearchResultsMsg{Query: query, Err: err}
		}
		// Read-only lookup: this closure runs in a bubbletea cmd
		// goroutine, so it must not write wctx.UserNames (shared with
		// the UI goroutine; see userResolver.Request).
		resolveUser := func(id string) (string, bool) {
			return lookupUserCached(id, wctx.UserNames, db)
		}
		resolveChannel := func(id string) (string, bool) {
			if db == nil {
				return "", false
			}
			if ch, err := db.GetChannel(id); err == nil && ch.Name != "" {
				return ch.Name, true
			}
			return "", false
		}
		items := searchResultItems(res.Matches, tsFormat, time.Now(), resolveUser, resolveChannel, wctx.UserGroups())
		return ui.WorkspaceSearchResultsMsg{Query: query, Items: items, Total: res.Total}
	}
}

// userIDShapeRe matches a string shaped like a Slack user ID. DM hits
// from search.messages carry the counterpart's raw user ID as the
// channel "name"; this is the detection heuristic for that case.
var userIDShapeRe = regexp.MustCompile(`^[UW][A-Z0-9]{5,}$`)

// searchResultItems converts search.messages matches into the modal's
// row items: snippets have mrkdwn entities flattened to plain text, DM
// channel names (raw user IDs on the wire) are resolved to the
// counterpart's display name, and thread TSes are recovered from
// permalinks. Pure: all lookups go through the supplied resolvers.
func searchResultItems(matches []slack.SearchMessage, tsFormat string, now time.Time, resolveUser, resolveChannel func(id string) (string, bool), userGroups map[string]string) []searchresults.Item {
	items := make([]searchresults.Item, 0, len(matches))
	for _, match := range matches {
		// ThreadTS comes from the hit's permalink. Known v1
		// limitation: a thread-reply hit with an unparseable
		// permalink degrades to plain-message nav, which may
		// toast "Message not found in loaded history" (replies
		// aren't in channel history).
		threadTS := ""
		if pl, ok := slackurl.Parse(match.Permalink); ok {
			threadTS = string(pl.ThreadTS)
		}

		// DM detection: an IM channel ID (D...) is authoritative; a
		// user-ID-shaped channel name counts only when it actually
		// resolves as a user (slack-go's CtxChannel has no IsIM flag).
		channelName := match.Channel.Name
		isDM := strings.HasPrefix(match.Channel.ID, "D")
		if userIDShapeRe.MatchString(channelName) {
			if name, ok := resolveUser(channelName); ok && name != "" {
				channelName = name
				isDM = true
			}
		}

		items = append(items, searchresults.Item{
			ChannelID:   match.Channel.ID,
			ChannelName: channelName,
			UserName:    match.Username,
			TS:          match.Timestamp,
			ThreadTS:    threadTS,
			Text:        messages.FlattenMrkdwnWithUserGroups(match.Text, resolveUser, resolveChannel, userGroups),
			Timestamp:   formatSearchTimestamp(match.Timestamp, tsFormat, now),
			IsDM:        isDM,
		})
	}
	return items
}

// formatSearchTimestamp formats a search-result timestamp for the
// modal's metadata line. Results span months, so non-today hits get a
// date prefix: "May 19, 8:01 PM" this year, "May 19 2025, 8:01 PM" for
// prior years. Today's hits show just the time (like Slack, and
// mirroring the message pane's "Today" separator). Unparseable
// timestamps fall back to the raw value, matching formatTimestamp.
func formatSearchTimestamp(ts, timeFormat string, now time.Time) string {
	parts := strings.SplitN(ts, ".", 2)
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ts
	}
	t := time.Unix(sec, 0)
	ny, nm, nd := now.Date()
	ty, tm, td := t.Date()
	switch {
	case ty == ny && tm == nm && td == nd:
		return t.Format(timeFormat)
	case ty == ny:
		return t.Format("Jan 2, ") + t.Format(timeFormat)
	default:
		return t.Format("Jan 2 2006, ") + t.Format(timeFormat)
	}
}

func formatTimestamp(ts, format string) string {
	// Slack ts is like "1700000001.000000" -- split on "." and parse the seconds
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) == 0 {
		return ts
	}
	sec, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ts
	}
	t := time.Unix(sec, 0)
	return t.Format(format)
}

func xdgConfig() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "slk")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "slk")
}

func xdgData() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "slk")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "slk")
}

func xdgCache() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "slk")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "slk")
}

// bootstrapPresenceAndDND fetches the user's current presence and DND
// state from Slack, populates the WorkspaceContext, and sends an initial
// StatusChangeMsg. Also subscribes to presence_change events for the self
// user and every DM peer so external state changes arrive over the WS.
func bootstrapPresenceAndDND(ctx context.Context, wctx *WorkspaceContext, program *tea.Program) {
	if wctx == nil || wctx.Client == nil {
		return
	}

	// Subscribe to presence for our own user plus every 1:1 DM peer so the
	// sidebar can show who is online. presence_sub REPLACES the prior
	// subscription set, so self and peers must go in one call. Failure is
	// non-fatal — manual_presence_change and dnd_updated work without it.
	//
	// This runs from OnConnect, which fires on the initial connect AND on
	// every reconnect. Re-subscribing per connection is required because
	// the subscription is connection-scoped.
	subscribeWorkspacePresence(wctx)

	// Initial presence fetch
	if p, err := wctx.Client.GetUserPresence(ctx, wctx.UserID); err == nil && p != nil {
		wctx.Presence = p.Presence
	}

	// Initial DND fetch.
	//
	// Slack's dnd_enabled flag means "the user has a DND schedule
	// configured", NOT "currently in DND". The user is currently in DND
	// only when (a) a manual snooze is active, or (b) the current time
	// falls inside the next scheduled window. The same rule lives in
	// internal/slack/events.go's computeDNDState for the WS event path.
	if st, err := wctx.Client.GetDNDInfo(ctx, wctx.UserID); err == nil && st != nil {
		now := time.Now().Unix()
		var isDND bool
		var endUnix int64
		switch {
		case st.SnoozeEnabled && int64(st.SnoozeEndTime) > now:
			isDND = true
			endUnix = int64(st.SnoozeEndTime)
		case st.Enabled && int64(st.NextStartTimestamp) > 0 &&
			int64(st.NextStartTimestamp) <= now && now < int64(st.NextEndTimestamp):
			isDND = true
			endUnix = int64(st.NextEndTimestamp)
		}
		wctx.DNDEnabled = isDND
		if endUnix > 0 {
			wctx.DNDEndTS = time.Unix(endUnix, 0)
		} else {
			wctx.DNDEndTS = time.Time{}
		}
	}

	if program != nil {
		program.Send(ui.StatusChangeMsg{
			TeamID:     wctx.TeamID,
			Presence:   wctx.Presence,
			DNDEnabled: wctx.DNDEnabled,
			DNDEndTS:   wctx.DNDEndTS,
		})
	}
}

// subscribeWorkspacePresence subscribes over the WebSocket to presence
// updates for the authenticated user plus every 1:1 DM peer, so the
// sidebar can show who is online. Slack's presence_sub REPLACES the prior
// subscription set and is connection-scoped, so this sends self + all DM
// peers in a single call and must be re-run on each (re)connect.
//
// Note: the WS is opened with no_query_on_subscribe=1, so Slack does not
// reply with each peer's current presence at subscribe time — DM rows are
// seeded from the local cache at build time and then updated live by
// presence_change events. Safe to call repeatedly.
func subscribeWorkspacePresence(wctx *WorkspaceContext) {
	if wctx == nil || wctx.Client == nil {
		return
	}
	ids := workspacePresenceIDs(wctx)
	if len(ids) == 0 {
		debuglog.General("subscribeWorkspacePresence: no ids to subscribe")
		return
	}
	if err := wctx.Client.SubscribePresence(ids); err != nil {
		debuglog.General("subscribeWorkspacePresence (%d ids) FAILED: %v", len(ids), err)
		return
	}
	debuglog.General("subscribeWorkspacePresence: sent presence_sub for %d ids (self+%d dm peers)", len(ids), len(ids)-1)
}

// workspacePresenceIDs returns the deduped list of user IDs to subscribe
// for presence: the authenticated user plus every 1:1 DM peer. Group DMs
// and app/bot DMs (which carry no human presence dot in the sidebar) are
// skipped. Pure function for testability.
func workspacePresenceIDs(wctx *WorkspaceContext) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0, len(wctx.Channels)+1)
	add := func(uid string) {
		if uid == "" {
			return
		}
		if _, ok := seen[uid]; ok {
			return
		}
		seen[uid] = struct{}{}
		ids = append(ids, uid)
	}
	add(wctx.UserID) // self — keeps the self presence subscription intact
	for _, ch := range wctx.Channels {
		if ch.Type == "dm" {
			add(ch.DMUserID)
		}
	}
	return ids
}

// mostRecentlyVisitedChannel returns the channel ID with the latest
// last-visited timestamp, or "" when there are no recorded visits (e.g.
// the first run). Drives last-channel restoration on startup.
func mostRecentlyVisitedChannel(visits map[string]int64) string {
	var bestID string
	var bestTS int64
	for id, ts := range visits {
		if ts > bestTS {
			bestTS = ts
			bestID = id
		}
	}
	return bestID
}

// rtmEventHandler bridges WebSocket events into bubbletea messages via p.Send()
// and caches all incoming messages to the SQLite database.
type rtmEventHandler struct {
	program     *tea.Program
	userNames   map[string]string
	tsFormat    string
	db          *cache.DB
	workspaceID string
	connected   bool
	isActive    func() bool

	// Notifications
	notifier        *notify.Notifier
	notifyCfg       config.Notifications
	currentUserID   string
	channelNames    map[string]string
	channelTypes    map[string]string
	workspaceName   string
	activeChannelID func() string

	// cfg is the loaded user config; used by OnConversationOpened to
	// resolve sidebar section + section order via buildChannelItem.
	cfg config.Config

	// Back-reference for self-presence/DND state mutation.
	wsCtx *WorkspaceContext

	// backfillGate enforces a 30 s minimum between reconnect-driven
	// backfill passes. Per-handler so each workspace has its own gate.
	// Initialized at construction with window = 30 * time.Second.
	backfillGate dedupeGate
}

func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File, blocks slack.Blocks, attachments []slack.Attachment, botID, username string) {
	// Bot messages (bot_message) carry no user, only a bot_id + username.
	// Key the row on the bot_id and resolve its avatar/name via bots.info,
	// mirroring the fetch-path messageAuthor helper.
	authorID := userID
	if authorID == "" && botID != "" {
		authorID = botID
		if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
			h.wsCtx.UserResolver.RequestBot(botID, username)
		}
	}
	// Cache every message to SQLite, regardless of active workspace.
	// Guard against nil db so handlers constructed in tests (without
	// real persistence) don't panic.
	if h.db != nil {
		synthetic := slack.Message{Msg: slack.Msg{
			Type:            "message",
			Timestamp:       ts,
			User:            authorID,
			Text:            text,
			ThreadTimestamp: threadTS,
			SubType:         subtype,
			Files:           files,
			Blocks:          blocks,
			Attachments:     attachments,
		}}
		rawBytes, _ := json.Marshal(synthetic)
		h.db.UpsertMessage(cache.Message{
			TS:          ts,
			ChannelID:   channelID,
			WorkspaceID: h.workspaceID,
			UserID:      authorID,
			Text:        text,
			ThreadTS:    threadTS,
			Subtype:     subtype,
			RawJSON:     string(rawBytes),
			CreatedAt:   time.Now().Unix(),
		})
		if err := h.db.SetChannelSyncedAt(channelID, time.Now().Unix()); err != nil {
			debuglog.Cache("OnMessage: SetChannelSyncedAt %s: %v", channelID, err)
		}
		// Advance the per-channel ts watermark used by reconnect
		// backfill. Slack delivers WS messages in order, so receipt
		// of a message with ts=X implies we have no missing messages
		// with ts <= X on this channel — that is exactly the
		// invariant latest_synced_ts encodes. AdvanceChannelLatestSyncedTS
		// is no-regress, so out-of-order replay (e.g., a delayed
		// duplicate after reconnect) won't move the cursor backward.
		if _, err := h.db.AdvanceChannelLatestSyncedTS(channelID, ts); err != nil {
			debuglog.Cache("OnMessage: AdvanceChannelLatestSyncedTS %s ts=%s: %v", channelID, ts, err)
		}
	}

	// Check if this message should trigger a desktop notification.
	// Do this before the active workspace check so inactive workspaces
	// can still trigger notifications.
	if h.notifier != nil && h.notifyCfg.Enabled {
		isActiveWS := h.isActive != nil && h.isActive()
		activeChID := ""
		if h.activeChannelID != nil {
			activeChID = h.activeChannelID()
		}
		ctx := notify.NotifyContext{
			CurrentUserID:   h.currentUserID,
			ActiveChannelID: activeChID,
			IsActiveWS:      isActiveWS,
			OnMention:       h.notifyCfg.OnMention,
			OnDM:            h.notifyCfg.OnDM,
			OnKeyword:       h.notifyCfg.OnKeyword,
			IsDND:           h.wsCtx != nil && h.wsCtx.DNDEnabled && (h.wsCtx.DNDEndTS.IsZero() || time.Now().Before(h.wsCtx.DNDEndTS)),
			IsMuted:         h.wsCtx != nil && h.wsCtx.MuteStore != nil && h.wsCtx.MuteStore.IsMuted(channelID),
		}
		chType := h.channelTypes[channelID]
		// Pass the raw userID (not authorID): ShouldNotify's self-message
		// suppression keys on the human sender, and a bot message
		// (userID == "", authorID == botID) can never be "you" — so the
		// empty userID is intentional, not a bug to "fix" later.
		if notify.ShouldNotify(ctx, channelID, userID, text, chType) {
			senderName := authorID
			if resolved, ok := h.userNames[authorID]; ok {
				senderName = resolved
			} else if username != "" {
				senderName = username
			}
			chName := h.channelNames[channelID]
			title := h.workspaceName + ": #" + chName
			if chType == "dm" || chType == "group_dm" {
				title = h.workspaceName + ": " + senderName
			}
			var groupNames map[string]string
			if h.wsCtx != nil {
				groupNames = h.wsCtx.UserGroups()
			}
			body := senderName + ": " + notify.StripSlackMarkupWithUserGroups(text, h.userNames, groupNames)
			go func() {
				if err := h.notifier.Notify(title, body); err != nil {
					debuglog.Notify("notification failed: %v", err)
				}
			}()
		}
	}

	// Read-state: mark the channel has_unread=true when a message
	// arrives in a channel the user is NOT actively viewing. Mirrors
	// Slack's channel-unread semantics — non-broadcast thread replies
	// do not mark the parent channel unread (only top-level messages
	// and thread_broadcast subtypes do). See internal/ui/app.go's
	// thread-reply guard for the matching active-path treatment.
	//
	// This write runs for BOTH active and inactive workspaces; the
	// active/inactive split below only governs the UI dispatch path,
	// not durable read state. Task 10 of the read-state-sync plan
	// consolidated the previously duplicated paths into this single
	// gated write.
	isThreadReply := threadTS != "" && threadTS != ts
	isBroadcast := subtype == "thread_broadcast"
	shouldMarkChannel := !isThreadReply || isBroadcast
	activeChIDForRead := ""
	if h.activeChannelID != nil {
		activeChIDForRead = h.activeChannelID()
	}
	if h.db != nil && shouldMarkChannel && activeChIDForRead != channelID {
		if err := h.db.UpdateChannelReadState(channelID, "", true); err != nil {
			log.Printf("Warning: failed to set has_unread for %s: %v", channelID, err)
		}
	}

	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — read state was already persisted above.
		// Fire a ReadStateChangedMsg so the workspace rail refreshes
		// its dot from db.WorkspacesWithUnreads(). The sidebar's
		// Invalidate is a no-op here because the active workspace's
		// sidebar isn't showing this channel anyway.
		if shouldMarkChannel {
			debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=inactive_workspace_persisted",
				h.workspaceID, channelID, ts, subtype, threadTS)
		} else {
			debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=skipped_thread_reply_inactive",
				h.workspaceID, channelID, ts, subtype, threadTS)
		}
		if h.program != nil {
			h.program.Send(ui.ReadStateChangedMsg{
				WorkspaceID: h.workspaceID,
				ChannelID:   channelID,
			})
		}
		return
	}

	userName, ok := resolveUserCached(authorID, h.userNames, h.db)
	if !ok {
		userName = authorID
		if userID != "" {
			if h.wsCtx != nil && h.wsCtx.UserResolver != nil {
				h.wsCtx.UserResolver.Request(userID)
			}
		} else if username != "" {
			// Bot author: show its name immediately; bots.info (already
			// requested above) fills in the avatar.
			userName = username
		}
	}
	debuglog.Cache("OnMessage: team=%s channel=%s ts=%s subtype=%q thread_ts=%s decision=dispatched_to_app",
		h.workspaceID, channelID, ts, subtype, threadTS)
	if h.program != nil {
		h.program.Send(ui.NewMessageMsg{
			ChannelID: channelID,
			Message: messages.MessageItem{
				TS:                ts,
				UserID:            authorID,
				UserName:          userName,
				Text:              text,
				Timestamp:         formatTimestamp(ts, h.tsFormat),
				ThreadTS:          threadTS,
				Subtype:           subtype,
				IsEdited:          edited,
				Attachments:       extractAttachments(files),
				Blocks:            extractBlocks(blocks),
				LegacyAttachments: extractLegacyAttachments(attachments),
			},
		})
	}
}

func (h *rtmEventHandler) OnMessageDeleted(channelID, ts string) {
	if err := h.db.DeleteMessage(channelID, ts); err != nil {
		log.Printf("Warning: failed to soft-delete cached message %s/%s: %v", channelID, ts, err)
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — nothing to update in the UI.
		return
	}
	h.program.Send(ui.WSMessageDeletedMsg{ChannelID: channelID, TS: ts})
}

func (h *rtmEventHandler) OnReactionAdded(channelID, ts, userID, emojiName string) {
	// Update cache regardless of active state
	rows, err := h.db.GetReactions(ts, channelID)
	if err == nil {
		found := false
		for _, r := range rows {
			if r.Emoji == emojiName {
				userIDs := append(r.UserIDs, userID)
				_ = h.db.UpsertReaction(ts, channelID, emojiName, userIDs, r.Count+1)
				found = true
				break
			}
		}
		if !found {
			_ = h.db.UpsertReaction(ts, channelID, emojiName, []string{userID}, 1)
		}
	}

	if h.isActive != nil && !h.isActive() {
		return
	}

	h.program.Send(ui.ReactionAddedMsg{
		ChannelID: channelID,
		MessageTS: ts,
		UserID:    userID,
		Emoji:     emojiName,
	})
}

func (h *rtmEventHandler) OnReactionRemoved(channelID, ts, userID, emojiName string) {
	// Update cache regardless of active state
	rows, err := h.db.GetReactions(ts, channelID)
	if err == nil {
		for _, r := range rows {
			if r.Emoji == emojiName {
				var newUserIDs []string
				for _, uid := range r.UserIDs {
					if uid != userID {
						newUserIDs = append(newUserIDs, uid)
					}
				}
				if len(newUserIDs) == 0 {
					_ = h.db.DeleteReaction(ts, channelID, emojiName)
				} else {
					_ = h.db.UpsertReaction(ts, channelID, emojiName, newUserIDs, r.Count-1)
				}
				break
			}
		}
	}

	if h.isActive != nil && !h.isActive() {
		return
	}

	h.program.Send(ui.ReactionRemovedMsg{
		ChannelID: channelID,
		MessageTS: ts,
		UserID:    userID,
		Emoji:     emojiName,
	})
}

func (h *rtmEventHandler) OnPresenceChange(userID, presence string) {
	_ = h.db.UpdatePresence(userID, presence)
	if h.program == nil {
		return
	}
	h.program.Send(ui.PresenceChangeMsg{
		UserID:   userID,
		Presence: presence,
	})
}

func (h *rtmEventHandler) OnUserTyping(channelID, userID string) {
	if h.program == nil {
		return
	}
	h.program.Send(ui.UserTypingMsg{
		ChannelID:   channelID,
		UserID:      userID,
		WorkspaceID: h.workspaceID,
	})
}

func (h *rtmEventHandler) OnConnect() {
	h.connected = true
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateConnected)})
	if h.wsCtx != nil {
		go bootstrapPresenceAndDND(context.Background(), h.wsCtx, h.program)
	}
	// Refresh Slack-native section state on reconnect. MaybeRebootstrap
	// is debounced to once per 30s (Task 6) so a rapid flap doesn't
	// thunder; a real long-disconnect-then-reconnect refreshes section
	// state we may have missed during the gap.
	//
	// Run synchronously on the WS read goroutine. This briefly blocks
	// inbound event delivery during the bootstrap HTTP call, but that
	// cost is bounded — at most one call per 30s per workspace — and
	// avoids racing wsCtx.Channels mutations against the same loop's
	// next event (which could be an OnConversationOpened that also
	// touches wsCtx.Channels).
	if h.wsCtx != nil && h.wsCtx.SectionStore != nil && h.wsCtx.Client != nil {
		if err := h.wsCtx.SectionStore.MaybeRebootstrap(context.Background(), h.wsCtx.Client); err != nil {
			log.Printf("section store rebootstrap for %s failed: %v", h.wsCtx.TeamName, err)
		} else {
			h.refreshSectionsForActive()
		}
	}

	// Reconnect backfill: catch up on messages missed while the WS
	// was dead. The 30 s dedupe in backfillGate prevents disconnect
	// flaps from spawning overlapping passes. Runs in its own
	// goroutine so the WS read loop isn't blocked on HTTP work.
	//
	// Note on first-connect: the initial WS connect also fires
	// OnConnect, so backfill runs at startup too. This is harmless —
	// synced_at for freshly-bootstrapped channels is current, so most
	// GetHistorySince calls return zero messages quickly. The 4-wide
	// concurrency cap bounds the cost.
	h.triggerBackfill("reconnect")

	// Force-stale the active channel's membership cache and re-fetch.
	// The WS may have missed member_joined/left deltas during the
	// disconnect window; a fresh full fetch reconciles divergence.
	// Inactive channels stay as-is — they'll re-fetch on their next
	// EnsureFresh via the channel-switch fetcher path.
	if h.wsCtx != nil && h.wsCtx.Membership != nil && h.activeChannelID != nil {
		activeID := h.activeChannelID()
		if activeID != "" {
			h.wsCtx.Membership.ForceStale(activeID)
			h.wsCtx.Membership.EnsureFresh(context.Background(), activeID)
		}
	}
}

// triggerBackfill kicks off a reconnect-style backfill pass for this
// workspace, subject to the per-handler 30 s dedupe gate. Called by
// OnConnect on every WS reconnect AND by the wake detector when the
// system wakes from sleep (where the WS may not have torn down — a
// short sleep can survive within the 60 s WS read deadline, so
// OnConnect never fires and no catch-up would happen without an
// explicit trigger).
//
// The dedupe gate is shared with OnConnect, so a wake event that
// happens to coincide with a real WS reconnect runs the backfill
// exactly once.
//
// Returns true if the backfill was started, false if the gate
// suppressed it.
func (h *rtmEventHandler) triggerBackfill(trigger string) bool {
	if h.wsCtx == nil || h.db == nil || h.wsCtx.Client == nil {
		return false
	}
	if !h.backfillGate.tryStart(time.Now()) {
		debuglog.Backfill("team=%s trigger=%s skipped reason=dedupe", h.workspaceID, trigger)
		return false
	}
	wctx := h.wsCtx
	workspaceID := h.workspaceID
	program := h.program
	db := h.db
	go func() {
		bf := newBackfiller(
			wctx.Client, db, workspaceID, wctx.Client.UserID(), program, 4, 500,
			func(available bool) { wctx.SubscriptionsAvailable = available },
		)
		_ = bf.run(context.Background())
	}()
	return true
}

func (h *rtmEventHandler) OnDisconnect() {
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateDisconnected)})
}

func (h *rtmEventHandler) OnSelfPresenceChange(presence string) {
	if h.wsCtx == nil {
		return
	}
	// Slack uses "active"/"away" in events; store verbatim.
	h.wsCtx.Presence = presence
	if h.program == nil {
		return
	}
	h.program.Send(ui.StatusChangeMsg{
		TeamID:     h.workspaceID,
		Presence:   presence,
		DNDEnabled: h.wsCtx.DNDEnabled,
		DNDEndTS:   h.wsCtx.DNDEndTS,
	})
}

func (h *rtmEventHandler) OnDNDChange(enabled bool, endUnix int64) {
	if h.wsCtx == nil {
		return
	}
	h.wsCtx.DNDEnabled = enabled
	if endUnix > 0 {
		h.wsCtx.DNDEndTS = time.Unix(endUnix, 0)
	} else {
		h.wsCtx.DNDEndTS = time.Time{}
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.StatusChangeMsg{
		TeamID:     h.workspaceID,
		Presence:   h.wsCtx.Presence,
		DNDEnabled: h.wsCtx.DNDEnabled,
		DNDEndTS:   h.wsCtx.DNDEndTS,
	})
}

func (h *rtmEventHandler) OnChannelMarked(channelID, ts string, unreadCount int) {
	// Slack's *_marked events fire in BOTH directions: when the user
	// reads a channel (unreadCount=0) AND when the user marks one
	// unread (unreadCount>0). The event payload's
	// `unread_count_display` tells us which case we're in. We must
	// use it instead of always clearing the unread flag — the
	// original spec hardcoded false here, which meant a remote
	// mark-unread (via another client) silently cleared slk's dot.
	hasUnread := unreadCount > 0
	// Persist regardless of active workspace so the cache stays
	// authoritative across workspace switches.
	if err := h.db.UpdateChannelReadState(channelID, ts, hasUnread); err != nil {
		log.Printf("Warning: failed to update read state on channel_marked %s/%s: %v", channelID, ts, err)
	}
	if h.program != nil {
		// Always notify so the workspace rail can refresh, regardless
		// of whether this workspace is active. The active-workspace
		// sidebar refresh and toast come from ChannelMarkedRemoteMsg
		// below; the rail refresh comes from ReadStateChangedMsg's
		// App.Update handler.
		h.program.Send(ui.ReadStateChangedMsg{WorkspaceID: h.workspaceID, ChannelID: channelID})
	}
	if h.isActive != nil && !h.isActive() {
		// Inactive workspace: persistence + rail refresh above are
		// the only visible effects; no sidebar/toast to update.
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ChannelMarkedRemoteMsg{
		ChannelID:   channelID,
		TS:          ts,
		UnreadCount: unreadCount,
	})
}

func (h *rtmEventHandler) OnThreadMarked(channelID, threadTS, ts string, read bool) {
	// Persist subscription state regardless of active-workspace state.
	// Mirrors OnChannelMarked / OnMessage: durable cache must reflect
	// every WS event, otherwise switching to an inactive workspace
	// would surface stale read state and (worse) miss newly-unread
	// threads until the next reconnect-driven reconcile. active =
	// !read per the dispatch in internal/slack/events.go: WS `active`
	// means "subscribed for unread updates", which corresponds to
	// active=1 in our table.
	if h.db != nil {
		if err := h.db.UpsertThreadSubscription(h.workspaceID, channelID, threadTS, ts, !read); err != nil {
			debuglog.Cache("OnThreadMarked: UpsertThreadSubscription %s/%s: %v",
				channelID, threadTS, err)
		}
	}

	// UI dispatch is active-only: the threads-view list and sidebar
	// badge live on the active workspace; inactive workspaces pick up
	// fresh state on the next switch via threadsListFetcher.
	if h.isActive != nil && !h.isActive() {
		return
	}
	if h.program == nil {
		return
	}
	h.program.Send(ui.ThreadMarkedRemoteMsg{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		TS:        ts,
		Read:      read,
	})
}

// OnThreadSubscriptionChanged persists a subscribe/unsubscribe event
// in the thread_subscriptions table. The threads-view UI refresh is
// handled by a ThreadsListDirtyMsg dispatch so a new subscription
// shows up (active=true) or an unsubscribe removes the row
// (active=false) without per-event UI logic here.
func (h *rtmEventHandler) OnThreadSubscriptionChanged(channelID, threadTS, lastRead string, active bool) {
	// Persist subscribe/unsubscribe regardless of active-workspace
	// state. Mirrors OnChannelMarked / OnMessage: dropping the DB
	// write on inactive workspaces means a thread the user just got
	// @-mentioned in (auto-subscribed) would never enter the local
	// thread_subscriptions table, and the threads view would silently
	// omit it on next workspace switch until the next reconnect's
	// ReconcileThreadSubscriptions catches up.
	if h.db != nil {
		if err := h.db.UpsertThreadSubscription(h.workspaceID, channelID, threadTS, lastRead, active); err != nil {
			debuglog.Cache("OnThreadSubscriptionChanged: UpsertThreadSubscription %s/%s: %v",
				channelID, threadTS, err)
		}
	}
	// UI refresh is filtered by team in the App.Update handler, so
	// it's safe (and harmless) to dispatch for inactive workspaces
	// too — but we still skip it when isActive is wired and false to
	// avoid waking the UI loop for no-op refreshes. The DB write
	// above ensures correctness on the eventual switch.
	if h.isActive != nil && !h.isActive() {
		return
	}
	if h.program != nil {
		h.program.Send(ui.ThreadsListDirtyMsg{TeamID: h.workspaceID})
	}
}

// OnConversationOpened handles WS events that surface a new or
// previously-closed conversation: mpim_open, im_created, group_joined,
// channel_joined. Builds a sidebar.ChannelItem via the shared helper,
// persists it in WorkspaceContext (de-duped by ID, preserving live
// unread/last-read state), upserts the SQLite cache row, mirrors
// channelNames/Types maps used by the notifier, and — if the
// workspace is active — forwards a ConversationOpenedMsg to the UI
// so the live sidebar updates.
func (h *rtmEventHandler) OnConversationOpened(ch slack.Channel) {
	if h.wsCtx == nil {
		return
	}

	item, finderItem := buildChannelItem(ch, h.wsCtx, h.cfg, h.workspaceID)
	if h.db != nil {
		upsertChannelInDB(h.db, ch, item.Type, h.workspaceID)
	}

	// Persist in the workspace context so a workspace switch later
	// shows the new conversation. De-dupe on ID — the same event can
	// arrive twice (e.g. im_open followed by im_created on first DM).
	// No read-state preservation is needed: those fields no longer
	// live on ChannelItem; the read-state DB (per workspace) is the
	// single source of truth and is unaffected by this in-memory upsert.
	replaced := false
	for i := range h.wsCtx.Channels {
		if h.wsCtx.Channels[i].ID == item.ID {
			h.wsCtx.Channels[i] = item
			replaced = true
			break
		}
	}
	if !replaced {
		h.wsCtx.Channels = append(h.wsCtx.Channels, item)
		// FinderItems is intentionally only appended on the new-channel
		// path. On dedupe, the existing finder entry was added at
		// bootstrap (or a prior open) and carries no unread state to
		// refresh, so re-appending would double-list the channel in
		// Ctrl+T.
		finderItem.LastVisited = h.wsCtx.LastVisitedByChannel[ch.ID]
		h.wsCtx.FinderItems = append(h.wsCtx.FinderItems, finderItem)
	}

	// Mirror channelTypes / channelNames maps used by the notifier so
	// follow-up messages on this channel get notified correctly.
	if h.channelNames != nil {
		h.channelNames[ch.ID] = item.Name
	}
	if h.channelTypes != nil {
		h.channelTypes[ch.ID] = item.Type
	}

	if h.program == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		// Persistence above already updated wctx.Channels; defer the
		// UI message until the user switches into this workspace.
		return
	}
	h.program.Send(ui.ConversationOpenedMsg{
		TeamID: h.workspaceID,
		Item:   item,
	})
}

// refreshSectionsForActive re-syncs every wctx.Channels item's Section
// field with the current SectionStore state, then (if this workspace
// is active) posts a SectionsRefreshedMsg so the App rebuckets the
// sidebar. Inactive workspaces still get their wctx.Channels mutated
// in place; the user sees the refresh on next workspace switch.
//
// Called from the four channel-section WS event handlers after they've
// already applied their delta to the store.
func (h *rtmEventHandler) refreshSectionsForActive() {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	store := h.wsCtx.SectionStore
	if !store.Ready() {
		return
	}
	// Update Section field on every channel in the workspace context
	// based on current store state. Channels not claimed by any
	// section have Section reset to "" — letting the sidebar's Slack
	// mode bucket them via type-default fallback (Task 8) or the
	// config-glob path if Slack mode isn't active.
	for i := range h.wsCtx.Channels {
		item := &h.wsCtx.Channels[i]
		if id, ok := store.SectionForChannel(item.ID); ok {
			item.Section = id
		} else {
			item.Section = ""
		}
		// SectionOrder is unused in Slack mode (linked-list order
		// comes from the provider); reset to 0 for consistency.
		item.SectionOrder = 0
	}
	if h.program == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		return
	}
	// Send a copy so the App can mutate without racing the workspace's
	// mutator path.
	channelsCopy := make([]sidebar.ChannelItem, len(h.wsCtx.Channels))
	copy(channelsCopy, h.wsCtx.Channels)
	h.program.Send(ui.SectionsRefreshedMsg{
		TeamID:   h.workspaceID,
		Channels: channelsCopy,
	})
}

// OnChannelSectionUpserted handles section create/rename/reorder/emoji-change.
// The store applies last-write-wins; the sidebar refresh is a no-op for
// channels (no membership change) but invalidates the cache so renames
// re-render section header labels.
func (h *rtmEventHandler) OnChannelSectionUpserted(ev slackclient.ChannelSectionUpserted) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyUpsert(ev)
	h.refreshSectionsForActive()
}

// OnChannelSectionDeleted handles section delete. Channels formerly in
// the section have their channel→section mapping dropped by the store;
// refreshSectionsForActive then resets Section="" on those items and
// the sidebar rebuckets them into the type-default bucket.
func (h *rtmEventHandler) OnChannelSectionDeleted(sectionID string) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyDelete(sectionID)
	h.refreshSectionsForActive()
}

// OnChannelSectionChannelsUpserted handles channels added (or moved
// between sections). The store overwrites prior section membership;
// refreshSectionsForActive picks up the new IDs.
func (h *rtmEventHandler) OnChannelSectionChannelsUpserted(sectionID string, channelIDs []string) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyChannelsAdded(sectionID, channelIDs)
	h.refreshSectionsForActive()
}

// OnChannelSectionChannelsRemoved handles channels removed from a section.
// The store drops them from channelToSection; refreshSectionsForActive
// resets their Section="" and the sidebar rebuckets via type-default.
func (h *rtmEventHandler) OnChannelSectionChannelsRemoved(sectionID string, channelIDs []string) {
	if h.wsCtx == nil || h.wsCtx.SectionStore == nil {
		return
	}
	h.wsCtx.SectionStore.ApplyChannelsRemoved(sectionID, channelIDs)
	h.refreshSectionsForActive()
}

// OnPrefChange handles user-pref mutations from the WebSocket. Currently
// the only pref slk reacts to is muted_channels: the MuteStore is
// updated and (when the set actually changed) every wctx.Channels item's
// IsMuted flag is recomputed and the active sidebar is asked to
// re-render. Other prefs are ignored — add a case here when slk grows
// support for them.
func (h *rtmEventHandler) OnPrefChange(name, value string) {
	debuglog.WS("pref_change received: name=%q value-len=%d", name, len(value))
	// Both names are routes to mute state. all_notifications_prefs is
	// the live per-channel notification blob (current Slack); the flat
	// muted_channels pref is legacy back-compat.
	if name != "muted_channels" && name != "all_notifications_prefs" {
		return
	}
	if h.wsCtx == nil || h.wsCtx.MuteStore == nil {
		return
	}
	changed := h.wsCtx.MuteStore.ApplyPrefChange(name, value)
	debuglog.WS("pref_change %s for %s: changed=%v muted=%v", name, h.wsCtx.TeamName, changed, h.wsCtx.MuteStore.MutedChannels())
	if !changed {
		return
	}
	h.refreshMutedForActive()
}

func (h *rtmEventHandler) OnMemberJoined(channelID, userID string) {
	if h.wsCtx == nil || h.wsCtx.Membership == nil {
		return
	}
	h.wsCtx.Membership.ApplyJoin(channelID, userID)
}

func (h *rtmEventHandler) OnMemberLeft(channelID, userID string) {
	if h.wsCtx == nil || h.wsCtx.Membership == nil {
		return
	}
	h.wsCtx.Membership.ApplyLeave(channelID, userID)
}

// refreshMutedForActive walks wctx.Channels, refreshes each item's
// IsMuted flag from the current MuteStore, and posts a
// SectionsRefreshedMsg so the App rebuilds the sidebar from the
// updated list. Mirrors refreshSectionsForActive but for the mute
// dimension; reuses the same message because the App treats it as a
// "channel-list-attributes-changed" signal regardless of what
// changed.
func (h *rtmEventHandler) refreshMutedForActive() {
	if h.wsCtx == nil || h.wsCtx.MuteStore == nil {
		return
	}
	store := h.wsCtx.MuteStore
	for i := range h.wsCtx.Channels {
		chID := h.wsCtx.Channels[i].ID
		before := h.wsCtx.Channels[i].IsMuted
		after := store.IsMuted(chID)
		if before != after {
			debuglog.Cache("refreshMutedForActive: channel=%s name=%q muted_before=%v muted_after=%v",
				chID, h.wsCtx.Channels[i].Name, before, after)
		}
		h.wsCtx.Channels[i].IsMuted = after
	}
	if h.program == nil {
		return
	}
	if h.isActive != nil && !h.isActive() {
		return
	}
	channelsCopy := make([]sidebar.ChannelItem, len(h.wsCtx.Channels))
	copy(channelsCopy, h.wsCtx.Channels)
	h.program.Send(ui.SectionsRefreshedMsg{
		TeamID:   h.workspaceID,
		Channels: channelsCopy,
	})
}

// listWorkspaces prints the configured workspaces with their TeamID and
// Name, one per line. Useful for users who want to hand-edit per-workspace
// settings in config.toml.
func listWorkspaces() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}
	configPath := filepath.Join(xdgConfig(), "config.toml")
	cfg, _ := config.Load(configPath) // best-effort

	// Print in the same order the rail would use, so the digit-key
	// mapping is obvious from the output.
	orderedTokens := config.OrderTokens(tokens, cfg)

	idW, slugW, nameW := len("TEAM ID"), len("SLUG"), len("NAME")
	for _, ot := range orderedTokens {
		if len(ot.Token.TeamID) > idW {
			idW = len(ot.Token.TeamID)
		}
		if len(ot.Slug) > slugW {
			slugW = len(ot.Slug)
		}
		if len(ot.Token.TeamName) > nameW {
			nameW = len(ot.Token.TeamName)
		}
	}
	fmt.Printf("%-*s  %-*s  %s\n", idW, "TEAM ID", slugW, "SLUG", "NAME")
	fmt.Printf("%s  %s  %s\n",
		strings.Repeat("-", idW),
		strings.Repeat("-", slugW),
		strings.Repeat("-", nameW))
	for _, ot := range orderedTokens {
		fmt.Printf("%-*s  %-*s  %s\n", idW, ot.Token.TeamID, slugW, ot.Slug, ot.Token.TeamName)
	}
	return nil
}

// dumpPrefs is a diagnostic command that calls users.prefs.get for
// every configured workspace and prints the raw JSON response. Use
// this when the muted-channel UI treatment isn't behaving as
// expected to confirm what Slack is (or isn't) returning for the
// muted_channels pref.
func dumpPrefs() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}
	ctx := context.Background()
	for _, tok := range tokens {
		fmt.Printf("=== %s (%s) ===\n", tok.TeamName, tok.TeamID)
		client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
		if err := client.Connect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  connect failed: %v\n\n", err)
			continue
		}
		raw, err := client.GetMutedChannelsRaw(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  fetch failed: %v\n\n", err)
			continue
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(raw))
		}
		fmt.Println()
	}
	return nil
}

// dumpSections is a diagnostic command that calls users.channelSections.list
// for every configured workspace and prints the raw JSON response, pretty-
// printed. Intended for reverse-engineering the undocumented endpoint; safe
// to remove once we ship server-side section support.
func dumpSections() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}

	ctx := context.Background()
	for _, tok := range tokens {
		fmt.Printf("=== %s (%s) ===\n", tok.TeamName, tok.TeamID)
		client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
		// Connect resolves the per-workspace API base URL via auth.test;
		// required for enterprise grid hosts.
		if err := client.Connect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  connect failed: %v\n\n", err)
			continue
		}
		raw, err := client.GetChannelSectionsRaw(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  fetch failed: %v\n\n", err)
			continue
		}
		// Pretty-print if it parses as JSON; otherwise dump raw.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(raw))
		}
		// Detect pagination truncation. GetChannelSectionsRaw is intentionally
		// first-page-only for the diagnostic; warn so the user knows.
		var trunc struct {
			Cursor string `json:"cursor"`
		}
		if err := json.Unmarshal(raw, &trunc); err == nil && trunc.Cursor != "" {
			fmt.Fprintf(os.Stderr, "  warning: response cursor=%q; additional sections beyond first page were not fetched\n", trunc.Cursor)
		}
		fmt.Println()
	}
	return nil
}
