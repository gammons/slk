package main

import (
	"fmt"
	"path/filepath"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	slackclient "github.com/gammons/slk/internal/slack"
)

// removeWorkspace presents an interactive picker of configured
// workspaces and deletes the selected workspace's token file from
// disk. It does not touch config.toml or the SQLite cache; users can
// hand-edit those if desired.
func removeWorkspace() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)

	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("listing tokens: %w", err)
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#4A9EFF")).
		MarginBottom(1)
	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))
	successStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#50C878")).
		MarginTop(1)

	fmt.Println()
	fmt.Println(titleStyle.Render("slk -- Remove Workspace"))

	if len(tokens) == 0 {
		fmt.Println(dimStyle.Render("  No workspaces configured. Nothing to remove."))
		fmt.Println()
		return nil
	}

	options := make([]huh.Option[string], 0, len(tokens))
	for _, t := range tokens {
		label := fmt.Sprintf("%s  (%s)", t.TeamName, t.TeamID)
		options = append(options, huh.NewOption(label, t.TeamID))
	}

	var teamID string
	var confirm bool

	pickForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a workspace to remove").
				Description("This deletes the saved token from disk. config.toml and the SQLite cache are left untouched.").
				Options(options...).
				Value(&teamID),
			huh.NewConfirm().
				Title("Are you sure?").
				Description("This cannot be undone (you can re-add with --add-workspace).").
				Affirmative("Remove").
				Negative("Cancel").
				Value(&confirm),
		),
	).WithTheme(huh.ThemeFunc(huh.ThemeDracula))

	if err := pickForm.Run(); err != nil {
		return fmt.Errorf("cancelled")
	}

	if !confirm {
		fmt.Println()
		fmt.Println(dimStyle.Render("  Cancelled. No changes made."))
		fmt.Println()
		return nil
	}

	// Find the selected token's display name for the success message.
	var teamName string
	for _, t := range tokens {
		if t.TeamID == teamID {
			teamName = t.TeamName
			break
		}
	}

	if err := store.Delete(teamID); err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}

	fmt.Println(successStyle.Render(fmt.Sprintf("  Removed workspace '%s' (%s).", teamName, teamID)))
	fmt.Println(dimStyle.Render("  Note: config.toml and cache.db were not modified."))
	fmt.Println()

	return nil
}
