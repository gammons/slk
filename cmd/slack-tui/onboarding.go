package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	slackclient "github.com/gammons/slack-tui/internal/slack"
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

	urlStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4A9EFF")).
		Underline(true)

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
	fmt.Println(titleStyle.Render("slack-tui -- Add Workspace"))
	fmt.Println(subtitleStyle.Render("Connect a Slack workspace to your terminal client."))
	fmt.Println()

	// Step 1: Create Slack App
	fmt.Println(stepStyle.Render("Step 1: Create a Slack App"))
	fmt.Println()
	fmt.Println("  " + urlStyle.Render("https://api.slack.com/apps?new_app=1"))
	fmt.Println()
	fmt.Println(dimStyle.Render("  a. Select 'From scratch', name it (e.g. 'slack-tui'), pick your workspace"))
	fmt.Println(dimStyle.Render("  b. Enable Socket Mode (left sidebar) -- create an app token with connections:write"))
	fmt.Println(dimStyle.Render("  c. OAuth & Permissions -- add these User Token Scopes:"))
	fmt.Println(dimStyle.Render("     channels:read, channels:history, groups:read, groups:history,"))
	fmt.Println(dimStyle.Render("     im:read, im:history, im:write, mpim:read, mpim:history, mpim:write,"))
	fmt.Println(dimStyle.Render("     chat:write, reactions:read, reactions:write, files:read, files:write,"))
	fmt.Println(dimStyle.Render("     users:read, search:read, team:read"))
	fmt.Println(dimStyle.Render("  d. Install the app to your workspace"))
	fmt.Println(dimStyle.Render("  e. Event Subscriptions -- subscribe to user events:"))
	fmt.Println(dimStyle.Render("     message.channels, message.groups, message.im, message.mpim"))
	fmt.Println()

	// Step 2: Enter tokens via huh form
	fmt.Println(stepStyle.Render("Step 2: Enter your tokens"))
	fmt.Println()

	var appToken, userToken string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("App-Level Token").
				Description("Found at: Basic Information > App-Level Tokens").
				Placeholder("xapp-...").
				Value(&appToken).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("token is required")
					}
					if !strings.HasPrefix(s, "xapp-") {
						return fmt.Errorf("must start with xapp-")
					}
					return nil
				}),

			huh.NewInput().
				Title("User OAuth Token").
				Description("Found at: OAuth & Permissions > User OAuth Token").
				Placeholder("xoxp-...").
				Value(&userToken).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return fmt.Errorf("token is required")
					}
					if !strings.HasPrefix(s, "xoxp-") {
						return fmt.Errorf("must start with xoxp-")
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeDracula())

	err := form.Run()
	if err != nil {
		return fmt.Errorf("form cancelled")
	}

	appToken = strings.TrimSpace(appToken)
	userToken = strings.TrimSpace(userToken)

	// Step 3: Validate tokens with spinner
	fmt.Println()
	fmt.Println(stepStyle.Render("Step 3: Validating tokens..."))

	var client *slackclient.Client
	var connectErr error

	spinErr := spinner.New().
		Title("Connecting to Slack...").
		Action(func() {
			client = slackclient.NewClient(userToken, appToken)
			connectErr = client.Connect(context.Background())
		}).
		Run()

	if spinErr != nil {
		return fmt.Errorf("spinner error: %w", spinErr)
	}

	if connectErr != nil {
		fmt.Println(errorStyle.Render("  Authentication failed: " + connectErr.Error()))
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
		AccessToken: userToken,
		AppToken:    appToken,
		TeamID:      teamID,
		TeamName:    wsName,
	}

	if err := tokenStore.Save(token); err != nil {
		return fmt.Errorf("saving token: %w", err)
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("  Workspace '%s' added successfully!", wsName)))
	fmt.Println()
	fmt.Println(dimStyle.Render("  Run ") + lipgloss.NewStyle().Bold(true).Render("slack-tui") + dimStyle.Render(" to start."))
	fmt.Println()

	return nil
}
