package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestProfileService_NilFuncUnsupported(t *testing.T) {
	svc := NewProfileService(ProfileServiceFuncs{})
	got, err := svc.Profile(context.Background(), "T1", "U1")
	if !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Profile error = %v, want errors.ErrUnsupported", err)
	}
	if got != (UserProfile{}) {
		t.Errorf("Profile result = %+v, want zero value", got)
	}
}

func TestProfileService_Delegates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	want := UserProfile{
		UserID:      "U1",
		TeamID:      "T1",
		Handle:      "priya",
		RealName:    "Priya Raman",
		DisplayName: "Priya",
		Title:       "Staff Engineer, Platform",
		Pronouns:    "she/her",
		Email:       "priya@example.com",
		Phone:       "+1 555 0100",
		TZ:          "America/Los_Angeles",
		TZAbbrev:    "PDT",
		TZOffset:    -25200,
		IsBot:       false,
		Deleted:     false,
	}
	wantErr := errors.New("boom")

	var gotCtx context.Context
	var gotTeamID, gotUserID string
	fn := func(c context.Context, teamID, userID string) (UserProfile, error) {
		gotCtx = c
		gotTeamID = teamID
		gotUserID = userID
		return want, wantErr
	}

	svc := NewProfileService(ProfileServiceFuncs{Profile: fn})
	got, err := svc.Profile(ctx, "T1", "U1")

	if gotCtx != ctx {
		t.Errorf("Profile did not pass through ctx")
	}
	if gotTeamID != "T1" || gotUserID != "U1" {
		t.Errorf("Profile args = (%q, %q), want (%q, %q)", gotTeamID, gotUserID, "T1", "U1")
	}
	if got != want {
		t.Errorf("Profile result = %+v, want %+v", got, want)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Profile error = %v, want %v", err, wantErr)
	}
}

func TestRateLimitedError_As(t *testing.T) {
	inner := &RateLimitedError{RetryAfter: 30 * time.Second}
	wrapped := fmt.Errorf("fetching profile: %w", inner)

	var target *RateLimitedError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As did not find *RateLimitedError through fmt.Errorf wrapping")
	}
	if target.RetryAfter != 30*time.Second {
		t.Errorf("RetryAfter = %v, want 30s", target.RetryAfter)
	}
	if got, want := inner.Error(), "rate limited, retry after 30s"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
