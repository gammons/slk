package slackhttp

import "testing"

func TestTeamIDFromFilesURL(t *testing.T) {
	cases := []struct{ name, url, want string }{
		{"files-pri canonical", "https://files.slack.com/files-pri/T01ABCDEF-F0123/foo.png", "T01ABCDEF"},
		{"files-tmb canonical", "https://files.slack.com/files-tmb/T01ABCDEF-F0123/foo_360.png", "T01ABCDEF"},
		{"files canonical", "https://files.slack.com/files/T01ABCDEF/foo.png", "T01ABCDEF"},
		{"query string", "https://files.slack.com/files-pri/T01ABCDEF-F0123/foo.png?t=abc", "T01ABCDEF"},
		{"spoof host suffix", "https://attacker.com/files.slack.com/files-pri/T01ABCDEF-F/x.png", ""},
		{"other host", "https://example.com/files-pri/T01ABCDEF-F/x.png", ""},
	}
	for _, c := range cases {
		if got := TeamIDFromFilesURL(c.url); got != c.want {
			t.Errorf("%s: TeamIDFromFilesURL(%q) = %q, want %q", c.name, c.url, got, c.want)
		}
	}
}

func TestAuthResolverKnownTeam(t *testing.T) {
	r := NewAuthResolver([]TeamAuth{
		{TeamID: "T1", Token: "xoxc-1", DCookie: "d1"},
		{TeamID: "T2", Token: "xoxc-2", DCookie: "d2"},
	})
	got := r.AuthsForURL("https://files.slack.com/files-pri/T2-F/doc.pdf")
	if len(got) != 1 || got[0].TeamID != "T2" {
		t.Fatalf("AuthsForURL = %#v", got)
	}
}

func TestAuthResolverForeignTeamFallbackAndLearn(t *testing.T) {
	r := NewAuthResolver([]TeamAuth{
		{TeamID: "T1", Token: "xoxc-1", DCookie: "d1"},
		{TeamID: "T2", Token: "xoxc-2", DCookie: "d2"},
	})
	foreign := "https://files.slack.com/files-pri/T9-F/doc.pdf"
	if got := r.AuthsForURL(foreign); len(got) != 2 {
		t.Fatalf("expected 2 fallbacks, got %#v", got)
	}
	r.Learn("T9", TeamAuth{TeamID: "T2", Token: "xoxc-2", DCookie: "d2"})
	got := r.AuthsForURL(foreign)
	if len(got) != 1 || got[0].TeamID != "T2" {
		t.Fatalf("after Learn, AuthsForURL = %#v", got)
	}
	// Learn must not overwrite a registered team.
	r.Learn("T1", TeamAuth{TeamID: "T2", Token: "xoxc-2"})
	if got := r.AuthsForURL("https://files.slack.com/files-pri/T1-F/x"); len(got) != 1 || got[0].TeamID != "T1" {
		t.Fatalf("Learn overwrote registered team: %#v", got)
	}
}

func TestAuthResolverSkipsEmptyAndNonSlack(t *testing.T) {
	r := NewAuthResolver([]TeamAuth{{TeamID: "", Token: "x"}, {TeamID: "T1", Token: ""}})
	if got := r.AuthsForURL("https://files.slack.com/files-pri/T1-F/x"); len(got) != 0 {
		t.Fatalf("expected no auths, got %#v", got)
	}
	if got := r.AuthsForURL("https://example.com/x.png"); got != nil {
		t.Fatalf("non-Slack URL should get nil auths, got %#v", got)
	}
}

// TestTeamIDFromFilesURL_HostCheck verifies that team-ID extraction
// requires the URL host to be exactly files.slack.com. A previous
// implementation used strings.Contains, which let hostile URLs that
// merely embedded "files.slack.com/files-pri/..." in their path or
// query trigger auth attachment, leaking the workspace's xoxc Bearer
// and 'd' cookie to the attacker.
func TestTeamIDFromFilesURL_HostCheck(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		// Legitimate Slack file URLs — must keep working.
		{
			name: "files-pri canonical",
			url:  "https://files.slack.com/files-pri/T01ABCDEF-F0123/foo.png",
			want: "T01ABCDEF",
		},
		{
			name: "files-tmb canonical",
			url:  "https://files.slack.com/files-tmb/T01ABCDEF-F0123/foo_360.png",
			want: "T01ABCDEF",
		},
		{
			name: "files canonical (no team-suffix split)",
			url:  "https://files.slack.com/files/T01ABCDEF/foo.png",
			want: "T01ABCDEF",
		},
		{
			name: "with query string",
			url:  "https://files.slack.com/files-pri/T01ABCDEF-F0123/foo.png?t=abc",
			want: "T01ABCDEF",
		},

		// Spoofing vectors — must NOT extract a team ID.
		{
			name: "attacker host with files.slack.com in path",
			url:  "https://attacker.com/files.slack.com/files-pri/T01ABCDEF/x.png",
			want: "",
		},
		{
			name: "attacker host with files.slack.com in query",
			url:  "https://attacker.com/x?u=https://files.slack.com/files-pri/T01ABCDEF/x.png",
			want: "",
		},
		{
			name: "subdomain spoof",
			url:  "https://files.slack.com.attacker.com/files-pri/T01ABCDEF/x.png",
			want: "",
		},
		{
			name: "userinfo spoof",
			url:  "https://files.slack.com@attacker.com/files-pri/T01ABCDEF/x.png",
			want: "",
		},

		// Non-matches that should remain non-matches.
		{name: "empty", url: "", want: ""},
		{name: "garbage", url: "::not a url::", want: ""},
		{name: "unrelated host", url: "https://example.com/files-pri/T01ABCDEF/x.png", want: ""},
		{name: "slack.com root, not files.", url: "https://slack.com/files-pri/T01ABCDEF/x.png", want: ""},
		{name: "files.slack.com but unknown path prefix", url: "https://files.slack.com/api/foo", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TeamIDFromFilesURL(tc.url)
			if got != tc.want {
				t.Errorf("TeamIDFromFilesURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}
