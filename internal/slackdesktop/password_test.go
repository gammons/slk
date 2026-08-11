package slackdesktop

import (
	"errors"
	"fmt"
	"testing"
)

func TestCollectDarwinPasswordsQueriesKnownAccounts(t *testing.T) {
	lookup := func(service, account string) ([]byte, error) {
		switch account {
		case "Slack Key":
			return []byte("direct-download-pw"), nil
		case "Slack App Store Key":
			return nil, fmt.Errorf("not found")
		case "":
			return []byte("legacy-pw"), nil
		}
		return nil, fmt.Errorf("unexpected account %q", account)
	}
	got, err := collectDarwinPasswords(lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"direct-download-pw", "legacy-pw"}
	if len(got) != len(want) {
		t.Fatalf("got %d passwords %q, want %q", len(got), got, want)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Errorf("password %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCollectDarwinPasswordsDedupes(t *testing.T) {
	lookup := func(service, account string) ([]byte, error) {
		return []byte("same-pw"), nil
	}
	got, err := collectDarwinPasswords(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d passwords, want 1 (deduped)", len(got))
	}
}

func TestCollectDarwinPasswordsNoneFound(t *testing.T) {
	lookup := func(service, account string) ([]byte, error) {
		return nil, fmt.Errorf("not found")
	}
	if _, err := collectDarwinPasswords(lookup); !errors.Is(err, ErrNoSecretService) {
		t.Errorf("err = %v, want ErrNoSecretService", err)
	}
}
