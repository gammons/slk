// internal/ui/bootstrap.go
//
// Multi-workspace startup overlay: tracks per-workspace connection
// status, decides when the overlay should dismiss, and renders the
// centered "Connecting to ..." box.
//
// Phase 2c of the SOLID refactor of internal/ui/app.go: extracts the
// `loading`, `loadingStates`, and `bootstrapActiveClaimed` fields plus
// the SetLoadingWorkspaces / MarkWorkspaceReady / MarkWorkspaceFailed /
// checkLoadingDone / renderLoadingOverlay quintet out of App.
//
// Dismissal policy (preserved verbatim from the old checkLoadingDone):
//   - As soon as ANY workspace reaches "ready", the overlay dismisses
//     (other workspaces keep connecting in the background).
//   - If none are ready and none are still connecting (all failed),
//     the overlay also dismisses.
//   - While at least one workspace is still connecting, the overlay
//     stays visible.
//
// The shared spinner-frame counter (`spinnerFrame`) is NOT owned here;
// it lives on App because it's also consumed by the messages pane's
// load spinner. Render takes the resolved glyph as an argument so this
// file has no dependency on styles.SpinnerChars.
package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gammons/slk/internal/ui/styles"
)

// loadingEntry is one workspace's row in the startup overlay.
type loadingEntry struct {
	TeamName string
	Status   string // "connecting", "ready", "failed"
}

// workspaceBootstrap owns the startup-overlay state machine.
type workspaceBootstrap struct {
	loading              bool
	states               []loadingEntry
	initialActiveClaimed bool
}

func newWorkspaceBootstrap() *workspaceBootstrap {
	return &workspaceBootstrap{}
}

// IsLoading reports whether the overlay is currently visible (used by
// Update arms to gate user input and by View to decide composition).
func (b *workspaceBootstrap) IsLoading() bool {
	return b.loading
}

// SetWorkspaces seeds the overlay with one "connecting" entry per
// workspace name and turns the overlay on. Called at program start
// from cmd/slk/main.go before any Slack connection is attempted.
func (b *workspaceBootstrap) SetWorkspaces(names []string) {
	b.loading = true
	b.states = nil
	for _, name := range names {
		b.states = append(b.states, loadingEntry{
			TeamName: name,
			Status:   "connecting",
		})
	}
}

// MarkReady flips the named workspace's status to "ready". Unknown
// names are a no-op (defensive: race-free re-entry from late
// WorkspaceReadyMsg paths). Triggers checkDone.
func (b *workspaceBootstrap) MarkReady(teamName string) {
	for i := range b.states {
		if b.states[i].TeamName == teamName {
			b.states[i].Status = "ready"
			break
		}
	}
	b.checkDone()
}

// MarkFailed flips the named workspace's status to "failed". Unknown
// names are a no-op. Triggers checkDone.
func (b *workspaceBootstrap) MarkFailed(teamName string) {
	for i := range b.states {
		if b.states[i].TeamName == teamName {
			b.states[i].Status = "failed"
			break
		}
	}
	b.checkDone()
}

// TimeoutPendingAsFailed flips any still-"connecting" entries to
// "failed" and dismisses the overlay unconditionally. Called from the
// LoadingTimeoutMsg arm (overlay has been up too long; surrender).
func (b *workspaceBootstrap) TimeoutPendingAsFailed() {
	if !b.loading {
		return
	}
	for i := range b.states {
		if b.states[i].Status == "connecting" {
			b.states[i].Status = "failed"
		}
	}
	b.loading = false
}

// ClaimInitialActive returns true exactly once: the first call where
// the caller is claiming the "initial active workspace" role. Defensive
// guard against any future bug delivering InitialActive=true twice.
func (b *workspaceBootstrap) ClaimInitialActive() bool {
	if b.initialActiveClaimed {
		return false
	}
	b.initialActiveClaimed = true
	return true
}

// checkDone applies the dismissal policy. See package comment.
func (b *workspaceBootstrap) checkDone() {
	// Dismiss as soon as at least one workspace is ready (others
	// continue connecting in the background).
	for _, e := range b.states {
		if e.Status == "ready" {
			b.loading = false
			return
		}
	}
	// If none ready, check if any are still connecting.
	for _, e := range b.states {
		if e.Status == "connecting" {
			return
		}
	}
	// All failed (and none ready): dismiss anyway.
	b.loading = false
}

// spinnerTickCmd schedules the next SpinnerTickMsg 100ms out.
// Used by the SpinnerTickMsg arm to keep the chain alive while
// either the bootstrap overlay or the messages pane is loading.
func spinnerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return SpinnerTickMsg{}
	})
}

// Handle is the bootstrap-family reducer for App.Update (Phase 4e).
// Owns SpinnerTickMsg (advance the shared spinner frame + reschedule
// while loading), LoadingTimeoutMsg (force-dismiss the overlay) and
// WorkspaceFailedMsg (flip a single workspace's status to failed).
// Returns (nil, false) for any other message type.
//
// WorkspaceReadyMsg deliberately does NOT route through here even
// though it calls bootstrap.MarkReady / ClaimInitialActive: that
// arm is a 60-line workspace-activation orchestrator (sidebar,
// threadsView, messagepane, threadPanel, compose, channelFinder,
// statusbar, workspaceRail, themes, presence, channels) whose
// bootstrap-related side effects are a small fraction of what it
// does. It belongs in reducer_workspace.go (Phase 4k).
//
// The shared spinner frame (a.spinnerFrame, a.messagepane spinner)
// is touched via `a` because it's also driven by messages-pane
// loading state, not just by the bootstrap overlay.
func (b *workspaceBootstrap) Handle(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case SpinnerTickMsg:
		_ = m
		if !(b.IsLoading() || a.anyWinModelLoading()) {
			// No loader is active anymore (overlay or any window's
			// model); let the chain die. The gate must consider
			// EVERY window: a backfill marks the then-focused model
			// loading, and if focus moves before completion a
			// focused-only gate would kill the chain and freeze the
			// other window's glyph.
			return nil, true
		}
		a.spinnerFrame = (a.spinnerFrame + 1) % len(styles.SpinnerChars)
		for _, mp := range a.allWinModels() {
			// Only loading models show the spinner; pushing the frame
			// into a non-loading model would bump its version 10x/sec
			// and force pointless rebuilds of its (live, cached)
			// unfocused pane. A model that starts loading later picks
			// up the current frame on the next tick.
			if mp.IsLoading() {
				mp.SetSpinnerFrame(a.spinnerFrame)
			}
		}
		return spinnerTickCmd(), true

	case LoadingTimeoutMsg:
		_ = m
		b.TimeoutPendingAsFailed()
		return nil, true

	case WorkspaceFailedMsg:
		b.MarkFailed(m.TeamName)
		return nil, true
	}
	return nil, false
}

// Render builds the centered overlay box for the given canvas size.
// spinnerGlyph is the single rune (as a string) used in the
// "Connecting to ..." rows; the caller resolves it from a shared
// spinner-frame counter so the same animation cadence is used for
// both this overlay and the messages-pane spinner.
func (b *workspaceBootstrap) Render(width, height int, spinnerGlyph string) string {
	var rows []string

	for _, entry := range b.states {
		switch entry.Status {
		case "ready":
			rows = append(rows, lipgloss.NewStyle().Foreground(styles.Accent).Render("✓")+" "+entry.TeamName)
		case "failed":
			rows = append(rows, lipgloss.NewStyle().Foreground(styles.Error).Render("✗")+" "+entry.TeamName+" (failed)")
		default:
			rows = append(rows, lipgloss.NewStyle().Foreground(styles.Primary).Render(spinnerGlyph)+" Connecting to "+entry.TeamName+"...")
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(width, height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(styles.SplashBackground)),
	)
}
