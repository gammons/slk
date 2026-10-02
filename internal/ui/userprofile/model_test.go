package userprofile

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gammons/slk/internal/core"
)

func TestOpenCloseVisibility(t *testing.T) {
	m := New()
	if m.IsVisible() {
		t.Fatal("new model should not be visible")
	}
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	if !m.IsVisible() {
		t.Fatal("Open should make the model visible")
	}
	if team, user := m.Target(); team != "T1" || user != "U1" {
		t.Errorf("Target() = %q, %q; want T1, U1", team, user)
	}
	m.Close()
	if m.IsVisible() {
		t.Fatal("Close should hide the model")
	}
}

func TestSetProfile_TargetMismatch(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})

	if m.SetProfile("T1", "U2", core.UserProfile{UserID: "U2"}) {
		t.Fatal("SetProfile with the wrong user should return false")
	}
	got := m.ViewOverlay(80, 24, "", Live{Now: time.Now()})
	if !containsAll(got, "Loading profile\u2026") {
		t.Errorf("mismatched SetProfile must not change the view: %s", got)
	}

	m.Close()
	if m.SetProfile("T1", "U1", core.UserProfile{UserID: "U1"}) {
		t.Fatal("SetProfile after Close should return false")
	}
}

func TestSetError_TargetMismatch(t *testing.T) {
	m := New()
	m.Open(Seed{TeamID: "T1", UserID: "U1", DisplayName: "Priya"})
	if m.SetError("T2", "U1", errors.New("boom")) {
		t.Fatal("SetError with the wrong team should return false")
	}
	m.Close()
	if m.SetError("T1", "U1", errors.New("boom")) {
		t.Fatal("SetError after Close should return false")
	}
}

func TestErrorLine(t *testing.T) {
	deadlineErr := fmt.Errorf("fetching profile: %w", context.DeadlineExceeded)
	rateLimited := &core.RateLimitedError{RetryAfter: 30 * time.Second}
	plain := errors.New("boom")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"timeout", deadlineErr, "Couldn't load full profile: timed out"},
		{"rate limited", rateLimited, "Rate limited \u2014 try again in 30s"},
		{"generic", plain, "Couldn't load full profile: boom"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errorLine(c.err); got != c.want {
				t.Errorf("errorLine(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}
