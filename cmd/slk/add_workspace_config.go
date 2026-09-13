package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/gammons/slk/internal/config"
)

// uniqueSlug returns base if it is non-empty and not in existing,
// otherwise appends -2, -3, ... until it finds an unused slug.
// An empty base falls back to "workspace".
func uniqueSlug(base string, existing map[string]bool) string {
	if base == "" {
		base = "workspace"
	}
	if !existing[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !existing[candidate] {
			return candidate
		}
	}
}

// appendWorkspaceConfigBlock appends a [workspaces.<slug>] block with
// team_id set, prefixed by a "# <teamName>" comment line. The file
// is created if it does not exist. Existing content is preserved
// verbatim (textual append, not TOML re-marshal).
//
// Writing a second block for a team_id that already has one is a no-op.
// The slug is what uniqueSlug de-duplicates, and two different slugs
// can name the same workspace — re-running --add-workspace with an
// already-configured workspace selected produced exactly that, and
// config.Load then rejects the file, so slk refuses to start until the
// user hand-edits it. Keying the skip on team_id rather than on the
// caller remembering to filter makes the writer safe no matter who
// calls it.
func appendWorkspaceConfigBlock(configPath, slug, teamID, teamName string) error {
	if teamID != "" && configuredTeamIDs(configPath)[teamID] {
		return nil
	}

	var existing []byte
	if data, err := os.ReadFile(configPath); err == nil {
		existing = data
	} else if !os.IsNotExist(err) {
		return err
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	fmt.Fprintf(&b, "# %s\n", safeName)
	fmt.Fprintf(&b, "[workspaces.%s]\n", slug)
	fmt.Fprintf(&b, "team_id = %s\n", tomlString(teamID))

	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(b.String()), 0644)
}

// configuredTeamIDs returns the set of team ids already present in
// configPath. An unreadable or absent file yields an empty set: the
// caller is about to create it.
//
// Decodes the file directly rather than going through config.Load,
// which validates. Validation is exactly what fails on a config that
// already carries a duplicate team_id — so routing this through Load
// would make the duplicate guard useless in the one case it exists
// for, and the second run would append a third block to a file slk
// already refuses to start on.
func configuredTeamIDs(configPath string) map[string]bool {
	out := map[string]bool{}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return out
	}
	var raw struct {
		Workspaces map[string]struct {
			TeamID string `toml:"team_id"`
		} `toml:"workspaces"`
	}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return out
	}
	for _, w := range raw.Workspaces {
		if w.TeamID != "" {
			out[w.TeamID] = true
		}
	}
	return out
}

// existingSlugs reads configPath (if present) and returns the set of
// already-used [workspaces.<key>] keys. Used by addWorkspace to
// avoid colliding with existing slug or legacy entries.
func existingSlugs(configPath string) map[string]bool {
	cfg, err := config.Load(configPath)
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(cfg.Workspaces))
	for k := range cfg.Workspaces {
		out[k] = true
	}
	return out
}
