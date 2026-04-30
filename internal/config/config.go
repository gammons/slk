package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	General       General                      `toml:"general"`
	Appearance    Appearance                   `toml:"appearance"`
	Animations    Animations                   `toml:"animations"`
	Notifications Notifications                `toml:"notifications"`
	Cache         CacheConfig                  `toml:"cache"`
	Sidebar       Sidebar                      `toml:"sidebar"`
	Sections      map[string]SectionDef        `toml:"sections"`
	Theme         Theme                        `toml:"theme"`
	Workspaces    map[string]Workspace         `toml:"workspaces"`
}

// SectionDef defines a sidebar section with channel name patterns.
// Channels matching any pattern are placed in this section.
// Patterns support simple glob matching (* for any characters).
type SectionDef struct {
	Channels []string `toml:"channels"`
	Order    int      `toml:"order"` // lower = higher in sidebar
}

type General struct {
	DefaultWorkspace string `toml:"default_workspace"`
}

type Appearance struct {
	Theme           string `toml:"theme"`
	TimestampFormat string `toml:"timestamp_format"`
	ShowAvatars     bool   `toml:"show_avatars"`
	// ImageProtocol controls how inline images are rendered.
	// One of: "auto", "kitty", "sixel", "halfblock", "off".
	ImageProtocol string `toml:"image_protocol"`
	// MaxImageRows caps the height of inline images in terminal rows.
	MaxImageRows int `toml:"max_image_rows"`
	// MaxImageCols caps the width of inline images in terminal columns.
	// If 0 or unset, defaults to 60. The image is also bounded by the
	// available message-pane width when narrower.
	MaxImageCols int `toml:"max_image_cols"`
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
	// MaxImageCacheMB caps the on-disk/in-memory image cache size in MB.
	MaxImageCacheMB int64 `toml:"max_image_cache_mb"`
}

// Sidebar holds preferences governing what appears in the channel
// sidebar.
type Sidebar struct {
	// HideInactiveAfterDays auto-hides channels (of any type) whose
	// last_read_ts is older than this many days. Set to 0 to disable.
	// Channels matching a custom [sections.*] glob, channels with
	// unread messages, and the currently-selected channel are never
	// hidden regardless of this setting.
	HideInactiveAfterDays int `toml:"hide_inactive_after_days"`
}

// Workspace holds per-workspace user preferences. The TOML key for
// the surrounding map can be either a user-chosen slug (with TeamID
// set explicitly via team_id) or — for backward compatibility —
// a raw Slack team ID (with TeamID left empty; Load fills it in
// from the key).
type Workspace struct {
	TeamID   string                `toml:"team_id"`
	Theme    string                `toml:"theme"`
	Sections map[string]SectionDef `toml:"sections"`
}

type Theme struct {
	Primary     string `toml:"primary"`
	Accent      string `toml:"accent"`
	Warning     string `toml:"warning"`
	Error       string `toml:"error"`
	Background  string `toml:"background"`
	Surface     string `toml:"surface"`
	SurfaceDark string `toml:"surface_dark"`
	Text        string `toml:"text"`
	TextMuted   string `toml:"text_muted"`
	Border      string `toml:"border"`
}

func Default() Config {
	return Config{
		Appearance: Appearance{
			Theme:           "nord",
			TimestampFormat: "3:04 PM",
			ImageProtocol:   "auto",
			MaxImageRows:    20,
			MaxImageCols:    60,
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
			MaxImageCacheMB:      200,
		},
		Sidebar: Sidebar{
			HideInactiveAfterDays: 30,
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

	resolved, err := resolveWorkspaceKeys(cfg.Workspaces)
	if err != nil {
		return cfg, err
	}
	cfg.Workspaces = resolved

	return cfg, nil
}

// WorkspaceByTeamID returns the configured Workspace for the given
// team ID, scanning c.Workspaces (which is keyed by either slug or
// legacy team ID). Returns false if no workspace matches.
func (c Config) WorkspaceByTeamID(teamID string) (Workspace, bool) {
	if teamID == "" {
		return Workspace{}, false
	}
	for _, ws := range c.Workspaces {
		if ws.TeamID == teamID {
			return ws, true
		}
	}
	return Workspace{}, false
}

// TeamIDForDefaultWorkspace resolves general.default_workspace to a
// team ID. The configured value can be either a slug ([workspaces.<slug>])
// or a legacy team-ID-shaped key. Returns ("", nil) if default_workspace
// is unset, and an error if it is set but does not match any
// configured workspace.
func (c Config) TeamIDForDefaultWorkspace() (string, error) {
	key := c.General.DefaultWorkspace
	if key == "" {
		return "", nil
	}
	if ws, ok := c.Workspaces[key]; ok {
		return ws.TeamID, nil
	}
	return "", fmt.Errorf("default_workspace %q not found in [workspaces.*]", key)
}

// MatchSection returns the section name for a given channel name in
// the context of the given workspace. If the workspace has its own
// non-empty Sections map, that fully replaces the global Sections;
// otherwise the global Sections apply. Returns "" if no pattern
// matches.
func (c Config) MatchSection(teamID, channelName string) string {
	sections := c.Sections
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && len(ws.Sections) > 0 {
		sections = ws.Sections
	}
	return matchSectionIn(sections, channelName)
}

// SectionOrder returns the Order field for the named section,
// resolved through the same workspace-vs-global precedence as
// MatchSection. Returns 0 if the section is not defined.
func (c Config) SectionOrder(teamID, sectionName string) int {
	sections := c.Sections
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && len(ws.Sections) > 0 {
		sections = ws.Sections
	}
	if def, ok := sections[sectionName]; ok {
		return def.Order
	}
	return 0
}

// matchSectionIn walks sections in Order-ascending order and returns
// the first section name whose patterns match channelName.
func matchSectionIn(sections map[string]SectionDef, channelName string) string {
	type entry struct {
		name     string
		order    int
		patterns []string
	}
	var entries []entry
	for name, def := range sections {
		entries = append(entries, entry{name: name, order: def.Order, patterns: def.Channels})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].order < entries[j].order
	})
	for _, e := range entries {
		for _, pattern := range e.patterns {
			if matched, _ := filepath.Match(pattern, channelName); matched {
				return e.name
			}
		}
	}
	return ""
}

// ResolveTheme returns the theme name to use for the given workspace,
// falling back to the global Appearance.Theme when no per-workspace theme
// is set, and to "nord" when no global theme is set either.
func (c Config) ResolveTheme(teamID string) string {
	if ws, ok := c.WorkspaceByTeamID(teamID); ok && ws.Theme != "" {
		return ws.Theme
	}
	if c.Appearance.Theme != "" {
		return c.Appearance.Theme
	}
	return "nord"
}
