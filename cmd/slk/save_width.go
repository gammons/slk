package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// saveWorkspaceWidth rewrites or appends a sidebar_width entry in
// [workspaces.<tomlKey>]. Mirrors saveWorkspaceTheme.
func saveWorkspaceWidth(configPath, tomlKey, teamID, teamName string, width int) error {
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
			if strings.HasPrefix(t, "sidebar_width") && strings.Contains(t, "=") {
				lines[j] = "sidebar_width = " + strconv.Itoa(width)
				updated = true
				break
			}
		}
		if !updated {
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:sectionStart+1]...)
			newLines = append(newLines, "sidebar_width = "+strconv.Itoa(width))
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
	lines = append(lines, commentLine, legacyHeader, "sidebar_width = "+strconv.Itoa(width))
	return writeConfigAtomic(configPath, []byte(strings.Join(lines, "\n")))
}
