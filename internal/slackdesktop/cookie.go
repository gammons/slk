package slackdesktop

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
)

// sessionCookiePrefix identifies a genuine Slack `d` cookie value. Decryption
// with a wrong key can pass the padding check and yield garbage, so candidate
// passwords/DBs are validated against this prefix.
var sessionCookiePrefix = []byte("xoxd-")

// decryptCookieValue picks the right algorithm for the OS and the value's
// version prefix, then tries each candidate password until one yields a real
// session cookie. getPasswords is the injected keychain/keyring/DPAPI source
// (unused for the linux v10 "peanuts" case).
func decryptCookieValue(goos string, enc []byte, getPasswords func() ([][]byte, error)) ([]byte, error) {
	if len(enc) < 3 {
		return nil, ErrDecryptFailed
	}
	version := string(enc[:3])
	body := enc[3:]

	var withPassword func([]byte) ([]byte, error)
	switch goos {
	case "windows":
		withPassword = func(pw []byte) ([]byte, error) { return decryptGCM(body, pw) }
	case "darwin":
		withPassword = func(pw []byte) ([]byte, error) { return decryptCBC(body, pw, 1003) }
	default: // linux
		if version == "v10" {
			return validateCookie(decryptCBC(body, []byte("peanuts"), 1))
		}
		withPassword = func(pw []byte) ([]byte, error) { return decryptCBC(body, pw, 1) }
	}

	passwords, err := getPasswords()
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, pw := range passwords {
		val, err := validateCookie(withPassword(pw))
		if err == nil {
			return val, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrDecryptFailed
}

func validateCookie(val []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(val, sessionCookiePrefix) {
		return nil, fmt.Errorf("%w: value is not a session cookie", ErrDecryptFailed)
	}
	return val, nil
}

// cookieDBCandidates lists the cookie DB locations under a Slack config dir,
// in preference order. Modern Chromium/Electron keeps the DB under Network/;
// older Slack builds kept it at the profile root. When both exist, the
// top-level file is a stale leftover from before the migration, so the
// Network/ location wins.
func cookieDBCandidates(dir string) []string {
	return []string{
		filepath.Join(dir, "Network", "Cookies"),
		filepath.Join(dir, "Cookies"),
	}
}

// cookieFromDir reads and decrypts the Slack desktop `d` cookie from the
// first candidate DB that yields a valid session cookie.
func cookieFromDir(dir, goos string, getPasswords func() ([][]byte, error)) (string, error) {
	lastErr := ErrCookieDBMissing
	for _, dbPath := range cookieDBCandidates(dir) {
		plain, enc, err := readCookieRow(dbPath)
		if errors.Is(err, ErrCookieDBMissing) {
			continue
		}
		if err != nil {
			return "", err
		}
		if plain != "" {
			return plain, nil
		}
		val, err := decryptCookieValue(goos, enc, getPasswords)
		if err != nil {
			// Undecryptable DB (e.g. a stale file encrypted under a rotated
			// key): fall through to the next candidate.
			lastErr = err
			continue
		}
		return string(val), nil
	}
	return "", lastErr
}

// Cookie reads and decrypts the Slack desktop `d` cookie.
func Cookie() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return cookieFromDir(dir, runtime.GOOS, keyringPasswords)
}
