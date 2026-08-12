package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gammons/slk/internal/config"
)

// TestConfigSavers_ReaderNeverSeesPartialFile is the regression guard
// for non-atomic config writes.
//
// configWriteMu only covers writers inside ONE process. Two slk
// instances against the same config.toml are not serialized at all,
// and with a truncate-then-write os.WriteFile the second one can read
// the file while it is half-written. Every saver here is a
// read-modify-write, so it then persists that truncation — the user
// loses themes, sections and whole workspace blocks.
//
// The reader below stands in for that second process: it re-reads and
// re-parses the file while writers hammer it, and demands that every
// single read be a complete config, never a prefix of one. With
// os.WriteFile this fails within a few hundred iterations; with
// writeConfigAtomic's rename it cannot fail, because a rename swaps
// whole inodes.
func TestConfigSavers_ReaderNeverSeesPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Padding makes the write large enough that a truncate-then-write
	// has a window a reader can land in. Without it the file is a few
	// hundred bytes and the race is too narrow to observe reliably,
	// which would make this test pass against the broken
	// implementation.
	var b strings.Builder
	b.WriteString("[appearance]\ntheme = \"dark\"\n\n")
	b.WriteString("# Acme\n[workspaces.acme]\nteam_id = \"T04T4TH8W\"\n")
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&b, "# padding line %d, here to widen the truncate-then-write window\n", i)
	}
	b.WriteString("\n# Other\n[workspaces.other]\nteam_id = \"T05U5UI9X\"\n")
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	writeErr := make(chan error, 4)

	writers := []func(i int) error{
		func(i int) error { return saveGlobalTheme(path, fmt.Sprintf("theme-%d", i)) },
		func(i int) error {
			return saveWorkspaceTheme(path, "acme", "T04T4TH8W", "Acme", fmt.Sprintf("t-%d", i))
		},
		func(i int) error { return saveWorkspaceWidth(path, "other", "T05U5UI9X", "Other", 20+i%30) },
		func(i int) error {
			return saveWorkspaceVersionTS(path, "acme", "T04T4TH8W", "Acme", fmt.Sprintf("17854%05d", i))
		},
	}
	for _, w := range writers {
		wg.Add(1)
		go func(save func(int) error) {
			defer wg.Done()
			for i := 0; !stop.Load(); i++ {
				if err := save(i); err != nil {
					writeErr <- err
					return
				}
			}
		}(w)
	}

	const reads = 500
	for i := 0; i < reads; i++ {
		cfg, err := config.Load(path)
		if err != nil {
			stop.Store(true)
			wg.Wait()
			raw, _ := os.ReadFile(path)
			t.Fatalf("read %d saw an unparseable config (%d bytes): %v", i, len(raw), err)
		}
		// A truncation cut at a line boundary still parses as TOML —
		// it is just missing the tail. The workspace blocks bracket
		// the padding, so requiring both catches a prefix read that
		// the parser alone would accept.
		if _, ok := cfg.Workspaces["acme"]; !ok {
			stop.Store(true)
			wg.Wait()
			t.Fatalf("read %d saw a config with no [workspaces.acme] block", i)
		}
		if _, ok := cfg.Workspaces["other"]; !ok {
			stop.Store(true)
			wg.Wait()
			raw, _ := os.ReadFile(path)
			t.Fatalf("read %d saw a truncated config: [workspaces.other] is gone (%d bytes)", i, len(raw))
		}
	}

	stop.Store(true)
	wg.Wait()
	close(writeErr)
	for err := range writeErr {
		t.Fatalf("save: %v", err)
	}
}

// TestWriteConfigAtomic_PreservesMode checks the saved file keeps the
// mode it already had. os.CreateTemp makes its file 0600, so without
// an explicit Chmod every save would silently tighten permissions the
// user chose.
func TestWriteConfigAtomic_PreservesMode(t *testing.T) {
	dir := t.TempDir()

	existing := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(existing, []byte("# old\n"), 0640); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeConfigAtomic(existing, []byte("# new\n")); err != nil {
		t.Fatalf("writeConfigAtomic: %v", err)
	}
	fi, err := os.Stat(existing)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0640 {
		t.Errorf("mode = %o after save; want the file's existing 0640", got)
	}
	if data, _ := os.ReadFile(existing); string(data) != "# new\n" {
		t.Errorf("contents = %q; want the new data", data)
	}

	// A file that did not exist yet gets the default, not 0600.
	fresh := filepath.Join(dir, "fresh.toml")
	if err := writeConfigAtomic(fresh, []byte("# fresh\n")); err != nil {
		t.Fatalf("writeConfigAtomic (new file): %v", err)
	}
	fi, err = os.Stat(fresh)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != defaultConfigPerm {
		t.Errorf("new file mode = %o; want %o", got, defaultConfigPerm)
	}
}

// TestWriteConfigAtomic_LeavesNoTempFiles guards the cleanup path: a
// stale .config-*.toml.tmp beside the user's config is litter, and one
// left with the old contents is a trap for anyone debugging by hand.
func TestWriteConfigAtomic_LeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := writeConfigAtomic(path, []byte("# a\n")); err != nil {
		t.Fatalf("writeConfigAtomic: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v; want only config.toml", names)
	}
}
