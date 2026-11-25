package main

import (
	"context"
	"log"

	"github.com/gammons/slk/internal/core"
)

// appCommandLister is the part of *slackclient.Client the command-list
// fetch needs.
type appCommandLister interface {
	AppCommands(ctx context.Context) ([]core.SlashCommand, error)
}

// fetchAppCommands loads client.appCommands once per connect. A failure
// leaves the list empty: commands still run, and the picker falls back
// to slk's built-ins. There is no UI message — the picker pulls the
// list the next time it opens.
func fetchAppCommands(ctx context.Context, wctx *WorkspaceContext, client appCommandLister) {
	if wctx == nil || client == nil {
		return
	}
	cmds, err := client.AppCommands(ctx)
	if err != nil {
		log.Printf("app commands fetch for %s failed: %v (slash completion stays closed; commands still run)", wctx.TeamName, err)
		return
	}
	wctx.SetAppCommands(cmds)
}
