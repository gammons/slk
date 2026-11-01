package slackclient

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTokenStore_SaveIf: a login is saved only over the entry the caller
// read. A replacement or a removal that another process made meanwhile
// is kept, and so is an entry that cannot be read.
func TestTokenStore_SaveIf(t *testing.T) {
	read := Token{TeamID: "T1", TeamName: "Acme", AccessToken: "xoxc-read", Cookie: "d-read"}
	fresh := Token{TeamID: "T1", TeamName: "Acme", AccessToken: "xoxc-fresh", Cookie: "d-fresh"}
	other := Token{TeamID: "T1", TeamName: "Acme", AccessToken: "xoxc-other", Cookie: "d-other", Source: TokenSourceBrowser}
	for _, tc := range []struct {
		name string
		// meanwhile is what another process does after the caller read
		// the entry, and before SaveIf.
		meanwhile func(t *testing.T, dir string)
		saved     bool
		// want is the entry afterwards; nil means none that can be read.
		want *Token
	}{
		{"the entry read is still there", func(*testing.T, string) {}, true, &fresh},
		{"added again", func(t *testing.T, dir string) { mustSave(t, NewTokenStore(dir), other) }, false, &other},
		{"removed", func(t *testing.T, dir string) {
			if err := NewTokenStore(dir).Delete("T1"); err != nil {
				t.Fatal(err)
			}
		}, false, nil},
		{"unreadable", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "T1.json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := NewTokenStore(dir)
			mustSave(t, s, read)
			tc.meanwhile(t, dir)

			saved, err := s.SaveIf(fresh, read)
			if err != nil || saved != tc.saved {
				t.Fatalf("SaveIf = %v, %v; want %v, nil", saved, err, tc.saved)
			}
			got, err := s.Load("T1")
			switch {
			case tc.want == nil && err == nil:
				t.Errorf("the store holds %+v, want no readable entry", got)
			case tc.want != nil && (err != nil || got != *tc.want):
				t.Errorf("the store holds %+v (%v), want %+v", got, err, *tc.want)
			}
		})
	}
}

// TestTokenStore_SaveWaitsForTheLock: the store's lock orders the
// writers of every slk process. A second open of the lock file stands in
// for another process here.
func TestTokenStore_SaveWaitsForTheLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockTokenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- NewTokenStore(dir).Save(Token{TeamID: "T1", AccessToken: "xoxc-1"}) }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("Save finished while another holder had the lock (err %v)", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Save: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save did not finish after the lock was released")
	}
}

func mustSave(t *testing.T, s *TokenStore, tok Token) {
	t.Helper()
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
}
