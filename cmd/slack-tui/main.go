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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gammons/slack-tui/internal/avatar"
	"github.com/gammons/slack-tui/internal/cache"
	"github.com/gammons/slack-tui/internal/config"
	"github.com/gammons/slack-tui/internal/service"
	slackclient "github.com/gammons/slack-tui/internal/slack"
	"github.com/gammons/slack-tui/internal/ui"
	"github.com/gammons/slack-tui/internal/ui/channelfinder"
	"github.com/gammons/slack-tui/internal/ui/messages"
	"github.com/gammons/slack-tui/internal/ui/sidebar"
	"github.com/gammons/slack-tui/internal/ui/workspace"
)

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
	var wsItems []workspace.WorkspaceItem

	// Track the active client for channel fetching
	var activeClient *slackclient.Client
	// User ID -> display name lookup
	userNames := make(map[string]string)
	tsFormat := cfg.Appearance.TimestampFormat

	// Initialize avatar cache
	avatarDir := filepath.Join(cacheDir, "avatars")
	avatarCache := avatar.NewCache(avatarDir)

	for _, token := range tokens {
		client := slackclient.NewClient(token.AccessToken, token.Cookie)
		if err := client.Connect(ctx); err != nil {
			log.Printf("Warning: failed to connect workspace %s: %v", token.TeamName, err)
			continue
		}
		activeClient = client

		wsMgr.AddWorkspace(client.TeamID(), token.TeamName, "")
		wsItems = append(wsItems, workspace.WorkspaceItem{
			ID:       client.TeamID(),
			Name:     token.TeamName,
			Initials: workspace.WorkspaceInitials(token.TeamName),
		})

		// Fetch users first to resolve display names
		users, err := client.GetUsers(ctx)
		if err != nil {
			log.Printf("Warning: failed to fetch users: %v", err)
		} else {
			for _, u := range users {
				name := u.Profile.DisplayName
				if name == "" {
					name = u.RealName
				}
				if name == "" {
					name = u.Name
				}
				userNames[u.ID] = name
				db.UpsertUser(cache.User{
					ID:          u.ID,
					WorkspaceID: client.TeamID(),
					Name:        u.Name,
					DisplayName: name,
					AvatarURL:   u.Profile.Image32,
					Presence:    "away",
				})
				// Preload avatar in background
				avatarCache.Preload(u.ID, u.Profile.Image32)
			}
		}

		// Fetch channels
		channels, err := client.GetChannels(ctx)
		if err != nil {
			log.Printf("Warning: failed to fetch channels: %v", err)
			continue
		}

		var sidebarItems []sidebar.ChannelItem
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
				if resolved, ok := userNames[ch.User]; ok {
					displayName = resolved
				} else {
					displayName = ch.User
				}
			}

			// Match channel to a config-defined section
			section := cfg.MatchSection(ch.Name)

			sidebarItems = append(sidebarItems, sidebar.ChannelItem{
				ID:      ch.ID,
				Name:    displayName,
				Type:    chType,
				Section: section,
			})
		}

		app.SetChannels(sidebarItems)

		// Populate channel finder with all channels/DMs
		var finderItems []channelfinder.Item
		for _, ch := range sidebarItems {
			finderItems = append(finderItems, channelfinder.Item{
				ID:       ch.ID,
				Name:     ch.Name,
				Type:     ch.Type,
				Presence: ch.Presence,
			})
		}
		app.SetChannelFinderItems(finderItems)

		// Load initial messages for first channel
		if len(sidebarItems) > 0 {
			firstCh := sidebarItems[0]
			msgItems := fetchChannelMessages(client, firstCh.ID, db, userNames, tsFormat)
			if len(msgItems) > 0 {
				app.SetInitialChannel(firstCh.ID, firstCh.Name, msgItems)
			}
		}
	}

	app.SetWorkspaces(wsItems)
	app.SetUserNames(userNames)

	// Wire avatar rendering
	app.SetAvatarFunc(func(userID string) string {
		return avatarCache.Get(userID)
	})

	// Wire up the channel fetcher so switching channels loads messages
	if activeClient != nil {
		client := activeClient
		app.SetChannelFetcher(func(channelID, channelName string) tea.Msg {
			msgItems := fetchChannelMessages(client, channelID, db, userNames, tsFormat)
			return ui.MessagesLoadedMsg{
				ChannelID: channelID,
				Messages:  msgItems,
			}
		})

		// Wire up message sending
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

		// Wire up older messages fetcher for infinite scroll
		app.SetOlderMessagesFetcher(func(channelID, oldestTS string) tea.Msg {
			msgItems := fetchOlderMessages(client, channelID, oldestTS, db, userNames, tsFormat)
			return ui.OlderMessagesLoadedMsg{
				ChannelID: channelID,
				Messages:  msgItems,
			}
		})
	}

	// Run the TUI
	p := tea.NewProgram(app, tea.WithAltScreen())

	// Start WebSocket for real-time events
	if activeClient != nil {
		handler := &rtmEventHandler{
			program:     p,
			userNames:   userNames,
			tsFormat:    tsFormat,
			db:          db,
			workspaceID: activeClient.TeamID(),
		}
		if err := activeClient.StartWebSocket(handler); err != nil {
			log.Printf("Warning: failed to start WebSocket: %v", err)
		} else {
			defer activeClient.StopWebSocket()
		}
	}

	_, err = p.Run()
	return err
}

func fetchOlderMessages(client *slackclient.Client, channelID, latestTS string, db *cache.DB, userNames map[string]string, tsFormat string) []messages.MessageItem {
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

		userName := m.User
		if resolved, ok := userNames[m.User]; ok {
			userName = resolved
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:         m.Timestamp,
			UserID:     m.User,
			UserName:   userName,
			Text:       m.Text,
			Timestamp:  formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:   m.ThreadTimestamp,
			ReplyCount: m.ReplyCount,
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
}

func fetchChannelMessages(client *slackclient.Client, channelID string, db *cache.DB, userNames map[string]string, tsFormat string) []messages.MessageItem {
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

		userName := m.User
		if resolved, ok := userNames[m.User]; ok {
			userName = resolved
		}

		msgItems = append(msgItems, messages.MessageItem{
			TS:         m.Timestamp,
			UserID:     m.User,
			UserName:   userName,
			Text:       m.Text,
			Timestamp:  formatTimestamp(m.Timestamp, tsFormat),
			ThreadTS:   m.ThreadTimestamp,
			ReplyCount: m.ReplyCount,
		})
	}

	// Reverse: Slack returns newest first
	for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
		msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
	}

	return msgItems
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
		return filepath.Join(dir, "slack-tui")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "slack-tui")
}

func xdgData() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "slack-tui")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "slack-tui")
}

func xdgCache() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "slack-tui")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "slack-tui")
}

// rtmEventHandler bridges WebSocket events into bubbletea messages via p.Send()
// and caches all incoming messages to the SQLite database.
type rtmEventHandler struct {
	program     *tea.Program
	userNames   map[string]string
	tsFormat    string
	db          *cache.DB
	workspaceID string
}

func (h *rtmEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	// Cache every message to SQLite, regardless of active channel
	h.db.UpsertMessage(cache.Message{
		TS:          ts,
		ChannelID:   channelID,
		WorkspaceID: h.workspaceID,
		UserID:      userID,
		Text:        text,
		ThreadTS:    threadTS,
		CreatedAt:   time.Now().Unix(),
	})

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

func (h *rtmEventHandler) OnReactionAdded(channelID, ts, userID, emoji string) {
	// TODO: implement reaction updates in UI
}

func (h *rtmEventHandler) OnReactionRemoved(channelID, ts, userID, emoji string) {
	// TODO: implement reaction updates in UI
}

func (h *rtmEventHandler) OnPresenceChange(userID, presence string) {
	// TODO: implement presence indicators in UI
}

func (h *rtmEventHandler) OnUserTyping(channelID, userID string) {
	// TODO: implement typing indicators in UI
}
