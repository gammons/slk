package slackdesktop

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCookieDBCandidatesPrefersNetworkDir(t *testing.T) {
	got := cookieDBCandidates("/cfg")
	want := []string{
		filepath.Join("/cfg", "Network", "Cookies"),
		filepath.Join("/cfg", "Cookies"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func makeCookieDBAt(t *testing.T, path, plain string, enc []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
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
}

func TestCookieFromDirPrefersNetworkCookies(t *testing.T) {
	dir := t.TempDir()
	makeCookieDBAt(t, filepath.Join(dir, "Cookies"), "", append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-stale"), []byte("old-pw"), 1003)...))
	makeCookieDBAt(t, filepath.Join(dir, "Network", "Cookies"), "", append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-current"), []byte("new-pw"), 1003)...))
	getPasswords := func() ([][]byte, error) {
		return [][]byte{[]byte("old-pw"), []byte("new-pw")}, nil
	}
	got, err := cookieFromDir(dir, "darwin", getPasswords)
	if err != nil {
		t.Fatal(err)
	}
	if got != "xoxd-current" {
		t.Errorf("got %q, want %q", got, "xoxd-current")
	}
}

func TestCookieFromDirFallsBackToLegacyPath(t *testing.T) {
	dir := t.TempDir()
	makeCookieDBAt(t, filepath.Join(dir, "Cookies"), "", append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-legacy"), []byte("pw"), 1003)...))
	got, err := cookieFromDir(dir, "darwin", singlePassword("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "xoxd-legacy" {
		t.Errorf("got %q, want %q", got, "xoxd-legacy")
	}
}

func TestCookieFromDirTriesNextDBWhenDecryptFails(t *testing.T) {
	dir := t.TempDir()
	makeCookieDBAt(t, filepath.Join(dir, "Network", "Cookies"), "", append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-unknown"), []byte("unknown-pw"), 1003)...))
	makeCookieDBAt(t, filepath.Join(dir, "Cookies"), "", append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-legacy"), []byte("pw"), 1003)...))
	got, err := cookieFromDir(dir, "darwin", singlePassword("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "xoxd-legacy" {
		t.Errorf("got %q, want %q", got, "xoxd-legacy")
	}
}

func TestCookieFromDirMissing(t *testing.T) {
	if _, err := cookieFromDir(t.TempDir(), "darwin", singlePassword("pw")); !errors.Is(err, ErrCookieDBMissing) {
		t.Errorf("err = %v, want ErrCookieDBMissing", err)
	}
}
