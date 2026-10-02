package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
	"golang.org/x/term"
)

// onboardingStyles are the lipgloss styles shared by the desktop and the
// browser sign-in flows.
type onboardingStyles struct {
	title, subtitle, step, success, errorText, dim lipgloss.Style
}

func newOnboardingStyles() onboardingStyles {
	return onboardingStyles{
		title:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4A9EFF")).MarginBottom(1),
		subtitle:  lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).MarginBottom(1),
		step:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#50C878")),
		success:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#50C878")).MarginTop(1),
		errorText: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E04040")),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")),
	}
}

// addWorkspace signs in through the Slack desktop app. When that session
// cannot be read (no desktop app, no keyring, ...) and stdin is a terminal, it
// offers the browser-session flow instead. forceBrowser skips the desktop app.
func addWorkspace(forceBrowser bool) error {
	dataDir := xdgData()
	tokenDir := filepath.Join(dataDir, "tokens")
	tokenStore := slackclient.NewTokenStore(tokenDir)

	st := newOnboardingStyles()
	titleStyle, subtitleStyle, stepStyle := st.title, st.subtitle, st.step
	successStyle, errorStyle, dimStyle := st.success, st.errorText, st.dim

	fmt.Println()
	fmt.Println(titleStyle.Render("slk -- Add Workspace"))
	if forceBrowser {
		return addWorkspaceFromBrowser(tokenStore, st)
	}
	fmt.Println(subtitleStyle.Render("Reading your signed-in workspaces from the Slack desktop app."))
	fmt.Println()

	browser := func() error { return addWorkspaceFromBrowser(tokenStore, st) }

	// Read cookie + workspaces from the desktop app.
	cookie, err := slackdesktop.Cookie()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + desktopErrorMessage(err)))
		return offerBrowserFallback(term.IsTerminal(int(os.Stdin.Fd())), askBrowserFallback, browser, err)
	}
	workspaces, err := slackdesktop.Workspaces()
	if err != nil {
		fmt.Println(errorStyle.Render("  " + desktopErrorMessage(err)))
		return offerBrowserFallback(term.IsTerminal(int(os.Stdin.Fd())), askBrowserFallback, browser, err)
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
				Height(visibleRows + 4),
		),
	).WithTheme(huh.ThemeFunc(huh.ThemeDracula))
	if err := form.Run(); err != nil {
		return fmt.Errorf("form cancelled")
	}
	selected := map[string]bool{}
	for _, id := range chosen {
		selected[id] = true
	}

	// Resolve tokens for the selected workspaces. Prefer the desktop app's
	// stored tokens (client-v2 no longer inlines them in page HTML); mint is a
	// fallback for older workspaces. A read failure here is non-fatal — mint
	// still covers those cases.
	desktopTokens, _ := slackdesktop.Tokens()

	fmt.Println()
	fmt.Println(stepStyle.Render("Connecting..."))
	tokens, err := buildWorkspaceTokens(context.Background(), cookie, desktopTokens, workspaces, selected, slackclient.MintToken)
	if err != nil {
		fmt.Println(errorStyle.Render("  Failed to obtain token: " + err.Error()))
		return err
	}

	// Validate each and save.
	for _, tok := range tokens {
		client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
		if err := client.Connect(context.Background()); err != nil {
			fmt.Println(errorStyle.Render(fmt.Sprintf("  %s: authentication failed: %v", tok.TeamName, err)))
			return fmt.Errorf("authentication failed for %s: %w", tok.TeamName, err)
		}
		if err := saveWorkspace(tokenStore, tok, st); err != nil {
			return err
		}
	}

	fmt.Println()
	fmt.Println(successStyle.Render(fmt.Sprintf("  %d workspace(s) added!", len(tokens))))
	fmt.Println(dimStyle.Render("  Run ") + lipgloss.NewStyle().Bold(true).Render("slk") + dimStyle.Render(" to start."))
	fmt.Println()
	return nil
}

// saveWorkspace persists a validated token and appends its
// [workspaces.<slug>] config block (best-effort). Re-adding a workspace
// already in config.toml (the browser flow's documented recovery when its
// session expires) only refreshes the token: a second block for the same
// team_id would make config.Load reject the file.
func saveWorkspace(tokenStore *slackclient.TokenStore, tok slackclient.Token, st onboardingStyles) error {
	if err := tokenStore.Save(tok); err != nil {
		return fmt.Errorf("saving token for %s: %w", tok.TeamName, err)
	}
	configPath := filepath.Join(xdgConfig(), "config.toml")
	cfg, err := config.Load(configPath)
	if err != nil {
		// Appending to a file that already fails to load (a duplicate an
		// earlier run left) cannot fix it, and may add one more block.
		fmt.Println(st.success.Render("  Added ") + st.dim.Render(tok.TeamName))
		fmt.Println(st.dim.Render("  Note: config.toml does not load (" + err.Error() + "), left as is."))
		return nil
	}
	if _, ok := cfg.WorkspaceByTeamID(tok.TeamID); ok {
		fmt.Println(st.success.Render("  Updated ") + st.dim.Render(tok.TeamName))
		return nil
	}
	slug := uniqueSlug(config.Slugify(tok.TeamName), existingSlugs(configPath))
	if err := appendWorkspaceConfigBlock(configPath, slug, tok.TeamID, tok.TeamName); err != nil {
		fmt.Println(st.dim.Render("  Note: could not write config.toml: " + err.Error()))
	}
	fmt.Println(st.success.Render("  Added ") + st.dim.Render(tok.TeamName))
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
	case errors.Is(err, slackdesktop.ErrSecretNotFound):
		return "No Slack entry found in your keyring or KWallet. Sign in to the Slack desktop app, then retry. " +
			"If Slack was launched with --password-store=basic it never stored a key at all, and slk cannot read that session."
	case errors.Is(err, slackdesktop.ErrDecryptFailed):
		// Keep the wrapped detail: it names which step failed (padding, length,
		// non-printable result), which is the difference between a diagnosable
		// report and a round-trip asking what actually broke.
		return "Could not decrypt the Slack session cookie: " + err.Error() +
			". If you have had more than one Slack build installed (App Store and standalone), " +
			"sign out of the one you no longer use. Otherwise please file an issue with your OS + Slack version."
	case errors.Is(err, slackdesktop.ErrCookieLocked):
		return "Your Slack cookie is locked by a running process of Slack. Close Slack and try again."
	default:
		return "Could not read Slack desktop session: " + err.Error()
	}
}
