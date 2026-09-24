package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestClipboardWriter(t *testing.T) {
	const text = "hello 世界 👋\nsecond line\n"
	for _, tt := range []struct {
		name       string
		goos       string
		env        map[string]string
		writeErr   error
		wantNative bool
	}{
		{name: "Terminal.app", goos: "darwin", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, wantNative: true},
		{name: "local tmux", goos: "darwin", env: map[string]string{"TMUX": "/tmp/tmux/default", "TERM_PROGRAM": "tmux"}, wantNative: true},
		{name: "other macOS terminal", goos: "darwin", wantNative: true},
		{name: "SSH connection", goos: "darwin", env: map[string]string{"SSH_CONNECTION": "client 123 server 22"}},
		{name: "SSH client", goos: "darwin", env: map[string]string{"SSH_CLIENT": "client 123 22"}},
		{name: "SSH tty", goos: "darwin", env: map[string]string{"SSH_TTY": "/dev/ttys001"}},
		{name: "Linux", goos: "linux"},
		{name: "Windows", goos: "windows"},
		{name: "native failure", goos: "darwin", writeErr: errors.New("pbcopy failed"), wantNative: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			writer := newClipboardWriter(tt.goos, func(key string) string { return tt.env[key] }, func(got string) error {
				calls++
				if got != text {
					t.Errorf("native payload = %q, want %q", got, text)
				}
				return tt.writeErr
			})
			cmd := writer(text)
			if calls != 0 {
				t.Fatal("clipboard write ran before command execution")
			}
			if cmd == nil {
				t.Fatal("clipboard writer returned no command")
			}
			got := cmd()
			wantCalls := 0
			if tt.wantNative {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Errorf("native calls = %d, want %d", calls, wantCalls)
			}
			if tt.wantNative && tt.writeErr == nil {
				if got != nil {
					t.Errorf("successful native write returned %T, want nil", got)
				}
			} else if want := tea.SetClipboard(text)(); !reflect.DeepEqual(got, want) {
				t.Errorf("OSC 52 message = %#v, want %#v", got, want)
			}
		})
	}
}

func TestWriteMacOSClipboard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pbcopy uses a POSIX shell")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatalf("find shell for fake pbcopy: %v", err)
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Fatalf("find cat for fake pbcopy: %v", err)
	}
	for _, tt := range []struct {
		name    string
		script  string
		wantErr bool
	}{
		{name: "exact stdin", script: fmt.Sprintf("#!%s\n%s > \"$SLK_TEST_CLIPBOARD\"\n", shell, cat)},
		{name: "nonzero exit", script: fmt.Sprintf("#!%s\nexit 1\n", shell), wantErr: true},
		{name: "missing executable", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "clipboard")
			t.Setenv("PATH", dir)
			t.Setenv("SLK_TEST_CLIPBOARD", output)
			if tt.script != "" {
				if err := os.WriteFile(filepath.Join(dir, "pbcopy"), []byte(tt.script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			const text = "hello 世界 👋\n'quotes' $literal `text`\n"
			err := writeMacOSClipboard(text)
			if (err != nil) != tt.wantErr {
				t.Fatalf("writeMacOSClipboard error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != text {
				t.Errorf("pbcopy stdin = %q, want %q", got, text)
			}
		})
	}
}
