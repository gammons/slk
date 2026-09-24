// This file implements the `slk export` subcommand: it parses the
// flags, picks the workspace and channel, and drives the fetch in
// export_collect.go into the Markdown writer in internal/export. It
// connects with the stored workspace credentials and never starts the
// TUI or the WebSocket.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/export"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackdesktop"
	"github.com/slack-go/slack"
)

// exportOptions holds the parsed `slk export` flags.
type exportOptions struct {
	Workspace string
	Channel   string
	Since     string
	Until     string
	Timezone  string
	Output    string
	Overlap   int
}

// errExportUsage marks a flag error whose message the flag package has
// already printed along with the usage text.
var errExportUsage = errors.New("invalid export arguments")

// parseExportArgs parses the arguments after `slk export`. It returns
// flag.ErrHelp when help was requested, and errExportUsage when the
// flag package already reported the problem on output.
func parseExportArgs(args []string, output io.Writer) (exportOptions, error) {
	var opts exportOptions
	fs := flag.NewFlagSet("slk export", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&opts.Workspace, "workspace", "", "workspace slug, team ID or name (required when more than one is configured)")
	fs.StringVar(&opts.Channel, "channel", "", "channel name or ID (required)")
	fs.StringVar(&opts.Since, "since", "", "first day to export, YYYY-MM-DD, inclusive (required)")
	fs.StringVar(&opts.Until, "until", "", "day to stop before, YYYY-MM-DD, exclusive (default: tomorrow)")
	fs.StringVar(&opts.Timezone, "timezone", "", "IANA timezone the dates are read in (default: local)")
	fs.StringVar(&opts.Output, "output", "", "directory to write, which must be empty or absent (default: under the slk exports directory)")
	fs.IntVar(&opts.Overlap, "overlap", 0, "calendar days to add before --since and after --until")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exportOptions{}, err
		}
		return exportOptions{}, errExportUsage
	}
	if fs.NArg() > 0 {
		return exportOptions{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if opts.Channel == "" {
		return exportOptions{}, errors.New("--channel is required")
	}
	if opts.Since == "" {
		return exportOptions{}, errors.New("--since is required")
	}
	return opts, nil
}

// selectExportToken picks the token for the requested workspace,
// matched case-insensitively against the config slug, team ID, team
// name and domain. An empty workspace is allowed only when exactly one
// workspace is configured.
func selectExportToken(tokens []config.OrderedToken, workspace string) (slackclient.Token, error) {
	if len(tokens) == 0 {
		return slackclient.Token{}, errors.New("no workspaces configured; run 'slk --add-workspace' first")
	}
	if workspace == "" {
		if len(tokens) == 1 {
			return tokens[0].Token, nil
		}
		return slackclient.Token{}, errors.New("--workspace is required when more than one workspace is configured; see 'slk --list-workspaces'")
	}
	for _, ot := range tokens {
		for _, candidate := range []string{ot.Slug, ot.Token.TeamID, ot.Token.TeamName, ot.Token.Domain} {
			if candidate != "" && strings.EqualFold(candidate, workspace) {
				return ot.Token, nil
			}
		}
	}
	return slackclient.Token{}, fmt.Errorf("workspace %q not found; see 'slk --list-workspaces'", workspace)
}

// findExportChannel matches want, a channel name (with or without a
// leading #) or a channel ID, against channels. ok is false when
// nothing matches.
func findExportChannel(channels []slack.Channel, want string) (slack.Channel, bool) {
	name := strings.TrimPrefix(want, "#")
	for _, ch := range channels {
		if ch.ID == want || (ch.Name != "" && strings.EqualFold(ch.Name, name)) {
			return ch, true
		}
	}
	return slack.Channel{}, false
}

// exportChannelLabel is the name a channel is exported under: its
// Slack name, or its ID for DMs, which have none.
func exportChannelLabel(ch slack.Channel) string {
	if ch.Name != "" {
		return ch.Name
	}
	return ch.ID
}

// resolveExportChannel finds the channel to export and returns it with
// an ID-to-name map of the user's conversations for rendering channel
// references. A channel the user has not joined is not in that list,
// so an unmatched want is tried as a channel ID via conversations.info.
func resolveExportChannel(ctx context.Context, client *slackclient.Client, want string) (slack.Channel, map[string]string, error) {
	channels, err := client.GetChannels(ctx)
	if err != nil {
		return slack.Channel{}, nil, err
	}
	channelNames := make(map[string]string, len(channels))
	for _, ch := range channels {
		if ch.Name != "" {
			channelNames[ch.ID] = ch.Name
		}
	}
	if ch, ok := findExportChannel(channels, want); ok {
		return ch, channelNames, nil
	}
	info, err := client.GetConversationInfo(ctx, want)
	if err != nil {
		return slack.Channel{}, nil, fmt.Errorf("channel %q is not one you have joined and is not a channel ID: %w", want, err)
	}
	return *info, channelNames, nil
}

// openExportCache opens the message cache for user-name lookups. The
// cache only saves API calls, so a failure is reported and tolerated:
// the nil DB it returns makes every lookup fall through to Slack.
func openExportCache(warn io.Writer) *cache.DB {
	db, err := cache.New(filepath.Join(xdgData(), "cache.db"))
	if err != nil {
		fmt.Fprintf(warn, "Warning: cache unavailable, resolving all names from Slack: %v\n", err)
		return nil
	}
	return db
}

// exportChannel runs `slk export` with the arguments that follow the
// subcommand name.
func exportChannel(args []string) error {
	opts, err := parseExportArgs(args, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	win, err := export.NewWindow(opts.Since, opts.Until, opts.Timezone, opts.Overlap, time.Now())
	if err != nil {
		return err
	}

	store := slackclient.NewTokenStore(filepath.Join(xdgData(), "tokens"))
	tokens, err := store.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}
	cfg, _ := config.Load(filepath.Join(xdgConfig(), "config.toml")) // best-effort, as in listWorkspaces
	tok, err := selectExportToken(config.OrderTokens(tokens, cfg), opts.Workspace)
	if err != nil {
		return err
	}

	// Ctrl-C or SIGTERM cancels ctx, which the history walks, reply
	// fetches and rate-limit waits all watch. Once it fires, default
	// signal handling is restored so a second Ctrl-C still kills the
	// process during any step that does not watch ctx.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)
	// xoxc tokens expire; refresh from the desktop app as the TUI does
	// at launch. remintTokens keeps the stored token on any failure.
	tok = remintTokens(ctx, []slackclient.Token{tok}, slackdesktop.Cookie, slackdesktop.Tokens, slackclient.MintToken, store.Save)[0]
	client := slackclient.NewClient(tok.AccessToken, tok.Cookie)
	if err := client.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to %s: %w", tok.TeamName, err)
	}

	channel, channelNames, err := resolveExportChannel(ctx, client, opts.Channel)
	if err != nil {
		return err
	}
	label := exportChannelLabel(channel)
	outDir := opts.Output
	if outDir == "" {
		if outDir, err = export.DefaultChannelDir(label, win); err != nil {
			return err
		}
	}
	if err := export.EnsureEmptyDir(outDir); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Exporting #%s from %s\n", label, tok.TeamName)
	raw, err := collectConversations(ctx, client, channel.ID, win, os.Stderr)
	if err != nil {
		return err
	}

	db := openExportCache(os.Stderr)
	if db != nil {
		defer db.Close()
	}
	userNames := make(map[string]string)
	convs := exportConversations(raw, userNames, db)
	if err := resolveExportNames(ctx, convs, userNames, db, client, os.Stderr); err != nil {
		return err
	}

	indexPath, err := export.WriteChannel(outDir, export.Channel{
		Workspace:     tok.TeamName,
		Name:          label,
		Window:        win,
		Conversations: convs,
	}, userNames, channelNames)
	if err != nil {
		return err
	}
	fmt.Printf("Exported %d conversations to %s\n", len(convs), indexPath)
	return nil
}
