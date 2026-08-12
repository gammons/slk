package slackhttp

import (
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestEnvelope_PreBootIdentity(t *testing.T) {
	e := NewEnvelope()
	// Before SetTeamID, _x_id uses the "noversion-" prefix and there is
	// no session id — matching the official client's pre-boot requests.
	id := e.RequestID()
	if !regexp.MustCompile(`^noversion-\d+\.\d{3}$`).MatchString(id) {
		t.Errorf("RequestID() = %q; want noversion-<unix>.<millis>", id)
	}
	if got := e.SessionID(); got != "" {
		t.Errorf("SessionID() = %q; want empty pre-boot", got)
	}
	if got := e.TeamID(); got != "" {
		t.Errorf("TeamID() = %q; want empty pre-boot", got)
	}
}

func TestEnvelope_PostBootIdentity(t *testing.T) {
	e := NewEnvelope()
	e.SetTeamID("T04T4TH8W")

	if got := e.TeamID(); got != "T04T4TH8W" {
		t.Errorf("TeamID() = %q; want T04T4TH8W", got)
	}
	id := e.RequestID()
	if !regexp.MustCompile(`^[0-9a-f]{8}-\d+\.\d{3}$`).MatchString(id) {
		t.Errorf("RequestID() = %q; want <8-hex>-<unix>.<millis> post-boot", id)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`).MatchString(e.SessionID()) {
		t.Errorf("SessionID() = %q; want an 11-char session id post-boot", e.SessionID())
	}
	// Client and session ids are stable across calls within a process.
	//nolint:staticcheck // SA4000: the repeated call is the point — it asserts
	// SessionID is a stored value, not one regenerated per call.
	if e.SessionID() != e.SessionID() {
		t.Error("SessionID() is not stable across calls")
	}
	a, b := e.RequestID(), e.RequestID()
	if a[:9] != b[:9] {
		t.Errorf("RequestID() client-id prefix changed: %q vs %q", a, b)
	}
}

func TestEnvelope_RequestIDFormat(t *testing.T) {
	// _x_id is deliberately NOT unique — see RequestID's doc comment.
	// What matters is the shape: <prefix>-<unix-seconds>.<millis>, with
	// exactly three digits of millisecond precision.
	e := NewEnvelope()
	e.SetTeamID("T04T4TH8W")

	re := regexp.MustCompile(`^([0-9a-f]{8})-(\d{10})\.(\d{3})$`)
	for i := 0; i < 20; i++ {
		id := e.RequestID()
		m := re.FindStringSubmatch(id)
		if m == nil {
			t.Fatalf("RequestID() = %q; want <8-hex>-<10-digit-unix>.<3-digit-millis>", id)
		}
		if m[1] != e.clientID {
			t.Errorf("RequestID() client id = %q; want %q (must be stable)", m[1], e.clientID)
		}
	}

	// The timestamp must actually track the clock, not be frozen at
	// construction — a constant _x_id would be its own signature.
	first := e.RequestID()
	time.Sleep(3 * time.Millisecond)
	if second := e.RequestID(); first == second {
		t.Errorf("RequestID() returned %q twice across a 3ms gap; timestamp is not advancing", first)
	}
}

func TestEnvelope_VersionTSDefaultAndOverride(t *testing.T) {
	e := NewEnvelope()
	if e.VersionTS() != DefaultVersionTS {
		t.Errorf("VersionTS() = %q; want default %q", e.VersionTS(), DefaultVersionTS)
	}
	e.SetVersionTS("1785403654")
	if e.VersionTS() != "1785403654" {
		t.Errorf("VersionTS() = %q; want 1785403654", e.VersionTS())
	}
	// Empty values must not clobber a good one — a failed lookup should
	// leave the previous value in place.
	e.SetVersionTS("")
	if e.VersionTS() != "1785403654" {
		t.Errorf("SetVersionTS(\"\") clobbered the value: %q", e.VersionTS())
	}
}

func TestEnvelope_SetTeamIDIgnoresEmpty(t *testing.T) {
	e := NewEnvelope()
	e.SetTeamID("T04T4TH8W")
	e.SetTeamID("")
	if got := e.TeamID(); got != "T04T4TH8W" {
		t.Errorf("SetTeamID(\"\") clobbered the value: %q", got)
	}
}

func TestEnvelope_ConcurrentAccess(t *testing.T) {
	e := NewEnvelope()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.SetTeamID("T04T4TH8W")
			e.SetVersionTS("1785403654")
			_ = e.RequestID()
			_ = e.TeamID()
			_ = e.SessionID()
			_ = e.VersionTS()
			_, _ = e.TraceIDs()
		}()
	}
	wg.Wait()
}

func TestEnvelope_TraceIDs(t *testing.T) {
	e := NewEnvelope()
	trace, span := e.TraceIDs()
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(trace) {
		t.Errorf("trace id = %q; want 32 hex chars", trace)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(span) {
		t.Errorf("span id = %q; want 16 hex chars", span)
	}
	t2, s2 := e.TraceIDs()
	if trace == t2 || span == s2 {
		t.Error("TraceIDs() must return fresh ids per call")
	}
}
