package slackclient

import "encoding/json"

// SidebarSection represents one entry in the user's Slack sidebar
// section list. Both the REST endpoint (users.channelSections.list)
// and the WebSocket events use this model after normalization;
// REST and WS use different field names for the same data, so the
// decoders translate into this canonical shape.
type SidebarSection struct {
	ID         string
	Name       string
	Type       string // standard | channels | direct_messages | recent_apps | stars | slack_connect | salesforce_records | agents
	Emoji      string
	Next       string // next_channel_section_id; "" = tail
	LastUpdate int64  // unix seconds
	IsRedacted bool

	// ChannelIDs is the membership of this section. Populated from
	// channel_ids_page on REST decode (first page only); follow-up
	// pagination calls or WS deltas extend it.
	ChannelIDs []string

	// ChannelsCount is total membership reported by the server,
	// even when ChannelIDs holds only the first page. Cursor is
	// non-empty when more pages remain.
	ChannelsCount  int
	ChannelsCursor string
}

// channelSectionsListResponse mirrors the REST shape of
// users.channelSections.list. SidebarSection's custom UnmarshalJSON
// normalizes field names.
type channelSectionsListResponse struct {
	OK       bool             `json:"ok"`
	Error    string           `json:"error"`
	Sections []SidebarSection `json:"channel_sections"`
	Cursor   string           `json:"cursor"`
	Count    int              `json:"count"`
}

// restSectionEnvelope is the literal REST JSON shape; we decode into
// it and then copy into SidebarSection so SidebarSection itself can
// be the canonical model.
type restSectionEnvelope struct {
	ID         string `json:"channel_section_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Emoji      string `json:"emoji"`
	Next       string `json:"next_channel_section_id"`
	LastUpdate int64  `json:"last_updated"`
	IsRedacted bool   `json:"is_redacted"`
	Page       struct {
		ChannelIDs []string `json:"channel_ids"`
		Count      int      `json:"count"`
		Cursor     string   `json:"cursor"`
	} `json:"channel_ids_page"`
}

// UnmarshalJSON for SidebarSection accepts the REST envelope and
// normalizes into the canonical struct.
func (s *SidebarSection) UnmarshalJSON(data []byte) error {
	var env restSectionEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	s.ID = env.ID
	s.Name = env.Name
	s.Type = env.Type
	s.Emoji = env.Emoji
	s.Next = env.Next
	s.LastUpdate = env.LastUpdate
	s.IsRedacted = env.IsRedacted
	s.ChannelIDs = env.Page.ChannelIDs
	s.ChannelsCount = env.Page.Count
	s.ChannelsCursor = env.Page.Cursor
	return nil
}

// wsChannelSectionUpserted is the literal WS JSON shape for a
// channel_section_upserted event. WS uses channel_section_type and
// last_update where REST uses type and last_updated; the toUpserted
// translator normalizes.
type wsChannelSectionUpserted struct {
	ID         string `json:"channel_section_id"`
	Name       string `json:"name"`
	Type       string `json:"channel_section_type"`
	Emoji      string `json:"emoji"`
	Next       string `json:"next_channel_section_id"`
	LastUpdate int64  `json:"last_update"`
	IsRedacted bool   `json:"is_redacted"`
}

func (e wsChannelSectionUpserted) toUpserted() ChannelSectionUpserted {
	return ChannelSectionUpserted{
		ID:         e.ID,
		Name:       e.Name,
		Type:       e.Type,
		Emoji:      e.Emoji,
		Next:       e.Next,
		LastUpdate: e.LastUpdate,
		IsRedacted: e.IsRedacted,
	}
}

// ChannelSectionUpserted carries the data from a channel_section_upserted
// WS event into the EventHandler.
type ChannelSectionUpserted struct {
	ID         string
	Name       string
	Type       string
	Emoji      string
	Next       string
	LastUpdate int64
	IsRedacted bool
}

// wsChannelSectionDeleted is the WS shape for channel_section_deleted.
type wsChannelSectionDeleted struct {
	ID         string `json:"channel_section_id"`
	LastUpdate int64  `json:"last_update"`
}

// wsChannelSectionsChannelsDelta is the WS shape for both the
// channel_sections_channels_upserted and _removed events.
type wsChannelSectionsChannelsDelta struct {
	SectionID  string   `json:"channel_section_id"`
	ChannelIDs []string `json:"channel_ids"`
	LastUpdate int64    `json:"last_update"`
}
