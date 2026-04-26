package slackclient

import (
	"testing"
	"time"
)

func TestBackoffSequence(t *testing.T) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	expected := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		30 * time.Second,
		30 * time.Second,
	}

	for i, want := range expected {
		if backoff != want {
			t.Errorf("step %d: expected %v, got %v", i, want, backoff)
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

func TestBackoffReset(t *testing.T) {
	backoff := 16 * time.Second
	backoff = 1 * time.Second
	if backoff != 1*time.Second {
		t.Errorf("expected 1s after reset, got %v", backoff)
	}
}
