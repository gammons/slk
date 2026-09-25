package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/slackdesktop"
)

// A bare "file an issue" message threw away the one thing that identifies which
// decryption step failed, turning a diagnosable fault into a support round-trip.
func TestDesktopErrorMessageKeepsDecryptDetail(t *testing.T) {
	err := fmt.Errorf("%w: decrypted value is not printable text (wrong Safe Storage key?)", slackdesktop.ErrDecryptFailed)

	got := desktopErrorMessage(err)
	if !strings.Contains(got, "not printable text") {
		t.Errorf("message = %q, want it to carry the wrapped detail", got)
	}
}

// The other branches are plain advice with nothing wrapped to surface; they must
// stay untouched.
func TestDesktopErrorMessageKnownCauses(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{slackdesktop.ErrDesktopNotFound, "Slack desktop app not found"},
		{slackdesktop.ErrNotSignedIn, "No Slack workspaces are signed in"},
		{slackdesktop.ErrCookieDBMissing, "never signed in"},
		{slackdesktop.ErrKeyringLocked, "keyring is locked"},
		{slackdesktop.ErrNoSecretService, "No system keyring"},
		{errors.New("boom"), "boom"},
	}
	for _, tc := range tests {
		if got := desktopErrorMessage(tc.err); !strings.Contains(got, tc.want) {
			t.Errorf("desktopErrorMessage(%v) = %q, want it to contain %q", tc.err, got, tc.want)
		}
	}
}
