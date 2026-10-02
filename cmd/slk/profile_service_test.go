package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/slack-go/slack"
)

func TestToCoreProfile_MapsFields(t *testing.T) {
	u := &slack.User{
		ID:       "U1",
		TeamID:   "T1",
		Name:     "grant",
		RealName: "Grant Miller",
		TZ:       "America/Los_Angeles",
		TZOffset: -25200,
		IsBot:    false,
		Deleted:  true,
		Profile: slack.UserProfile{
			DisplayName: "gmiller",
			Title:       "Engineer",
			Pronouns:    "he/him",
			Email:       "grant@example.com",
			Phone:       "555-1234",
		},
	}
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	got := toCoreProfile(u, at)

	want := core.UserProfile{
		UserID:      "U1",
		TeamID:      "T1",
		Handle:      "grant",
		RealName:    "Grant Miller",
		DisplayName: "gmiller",
		Title:       "Engineer",
		Pronouns:    "he/him",
		Email:       "grant@example.com",
		Phone:       "555-1234",
		TZ:          "America/Los_Angeles",
		TZAbbrev:    "PDT",
		TZOffset:    -25200,
		IsBot:       false,
		Deleted:     true,
	}
	if got != want {
		t.Errorf("toCoreProfile() = %+v, want %+v", got, want)
	}
}

func TestToCoreProfile_IsBotFromIsAppUser(t *testing.T) {
	u := &slack.User{ID: "U2", IsAppUser: true}
	got := toCoreProfile(u, time.Now())
	if !got.IsBot {
		t.Error("IsAppUser should map to IsBot=true")
	}
}

func TestZoneAbbrev(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		tz   string
		want string
	}{
		{"America/Los_Angeles", "PDT"},
		{"America/Sao_Paulo", ""},
		{"Not/AZone", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := zoneAbbrev(c.tz, at); got != c.want {
			t.Errorf("zoneAbbrev(%q, %v) = %q, want %q", c.tz, at, got, c.want)
		}
	}
}

func TestProfileCache_HitWithinTTL(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
		calls++
		return core.UserProfile{UserID: userID, TeamID: teamID}, nil
	}
	now := time.Now()
	c := newProfileCache(5*time.Minute, func() time.Time { return now }, fetch)

	if _, err := c.Profile(context.Background(), "T1", "U1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Profile(context.Background(), "T1", "U1"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestProfileCache_ExpiresAfterTTL(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
		calls++
		return core.UserProfile{UserID: userID, TeamID: teamID}, nil
	}
	now := time.Now()
	c := newProfileCache(5*time.Minute, func() time.Time { return now }, fetch)

	if _, err := c.Profile(context.Background(), "T1", "U1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5*time.Minute + time.Second)
	if _, err := c.Profile(context.Background(), "T1", "U1"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestProfileCache_ErrorsNotCached(t *testing.T) {
	calls := 0
	boom := errors.New("boom")
	fetch := func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
		calls++
		return core.UserProfile{}, boom
	}
	now := time.Now()
	c := newProfileCache(5*time.Minute, func() time.Time { return now }, fetch)

	if _, err := c.Profile(context.Background(), "T1", "U1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if _, err := c.Profile(context.Background(), "T1", "U1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (errors not cached)", calls)
	}
}

func TestProfileCache_KeyedByTeam(t *testing.T) {
	calls := 0
	fetch := func(ctx context.Context, teamID, userID string) (core.UserProfile, error) {
		calls++
		return core.UserProfile{UserID: userID, TeamID: teamID}, nil
	}
	now := time.Now()
	c := newProfileCache(5*time.Minute, func() time.Time { return now }, fetch)

	if _, err := c.Profile(context.Background(), "T1", "U1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Profile(context.Background(), "T2", "U1"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (keyed by team)", calls)
	}
}

func TestProfileFetch_RateLimitWrapped(t *testing.T) {
	lookup := func(teamID string) (func(context.Context, string) (*slack.User, error), bool) {
		return func(ctx context.Context, userID string) (*slack.User, error) {
			return nil, &slack.RateLimitedError{RetryAfter: 30 * time.Second}
		}, true
	}
	fetch := newProfileFetch(lookup)
	_, err := fetch(context.Background(), "T1", "U1")

	var rl *core.RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("errors.As did not find *core.RateLimitedError; err = %v", err)
	}
	if rl.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", rl.RetryAfter)
	}
}

func TestProfileFetch_UnknownWorkspace(t *testing.T) {
	lookup := func(teamID string) (func(context.Context, string) (*slack.User, error), bool) {
		return nil, false
	}
	fetch := newProfileFetch(lookup)
	_, err := fetch(context.Background(), "T9", "U1")

	if err == nil || !strings.Contains(err.Error(), `workspace "T9" is unavailable`) {
		t.Fatalf("err = %v, want to contain workspace \"T9\" is unavailable", err)
	}
}

func TestProfileFetch_DeadlineExceededPassesThrough(t *testing.T) {
	lookup := func(teamID string) (func(context.Context, string) (*slack.User, error), bool) {
		return func(ctx context.Context, userID string) (*slack.User, error) {
			return nil, context.DeadlineExceeded
		}, true
	}
	fetch := newProfileFetch(lookup)
	_, err := fetch(context.Background(), "T1", "U1")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false; err = %v", err)
	}
}
