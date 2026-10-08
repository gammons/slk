package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

func TestParseExportArgs_AllFlags(t *testing.T) {
	var out bytes.Buffer
	got, err := parseExportArgs([]string{
		"--workspace", "example-workspace",
		"--channel", "project_alpha",
		"--since", "2026-04-01",
		"--until", "2026-07-01",
		"--timezone", "America/New_York",
		"--output", "./project-alpha-export",
		"--overlap", "14",
	}, &out)
	if err != nil {
		t.Fatalf("parseExportArgs: %v", err)
	}
	want := exportOptions{
		Workspace: "example-workspace",
		Channel:   "project_alpha",
		Since:     "2026-04-01",
		Until:     "2026-07-01",
		Timezone:  "America/New_York",
		Output:    "./project-alpha-export",
		Overlap:   14,
	}
	if got != want {
		t.Errorf("options = %+v\nwant %+v", got, want)
	}
}

func TestParseExportArgs_Defaults(t *testing.T) {
	var out bytes.Buffer
	got, err := parseExportArgs([]string{"--channel", "general", "--since", "2026-04-01"}, &out)
	if err != nil {
		t.Fatalf("parseExportArgs: %v", err)
	}
	if want := (exportOptions{Channel: "general", Since: "2026-04-01"}); got != want {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}

func TestParseExportArgs_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"missing channel", []string{"--since", "2026-04-01"}, "--channel is required"},
		{"missing since", []string{"--channel", "general"}, "--since is required"},
		{"stray argument", []string{"--channel", "general", "--since", "2026-04-01", "extra"}, `unexpected argument "extra"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			_, err := parseExportArgs(tc.args, &out)
			if err == nil || err.Error() != tc.wantMsg {
				t.Errorf("err = %v, want %q", err, tc.wantMsg)
			}
		})
	}
}

func TestParseExportArgs_FlagErrorsAreReportedOnce(t *testing.T) {
	for _, args := range [][]string{{"--bogus"}, {"--overlap", "many"}} {
		var out bytes.Buffer
		_, err := parseExportArgs(args, &out)
		if !errors.Is(err, errExportUsage) {
			t.Errorf("%v: err = %v, want errExportUsage", args, err)
		}
		if !strings.Contains(out.String(), "Usage of slk export") {
			t.Errorf("%v: usage not printed, got %q", args, out.String())
		}
	}
}

func TestParseExportArgs_Help(t *testing.T) {
	var out bytes.Buffer
	_, err := parseExportArgs([]string{"--help"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out.String(), "-overlap") {
		t.Errorf("help does not list the flags, got %q", out.String())
	}
}

// exportTestTokens is two configured workspaces.
func exportTestTokens() []config.OrderedToken {
	return []config.OrderedToken{
		{Slug: "example-workspace", Token: slackclient.Token{TeamID: "T1", TeamName: "Example Inc", Domain: "example"}},
		{Slug: "", Token: slackclient.Token{TeamID: "T2", TeamName: "Side Project", Domain: "side"}},
	}
}

func TestSelectExportToken_Matches(t *testing.T) {
	cases := []struct {
		workspace string
		wantTeam  string
	}{
		{"example-workspace", "T1"}, // slug
		{"EXAMPLE-Workspace", "T1"}, // case-insensitive
		{"T2", "T2"},                // team ID
		{"side project", "T2"},      // team name
		{"side", "T2"},              // domain
	}
	for _, tc := range cases {
		got, err := selectExportToken(exportTestTokens(), tc.workspace)
		if err != nil {
			t.Errorf("%q: %v", tc.workspace, err)
			continue
		}
		if got.TeamID != tc.wantTeam {
			t.Errorf("%q: team = %q, want %q", tc.workspace, got.TeamID, tc.wantTeam)
		}
	}
}

func TestSelectExportToken_EmptyWorkspace(t *testing.T) {
	only := exportTestTokens()[:1]
	got, err := selectExportToken(only, "")
	if err != nil || got.TeamID != "T1" {
		t.Errorf("single workspace: team = %q, err = %v; want T1, nil", got.TeamID, err)
	}
	if _, err := selectExportToken(exportTestTokens(), ""); err == nil || !strings.Contains(err.Error(), "--workspace is required") {
		t.Errorf("two workspaces: err = %v, want a --workspace is required error", err)
	}
}

func TestSelectExportToken_Errors(t *testing.T) {
	if _, err := selectExportToken(nil, "anything"); err == nil || !strings.Contains(err.Error(), "no workspaces configured") {
		t.Errorf("no tokens: err = %v", err)
	}
	if _, err := selectExportToken(exportTestTokens(), "nope"); err == nil || !strings.Contains(err.Error(), `workspace "nope" not found`) {
		t.Errorf("unknown workspace: err = %v", err)
	}
	// An unset slug must not match an empty-looking request for it.
	if _, err := selectExportToken(exportTestTokens()[1:], " "); err == nil {
		t.Error("blank workspace matched a token with an empty slug")
	}
}

// exportTestChannel builds a channel with the given ID and name.
func exportTestChannel(id, name string) slack.Channel {
	var ch slack.Channel
	ch.ID = id
	ch.Name = name
	return ch
}

func TestFindExportChannel(t *testing.T) {
	channels := []slack.Channel{
		exportTestChannel("C1", "general"),
		exportTestChannel("C2", "project_alpha"),
		exportTestChannel("D1", ""),
	}
	cases := []struct {
		want   string
		wantID string
	}{
		{"project_alpha", "C2"},
		{"#project_alpha", "C2"},
		{"Project_Alpha", "C2"},
		{"C1", "C1"},
		{"D1", "D1"},
	}
	for _, tc := range cases {
		got, ok := findExportChannel(channels, tc.want)
		if !ok || got.ID != tc.wantID {
			t.Errorf("findExportChannel(%q) = %q, %v; want %q", tc.want, got.ID, ok, tc.wantID)
		}
	}
	for _, miss := range []string{"nope", "", "#"} {
		if got, ok := findExportChannel(channels, miss); ok {
			t.Errorf("findExportChannel(%q) matched %q, want no match", miss, got.ID)
		}
	}
}

func TestExportChannelLabel(t *testing.T) {
	if got := exportChannelLabel(exportTestChannel("C1", "general")); got != "general" {
		t.Errorf("label = %q, want general", got)
	}
	if got := exportChannelLabel(exportTestChannel("D1", "")); got != "D1" {
		t.Errorf("DM label = %q, want its ID", got)
	}
}
