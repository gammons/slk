package slackclient

import (
	"bytes"
	"cmp"
	"encoding/json"
	"sync"

	"github.com/gammons/slk/internal/core"
)

// An incoming webhook can post each message under its own username and
// icon. The icon arrives as an `icons` object with image_36/48/72 URLs,
// which slack-go cannot decode (its Icon type reads icon_url and
// icon_emoji), so the typed history and replies calls drop it. roomTap
// reads it off those responses and the WebSocket loop off message
// frames, keyed like the message's author (core.BotAuthorID), for
// BotIcon. An emoji icon is not kept: the bots.info icon stands in.

// botIconStoreMax bounds the remembered senders. A full store starts over.
const botIconStoreMax = 1024

// botIconStore maps a core.BotAuthorID to the icon URL its latest message
// carried. Safe for concurrent use. A nil store records and returns
// nothing.
type botIconStore struct {
	mu    sync.Mutex
	icons map[string]string
}

func newBotIconStore() *botIconStore {
	return &botIconStore{icons: map[string]string{}}
}

// iconMessage is the part of a message the store reads.
type iconMessage struct {
	Type     string `json:"type"`
	BotID    string `json:"bot_id"`
	Username string `json:"username"`
	Icons    struct {
		Image36 string `json:"image_36"`
		Image48 string `json:"image_48"`
		Image72 string `json:"image_72"`
	} `json:"icons"`
}

func (s *botIconStore) put(m iconMessage) {
	icon := cmp.Or(m.Icons.Image72, m.Icons.Image48, m.Icons.Image36)
	if m.BotID == "" || icon == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.icons) >= botIconStoreMax {
		clear(s.icons)
	}
	s.icons[core.BotAuthorID(m.BotID, m.Username)] = icon
}

func (s *botIconStore) get(authorID string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.icons[authorID]
}

// recordMessages keeps the icons of the messages in a history or replies
// response body.
func (s *botIconStore) recordMessages(body []byte) {
	if s == nil || !bytes.Contains(body, []byte(`"icons"`)) {
		return
	}
	var env struct {
		Messages []iconMessage `json:"messages"`
	}
	if json.Unmarshal(body, &env) != nil {
		return
	}
	for _, m := range env.Messages {
		s.put(m)
	}
}

// recordFrame keeps the icon of a WebSocket message frame.
func (s *botIconStore) recordFrame(frame []byte) {
	if s == nil || !bytes.Contains(frame, []byte(`"icons"`)) {
		return
	}
	var m iconMessage
	if json.Unmarshal(frame, &m) != nil || m.Type != "message" {
		return
	}
	s.put(m)
}

// BotIcon returns the icon URL the latest message of a bot author (a
// core.BotAuthorID) carried in a history, replies or WebSocket message,
// or "" when none was seen.
func (c *Client) BotIcon(authorID string) string {
	if c == nil {
		return ""
	}
	return c.botIcons.get(authorID)
}
