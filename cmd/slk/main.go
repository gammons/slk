package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/editor"
	emojiwidth "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/export"
	"github.com/gammons/slk/internal/filedl"
	"github.com/gammons/slk/internal/ids"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/notify"
	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/workspace"
	versionpkg "github.com/gammons/slk/internal/version"
	"github.com/gammons/slk/internal/wake"
	"golang.design/x/clipboard"
	"golang.org/x/term"
)

// Build-time version info, injected via -ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

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

// newImageHTTPClient builds the HTTP client the avatar/thumbnail
// fetcher uses.
//
// Split out of run() so a test can pin the wiring: the difference
// between this and the XHR client is invisible at the call site but
// changes every asset request on the wire.
func newImageHTTPClient() *http.Client {
	c := slackhttp.NewImageHTTPClient(nil)
	c.Timeout = 10 * time.Second
	return c
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
	styles.LoadCustomThemes(os.DirFS(themesDir))
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
	if IsWayland() {
		if HasWlPaste() {
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
		slackdesktop.Tokens,
		slackclient.MintToken,
		tokenStore.Save,
	)

	// Initialize services
	wsMgr := service.NewWorkspaceManager(db)
	msgSvc := service.NewMessageService(db)
	_ = msgSvc // will wire for send/receive

	// Create app
	app := ui.NewApp()
	// Frame-correlated sixel output: the App publishes immutable
	// placement snapshots into sixelFrames; terminalOutput strips the
	// internal frame marker from Bubble Tea's window title, takes the
	// exact flushed frame, and paints its sixel operations after the
	// text diff. Kitty uploads share the same serialized writer so
	// byte streams from different goroutines can't interleave.
	sixelFrames := imgpkg.NewSixelFrameStore()
	terminalOutput := imgpkg.NewFrameOutput(os.Stdout, sixelFrames)
	imgpkg.KittyOutput = terminalOutput.SideChannel()
	app.SetSixelFrameStore(sixelFrames)
	app.SetHelpFooter(versionpkg.ModalFooter(version))
	app.SetClipboardAvailable(clipboardOK)
	desktop := core.DesktopServiceFuncs{
		Open:          launchOS,
		ReadClipboard: nativeClipboardRead,
		Stat:          os.Stat,
		SaveThread:    export.SaveThread,
	}
	if sr := notify.NewStatusReporter(cfg.Notifications.StatusCommand); sr != nil {
		// Enqueue never blocks a render: it hands the state to the reporter's
		// single worker, which serializes runs and coalesces bursts so the
		// external surface can't end up pinned to a stale count by an
		// out-of-order subprocess.
		desktop.ReportStatus = sr.Enqueue
	}
	if useWaylandClipboard {
		desktop.ReadClipboard = WaylandClipboardReader()
	}
	app.SetDesktopService(core.NewDesktopService(desktop))

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
	imageFetcher := imgpkg.NewFetcher(imageCache, newImageHTTPClient())
	imageFetcher.SetAuths(auths)

	// File attachment downloads (`d` keybinding) share the image
	// fetcher's auth mechanism via slackhttp.AuthResolver. The
	// downloader gets its own resolver instance; it learns foreign-team
	// (Slack Connect) auth independently of the image fetcher.
	// Destination directory is configurable via [general].download_dir
	// (default "~/Downloads", expanded by config.Load).
	fileDownloader := filedl.New(slackhttp.NewAuthResolver(auths),
		cfg.General.DownloadDir)

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

	// Sixel capability probe (issue #116). Detect only knows the handful
	// of terminals it can name from $TERM / $TERM_PROGRAM, so plenty of
	// sixel-capable terminals (xterm -ti vt340, DomTerm, toyterm, foot
	// behind a generic TERM, …) land on halfblock and render the blocky
	// mosaic even though real pixels would work. Ask the terminal
	// directly via DA1 before settling for halfblock.
	//
	// Only in auto mode — an explicit image_protocol=halfblock must stay
	// half-block — and never inside a multiplexer: tmux keeps sixel off
	// by policy (see Detect's doc comment) and zellij has no sixel path
	// at all, so probing there only risks paying the full timeout for an
	// answer we would ignore.
	if proto == imgpkg.ProtoHalfBlock &&
		imgpkg.IsAutoProtocol(cfg.Appearance.ImageProtocol) &&
		os.Getenv("TMUX") == "" && os.Getenv("ZELLIJ") == "" &&
		term.IsTerminal(int(os.Stdin.Fd())) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			debuglog.ImgRender("sixel probe skipped: cannot enter raw mode: %v", err)
		} else {
			ok := imgpkg.ProbeSixel(os.Stdout, os.Stdin, 200*time.Millisecond)
			if rerr := term.Restore(int(os.Stdin.Fd()), state); rerr != nil {
				debuglog.ImgRender("term restore after sixel probe: %v", rerr)
			}
			if ok {
				debuglog.ImgRender("sixel probe succeeded, upgrading halfblock to sixel")
				proto = imgpkg.ProtoSixel
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

	// Cell pixel metrics for image encoding. Sixel uses them for its
	// absolute raster dimensions; kitty places by cell but uses them to
	// transmit enough source pixels for the terminal's native cell
	// resolution. SetCellPixels retains its 8x16 fallback when the
	// terminal reports no usable geometry.
	pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
	debuglog.ImgRender("cell pixels: %dx%d", pxW, pxH)
	imgpkg.SetCellPixels(pxW, pxH)

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
	// The rail reader asks these workspaces about unread threads: the
	// configured set, not the connected one, so a workspace still
	// connecting keeps last session's thread dot the way it keeps its
	// channel dot.
	railTeamIDs := make([]string, 0, len(wsItems))
	for _, it := range wsItems {
		railTeamIDs = append(railTeamIDs, it.ID)
	}
	app.SetTypingEnabled(cfg.Animations.TypingIndicators)
	app.SetSidebarStaleThreshold(time.Duration(cfg.Sidebar.HideInactiveAfterDays) * 24 * time.Hour)
	app.SetMouseWheelLines(cfg.Appearance.MouseWheelLines)
	app.SetColoredUsernames(cfg.Appearance.ColoredUsernames)
	app.SetEditorService(core.NewEditorService(editor.WriteDraft, editor.Edit, editor.TakeDraft))
	if editor, ok := ui.ResolveEditor(cfg.Compose.Editor); ok {
		app.SetComposeEditor(editor)
	}

	// Wire theme switcher
	app.SetThemeItems(styles.ThemeNames())
	app.SetThemeOverrides(cfg.Theme)

	// AvatarFunc is wired below, after `router` is declared, because
	// the lazy-fetch path needs router.Active().AvatarURLs to look up
	// the avatar URL on cache misses.

	// Declare p before wiring callbacks so closures can capture it
	var p *tea.Program
	workspacesStore := newWorkspaceConfigStore(cfg)

	// router holds the program-wide active workspace pointer. All
	// wireCallbacks-registered callbacks read router.Active() at
	// invocation time so they always see the current workspace.
	router := newWorkspaceRouter()

	// Wire avatar rendering with a lazy-fetch path. AvatarFunc is
	// called by the messages/thread panes on the bubbletea Update
	// goroutine for every message authored row. The fast path is a
	// straight map lookup; on miss, we trigger a background Preload
	// keyed by the workspace's AvatarURLs (populated at connect time
	// from the local user cache and from the boot response).
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
	app.SetAvatarService(core.NewAvatarService(func(userID string) string {
		if rendered := avatarCache.Get(userID); rendered != "" {
			return rendered
		}
		// Cache miss: trigger a lazy Preload using the URL the
		// workspace recorded at connect time (or that resolveUser
		// filled in). No router-active = pre-workspace-ready render;
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
	}))

	// Wire theme switcher: dispatch to the appropriate saver based on scope.
	saveTheme := func(name string, scope themeswitcher.ThemeScope) {
		switch scope {
		case themeswitcher.ScopeWorkspace:
			active := router.Active()
			if active == nil {
				return // shouldn't happen, but guard against it
			}
			teamID := active.TeamID
			teamName := teamID
			if wctx := router.ByID(teamID); wctx != nil && wctx.TeamName != "" {
				teamName = wctx.TeamName
			}
			tomlKey := workspacesStore.SaveTheme(teamID, name)
			// Persist.
			if err := saveWorkspaceTheme(configPath, tomlKey, teamID, teamName, name); err != nil {
				log.Printf("save workspace theme: %v", err)
			}
		case themeswitcher.ScopeGlobal:
			cfg.Appearance.Theme = name
			if err := saveGlobalTheme(configPath, name); err != nil {
				log.Printf("save global theme: %v", err)
			}
		}
	}

	// Wire sidebar width saver: always persist to the active workspace.
	saveSidebarWidth := func(width int) {
		active := router.Active()
		if active == nil {
			return
		}
		teamID := active.TeamID
		teamName := teamID
		if wctx := router.ByID(teamID); wctx != nil && wctx.TeamName != "" {
			teamName = wctx.TeamName
		}
		tomlKey := workspacesStore.SaveSidebarWidth(teamID, width)
		if err := saveWorkspaceWidth(configPath, tomlKey, teamID, teamName, width); err != nil {
			log.Printf("save workspace sidebar width: %v", err)
		}
	}
	app.SetSettingsService(core.NewSettingsService(saveTheme, saveSidebarWidth))

	// Wire presence/DND status setter. Resolves the active team ID
	// through the router at invocation so the closure always targets
	// the currently-active workspace context.
	setStatus := func(action presencemenu.Action, snoozeMinutes int) {
		wctx := router.Active()
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
	}

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
		channelReadStates := func() map[string]cache.ReadState {
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
		}

		unreadWorkspaces := func() []string {
			unread, err := db.UnreadChannels()
			if err != nil {
				log.Printf("Warning: UnreadChannels: %v", err)
				return nil
			}
			return railUnreadWorkspaces(unread, railTeamIDs, router.ByID, railThreadsUnread(db))
		}
		app.SetUnreadService(core.NewUnreadService(channelReadStates, unreadWorkspaces))

		app.SetChannelService(core.NewChannelService(core.ChannelServiceFuncs{
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
			// The finder's non-joined results. Debounced by the App
			// (see scheduleChannelSearch) and only ever called for a
			// non-empty query, so this runs once per typing pause
			// rather than once per boot per workspace, which is what
			// the conversations.list walk it replaced did.
			SearchRemote: func(query string) []channelfinder.Item {
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				return searchChannelsRemote(ctx, wctx.Edge, wctx.LastVisitedByChannel, query)
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
			OpenConversation: func(userIDs []string, requestID uint64) core.Cmd {
				wctx := router.Active()
				if wctx == nil {
					return func() core.Msg {
						return ui.NewMessageFailedMsg{
							RequestID: requestID,
							Err:       fmt.Errorf("no active workspace"),
						}
					}
				}
				client := wctx.Client
				return func() core.Msg {
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
			Fetch: func(channelID ids.ChannelID, channelName string) core.Msg {
				chIDStr := string(channelID)
				wctx := router.Active()
				if wctx == nil || wctx.Client == nil {
					return nil
				}
				msgItems := fetchChannelMessages(wctx.Client, chIDStr, db, wctx.UserNames, tsFormat, avatarCache, router)

				state, _ := db.GetChannelReadState(chIDStr)
				lastReadTS := state.LastReadTS

				// Mark channel as read up to the latest message
				markedTS := ""
				if len(msgItems) > 0 {
					markedTS = msgItems[len(msgItems)-1].TS
					markChannelReadAsync(ctx, wctx.Client, db, p, chIDStr, markedTS)
				}

				return ui.MessagesLoadedMsg{
					ChannelID:  chIDStr,
					Messages:   msgItems,
					LastReadTS: lastReadTS,
					// Reported so the reducer can record it on the
					// Update goroutine and suppress the echo of this
					// mark; see ui.MessagesLoadedMsg.MarkedTS.
					MarkedTS: markedTS,
				}
			},
			MarkRead: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
				wctx := router.Active()
				if wctx == nil || wctx.Client == nil {
					return nil
				}
				markChannelReadAsync(ctx, wctx.Client, db, p, string(channelID), string(ts))
				return nil // ChannelMarkedReadMsg is emitted from inside the goroutine
			},
			FetchOlder: func(channelID ids.ChannelID, oldestTS ids.MessageTS) core.Msg {
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
			FetchAround: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
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
			Join: func(channelID ids.ChannelID, channelName string) core.Msg {
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

		app.SetSearchService(core.NewSearchService(core.SearchServiceFuncs{
			SearchChannel: func(channelID ids.ChannelID, query string) core.Msg {
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

		app.SetMessageService(core.NewMessageService(core.MessageServiceFuncs{
			Send: func(channelID ids.ChannelID, text string) core.Msg {
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
			Edit: func(channelID ids.ChannelID, ts ids.MessageTS, text string) core.Msg {
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
			Delete: func(channelID ids.ChannelID, ts ids.MessageTS) core.Msg {
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
			MarkUnread: func(channelID ids.ChannelID, threadTS ids.ThreadTS, boundaryTS ids.MessageTS, unreadCount int) core.Msg {
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
						// Recount the badge from the new boundary.
						//
						// This used to write 0 and wait for Slack's
						// echoed *_marked event to supply the real
						// number. The echo does not carry one: marking
						// a direct mention unread left the channel
						// showing a plain dot, losing exactly the
						// signal the user was trying to preserve.
						//
						// Counting the cached messages at or after the
						// boundary is not the invented number that
						// comment was avoiding — the user picked the
						// boundary off their own screen, so those
						// messages are cached. A failure here leaves
						// the previous count rather than zeroing it,
						// since a stale badge beats a vanished one.
						chType := ""
						if wctx.RTMHandler != nil {
							chType = wctx.RTMHandler.channelTypes[chIDStr]
						}
						n, cntErr := countMentionsSince(db, chType, chIDStr, boundaryTSStr, wctx.Client.UserID())
						if cntErr != nil {
							log.Printf("Warning: failed to count mentions on mark-unread %s: %v", chIDStr, cntErr)
						} else if dbErr := db.SetChannelMentionCount(chIDStr, n); dbErr != nil {
							log.Printf("Warning: failed to set mention count on mark-unread %s: %v", chIDStr, dbErr)
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
					// updates immediately via applyThreadMarkUnread; on
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

		upload := func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) core.Cmd {
			return func() core.Msg {
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
		}
		app.SetFileService(core.NewFileService(upload, fileDownloader.Download))

		app.SetThreadService(core.NewThreadService(core.ThreadServiceFuncs{
			Fetch: func(channelID ids.ChannelID, threadTS ids.ThreadTS) core.Msg {
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
			Mark: func(channelID ids.ChannelID, threadTS ids.ThreadTS, ts ids.MessageTS) core.Cmd {
				chIDStr, threadTSStr, tsStr := string(channelID), string(threadTS), string(ts)
				wctx := router.Active()
				if wctx == nil {
					return nil
				}
				client := wctx.Client
				teamID := wctx.TeamID
				return func() core.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					// markThreadRead persists the cursor only after
					// Slack accepts; see its doc comment.
					if err := markThreadRead(ctx, client, db, teamID, chIDStr, threadTSStr, tsStr); err != nil {
						log.Printf("Warning: MarkThread(%s, %s): %v", chIDStr, threadTSStr, err)
						return ui.ThreadMarkedLocalMsg{
							ChannelID: chIDStr, ThreadTS: threadTSStr, TS: tsStr, Err: err,
						}
					}
					return ui.ThreadMarkedLocalMsg{
						ChannelID: chIDStr, ThreadTS: threadTSStr, TS: tsStr,
					}
				}
			},
			SendReply: func(channelID ids.ChannelID, threadTS ids.ThreadTS, text string, broadcast bool) core.Msg {
				chIDStr, threadTSStr := string(channelID), string(threadTS)
				wctx := router.Active()
				if wctx == nil {
					return ui.ThreadReplySendFailedMsg{ChannelID: chIDStr, ThreadTS: threadTSStr, Reason: "no active workspace"}
				}
				client := wctx.Client
				userNames := wctx.UserNames
				ctx := context.Background()
				ts, sentMrkdwn, err := client.SendReply(ctx, chIDStr, threadTSStr, text, broadcast)
				if err != nil {
					log.Printf("Warning: failed to send thread reply: %v", err)
					return ui.ThreadReplySendFailedMsg{ChannelID: chIDStr, ThreadTS: threadTSStr, Reason: err.Error()}
				}
				userName := "you"
				if resolved, ok := userNames[client.UserID()]; ok {
					userName = resolved
				}
				// Broadcast replies surface in the parent channel feed as
				// thread_broadcast rows; flag the authoritative copy so the
				// reducer's channel-pane swap renders the same label the WS
				// echo would have carried.
				subtype := ""
				if broadcast {
					subtype = "thread_broadcast"
				}
				return ui.ThreadReplySentMsg{
					ChannelID: chIDStr,
					ThreadTS:  threadTSStr,
					Broadcast: broadcast,
					Message: messages.MessageItem{
						TS:        ts,
						UserID:    client.UserID(),
						UserName:  userName,
						Text:      sentMrkdwn,
						Timestamp: formatTimestamp(ts, tsFormat),
						ThreadTS:  threadTSStr,
						Subtype:   subtype,
					},
				}
			},
			ListFetch: func(teamID ids.TeamID) core.Msg {
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
			// Workspace-ready (boot) is the primary trigger, with
			// Threads-view activation as the safety net; the gate
			// inside ensureThreadSubscriptions collapses both to one
			// throttled, staggered getView sweep per workspace.
			// Returns immediately; the list renders from cache and
			// refreshes via ThreadsListDirtyMsg when the fetch lands.
			//
			// ByID, not Active: boot fires this for every workspace
			// as it connects, including background ones the user may
			// never activate this session — on Enterprise Grid those
			// are exactly the workspaces whose threads would
			// otherwise stay stale.
			EnsureSubscriptions: func(teamID ids.TeamID) {
				wctx := router.ByID(string(teamID))
				if wctx == nil || wctx.Client == nil {
					return
				}
				ensureWorkspaceThreadSubs(ctx, wctx, db, p.Send)
			},
			ThreadLastRead: func(channelID ids.ChannelID, threadTS ids.ThreadTS) string {
				wctx := router.Active()
				if wctx == nil {
					return ""
				}
				lastRead, err := db.GetThreadLastRead(wctx.TeamID, string(channelID), string(threadTS))
				if err != nil {
					debuglog.Cache("ThreadLastRead: %s/%s: %v", channelID, threadTS, err)
					return ""
				}
				return lastRead
			},
		}))

		app.SetReactionService(core.NewReactionService(
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

		sendTyping := func(channelID string) {
			wctx := router.Active()
			if wctx == nil {
				return
			}
			_ = wctx.Client.SendTyping(channelID)
		}
		app.SetPresenceService(core.NewPresenceService(setStatus, sendTyping))

	}

	// Bind all callbacks once. They read router.Active() at invocation.
	wireCallbacks(router)

	// Wire workspace switcher
	app.SetWorkspaceService(core.NewWorkspaceService(func(teamID string) core.Msg {
		wctx := router.ByID(teamID)
		if wctx == nil {
			return nil
		}

		// Update active pointer; callbacks read router.Active() at
		// invocation time, so no closure rebinding is needed. Theme/
		// SidebarWidth below still go through workspacesStore, not the
		// outer cfg, for the same reason.
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

		// Statuses are re-read from the cache, which live changes keep
		// current, rather than the connect-time items. DND is not
		// cached; RefreshPeerDND below fetches it after the switch
		// applies, so the switch itself returns without waiting on the
		// network.
		statuses := cachedPeerStatuses(db, wctx.TeamID)
		wctx.PeerStatus.SeedHuddles(statuses)
		channels := withPeerStatuses(wctx.Channels, statuses)

		snap := workspacesStore.Snapshot()
		return ui.WorkspaceSwitchedMsg{
			TeamID:           wctx.TeamID,
			TeamName:         wctx.TeamName,
			Domain:           wctx.Client.TeamSubdomain(),
			Theme:            snap.ResolveTheme(teamID),
			SidebarWidth:     snap.ResolveWidth(teamID),
			Channels:         channels,
			FinderItems:      wctx.FinderItems,
			UserNames:        wctx.UserNames,
			UserStatuses:     statuses,
			ExternalUsers:    external,
			UserID:           wctx.UserID,
			CustomEmoji:      wctx.CustomEmoji(),
			UserGroups:       wctx.UserGroups(),
			SectionsProvider: sectionsProviderAdapter{store: wctx.SectionStore},
			// Ordered by reduceWorkspaceSwitched to run after
			// ResetPresence and the activeTeamID update, so its
			// UserDNDChangeMsg result isn't wiped or dropped as stale.
			RefreshPeerDND: func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), dndRefreshTimeout)
				defer cancel()
				wctx.PeerStatus.RefreshDND(ctx, workspacePresenceIDs(wctx))
				return nil
			},
		}
	}))

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
	// race where two simultaneous WorkspaceReadyMsgs both observed no
	// active workspace yet and both set InitialActive=true.
	var firstReady sync.Once

	// Start the TUI immediately (shows loading overlay). All output —
	// every protocol — flows through the frame-correlated writer: it is
	// a serialized pass-through for non-sixel frames (no marker present)
	// and the sixel paint site for marked frames.
	p = tea.NewProgram(app, tea.WithOutput(terminalOutput))

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
			// cfg as this goroutine currently receives it: a value
			// copy from workspacesStore, taken once at connect time.
			cfgSnap := workspacesStore.Snapshot()
			wctx, err := connectWorkspace(ctx, tok, db, cfgSnap, avatarCache, p, configPath)
			if err != nil {
				// Log it. WorkspaceFailedMsg carries only the team
				// name, so without this the reason never reaches the
				// user OR the debug log, and a workspace that fails to
				// connect is indistinguishable from one that connected
				// and found nothing: empty sidebar, no threads, and
				// "no active workspace" from every service closure.
				//
				// That cost a full round trip with a Grid user in #5,
				// whose users.conversations call was being rejected
				// with enterprise_is_restricted while slk reported
				// nothing at all.
				log.Printf("workspace %s failed to connect: %v", tok.TeamName, err)
				debuglog.General("workspace %s failed to connect: %v", tok.TeamName, err)
				p.Send(ui.WorkspaceFailedMsg{TeamName: tok.TeamName})
				return
			}

			router.Add(wctx)
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
				}
				// else: not the configured default; never claim.
			} else {
				firstReady.Do(func() {
					isInitial = true
					router.Set(wctx)
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
				isActive:        func() bool { a := router.Active(); return a != nil && a.TeamID == teamID },
				notifier:        notifier,
				notifyCfg:       cfgSnap.Notifications,
				currentUserID:   wctx.UserID,
				channelNames:    channelNames,
				channelTypes:    channelTypes,
				workspaceName:   wctx.TeamName,
				activeChannelID: func() string { return app.ActiveChannelID() },
				cfg:             cfgSnap,
				wsCtx:           wctx,
				backfillGate:    dedupeGate{window: 30 * time.Second},
				// The reconnect refresh, deliberately NOT the
				// ChannelService.Fetch closure: that one also marks the
				// channel read, which is right when the user just
				// clicked into it and wrong here, where slk is catching
				// up on messages that arrived while it was offline and
				// the user may not have looked at the terminal for
				// hours.
				refreshChannel: func(ctx context.Context, channelID string) {
					msgItems := fetchChannelMessages(wctx.Client, channelID, db, wctx.UserNames, tsFormat, avatarCache, router)
					state, _ := db.GetChannelReadState(channelID)
					p.Send(ui.MessagesLoadedMsg{
						ChannelID:  channelID,
						Messages:   msgItems,
						LastReadTS: state.LastReadTS,
					})
				},
				// Reconnect/wake kick for the throttled thread-
				// subscription sync; the gate inside decides whether a
				// getView sweep actually runs.
				ensureThreadSubs: func() {
					ensureWorkspaceThreadSubs(context.Background(), wctx, db, p.Send)
				},
				resolveConversation: wctx.Client.GetConversationInfo,
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

			readyStatuses := cachedPeerStatuses(db, wctx.TeamID)
			wctx.PeerStatus.SeedHuddles(readyStatuses)
			p.Send(ui.WorkspaceReadyMsg{
				TeamID:           wctx.TeamID,
				TeamName:         wctx.TeamName,
				Domain:           wctx.Client.TeamSubdomain(),
				Theme:            cfgSnap.ResolveTheme(wctx.TeamID),
				SidebarWidth:     cfgSnap.ResolveWidth(wctx.TeamID),
				Channels:         wctx.Channels,
				FinderItems:      wctx.FinderItems,
				UserNames:        wctx.UserNames,
				UserStatuses:     readyStatuses,
				ExternalUsers:    external,
				UserID:           wctx.UserID,
				CustomEmoji:      wctx.CustomEmoji(), // bootstrap subset; replaced by the goroutine below
				UserGroups:       wctx.UserGroups(),  // empty at this point; filled by the goroutine below
				SectionsProvider: sectionsProviderAdapter{store: wctx.SectionStore},
				InitialActive:    isInitial,
				LastChannelID:    mostRecentlyVisitedChannel(wctx.LastVisitedByChannel),
			})

			// Fetch the workspace's custom emoji in the background. When
			// done, a follow-up message makes rendering and the emoji
			// picker pick up the full set. Runs unconditionally — see
			// fetchWorkspaceEmoji for why the bootstrap subset must not
			// be treated as an answer.
			go fetchWorkspaceEmoji(ctx, wctx, wctx.Client, p, wctx.TeamID)

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

			// Resolve unknown DM user names in background
			if len(wctx.UnresolvedDMs) > 0 {
				go resolveDMNames(wctx, db, avatarCache, func(msg tea.Msg) {
					if p != nil {
						p.Send(msg)
					}
				})
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
		for _, wctx := range router.All() {
			if wctx == nil || wctx.RTMHandler == nil {
				continue
			}
			wctx.RTMHandler.syncOnReconnect("wake")
		}
	}).Run(wakeCtx)

	_, err = p.Run()

	// Dump the API request tally before anything else at shutdown.
	//
	// Phase 2b's success criteria are call counts -- "a boot issues
	// <= 10 API calls, with zero users.list and zero per-channel
	// conversations.history fan-out" -- and nothing in slk could
	// report them. Reconstructing the numbers from a debug log only
	// worked at all because triggerBackfill happens to log per
	// channel; there was no way to see users.list or a total.
	//
	// Nobody is testing slk against a real Enterprise Grid account
	// until the whole grid-parity series lands, so this is the only
	// feedback loop the work has.
	if debuglog.Enabled() {
		debuglog.General("shutdown API request tally:\n%s", slackhttp.DefaultCounter.Report())
	}

	// Clean up connection managers
	for _, wctx := range router.All() {
		if wctx.ConnMgr != nil {
			wctx.ConnMgr.Stop()
		}
	}

	return err
}
