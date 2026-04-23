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
