//go:build darwin

package slackdesktop

import (
	"bytes"
	"os/exec"
	"strings"
)

// keyringPasswords collects every "Slack Safe Storage" password from the
// macOS login keychain by shelling out to /usr/bin/security. Slack stores the
// item under different account names depending on the build (see
// darwinKeychainQueries), and a machine that has seen multiple Slack installs
// can hold several items under the same service name; the caller tries each
// candidate and keeps the one that decrypts a real session cookie.
//
// We deliberately shell out rather than use a Security.framework binding: the
// release build sets CGO_ENABLED=0 (see .goreleaser.yaml) and cross-compiles
// darwin from Linux, so a cgo keychain dependency would break the macOS build.
// `security find-generic-password -w` prints just the password on stdout.
func keyringPasswords() ([][]byte, error) {
	return collectDarwinPasswords(func(service, account string) ([]byte, error) {
		args := []string{"find-generic-password", "-w", "-s", service}
		if account != "" {
			args = append(args, "-a", account)
		}
		cmd := exec.Command("/usr/bin/security", args...)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		pw := strings.TrimRight(out.String(), "\r\n")
		if pw == "" {
			return nil, ErrNoSecretService
		}
		return []byte(pw), nil
	})
}
