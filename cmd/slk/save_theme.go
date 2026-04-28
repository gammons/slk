package main

import (
	"fmt"
	"os"
	"strings"
)

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
	data, err := os.ReadFile(configPath)
	if err != nil {
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
			return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
		}
	}
	// No [appearance] theme line found — append a new section.
	lines = append(lines, "", "[appearance]", "theme = "+tomlString(themeName))
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
				lines[j] = "theme = " + tomlString(themeName)
				updated = true
				break
			}
		}
		if !updated {
			// Insert theme line right after the header.
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, "theme = "+tomlString(themeName))
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
	safeName := sanitizeComment(teamName)
	if safeName == "" {
		safeName = teamID
	}
	commentLine := "# " + safeName
	lines = append(lines, commentLine, header, "theme = "+tomlString(themeName))
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0644)
}
