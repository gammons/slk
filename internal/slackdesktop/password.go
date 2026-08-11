package slackdesktop

// This file holds macOS keychain lookup logic in platform-agnostic form so it
// can be unit-tested on any OS; the darwin build wires it to /usr/bin/security
// (see password_darwin.go).

// darwinKeychainQueries are the macOS keychain lookups for Slack's Safe
// Storage item, in preference order. Slack uses different account names per
// build: "Slack Key" for the direct-download Electron build, "Slack App Store
// Key" for the Mac App Store build, and "Slack" (or other names) on older
// installs. A service-only lookup would return an arbitrary match — possibly
// a stale item from a previous install, whose password fails to decrypt the
// current cookie — so known accounts are queried explicitly first and the
// service-only lookup is kept last as a catch-all.
var darwinKeychainQueries = []struct {
	service string
	account string
}{
	{"Slack Safe Storage", "Slack Key"},
	{"Slack Safe Storage", "Slack App Store Key"},
	{"Slack Safe Storage", ""},
}

// collectDarwinPasswords runs each keychain query and returns every distinct
// password found, in query order. ErrNoSecretService is returned when no
// query yields a password.
func collectDarwinPasswords(lookup func(service, account string) ([]byte, error)) ([][]byte, error) {
	var passwords [][]byte
	seen := map[string]bool{}
	for _, q := range darwinKeychainQueries {
		pw, err := lookup(q.service, q.account)
		if err != nil {
			continue
		}
		if s := string(pw); !seen[s] {
			seen[s] = true
			passwords = append(passwords, pw)
		}
	}
	if len(passwords) == 0 {
		return nil, ErrNoSecretService
	}
	return passwords, nil
}
