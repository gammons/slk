package notify

import (
	"os"
	"os/exec"
	"strconv"

	"github.com/gammons/slk/internal/debuglog"
)

// statusState is one snapshot of the unread state exposed to status_command.
type statusState struct {
	unread      int
	otherUnread int
	mentions    int
	workspace   string
	title       string
}

// StatusReporter runs a user-configured status_command whenever slk's unread
// state changes, exposing that state through environment variables so an
// external surface (a status bar, tmux, a terminal multiplexer's sidebar) can
// reflect it.
//
// Executions are serialized through a single worker goroutine with
// newest-wins coalescing: Enqueue never blocks, and when states change faster
// than the command runs, intermediate states are dropped. Overlapping
// subprocesses could otherwise finish out of order under a burst (bulk
// catch-up, a flurry of messages) and pin the external surface to a stale
// count — each state fully describes the world, so only the newest one
// matters.
type StatusReporter struct {
	command string
	// latest is a capacity-1 mailbox: Enqueue evicts any pending state, and
	// the worker always runs the newest.
	latest chan statusState
}

// NewStatusReporter returns a StatusReporter with its worker started, or nil
// when command is empty so callers can skip wiring it. Enqueue and Report are
// nil-safe, so a nil result is usable. The worker lives for the life of the
// process, matching slk's other per-config background goroutines.
func NewStatusReporter(command string) *StatusReporter {
	if command == "" {
		return nil
	}
	r := &StatusReporter{
		command: command,
		latest:  make(chan statusState, 1),
	}
	go r.run()
	return r
}

// Enqueue hands the worker a new unread state and returns immediately, so it
// is safe to call from the UI goroutine. A still-pending state is replaced
// rather than queued behind. Nil-safe (no-op).
func (r *StatusReporter) Enqueue(unread, otherUnread, mentions int, workspace, title string) {
	if r == nil {
		return
	}
	s := statusState{unread: unread, otherUnread: otherUnread, mentions: mentions, workspace: workspace, title: title}
	for {
		select {
		case r.latest <- s:
			return
		default:
		}
		// Mailbox full: evict the stale pending state and retry. The worker
		// may drain it first, so this receive must not block either.
		select {
		case <-r.latest:
		default:
		}
	}
}

// run executes states one at a time, so a slow command can never overlap —
// and finish after — the run for a newer state. Failures are surfaced via
// debuglog ([notify]) rather than dropped, since nothing else observes them.
func (r *StatusReporter) run() {
	for s := range r.latest {
		if err := r.Report(s.unread, s.otherUnread, s.mentions, s.workspace, s.title); err != nil {
			debuglog.Notify("status_command failed: %v", err)
		}
	}
}

// Report runs the status_command synchronously with the current unread state
// exposed as $SLK_UNREAD, $SLK_OTHER_UNREAD, $SLK_MENTIONS, $SLK_WORKSPACE and
// $SLK_TITLE.
// Values are passed through the environment rather than interpolated into the
// command, so a workspace name or title can't inject shell syntax. Nil-safe
// (no-op). Production callers should go through Enqueue, which adds the
// serialization and coalescing described on StatusReporter.
func (r *StatusReporter) Report(unread, otherUnread, mentions int, workspace, title string) error {
	if r == nil {
		return nil
	}
	cmd := exec.Command("sh", "-c", r.command)
	cmd.Env = append(os.Environ(),
		"SLK_UNREAD="+strconv.Itoa(unread),
		"SLK_OTHER_UNREAD="+strconv.Itoa(otherUnread),
		"SLK_MENTIONS="+strconv.Itoa(mentions),
		"SLK_WORKSPACE="+workspace,
		"SLK_TITLE="+title,
	)
	return cmd.Run()
}
