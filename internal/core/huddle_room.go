package core

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// HuddleRoom is the `room` object Slack attaches to a huddle_thread
// message: who started the huddle and when it began and ended.
//
// Only the fields slk has seen are decoded, and leniently: a missing or
// malformed one stays at its zero value. Raw keeps the object verbatim
// (the shape of the participant fields is not known), so nothing Slack
// sends is lost between capture and rendering.
type HuddleRoom struct {
	CreatedBy string
	// DateStart and DateEnd are Unix seconds; 0 when absent or unparseable.
	DateStart int64
	DateEnd   int64
	HasEnded  bool
	// Participants are user IDs, taken from the first of `participants`,
	// `participant_history`, `attendees` that is an array of strings or of
	// objects with a user/user_id/id string. These names are unverified
	// guesses; any other shape leaves it empty.
	Participants []string
	Raw          json.RawMessage
}

var participantKeys = []string{"participants", "participant_history", "attendees"}

func participantIDs(obj map[string]json.RawMessage) []string {
	for _, k := range participantKeys {
		raw, ok := obj[k]
		if !ok {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			continue
		}
		var ids []string
		for _, it := range items {
			var s string
			if json.Unmarshal(it, &s) == nil {
				if s != "" {
					ids = append(ids, s)
				}
				continue
			}
			var o map[string]json.RawMessage
			if json.Unmarshal(it, &o) != nil {
				continue
			}
			for _, f := range []string{"user", "user_id", "id"} {
				if json.Unmarshal(o[f], &s) == nil && s != "" {
					ids = append(ids, s)
					break
				}
			}
		}
		return ids
	}
	return nil
}

// ParseHuddleRoom decodes a message's `room` object. It returns nil when
// raw is empty, null or not a JSON object, so a caller can use the nil
// to mean "this message has no room".
func ParseHuddleRoom(raw json.RawMessage) *HuddleRoom {
	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var f struct {
		CreatedBy json.RawMessage `json:"created_by"`
		DateStart json.RawMessage `json:"date_start"`
		DateEnd   json.RawMessage `json:"date_end"`
		HasEnded  json.RawMessage `json:"has_ended"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return nil
	}
	r := &HuddleRoom{
		DateStart: lenientUnix(f.DateStart),
		DateEnd:   lenientUnix(f.DateEnd),
		Raw:       append(json.RawMessage(nil), raw...),
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &all) == nil {
		r.Participants = participantIDs(all)
	}
	_ = json.Unmarshal(f.CreatedBy, &r.CreatedBy)
	var b bool
	if json.Unmarshal(f.HasEnded, &b) == nil {
		r.HasEnded = b
	} else {
		var s string
		if json.Unmarshal(f.HasEnded, &s) == nil {
			r.HasEnded, _ = strconv.ParseBool(s)
		}
	}
	return r
}

// lenientUnix reads a Unix-seconds value that Slack may send as a JSON
// number (integer or fractional) or as a string. Anything else is 0.
func lenientUnix(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var f float64
	if json.Unmarshal(raw, &f) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0
		}
		var err error
		if f, err = strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
			return 0
		}
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > math.MaxInt64/2 {
		return 0
	}
	return int64(f)
}
