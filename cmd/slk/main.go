package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/avatar"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	emojiwidth "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/notify"
	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackfmt"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/compose"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/presencemenu"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/themeswitcher"
	"github.com/gammons/slk/internal/ui/workspace"
	emoji "github.com/kyokomi/emoji/v2"
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

// WorkspaceContext holds all state for a single connected workspace.
type WorkspaceContext struct {
	Client      *slackclient.Client
	ConnMgr     *slackclient.ConnectionManager
	RTMHandler  *rtmEventHandler
	UserNames   map[string]string
	// UserNamesByHandle maps a user's handle (the Slack `name` field
	// without an `@`) to a display name. Used to resolve participant
	// handles in mpdm channel names like `mpdm-grant--myles--ray-1`.
	UserNamesByHandle map[string]string
	// BotUserIDs is the set of user IDs known to be Slack apps or bots.
	// Populated from the local cache on startup and refreshed by the
	// background users.list fetch and any on-demand resolveUser calls.
	// Used during channel construction to bucket app DMs into a separate
	// "Apps" sidebar section.
	BotUserIDs        map[string]bool
	LastReadMap       map[string]string
	Channels    []sidebar.ChannelItem
	// FinderItems is the merged list shown in the Ctrl+T finder. Initially
	// contains only joined channels; the BrowseableChannelsLoadedMsg pipeline
	// extends it with non-joined public channels in the background.
	FinderItems   []channelfinder.Item
	TeamID        string
	TeamName      string
	UserID        string
	UnresolvedDMs []UnresolvedDM
	CustomEmoji   map[string]string // emoji name -> URL or "alias:target"
	// Self presence and DND state for this workspace. Populated on connect
	// and updated by manual_presence_change / dnd_updated WS events plus
	// optimistic writes from the presence menu.
	Presence   string    // "active" or "away"; "" until first fetch
	DNDEnabled bool      // true if either snooze or admin-DND is active
	DNDEndTS   time.Time // unified end timestamp; zero if not in DND
}

func main() {
	// Debug log to file when SLK_DEBUG is set; otherwise discard so
	// log lines don't bleed into the user's terminal under altscreen
	// (some terminals show stderr writes overlaid on the rendered UI;
	// even if they don't, stderr can show up after slk exits and
	// pollute the parent shell).
	if os.Getenv("SLK_DEBUG") != "" {
		f, err := os.OpenFile("/tmp/slk-debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			log.SetOutput(f)
			log.Printf("=== slk debug session started ===")
		}
	} else {
		log.SetOutput(io.Discard)
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
		case "--list-workspaces":
			if err := listWorkspaces(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	// Emoji width probing: parse flags and call Init before bubbletea starts.
	skipProbe := false
	forceProbe := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--no-emoji-probe":
			skipProbe = true
		case "--probe-emoji":
			forceProbe = true
		}
	}

	probedNow := false
	probeStart := time.Now()
	probeOpts := emojiwidth.InitOptions{
		SkipProbe:  skipProbe,
		ForceProbe: forceProbe,
	}
	if emojiwidth.WillProbe(probeOpts) {
		fmt.Fprintln(os.Stderr, "Calibrating emoji widths for your terminal (one-time, ~1 second)...")
		probedNow = true
	}

	if err := emojiwidth.Init(probeOpts); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: emoji width calibration failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "Falling back to library defaults; some emoji may render with incorrect width.")
	}

	if probedNow && emojiwidth.IsCalibrated() {
		cachePath := emojiwidth.CachePath(emojiwidth.IdentifyTerminal())
		fmt.Fprintf(os.Stderr, "Done in %dms. Cached to %s\n", time.Since(probeStart).Milliseconds(), cachePath)
	}

	if forceProbe {
		// --probe-emoji is a diagnostic flag: probe and exit.
		fmt.Fprintln(os.Stderr, "Probe complete. Exiting.")
		os.Exit(0)
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
  slk --add-workspace    Add a Slack workspace (interactive)
  slk --list-workspaces  List configured workspaces (TeamID, Slug, Name)
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

	notifier := notify.New(cfg.Notifications.Enabled)

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

	// Initialize services
	wsMgr := service.NewWorkspaceManager(db)
	msgSvc := service.NewMessageService(db)
	_ = msgSvc // will wire for send/receive

	// Create app
	app := ui.NewApp()
	app.SetClipboardAvailable(clipboardOK)
	if useWaylandClipboard {
		app.SetClipboardReader(ui.WaylandClipboardReader())
	}

	// Connect to workspaces
	ctx := context.Background()
	tsFormat := cfg.Appearance.TimestampFormat

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
	imageHTTPClient := &http.Client{Timeout: 10 * time.Second}
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

	// Optional: run kitty version probe if detected as kitty AND stdin is a TTY.
	// Must happen BEFORE bubbletea takes over the terminal.
	if proto == imgpkg.ProtoKitty && term.IsTerminal(int(os.Stdin.Fd())) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			log.Printf("kitty probe skipped: cannot enter raw mode: %v", err)
		} else {
			ok := imgpkg.ProbeKittyGraphics(os.Stdout, os.Stdin, 200*time.Millisecond)
			if rerr := term.Restore(int(os.Stdin.Fd()), state); rerr != nil {
				log.Printf("term restore after kitty probe: %v", rerr)
			}
			if !ok {
				log.Println("kitty probe failed, downgrading to halfblock")
				proto = imgpkg.ProtoHalfBlock
			}
		}
	}
	log.Printf("image protocol: %s", proto)

	// Avatars use kitty graphics when available (sharper). Sixel and
	// half-block terminals fall back to half-block — re-emitting sixel
	// per visible avatar per redraw would dominate the bandwidth budget.
	avatarCache := avatar.NewCache(imageFetcher, imgpkg.KittyRendererInstance(), proto == imgpkg.ProtoKitty)

	// Cell pixel metrics for sizing decisions.
	pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
	log.Printf("cell pixels: %dx%d", pxW, pxH)

	// Wire the inline-image pipeline into the messages pane. SendMsg
	// stays nil here because tea.NewProgram has not run yet; we re-call
	// SetImageContext after `p` is constructed to populate it (see
	// below). Both calls share buildImgCtx so the only difference is
	// the SendMsg callback.
	buildImgCtx := func(send func(tea.Msg)) messages.ImageContext {
		return messages.ImageContext{
			Protocol:    proto,
			Fetcher:     imageFetcher,
			KittyRender: imgpkg.KittyRendererInstance(),
			CellPixels:  image.Pt(pxW, pxH),
			MaxRows:     cfg.Appearance.MaxImageRows,
			MaxCols:     cfg.Appearance.MaxImageCols,
			SendMsg:     send,
		}
	}
	app.SetImageContext(buildImgCtx(nil))
	app.SetImageFetcher(imageFetcher)
	app.SetImageProtocol(proto)

	// Build workspace rail items for all tokens
	var wsItems []workspace.WorkspaceItem
	for _, token := range tokens {
		wsItems = append(wsItems, workspace.WorkspaceItem{
			ID:       token.TeamID,
			Name:     token.TeamName,
			Initials: workspace.WorkspaceInitials(token.TeamName),
		})
	}

	// Set up loading overlay with workspace names
	var wsNames []string
	for _, t := range tokens {
		wsNames = append(wsNames, t.TeamName)
	}
	app.SetLoadingWorkspaces(wsNames)
	app.SetWorkspaces(wsItems)
	app.SetTypingEnabled(cfg.Animations.TypingIndicators)
	app.SetSidebarStaleThreshold(time.Duration(cfg.Sidebar.HideInactiveAfterDays) * 24 * time.Hour)

	// Wire theme switcher
	app.SetThemeItems(styles.ThemeNames())
	app.SetThemeOverrides(cfg.Theme)

	// Wire avatar rendering
	app.SetAvatarFunc(func(userID string) string {
		return avatarCache.Get(userID)
	})

	// Wire up frecent emoji functions (not workspace-specific)
	app.SetFrecentFuncs(
		func(limit int) []reactionpicker.EmojiEntry {
			names, err := db.GetFrecentEmoji(limit)
			if err != nil {
				return nil
			}
			codeMap := emoji.CodeMap()
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
		func(emojiName string) {
			_ = db.RecordEmojiUse(emojiName)
		},
	)

	// Declare p before wiring callbacks so closures can capture it
	var p *tea.Program
	workspaces := make(map[string]*WorkspaceContext)
	var activeTeamID string

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

	// wireCallbacks sets all App callbacks to use the given workspace context.
	// Called on initial setup and again when the user switches workspaces.
	wireCallbacks := func(wctx *WorkspaceContext) {
		client := wctx.Client
		userNames := wctx.UserNames
		lastReadMap := wctx.LastReadMap

		app.SetChannelLastReadFetcher(func(channelID string) string {
			return lastReadMap[channelID]
		})

		app.SetChannelFetcher(func(channelID, channelName string) tea.Msg {
			msgItems := fetchChannelMessages(client, channelID, db, userNames, tsFormat, avatarCache)

			lastReadTS := lastReadMap[channelID]

			// Mark channel as read up to the latest message
			if len(msgItems) > 0 {
				latestTS := msgItems[len(msgItems)-1].TS
				go func() {
					_ = client.MarkChannel(ctx, channelID, latestTS)
					_ = db.UpdateLastReadTS(channelID, latestTS)
					lastReadMap[channelID] = latestTS
					if p != nil {
						p.Send(ui.ChannelMarkedReadMsg{ChannelID: channelID})
					}
				}()
			}

			return ui.MessagesLoadedMsg{
				ChannelID:  channelID,
				Messages:   msgItems,
				LastReadTS: lastReadTS,
			}
		})

		app.SetMessageSender(func(channelID, text string) tea.Msg {
			ctx := context.Background()
			ts, err := client.SendMessage(ctx, channelID, text)
			if err != nil {
				log.Printf("Warning: failed to send message: %v", err)
				return nil
			}
			userName := "you"
			if resolved, ok := userNames[client.UserID()]; ok {
				userName = resolved
			}
			return ui.MessageSentMsg{
				ChannelID: channelID,
				Message: messages.MessageItem{
					TS:        ts,
					UserID:    client.UserID(),
					UserName:  userName,
					Text:      text,
					Timestamp: formatTimestamp(ts, tsFormat),
				},
			}
		})

		app.SetMessageEditor(func(channelID, ts, text string) tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := client.EditMessage(ctx, channelID, ts, text)
			if err != nil {
				log.Printf("Warning: failed to edit message %s/%s: %v", channelID, ts, err)
			}
			return ui.MessageEditedMsg{ChannelID: channelID, TS: ts, Err: err}
		})

		app.SetMessageDeleter(func(channelID, ts string) tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := client.RemoveMessage(ctx, channelID, ts)
			if err != nil {
				log.Printf("Warning: failed to delete message %s/%s: %v", channelID, ts, err)
			}
			return ui.MessageDeletedMsg{ChannelID: channelID, TS: ts, Err: err}
		})

		app.SetUploader(func(channelID, threadTS, caption string, attachments []compose.PendingAttachment) tea.Cmd {
			return func() tea.Msg {
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

		app.SetOlderMessagesFetcher(func(channelID, oldestTS string) tea.Msg {
			msgItems := fetchOlderMessages(client, channelID, oldestTS, db, userNames, tsFormat, avatarCache)
			return ui.OlderMessagesLoadedMsg{
				ChannelID: channelID,
				Messages:  msgItems,
			}
		})

		app.SetThreadFetcher(func(channelID, threadTS string) tea.Msg {
			replies := fetchThreadReplies(client, channelID, threadTS, db, userNames, tsFormat, avatarCache)
			return ui.ThreadRepliesLoadedMsg{
				ThreadTS: threadTS,
				Replies:  replies,
			}
		})

		app.SetThreadsListFetcher(func(teamID string) tea.Msg {
			summaries, err := db.ListInvolvedThreads(teamID, client.UserID())
			if err != nil {
				log.Printf("Warning: ListInvolvedThreads(%s): %v", teamID, err)
				return ui.ThreadsListLoadedMsg{TeamID: teamID, Summaries: nil}
			}
			return ui.ThreadsListLoadedMsg{TeamID: teamID, Summaries: summaries}
		})

		app.SetThreadReplySender(func(channelID, threadTS, text string) tea.Msg {
			ctx := context.Background()
			ts, err := client.SendReply(ctx, channelID, threadTS, text)
			if err != nil {
				log.Printf("Warning: failed to send thread reply: %v", err)
				return nil
			}
			userName := "you"
			if resolved, ok := userNames[client.UserID()]; ok {
				userName = resolved
			}
			return ui.ThreadReplySentMsg{
				ChannelID: channelID,
				ThreadTS:  threadTS,
				Message: messages.MessageItem{
					TS:        ts,
					UserID:    client.UserID(),
					UserName:  userName,
					Text:      text,
					Timestamp: formatTimestamp(ts, tsFormat),
					ThreadTS:  threadTS,
				},
			}
		})

		app.SetReactionSender(
			func(channelID, messageTS, emojiName string) error {
				return client.AddReaction(ctx, channelID, messageTS, emojiName)
			},
			func(channelID, messageTS, emojiName string) error {
				return client.RemoveReaction(ctx, channelID, messageTS, emojiName)
			},
		)

		app.SetPermalinkFetcher(func(ctx context.Context, channelID, ts string) (string, error) {
			return client.GetPermalink(ctx, channelID, ts)
		})

		app.SetCurrentUserID(client.UserID())

		app.SetTypingSender(func(channelID string) {
			_ = client.SendTyping(channelID)
		})

		app.SetChannelJoiner(func(channelID, channelName string) tea.Msg {
			ctx := context.Background()
			if err := client.JoinChannel(ctx, channelID); err != nil {
				return ui.ChannelJoinFailedMsg{ID: channelID, Name: channelName, Err: err}
			}
			return ui.ChannelJoinedMsg{ID: channelID, Name: channelName}
		})
	}

	// Wire workspace switcher
	app.SetWorkspaceSwitcher(func(teamID string) tea.Msg {
		wctx, ok := workspaces[teamID]
		if !ok {
			return nil
		}

		// Update active pointer
		activeTeamID = teamID

		// Re-wire all callbacks to the new workspace's client
		wireCallbacks(wctx)

		return ui.WorkspaceSwitchedMsg{
			TeamID:      wctx.TeamID,
			TeamName:    wctx.TeamName,
			Theme:       cfg.ResolveTheme(teamID),
			Channels:    wctx.Channels,
			FinderItems: wctx.FinderItems,
			UserNames:   wctx.UserNames,
			UserID:      wctx.UserID,
			CustomEmoji: wctx.CustomEmoji,
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

	// Start the TUI immediately (shows loading overlay)
	p = tea.NewProgram(app)

	// Now that `p` exists, re-install the ImageContext with a real
	// SendMsg callback so the prefetcher can dispatch ImageReadyMsg
	// back into the program loop. This must happen before any
	// rendering kicks off prefetches whose completions would otherwise
	// be dropped on the floor.
	app.SetImageContext(buildImgCtx(p.Send))

	// Launch workspace connections in background goroutines
	// Results are sent to the TUI via p.Send()
	for _, token := range tokens {
		go func(tok slackclient.Token) {
			wctx, err := connectWorkspace(ctx, tok, db, cfg, avatarCache)
			if err != nil {
				p.Send(ui.WorkspaceFailedMsg{TeamName: tok.TeamName})
				return
			}

			workspaces[wctx.TeamID] = wctx
			wsMgr.AddWorkspace(wctx.TeamID, wctx.TeamName, "")

			// Decide whether this workspace becomes the active one.
			// If default_workspace resolved to a team ID, only that
			// workspace claims active. Otherwise the first to connect
			// claims it.
			claimActive := false
			if defaultTeamID != "" {
				claimActive = wctx.TeamID == defaultTeamID
			} else {
				claimActive = activeTeamID == ""
			}
			if claimActive {
				activeTeamID = wctx.TeamID
				wireCallbacks(wctx)
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
				wsCtx:           wctx,
			}
			wctx.RTMHandler = handler
			wctx.ConnMgr = slackclient.NewConnectionManager(wctx.Client, handler)
			go wctx.ConnMgr.Run(ctx)

			p.Send(ui.WorkspaceReadyMsg{
				TeamID:      wctx.TeamID,
				TeamName:    wctx.TeamName,
				Theme:       cfg.ResolveTheme(wctx.TeamID),
				Channels:    wctx.Channels,
				FinderItems: wctx.FinderItems,
				UserNames:   wctx.UserNames,
				UserID:      wctx.UserID,
				CustomEmoji: wctx.CustomEmoji, // empty at this point; filled by the goroutine below
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
		}(token)
	}

	_, err = p.Run()

	// Clean up connection managers
	for _, wctx := range workspaces {
		if wctx.ConnMgr != nil {
			wctx.ConnMgr.Stop()
		}
	}

	return err
}

func connectWorkspace(ctx context.Context, token slackclient.Token, db *cache.DB, cfg config.Config, avatarCache *avatar.Cache) (*WorkspaceContext, error) {
	client := slackclient.NewClient(token.AccessToken, token.Cookie)
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connecting %s: %w", token.TeamName, err)
	}

	wctx := &WorkspaceContext{
		Client:      client,
		TeamID:      client.TeamID(),
		TeamName:    token.TeamName,
		UserID:      client.UserID(),
		UserNames:         make(map[string]string),
		UserNamesByHandle: make(map[string]string),
		BotUserIDs:        make(map[string]bool),
		LastReadMap:       make(map[string]string),
		CustomEmoji: make(map[string]string),
	}

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
			db.UpsertUser(cache.User{
				ID:          u.ID,
				WorkspaceID: client.TeamID(),
				Name:        u.Name,
				DisplayName: name,
				AvatarURL:   u.Profile.Image32,
				Presence:    "away",
				IsBot:       isBot,
			})
			avatarCache.Preload(u.ID, u.Profile.Image32)
		}
	}()

	// Fetch channels
	channels, err := client.GetChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching channels for %s: %w", token.TeamName, err)
	}

	for _, ch := range channels {
		chType := "channel"
		if ch.IsIM {
			// Slack returns the same `is_im=true` for human DMs and app
			// DMs; the only differentiator is the peer user's IsBot/
			// IsAppUser flag, which we look up via the cache-seeded
			// BotUserIDs set. Unknown peers default to "dm" and are
			// reclassified later by the resolveUser path below.
			if wctx.BotUserIDs[ch.User] {
				chType = "app"
			} else {
				chType = "dm"
			}
		} else if ch.IsMpIM {
			chType = "group_dm"
		} else if ch.IsPrivate {
			chType = "private"
		}

		db.UpsertChannel(cache.Channel{
			ID:          ch.ID,
			WorkspaceID: client.TeamID(),
			Name:        ch.Name,
			Type:        chType,
			Topic:       ch.Topic.Value,
			IsMember:    ch.IsMember,
		})

		displayName := ch.Name
		if ch.IsIM {
			if resolved, ok := wctx.UserNames[ch.User]; ok {
				displayName = resolved
			} else {
				displayName = ch.User
				wctx.UnresolvedDMs = append(wctx.UnresolvedDMs, UnresolvedDM{
					ChannelID: ch.ID,
					UserID:    ch.User,
				})
			}
		} else if ch.IsMpIM {
			displayName = slackfmt.FormatMPDMName(ch.Name, func(h string) string {
				return wctx.UserNamesByHandle[h]
			})
		}

		section := cfg.MatchSection(client.TeamID(), ch.Name)
		var sectionOrder int
		if section != "" {
			sectionOrder = cfg.SectionOrder(client.TeamID(), section)
		}
		item := sidebar.ChannelItem{
			ID:           ch.ID,
			Name:         displayName,
			Type:         chType,
			Section:      section,
			SectionOrder: sectionOrder,
		}
		if ch.IsIM {
			item.DMUserID = ch.User
			if cachedUser, err := db.GetUser(ch.User); err == nil && cachedUser.Presence != "" {
				item.Presence = cachedUser.Presence
			}
		}
		wctx.Channels = append(wctx.Channels, item)
	}

	// Fetch unread counts
	unreadCounts, _ := client.GetUnreadCounts()
	unreadMap := make(map[string]int)
	for _, u := range unreadCounts {
		if u.HasUnread {
			unreadMap[u.ChannelID] = u.Count
		}
		if u.LastRead != "" {
			wctx.LastReadMap[u.ChannelID] = u.LastRead
			_ = db.UpdateLastReadTS(u.ChannelID, u.LastRead)
		}
	}
	for i := range wctx.Channels {
		if count, ok := unreadMap[wctx.Channels[i].ID]; ok {
			wctx.Channels[i].UnreadCount = count
		}
		if lr, ok := wctx.LastReadMap[wctx.Channels[i].ID]; ok {
			wctx.Channels[i].LastReadTS = lr
		}
	}

	// Build finder items. The user is a member of every channel returned by
	// GetChannels (it's backed by users.conversations), so Joined=true here.
	// A separate background fetch surfaces non-joined public channels for
	// browsing -- see startBrowseableChannelsFetch in main.go.
	for _, ch := range wctx.Channels {
		wctx.FinderItems = append(wctx.FinderItems, channelfinder.Item{
			ID:       ch.ID,
			Name:     ch.Name,
			Type:     ch.Type,
			Presence: ch.Presence,
			Joined:   true,
		})
	}

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
			ID:     ch.ID,
			Name:   ch.Name,
			Type:   "channel",
			Joined: false,
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
				avatarCache.Preload(userID, u.Profile.Image32)
				db.UpsertUser(cache.User{
					ID:          userID,
					WorkspaceID: client.TeamID(),
					Name:        u.Name,
					DisplayName: name,
					AvatarURL:   u.Profile.Image32,
					Presence:    "away",
					IsBot:       isBot,
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
		})
		return name, isBot
	}
	return userID, false
}

func fetchOlderMessages(client *slackclient.Client, channelID, latestTS string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	history, err := client.GetOlderHistory(ctx, channelID, 50, latestTS)
	if err != nil {
		return nil
	}

	var msgItems []messages.MessageItem
	for _, m := range history {
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			CreatedAt:   time.Now().Unix(),
		})

		userName, _ := resolveUser(client, m.User, userNames, db, avatarCache)

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
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:          m.Timestamp,
			UserID:      m.User,
			UserName:    userName,
			Text:        m.Text,
			Timestamp:   formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			Reactions:   reactions,
			Attachments: extractAttachments(m.Files),
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
}

func fetchChannelMessages(client *slackclient.Client, channelID string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	history, err := client.GetHistory(ctx, channelID, 50, "")
	if err != nil {
		return nil
	}

	var msgItems []messages.MessageItem
	for _, m := range history {
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			CreatedAt:   time.Now().Unix(),
		})

		userName, _ := resolveUser(client, m.User, userNames, db, avatarCache)

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
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:          m.Timestamp,
			UserID:      m.User,
			UserName:    userName,
			Text:        m.Text,
			Timestamp:   formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			Reactions:   reactions,
			Attachments: extractAttachments(m.Files),
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
}

func fetchThreadReplies(client *slackclient.Client, channelID, threadTS string, db *cache.DB, userNames map[string]string, tsFormat string, avatarCache *avatar.Cache) []messages.MessageItem {
	ctx := context.Background()
	history, err := client.GetReplies(ctx, channelID, threadTS)
	if err != nil {
		log.Printf("Warning: failed to fetch thread replies: %v", err)
		return nil
	}

	var msgItems []messages.MessageItem
	for _, m := range history {
		db.UpsertMessage(cache.Message{
			TS:          m.Timestamp,
			ChannelID:   channelID,
			WorkspaceID: client.TeamID(),
			UserID:      m.User,
			Text:        m.Text,
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			CreatedAt:   time.Now().Unix(),
		})

		userName, _ := resolveUser(client, m.User, userNames, db, avatarCache)

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
			})
			_ = db.UpsertReaction(m.Timestamp, channelID, r.Name, r.Users, r.Count)
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:          m.Timestamp,
			UserID:      m.User,
			UserName:    userName,
			Text:        m.Text,
			Timestamp:   formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:    m.ThreadTimestamp,
			ReplyCount:  m.ReplyCount,
			Subtype:     m.SubType,
			Reactions:   reactions,
			Attachments: extractAttachments(m.Files),
		})
	}

	// First message from GetConversationReplies is the parent -- skip it for the replies list
	if len(msgItems) > 1 {
		return msgItems[1:]
	}
	return nil
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
// StatusChangeMsg. Also subscribes to presence_change events for the
// self user so external state changes arrive over the WS.
func bootstrapPresenceAndDND(ctx context.Context, wctx *WorkspaceContext, program *tea.Program) {
	if wctx == nil || wctx.Client == nil {
		return
	}

	// Subscribe so future presence_change events for our own user arrive.
	// Failure is non-fatal — manual_presence_change and dnd_updated work
	// without an explicit subscription.
	_ = wctx.Client.SubscribePresence([]string{wctx.UserID})

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

	// Back-reference for self-presence/DND state mutation.
	wsCtx *WorkspaceContext
}

func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS, subtype string, edited bool, files []slack.File) {
	// Cache every message to SQLite, regardless of active workspace
	h.db.UpsertMessage(cache.Message{
		TS:          ts,
		ChannelID:   channelID,
		WorkspaceID: h.workspaceID,
		UserID:      userID,
		Text:        text,
		ThreadTS:    threadTS,
		Subtype:     subtype,
		CreatedAt:   time.Now().Unix(),
	})

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
		}
		chType := h.channelTypes[channelID]
		if notify.ShouldNotify(ctx, channelID, userID, text, chType) {
			senderName := userID
			if resolved, ok := h.userNames[userID]; ok {
				senderName = resolved
			}
			chName := h.channelNames[channelID]
			title := h.workspaceName + ": #" + chName
			if chType == "dm" || chType == "group_dm" {
				title = h.workspaceName + ": " + senderName
			}
			body := senderName + ": " + notify.StripSlackMarkup(text, h.userNames)
			go h.notifier.Notify(title, body)
		}
	}

	if h.isActive != nil && !h.isActive() {
		// Inactive workspace — just notify about unread
		h.program.Send(ui.WorkspaceUnreadMsg{
			TeamID:    h.workspaceID,
			ChannelID: channelID,
		})
		return
	}

	userName := userID
	if resolved, ok := h.userNames[userID]; ok {
		userName = resolved
	}
	h.program.Send(ui.NewMessageMsg{
		ChannelID: channelID,
		Message: messages.MessageItem{
			TS:          ts,
			UserID:      userID,
			UserName:    userName,
			Text:        text,
			Timestamp:   formatTimestamp(ts, h.tsFormat),
			ThreadTS:    threadTS,
			Subtype:     subtype,
			IsEdited:    edited,
			Attachments: extractAttachments(files),
		},
	})
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

	slugByTeamID := make(map[string]string, len(cfg.Workspaces))
	for k, w := range cfg.Workspaces {
		slugByTeamID[w.TeamID] = k
	}

	idW, slugW, nameW := len("TEAM ID"), len("SLUG"), len("NAME")
	for _, t := range tokens {
		if len(t.TeamID) > idW {
			idW = len(t.TeamID)
		}
		if s := slugByTeamID[t.TeamID]; len(s) > slugW {
			slugW = len(s)
		}
		if len(t.TeamName) > nameW {
			nameW = len(t.TeamName)
		}
	}
	fmt.Printf("%-*s  %-*s  %s\n", idW, "TEAM ID", slugW, "SLUG", "NAME")
	fmt.Printf("%s  %s  %s\n",
		strings.Repeat("-", idW),
		strings.Repeat("-", slugW),
		strings.Repeat("-", nameW))
	for _, t := range tokens {
		fmt.Printf("%-*s  %-*s  %s\n", idW, t.TeamID, slugW, slugByTeamID[t.TeamID], t.TeamName)
	}
	return nil
}
