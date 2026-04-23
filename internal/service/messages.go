package service

import (
	"github.com/gammons/slack-tui/internal/cache"
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
