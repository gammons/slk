package messages

import "time"

// HuddleStarter returns the user ID that started the huddle a huddle_thread
// message announces, or "" when the message carries no room or the room
// names no creator.
func HuddleStarter(msg MessageItem) string {
	if msg.HuddleRoom == nil {
		return ""
	}
	return msg.HuddleRoom.CreatedBy
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
