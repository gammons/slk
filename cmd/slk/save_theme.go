package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// configWriteMu serializes the read-modify-write cycles the save*
// helpers below perform on config.toml. The theme and width savers run
// on the UI goroutine, but the version_ts refresh fires from one
// background goroutine per workspace, all racing on the same file:
// without this lock two concurrent saves lose one another's update, and
// a reader can observe a half-written file and persist the truncation.
var configWriteMu sync.Mutex

// defaultConfigPerm is the mode a config file is created with when it
// does not exist yet. An existing file keeps its own mode.
const defaultConfigPerm os.FileMode = 0644

// writeConfigAtomic replaces configPath's contents with data by writing
// a temp file in the same directory and renaming it over the target.
//
// configWriteMu serialises writers inside one process, but nothing
// stops two slk instances sharing one config.toml. With a plain
// os.WriteFile — truncate, then write — the other process can read the
// file in its truncated state, and every saver here is a
// read-modify-write, so it then writes that truncation back: the
// user's themes, sections and workspace entries are gone. The risk
// profile changed when version_ts started saving automatically on
// every boot, once per workspace, rather than only on rare user
// actions.
//
// Rename is atomic within a filesystem, so a reader sees either the
// whole old file or the whole new one. That is why the temp file must
// be created in the target's own directory rather than os.TempDir().
//
// This does not make the read-modify-write cycle itself atomic across
// processes — two instances can still lose one another's *update*.
// Preventing that needs file locking; this fixes the destructive half,
// where a partial read is persisted as the whole file.
func writeConfigAtomic(configPath string, data []byte) error {
	perm := defaultConfigPerm
	if fi, err := os.Stat(configPath); err == nil {
		perm = fi.Mode().Perm()
	}

	f, err := os.CreateTemp(filepath.Dir(configPath), ".config-*.toml.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// No-op once the rename below succeeds; on any earlier return it
	// keeps a failed save from littering the config directory.
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	// Without this a crash between rename and writeback can leave a
	// zero-length config, which is the same data loss by another route.
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	// CreateTemp makes the file 0600. Restore the mode the config
	// actually had, so a save doesn't silently tighten permissions the
	// user chose.
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, configPath)
}

// tomlString returns s as a properly escaped TOML basic string,
// including the surrounding quotes. Backslashes and double quotes
// are escaped; control characters become their TOML escape forms.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// sanitizeComment turns arbitrary text into a single-line comment-safe
// string by replacing CR/LF and ASCII control characters with spaces.
// The leading "# " is added by the caller.
func sanitizeComment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\r' || r == '\n' || r < 0x20 {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// saveGlobalTheme rewrites the [appearance] theme line in config.toml.
// If the file has no theme line, it appends a new [appearance] section.
// Existing comments and ordering are preserved (textual rewrite, not
// TOML re-marshal).
func saveGlobalTheme(configPath, themeName string) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return err
		}
		data = nil
	} else if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	// Track current section. Match a "theme = ..." line ONLY when we're
	// currently inside the [appearance] section. This avoids clobbering
	// per-workspace [workspaces.X] theme lines.
	inAppearance := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inAppearance = trimmed == "[appearance]"
			continue
		}
		if !inAppearance {
			continue
		}
		if strings.HasPrefix(trimmed, "theme") && strings.Contains(trimmed, "=") &&
			!strings.HasPrefix(trimmed, "theme.") {
			lines[i] = "theme = " + tomlString(themeName)
			return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
		}
	}
	// No [appearance] theme line found — append a new section.
	lines = append(lines, "", "[appearance]", "theme = "+tomlString(themeName))
	return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
}

// saveWorkspaceTheme rewrites or appends a [workspaces.<tomlKey>]
// theme entry. tomlKey is the literal TOML key in the config — for
// slug-keyed blocks that's the slug, for legacy blocks it's the team
// ID. teamID is the underlying Slack team ID; when we are creating a
// brand-new slug-keyed block, teamID is written as the team_id =
// "..." line (currently we only create legacy-keyed blocks here, but
// slug callers update an existing block).
func saveWorkspaceTheme(configPath, tomlKey, teamID, teamName, themeName string) error {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	data, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return err
		}
		data = nil
	} else if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	header := fmt.Sprintf("[workspaces.%s]", tomlKey)

	sectionStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			sectionStart = i
			break
		}
	}

	if sectionStart >= 0 {
		end := len(lines)
		for j := sectionStart + 1; j < len(lines); j++ {
			t := strings.TrimSpace(lines[j])
			if t == "" || strings.HasPrefix(t, "[") {
				end = j
				break
			}
		}
		updated := false
		for j := sectionStart + 1; j < end; j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "theme") && strings.Contains(t, "=") &&
				!strings.HasPrefix(t, "theme.") {
				lines[j] = "theme = " + tomlString(themeName)
				updated = true
				break
			}
		}
		if !updated {
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, "theme = "+tomlString(themeName))
			newLines = append(newLines, lines[sectionStart+1:]...)
			lines = newLines
		}
		return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
	}

	// No existing section — append at end. We only get here when no
	// block exists for either the slug or the team ID, which means we
	// fall back to a legacy-keyed [workspaces.<teamID>] block.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	commentLine := "# " + safeName
	legacyHeader := fmt.Sprintf("[workspaces.%s]", teamID)
	lines = append(lines, commentLine, legacyHeader, "theme = "+tomlString(themeName))
	return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
}
