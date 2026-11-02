package slackhttp

import (
	"net/url"
	"slices"
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
	mu          sync.RWMutex
	authsByTeam map[string]TeamAuth
	fallbacks   []TeamAuth // ordered; tried in sequence for foreign teams
	// learnedFrom maps a foreign team (Slack Connect) to the registered
	// workspace whose auth worked for it. It holds the workspace's team
	// ID, not a copy of its auth, so AuthsForURL always hands out the
	// workspace's current auth: a login refreshed by SetTeamAuth reaches
	// a foreign team at once, even one that a download learns after the
	// refresh with the auth it started with.
	learnedFrom map[string]string
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
		authsByTeam: byTeam,
		fallbacks:   fallbacks,
		learnedFrom: map[string]string{},
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
	if from, ok := r.learnedFrom[teamID]; ok {
		return []TeamAuth{r.authsByTeam[from]}
	}
	return r.fallbacks
}

// Learn records that auth worked for teamID, so future AuthsForURL
// calls for that foreign team skip the fallback search. It records the
// registered workspace auth belongs to (auth.TeamID), not auth itself,
// so the foreign team gets that workspace's current auth (learnedFrom).
// No-op when teamID is empty, auth is unusable or is not a registered
// workspace's, or teamID is already registered or learned.
func (r *AuthResolver) Learn(teamID string, auth TeamAuth) {
	if teamID == "" || auth.TeamID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.authsByTeam[teamID]; ok {
		return
	}
	if _, ok := r.learnedFrom[teamID]; ok {
		return
	}
	if _, ok := r.authsByTeam[auth.TeamID]; !ok {
		return
	}
	r.learnedFrom[teamID] = auth.TeamID
}

// SetTeamAuth replaces a registered workspace's credentials after its
// login was refreshed while slk runs (cmd/slk's applyLogin): for the
// workspace's own files, in the fallback list, and, through
// learnedFrom, for the foreign teams that learned it. It ignores a team
// that is not registered, and an auth with no TeamID or Token, the
// entries NewAuthResolver skips.
func (r *AuthResolver) SetTeamAuth(auth TeamAuth) {
	if auth.TeamID == "" || auth.Token == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.authsByTeam[auth.TeamID]; !ok {
		return
	}
	r.authsByTeam[auth.TeamID] = auth
	// AuthsForURL hands out r.fallbacks itself, and its callers read it
	// after the lock is released, so the list is replaced, never
	// changed in place.
	fallbacks := slices.Clone(r.fallbacks)
	for i := range fallbacks {
		if fallbacks[i].TeamID == auth.TeamID {
			fallbacks[i] = auth
		}
	}
	r.fallbacks = fallbacks
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
