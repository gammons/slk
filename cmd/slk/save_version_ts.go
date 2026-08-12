package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/slackhttp"
)

// seedVersionTS primes env with the build timestamp cached for teamID
// on a previous run, so the very first request of this session carries
// a current _x_version_ts instead of the compiled-in fallback. A nil
// envelope, an unconfigured workspace, or an empty cached value all
// leave the fallback in place (Envelope.SetVersionTS ignores "").
func seedVersionTS(env *slackhttp.Envelope, cfg config.Config, teamID string) {
	if env == nil {
		return
	}
	ws, ok := cfg.WorkspaceByTeamID(teamID)
	if !ok {
		return
	}
	env.SetVersionTS(ws.VersionTS)
}

// workspaceTOMLKey returns the literal TOML key of the [workspaces.*]
// block configured for teamID. Blocks are keyed either by a user-chosen
// slug (with team_id set) or, for legacy configs, by the raw team ID.
// When no block exists we fall back to the team ID, which is the key
// the saveWorkspace* helpers use when they append a new block.
func workspaceTOMLKey(cfg config.Config, teamID string) string {
	for k, w := range cfg.Workspaces {
		if w.TeamID == teamID {
			return k
		}
	}
	return teamID
}

// saveWorkspaceVersionTS rewrites or appends a version_ts entry in
// [workspaces.<tomlKey>]. Mirrors saveWorkspaceWidth / saveWorkspaceTheme:
// a textual line rewrite rather than a TOML re-marshal, so user comments
// and field ordering survive the write.
func saveWorkspaceVersionTS(configPath, tomlKey, teamID, teamName, versionTS string) error {
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
			if strings.HasPrefix(t, "version_ts") && strings.Contains(t, "=") {
				lines[j] = "version_ts = " + tomlString(versionTS)
				updated = true
				break
			}
		}
		if !updated {
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, "version_ts = "+tomlString(versionTS))
			newLines = append(newLines, lines[sectionStart+1:]...)
			lines = newLines
		}
		return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
	}

	// No existing section — append a legacy-keyed block.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	commentLine := "# " + safeName
	legacyHeader := fmt.Sprintf("[workspaces.%s]", teamID)
	lines = append(lines, commentLine, legacyHeader, "version_ts = "+tomlString(versionTS))
	return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
}
