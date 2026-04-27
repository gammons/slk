package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"charm.land/huh/v2/spinner"
	"charm.land/lipgloss/v2"
	slackclient "github.com/gammons/slk/internal/slack"
)

func addWorkspace() error {
	dataDir := xdgData()
	tokenDir := filepath.Join(dataDir, "tokens")
	tokenStore := slackclient.NewTokenStore(tokenDir)

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#4A9EFF")).
		MarginBottom(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginBottom(1)

	stepStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#50C878"))

	successStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#50C878")).
		MarginTop(1)

	errorStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#E04040"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666"))

	// Welcome
	fmt.Println()
	fmt.Println(titleStyle.Render("slk -- Add Workspace"))
	fmt.Println(subtitleStyle.Render("Connect a Slack workspace using your browser session."))
	fmt.Println()

	// Step 1: Instructions
	fmt.Println(stepStyle.Render("Step 1: Get your browser tokens"))
	fmt.Println()
	fmt.Println(dimStyle.Render("  a. Open Slack in your browser and log into your workspace"))
	fmt.Println(dimStyle.Render("  b. Open DevTools (F12 or Cmd+Option+I)"))
	fmt.Println(dimStyle.Render("  c. Go to Application > Cookies > https://app.slack.com"))
	fmt.Println(dimStyle.Render("     Find the cookie named 'd' and copy its value"))
	fmt.Println(dimStyle.Render("  d. Go to the Console tab and run:"))
	fmt.Println(dimStyle.Render("     Object.entries(JSON.parse(localStorage.localConfig_v2).teams).forEach(([id,t]) => console.log(t.name, t.token))"))
	fmt.Println(dimStyle.Render("     This prints the name and xoxc token for each workspace."))
	fmt.Println(dimStyle.Render("     Copy the xoxc-... token for the workspace you want to add."))
	fmt.Println()

	// Step 2: Enter tokens via huh form
	fmt.Println(stepStyle.Render("Step 2: Enter your tokens"))
	fmt.Println()

	var xoxcToken, dCookie string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Cookie (d)").
				Description("The 'd' cookie value from Application > Cookies").
				Placeholder("xoxd-...").
				Value(&dCookie).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("cookie is required")
					}
					return nil
				}),

			huh.NewInput().
				Title("Token (xoxc)").
				Description("The xoxc-... token from your browser console").
				Placeholder("xoxc-...").
				Value(&xoxcToken).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("token is required")
					}
					if !strings.HasPrefix(s, "xoxc-") {
						return fmt.Errorf("must start with xoxc-")
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeDracula())

	err := form.Run()
	if err != nil {
		return fmt.Errorf("form cancelled")
	}

	xoxcToken = strings.TrimSpace(xoxcToken)
	dCookie = strings.TrimSpace(dCookie)

	// Step 3: Validate tokens with spinner
	fmt.Println()
	fmt.Println(stepStyle.Render("Step 3: Validating tokens..."))

	var client *slackclient.Client
	var connectErr error

	spinErr := spinner.New().
		Title("Connecting to Slack...").
		Action(func() {
			client = slackclient.NewClient(xoxcToken, dCookie)
			connectErr = client.Connect(context.Background())
		}).
		Run()

	if spinErr != nil {
		return fmt.Errorf("spinner error: %w", spinErr)
	}

	if connectErr != nil {
		fmt.Println(errorStyle.Render("  Authentication failed: " + connectErr.Error()))
		fmt.Println()
		fmt.Println(dimStyle.Render("  Make sure you're logged into Slack in your browser"))
		fmt.Println(dimStyle.Render("  and that you copied the correct token and cookie values."))
		return fmt.Errorf("authentication failed: %w", connectErr)
	}

	teamID := client.TeamID()
	fmt.Println(successStyle.Render("  Connected!") + dimStyle.Render(fmt.Sprintf(" (team: %s)", teamID)))
	fmt.Println()

	// Step 4: Workspace name
	fmt.Println(stepStyle.Render("Step 4: Name your workspace"))
	fmt.Println()

	var wsName string
	nameForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Display Name").
				Description("A friendly name for this workspace (e.g., 'Acme Corp')").
				Placeholder(teamID).
				Value(&wsName),
		),
	).WithTheme(huh.ThemeDracula())

	if err := nameForm.Run(); err != nil {
		wsName = teamID
	}

	wsName = strings.TrimSpace(wsName)
	if wsName == "" {
		wsName = teamID
	}

	// Save
	token := slackclient.Token{
		AccessToken: xoxcToken,
		Cookie:      dCookie,
		TeamID:      teamID,
		TeamName:    wsName,
	}

	if err := tokenStore.Save(token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("  Workspace '%s' added successfully!", wsName)))
	fmt.Println()
	fmt.Println(dimStyle.Render("  Run ") + lipgloss.NewStyle().Bold(true).Render("slk") + dimStyle.Render(" to start."))
	fmt.Println()

	return nil
}
