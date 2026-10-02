// Package userprofile is the read-only "who is this person?" modal
// opened by K over the selected message's author. It holds the seed
// identity from the UI's own cache, the fetch result (or error) once
// it arrives, and renders itself as a centered lipgloss box over a
// dimmed backdrop, the same way the other 13 modal packages do.
//
// The box chrome (RoundedBorder + overlay.DimmedOverlay) is written
// directly here rather than through a shared package, because none
// exists yet: AGENTS.md tracks 11 near-duplicate renderBox
// implementations awaiting a Phase 4 consolidation. This modal moves
// onto that shared chrome when it lands and should add no further
// renderBox/visibleWindow copy in the meantime.
package userprofile

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/peerstatus"
)

// Seed is the cached identity the UI already knows about a user before
// the profile fetch completes: enough to open the dialog and show a
// name immediately.
type Seed struct {
	TeamID, UserID, DisplayName string
	IsExternal                  bool
}

// Live carries the render-time inputs that can change while the modal
// is open without the model copying them: the rendered avatar, presence,
// custom-status/DND/huddle state, and the clock used for "local time"
// and status-expiry math.
type Live struct {
	Avatar   string // "" = not loaded
	Presence string // "active" | "away" | "" (unknown -> omitted)
	Status   peerstatus.Status
	Now      time.Time // its zone offset is "you"
}

// fetchState is the modal's own small state machine, reset by Open.
type fetchState int

const (
	stateLoading fetchState = iota
	stateLoaded
	stateFailed
)

// Model is the user-profile modal's state.
type Model struct {
	visible bool
	seed    Seed
	state   fetchState
	profile core.UserProfile
	err     error
	// lastLive is the Live the most recent frame was drawn with.
	// BoxSize and ClickAt lay the box out again from it so their
	// geometry matches what is on screen (status rows above the
	// details move the email row).
	lastLive Live
}

// New creates an empty, hidden modal.
func New() *Model {
	return &Model{}
}

// Open shows the modal for seed's user and resets any previous fetch
// result: the modal always starts in the loading state.
func (m *Model) Open(s Seed) {
	m.seed = s
	m.state = stateLoading
	m.profile = core.UserProfile{}
	m.err = nil
	m.visible = true
}

// Close hides the modal and clears its state.
func (m *Model) Close() {
	*m = Model{}
}

// Email returns the loaded profile's email, "" until the fetch has
// succeeded or when the user has none.
func (m *Model) Email() string {
	if m.state != stateLoaded {
		return ""
	}
	return m.profile.Email
}

// IsVisible reports whether the modal is showing.
func (m *Model) IsVisible() bool { return m.visible }

// Target returns the team and user ID the modal was opened for, so the
// caller can match a fetch result against it before applying.
func (m *Model) Target() (teamID, userID string) {
	return m.seed.TeamID, m.seed.UserID
}

// SetProfile applies a successful fetch. It returns false, applying
// nothing, when the modal is closed or targets a different team/user
// than teamID/userID (a stale result from a fetch the user has since
// moved on from).
func (m *Model) SetProfile(teamID, userID string, p core.UserProfile) bool {
	if !m.visible || teamID != m.seed.TeamID || userID != m.seed.UserID {
		return false
	}
	m.profile = p
	m.state = stateLoaded
	m.err = nil
	return true
}

// SetError applies a failed fetch, with the same staleness guard as
// SetProfile.
func (m *Model) SetError(teamID, userID string, err error) bool {
	if !m.visible || teamID != m.seed.TeamID || userID != m.seed.UserID {
		return false
	}
	m.err = err
	m.state = stateFailed
	return true
}

// errorLine renders the one-line reason shown in the details area after
// a failed fetch. context.DeadlineExceeded (wrapped, since the fetch
// command runs under a bounded context) reports as "timed out";
// *core.RateLimitedError gets its own copy with the retry delay rounded
// to whole seconds; everything else reports err.Error() verbatim.
func errorLine(err error) string {
	var rl *core.RateLimitedError
	if errors.As(err, &rl) {
		secs := int(rl.RetryAfter.Round(time.Second) / time.Second)
		return "Rate limited \u2014 try again in " + strconv.Itoa(secs) + "s"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Couldn't load full profile: timed out"
	}
	return "Couldn't load full profile: " + err.Error()
}
