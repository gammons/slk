package slackclient

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("xoxp-test", "xapp-test")
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.TeamID() != "" {
		t.Error("expected empty team ID before connecting")
	}
}
