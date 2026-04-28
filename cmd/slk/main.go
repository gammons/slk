package main

import (
	"context"
	"fmt"
	"log"
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
	"github.com/gammons/slk/internal/notify"
	"github.com/gammons/slk/internal/service"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/channelfinder"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/reactionpicker"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/gammons/slk/internal/ui/statusbar"
	"github.com/gammons/slk/internal/ui/styles"
	"github.com/gammons/slk/internal/ui/workspace"
	emoji "github.com/kyokomi/emoji/v2"
	"github.com/slack-go/slack"
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
	LastReadMap map[string]string
	Channels    []sidebar.ChannelItem
	// FinderItems is the merged list shown in the Ctrl+T finder. Initially
	// contains only joined channels; the BrowseableChannelsLoadedMsg pipeline
	// extends it with non-joined public channels in the background.
	FinderItems   []channelfinder.Item
	TeamID        string
	TeamName      string
	UserID        string
	UnresolvedDMs []UnresolvedDM
}

func main() {
	// Handle --add-workspace before anything else
	if len(os.Args) > 1 && os.Args[1] == "--add-workspace" {
		if err := addWorkspace(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
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
	styles.Apply(cfg.Appearance.Theme, cfg.Theme)

	notifier := notify.New(cfg.Notifications.Enabled)

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

	// Connect to workspaces
	ctx := context.Background()
	tsFormat := cfg.Appearance.TimestampFormat

	// Initialize avatar cache
	avatarDir := filepath.Join(cacheDir, "avatars")
	avatarCache := avatar.NewCache(avatarDir)

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

	// Wire theme switcher
	app.SetThemeItems(styles.ThemeNames())
	app.SetThemeOverrides(cfg.Theme)
	app.SetThemeSaver(func(name string) {
		cfg.Appearance.Theme = name
		// Write updated theme to config file
		data, err := os.ReadFile(configPath)
		if err != nil {
			return
		}
		// Simple string replacement for theme field
		lines := strings.Split(string(data), "\n")
		found := false
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "theme") && strings.Contains(trimmed, "=") {
				lines[i] = "theme = \"" + name + "\""
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, "", "[appearance]", "theme = \""+name+"\"")
		}
		os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
	})

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

	// wireCallbacks sets all App callbacks to use the given workspace context.
	// Called on initial setup and again when the user switches workspaces.
	wireCallbacks := func(wctx *WorkspaceContext) {
		client := wctx.Client
		userNames := wctx.UserNames
		lastReadMap := wctx.LastReadMap

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
			Channels:    wctx.Channels,
			FinderItems: wctx.FinderItems,
			UserNames:   wctx.UserNames,
			UserID:      wctx.UserID,
		}
	})

	// Start the TUI immediately (shows loading overlay)
	p = tea.NewProgram(app)

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

			// Wire callbacks for the first workspace that connects
			if activeTeamID == "" {
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
			}
			wctx.RTMHandler = handler
			wctx.ConnMgr = slackclient.NewConnectionManager(wctx.Client, handler)
			go wctx.ConnMgr.Run(ctx)

			p.Send(ui.WorkspaceReadyMsg{
				TeamID:      wctx.TeamID,
				TeamName:    wctx.TeamName,
				Channels:    wctx.Channels,
				FinderItems: wctx.FinderItems,
				UserNames:   wctx.UserNames,
				UserID:      wctx.UserID,
			})

			// Background fetch of all public channels so the finder can show
			// channels the user is not yet a member of. Slow on big workspaces;
			// must not block initial workspace readiness.
			go fetchBrowseableChannels(ctx, wctx, p)

			// Resolve unknown DM user names in background
			if len(wctx.UnresolvedDMs) > 0 {
				go func() {
					for _, dm := range wctx.UnresolvedDMs {
						resolved := resolveUser(wctx.Client, dm.UserID, wctx.UserNames, db, avatarCache)
						if resolved != dm.UserID {
							p.Send(ui.DMNameResolvedMsg{
								ChannelID:   dm.ChannelID,
								DisplayName: resolved,
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
		UserNames:   make(map[string]string),
		LastReadMap: make(map[string]string),
	}

	// Seed user names from cache (fast, local)
	cachedUsers, _ := db.ListUsers(client.TeamID())
	for _, u := range cachedUsers {
		name := u.DisplayName
		if name == "" {
			name = u.Name
		}
		wctx.UserNames[u.ID] = name
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
			db.UpsertUser(cache.User{
				ID:          u.ID,
				WorkspaceID: client.TeamID(),
				Name:        u.Name,
				DisplayName: name,
				AvatarURL:   u.Profile.Image32,
				Presence:    "away",
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
			chType = "dm"
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
		}

		section := cfg.MatchSection(ch.Name)
		item := sidebar.ChannelItem{
			ID:      ch.ID,
			Name:    displayName,
			Type:    chType,
			Section: section,
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
		out = append(out, messages.Attachment{Kind: kind, Name: name, URL: pickAttachmentURL(f, kind)})
	}
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
func resolveUser(client *slackclient.Client, userID string, userNames map[string]string, db *cache.DB, avatarCache *avatar.Cache) string {
	if name, ok := userNames[userID]; ok {
		// Check if avatar is also cached
		if avatarCache.Get(userID) == "" {
			// Have name but no avatar — try to fetch profile for avatar URL
			if u, err := client.GetUserProfile(userID); err == nil {
				avatarCache.Preload(userID, u.Profile.Image32)
				db.UpsertUser(cache.User{
					ID:          userID,
					WorkspaceID: client.TeamID(),
					Name:        u.Name,
					DisplayName: name,
					AvatarURL:   u.Profile.Image32,
					Presence:    "away",
				})
			}
		}
		return name
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
		userNames[userID] = name
		avatarCache.Preload(userID, u.Profile.Image32)
		db.UpsertUser(cache.User{
			ID:          userID,
			WorkspaceID: client.TeamID(),
			Name:        u.Name,
			DisplayName: name,
			AvatarURL:   u.Profile.Image32,
			Presence:    "away",
		})
		return name
	}
	return userID
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
			CreatedAt:   time.Now().Unix(),
		})

		userName := resolveUser(client, m.User, userNames, db, avatarCache)

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
			CreatedAt:   time.Now().Unix(),
		})

		userName := resolveUser(client, m.User, userNames, db, avatarCache)

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
			CreatedAt:   time.Now().Unix(),
		})

		userName := resolveUser(client, m.User, userNames, db, avatarCache)

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
}

func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	// Cache every message to SQLite, regardless of active workspace
	h.db.UpsertMessage(cache.Message{
		TS:          ts,
		ChannelID:   channelID,
		WorkspaceID: h.workspaceID,
		UserID:      userID,
		Text:        text,
		ThreadTS:    threadTS,
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
			body := senderName + ": " + notify.StripSlackMarkup(text)
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
			TS:        ts,
			UserID:    userID,
			UserName:  userName,
			Text:      text,
			Timestamp: formatTimestamp(ts, h.tsFormat),
			ThreadTS:  threadTS,
			IsEdited:  edited,
		},
	})
}

func (h *rtmEventHandler) OnMessageDeleted(channelID, ts string) {
	// TODO: implement message deletion in UI
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
}

func (h *rtmEventHandler) OnDisconnect() {
	h.program.Send(ui.ConnectionStateMsg{State: int(statusbar.StateDisconnected)})
}
