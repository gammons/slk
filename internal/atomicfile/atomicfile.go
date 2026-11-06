// Package atomicfile replaces a file's contents so that a concurrent
// reader, in this process or another one, sees either the whole old
// file or the whole new one, never a truncated mix.
//
// slk needs this wherever two processes share a file: two slk
// instances on one config.toml, or a TUI that lists the token store
// while a CLI command saves a refreshed token into it.
package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
)

// Write replaces path's contents with data by writing a temp file in
// the same directory and renaming it over the target.
//
// A plain os.WriteFile truncates the file and then writes it, so a
// reader can land in between and see an empty or half-written file.
// Rename is atomic within a filesystem, so a reader sees either the
// whole old file or the whole new one. That is why the temp file must
// be created in the target's own directory rather than os.TempDir().
//
// An existing file keeps its permission bits; a new file gets
// defaultPerm. The temp file is named ".<stem>-<random><ext>.tmp", so a
// directory scan that matches the target's extension (the token
// store lists "*.json") never mistakes it for a real file.
//
// Write does not make a read-modify-write cycle atomic: two processes
// can still lose one another's update. Callers that read, change and
// write back serialize themselves (see configWriteMu in cmd/slk).
func Write(path string, data []byte, defaultPerm os.FileMode) error {
	perm := defaultPerm
	if fi, err := os.Stat(path); err == nil {
		perm = fi.Mode().Perm()
	}

	f, err := os.CreateTemp(filepath.Dir(path), tempPattern(filepath.Base(path)))
	if err != nil {
		return err
	}
	tmp := f.Name()
	// No-op once the rename below succeeds; on any earlier return it
	// keeps a failed save from littering the directory.
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	// Without this a crash between rename and writeback can leave a
	// zero-length file, which is the same data loss by another route.
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	// CreateTemp makes the file 0600. Restore the mode the file
	// actually had, so a save doesn't silently change permissions the
	// user chose.
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// tempPattern is the os.CreateTemp pattern for a temp file beside
// base: "config.toml" gives ".config-*.toml.tmp". The leading dot hides
// it from a casual ls, and the ".tmp" suffix keeps it out of any scan
// for base's own extension.
func tempPattern(base string) string {
	ext := filepath.Ext(base)
	return "." + strings.TrimSuffix(base, ext) + "-*" + ext + ".tmp"
}
