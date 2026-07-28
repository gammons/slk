package slackdesktop

import (
	"encoding/json"
	"sort"
	"unicode/utf16"
)

// decodeLocalStorageValue strips the 1-byte encoding marker Chromium's Local
// Storage prepends to values: 0x00 => UTF-16LE, 0x01 => UTF-8/Latin-1.
func decodeLocalStorageValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		b := v[1:]
		u16 := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return string(utf16.Decode(u16))
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

// parseLocalConfig parses the localConfig_v2 JSON into signed-in workspaces
// (teams with a usable token). Entries missing domain/id/token are skipped.
func parseLocalConfig(data []byte) ([]Workspace, error) {
	var cfg struct {
		Teams map[string]struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Domain string `json:"domain"`
			Token  string `json:"token"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	var out []Workspace
	for _, t := range cfg.Teams {
		if t.Domain == "" || t.ID == "" || t.Token == "" {
			continue
		}
		out = append(out, Workspace{Name: t.Name, Domain: t.Domain, TeamID: t.ID, Token: t.Token})
	}
	if len(out) == 0 {
		return nil, ErrNotSignedIn
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
