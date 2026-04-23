# slack-tui MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working Slack TUI that connects to one workspace via Socket Mode, displays channels and messages, and allows sending messages with vim-style keyboard navigation.

**Architecture:** Service-oriented layers (UI / Service / Client / Data). bubbletea for TUI rendering, lipgloss for styling, SQLite for local cache, slack-go for API communication. Three-panel layout: workspace rail, channel sidebar, message pane.

**Tech Stack:** Go 1.22+, bubbletea, lipgloss, bubbles, slack-go, modernc.org/sqlite, go-toml/v2

**Spec:** `docs/superpowers/specs/2026-04-23-slack-tui-design.md`

---

## File Structure

```
slack-tui/
├── cmd/slack-tui/
│   └── main.go                     # Entry point, wires dependencies
├── internal/
│   ├── config/
│   │   ├── config.go               # Config struct, loading, defaults
│   │   └── config_test.go
│   ├── cache/
│   │   ├── db.go                   # SQLite connection, migrations, schema
│   │   ├── db_test.go
│   │   ├── channels.go             # Channel cache CRUD
│   │   ├── channels_test.go
│   │   ├── messages.go             # Message cache CRUD
│   │   ├── messages_test.go
│   │   ├── users.go                # User cache CRUD
│   │   ├── users_test.go
│   │   ├── workspaces.go           # Workspace cache CRUD
│   │   └── workspaces_test.go
│   ├── slack/
│   │   ├── client.go               # SlackClient wrapper (Web API + Socket Mode)
│   │   ├── client_test.go
│   │   ├── auth.go                 # OAuth flow + token storage
│   │   ├── auth_test.go
│   │   ├── events.go               # Socket Mode event dispatcher
│   │   └── events_test.go
│   ├── service/
│   │   ├── workspace.go            # WorkspaceManager service
│   │   ├── workspace_test.go
│   │   ├── messages.go             # MessageService
│   │   └── messages_test.go
│   └── ui/
│       ├── app.go                  # Root model, layout, focus management
│       ├── app_test.go
│       ├── keys.go                 # Key bindings definition
│       ├── mode.go                 # Vim mode enum + transitions
│       ├── styles/
│       │   └── styles.go           # Lipgloss style definitions
│       ├── workspace/
│       │   ├── model.go            # Workspace rail component
│       │   └── model_test.go
│       ├── sidebar/
│       │   ├── model.go            # Channel sidebar component
│       │   └── model_test.go
│       ├── messages/
│       │   ├── model.go            # Message pane component
│       │   └── model_test.go
│       ├── compose/
│       │   ├── model.go            # Message compose box
│       │   └── model_test.go
│       └── statusbar/
│           ├── model.go            # Status bar component
│           └── model_test.go
├── Makefile
├── go.mod
└── .gitignore
```

---

## Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.gitignore`
- Create: `cmd/slack-tui/main.go`

- [ ] **Step 1: Initialize Go module**

Run: `go mod init github.com/yourusername/slack-tui`

- [ ] **Step 2: Create Makefile**

```makefile
.PHONY: build test lint run clean

BINARY=slack-tui
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/slack-tui

test:
	go test ./... -v -race

lint:
	golangci-lint run ./...

run: build
	./$(BUILD_DIR)/$(BINARY)

clean:
	rm -rf $(BUILD_DIR)
```

- [ ] **Step 3: Create .gitignore**

```
bin/
*.db
*.db-journal
.superpowers/
.env
```

- [ ] **Step 4: Create stub main.go**

```go
// cmd/slack-tui/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("slack-tui starting...")
	os.Exit(0)
}
```

- [ ] **Step 5: Verify it builds**

Run: `make build`
Expected: Binary created at `bin/slack-tui` with no errors.

- [ ] **Step 6: Install core dependencies**

Run:
```bash
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/slack-go/slack@latest
go get modernc.org/sqlite@latest
go get github.com/pelletier/go-toml/v2@latest
go get github.com/sahilm/fuzzy@latest
```

- [ ] **Step 7: Commit**

```bash
git init
git add .
git commit -m "feat: initial project scaffolding"
```

---

## Task 2: Config Layer

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write failing test for config defaults**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.Appearance.Theme != "dark" {
		t.Errorf("expected default theme 'dark', got %q", cfg.Appearance.Theme)
	}
	if cfg.Appearance.TimestampFormat != "3:04 PM" {
		t.Errorf("expected default timestamp format '3:04 PM', got %q", cfg.Appearance.TimestampFormat)
	}
	if !cfg.Animations.Enabled {
		t.Error("expected animations enabled by default")
	}
	if !cfg.Notifications.Enabled {
		t.Error("expected notifications enabled by default")
	}
	if !cfg.Notifications.OnMention {
		t.Error("expected on_mention enabled by default")
	}
	if !cfg.Notifications.OnDM {
		t.Error("expected on_dm enabled by default")
	}
	if cfg.Cache.MessageRetentionDays != 30 {
		t.Errorf("expected 30 day retention, got %d", cfg.Cache.MessageRetentionDays)
	}
	if cfg.Cache.MaxDBSizeMB != 500 {
		t.Errorf("expected 500 MB max, got %d", cfg.Cache.MaxDBSizeMB)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	err := os.WriteFile(configPath, []byte(`
[general]
default_workspace = "myteam"

[appearance]
theme = "light"

[animations]
enabled = false

[cache]
message_retention_days = 7
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.General.DefaultWorkspace != "myteam" {
		t.Errorf("expected workspace 'myteam', got %q", cfg.General.DefaultWorkspace)
	}
	if cfg.Appearance.Theme != "light" {
		t.Errorf("expected theme 'light', got %q", cfg.Appearance.Theme)
	}
	if cfg.Animations.Enabled {
		t.Error("expected animations disabled")
	}
	// Defaults should fill in unset values
	if cfg.Cache.MaxDBSizeMB != 500 {
		t.Errorf("expected default max_db_size_mb 500, got %d", cfg.Cache.MaxDBSizeMB)
	}
	if cfg.Cache.MessageRetentionDays != 7 {
		t.Errorf("expected 7 day retention, got %d", cfg.Cache.MessageRetentionDays)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatal("expected no error for missing file, got:", err)
	}
	// Should return defaults
	if cfg.Appearance.Theme != "dark" {
		t.Errorf("expected default theme 'dark', got %q", cfg.Appearance.Theme)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL -- `config` package doesn't exist yet.

- [ ] **Step 3: Implement config**

```go
// internal/config/config.go
package config

import (
	"errors"
	"os"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	General       General       `toml:"general"`
	Appearance    Appearance    `toml:"appearance"`
	Animations    Animations    `toml:"animations"`
	Notifications Notifications `toml:"notifications"`
	Cache         CacheConfig   `toml:"cache"`
}

type General struct {
	DefaultWorkspace string `toml:"default_workspace"`
}

type Appearance struct {
	Theme           string `toml:"theme"`
	TimestampFormat string `toml:"timestamp_format"`
	ShowAvatars     bool   `toml:"show_avatars"`
}

type Animations struct {
	Enabled          bool `toml:"enabled"`
	SmoothScrolling  bool `toml:"smooth_scrolling"`
	TypingIndicators bool `toml:"typing_indicators"`
	ToastTransitions bool `toml:"toast_transitions"`
	MessageFadeIn    bool `toml:"message_fade_in"`
}

type Notifications struct {
	Enabled    bool     `toml:"enabled"`
	OnMention  bool     `toml:"on_mention"`
	OnDM       bool     `toml:"on_dm"`
	OnKeyword  []string `toml:"on_keyword"`
	QuietHours string   `toml:"quiet_hours"`
}

type CacheConfig struct {
	MessageRetentionDays int `toml:"message_retention_days"`
	MaxDBSizeMB          int `toml:"max_db_size_mb"`
}

func Default() Config {
	return Config{
		Appearance: Appearance{
			Theme:           "dark",
			TimestampFormat: "3:04 PM",
		},
		Animations: Animations{
			Enabled:          true,
			SmoothScrolling:  true,
			TypingIndicators: true,
			ToastTransitions: true,
			MessageFadeIn:    true,
		},
		Notifications: Notifications{
			Enabled:   true,
			OnMention: true,
			OnDM:      true,
		},
		Cache: CacheConfig{
			MessageRetentionDays: 30,
			MaxDBSizeMB:          500,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add TOML config layer with defaults"
```

---

## Task 3: SQLite Cache - Database Setup

**Files:**
- Create: `internal/cache/db.go`
- Create: `internal/cache/db_test.go`

- [ ] **Step 1: Write failing test for database creation and migrations**

```go
// internal/cache/db_test.go
package cache

import (
	"testing"
)

func TestNewDB(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("failed to create db:", err)
	}
	defer db.Close()

	// Verify tables exist by querying them
	tables := []string{"workspaces", "users", "channels", "messages", "reactions", "files"}
	for _, table := range tables {
		var count int
		err := db.conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}
}

func TestNewDBCreatesIndexes(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal("failed to create db:", err)
	}
	defer db.Close()

	// Check that key indexes exist
	var count int
	err = db.conn.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='index' AND name='idx_messages_channel'
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("expected idx_messages_channel index to exist")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -v`
Expected: FAIL -- `cache` package doesn't exist yet.

- [ ] **Step 3: Implement database setup**

```go
// internal/cache/db.go
package cache

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func New(dsn string) (*DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS workspaces (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		domain TEXT NOT NULL DEFAULT '',
		icon_url TEXT NOT NULL DEFAULT '',
		last_synced_at INTEGER NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		presence TEXT NOT NULL DEFAULT 'away',
		updated_at INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
	);

	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL,
		type TEXT NOT NULL DEFAULT 'channel',
		topic TEXT NOT NULL DEFAULT '',
		is_member INTEGER NOT NULL DEFAULT 0,
		is_starred INTEGER NOT NULL DEFAULT 0,
		last_read_ts TEXT NOT NULL DEFAULT '',
		unread_count INTEGER NOT NULL DEFAULT 0,
		updated_at INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (workspace_id) REFERENCES workspaces(id)
	);

	CREATE TABLE IF NOT EXISTS messages (
		ts TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		workspace_id TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		text TEXT NOT NULL DEFAULT '',
		thread_ts TEXT NOT NULL DEFAULT '',
		reply_count INTEGER NOT NULL DEFAULT 0,
		edited_at TEXT NOT NULL DEFAULT '',
		is_deleted INTEGER NOT NULL DEFAULT 0,
		raw_json TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (ts, channel_id)
	);

	CREATE TABLE IF NOT EXISTS reactions (
		message_ts TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		emoji TEXT NOT NULL,
		user_ids TEXT NOT NULL DEFAULT '[]',
		count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (message_ts, channel_id, emoji)
	);

	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		message_ts TEXT NOT NULL DEFAULT '',
		channel_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		mimetype TEXT NOT NULL DEFAULT '',
		size INTEGER NOT NULL DEFAULT 0,
		url_private TEXT NOT NULL DEFAULT '',
		local_path TEXT NOT NULL DEFAULT '',
		thumbnail_path TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(channel_id, ts);
	CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_ts, channel_id);
	CREATE INDEX IF NOT EXISTS idx_channels_workspace ON channels(workspace_id);
	CREATE INDEX IF NOT EXISTS idx_users_workspace ON users(workspace_id);
	`

	_, err := db.conn.Exec(schema)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/ -v`
Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cache/
git commit -m "feat: add SQLite cache layer with schema migrations"
```

---

## Task 4: Cache CRUD Operations

**Files:**
- Create: `internal/cache/workspaces.go`
- Create: `internal/cache/workspaces_test.go`
- Create: `internal/cache/users.go`
- Create: `internal/cache/users_test.go`
- Create: `internal/cache/channels.go`
- Create: `internal/cache/channels_test.go`
- Create: `internal/cache/messages.go`
- Create: `internal/cache/messages_test.go`

- [ ] **Step 1: Write failing tests for workspace CRUD**

```go
// internal/cache/workspaces_test.go
package cache

import (
	"testing"
)

func TestUpsertAndGetWorkspace(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ws := Workspace{
		ID:     "T123",
		Name:   "Acme Corp",
		Domain: "acme",
	}

	if err := db.UpsertWorkspace(ws); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetWorkspace("T123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Acme Corp" {
		t.Errorf("expected name 'Acme Corp', got %q", got.Name)
	}
	if got.Domain != "acme" {
		t.Errorf("expected domain 'acme', got %q", got.Domain)
	}
}

func TestListWorkspaces(t *testing.T) {
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.UpsertWorkspace(Workspace{ID: "T1", Name: "Team 1", Domain: "t1"})
	db.UpsertWorkspace(Workspace{ID: "T2", Name: "Team 2", Domain: "t2"})

	workspaces, err := db.ListWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(workspaces))
	}
}
```

- [ ] **Step 2: Implement workspace types and CRUD**

```go
// internal/cache/workspaces.go
package cache

import "fmt"

type Workspace struct {
	ID           string
	Name         string
	Domain       string
	IconURL      string
	LastSyncedAt int64
}

func (db *DB) UpsertWorkspace(ws Workspace) error {
	_, err := db.conn.Exec(`
		INSERT INTO workspaces (id, name, domain, icon_url, last_synced_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			domain=excluded.domain,
			icon_url=excluded.icon_url,
			last_synced_at=excluded.last_synced_at
	`, ws.ID, ws.Name, ws.Domain, ws.IconURL, ws.LastSyncedAt)
	if err != nil {
		return fmt.Errorf("upserting workspace: %w", err)
	}
	return nil
}

func (db *DB) GetWorkspace(id string) (Workspace, error) {
	var ws Workspace
	err := db.conn.QueryRow(`
		SELECT id, name, domain, icon_url, last_synced_at
		FROM workspaces WHERE id = ?
	`, id).Scan(&ws.ID, &ws.Name, &ws.Domain, &ws.IconURL, &ws.LastSyncedAt)
	if err != nil {
		return ws, fmt.Errorf("getting workspace: %w", err)
	}
	return ws, nil
}

func (db *DB) ListWorkspaces() ([]Workspace, error) {
	rows, err := db.conn.Query(`
		SELECT id, name, domain, icon_url, last_synced_at
		FROM workspaces ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("listing workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Domain, &ws.IconURL, &ws.LastSyncedAt); err != nil {
			return nil, fmt.Errorf("scanning workspace: %w", err)
		}
		workspaces = append(workspaces, ws)
	}
	return workspaces, rows.Err()
}
```

- [ ] **Step 3: Run workspace tests**

Run: `go test ./internal/cache/ -run TestUpsert -v && go test ./internal/cache/ -run TestListWork -v`
Expected: PASS

- [ ] **Step 4: Write failing tests for channel CRUD**

```go
// internal/cache/channels_test.go
package cache

import (
	"testing"
)

func setupDBWithWorkspace(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.UpsertWorkspace(Workspace{ID: "T1", Name: "Test", Domain: "test"})
	return db
}

func TestUpsertAndGetChannel(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	ch := Channel{
		ID:          "C123",
		WorkspaceID: "T1",
		Name:        "general",
		Type:        "channel",
		Topic:       "General discussion",
		IsMember:    true,
	}

	if err := db.UpsertChannel(ch); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetChannel("C123")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "general" {
		t.Errorf("expected 'general', got %q", got.Name)
	}
	if !got.IsMember {
		t.Error("expected is_member true")
	}
}

func TestListChannelsByWorkspace(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	db.UpsertChannel(Channel{ID: "C2", WorkspaceID: "T1", Name: "random", Type: "channel", IsMember: true})
	db.UpsertChannel(Channel{ID: "C3", WorkspaceID: "T1", Name: "archived", Type: "channel", IsMember: false})

	channels, err := db.ListChannels("T1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 {
		t.Errorf("expected 2 member channels, got %d", len(channels))
	}
}

func TestUpdateUnreadCount(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	if err := db.UpdateUnreadCount("C1", 5); err != nil {
		t.Fatal(err)
	}

	ch, _ := db.GetChannel("C1")
	if ch.UnreadCount != 5 {
		t.Errorf("expected unread count 5, got %d", ch.UnreadCount)
	}
}
```

- [ ] **Step 5: Implement channel types and CRUD**

```go
// internal/cache/channels.go
package cache

import "fmt"

type Channel struct {
	ID          string
	WorkspaceID string
	Name        string
	Type        string // channel, dm, group_dm, private
	Topic       string
	IsMember    bool
	IsStarred   bool
	LastReadTS  string
	UnreadCount int
	UpdatedAt   int64
}

func (db *DB) UpsertChannel(ch Channel) error {
	_, err := db.conn.Exec(`
		INSERT INTO channels (id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			type=excluded.type,
			topic=excluded.topic,
			is_member=excluded.is_member,
			is_starred=excluded.is_starred,
			last_read_ts=excluded.last_read_ts,
			unread_count=excluded.unread_count,
			updated_at=excluded.updated_at
	`, ch.ID, ch.WorkspaceID, ch.Name, ch.Type, ch.Topic,
		boolToInt(ch.IsMember), boolToInt(ch.IsStarred),
		ch.LastReadTS, ch.UnreadCount, ch.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting channel: %w", err)
	}
	return nil
}

func (db *DB) GetChannel(id string) (Channel, error) {
	var ch Channel
	var isMember, isStarred int
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at
		FROM channels WHERE id = ?
	`, id).Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Topic,
		&isMember, &isStarred, &ch.LastReadTS, &ch.UnreadCount, &ch.UpdatedAt)
	if err != nil {
		return ch, fmt.Errorf("getting channel: %w", err)
	}
	ch.IsMember = isMember == 1
	ch.IsStarred = isStarred == 1
	return ch, nil
}

func (db *DB) ListChannels(workspaceID string, membersOnly bool) ([]Channel, error) {
	query := `
		SELECT id, workspace_id, name, type, topic, is_member, is_starred, last_read_ts, unread_count, updated_at
		FROM channels WHERE workspace_id = ?`
	args := []any{workspaceID}

	if membersOnly {
		query += " AND is_member = 1"
	}
	query += " ORDER BY name"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		var isMember, isStarred int
		if err := rows.Scan(&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Topic,
			&isMember, &isStarred, &ch.LastReadTS, &ch.UnreadCount, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning channel: %w", err)
		}
		ch.IsMember = isMember == 1
		ch.IsStarred = isStarred == 1
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (db *DB) UpdateUnreadCount(channelID string, count int) error {
	_, err := db.conn.Exec(`UPDATE channels SET unread_count = ? WHERE id = ?`, count, channelID)
	if err != nil {
		return fmt.Errorf("updating unread count: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 6: Run channel tests**

Run: `go test ./internal/cache/ -run TestChannel -v && go test ./internal/cache/ -run TestListChannel -v && go test ./internal/cache/ -run TestUpdate -v`
Expected: PASS

- [ ] **Step 7: Write failing tests for message CRUD**

```go
// internal/cache/messages_test.go
package cache

import (
	"fmt"
	"testing"
)

func TestUpsertAndGetMessages(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	msgs := []Message{
		{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "Hello"},
		{TS: "1700000002.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "World"},
		{TS: "1700000003.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "!"},
	}

	for _, m := range msgs {
		if err := db.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.GetMessages("C1", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 messages, got %d", len(got))
	}
	// Should be ordered by ts ascending
	if got[0].Text != "Hello" {
		t.Errorf("expected first message 'Hello', got %q", got[0].Text)
	}
}

func TestGetMessagesWithCursor(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	for i := 0; i < 5; i++ {
		db.UpsertMessage(Message{
			TS:          fmt.Sprintf("170000000%d.000000", i),
			ChannelID:   "C1",
			WorkspaceID: "T1",
			UserID:      "U1",
			Text:        fmt.Sprintf("msg %d", i),
		})
	}

	// Get only messages before ts 1700000003
	got, err := db.GetMessages("C1", 10, "1700000003.000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 messages before cursor, got %d", len(got))
	}
}

func TestGetThreadReplies(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()
	db.UpsertChannel(Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})

	// Parent message
	db.UpsertMessage(Message{TS: "1700000001.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "parent"})
	// Thread replies
	db.UpsertMessage(Message{TS: "1700000002.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "reply 1", ThreadTS: "1700000001.000000"})
	db.UpsertMessage(Message{TS: "1700000003.000000", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "reply 2", ThreadTS: "1700000001.000000"})

	replies, err := db.GetThreadReplies("C1", "1700000001.000000")
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Errorf("expected 2 replies, got %d", len(replies))
	}
}
```

- [ ] **Step 8: Implement message types and CRUD**

```go
// internal/cache/messages.go
package cache

import "fmt"

type Message struct {
	TS          string
	ChannelID   string
	WorkspaceID string
	UserID      string
	Text        string
	ThreadTS    string
	ReplyCount  int
	EditedAt    string
	IsDeleted   bool
	RawJSON     string
	CreatedAt   int64
}

func (db *DB) UpsertMessage(m Message) error {
	_, err := db.conn.Exec(`
		INSERT INTO messages (ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ts, channel_id) DO UPDATE SET
			user_id=excluded.user_id,
			text=excluded.text,
			thread_ts=excluded.thread_ts,
			reply_count=excluded.reply_count,
			edited_at=excluded.edited_at,
			is_deleted=excluded.is_deleted,
			raw_json=excluded.raw_json
	`, m.TS, m.ChannelID, m.WorkspaceID, m.UserID, m.Text, m.ThreadTS,
		m.ReplyCount, m.EditedAt, boolToInt(m.IsDeleted), m.RawJSON, m.CreatedAt)
	if err != nil {
		return fmt.Errorf("upserting message: %w", err)
	}
	return nil
}

// GetMessages returns messages for a channel, ordered by ts ascending.
// If beforeTS is non-empty, only returns messages with ts < beforeTS (for pagination).
func (db *DB) GetMessages(channelID string, limit int, beforeTS string) ([]Message, error) {
	query := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at
		FROM messages
		WHERE channel_id = ? AND is_deleted = 0 AND thread_ts = ''`
	args := []any{channelID}

	if beforeTS != "" {
		query += " AND ts < ?"
		args = append(args, beforeTS)
	}

	query += " ORDER BY ts ASC LIMIT ?"
	args = append(args, limit)

	return db.queryMessages(query, args...)
}

func (db *DB) GetThreadReplies(channelID, threadTS string) ([]Message, error) {
	query := `
		SELECT ts, channel_id, workspace_id, user_id, text, thread_ts, reply_count, edited_at, is_deleted, raw_json, created_at
		FROM messages
		WHERE channel_id = ? AND thread_ts = ? AND is_deleted = 0
		ORDER BY ts ASC`

	return db.queryMessages(query, channelID, threadTS)
}

func (db *DB) DeleteMessage(channelID, ts string) error {
	_, err := db.conn.Exec(`UPDATE messages SET is_deleted = 1 WHERE channel_id = ? AND ts = ?`, channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

func (db *DB) queryMessages(query string, args ...any) ([]Message, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var isDeleted int
		if err := rows.Scan(&m.TS, &m.ChannelID, &m.WorkspaceID, &m.UserID, &m.Text,
			&m.ThreadTS, &m.ReplyCount, &m.EditedAt, &isDeleted, &m.RawJSON, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		m.IsDeleted = isDeleted == 1
		messages = append(messages, m)
	}
	return messages, rows.Err()
}
```

- [ ] **Step 9: Implement user CRUD**

```go
// internal/cache/users.go
package cache

import "fmt"

type User struct {
	ID          string
	WorkspaceID string
	Name        string
	DisplayName string
	AvatarURL   string
	Presence    string
	UpdatedAt   int64
}

func (db *DB) UpsertUser(u User) error {
	_, err := db.conn.Exec(`
		INSERT INTO users (id, workspace_id, name, display_name, avatar_url, presence, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			display_name=excluded.display_name,
			avatar_url=excluded.avatar_url,
			presence=excluded.presence,
			updated_at=excluded.updated_at
	`, u.ID, u.WorkspaceID, u.Name, u.DisplayName, u.AvatarURL, u.Presence, u.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upserting user: %w", err)
	}
	return nil
}

func (db *DB) GetUser(id string) (User, error) {
	var u User
	err := db.conn.QueryRow(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, updated_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.UpdatedAt)
	if err != nil {
		return u, fmt.Errorf("getting user: %w", err)
	}
	return u, nil
}

func (db *DB) ListUsers(workspaceID string) ([]User, error) {
	rows, err := db.conn.Query(`
		SELECT id, workspace_id, name, display_name, avatar_url, presence, updated_at
		FROM users WHERE workspace_id = ? ORDER BY display_name, name
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.WorkspaceID, &u.Name, &u.DisplayName, &u.AvatarURL, &u.Presence, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (db *DB) UpdatePresence(userID, presence string) error {
	_, err := db.conn.Exec(`UPDATE users SET presence = ? WHERE id = ?`, presence, userID)
	if err != nil {
		return fmt.Errorf("updating presence: %w", err)
	}
	return nil
}
```

- [ ] **Step 10: Write user tests**

```go
// internal/cache/users_test.go
package cache

import (
	"testing"
)

func TestUpsertAndGetUser(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	u := User{
		ID:          "U123",
		WorkspaceID: "T1",
		Name:        "alice",
		DisplayName: "Alice Smith",
		Presence:    "active",
	}

	if err := db.UpsertUser(u); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetUser("U123")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %q", got.DisplayName)
	}
	if got.Presence != "active" {
		t.Errorf("expected 'active', got %q", got.Presence)
	}
}

func TestUpdatePresence(t *testing.T) {
	db := setupDBWithWorkspace(t)
	defer db.Close()

	db.UpsertUser(User{ID: "U1", WorkspaceID: "T1", Name: "alice", Presence: "active"})

	if err := db.UpdatePresence("U1", "away"); err != nil {
		t.Fatal(err)
	}

	got, _ := db.GetUser("U1")
	if got.Presence != "away" {
		t.Errorf("expected 'away', got %q", got.Presence)
	}
}
```

- [ ] **Step 11: Run all cache tests**

Run: `go test ./internal/cache/ -v -race`
Expected: All tests PASS.

- [ ] **Step 12: Commit**

```bash
git add internal/cache/
git commit -m "feat: add cache CRUD for workspaces, channels, messages, users"
```

---

## Task 5: Slack Client - OAuth & Token Management

**Files:**
- Create: `internal/slack/auth.go`
- Create: `internal/slack/auth_test.go`

- [ ] **Step 1: Write failing test for token storage**

```go
// internal/slack/auth_test.go
package slack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadToken(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	token := Token{
		AccessToken:  "xoxp-test-token",
		RefreshToken: "xoxr-refresh-token",
		TeamID:       "T123",
		TeamName:     "Acme",
	}

	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("T123")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "xoxp-test-token" {
		t.Errorf("expected access token 'xoxp-test-token', got %q", got.AccessToken)
	}
	if got.TeamName != "Acme" {
		t.Errorf("expected team name 'Acme', got %q", got.TeamName)
	}
}

func TestLoadTokenNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Error("expected error for missing token")
	}
}

func TestListTokens(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)

	store.Save(Token{AccessToken: "t1", TeamID: "T1", TeamName: "Team 1"})
	store.Save(Token{AccessToken: "t2", TeamID: "T2", TeamName: "Team 2"})

	tokens, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(tokens))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/slack/ -v`
Expected: FAIL -- package doesn't exist.

- [ ] **Step 3: Implement token storage**

Note: For the MVP, tokens are stored as JSON files in the XDG data directory. A future iteration will add encryption at rest.

```go
// internal/slack/auth.go
package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TeamID       string `json:"team_id"`
	TeamName     string `json:"team_name"`
}

type TokenStore struct {
	dir string
}

func NewTokenStore(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

func (s *TokenStore) Save(token Token) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("creating token dir: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}

	path := filepath.Join(s.dir, token.TeamID+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing token: %w", err)
	}
	return nil
}

func (s *TokenStore) Load(teamID string) (Token, error) {
	path := filepath.Join(s.dir, teamID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{}, fmt.Errorf("token not found for team %s", teamID)
		}
		return Token{}, fmt.Errorf("reading token: %w", err)
	}

	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return Token{}, fmt.Errorf("unmarshaling token: %w", err)
	}
	return token, nil
}

func (s *TokenStore) List() ([]Token, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tokens: %w", err)
	}

	var tokens []Token
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		teamID := strings.TrimSuffix(entry.Name(), ".json")
		token, err := s.Load(teamID)
		if err != nil {
			continue // skip corrupted tokens
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func (s *TokenStore) Delete(teamID string) error {
	path := filepath.Join(s.dir, teamID+".json")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("deleting token: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/slack/ -v`
Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/slack/
git commit -m "feat: add token storage for Slack OAuth credentials"
```

---

## Task 6: Slack Client - Socket Mode & Web API

**Files:**
- Create: `internal/slack/client.go`
- Create: `internal/slack/client_test.go`
- Create: `internal/slack/events.go`
- Create: `internal/slack/events_test.go`

- [ ] **Step 1: Write failing test for client creation**

```go
// internal/slack/client_test.go
package slack

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("xoxp-test", "xapp-test")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.TeamID() != "" {
		t.Error("expected empty team ID before connecting")
	}
}
```

- [ ] **Step 2: Implement Slack client wrapper**

The client wraps `slack-go` and provides a simplified interface for the service layer. Socket Mode connection management happens here.

```go
// internal/slack/client.go
package slack

import (
	"context"
	"fmt"
	"log"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
)

// SlackAPI defines the subset of the Slack API we use.
// This interface enables mocking in tests.
type SlackAPI interface {
	GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersContext(ctx context.Context) ([]slack.User, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp, text string) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	AuthTest() (*slack.AuthTestResponse, error)
}

type Client struct {
	api      *slack.Client
	socket   *socketmode.Client
	teamID   string
	userID   string
	appToken string
}

func NewClient(userToken, appToken string) *Client {
	api := slack.New(
		userToken,
		slack.OptionAppLevelToken(appToken),
	)

	socket := socketmode.New(
		api,
		socketmode.OptionLog(log.New(log.Writer(), "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	return &Client{
		api:      api,
		socket:   socket,
		appToken: appToken,
	}
}

func (c *Client) TeamID() string {
	return c.teamID
}

func (c *Client) UserID() string {
	return c.userID
}

func (c *Client) Connect(ctx context.Context) error {
	resp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("auth test failed: %w", err)
	}
	c.teamID = resp.TeamID
	c.userID = resp.UserID
	return nil
}

func (c *Client) RunSocketMode(ctx context.Context, handler EventHandler) error {
	go func() {
		for evt := range c.socket.Events {
			handler.HandleEvent(evt)
		}
	}()
	return c.socket.RunContext(ctx)
}

func (c *Client) GetChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		params := &slack.GetConversationsParameters{
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := c.api.GetConversations(params)
		if err != nil {
			return nil, fmt.Errorf("getting conversations: %w", err)
		}

		allChannels = append(allChannels, channels...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
}

func (c *Client) GetHistory(ctx context.Context, channelID string, limit int, oldest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}
	if oldest != "" {
		params.Oldest = oldest
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting history: %w", err)
	}

	return resp.Messages, nil
}

func (c *Client) GetUsers(ctx context.Context) ([]slack.User, error) {
	users, err := c.api.GetUsersContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting users: %w", err)
	}
	return users, nil
}

func (c *Client) SendMessage(ctx context.Context, channelID, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return "", fmt.Errorf("sending message: %w", err)
	}
	return ts, nil
}

func (c *Client) SendReply(ctx context.Context, channelID, threadTS, text string) (string, error) {
	_, ts, err := c.api.PostMessage(channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	)
	if err != nil {
		return "", fmt.Errorf("sending reply: %w", err)
	}
	return ts, nil
}

func (c *Client) EditMessage(ctx context.Context, channelID, ts, text string) error {
	_, _, _, err := c.api.UpdateMessage(channelID, ts, text)
	if err != nil {
		return fmt.Errorf("editing message: %w", err)
	}
	return nil
}

func (c *Client) RemoveMessage(ctx context.Context, channelID, ts string) error {
	_, _, err := c.api.DeleteMessage(channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

func (c *Client) AddReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.AddReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

func (c *Client) RemoveReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.RemoveReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}
```

- [ ] **Step 3: Implement event handler interface and dispatcher**

```go
// internal/slack/events.go
package slack

import (
	"log"

	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// EventHandler processes Socket Mode events.
type EventHandler interface {
	OnMessage(channelID, userID, ts, text, threadTS string, edited bool)
	OnMessageDeleted(channelID, ts string)
	OnReactionAdded(channelID, ts, userID, emoji string)
	OnReactionRemoved(channelID, ts, userID, emoji string)
	OnPresenceChange(userID, presence string)
	OnUserTyping(channelID, userID string)
}

// EventDispatcher routes socketmode events to the EventHandler.
type EventDispatcher struct {
	handler EventHandler
	client  *socketmode.Client
}

func NewEventDispatcher(client *socketmode.Client, handler EventHandler) *EventDispatcher {
	return &EventDispatcher{
		handler: handler,
		client:  client,
	}
}

func (d *EventDispatcher) HandleEvent(evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		d.client.Ack(*evt.Request)
		d.handleEventsAPI(eventsAPIEvent)

	default:
		// Acknowledge unknown events to prevent retries
		if evt.Request != nil {
			d.client.Ack(*evt.Request)
		}
	}
}

func (d *EventDispatcher) handleEventsAPI(evt slackevents.EventsAPIEvent) {
	switch ev := evt.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		switch ev.SubType {
		case "":
			d.handler.OnMessage(ev.Channel, ev.User, ev.TimeStamp, ev.Text, ev.ThreadTimeStamp, false)
		case "message_changed":
			if ev.Message != nil {
				d.handler.OnMessage(ev.Channel, ev.Message.User, ev.Message.TimeStamp, ev.Message.Text, ev.Message.ThreadTimeStamp, true)
			}
		case "message_deleted":
			d.handler.OnMessageDeleted(ev.Channel, ev.DeletedTimeStamp)
		}

	case *slackevents.ReactionAddedEvent:
		d.handler.OnReactionAdded(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	case *slackevents.ReactionRemovedEvent:
		d.handler.OnReactionRemoved(ev.Item.Channel, ev.Item.Timestamp, ev.User, ev.Reaction)

	default:
		log.Printf("unhandled event type: %T", ev)
	}
}
```

- [ ] **Step 4: Write event dispatcher test**

```go
// internal/slack/events_test.go
package slack

import (
	"testing"
)

type mockEventHandler struct {
	messages        []string
	deletedMessages []string
	reactions       []string
}

func (m *mockEventHandler) OnMessage(channelID, userID, ts, text, threadTS string, edited bool) {
	m.messages = append(m.messages, text)
}

func (m *mockEventHandler) OnMessageDeleted(channelID, ts string) {
	m.deletedMessages = append(m.deletedMessages, ts)
}

func (m *mockEventHandler) OnReactionAdded(channelID, ts, userID, emoji string) {
	m.reactions = append(m.reactions, emoji)
}

func (m *mockEventHandler) OnReactionRemoved(channelID, ts, userID, emoji string) {}
func (m *mockEventHandler) OnPresenceChange(userID, presence string)              {}
func (m *mockEventHandler) OnUserTyping(channelID, userID string)                 {}

func TestEventHandlerInterface(t *testing.T) {
	handler := &mockEventHandler{}

	// Verify the interface is satisfied
	var _ EventHandler = handler

	handler.OnMessage("C1", "U1", "123.456", "hello", "", false)
	if len(handler.messages) != 1 || handler.messages[0] != "hello" {
		t.Error("expected message to be recorded")
	}
}
```

- [ ] **Step 5: Run all slack package tests**

Run: `go test ./internal/slack/ -v`
Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/slack/
git commit -m "feat: add Slack client wrapper with Socket Mode event handling"
```

---

## Task 7: Service Layer

**Files:**
- Create: `internal/service/workspace.go`
- Create: `internal/service/workspace_test.go`
- Create: `internal/service/messages.go`
- Create: `internal/service/messages_test.go`

- [ ] **Step 1: Write failing test for WorkspaceManager**

```go
// internal/service/workspace_test.go
package service

import (
	"testing"

	"github.com/yourusername/slack-tui/internal/cache"
)

func TestWorkspaceManagerAddWorkspace(t *testing.T) {
	db, _ := cache.New(":memory:")
	defer db.Close()

	mgr := NewWorkspaceManager(db)

	mgr.AddWorkspace("T1", "Acme Corp", "acme")

	workspaces := mgr.Workspaces()
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}
	if workspaces[0].Name != "Acme Corp" {
		t.Errorf("expected 'Acme Corp', got %q", workspaces[0].Name)
	}
}

func TestWorkspaceManagerActiveWorkspace(t *testing.T) {
	db, _ := cache.New(":memory:")
	defer db.Close()

	mgr := NewWorkspaceManager(db)
	mgr.AddWorkspace("T1", "Acme", "acme")
	mgr.AddWorkspace("T2", "Beta", "beta")

	if mgr.ActiveWorkspaceID() != "T1" {
		t.Error("expected first workspace to be active")
	}

	mgr.SetActiveWorkspace("T2")
	if mgr.ActiveWorkspaceID() != "T2" {
		t.Error("expected T2 to be active")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement WorkspaceManager**

```go
// internal/service/workspace.go
package service

import (
	"sync"

	"github.com/yourusername/slack-tui/internal/cache"
)

type WorkspaceInfo struct {
	ID     string
	Name   string
	Domain string
}

type WorkspaceManager struct {
	mu         sync.RWMutex
	db         *cache.DB
	workspaces []WorkspaceInfo
	activeIdx  int
}

func NewWorkspaceManager(db *cache.DB) *WorkspaceManager {
	return &WorkspaceManager{
		db:        db,
		activeIdx: 0,
	}
}

func (m *WorkspaceManager) AddWorkspace(id, name, domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.workspaces = append(m.workspaces, WorkspaceInfo{
		ID:     id,
		Name:   name,
		Domain: domain,
	})

	m.db.UpsertWorkspace(cache.Workspace{
		ID:     id,
		Name:   name,
		Domain: domain,
	})
}

func (m *WorkspaceManager) Workspaces() []WorkspaceInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]WorkspaceInfo, len(m.workspaces))
	copy(result, m.workspaces)
	return result
}

func (m *WorkspaceManager) ActiveWorkspaceID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.workspaces) == 0 {
		return ""
	}
	return m.workspaces[m.activeIdx].ID
}

func (m *WorkspaceManager) ActiveWorkspace() (WorkspaceInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.workspaces) == 0 {
		return WorkspaceInfo{}, false
	}
	return m.workspaces[m.activeIdx], true
}

func (m *WorkspaceManager) SetActiveWorkspace(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, ws := range m.workspaces {
		if ws.ID == id {
			m.activeIdx = i
			return
		}
	}
}

func (m *WorkspaceManager) SetActiveByIndex(idx int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx < 0 || idx >= len(m.workspaces) {
		return false
	}
	m.activeIdx = idx
	return true
}
```

- [ ] **Step 4: Run workspace service tests**

Run: `go test ./internal/service/ -v`
Expected: PASS.

- [ ] **Step 5: Write failing test for MessageService**

```go
// internal/service/messages_test.go
package service

import (
	"testing"

	"github.com/yourusername/slack-tui/internal/cache"
)

func setupTestDB(t *testing.T) *cache.DB {
	t.Helper()
	db, err := cache.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.UpsertWorkspace(cache.Workspace{ID: "T1", Name: "Test", Domain: "test"})
	db.UpsertChannel(cache.Channel{ID: "C1", WorkspaceID: "T1", Name: "general", Type: "channel", IsMember: true})
	return db
}

func TestMessageServiceGetCachedMessages(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	db.UpsertMessage(cache.Message{TS: "1.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "hello"})
	db.UpsertMessage(cache.Message{TS: "2.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U2", Text: "world"})

	svc := NewMessageService(db)
	msgs, err := svc.GetMessages("C1", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestMessageServiceCacheMessage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewMessageService(db)
	svc.CacheMessage(cache.Message{TS: "1.0", ChannelID: "C1", WorkspaceID: "T1", UserID: "U1", Text: "new msg"})

	msgs, _ := svc.GetMessages("C1", 50)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Text != "new msg" {
		t.Errorf("expected 'new msg', got %q", msgs[0].Text)
	}
}
```

- [ ] **Step 6: Implement MessageService**

```go
// internal/service/messages.go
package service

import (
	"github.com/yourusername/slack-tui/internal/cache"
)

type MessageService struct {
	db *cache.DB
}

func NewMessageService(db *cache.DB) *MessageService {
	return &MessageService{db: db}
}

func (s *MessageService) GetMessages(channelID string, limit int) ([]cache.Message, error) {
	return s.db.GetMessages(channelID, limit, "")
}

func (s *MessageService) GetOlderMessages(channelID string, limit int, beforeTS string) ([]cache.Message, error) {
	return s.db.GetMessages(channelID, limit, beforeTS)
}

func (s *MessageService) GetThreadReplies(channelID, threadTS string) ([]cache.Message, error) {
	return s.db.GetThreadReplies(channelID, threadTS)
}

func (s *MessageService) CacheMessage(msg cache.Message) error {
	return s.db.UpsertMessage(msg)
}

func (s *MessageService) MarkDeleted(channelID, ts string) error {
	return s.db.DeleteMessage(channelID, ts)
}
```

- [ ] **Step 7: Run all service tests**

Run: `go test ./internal/service/ -v -race`
Expected: All tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/service/
git commit -m "feat: add WorkspaceManager and MessageService"
```

---

## Task 8: UI Foundation - Styles, Modes, Key Bindings

**Files:**
- Create: `internal/ui/styles/styles.go`
- Create: `internal/ui/mode.go`
- Create: `internal/ui/keys.go`

- [ ] **Step 1: Create lipgloss style definitions**

```go
// internal/ui/styles/styles.go
package styles

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	Primary     = lipgloss.Color("#4A9EFF")
	Secondary   = lipgloss.Color("#666666")
	Accent      = lipgloss.Color("#50C878")
	Warning     = lipgloss.Color("#E0A030")
	Error       = lipgloss.Color("#E04040")
	Background  = lipgloss.Color("#1A1A2E")
	Surface     = lipgloss.Color("#16162B")
	SurfaceDark = lipgloss.Color("#0F0F23")
	TextPrimary = lipgloss.Color("#E0E0E0")
	TextMuted   = lipgloss.Color("#888888")
	Border      = lipgloss.Color("#333333")

	// Panel styles
	FocusedBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Primary)

	UnfocusedBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Border)

	// Workspace rail
	WorkspaceActive = lipgloss.NewStyle().
			Background(Primary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1).
			Align(lipgloss.Center)

	WorkspaceInactive = lipgloss.NewStyle().
				Background(lipgloss.Color("#444444")).
				Foreground(TextPrimary).
				Padding(0, 1).
				Align(lipgloss.Center)

	// Channel sidebar
	ChannelSelected = lipgloss.NewStyle().
			Background(lipgloss.Color("#4A9EFF33")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	ChannelNormal = lipgloss.NewStyle().
			Foreground(TextPrimary).
			Padding(0, 1)

	ChannelUnread = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	UnreadBadge = lipgloss.NewStyle().
			Background(Error).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)

	SectionHeader = lipgloss.NewStyle().
			Foreground(TextMuted).
			Bold(true).
			MarginTop(1).
			Padding(0, 1)

	// Messages
	Username = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	Timestamp = lipgloss.NewStyle().
			Foreground(TextMuted).
			Italic(true)

	MessageText = lipgloss.NewStyle().
			Foreground(TextPrimary)

	ThreadIndicator = lipgloss.NewStyle().
			Foreground(Primary).
			Italic(true)

	// Status bar
	StatusBar = lipgloss.NewStyle().
			Background(SurfaceDark).
			Foreground(TextPrimary).
			Padding(0, 1)

	StatusMode = lipgloss.NewStyle().
			Background(Primary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	StatusModeInsert = lipgloss.NewStyle().
				Background(Accent).
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true).
				Padding(0, 1)

	StatusModeCommand = lipgloss.NewStyle().
				Background(Warning).
				Foreground(lipgloss.Color("#000000")).
				Bold(true).
				Padding(0, 1)

	// Compose box
	ComposeBox = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			Padding(0, 1)

	ComposeFocused = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(0, 1)

	// Presence indicators
	PresenceOnline = lipgloss.NewStyle().Foreground(Accent)
	PresenceAway   = lipgloss.NewStyle().Foreground(TextMuted)
)
```

- [ ] **Step 2: Create vim mode definitions**

```go
// internal/ui/mode.go
package ui

type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand
	ModeSearch
)

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeCommand:
		return "COMMAND"
	case ModeSearch:
		return "SEARCH"
	default:
		return "UNKNOWN"
	}
}
```

- [ ] **Step 3: Create key binding definitions**

```go
// internal/ui/keys.go
package ui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Up            key.Binding
	Down          key.Binding
	Left          key.Binding
	Right         key.Binding
	Enter         key.Binding
	Escape        key.Binding
	InsertMode    key.Binding
	CommandMode   key.Binding
	SearchMode    key.Binding
	Tab           key.Binding
	ShiftTab      key.Binding
	ToggleSidebar key.Binding
	ToggleThread  key.Binding
	FuzzyFinder   key.Binding
	FuzzyFinderAlt key.Binding
	Top           key.Binding
	Bottom        key.Binding
	Quit          key.Binding
	Reaction      key.Binding
	Edit          key.Binding
	Yank          key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:    key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/up", "up")),
		Down:  key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/down", "down")),
		Left:  key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/left", "left")),
		Right: key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/right", "right")),
		Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/confirm")),
		Escape: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		InsertMode:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "insert mode")),
		CommandMode:   key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command mode")),
		SearchMode:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Tab:           key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		ShiftTab:      key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev panel")),
		ToggleSidebar: key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "toggle sidebar")),
		ToggleThread:  key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "toggle thread")),
		FuzzyFinder:   key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "fuzzy find")),
		FuzzyFinderAlt: key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "fuzzy find")),
		Top:    key.NewBinding(key.WithKeys("g"), key.WithHelp("gg", "top")),
		Bottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		Quit:   key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
		Reaction: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "add reaction")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit message")),
		Yank:     key.NewBinding(key.WithKeys("y"), key.WithHelp("yy", "yank")),
	}
}
```

- [ ] **Step 4: Verify it compiles**

Run: `go build ./internal/ui/...`
Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "feat: add UI styles, vim modes, and key bindings"
```

---

## Task 9: UI Components - Workspace Rail & Channel Sidebar

**Files:**
- Create: `internal/ui/workspace/model.go`
- Create: `internal/ui/workspace/model_test.go`
- Create: `internal/ui/sidebar/model.go`
- Create: `internal/ui/sidebar/model_test.go`

- [ ] **Step 1: Write failing test for workspace rail**

```go
// internal/ui/workspace/model_test.go
package workspace

import (
	"strings"
	"testing"
)

func TestWorkspaceRailView(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Name: "Acme Corp", Initials: "AC", HasUnread: false},
		{ID: "T2", Name: "Beta Inc", Initials: "BI", HasUnread: true},
	}, 0)

	view := m.View(20) // 20 rows height
	if !strings.Contains(view, "AC") {
		t.Error("expected 'AC' in view")
	}
	if !strings.Contains(view, "BI") {
		t.Error("expected 'BI' in view")
	}
}

func TestWorkspaceRailSelect(t *testing.T) {
	m := New([]WorkspaceItem{
		{ID: "T1", Name: "Acme", Initials: "AC"},
		{ID: "T2", Name: "Beta", Initials: "BE"},
	}, 0)

	if m.SelectedID() != "T1" {
		t.Error("expected T1 selected initially")
	}

	m.Select(1)
	if m.SelectedID() != "T2" {
		t.Error("expected T2 selected after Select(1)")
	}
}
```

- [ ] **Step 2: Implement workspace rail model**

```go
// internal/ui/workspace/model.go
package workspace

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/slack-tui/internal/ui/styles"
)

type WorkspaceItem struct {
	ID        string
	Name      string
	Initials  string
	HasUnread bool
}

type Model struct {
	items    []WorkspaceItem
	selected int
}

func New(items []WorkspaceItem, selected int) Model {
	return Model{items: items, selected: selected}
}

func (m *Model) SelectedID() string {
	if len(m.items) == 0 {
		return ""
	}
	return m.items[m.selected].ID
}

func (m *Model) SelectedIndex() int {
	return m.selected
}

func (m *Model) Select(idx int) {
	if idx >= 0 && idx < len(m.items) {
		m.selected = idx
	}
}

func (m *Model) SetItems(items []WorkspaceItem) {
	m.items = items
	if m.selected >= len(items) {
		m.selected = 0
	}
}

func (m Model) View(height int) string {
	if len(m.items) == 0 {
		return ""
	}

	var rows []string
	for i, item := range m.items {
		var style lipgloss.Style
		if i == m.selected {
			style = styles.WorkspaceActive
		} else {
			style = styles.WorkspaceInactive
		}

		label := style.Render(item.Initials)
		if item.HasUnread && i != m.selected {
			label += "\n" + styles.PresenceOnline.Render("*")
		}
		rows = append(rows, label)
	}

	content := strings.Join(rows, "\n\n")

	rail := lipgloss.NewStyle().
		Width(6).
		Height(height).
		Background(styles.SurfaceDark).
		Padding(1, 0).
		Align(lipgloss.Center).
		Render(content)

	return rail
}

func (m Model) Width() int {
	return 6
}

func WorkspaceInitials(name string) string {
	words := strings.Fields(name)
	switch len(words) {
	case 0:
		return "?"
	case 1:
		if len(words[0]) >= 2 {
			return strings.ToUpper(words[0][:2])
		}
		return strings.ToUpper(words[0])
	default:
		return strings.ToUpper(fmt.Sprintf("%c%c", words[0][0], words[1][0]))
	}
}
```

- [ ] **Step 3: Run workspace rail tests**

Run: `go test ./internal/ui/workspace/ -v`
Expected: PASS.

- [ ] **Step 4: Write failing test for channel sidebar**

```go
// internal/ui/sidebar/model_test.go
package sidebar

import (
	"strings"
	"testing"
)

func TestSidebarView(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", UnreadCount: 0},
		{ID: "C2", Name: "random", Type: "channel", UnreadCount: 3},
		{ID: "C3", Name: "alice", Type: "dm", Presence: "active"},
	}

	m := New(channels)
	view := m.View(20, 25) // height=20, width=25

	if !strings.Contains(view, "general") {
		t.Error("expected 'general' in view")
	}
	if !strings.Contains(view, "random") {
		t.Error("expected 'random' in view")
	}
}

func TestSidebarNavigation(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	if m.SelectedID() != "C1" {
		t.Error("expected C1 selected initially")
	}

	m.MoveDown()
	if m.SelectedID() != "C2" {
		t.Error("expected C2 after move down")
	}

	m.MoveDown()
	m.MoveDown() // should stop at bottom
	if m.SelectedID() != "C3" {
		t.Error("expected C3 at bottom")
	}

	m.MoveUp()
	if m.SelectedID() != "C2" {
		t.Error("expected C2 after move up")
	}
}

func TestSidebarFilter(t *testing.T) {
	channels := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "C2", Name: "random", Type: "channel"},
		{ID: "C3", Name: "eng", Type: "channel"},
	}

	m := New(channels)
	m.SetFilter("gen")

	visible := m.VisibleItems()
	if len(visible) != 1 {
		t.Errorf("expected 1 filtered result, got %d", len(visible))
	}
	if visible[0].Name != "general" {
		t.Errorf("expected 'general', got %q", visible[0].Name)
	}

	m.SetFilter("")
	visible = m.VisibleItems()
	if len(visible) != 3 {
		t.Errorf("expected 3 items after clear filter, got %d", len(visible))
	}
}
```

- [ ] **Step 5: Implement channel sidebar model**

```go
// internal/ui/sidebar/model.go
package sidebar

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/slack-tui/internal/ui/styles"
)

type ChannelItem struct {
	ID          string
	Name        string
	Type        string // channel, dm, group_dm, private
	UnreadCount int
	IsStarred   bool
	Presence    string // for DMs: active, away, dnd
}

type Model struct {
	items    []ChannelItem
	selected int
	filter   string
	filtered []int // indices into items that match filter
}

func New(items []ChannelItem) Model {
	m := Model{items: items}
	m.rebuildFilter()
	return m
}

func (m *Model) SetItems(items []ChannelItem) {
	m.items = items
	m.rebuildFilter()
	if m.selected >= len(m.filtered) {
		m.selected = 0
	}
}

func (m *Model) SelectedID() string {
	if len(m.filtered) == 0 {
		return ""
	}
	idx := m.filtered[m.selected]
	return m.items[idx].ID
}

func (m *Model) SelectedItem() (ChannelItem, bool) {
	if len(m.filtered) == 0 {
		return ChannelItem{}, false
	}
	idx := m.filtered[m.selected]
	return m.items[idx], true
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) GoToTop() {
	m.selected = 0
}

func (m *Model) GoToBottom() {
	if len(m.filtered) > 0 {
		m.selected = len(m.filtered) - 1
	}
}

func (m *Model) SetFilter(filter string) {
	m.filter = filter
	m.selected = 0
	m.rebuildFilter()
}

func (m *Model) VisibleItems() []ChannelItem {
	var result []ChannelItem
	for _, idx := range m.filtered {
		result = append(result, m.items[idx])
	}
	return result
}

func (m *Model) SelectByID(id string) {
	for i, idx := range m.filtered {
		if m.items[idx].ID == id {
			m.selected = i
			return
		}
	}
}

func (m *Model) rebuildFilter() {
	m.filtered = nil
	lower := strings.ToLower(m.filter)
	for i, item := range m.items {
		if m.filter == "" || strings.Contains(strings.ToLower(item.Name), lower) {
			m.filtered = append(m.filtered, i)
		}
	}
}

func (m Model) View(height, width int) string {
	if len(m.items) == 0 {
		return lipgloss.NewStyle().Width(width).Height(height).Render("No channels")
	}

	// Group channels and DMs
	var channelRows []string
	var dmRows []string

	for fi, idx := range m.filtered {
		item := m.items[idx]
		isSelected := fi == m.selected

		var prefix string
		switch item.Type {
		case "dm":
			if item.Presence == "active" {
				prefix = styles.PresenceOnline.Render("* ")
			} else {
				prefix = styles.PresenceAway.Render("o ")
			}
		default:
			prefix = "# "
		}

		label := prefix + item.Name

		if item.UnreadCount > 0 {
			badge := styles.UnreadBadge.Render(fmt.Sprintf(" %d ", item.UnreadCount))
			label += " " + badge
		}

		var style lipgloss.Style
		if isSelected {
			style = styles.ChannelSelected
		} else if item.UnreadCount > 0 {
			style = styles.ChannelUnread
		} else {
			style = styles.ChannelNormal
		}

		row := style.Width(width - 2).Render(label)

		if item.Type == "dm" || item.Type == "group_dm" {
			dmRows = append(dmRows, row)
		} else {
			channelRows = append(channelRows, row)
		}
	}

	var sections []string

	if len(channelRows) > 0 {
		header := styles.SectionHeader.Render("Channels")
		sections = append(sections, header)
		sections = append(sections, channelRows...)
	}

	if len(dmRows) > 0 {
		header := styles.SectionHeader.Render("Direct Messages")
		sections = append(sections, header)
		sections = append(sections, dmRows...)
	}

	content := strings.Join(sections, "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(styles.Surface).
		Render(content)
}

func (m Model) Width() int {
	return 25
}
```

- [ ] **Step 6: Run sidebar tests**

Run: `go test ./internal/ui/sidebar/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/workspace/ internal/ui/sidebar/
git commit -m "feat: add workspace rail and channel sidebar UI components"
```

---

## Task 10: UI Components - Message Pane & Compose Box

**Files:**
- Create: `internal/ui/messages/model.go`
- Create: `internal/ui/messages/model_test.go`
- Create: `internal/ui/compose/model.go`
- Create: `internal/ui/compose/model_test.go`

- [ ] **Step 1: Write failing test for message pane**

```go
// internal/ui/messages/model_test.go
package messages

import (
	"strings"
	"testing"
)

func TestMessagePaneView(t *testing.T) {
	msgs := []MessageItem{
		{UserName: "alice", Text: "Hello world", Timestamp: "10:30 AM"},
		{UserName: "bob", Text: "Hey there!", Timestamp: "10:31 AM"},
	}

	m := New(msgs, "general")
	view := m.View(20, 60) // height=20, width=60

	if !strings.Contains(view, "alice") {
		t.Error("expected 'alice' in view")
	}
	if !strings.Contains(view, "Hello world") {
		t.Error("expected 'Hello world' in view")
	}
	if !strings.Contains(view, "general") {
		t.Error("expected channel name in header")
	}
}

func TestMessagePaneNavigation(t *testing.T) {
	msgs := []MessageItem{
		{TS: "1.0", UserName: "alice", Text: "msg 1"},
		{TS: "2.0", UserName: "bob", Text: "msg 2"},
		{TS: "3.0", UserName: "carol", Text: "msg 3"},
	}

	m := New(msgs, "general")
	// Should start at bottom (newest message)
	if m.SelectedIndex() != 2 {
		t.Errorf("expected selected index 2, got %d", m.SelectedIndex())
	}

	m.MoveUp()
	if m.SelectedIndex() != 1 {
		t.Errorf("expected index 1 after move up, got %d", m.SelectedIndex())
	}
}

func TestMessagePaneAppend(t *testing.T) {
	m := New(nil, "general")

	m.AppendMessage(MessageItem{TS: "1.0", UserName: "alice", Text: "new message"})
	if len(m.Messages()) != 1 {
		t.Errorf("expected 1 message, got %d", len(m.Messages()))
	}
}
```

- [ ] **Step 2: Implement message pane model**

```go
// internal/ui/messages/model.go
package messages

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/slack-tui/internal/ui/styles"
)

type MessageItem struct {
	TS          string
	UserName    string
	Text        string
	Timestamp   string
	ThreadTS    string
	ReplyCount  int
	Reactions   []ReactionItem
	IsEdited    bool
}

type ReactionItem struct {
	Emoji string
	Count int
}

type Model struct {
	messages    []MessageItem
	selected    int
	channelName string
	channelTopic string
}

func New(msgs []MessageItem, channelName string) Model {
	selected := 0
	if len(msgs) > 0 {
		selected = len(msgs) - 1
	}
	return Model{
		messages:    msgs,
		selected:    selected,
		channelName: channelName,
	}
}

func (m *Model) SetChannel(name, topic string) {
	m.channelName = name
	m.channelTopic = topic
}

func (m *Model) SetMessages(msgs []MessageItem) {
	m.messages = msgs
	if m.selected >= len(msgs) {
		m.selected = len(msgs) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

func (m *Model) AppendMessage(msg MessageItem) {
	m.messages = append(m.messages, msg)
	// Auto-scroll to bottom if we were at the bottom
	if m.selected == len(m.messages)-2 || len(m.messages) == 1 {
		m.selected = len(m.messages) - 1
	}
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

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.messages)-1 {
		m.selected++
	}
}

func (m *Model) GoToTop() {
	m.selected = 0
}

func (m *Model) GoToBottom() {
	if len(m.messages) > 0 {
		m.selected = len(m.messages) - 1
	}
}

func (m Model) View(height, width int) string {
	// Header
	header := styles.ChannelUnread.Copy().
		Width(width).
		Render(fmt.Sprintf("# %s", m.channelName))

	if m.channelTopic != "" {
		header += "\n" + styles.Timestamp.Width(width).Render(m.channelTopic)
	}

	headerHeight := lipgloss.Height(header) + 1 // +1 for separator
	separator := lipgloss.NewStyle().Width(width).Foreground(styles.Border).Render(strings.Repeat("-", width))

	// Messages area
	msgAreaHeight := height - headerHeight - 1
	if msgAreaHeight < 1 {
		msgAreaHeight = 1
	}

	var msgRows []string
	for i, msg := range m.messages {
		isSelected := i == m.selected

		// Username + timestamp
		userStyle := styles.Username
		if isSelected {
			userStyle = userStyle.Copy().Underline(true)
		}
		line := userStyle.Render(msg.UserName) + "  " + styles.Timestamp.Render(msg.Timestamp)

		// Message text
		text := styles.MessageText.Width(width - 4).Render(msg.Text)

		// Thread indicator
		var threadLine string
		if msg.ReplyCount > 0 {
			threadLine = "\n" + styles.ThreadIndicator.Render(
				fmt.Sprintf("[%d replies ->]", msg.ReplyCount))
		}

		// Reactions
		var reactionLine string
		if len(msg.Reactions) > 0 {
			var parts []string
			for _, r := range msg.Reactions {
				parts = append(parts, fmt.Sprintf("%s %d", r.Emoji, r.Count))
			}
			reactionLine = "\n" + lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
				strings.Join(parts, "  "))
		}

		// Edited indicator
		var editedMark string
		if msg.IsEdited {
			editedMark = " " + styles.Timestamp.Render("(edited)")
		}

		entry := line + editedMark + "\n" + text + threadLine + reactionLine

		if isSelected {
			entry = lipgloss.NewStyle().
				Background(lipgloss.Color("#222233")).
				Width(width - 2).
				Padding(0, 1).
				Render(entry)
		}

		msgRows = append(msgRows, entry)
	}

	msgContent := strings.Join(msgRows, "\n\n")

	return header + "\n" + separator + "\n" + lipgloss.NewStyle().
		Width(width).
		Height(msgAreaHeight).
		Render(msgContent)
}
```

- [ ] **Step 3: Run message pane tests**

Run: `go test ./internal/ui/messages/ -v`
Expected: PASS.

- [ ] **Step 4: Write failing test for compose box**

```go
// internal/ui/compose/model_test.go
package compose

import (
	"strings"
	"testing"
)

func TestComposeViewPlaceholder(t *testing.T) {
	m := New("general")
	view := m.View(40, false)

	if !strings.Contains(view, "general") {
		t.Error("expected channel name in placeholder")
	}
}

func TestComposeViewFocused(t *testing.T) {
	m := New("general")
	view := m.View(40, true)

	// When focused, should have a different style (focused border)
	if view == "" {
		t.Error("expected non-empty view when focused")
	}
}

func TestComposeValue(t *testing.T) {
	m := New("general")
	m.SetValue("hello world")

	if m.Value() != "hello world" {
		t.Errorf("expected 'hello world', got %q", m.Value())
	}

	m.Reset()
	if m.Value() != "" {
		t.Error("expected empty after reset")
	}
}
```

- [ ] **Step 5: Implement compose box model**

```go
// internal/ui/compose/model.go
package compose

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/slack-tui/internal/ui/styles"
)

type Model struct {
	input       textinput.Model
	channelName string
}

func New(channelName string) Model {
	ti := textinput.New()
	ti.Placeholder = "Message #" + channelName + "... (i to insert)"
	ti.CharLimit = 40000 // Slack's message limit
	ti.Width = 40

	return Model{
		input:       ti,
		channelName: channelName,
	}
}

func (m *Model) SetChannel(name string) {
	m.channelName = name
	m.input.Placeholder = "Message #" + name + "... (i to insert)"
}

func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

func (m *Model) Blur() {
	m.input.Blur()
}

func (m *Model) Value() string {
	return m.input.Value()
}

func (m *Model) SetValue(s string) {
	m.input.SetValue(s)
}

func (m *Model) Reset() {
	m.input.SetValue("")
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View(width int, focused bool) string {
	m.input.Width = width - 4 // account for padding/border

	var style lipgloss.Style
	if focused {
		style = styles.ComposeFocused.Copy().Width(width - 2)
	} else {
		style = styles.ComposeBox.Copy().Width(width - 2)
	}

	return style.Render(m.input.View())
}
```

- [ ] **Step 6: Run compose box tests**

Run: `go test ./internal/ui/compose/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ui/messages/ internal/ui/compose/
git commit -m "feat: add message pane and compose box UI components"
```

---

## Task 11: UI Components - Status Bar

**Files:**
- Create: `internal/ui/statusbar/model.go`
- Create: `internal/ui/statusbar/model_test.go`

- [ ] **Step 1: Write failing test for status bar**

```go
// internal/ui/statusbar/model_test.go
package statusbar

import (
	"strings"
	"testing"

	"github.com/yourusername/slack-tui/internal/ui"
)

func TestStatusBarNormalMode(t *testing.T) {
	m := New()
	m.SetMode(ui.ModeNormal)
	m.SetChannel("general")
	m.SetWorkspace("Acme Corp")

	view := m.View(80)

	if !strings.Contains(view, "NORMAL") {
		t.Error("expected 'NORMAL' in status bar")
	}
	if !strings.Contains(view, "general") {
		t.Error("expected 'general' in status bar")
	}
	if !strings.Contains(view, "Acme Corp") {
		t.Error("expected 'Acme Corp' in status bar")
	}
}

func TestStatusBarInsertMode(t *testing.T) {
	m := New()
	m.SetMode(ui.ModeInsert)
	view := m.View(80)

	if !strings.Contains(view, "INSERT") {
		t.Error("expected 'INSERT' in status bar")
	}
}

func TestStatusBarUnreadCount(t *testing.T) {
	m := New()
	m.SetUnreadCount(5)
	view := m.View(80)

	if !strings.Contains(view, "5") {
		t.Error("expected unread count in status bar")
	}
}
```

- [ ] **Step 2: Implement status bar**

```go
// internal/ui/statusbar/model.go
package statusbar

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/slack-tui/internal/ui"
	"github.com/yourusername/slack-tui/internal/ui/styles"
)

type Model struct {
	mode        ui.Mode
	channel     string
	workspace   string
	unreadCount int
	connected   bool
}

func New() Model {
	return Model{
		mode:      ui.ModeNormal,
		connected: true,
	}
}

func (m *Model) SetMode(mode ui.Mode) {
	m.mode = mode
}

func (m *Model) SetChannel(name string) {
	m.channel = name
}

func (m *Model) SetWorkspace(name string) {
	m.workspace = name
}

func (m *Model) SetUnreadCount(count int) {
	m.unreadCount = count
}

func (m *Model) SetConnected(connected bool) {
	m.connected = connected
}

func (m Model) View(width int) string {
	// Mode indicator
	var modeStyle lipgloss.Style
	switch m.mode {
	case ui.ModeInsert:
		modeStyle = styles.StatusModeInsert
	case ui.ModeCommand:
		modeStyle = styles.StatusModeCommand
	default:
		modeStyle = styles.StatusMode
	}
	modeLabel := modeStyle.Render(fmt.Sprintf(" %s ", m.mode.String()))

	// Channel info
	channelInfo := styles.StatusBar.Render(fmt.Sprintf(" #%s ", m.channel))

	// Workspace
	wsInfo := styles.StatusBar.Render(fmt.Sprintf(" %s ", m.workspace))

	// Right side: unread + connection
	var rightParts []string

	if m.unreadCount > 0 {
		rightParts = append(rightParts,
			styles.UnreadBadge.Render(fmt.Sprintf(" %d unread ", m.unreadCount)))
	}

	if m.connected {
		rightParts = append(rightParts, styles.PresenceOnline.Render("*"))
	} else {
		rightParts = append(rightParts, styles.Error.Render("DISCONNECTED"))
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center, modeLabel, channelInfo, wsInfo)

	rightContent := ""
	for _, p := range rightParts {
		rightContent += p + " "
	}

	// Fill the bar to full width
	gap := width - lipgloss.Width(left) - lipgloss.Width(rightContent)
	if gap < 0 {
		gap = 0
	}
	filler := styles.StatusBar.Render(fmt.Sprintf("%*s", gap, ""))

	return lipgloss.JoinHorizontal(lipgloss.Center, left, filler, rightContent)
}
```

- [ ] **Step 3: Run status bar tests**

Run: `go test ./internal/ui/statusbar/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/statusbar/
git commit -m "feat: add status bar UI component"
```

---

## Task 12: Root App Model & Layout

**Files:**
- Create: `internal/ui/app.go`
- Create: `internal/ui/app_test.go`

- [ ] **Step 1: Write failing test for app model**

```go
// internal/ui/app_test.go
package ui

import (
	"testing"

	"github.com/yourusername/slack-tui/internal/ui/sidebar"
	"github.com/yourusername/slack-tui/internal/ui/workspace"
)

func TestAppFocusCycle(t *testing.T) {
	app := NewApp()

	if app.focusedPanel != PanelSidebar {
		t.Errorf("expected initial focus on sidebar, got %d", app.focusedPanel)
	}

	app.FocusNext()
	if app.focusedPanel != PanelMessages {
		t.Errorf("expected focus on messages, got %d", app.focusedPanel)
	}

	app.FocusNext()
	if app.focusedPanel != PanelSidebar {
		t.Errorf("expected focus to wrap to sidebar, got %d", app.focusedPanel)
	}

	app.FocusPrev()
	if app.focusedPanel != PanelMessages {
		t.Errorf("expected focus on messages after prev, got %d", app.focusedPanel)
	}
}

func TestAppToggleSidebar(t *testing.T) {
	app := NewApp()

	if !app.sidebarVisible {
		t.Error("expected sidebar visible initially")
	}

	app.ToggleSidebar()
	if app.sidebarVisible {
		t.Error("expected sidebar hidden after toggle")
	}

	app.ToggleSidebar()
	if !app.sidebarVisible {
		t.Error("expected sidebar visible after second toggle")
	}
}

func TestAppModeTransitions(t *testing.T) {
	app := NewApp()

	if app.mode != ModeNormal {
		t.Error("expected normal mode initially")
	}

	app.SetMode(ModeInsert)
	if app.mode != ModeInsert {
		t.Error("expected insert mode")
	}

	app.SetMode(ModeNormal)
	if app.mode != ModeNormal {
		t.Error("expected normal mode after escape")
	}
}
```

- [ ] **Step 2: Implement root app model**

```go
// internal/ui/app.go
package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yourusername/slack-tui/internal/ui/compose"
	"github.com/yourusername/slack-tui/internal/ui/messages"
	"github.com/yourusername/slack-tui/internal/ui/sidebar"
	"github.com/yourusername/slack-tui/internal/ui/statusbar"
	"github.com/yourusername/slack-tui/internal/ui/styles"
	"github.com/yourusername/slack-tui/internal/ui/workspace"
)

type Panel int

const (
	PanelWorkspace Panel = iota
	PanelSidebar
	PanelMessages
	PanelThread
)

// Messages sent between components
type (
	ChannelSelectedMsg struct {
		ID   string
		Name string
	}
	MessagesLoadedMsg struct {
		ChannelID string
		Messages  []messages.MessageItem
	}
	NewMessageMsg struct {
		Message messages.MessageItem
	}
	SendMessageMsg struct {
		ChannelID string
		Text      string
	}
)

type App struct {
	// Sub-models
	workspaceRail workspace.Model
	sidebar       sidebar.Model
	messagepane   messages.Model
	compose       compose.Model
	statusbar     statusbar.Model

	// State
	mode           Mode
	focusedPanel   Panel
	sidebarVisible bool
	threadVisible  bool
	width          int
	height         int
	keys           KeyMap

	// Current context
	activeChannelID string
}

func NewApp() *App {
	return &App{
		workspaceRail:  workspace.New(nil, 0),
		sidebar:        sidebar.New(nil),
		messagepane:    messages.New(nil, ""),
		compose:        compose.New(""),
		statusbar:      statusbar.New(),
		mode:           ModeNormal,
		focusedPanel:   PanelSidebar,
		sidebarVisible: true,
		keys:           DefaultKeyMap(),
	}
}

func (a *App) Init() tea.Cmd {
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil

	case tea.KeyMsg:
		cmd := a.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case ChannelSelectedMsg:
		a.activeChannelID = msg.ID
		a.messagepane.SetChannel(msg.Name, "")
		a.compose.SetChannel(msg.Name)
		a.statusbar.SetChannel(msg.Name)

	case MessagesLoadedMsg:
		if msg.ChannelID == a.activeChannelID {
			a.messagepane.SetMessages(msg.Messages)
		}

	case NewMessageMsg:
		a.messagepane.AppendMessage(msg.Message)
	}

	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	// Always handle quit
	if key.Matches(msg, a.keys.Quit) {
		return tea.Quit
	}

	// Mode-specific handling
	switch a.mode {
	case ModeInsert:
		return a.handleInsertMode(msg)
	case ModeCommand:
		return a.handleCommandMode(msg)
	default:
		return a.handleNormalMode(msg)
	}
}

func (a *App) handleNormalMode(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, a.keys.InsertMode):
		a.SetMode(ModeInsert)
		a.focusedPanel = PanelMessages
		return a.compose.Focus()

	case key.Matches(msg, a.keys.Escape):
		a.SetMode(ModeNormal)
		a.compose.Blur()

	case key.Matches(msg, a.keys.Tab):
		a.FocusNext()

	case key.Matches(msg, a.keys.ShiftTab):
		a.FocusPrev()

	case key.Matches(msg, a.keys.ToggleSidebar):
		a.ToggleSidebar()

	case key.Matches(msg, a.keys.Down):
		a.handleDown()

	case key.Matches(msg, a.keys.Up):
		a.handleUp()

	case key.Matches(msg, a.keys.Left):
		a.FocusPrev()

	case key.Matches(msg, a.keys.Right):
		a.FocusNext()

	case key.Matches(msg, a.keys.Enter):
		return a.handleEnter()

	case key.Matches(msg, a.keys.Bottom):
		a.handleGoToBottom()

	}
	return nil
}

func (a *App) handleInsertMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.SetMode(ModeNormal)
		a.compose.Blur()
		return nil
	}

	// Handle Enter in insert mode to send message
	if msg.Type == tea.KeyEnter {
		text := a.compose.Value()
		if text != "" {
			a.compose.Reset()
			return func() tea.Msg {
				return SendMessageMsg{
					ChannelID: a.activeChannelID,
					Text:      text,
				}
			}
		}
		return nil
	}

	// Forward to compose box
	var cmd tea.Cmd
	a.compose, cmd = a.compose.Update(msg)
	return cmd
}

func (a *App) handleCommandMode(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, a.keys.Escape) {
		a.SetMode(ModeNormal)
	}
	return nil
}

func (a *App) handleDown() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveDown()
	case PanelMessages:
		a.messagepane.MoveDown()
	}
}

func (a *App) handleUp() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.MoveUp()
	case PanelMessages:
		a.messagepane.MoveUp()
	}
}

func (a *App) handleGoToBottom() {
	switch a.focusedPanel {
	case PanelSidebar:
		a.sidebar.GoToBottom()
	case PanelMessages:
		a.messagepane.GoToBottom()
	}
}

func (a *App) handleEnter() tea.Cmd {
	if a.focusedPanel == PanelSidebar {
		item, ok := a.sidebar.SelectedItem()
		if ok {
			return func() tea.Msg {
				return ChannelSelectedMsg{ID: item.ID, Name: item.Name}
			}
		}
	}
	return nil
}

func (a *App) SetMode(mode Mode) {
	a.mode = mode
	a.statusbar.SetMode(mode)
}

func (a *App) FocusNext() {
	if a.sidebarVisible {
		if a.focusedPanel == PanelSidebar {
			a.focusedPanel = PanelMessages
		} else {
			a.focusedPanel = PanelSidebar
		}
	}
}

func (a *App) FocusPrev() {
	if a.sidebarVisible {
		if a.focusedPanel == PanelMessages {
			a.focusedPanel = PanelSidebar
		} else {
			a.focusedPanel = PanelMessages
		}
	}
}

func (a *App) ToggleSidebar() {
	a.sidebarVisible = !a.sidebarVisible
	if !a.sidebarVisible && a.focusedPanel == PanelSidebar {
		a.focusedPanel = PanelMessages
	}
}

// Setters for external use (wiring services)
func (a *App) SetWorkspaces(items []workspace.WorkspaceItem) {
	a.workspaceRail.SetItems(items)
}

func (a *App) SetChannels(items []sidebar.ChannelItem) {
	a.sidebar.SetItems(items)
}

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Initializing..."
	}

	statusHeight := 1
	contentHeight := a.height - statusHeight

	// Calculate widths
	railWidth := a.workspaceRail.Width()
	sidebarWidth := 0
	if a.sidebarVisible {
		sidebarWidth = a.sidebar.Width()
	}
	msgWidth := a.width - railWidth - sidebarWidth

	// Render panels
	rail := a.workspaceRail.View(contentHeight)

	var panels []string
	panels = append(panels, rail)

	if a.sidebarVisible {
		sidebarView := a.sidebar.View(contentHeight, sidebarWidth)
		if a.focusedPanel == PanelSidebar {
			sidebarView = styles.FocusedBorder.Width(sidebarWidth).Render(sidebarView)
		}
		panels = append(panels, sidebarView)
	}

	// Message pane = messages + compose
	composeHeight := 3
	msgContentHeight := contentHeight - composeHeight
	msgView := a.messagepane.View(msgContentHeight, msgWidth)
	composeView := a.compose.View(msgWidth, a.mode == ModeInsert)
	msgPanel := lipgloss.JoinVertical(lipgloss.Left, msgView, composeView)
	panels = append(panels, msgPanel)

	content := lipgloss.JoinHorizontal(lipgloss.Top, panels...)
	status := a.statusbar.View(a.width)

	return lipgloss.JoinVertical(lipgloss.Left, content, status)
}
```

- [ ] **Step 3: Run app tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat: add root app model with layout and vim mode switching"
```

---

## Task 13: Integration - Wire Everything Together

**Files:**
- Modify: `cmd/slack-tui/main.go`

- [ ] **Step 1: Implement main.go with dependency wiring**

This wires the config, cache, services, and UI together. For the MVP, we use a simplified startup that loads tokens from the store and connects to Slack.

```go
// cmd/slack-tui/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yourusername/slack-tui/internal/cache"
	"github.com/yourusername/slack-tui/internal/config"
	"github.com/yourusername/slack-tui/internal/service"
	slackclient "github.com/yourusername/slack-tui/internal/slack"
	"github.com/yourusername/slack-tui/internal/ui"
	"github.com/yourusername/slack-tui/internal/ui/messages"
	"github.com/yourusername/slack-tui/internal/ui/sidebar"
	"github.com/yourusername/slack-tui/internal/ui/workspace"
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
	_ = cfg // will use for animation settings etc.

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
						Timestamp:  formatTimestamp(m.Timestamp),
						ThreadTS:   m.ThreadTimestamp,
						ReplyCount: m.ReplyCount,
					})
				}
				// Reverse: Slack returns newest first
				for i, j := 0, len(msgItems)-1; i < j; i, j = i+1, j-1 {
					msgItems[i], msgItems[j] = msgItems[j], msgItems[i]
				}
				app.SetChannels(sidebarItems)
			}
		}
	}

	app.SetWorkspaces(wsItems)
	_ = msgSvc // will wire for send/receive

	// Run the TUI
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func formatTimestamp(ts string) string {
	// Simple timestamp formatting - parse Slack ts to readable time
	// Slack ts is like "1700000001.000000"
	return ts // placeholder - will format properly
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
```

- [ ] **Step 2: Verify it compiles**

Run: `make build`
Expected: Binary compiles successfully.

- [ ] **Step 3: Run all tests**

Run: `make test`
Expected: All tests across all packages PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/slack-tui/main.go
git commit -m "feat: wire up main with config, cache, slack client, and UI"
```

- [ ] **Step 5: Run the binary to verify startup behavior**

Run: `./bin/slack-tui`
Expected: Should show an error message about no workspaces configured (since no tokens exist yet). This confirms the startup flow works correctly.

- [ ] **Step 6: Final commit with any fixes from the integration test**

```bash
git add -A
git commit -m "fix: address integration issues from startup test"
```
