package slackclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReadAuthFile(t *testing.T) {
	for _, size := range []int{0, 1, AuthBundleLimit - 1, AuthBundleLimit, AuthBundleLimit + 1, AuthBundleLimit * 4} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bundle.json")
			want := strings.Repeat("x", size)
			if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadAuthFile(path)
			if size > AuthBundleLimit {
				if got != nil || !errors.Is(err, ErrInvalidAuthBundle) {
					t.Fatalf("oversized input = %d bytes, %v; want nil, invalid-bundle error", len(got), err)
				}
				return
			}
			if err != nil || string(got) != want {
				t.Fatalf("read returned %d bytes, %v; want %d bytes", len(got), err, size)
			}
		})
	}
}

func TestReadAuthFileRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "dangling symlink", "group readable", "group writable", "group executable", "world readable", "world writable", "world executable"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "FAKE-SECRET-MARKER.json")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink", "dangling symlink":
				target := filepath.Join(dir, "target.json")
				if kind == "symlink" {
					if err := os.WriteFile(target, []byte("FAKE-SECRET-MARKER"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skip("symlinks unavailable")
				}
			default:
				if runtime.GOOS == "windows" {
					t.Skip("POSIX permissions")
				}
				mode := map[string]os.FileMode{
					"group readable": 0o640, "group writable": 0o620, "group executable": 0o610,
					"world readable": 0o604, "world writable": 0o602, "world executable": 0o601,
				}[kind]
				if err := os.WriteFile(path, []byte("FAKE-SECRET-MARKER"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := ReadAuthFile(path); got != nil || err != ErrUnsafeAuthFile {
				t.Fatalf("unsafe input = %d bytes, %v; want nil, fixed unsafe-file error", len(got), err)
			}
		})
	}
}

func TestReadAuthFileIOErrorsAreFixed(t *testing.T) {
	for _, kind := range []string{"missing", "invalid path", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "FAKE-SECRET-MARKER.json")
			switch kind {
			case "invalid path":
				path += "\x00"
			case "unreadable":
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("requires unprivileged POSIX permission enforcement")
				}
				if err := os.WriteFile(path, []byte("FAKE-SECRET-MARKER"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o200); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadAuthFile(path)
			if got != nil || err == nil || err.Error() != "cannot read authentication file" || errors.Unwrap(err) != nil {
				t.Fatalf("I/O failure = %d bytes, %v; want nil, fixed I/O error", len(got), err)
			}
		})
	}
}

func TestReadAuthFileRechecksOpenedFile(t *testing.T) {
	for _, kind := range []string{"replacement", "symlink replacement", "permissions changed", "removed"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "bundle.json")
			if err := os.WriteFile(path, []byte("FAKE-SECRET-OLD"), 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			wantErr := ErrUnsafeAuthFile
			switch kind {
			case "replacement", "symlink replacement":
				if err := os.Rename(path, filepath.Join(dir, "old.json")); err != nil {
					t.Fatal(err)
				}
				if kind == "replacement" {
					if err := os.WriteFile(path, []byte("FAKE-SECRET-NEW"), 0o600); err != nil {
						t.Fatal(err)
					}
				} else {
					target := filepath.Join(dir, "new.json")
					if err := os.WriteFile(target, []byte("FAKE-SECRET-NEW"), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, path); err != nil {
						t.Skip("symlinks unavailable")
					}
				}
			case "permissions changed":
				if runtime.GOOS == "windows" {
					t.Skip("POSIX permissions")
				}
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
			case "removed":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				wantErr = errReadAuthFile
			}
			if got, err := readAuthFile(path, info); got != nil || err != wantErr {
				t.Fatalf("changed input = %d bytes, %v; want nil, %v", len(got), err, wantErr)
			}
		})
	}
}

func TestValidateAuthDirectory(t *testing.T) {
	for _, kind := range []string{"existing", "missing", "readable", "file", "symlink", "dangling symlink", "group writable", "world writable", "writable ancestor", "symlink ancestor", "private boundary"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "tokens")
			wantErr := ErrUnsafeAuthFile
			switch kind {
			case "existing", "readable", "group writable", "world writable":
				mode := map[string]os.FileMode{"existing": 0o700, "readable": 0o755, "group writable": 0o770, "world writable": 0o702}[kind]
				if mode&0o022 != 0 && runtime.GOOS == "windows" {
					t.Skip("POSIX permissions")
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
				if mode&0o022 == 0 {
					wantErr = nil
				}
			case "missing":
				path = filepath.Join(path, "missing")
				wantErr = nil
			case "file":
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink", "dangling symlink":
				target := filepath.Join(base, "target")
				if kind == "symlink" {
					target = t.TempDir()
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skip("symlinks unavailable")
				}
			case "writable ancestor", "private boundary":
				if runtime.GOOS == "windows" {
					t.Skip("POSIX permissions")
				}
				if err := os.Chmod(base, 0o777); err != nil {
					t.Fatal(err)
				}
				if kind == "private boundary" {
					boundary := filepath.Join(base, "private")
					if err := os.Mkdir(boundary, 0o700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(boundary, "tokens")
				}
			case "symlink ancestor":
				link := filepath.Join(base, "link")
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Skip("symlinks unavailable")
				}
				path = filepath.Join(link, "tokens")
			}
			if err := ValidateAuthDirectory(path); err != wantErr {
				t.Fatalf("directory validation = %v; want %v", err, wantErr)
			}
			if kind == "missing" || kind == "writable ancestor" || kind == "private boundary" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("directory validation created the missing directory")
				}
			}
		})
	}
}

func TestValidateAuthDirectoryMacOSTempAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS /var alias")
	}
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolved, "/private/var/") {
		t.Skip("temporary directory is outside /private/var")
	}
	alias := strings.TrimPrefix(resolved, "/private")
	for _, path := range []string{alias, filepath.Join(alias, "missing", "tokens")} {
		if err := ValidateAuthDirectory(path); err != nil {
			t.Fatalf("private temporary directory through /var alias: %v", err)
		}
	}
}

func TestTokenStoreImportIf(t *testing.T) {
	old := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD", UserID: "U1"}
	fresh := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW", UserID: "U1", Source: TokenSourceImported}
	other := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OTHER", Cookie: "FAKE-OTHER", UserID: "U1"}
	for _, tc := range []struct {
		name     string
		existing *Token
		expected *Token
		saved    bool
		want     *Token
	}{
		{"absent", nil, nil, true, &fresh},
		{"existing without expectation", &old, nil, false, &old},
		{"guarded replacement", &old, &old, true, &fresh},
		{"deleted meanwhile", nil, &old, false, nil},
		{"refreshed meanwhile", &other, &old, false, &other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTokenStore(filepath.Join(t.TempDir(), "new", "tokens"))
			if tc.existing != nil {
				mustSave(t, s, *tc.existing)
			}
			snapshot, err := s.SnapshotForImport("T1")
			if err != nil || (snapshot == nil) != (tc.existing == nil) {
				t.Fatalf("snapshot presence: %v", err)
			}
			if snapshot != nil && *snapshot != *tc.existing {
				t.Fatal("snapshot changed existing credentials")
			}
			if tc.existing == nil {
				if _, err := os.Lstat(s.dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("snapshot created the missing token directory")
				}
			}
			saved, err := s.ImportIf(context.Background(), fresh, tc.expected)
			if err != nil || saved != tc.saved {
				t.Fatalf("ImportIf = %v, %v; want %v, nil", saved, err, tc.saved)
			}
			got, err := s.SnapshotForImport("T1")
			if err != nil || (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Fatalf("unexpected stored result (error %v)", err)
			}
			if tc.saved && runtime.GOOS != "windows" {
				info, err := os.Stat(filepath.Join(s.dir, "T1.json"))
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatalf("stored file permissions: %v", err)
				}
				info, err = os.Stat(s.dir)
				if err != nil || info.Mode().Perm() != 0o700 {
					t.Fatalf("stored directory permissions: %v", err)
				}
			}
		})
	}
}

func TestTokenStoreImportIfConcurrent(t *testing.T) {
	old := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD"}
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "absence", true: "replacement"}[replace], func(t *testing.T) {
			dir := t.TempDir()
			var expected *Token
			if replace {
				mustSave(t, NewTokenStore(dir), old)
				expected = &old
			}
			start := make(chan struct{})
			results := make(chan bool, 2)
			errs := make(chan error, 2)
			var wg sync.WaitGroup
			for _, suffix := range []string{"A", "B"} {
				wg.Go(func() {
					<-start
					candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-" + suffix, Cookie: "FAKE-" + suffix, Source: TokenSourceImported}
					saved, err := NewTokenStore(dir).ImportIf(context.Background(), candidate, expected)
					results <- saved
					errs <- err
				})
			}
			close(start)
			wg.Wait()
			wins := 0
			for range 2 {
				if <-results {
					wins++
				}
				if err := <-errs; err != nil {
					t.Fatal(err)
				}
			}
			if wins != 1 {
				t.Fatalf("successful imports = %d; want 1", wins)
			}
		})
	}
}

func TestTokenStoreImportCanceledBeforeLock(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "canceled", true: "deadline exceeded"}[expired], func(t *testing.T) {
			s := NewTokenStore(filepath.Join(t.TempDir(), "not-created"))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			wantErr := context.Canceled
			if expired {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				wantErr = context.DeadlineExceeded
			}
			candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}
			if saved, err := s.ImportIf(ctx, candidate, nil); saved || err != wantErr {
				t.Fatalf("ImportIf = %v, %v; want false, %v", saved, err, wantErr)
			}
			if _, err := os.Lstat(s.dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("canceled import created the token directory")
			}
		})
	}
}

func TestTokenStoreImportCanceledWhileWaitingForLock(t *testing.T) {
	for _, kind := range []string{"absence", "replacement", "corrupt snapshot"} {
		t.Run(kind, func(t *testing.T) {
			s := NewTokenStore(t.TempDir())
			var expected *Token
			if kind != "absence" {
				old := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD"}
				mustSave(t, s, old)
				expected = &old
			}
			unlock, err := lockTokenDir(s.dir)
			if err != nil {
				t.Fatal(err)
			}
			release := sync.OnceFunc(unlock)
			t.Cleanup(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				saved bool
				err   error
			}
			done := make(chan result, 1)
			go func() {
				candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}
				saved, err := s.ImportIf(ctx, candidate, expected)
				done <- result{saved, err}
			}()
			select {
			case got := <-done:
				t.Fatalf("import finished while lock held: %v, %v", got.saved, got.err)
			case <-time.After(100 * time.Millisecond):
			}
			path := filepath.Join(s.dir, "T1.json")
			if kind == "corrupt snapshot" {
				if err := os.WriteFile(path, []byte("FAKE-SECRET-MARKER"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			cancel()
			release()
			select {
			case got := <-done:
				if got.saved || got.err != context.Canceled {
					t.Fatalf("ImportIf = %v, %v; want false, context.Canceled", got.saved, got.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("canceled import did not finish after unlock")
			}
			after, err := os.ReadFile(path)
			if kind == "absence" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("canceled import wrote a new login")
				}
			} else if err != nil || string(after) != string(before) {
				t.Fatal("canceled import changed the old login")
			}
		})
	}
}

func TestTokenStoreImportIOErrorsAreFixed(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged POSIX permission enforcement")
	}
	for _, kind := range []string{"invalid directory path", "inaccessible ancestor", "unreadable directory", "unreadable lock", "unwritable directory"} {
		t.Run(kind, func(t *testing.T) {
			s := NewTokenStore(filepath.Join(t.TempDir(), "FAKE-SECRET-MARKER"))
			if err := os.Mkdir(s.dir, 0o700); err != nil {
				t.Fatal(err)
			}
			snapshotFails := false
			switch kind {
			case "invalid directory path":
				s.dir += "\x00"
				snapshotFails = true
			case "inaccessible ancestor":
				parent := s.dir
				if err := os.Chmod(parent, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
				s.dir = filepath.Join(parent, "tokens")
				snapshotFails = true
			case "unreadable directory":
				if err := os.Chmod(s.dir, 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(s.dir, 0o700) })
				snapshotFails = true
			case "unreadable lock":
				if err := os.WriteFile(filepath.Join(s.dir, tokenLockFile), nil, 0o200); err != nil {
					t.Fatal(err)
				}
			case "unwritable directory":
				if err := os.Chmod(s.dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(s.dir, 0o700) })
			}
			got, err := s.SnapshotForImport("T1")
			if snapshotFails {
				if got != nil || err != errAuthStorageIO {
					t.Fatalf("snapshot I/O failure = %v; want fixed storage I/O error", err)
				}
			} else if got != nil || err != nil {
				t.Fatalf("absent snapshot = %v; want nil", err)
			}
			candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}
			if saved, err := s.ImportIf(context.Background(), candidate, nil); saved || err != errAuthStorageIO {
				t.Fatalf("import I/O failure = %v, %v; want false, fixed storage I/O error", saved, err)
			}
		})
	}
}

func TestTokenStoreImportRejectsUnsafeFiles(t *testing.T) {
	candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, s *TokenStore)
	}{
		{"corrupt token", func(t *testing.T, s *TokenStore) {
			if err := os.WriteFile(filepath.Join(s.dir, "T1.json"), []byte(`{"access_token":"FAKE-SECRET-MARKER"`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversized token", func(t *testing.T, s *TokenStore) {
			if err := os.WriteFile(filepath.Join(s.dir, "T1.json"), []byte(strings.Repeat(" ", AuthBundleLimit+1)), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"mismatched stored team", func(t *testing.T, s *TokenStore) {
			mustSave(t, s, Token{TeamID: "T2", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD"})
			if err := os.Rename(filepath.Join(s.dir, "T2.json"), filepath.Join(s.dir, "T1.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"empty token object", func(t *testing.T, s *TokenStore) {
			if err := os.WriteFile(filepath.Join(s.dir, "T1.json"), []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"token directory", func(t *testing.T, s *TokenStore) {
			if err := os.Mkdir(filepath.Join(s.dir, "T1.json"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"token symlink", func(t *testing.T, s *TokenStore) {
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("FAKE-SECRET-MARKER"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(s.dir, "T1.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"dangling token symlink", func(t *testing.T, s *TokenStore) {
			if err := os.Symlink(filepath.Join(s.dir, "missing"), filepath.Join(s.dir, "T1.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock directory", func(t *testing.T, s *TokenStore) {
			if err := os.Mkdir(filepath.Join(s.dir, tokenLockFile), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"lock symlink", func(t *testing.T, s *TokenStore) {
			if err := os.Symlink(filepath.Join(s.dir, "missing"), filepath.Join(s.dir, tokenLockFile)); err != nil {
				t.Fatal(err)
			}
		}},
		{"public token", func(t *testing.T, s *TokenStore) {
			if runtime.GOOS == "windows" {
				t.Skip("POSIX permissions")
			}
			mustSave(t, s, candidate)
			if err := os.Chmod(filepath.Join(s.dir, "T1.json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"public lock", func(t *testing.T, s *TokenStore) {
			if runtime.GOOS == "windows" {
				t.Skip("POSIX permissions")
			}
			if err := os.WriteFile(filepath.Join(s.dir, tokenLockFile), nil, 0o666); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(s.dir, tokenLockFile), 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{"writable token directory", func(t *testing.T, s *TokenStore) {
			if runtime.GOOS == "windows" {
				t.Skip("POSIX permissions")
			}
			if err := os.Chmod(s.dir, 0o777); err != nil {
				t.Fatal(err)
			}
		}},
		{"writable ancestor", func(t *testing.T, s *TokenStore) {
			if runtime.GOOS == "windows" {
				t.Skip("POSIX permissions")
			}
			if err := os.Chmod(filepath.Dir(s.dir), 0o777); err != nil {
				t.Fatal(err)
			}
		}},
		{"ancestor symlink", func(t *testing.T, s *TokenStore) {
			parent := filepath.Dir(s.dir)
			moved := parent + "-moved"
			if err := os.Rename(parent, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, parent); err != nil {
				t.Fatal(err)
			}
		}},
		{"token directory symlink", func(t *testing.T, s *TokenStore) {
			if err := os.Remove(s.dir); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), s.dir); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewTokenStore(filepath.Join(t.TempDir(), "parent", "tokens"))
			if err := os.MkdirAll(s.dir, 0o700); err != nil {
				t.Fatal(err)
			}
			tc.setup(t, s)
			wantErr := ErrUnsafeAuthFile
			switch tc.name {
			case "corrupt token", "oversized token", "mismatched stored team", "empty token object":
				wantErr = ErrInvalidAuthBundle
			}
			if got, err := s.SnapshotForImport("T1"); err != wantErr || got != nil {
				t.Fatalf("snapshot error = %v; want %v", err, wantErr)
			} else if strings.Contains(err.Error(), "FAKE-SECRET-MARKER") {
				t.Fatal("snapshot error disclosed credentials")
			}
			if saved, err := s.ImportIf(context.Background(), candidate, nil); err != wantErr || saved {
				t.Fatalf("import = %v, %v; want false, %v", saved, err, wantErr)
			} else if strings.Contains(err.Error(), "FAKE-SECRET-MARKER") {
				t.Fatal("import error disclosed credentials")
			}
		})
	}
}

func TestTokenStoreImportUnreadableSnapshotError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged POSIX permission enforcement")
	}
	s := NewTokenStore(t.TempDir())
	mustSave(t, s, Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD"})
	if err := os.Chmod(filepath.Join(s.dir, "T1.json"), 0o200); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SnapshotForImport("T1"); got != nil || err != errReadAuthFile {
		t.Fatalf("unreadable snapshot = %v; want fixed read error", err)
	}
	candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}
	if saved, err := s.ImportIf(context.Background(), candidate, nil); saved || err != errReadAuthFile {
		t.Fatalf("unreadable import = %v, %v; want false, fixed read error", saved, err)
	}
}

func TestTokenStoreImportRechecksSafety(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	s := NewTokenStore(t.TempDir())
	old := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD"}
	mustSave(t, s, old)
	expected, err := s.SnapshotForImport("T1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.dir, "T1.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := old
	fresh.AccessToken = "xoxc-FAKE-NEW"
	if saved, err := s.ImportIf(context.Background(), fresh, expected); saved || !errors.Is(err, ErrUnsafeAuthFile) {
		t.Fatalf("ImportIf = %v, %v; want unsafe-file error", saved, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("unsafe replacement changed the old file")
	}
}

func TestTokenStoreImportValidatesPathBeforeIO(t *testing.T) {
	s := NewTokenStore(filepath.Join(t.TempDir(), "not-created"))
	for _, teamID := range []string{"", "../T1", "/T1", `T1\T2`, "T1/../T2", "T1\n"} {
		if got, err := s.SnapshotForImport(teamID); got != nil || !errors.Is(err, ErrInvalidAuthBundle) {
			t.Fatal("snapshot accepted an invalid workspace ID")
		}
		candidate := Token{TeamID: teamID, AccessToken: "xoxc-FAKE", Cookie: "FAKE"}
		if saved, err := s.ImportIf(context.Background(), candidate, nil); saved || !errors.Is(err, ErrInvalidAuthBundle) {
			t.Fatal("import accepted an invalid workspace ID")
		}
	}
	if _, err := os.Lstat(s.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid input touched the token directory")
	}
}

func TestTokenStoreImportWaitsForLockAndPreservesUnrelated(t *testing.T) {
	dir := t.TempDir()
	s := NewTokenStore(dir)
	other := Token{TeamID: "T2", AccessToken: "xoxc-FAKE-OTHER", Cookie: "FAKE-OTHER"}
	mustSave(t, s, other)
	unrelated := filepath.Join(dir, "T2.json")
	before, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockTokenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.ImportIf(context.Background(), Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("import finished while lock held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("import did not finish after unlock")
	}
	after, err := os.ReadFile(unrelated)
	if err != nil || string(after) != string(before) {
		t.Fatal("import changed an unrelated workspace")
	}
}

func TestTokenStoreImportRejectsUnsafeNewDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	s := NewTokenStore(filepath.Join(parent, "missing", "tokens"))
	if got, err := s.SnapshotForImport("T1"); got != nil || !errors.Is(err, ErrUnsafeAuthFile) {
		t.Fatal("snapshot accepted unsafe first-use directory")
	}
	candidate := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-NEW", Cookie: "FAKE-NEW"}
	if saved, err := s.ImportIf(context.Background(), candidate, nil); saved || !errors.Is(err, ErrUnsafeAuthFile) {
		t.Fatal("import accepted unsafe first-use directory")
	}
	if _, err := os.Lstat(filepath.Join(parent, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe import created directories")
	}
}

func TestTokenStoreImportWriteFailurePreservesOldLogin(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires unprivileged POSIX permission enforcement")
	}
	s := NewTokenStore(t.TempDir())
	old := Token{TeamID: "T1", AccessToken: "xoxc-FAKE-OLD", Cookie: "FAKE-OLD"}
	mustSave(t, s, old)
	before, err := os.ReadFile(filepath.Join(s.dir, "T1.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := s.SnapshotForImport("T1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.dir, 0o700) })
	fresh := old
	fresh.AccessToken = "xoxc-FAKE-NEW"
	if saved, err := s.ImportIf(context.Background(), fresh, expected); saved || err != errAuthStorageIO {
		t.Fatalf("failed write = %v, %v; want false, fixed storage I/O error", saved, err)
	}
	after, err := os.ReadFile(filepath.Join(s.dir, "T1.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("failed write changed the old login")
	}
}
