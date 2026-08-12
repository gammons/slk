package edge

import "sync/atomic"

// Health records whether edge resolution is working for one workspace
// this session. Two states — unknown and degraded — and deliberately
// nothing else: it exists so that a workspace where edge resolution
// fails wholesale (Enterprise Grid today: the enterprise-id group
// resolves nothing, the foreign teams are Unauthenticated) pays for
// that discovery once per boot instead of once per call site.
//
// Session-scoped by construction. Persisting pessimism would risk
// suppressing the real Grid-scoping fix when it lands, and a cold
// boot re-discovers degradation for the cost of a handful of calls.
type Health struct {
	degraded atomic.Bool
}

// NewHealth returns a Health in the unknown (not degraded) state.
func NewHealth() *Health { return &Health{} }

// MarkDegraded latches the degraded state. Idempotent; there is no
// path back within a session.
func (h *Health) MarkDegraded() { h.degraded.Store(true) }

// Degraded reports whether edge resolution has failed wholesale this
// session. Nil-safe: a nil *Health reads as not degraded, so callers
// wired optionally need no guard.
func (h *Health) Degraded() bool { return h != nil && h.degraded.Load() }
