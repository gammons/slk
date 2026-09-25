//go:build darwin

package slackdesktop

import (
	"slices"
	"testing"
)

// Real `security dump-keychain` output, trimmed to the attributes we read. Note
// the two Slack items sharing one service name and differing only by account —
// the situation that made a service-only lookup return the wrong key.
const dumpKeychainSample = `keychain: "/Users/x/Library/Keychains/login.keychain-db"
class: "genp"
attributes:
    "acct"<blob>="Slack App Store Key"
    "desc"<blob>=<NULL>
    "svce"<blob>="Slack Safe Storage"
keychain: "/Users/x/Library/Keychains/login.keychain-db"
class: "genp"
attributes:
    "acct"<blob>="Slack Key"
    "svce"<blob>="Slack Safe Storage"
keychain: "/Users/x/Library/Keychains/login.keychain-db"
class: "genp"
attributes:
    "acct"<blob>="Chrome"
    "svce"<blob>="Chrome Safe Storage"
`

func TestParseKeychainAccounts(t *testing.T) {
	got := parseKeychainAccounts(dumpKeychainSample, "Slack Safe Storage")
	want := []string{"Slack App Store Key", "Slack Key"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseKeychainAccountsIgnoresOtherServices(t *testing.T) {
	if got := parseKeychainAccounts(dumpKeychainSample, "Chrome Safe Storage"); !slices.Equal(got, []string{"Chrome"}) {
		t.Errorf("got %q, want [Chrome]", got)
	}
}

func TestKeychainAttrValue(t *testing.T) {
	tests := []struct {
		name, line, want string
	}{
		{"plain blob", `"acct"<blob>="Slack Key"`, "Slack Key"},
		{"hex then quoted", `"acct"<blob>=0x536C61636B  "Slack"`, "Slack"},
		{"null", `"desc"<blob>=<NULL>`, ""},
		{"no equals", `attributes:`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := keychainAttrValue(tc.line); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Each keychain read can raise an authorization prompt, so the candidate that
// matches the profile we are reading must come first.
func TestOrderAccountsForProfile(t *testing.T) {
	accounts := []string{"Slack App Store Key", "Slack Key"}

	if got := orderAccountsForProfile(accounts, false); !slices.Equal(got, []string{"Slack Key", "Slack App Store Key"}) {
		t.Errorf("standalone profile: got %q, want the non-App-Store key first", got)
	}
	if got := orderAccountsForProfile(accounts, true); !slices.Equal(got, []string{"Slack App Store Key", "Slack Key"}) {
		t.Errorf("sandboxed profile: got %q, want the App Store key first", got)
	}
}
