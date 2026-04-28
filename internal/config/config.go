package config

import (
	"errors"
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
	Sections      map[string]SectionDef        `toml:"sections"`
	Theme         Theme                        `toml:"theme"`
	Workspaces    map[string]WorkspaceSettings `toml:"workspaces"`
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

// WorkspaceSettings holds per-workspace user preferences. Currently
// only Theme is configurable; future per-workspace settings (notification
// rules, default channel, etc.) belong here.
type WorkspaceSettings struct {
	Theme string `toml:"theme"`
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

// MatchSection returns the section name for a given channel name,
// or empty string if no section matches.
func (c Config) MatchSection(channelName string) string {
	// Build ordered list of sections
	type entry struct {
		name     string
		order    int
		patterns []string
	}
	var entries []entry
	for name, def := range c.Sections {
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
// is set, and to "dark" when no global theme is set either.
func (c Config) ResolveTheme(teamID string) string {
	if ws, ok := c.Workspaces[teamID]; ok && ws.Theme != "" {
		return ws.Theme
	}
	if c.Appearance.Theme != "" {
		return c.Appearance.Theme
	}
	return "dark"
}
