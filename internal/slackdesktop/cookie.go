package slackdesktop

import (
	"fmt"
	"path/filepath"
	"runtime"
)

// looksLikeCookieValue reports whether b is a plausible decrypted cookie:
// non-empty and printable ASCII.
//
// decryptCBC's only integrity check is PKCS#7 padding, and that is weak — the
// final plaintext byte merely has to land in 1..16, so a wrong Safe Storage key
// survives it about one time in sixteen and yields binary garbage. Sent on as a
// `d` cookie, net/http silently drops the invalid bytes and Slack answers
// invalid_auth, which points nowhere near the real fault. Requiring printable
// ASCII closes that gap: garbage survives with probability (95/256)^len.
//
// We deliberately do not require an "xoxd-" prefix — that would break the day
// Slack renames the cookie — nor a domain-hash prefix, which only exists from
// Chromium 130 / cookie-DB version 24 onward and would break older installs.
func looksLikeCookieValue(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// passwordSource yields every Safe Storage key candidate for this machine, in
// preference order. A macOS login keychain can hold several items under the
// same "Slack Safe Storage" service — one per Slack build ever installed — and
// only trying them tells us which one belongs to the profile we are reading.
type passwordSource func() ([][]byte, error)

// decryptCookieValue picks the right algorithm for the OS and the value's
// version prefix, then tries each candidate key until one yields a plausible
// cookie. getPasswords is the injected keyring/keychain/DPAPI source; it is not
// consulted at all for the linux v10 "peanuts" case.
func decryptCookieValue(goos string, enc []byte, getPasswords passwordSource) ([]byte, error) {
	if len(enc) < 3 {
		return nil, ErrDecryptFailed
	}
	version := string(enc[:3])
	body := enc[3:]

	switch goos {
	case "windows", "darwin":
		// Always keyed: DPAPI on Windows, keychain on macOS.
	default: // linux and others
		if version == "v10" {
			return checked(decryptCBC(body, []byte("peanuts"), 1))
		}
	}

	pws, err := getPasswords()
	if err != nil {
		return nil, err
	}
	if len(pws) == 0 {
		return nil, ErrNoSecretService
	}

	var lastErr error
	for _, pw := range pws {
		out, err := checked(decryptWithKey(goos, body, pw))
		if err != nil {
			lastErr = err
			continue
		}
		return out, nil
	}
	return nil, lastErr
}

func decryptWithKey(goos string, body, pw []byte) ([]byte, error) {
	switch goos {
	case "windows":
		return decryptGCM(body, pw)
	case "darwin":
		return decryptCBC(body, pw, 1003)
	default: // linux and others
		return decryptCBC(body, pw, 1)
	}
}

// checked rejects a decryption that "succeeded" but produced something no
// cookie value could be. See looksLikeCookieValue for why padding alone is not
// enough of a guarantee.
func checked(out []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if !looksLikeCookieValue(out) {
		return nil, fmt.Errorf("%w: decrypted value is not printable text (wrong Safe Storage key?)", ErrDecryptFailed)
	}
	return out, nil
}

// Cookie reads and decrypts the Slack desktop `d` cookie.
func Cookie() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	dbPath := filepath.Join(dir, "Cookies")
	if runtime.GOOS == "windows" {
		dbPath = filepath.Join(dir, "Network", "Cookies")
	}
	plain, enc, err := readCookieRow(dbPath)
	if err != nil {
		return "", err
	}
	if plain != "" {
		return plain, nil
	}
	val, err := decryptCookieValue(runtime.GOOS, enc, keyringPasswords)
	if err != nil {
		return "", err
	}
	return string(val), nil
}
