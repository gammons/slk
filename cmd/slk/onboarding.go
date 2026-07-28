package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
)

func addWorkspace() error {
	dataDir := xdgData()
	tokenDir := filepath.Join(dataDir, "tokens")
	tokenStore := slackclient.NewTokenStore(tokenDir)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4A9EFF")).MarginBottom(1)
	subtitleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).MarginBottom(1)
	stepStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#50C878"))
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#50C878")).MarginTop(1)
	errorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E04040"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	fmt.Println()
	fmt.Println(titleStyle.Render("slk -- Add Workspace"))
	fmt.Println(subtitleStyle.Render("Reading your signed-in workspaces from the Slack desktop app."))
	fmt.Println()

	// Read cookie + workspaces from the desktop app.
	cookie, err := slackdesktop.Cookie()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + desktopErrorMessage(err)))
		return err
	}
	workspaces, err := slackdesktop.Workspaces()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + desktopErrorMessage(err)))
		return err
	}

	// Multi-select (all pre-selected).
	var opts []huh.Option[string]
	for _, w := range workspaces {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s.slack.com)", w.Name, w.Domain), w.TeamID))
	}
	chosen := make([]string, 0, len(workspaces))
	for _, w := range workspaces {
		chosen = append(chosen, w.TeamID)
	}
	// huh sizes the MultiSelect option viewport to (Height - title/description
	// lines); when Height is unset the viewport collapses to a row or two and
	// the user has to scroll a 3-item list. Size it to show every workspace
	// (capped so a very long list can't overflow a small terminal). The +4
	// covers the title + description overhead with a little slack.
	visibleRows := len(workspaces)
	if visibleRows > 12 {
		visibleRows = 12
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Workspaces to add").
				Description("All selected by default; space to toggle, enter to confirm.").
				Options(opts...).
				Value(&chosen).
				Height(visibleRows+4),
		),
	).WithTheme(huh.ThemeFunc(huh.ThemeDracula))
	if err := form.Run(); err != nil {
		return fmt.Errorf("form cancelled")
	}
	selected := map[string]bool{}
	for _, id := range chosen {
		selected[id] = true
	}

	fmt.Println()
	fmt.Println(stepStyle.Render("Connecting..."))
	tokens := buildWorkspaceTokens(cookie, workspaces, selected)

	// Validate each and save.
	for _, tok := range tokens {
		client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
		if err := client.Connect(context.Background()); err != nil {
			fmt.Println(errorStyle.Render(fmt.Sprintf("  %s: authentication failed: %v", tok.TeamName, err)))
			return fmt.Errorf("authentication failed for %s: %w", tok.TeamName, err)
		}
		if err := tokenStore.Save(tok); err != nil {
			return fmt.Errorf("saving token for %s: %w", tok.TeamName, err)
		}

		// Append a [workspaces.<slug>] config block (best-effort).
		configPath := filepath.Join(xdgConfig(), "config.toml")
		slug := uniqueSlug(config.Slugify(tok.TeamName), existingSlugs(configPath))
		if err := appendWorkspaceConfigBlock(configPath, slug, tok.TeamID, tok.TeamName); err != nil {
			fmt.Println(dimStyle.Render("  Note: could not write config.toml: " + err.Error()))
		}
		fmt.Println(successStyle.Render("  Added ") + dimStyle.Render(tok.TeamName))
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("  %d workspace(s) added!", len(tokens))))
	fmt.Println(dimStyle.Render("  Run ") + lipgloss.NewStyle().Bold(true).Render("slk") + dimStyle.Render(" to start."))
	fmt.Println()
	return nil
}

// desktopErrorMessage maps a slackdesktop error to an actionable message.
func desktopErrorMessage(err error) string {
	switch {
	case errors.Is(err, slackdesktop.ErrDesktopNotFound):
		return "Slack desktop app not found. Install it and sign in, then retry."
	case errors.Is(err, slackdesktop.ErrNotSignedIn):
		return "No Slack workspaces are signed in. Open Slack, sign in, then retry."
	case errors.Is(err, slackdesktop.ErrCookieDBMissing):
		return "Slack is installed but has never signed in on this machine."
	case errors.Is(err, slackdesktop.ErrKeyringLocked):
		return "Your system keyring is locked. Unlock it (log in to your desktop session) and retry."
	case errors.Is(err, slackdesktop.ErrNoSecretService):
		return "No system keyring/secret service found. slk needs it to read the Slack session."
	case errors.Is(err, slackdesktop.ErrDecryptFailed):
		return "Could not decrypt the Slack session cookie. Please file an issue with your OS + Slack version."
	default:
		return "Could not read Slack desktop session: " + err.Error()
	}
}
