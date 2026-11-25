package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
)

// Defaults so the App can dispatch to a service before one is wired
// (typically tests that don't exercise it).
var (
	noopChannelService     = core.NewChannelService(core.ChannelServiceFuncs{})
	noopMessageService     = core.NewMessageService(core.MessageServiceFuncs{})
	noopThreadService      = core.NewThreadService(core.ThreadServiceFuncs{})
	noopActivityService    = core.NewActivityService(core.ActivityServiceFuncs{})
	noopReactionService    = core.NewReactionService(nil, nil, nil, nil)
	noopInteractionService = core.NewInteractionService(core.InteractionServiceFuncs{})
	noopCommandService     = core.NewCommandService(core.CommandServiceFuncs{})
	noopAppHomeService     = core.NewAppHomeService(core.AppHomeServiceFuncs{})
	noopAppViewService     = core.NewAppViewService(core.AppViewServiceFuncs{})
	noopSearchService      = core.NewSearchService(core.SearchServiceFuncs{})
	noopDesktopService     = core.NewDesktopService(core.DesktopServiceFuncs{})
	noopEditorService      = core.NewEditorService(nil, nil, nil)
	noopProfileService     = core.NewProfileService(core.ProfileServiceFuncs{})
)

// teaCmd adapts a service's deferred work to a tea.Cmd, keeping nil nil
// so callers can still tell "nothing to do" apart.
func teaCmd(c core.Cmd) tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg { return c() }
}
