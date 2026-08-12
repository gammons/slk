package edge

import "testing"

func TestHealth(t *testing.T) {
	h := NewHealth()
	if h.Degraded() {
		t.Error("a new Health is degraded; degradation must be earned by an observed wholesale failure")
	}
	h.MarkDegraded()
	h.MarkDegraded() // idempotent
	if !h.Degraded() {
		t.Error("MarkDegraded did not stick")
	}
}
