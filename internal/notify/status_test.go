package notify

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStatusReporter_RunsWithEnv(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	sr := NewStatusReporter(`printf '%s|%s|%s|%s' "$SLK_UNREAD" "$SLK_OTHER_UNREAD" "$SLK_WORKSPACE" "$SLK_TITLE" >` + out)
	if err := sr.Report(3, 1, "Tone Labs", "slk TL (3) +1"); err != nil {
		t.Fatalf("Report error: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading status_command output: %v", err)
	}
	if want := "3|1|Tone Labs|slk TL (3) +1"; string(got) != want {
		t.Errorf("status_command received %q, want %q", got, want)
	}
}

func TestStatusReporter_EmptyIsNil(t *testing.T) {
	if sr := NewStatusReporter(""); sr != nil {
		t.Fatal("empty command should yield a nil StatusReporter")
	}
}

func TestStatusReporter_NilReportIsNoop(t *testing.T) {
	var sr *StatusReporter // nil
	if err := sr.Report(1, 0, "ws", "title"); err != nil {
		t.Errorf("nil StatusReporter.Report should be a no-op, got %v", err)
	}
}

func TestStatusReporter_NilEnqueueIsNoop(t *testing.T) {
	var sr *StatusReporter          // nil
	sr.Enqueue(1, 0, "ws", "title") // must not panic or block
}

// waitFor polls cond until it returns true or the deadline passes.
func waitFor(t *testing.T, d time.Duration, cond func() bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", desc)
}

// Enqueue during an in-flight run must not spawn an overlapping subprocess;
// pending states coalesce so only the newest runs once the worker is free.
// The first run blocks on a gate file, so the burst enqueued behind it is
// provably concurrent with an in-flight command.
func TestStatusReporter_SerializesAndCoalesces(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	started := filepath.Join(dir, "started")
	gate := filepath.Join(dir, "gate")
	sr := NewStatusReporter(`echo "$SLK_UNREAD" >>` + out +
		`; if [ ! -f ` + gate + ` ]; then touch ` + started +
		`; while [ ! -f ` + gate + ` ]; do sleep 0.01; done; fi`)

	sr.Enqueue(1, 0, "ws", "t")
	waitFor(t, 5*time.Second, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, "first status_command to start")

	// Worker is blocked inside run 1: these must coalesce down to just 9.
	for i := 2; i <= 9; i++ {
		sr.Enqueue(i, 0, "ws", "t")
	}
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatalf("creating gate file: %v", err)
	}

	waitFor(t, 5*time.Second, func() bool {
		body, err := os.ReadFile(out)
		return err == nil && strings.Count(string(body), "\n") >= 2
	}, "coalesced second run to complete")
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if got, want := string(body), "1\n9\n"; got != want {
		t.Errorf("executed states %q, want %q (overlap or missing coalescing)", got, want)
	}
}

// Under a rapid burst the executed states must be an in-order subsequence
// ending in the final state — the out-of-order-completion hazard the worker
// exists to prevent would leave a stale state as the last line.
func TestStatusReporter_BurstConvergesToFinalState(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	sr := NewStatusReporter(`echo "$SLK_UNREAD" >>` + out)
	const last = 50
	for i := 0; i <= last; i++ {
		sr.Enqueue(i, 0, "ws", "t")
	}
	waitFor(t, 5*time.Second, func() bool {
		body, err := os.ReadFile(out)
		if err != nil {
			return false
		}
		lines := strings.Fields(string(body))
		return len(lines) > 0 && lines[len(lines)-1] == "50"
	}, "final state to be reported")

	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	prev := -1
	for _, line := range strings.Fields(string(body)) {
		n, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("non-numeric state line %q", line)
		}
		if n <= prev {
			t.Fatalf("states ran out of order: %d after %d (full: %q)", n, prev, body)
		}
		prev = n
	}
	if prev != last {
		t.Errorf("last executed state = %d, want %d", prev, last)
	}
}

// Workspace/title reach the command via the environment, so they can't inject a
// second shell command.
func TestStatusReporter_NotInjected(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	pwned := filepath.Join(dir, "pwned")
	sr := NewStatusReporter(`printf '%s' "$SLK_WORKSPACE" >` + out)
	if err := sr.Report(1, 0, "; touch "+pwned, "t"); err != nil {
		t.Fatalf("Report error: %v", err)
	}
	if _, err := os.Stat(pwned); !os.IsNotExist(err) {
		t.Error("workspace name was able to inject a shell command")
	}
	got, _ := os.ReadFile(out)
	if want := "; touch " + pwned; string(got) != want {
		t.Errorf("workspace not passed literally: got %q, want %q", got, want)
	}
}
