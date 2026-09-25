package slackhttp

import (
	"net/url"
	"strings"
	"sync"
)

// TeamAuth pairs a Slack workspace's xoxc token with its 'd' cookie.
// Both are required to authenticate fetches on files.slack.com.
type TeamAuth struct {
	TeamID  string
	Token   string // xoxc-...
	DCookie string
}

// AuthResolver decides which workspace credentials (if any) to attach
// to a files.slack.com request. Shared by the image fetcher and the
// file downloader. Safe for concurrent use.
type AuthResolver struct {
	mu           sync.RWMutex
	authsByTeam  map[string]TeamAuth
	fallbacks    []TeamAuth // ordered; tried in sequence for foreign teams
	learnedAuths map[string]TeamAuth
}

// NewAuthResolver builds a resolver from per-workspace credentials.
// Entries with empty TeamID or Token are skipped. The slice order is
// the fallback order for foreign-team URLs (Slack Connect).
func NewAuthResolver(auths []TeamAuth) *AuthResolver {
	byTeam := make(map[string]TeamAuth, len(auths))
	fallbacks := make([]TeamAuth, 0, len(auths))
	for _, a := range auths {
		if a.TeamID == "" || a.Token == "" {
			continue
		}
		byTeam[a.TeamID] = a
		fallbacks = append(fallbacks, a)
	}
	return &AuthResolver{
		authsByTeam:  byTeam,
		fallbacks:    fallbacks,
		learnedAuths: map[string]TeamAuth{},
	}
}

// AuthsForURL returns the ordered list of auths to try for rawURL.
// Non-Slack URLs return nil (request goes out unauthenticated). For
// files.slack.com: the URL's own team auth if registered, else a
// previously learned auth, else the full ordered fallback list.
func (r *AuthResolver) AuthsForURL(rawURL string) []TeamAuth {
	teamID := TeamIDFromFilesURL(rawURL)
	if teamID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if a, ok := r.authsByTeam[teamID]; ok {
		return []TeamAuth{a}
	}
	if a, ok := r.learnedAuths[teamID]; ok {
		return []TeamAuth{a}
	}
	return r.fallbacks
}

// Learn records that auth worked for teamID, so future AuthsForURL
// calls for that foreign team skip the fallback search. No-op when
// teamID is empty, auth is unusable, or teamID is already registered
// or learned.
func (r *AuthResolver) Learn(teamID string, auth TeamAuth) {
	if teamID == "" || auth.TeamID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.authsByTeam[teamID]; ok {
		return
	}
	if _, ok := r.learnedAuths[teamID]; ok {
		return
	}
	r.learnedAuths[teamID] = auth
}

// TeamIDFromFilesURL extracts the team ID embedded in a Slack file URL.
// Returns "" for URLs that aren't on files.slack.com or don't match a
// recognized path pattern.
//
// The host check uses url.Parse + exact equality rather than substring
// matching: a substring check would accept hostile URLs like
// https://attacker.com/files.slack.com/files-pri/T01ABCDEF/x.png and
// AuthsForURL would then attach the workspace's xoxc Bearer + 'd' cookie
// to the request, leaking the session to the attacker.
func TeamIDFromFilesURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if u.Host != "files.slack.com" {
		return ""
	}
	rest := u.Path
	for _, prefix := range []string{"/files-tmb/", "/files-pri/", "/files/"} {
		if !strings.HasPrefix(rest, prefix) {
			continue
		}
		seg := rest[len(prefix):]
		if j := strings.IndexByte(seg, '/'); j >= 0 {
			seg = seg[:j]
		}
		if prefix == "/files/" {
			return seg
		}
		if j := strings.IndexByte(seg, '-'); j >= 0 {
			return seg[:j]
		}
		return seg
	}
	return ""
}
