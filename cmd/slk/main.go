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
)

// WorkspaceContext holds all state for a single connected workspace.
type WorkspaceContext struct {
	Client      *slackclient.Client
	ConnMgr     *slackclient.ConnectionManager
	RTMHandler  *rtmEventHandler
	UserNames   map[string]string
	LastReadMap map[string]string
	Channels    []sidebar.ChannelItem
	FinderItems []channelfinder.Item
	TeamID      string
	TeamName    string
	UserID      string
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
	p = tea.NewProgram(app, tea.WithAltScreen())

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
			}
		}

		section := cfg.MatchSection(ch.Name)
		wctx.Channels = append(wctx.Channels, sidebar.ChannelItem{
			ID:      ch.ID,
			Name:    displayName,
			Type:    chType,
			Section: section,
		})
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

	// Build finder items
	for _, ch := range wctx.Channels {
		wctx.FinderItems = append(wctx.FinderItems, channelfinder.Item{
			ID:       ch.ID,
			Name:     ch.Name,
			Type:     ch.Type,
			Presence: ch.Presence,
		})
	}

	return wctx, nil
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
			TS:         m.Timestamp,
			UserID:     m.User,
			UserName:   userName,
			Text:       m.Text,
			Timestamp:  formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:   m.ThreadTimestamp,
			ReplyCount: m.ReplyCount,
			Reactions:  reactions,
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
			TS:         m.Timestamp,
			UserID:     m.User,
			UserName:   userName,
			Text:       m.Text,
			Timestamp:  formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:   m.ThreadTimestamp,
			ReplyCount: m.ReplyCount,
			Reactions:  reactions,
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
			TS:         m.Timestamp,
			UserID:     m.User,
			UserName:   userName,
			Text:       m.Text,
			Timestamp:  formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:   m.ThreadTimestamp,
			ReplyCount: m.ReplyCount,
			Reactions:  reactions,
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
	// TODO: implement presence indicators in UI
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
