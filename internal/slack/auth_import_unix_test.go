//go:build unix

package slackclient

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadAuthFileRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadAuthFile(path); got != nil || err != ErrUnsafeAuthFile {
		t.Fatalf("FIFO input = %d bytes, %v; want nil, unsafe-file error", len(got), err)
	}
}

func TestReadAuthFileRejectsFIFOReplacementPromptly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, []byte("FAKE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		data, err := readAuthFile(path, info)
		if data != nil {
			err = nil
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != ErrUnsafeAuthFile {
			t.Fatalf("FIFO replacement error = %v; want unsafe-file error", err)
		}
	case <-time.After(time.Second):
		writer, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("FIFO reader did not stop after being unblocked")
		}
		t.Fatal("FIFO replacement blocked authentication input")
	}
}

func TestReadAuthFileRejectsSymlinkToOriginalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, []byte("FAKE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	original := path + ".original"
	if err := os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, path); err != nil {
		t.Fatal(err)
	}
	if data, err := readAuthFile(path, info); data != nil || err != ErrUnsafeAuthFile {
		t.Fatalf("symlink to original = %d bytes, %v; want unsafe-file error", len(data), err)
	}
}

func TestSafeImportFileOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("missing POSIX owner metadata")
	}
	if !safeImportFile(info) {
		t.Fatal("rejected a private regular file owned by the current user")
	}
	stat.Uid = uint32(os.Geteuid()) + 1
	if safeImportFile(info) {
		t.Fatal("accepted a private regular file owned by another user")
	}
}

func TestImportOwner(t *testing.T) {
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("missing POSIX owner metadata")
	}
	uid := uint32(os.Geteuid())
	for _, tc := range []struct {
		name      string
		uid       uint32
		allowRoot bool
		want      bool
	}{
		{"current user", uid, false, true},
		{"another user", uid + 1, false, false},
		{"another ancestor owner", uid + 1, true, false},
		{"trusted root ancestor", 0, true, true},
		{"root file", 0, false, uid == 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stat.Uid = tc.uid
			if got := importOwner(info, tc.allowRoot); got != tc.want {
				t.Errorf("importOwner = %v; want %v", got, tc.want)
			}
		})
	}
}
