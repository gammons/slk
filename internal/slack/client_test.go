package slackclient

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("xoxc-test", "test-cookie-value")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.TeamID() != "" {
		t.Error("expected empty team ID before connecting")
	}
}
