package messages

import (
	"math"
	"testing"
	"time"
)

func TestHuddleStarter(t *testing.T) {
	if got := HuddleStarter(MessageItem{}); got != "" {
		t.Errorf("nil room: %q", got)
	}
	if got := HuddleStarter(MessageItem{HuddleRoom: &HuddleRoom{}}); got != "" {
		t.Errorf("no creator: %q", got)
	}
	if got := HuddleStarter(MessageItem{HuddleRoom: &HuddleRoom{CreatedBy: "UFAKE0001"}}); got != "UFAKE0001" {
		t.Errorf("got %q", got)
	}
}

func TestHuddleDuration(t *testing.T) {
	cases := []struct {
		name string
		room *HuddleRoom
		want time.Duration
		ok   bool
	}{
		{"nil", nil, 0, false},
		{"not ended", &HuddleRoom{DateStart: 100, DateEnd: 200}, 0, false},
		{"end equals start", &HuddleRoom{HasEnded: true, DateStart: 100, DateEnd: 100}, 0, false},
		{"end before start", &HuddleRoom{HasEnded: true, DateStart: 200, DateEnd: 100}, 0, false},
		{"zero start", &HuddleRoom{HasEnded: true, DateStart: 0, DateEnd: 100}, 0, false},
		{"negative start", &HuddleRoom{HasEnded: true, DateStart: -5, DateEnd: 100}, 0, false},
		{"zero end", &HuddleRoom{HasEnded: true, DateStart: 100}, 0, false},
		{"sub-minute", &HuddleRoom{HasEnded: true, DateStart: 100, DateEnd: 145}, 45 * time.Second, true},
		{"hours", &HuddleRoom{HasEnded: true, DateStart: 1700000000, DateEnd: 1700007200}, 2 * time.Hour, true},
		{"overflow", &HuddleRoom{HasEnded: true, DateStart: 1, DateEnd: math.MaxInt64}, 0, false},
	}
	for _, c := range cases {
		got, ok := HuddleDuration(c.room)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %v,%v want %v,%v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestSetHuddleRoom(t *testing.T) {
	m := New([]MessageItem{{TS: "1.0"}, {TS: "2.0"}}, "general")
	room := &HuddleRoom{CreatedBy: "UFAKE0001"}
	if !m.SetHuddleRoom("2.0", room) {
		t.Fatal("not found")
	}
	if m.messages[1].HuddleRoom != room || m.messages[0].HuddleRoom != nil {
		t.Error("room set on the wrong message")
	}
	if m.SetHuddleRoom("9.0", room) {
		t.Error("found a missing message")
	}
}
