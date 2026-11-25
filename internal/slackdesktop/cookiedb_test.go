package slackdesktop

import (
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

func makeCookieDB(t *testing.T, plain string, enc []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Cookies")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cookies VALUES (?,?,?,?)`, ".slack.com", "d", plain, enc); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadCookieRow(t *testing.T) {
	path := makeCookieDB(t, "", []byte{0x01, 0x02, 0x03})
	plain, enc, err := readCookieRow(path)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "" {
		t.Errorf("plain = %q, want empty", plain)
	}
	if len(enc) != 3 {
		t.Errorf("enc len = %d, want 3", len(enc))
	}
}

func TestCopyToTempWithLockedCookie(t *testing.T) {
	_, err := copyToTemp("/Slack/Network/Cookie", func(err error) bool { return true })

	if err == nil || err != ErrCookieLocked {
		t.Errorf("error = %q, expected = %q", err, ErrCookieLocked)
	}
}

// #146: macOS TCC lets stat succeed on the App Store build's container
// but fails open with EPERM. That must not read as "never signed in".
func TestReadCookieRowUnreadableIsNotMissing(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	path := makeCookieDB(t, "", []byte{1, 2, 3})
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	_, _, err := readCookieRow(path)
	if !errors.Is(err, ErrCookieDBUnreadable) {
		t.Fatalf("error = %v, want ErrCookieDBUnreadable", err)
	}
	if errors.Is(err, ErrCookieDBMissing) {
		t.Errorf("error = %v must not also be ErrCookieDBMissing", err)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("error = %v should keep the underlying permission error", err)
	}
}

func TestCopyToTempMissingFile(t *testing.T) {
	_, err := copyToTemp(filepath.Join(t.TempDir(), "Cookies"), func(error) bool { return false })
	if !errors.Is(err, ErrCookieDBMissing) {
		t.Errorf("error = %v, want ErrCookieDBMissing", err)
	}
}
