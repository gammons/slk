package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
func appendWorkspaceConfigBlock(configPath, slug, teamID, teamName string) error {
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
