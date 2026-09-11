//go:build windows

package slackdesktop

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

const childFlag = "--run-lock-child-process"
const filePathEnv = "FILE_PATH"

func TestMain(m *testing.M) {
	// Check if this execution is the spawned child process, and lock the file
	// if it is.
	for _, arg := range os.Args {
		if arg == childFlag {
			openFileNoSharing()
			os.Exit(0)
		}
	}

	// If not a child process, run the normal tests.
	os.Exit(m.Run())
}

func openFileNoSharing() {
	// Get file path from the environment variable.
	filePath := os.Getenv(filePathEnv)
	if filePath == "" {
		fmt.Fprintln(os.Stderr, filePathEnv+" missing")
		os.Exit(1)
	}

	path16, err := windows.UTF16PtrFromString(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create pointer to the path string: %v\n", err)
		os.Exit(1)
	}

	// dwShareMode = 0 prevents any other process from reading, writing, or
	// deleting
	handle, err := windows.CreateFile(
		path16,
		windows.GENERIC_READ|windows.GENERIC_WRITE, // Desired access
		0,                             // dwShareMode: 0 = NO SHARING
		nil,                           // Security attributes
		windows.OPEN_ALWAYS,           // Creation disposition
		windows.FILE_ATTRIBUTE_NORMAL, // Flags and attributes
		0,                             // Template file
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create file handle: %v\n", err)
		os.Exit(1)
	}
	defer windows.CloseHandle(handle)

	// Signal to the parent process via stdout that the lock is active
	fmt.Println("LOCKED")

	// Wait for parent signal on stdin to unlock and terminate
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		_ = scanner.Text() // Parent sent signal to exit
	}
}

func TestIsFileLockError(t *testing.T) {
	// Create file and write to it.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "LockFile.txt")
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Run child process to lock the file
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("Failed to get test executable path: %v", err)
	}

	cmd := exec.Command(execPath, "-test.run=TestMain", "--", childFlag)
	cmd.Env = append(os.Environ(), filePathEnv+"="+filePath)
	cmd.Stderr = os.Stderr

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("failed to create stdin pipe: %v", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start child process: %v", err)
	}
	t.Logf("child process running: %d", cmd.Process.Pid)

	// Ensure child process gets killed if test fails prematurely
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	// Perform the tests after the child process returns LOCKED
	reader := bufio.NewReader(stdoutPipe)
	status, err := reader.ReadString('\n')
	if err != nil || status != "LOCKED\n" {
		t.Fatalf("child process failed to lock file: %v", status)
	} else {
		t.Log("child process has locked the file. Running assertions...")
	}

	_, err = os.OpenFile(filePath, os.O_RDWR, 0)
	if err == nil {
		t.Error("expected file lock error")
	} else {
		if !IsFileLockError(err) {
			t.Error("expected file lock error")
		} else {
			t.Log("file lock detected")
		}
	}

	// Signal child process to release lock and exit
	_, _ = io.WriteString(stdinPipe, "RELEASE\n")
	_ = stdinPipe.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child process exited with error: %v", err)
	}
}
