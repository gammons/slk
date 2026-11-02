package slackclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGetStarredConversations_UsesBootConversationIDs(t *testing.T) {
	for _, tc := range []struct {
		name, starred string
		want          []string
	}{
		{"channels", `["C1","C2"]`, []string{"C1", "C2"}},
		{"people and duplicates", `["D2","D1","D2",""]`, []string{"D2", "D1"}},
		{"empty", `[]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/api/client.userBoot" {
					_, _ = w.Write([]byte(`{"ok":false,"error":"enterprise_is_restricted"}`))
					return
				}
				_, _ = w.Write([]byte(`{"ok":true,"starred":` + tc.starred + `}`))
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
