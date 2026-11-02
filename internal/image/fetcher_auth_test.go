package image

import "testing"

// TestFetcherSetTeamAuthReachesItsResolver: a workspace's refreshed login
// must reach the credentials the fetcher attaches to that workspace's
// files.
func TestFetcherSetTeamAuthReachesItsResolver(t *testing.T) {
	f := NewFetcher(nil, nil)
	f.SetAuths([]TeamAuth{{TeamID: "T1", Token: "xoxc-1", DCookie: "d1"}})

	fresh := TeamAuth{TeamID: "T1", Token: "xoxc-1b", DCookie: "d1b"}
	f.SetTeamAuth(fresh)

	got := f.resolver.AuthsForURL("https://files.slack.com/files-pri/T1-F/x.png")
	if len(got) != 1 || got[0] != fresh {
		t.Errorf("AuthsForURL after SetTeamAuth = %#v, want [%#v]", got, fresh)
	}
}
