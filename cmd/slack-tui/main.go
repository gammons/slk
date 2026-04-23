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
	"github.com/gammons/slack-tui/internal/cache"
	"github.com/gammons/slack-tui/internal/config"
	"github.com/gammons/slack-tui/internal/service"
	slackclient "github.com/gammons/slack-tui/internal/slack"
	"github.com/gammons/slack-tui/internal/ui"
	"github.com/gammons/slack-tui/internal/ui/messages"
	"github.com/gammons/slack-tui/internal/ui/sidebar"
	"github.com/gammons/slack-tui/internal/ui/workspace"
)

func main() {
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
		return fmt.Errorf("no workspaces configured. Run with --add-workspace to authenticate")
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

	for _, token := range tokens {
		client := slackclient.NewClient(token.AccessToken, "")
		if err := client.Connect(ctx); err != nil {
			log.Printf("Warning: failed to connect workspace %s: %v", token.TeamName, err)
			continue
		}

		wsMgr.AddWorkspace(client.TeamID(), token.TeamName, "")
		wsItems = append(wsItems, workspace.WorkspaceItem{
			ID:       client.TeamID(),
			Name:     token.TeamName,
			Initials: workspace.WorkspaceInitials(token.TeamName),
		})

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
				displayName = ch.User // will be user ID, resolve later
			}

			sidebarItems = append(sidebarItems, sidebar.ChannelItem{
				ID:   ch.ID,
				Name: displayName,
				Type: chType,
			})
		}

		app.SetChannels(sidebarItems)

		// Load initial messages for first channel
		if len(sidebarItems) > 0 {
			firstCh := sidebarItems[0]
			history, err := client.GetHistory(ctx, firstCh.ID, 50, "")
			if err == nil {
				var msgItems []messages.MessageItem
				for _, m := range history {
					db.UpsertMessage(cache.Message{
						TS:          m.Timestamp,
						ChannelID:   firstCh.ID,
						WorkspaceID: client.TeamID(),
						UserID:      m.User,
						Text:        m.Text,
						ThreadTS:    m.ThreadTimestamp,
						ReplyCount:  m.ReplyCount,
						CreatedAt:   time.Now().Unix(),
					})

					msgItems = append(msgItems, messages.MessageItem{
						TS:         m.Timestamp,
						UserName:   m.User, // will resolve to display name later
						Text:       m.Text,
						Timestamp:  formatTimestamp(m.Timestamp, cfg.Appearance.TimestampFormat),
						ThreadTS:   m.ThreadTimestamp,
						ReplyCount: m.ReplyCount,
					})
				}
				// Reverse: Slack returns newest first
				for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
					msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
				}
				app.SetInitialChannel(firstCh.ID, firstCh.Name, msgItems)
			}
		}
	}

	app.SetWorkspaces(wsItems)

	// Run the TUI
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
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
