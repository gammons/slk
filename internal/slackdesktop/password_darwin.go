//go:build darwin

package slackdesktop

import (
	"bytes"
	"os/exec"
	"strings"
)

const safeStorageService = "Slack Safe Storage"

// keyringPasswords fetches every "Slack Safe Storage" password from the macOS
// login keychain by shelling out to /usr/bin/security.
//
// We deliberately shell out rather than use a Security.framework binding: the
// release build sets CGO_ENABLED=0 (see .goreleaser.yaml) and cross-compiles
// darwin from Linux, so a cgo keychain dependency would break the macOS build.
//
// A machine that has run more than one Slack build carries more than one item
// under this service name: the sandboxed App Store build stores its key under
// account "Slack App Store Key", the standalone build under its own. The two
// are not interchangeable — each encrypts only its own profile's cookie DB —
// and `security find-generic-password -s <svc>` returns an arbitrary match. So
// enumerate the accounts and return every key, most likely first, and let
// decryptCookieValue settle which one actually belongs to the profile.
func keyringPasswords() ([][]byte, error) {
	accounts := orderAccountsForProfile(keychainAccounts(safeStorageService), sandboxedProfile())

	var out [][]byte
	seen := map[string]bool{}
	for _, acct := range accounts {
		pw, err := keychainPassword(safeStorageService, acct)
		if err != nil || pw == "" || seen[pw] {
			continue
		}
		seen[pw] = true
		out = append(out, []byte(pw))
	}
	if len(out) > 0 {
		return out, nil
	}

	// Enumeration told us nothing — dump-keychain unavailable, or output in a
	// shape we do not recognise. Fall back to the service-only lookup, which is
	// all slk ever did before and is correct whenever only one item exists.
	pw, err := keychainPassword(safeStorageService, "")
	if err != nil || pw == "" {
		return nil, ErrNoSecretService
	}
	return [][]byte{[]byte(pw)}, nil
}

// keychainPassword reads one secret. `security find-generic-password -w` prints
// just the password on stdout. An empty account means "match on service alone".
func keychainPassword(service, account string) (string, error) {
	args := []string{"find-generic-password", "-w", "-s", service}
	if account != "" {
		args = append(args, "-a", account)
	}
	cmd := exec.Command("/usr/bin/security", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

// keychainAccounts lists the account names of every generic-password item with
// the given service. `security dump-keychain` prints attributes only — never
// secret material — so this needs no unlock and raises no authorization prompt.
func keychainAccounts(service string) []string {
	cmd := exec.Command("/usr/bin/security", "dump-keychain")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	return parseKeychainAccounts(out.String(), service)
}

// parseKeychainAccounts scans `security dump-keychain` output for items whose
// "svce" attribute equals service and returns their "acct" values. Items are
// introduced by a "keychain:" line, and within one item attributes are printed
// alphabetically, so "acct" always precedes "svce".
func parseKeychainAccounts(dump, service string) []string {
	var out []string
	acct := ""
	for line := range strings.SplitSeq(dump, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "keychain:"):
			acct = ""
		case strings.HasPrefix(t, `"acct"`):
			acct = keychainAttrValue(t)
		case strings.HasPrefix(t, `"svce"`):
			if acct != "" && keychainAttrValue(t) == service {
				out = append(out, acct)
			}
		}
	}
	return out
}

// keychainAttrValue pulls the quoted value out of a dump-keychain attribute
// line. Values come either bare — `"acct"<blob>="Slack Key"` — or hex-first
// with the text alongside — `"acct"<blob>=0x536C61636B  "Slack"`; both end in
// the quoted form. `<NULL>` and non-attribute lines yield "".
func keychainAttrValue(line string) string {
	eq := strings.Index(line, "=")
	if eq < 0 {
		return ""
	}
	v := line[eq+1:]
	first := strings.Index(v, `"`)
	last := strings.LastIndex(v, `"`)
	if first < 0 || last <= first {
		return ""
	}
	return v[first+1 : last]
}

// sandboxedProfile reports whether the profile we are about to read belongs to
// the App Store build, whose data lives under ~/Library/Containers.
func sandboxedProfile() bool {
	dir, err := ConfigDir()
	if err != nil {
		return false
	}
	return strings.Contains(dir, "/Library/Containers/")
}

// orderAccountsForProfile puts the account most likely to match the profile
// first, so the common case needs a single keychain read — each read can raise
// an authorization prompt when the item's ACL omits /usr/bin/security.
func orderAccountsForProfile(accounts []string, sandboxed bool) []string {
	match := make([]string, 0, len(accounts))
	rest := make([]string, 0, len(accounts))
	for _, a := range accounts {
		if strings.Contains(a, "App Store") == sandboxed {
			match = append(match, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(match, rest...)
}
