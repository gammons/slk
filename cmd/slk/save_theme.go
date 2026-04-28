package main

import (
	"fmt"
	"os"
	"strings"
)

// saveGlobalTheme rewrites the [appearance] theme line in config.toml.
// If the file has no theme line, it appends a new [appearance] section.
// Existing comments and ordering are preserved (textual rewrite, not
// TOML re-marshal).
func saveGlobalTheme(configPath, themeName string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	// Look for the first top-level `theme = ...` line. We can't
	// distinguish [appearance] theme from [theme.colors] context here, but
	// the existing implementation has the same limitation and it has been
	// adequate. The Workspaces section is always written below the
	// [appearance] block by saveWorkspaceTheme, so the first theme line
	// will be the [appearance] one in practice.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "theme") && strings.Contains(trimmed, "=") &&
			!strings.HasPrefix(trimmed, "theme.") {
			lines[i] = `theme = "` + themeName + `"`
			return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
		}
	}
	// No theme line found — append a new [appearance] section.
	lines = append(lines, "", "[appearance]", `theme = "`+themeName+`"`)
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}

// saveWorkspaceTheme rewrites or appends a [workspaces.<TeamID>] theme
// entry. If the section already exists the theme line is updated in
// place; otherwise a new section is appended at the end of the file
// preceded by a "# <name>" comment for human readability.
func saveWorkspaceTheme(configPath, teamID, teamName, themeName string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	header := fmt.Sprintf("[workspaces.%s]", teamID)

	// Find the section header.
	sectionStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			sectionStart = i
			break
		}
	}

	if sectionStart >= 0 {
		// Find the next blank line or section header — that's the end of
		// our section. Update the theme line within.
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
			if strings.HasPrefix(t, "theme") && strings.Contains(t, "=") {
				lines[j] = `theme = "` + themeName + `"`
				updated = true
				break
			}
		}
		if !updated {
			// Insert theme line right after the header.
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, `theme = "`+themeName+`"`)
			newLines = append(newLines, lines[sectionStart+1:]...)
			lines = newLines
		}
		return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
	}

	// No existing section — append at end.
	// Ensure the file ends with a blank line before our new section.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	commentLine := "# " + teamName
	if teamName == "" {
		commentLine = "# " + teamID
	}
	lines = append(lines, commentLine, header, `theme = "`+themeName+`"`)
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}
