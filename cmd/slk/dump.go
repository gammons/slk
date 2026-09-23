package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
)

// listWorkspaces prints the configured workspaces with their TeamID and
// Name, one per line. Useful for users who want to hand-edit per-workspace
// settings in config.toml.
func listWorkspaces() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}
	configPath := filepath.Join(xdgConfig(), "config.toml")
	cfg, _ := config.Load(configPath) // best-effort

	// Print in the same order the rail would use, so the digit-key
	// mapping is obvious from the output.
	orderedTokens := config.OrderTokens(tokens, cfg)

	idW, slugW, nameW := len("TEAM ID"), len("SLUG"), len("NAME")
	for _, ot := range orderedTokens {
		if len(ot.Token.TeamID) > idW {
			idW = len(ot.Token.TeamID)
		}
		if len(ot.Slug) > slugW {
			slugW = len(ot.Slug)
		}
		if len(ot.Token.TeamName) > nameW {
			nameW = len(ot.Token.TeamName)
		}
	}
	fmt.Printf("%-*s  %-*s  %s\n", idW, "TEAM ID", slugW, "SLUG", "NAME")
	fmt.Printf("%s  %s  %s\n",
		strings.Repeat("-", idW),
		strings.Repeat("-", slugW),
		strings.Repeat("-", nameW))
	for _, ot := range orderedTokens {
		fmt.Printf("%-*s  %-*s  %s\n", idW, ot.Token.TeamID, slugW, ot.Slug, ot.Token.TeamName)
	}
	return nil
}

// dumpPrefs is a diagnostic command that calls users.prefs.get for
// every configured workspace and prints the raw JSON response. Use
// this when the muted-channel UI treatment isn't behaving as
// expected to confirm what Slack is (or isn't) returning for the
// muted_channels pref.
func dumpPrefs() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}
	ctx := context.Background()
	for _, tok := range tokens {
		fmt.Printf("=== %s (%s) ===\n", tok.TeamName, tok.TeamID)
		client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
		if err := client.Connect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  connect failed: %v\n\n", err)
			continue
		}
		raw, err := client.GetMutedChannelsRaw(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  fetch failed: %v\n\n", err)
			continue
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(raw))
		}
		fmt.Println()
	}
	return nil
}

// dumpSections is a diagnostic command that calls users.channelSections.list
// for every configured workspace and prints the raw JSON response, pretty-
// printed. Intended for reverse-engineering the undocumented endpoint; safe
// to remove once we ship server-side section support.
func dumpSections() error {
	tokenDir := filepath.Join(xdgData(), "tokens")
	store := slackclient.NewTokenStore(tokenDir)
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	if len(tokens) == 0 {
		fmt.Println("No workspaces configured. Run 'slk --add-workspace' first.")
		return nil
	}

	ctx := context.Background()
	for _, tok := range tokens {
		fmt.Printf("=== %s (%s) ===\n", tok.TeamName, tok.TeamID)
		client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
		// Connect resolves the per-workspace API base URL via auth.test;
		// required for enterprise grid hosts.
		if err := client.Connect(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "  connect failed: %v\n\n", err)
			continue
		}
		raw, err := client.GetChannelSectionsRaw(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  fetch failed: %v\n\n", err)
			continue
		}
		// Pretty-print if it parses as JSON; otherwise dump raw.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(raw))
		}
		// Detect pagination truncation. GetChannelSectionsRaw is intentionally
		// first-page-only for the diagnostic; warn so the user knows.
		var trunc struct {
			Cursor string `json:"cursor"`
		}
		if err := json.Unmarshal(raw, &trunc); err == nil && trunc.Cursor != "" {
			fmt.Fprintf(os.Stderr, "  warning: response cursor=%q; additional sections beyond first page were not fetched\n", trunc.Cursor)
		}
		fmt.Println()
	}
	return nil
}
