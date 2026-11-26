package slackclient

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/core"
)

// The shape users.customStatus.list returned when probed; values invented.
const recentStatusesBody = `{"ok":true,
 "statuses":[
  {"id":"S1","text":"On a break","emoji":":coffee:","duration":"1_hour","date_created":1700000000,"date_expire":1700003600,"is_dnd":false,"is_active":true},
  {"id":"S2","text":"","emoji":":house:","duration":"custom","date_created":1690000000,"date_expire":0,"is_dnd":false,"is_active":false},
  {"id":"S3","text":"","emoji":"","duration":"custom","date_created":1680000000,"date_expire":0}
 ],
 "scheduled_statuses":[],"custom_sections":[]}`

func TestParseRecentStatuses(t *testing.T) {
	got, err := parseRecentStatuses([]byte(recentStatusesBody))
	if err != nil {
		t.Fatal(err)
	}
	want := []core.RecentStatus{
		{Text: "On a break", Emoji: ":coffee:", Created: 1700000000, Expire: 1700003600},
		{Emoji: ":house:", Created: 1690000000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseRecentStatusesErrors(t *testing.T) {
	for name, body := range map[string]string{
		"not ok":    `{"ok":false,"error":"unknown_method"}`,
		"malformed": `{"ok":true,"statuses":[{"text":1}]}`,
		"not json":  `<html>`,
	} {
		if got, err := parseRecentStatuses([]byte(body)); err == nil {
			t.Errorf("%s: got %+v and no error", name, got)
		}
	}
}
