package slackclient

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateAuthDirectoryWholeAncestry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	for _, tc := range []struct {
		name string
		mode os.FileMode
		want error
	}{
		{"private parent", 0o700, nil},
		{"readable parent", 0o755, nil},
		{"group writable parent", 0o770, ErrUnsafeAuthFile},
		{"world writable parent", 0o777, ErrUnsafeAuthFile},
		{"sticky group writable parent", os.ModeSticky | 0o770, nil},
		{"sticky world writable parent", os.ModeSticky | 0o777, nil},
	} {
		for _, leaf := range []string{"existing", "missing"} {
			t.Run(tc.name+"/"+leaf, func(t *testing.T) {
				ancestor := filepath.Join(t.TempDir(), "ancestor")
				private := filepath.Join(ancestor, "private")
				if err := os.MkdirAll(private, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(ancestor, tc.mode); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(private, "tokens")
				if leaf == "existing" {
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := ValidateAuthDirectory(path); err != tc.want {
					t.Fatalf("directory validation = %v; want %v", err, tc.want)
				}
				if leaf == "missing" {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("directory validation created the missing directory")
					}
				}
			})
		}
	}
}

func TestValidateAuthDirectoryStickyRequiresExistingChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, os.ModeSticky|0o777); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{parent, filepath.Join(parent, "missing"), filepath.Join(parent, "missing", "tokens")} {
		if err := ValidateAuthDirectory(path); err != ErrUnsafeAuthFile {
			t.Fatalf("directory validation = %v; want unsafe-file error", err)
		}
	}
}

func TestValidateAuthDirectoryRejectsSymlinkAbovePrivateTree(t *testing.T) {
	base := t.TempDir()
	private := filepath.Join(base, "real", "private")
	if err := os.MkdirAll(private, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Dir(private), link); err != nil {
		t.Skip("symlinks unavailable")
	}
	for _, path := range []string{filepath.Join(link, "private"), filepath.Join(link, "private", "tokens")} {
		if err := ValidateAuthDirectory(path); err != ErrUnsafeAuthFile {
			t.Fatalf("directory validation = %v; want unsafe-file error", err)
		}
	}
}

func TestValidateAuthDirectorySystemTempAncestry(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix system temporary directory")
	}
	t.Setenv("TMPDIR", "/tmp")
	base := t.TempDir()
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{base, resolved} {
		for _, path := range []string{parent, filepath.Join(parent, "missing", "tokens")} {
			if err := ValidateAuthDirectory(path); err != nil {
				t.Fatalf("system temporary directory validation = %v", err)
			}
		}
	}
}

func TestValidateAuthDirectoryRejectsOtherOwner(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() != 0 {
		t.Skip("requires root to change directory owners")
	}
	for _, kind := range []string{"ancestor", "sticky ancestor", "sticky child", "leaf"} {
		t.Run(kind, func(t *testing.T) {
			ancestor := filepath.Join(t.TempDir(), "ancestor")
			private := filepath.Join(ancestor, "private")
			path := filepath.Join(private, "tokens")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			other := ancestor
			if kind == "sticky ancestor" || kind == "sticky child" {
				if err := os.Chmod(ancestor, os.ModeSticky|0o777); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "sticky child" {
				other = private
			} else if kind == "leaf" {
				other = path
			}
			if err := os.Chown(other, 1, -1); err != nil {
				t.Fatal(err)
			}
			if err := ValidateAuthDirectory(path); err != ErrUnsafeAuthFile {
				t.Fatalf("directory validation = %v; want unsafe-file error", err)
			}
		})
	}
}
