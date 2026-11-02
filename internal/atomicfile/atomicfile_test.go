package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWrite_KeepsExistingModeAndDefaultsNewOnes checks both halves of
// the permission rule: an existing file keeps its mode (CreateTemp's
// 0600 must not leak through), and a new one gets defaultPerm.
func TestWrite_KeepsExistingModeAndDefaultsNewOnes(t *testing.T) {
	dir := t.TempDir()

	existing := filepath.Join(dir, "a.toml")
	if err := os.WriteFile(existing, []byte("old"), 0640); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// WriteFile's mode goes through the umask (077 turns 0640 into
	// 0600); Chmod does not.
	if err := os.Chmod(existing, 0640); err != nil {
		t.Fatalf("seed mode: %v", err)
	}
	if err := Write(existing, []byte("new"), 0600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if fi, _ := os.Stat(existing); fi.Mode().Perm() != 0640 {
		t.Errorf("existing file mode = %o, want 0640", fi.Mode().Perm())
	}
	if data, _ := os.ReadFile(existing); string(data) != "new" {
		t.Errorf("contents = %q, want %q", data, "new")
	}

	fresh := filepath.Join(dir, "b.json")
	if err := Write(fresh, []byte("{}"), 0604); err != nil {
		t.Fatalf("Write (new file): %v", err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0604 {
		t.Errorf("new file mode = %o, want 0604", fi.Mode().Perm())
	}
}

// TestWrite_LeavesOnlyTheTarget checks that no temp file is left
// behind, after a successful write and after one whose rename fails.
func TestWrite_LeavesOnlyTheTarget(t *testing.T) {
	dir := t.TempDir()
	if err := Write(filepath.Join(dir, "T1.json"), []byte("{}"), 0600); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// A directory where the target should be makes the rename fail
	// after the temp file exists. (A read-only directory would not:
	// root, which CI can be, writes to it anyway.)
	if err := os.Mkdir(filepath.Join(dir, "T2.json"), 0700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Write(filepath.Join(dir, "T2.json"), []byte("{}"), 0600); err == nil {
		t.Fatal("Write over a directory succeeded")
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "T1.json" && e.Name() != "T2.json" {
			t.Errorf("directory holds %q, want only T1.json and T2.json", e.Name())
		}
	}
}

// TestTempPattern pins the temp-file name. The token store lists every
// "*.json" in its directory, so a temp file ending in ".json" would be
// read as a half-written token while a save is in flight. The config
// pattern is the one config.toml's saver has always used.
func TestTempPattern(t *testing.T) {
	for base, want := range map[string]string{
		"config.toml": ".config-*.toml.tmp",
		"T0123.json":  ".T0123-*.json.tmp",
		"noext":       ".noext-*.tmp",
	} {
		if got := tempPattern(base); got != want {
			t.Errorf("tempPattern(%q) = %q, want %q", base, got, want)
		}
	}
}
