package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetStarredConversations_OnlyPeopleAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name, items string
		want        []string
	}{
		{"people only", `[{"type":"im","channel":"D2"},{"type":"im","channel":"D1"},{"type":"im","channel":"D2"}]`, []string{"D2", "D1"}},
		{"empty list", `[]`, nil},
		{"empty IDs", `[{"type":"im"},{"type":"channel","channel":""}]`, nil},
		{"non-conversations", `[{"type":"message","channel":"D1"},{"type":"file","channel":"D2"},{"type":"file_comment","channel":"D3"},{"type":"mpim","channel":"D4"}]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"items":` + tc.items + `}`))
			}))
			defer srv.Close()
			c := &Client{token: "xoxc-test", apiBaseURL: srv.URL + "/api/"}
			got, err := c.GetStarredConversations(context.Background())
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("IDs = %v, err = %v, want %v", got, err, tc.want)
			}
		})
	}
}
