package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/slack-go/slack"
)

// toCoreProfile maps a slack-go User (returned by users.info) to the
// core.UserProfile the profile dialog renders. at is the time used to
// resolve the user's timezone abbreviation (America/Los_Angeles at a
// given instant may be PST or PDT).
func toCoreProfile(u *slack.User, at time.Time) core.UserProfile {
	teamID := u.TeamID
	return core.UserProfile{
		UserID:      u.ID,
		TeamID:      teamID,
		Handle:      u.Name,
		RealName:    u.RealName,
		DisplayName: u.Profile.DisplayName,
		Title:       u.Profile.Title,
		Pronouns:    u.Profile.Pronouns,
		Email:       u.Profile.Email,
		Phone:       u.Profile.Phone,
		TZ:          u.TZ,
		TZAbbrev:    zoneAbbrev(u.TZ, at),
		TZOffset:    u.TZOffset,
		IsBot:       u.IsBot || u.IsAppUser,
		Deleted:     u.Deleted,
	}
}

// zoneAbbrev returns the timezone abbreviation (e.g. "PDT") for tz at
// the instant at, or "" when tz is empty, fails to load, or resolves to
// a numeric offset (e.g. "-03") rather than a named abbreviation.
func zoneAbbrev(tz string, at time.Time) string {
	if tz == "" {
		return ""
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return ""
	}
	abbrev, _ := at.In(loc).Zone()
	if abbrev == "" {
		return ""
	}
	switch abbrev[0] {
	case '+', '-':
		return ""
	}
	if abbrev[0] >= '0' && abbrev[0] <= '9' {
		return ""
	}
	return abbrev
}

// profileCacheEntry holds a cached fetch result and the time it was
// fetched.
type profileCacheEntry struct {
	profile   core.UserProfile
	fetchedAt time.Time
}

// profileCache is a simple TTL cache in front of a fetch function,
// keyed by "teamID/userID". Errors are never cached, per the design
// (a transient failure shouldn't lock the user out of retrying for
// the whole TTL window). now is injectable for tests.
type profileCache struct {
	ttl   time.Duration
	now   func() time.Time
	fetch func(ctx context.Context, teamID, userID string) (core.UserProfile, error)

	mu      sync.Mutex
	entries map[string]profileCacheEntry
}

// newProfileCache builds a profileCache wrapping fetch with a ttl TTL,
// using now to read the current time.
func newProfileCache(ttl time.Duration, now func() time.Time, fetch func(ctx context.Context, teamID, userID string) (core.UserProfile, error)) *profileCache {
	return &profileCache{
		ttl:     ttl,
		now:     now,
		fetch:   fetch,
		entries: make(map[string]profileCacheEntry),
	}
}

// Profile returns userID's profile in workspace teamID, from cache if
// fresh, otherwise from a live fetch. Successful fetches are cached
// for ttl; errors are not cached.
func (c *profileCache) Profile(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
	key := teamID + "/" + userID

	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()
	if ok && c.now().Sub(entry.fetchedAt) < c.ttl {
		return entry.profile, nil
	}

	profile, err := c.fetch(ctx, teamID, userID)
	if err != nil {
		return core.UserProfile{}, err
	}

	c.mu.Lock()
	c.entries[key] = profileCacheEntry{profile: profile, fetchedAt: c.now()}
	c.mu.Unlock()

	return profile, nil
}

// newProfileFetch builds the per-workspace profile fetch function used
// by ProfileService, from a lookup that resolves a workspace's
// GetUserInfoContext-shaped function by team ID. Separated from the
// live *workspaceRouter so it can be tested without a live *slack.Client.
func newProfileFetch(lookup func(teamID string) (getUser func(ctx context.Context, userID string) (*slack.User, error), ok bool)) func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
	return func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
		getUser, ok := lookup(teamID)
		if !ok {
			return core.UserProfile{}, fmt.Errorf("workspace %q is unavailable", teamID)
		}

		u, err := getUser(ctx, userID)
		if err != nil {
			var rlErr *slack.RateLimitedError
			if errors.As(err, &rlErr) {
				return core.UserProfile{}, &core.RateLimitedError{RetryAfter: rlErr.RetryAfter}
			}
			return core.UserProfile{}, fmt.Errorf("fetching profile: %w", err)
		}

		profile := toCoreProfile(u, time.Now())
		if u.TeamID == "" {
			profile.TeamID = teamID
		}
		return profile, nil
	}
}
