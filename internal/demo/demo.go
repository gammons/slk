package demo

import (
	"context"
	"image"
	"time"

	tea "charm.land/bubbletea/v2"

	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/imgrender"
	"github.com/gammons/slk/internal/ui/workspace"
)

// connected is ui.ConnectionStateMsg's State: 0 connecting, 1 connected,
// 2 disconnected.
const connected = 1

// Demo is one demo session: the World, the director playing the chosen
// scenario, the avatars and the images.
type Demo struct {
	world    *World
	director *Director
	avatars  map[string]string // user ID -> rendered tile; read-only after newDemo
	images   imageStore

	// proto and cellPx are how Install renders images: half-block at
	// 8x16 unless UseImageProtocol says otherwise.
	proto  imgpkg.Protocol
	cellPx image.Point
}

// New builds a demo session for scenario, with every fixture time
// relative to now. The error for an unknown scenario lists the valid ones.
func New(scenario string, now time.Time) (*Demo, error) {
	return newDemo(scenario, now, time.Now)
}

func newDemo(scenario string, now time.Time, clock func() time.Time) (*Demo, error) {
	rules, err := rulesFor(scenario)
	if err != nil {
		return nil, err
	}
	images, err := newImageStore()
	if err != nil {
		return nil, err
	}
	w := newWorld(fixtureTeams(), now, clock)
	d := &Demo{
		world:    w,
		director: newDirector(w, rules),
		avatars:  map[string]string{},
		images:   images,
		proto:    imgpkg.ProtoHalfBlock,
		cellPx:   image.Pt(8, 16),
	}
	for _, s := range w.snapshots() {
		for id, name := range s.userNames {
			d.avatars[id] = renderAvatar(id, name)
		}
	}
	return d, nil
}

// InitialTheme is the theme of the workspace active at startup, to apply
// before the first frame.
func (d *Demo) InitialTheme() string { return d.world.snapshots()[0].theme }

// Install wires the demo's workspaces and services into app.
func (d *Demo) Install(app *ui.App) {
	snaps := d.world.snapshots()
	names := make([]string, 0, len(snaps))
	rail := make([]workspace.WorkspaceItem, 0, len(snaps))
	for _, s := range snaps {
		names = append(names, s.name)
		rail = append(rail, workspace.WorkspaceItem{ID: s.id, Name: s.name, Initials: workspace.WorkspaceInitials(s.name)})
	}
	app.SetLoadingWorkspaces(names)
	app.SetWorkspaces(rail)

	s := d.services()
	app.SetChannelService(s.channels)
	app.SetThreadService(s.threads)
	app.SetMessageService(s.messages)
	app.SetReactionService(s.reactions)
	app.SetSearchService(s.search)
	app.SetUnreadService(s.unread)
	app.SetWorkspaceService(s.workspace)
	app.SetAvatarService(s.avatars)
	app.SetSettingsService(s.settings)
	app.SetPresenceService(s.presence)
	app.SetFileService(s.files)
	app.SetProfileService(s.profiles)

	app.SetImageFetcher(d.images)
	app.SetImageProtocol(d.proto)
	app.SetImageContext(d.imageContext())
}

// UseImageProtocol makes Install render images with proto, sized for
// terminal cells of cellPx pixels. cmd/slk calls it for recordings that
// can capture sixel (see docs/assets/demo/settings.tape); call it before
// Install.
func (d *Demo) UseImageProtocol(proto imgpkg.Protocol, cellPx image.Point) {
	d.proto, d.cellPx = proto, cellPx
}

func (d *Demo) imageContext() imgrender.ImageContext {
	return imgrender.ImageContext{
		Protocol:   d.proto,
		Fetcher:    d.images,
		CellPixels: d.cellPx,
		MaxRows:    12,
		MaxCols:    60,
	}
}

// StartupMsgs are what cmd/slk sends as workspaces connect: one ready
// message per workspace (the first is the initial active one), then
// "connected".
func (d *Demo) StartupMsgs() []tea.Msg {
	var out []tea.Msg
	for i, s := range d.world.snapshots() {
		out = append(out, ui.WorkspaceReadyMsg{
			TeamID: s.id, TeamName: s.name, Domain: s.domain, Theme: s.theme,
			Channels: s.channels, FinderItems: s.finder, UserNames: s.userNames,
			UserStatuses: s.statuses, UserID: s.selfID,
			InitialActive: i == 0, LastChannelID: s.firstChannel,
		})
	}
	return append(out, ui.ConnectionStateMsg{State: connected})
}

// Run plays the scenario until ctx is cancelled, sending through send
// (tea.Program.Send). It returns once every scripted step has stopped.
func (d *Demo) Run(ctx context.Context, send func(tea.Msg)) { d.director.run(ctx, send) }
