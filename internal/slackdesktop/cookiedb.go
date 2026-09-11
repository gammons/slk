package slackdesktop

import (
	"database/sql"
	"errors"
	"io"
	"os"

	_ "modernc.org/sqlite"
)

const cookieQuery = `SELECT value, encrypted_value FROM cookies WHERE host_key=".slack.com" AND name="d"`

// readCookieRow returns the plaintext value (usually empty) and the encrypted
// value blob for the Slack `d` cookie.
//
// Slack keeps the Cookies DB open (often in WAL mode) while running, so we copy
// it to a temp file and read the copy. This avoids taking a lock on — or
// writing -wal/-shm side-effect files into — Slack's live profile directory,
// and sidesteps cross-platform read-only DSN quirks. The `d` cookie is written
// at login and long-since checkpointed, so the main-file snapshot is complete.
func readCookieRow(dbPath string) (string, []byte, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return "", nil, ErrCookieDBMissing
	}

	tmp, err := copyToTemp(dbPath, IsFileLockError)
	if err != nil {
		return "", nil, err
	}
	defer os.Remove(tmp)

	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		return "", nil, err
	}
	defer db.Close()

	var plain string
	var enc []byte
	if err := db.QueryRow(cookieQuery).Scan(&plain, &enc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// DB exists but has no `d` cookie: installed, never signed in.
			return "", nil, ErrCookieDBMissing
		}
		return "", nil, err
	}
	return plain, enc, nil
}

// copyToTemp copies src to a fresh temp file and returns its path. The caller
// is responsible for removing it.
func copyToTemp(src string, isLockError func(error) bool) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		// In Windows, a running Slack process locks the Cookie file,
		// preventing access to other processes. If the Cookie file is locked,
		// inform the user to close Slack and try again.
		if isLockError(err) {
			return "", ErrCookieLocked
		}
		return "", ErrCookieDBMissing
	}
	defer in.Close()

	f, err := os.CreateTemp("", "slk-cookies-*.db")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
