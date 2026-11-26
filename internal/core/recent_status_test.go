package core

import (
	"testing"
	"time"
)

func TestRecentStatusExpiry(t *testing.T) {
	now := time.Unix(5_000_000, 0)
	for _, tc := range []struct {
		name string
		r    RecentStatus
		want int64
	}{
		{"one hour", RecentStatus{Created: 1000, Expire: 4600}, 5_003_600},
		{"never cleared", RecentStatus{Created: 1000}, 0},
		{"no created date", RecentStatus{Expire: 4600}, 0},
		{"expire before created", RecentStatus{Created: 4600, Expire: 1000}, 0},
	} {
		if got := tc.r.Expiry(now); got != tc.want {
			t.Errorf("%s: Expiry = %d, want %d", tc.name, got, tc.want)
		}
	}
}
