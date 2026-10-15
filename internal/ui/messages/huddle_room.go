package messages

import (
	"fmt"
	"strings"
	"time"
)

// HuddleStarter returns the user ID that started the huddle a huddle_thread
// message announces, or "" when the message carries no room or the room
// names no creator.
func HuddleStarter(msg MessageItem) string {
	if msg.HuddleRoom == nil {
		return ""
	}
	return msg.HuddleRoom.CreatedBy
}

// huddleStarterName is the starter's display name from names, "" when the
// message is not a huddle announcement, has no room or starter, or the
// starter does not resolve. Callers fall back to today's rendering then.
func huddleStarterName(msg MessageItem, names map[string]string) (id, name string) {
	if msg.Subtype != "huddle_thread" {
		return "", ""
	}
	id = HuddleStarter(msg)
	if id == "" {
		return "", ""
	}
	return id, names[id]
}

// FormatHuddleDuration words how long a huddle lasted: "under a minute",
// "23 min", "1h 5m".
func FormatHuddleDuration(d time.Duration) string {
	mins := int(d / time.Minute)
	switch {
	case mins < 1:
		return "under a minute"
	case mins < 60:
		return fmt.Sprintf("%d min", mins)
	case mins%60 == 0:
		return fmt.Sprintf("%dh", mins/60)
	}
	return fmt.Sprintf("%dh %dm", mins/60, mins%60)
}

// huddleFacts is the " · lasted 23 min · with A, B" tail: the duration
// when the huddle ended with a valid one, and the participants (other than
// the starter) whose names resolve. Empty when there is nothing to add.
func huddleFacts(msg MessageItem, starterID string, names map[string]string) string {
	var tail string
	if d, ok := HuddleDuration(msg.HuddleRoom); ok {
		tail += " · lasted " + FormatHuddleDuration(d)
	}
	var with []string
	seen := map[string]bool{starterID: true}
	for _, id := range msg.HuddleRoom.Participants {
		if seen[id] {
			continue
		}
		seen[id] = true
		if n := names[id]; n != "" {
			with = append(with, n)
		}
	}
	switch {
	case len(with) == 0:
	case len(with) <= 3:
		tail += " · with " + strings.Join(with, ", ")
	default:
		tail += fmt.Sprintf(" · with %s and %d more", strings.Join(with[:3], ", "), len(with)-3)
	}
	return tail
}

// huddleNoticeText is the centred notice for a huddle nobody replied to:
// "<Starter> started a huddle · lasted 23 min · with A, B". "" when the
// starter is unknown, so the caller keeps the generic notice.
func huddleNoticeText(msg MessageItem, names map[string]string) string {
	id, name := huddleStarterName(msg, names)
	if name == "" {
		return ""
	}
	return name + " started a huddle" + huddleFacts(msg, id, names)
}

// HuddleView is how a huddle announcement is drawn. With replies it is a
// normal message by the huddle's starter, not Slackbot: the copy has the
// starter as author and no subtype, and the body "Started a huddle ·
// lasted 23 min". Without replies, or when the room or the starter's name
// is unknown, msg comes back unchanged (the centred notice). The panes
// render this copy (author line, avatar, colour); the stored item keeps
// its Slackbot author, so edit/delete/react permissions, dedup, unread and
// thread bookkeeping, which read the stored item, are untouched.
func HuddleView(msg MessageItem, names map[string]string) MessageItem {
	if msg.ReplyCount <= 0 {
		return msg
	}
	id, name := huddleStarterName(msg, names)
	if name == "" {
		return msg
	}
	msg.UserID = id
	msg.UserName = name
	msg.BotID = ""
	msg.Subtype = ""
	msg.IsEdited = false
	msg.Blocks = nil
	msg.Text = "Started a huddle" + huddleFacts(msg, id, names)
	return msg
}

// HuddleDuration is how long a huddle lasted: end minus start. It reports
// false unless the room has ended and both dates are known with the end
// after the start; a still-running huddle has no duration yet.
func HuddleDuration(room *HuddleRoom) (time.Duration, bool) {
	if room == nil || !room.HasEnded {
		return 0, false
	}
	if room.DateStart <= 0 || room.DateEnd <= room.DateStart {
		return 0, false
	}
	secs := room.DateEnd - room.DateStart
	const maxSecs = int64(1<<63-1) / int64(time.Second)
	if secs > maxSecs {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}
