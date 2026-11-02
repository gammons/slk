package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	slackclient "github.com/gammons/slk/internal/slack"
)

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
