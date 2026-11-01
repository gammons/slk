package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/help"
)

// dialogBenchChannelCount is the size of the channel finder's item list: a
// few hundred, like a real workspace.
const dialogBenchChannelCount = 300

func dialogBenchFinderItems() []core.ChannelFinderItem {
	items := make([]core.ChannelFinderItem, dialogBenchChannelCount)
	for i := range items {
		typ := "channel"
		if i%7 == 0 {
			typ = "private"
		}
		items[i] = core.ChannelFinderItem{
			ID:          fmt.Sprintf("C%04d", i),
			Name:        fmt.Sprintf("team-%03d-chat", i),
			Type:        typ,
			Joined:      i%5 != 0,
			LastVisited: int64(1700000000 - i),
		}
	}
	return items
}

// dialogBenchApp builds the standard 100-channel / 200-message fixture at
// w x h through the real resize path, with the render globals pinned like the
// golden fixtures (dark theme, emoji images off, fixed clock), in normal mode
// and primed with one render. The globals are restored when the benchmark
// ends.
func dialogBenchApp(b *testing.B, w, h int) *App {
	b.Helper()
	pinRenderGlobals(b)

	a := newTestApp(b,
		withWindowSize(w, h),
		withChannels(benchChannels()...),
		withMessages(benchMessages()...),
		withActiveChannel("C1"),
		withActiveTeam("T1"),
	)
	a.now = goldenClock
	_ = a.View()
	return a
}

// BenchmarkAppViewDialog measures one App.View() per op with a dialog open
// over the standard fixture, at two terminal sizes. "none" is the same App
// with no dialog (its fixture differs from BenchmarkAppViewIdle's, so do not
// compare the two). The View is repeated with
// nothing changing, so any cost beyond "none" is the dialog's own render, the
// DimmedOverlay composite, or panes re-rendering underneath it.
func BenchmarkAppViewDialog(b *testing.B) {
	scenarios := []struct {
		name  string
		open  func(b *testing.B, a *App)
		shown func(a *App) bool
	}{
		{
			name:  "none",
			open:  func(*testing.B, *App) {},
			shown: func(*App) bool { return true },
		},
		{
			name: "channel_finder",
			open: func(b *testing.B, a *App) {
				a.SetChannelFinderItems(dialogBenchFinderItems())
				a.channelFinder.Open()
				a.SetMode(ModeChannelFinder)
			},
			shown: func(a *App) bool { return a.channelFinder.IsVisible() },
		},
		{
			name: "channel_finder_filtered",
			open: func(b *testing.B, a *App) {
				a.SetChannelFinderItems(dialogBenchFinderItems())
				a.channelFinder.Open()
				for _, k := range "team-1" {
					a.channelFinder.HandleKey(string(k))
				}
				a.SetMode(ModeChannelFinder)
			},
			shown: func(a *App) bool { return a.channelFinder.IsVisible() },
		},
		{
			name: "confirm_prompt",
			open: func(b *testing.B, a *App) {
				a.openConfirmPrompt("Leave #team-001-chat?",
					"You will stop receiving messages from #team-001-chat. You can rejoin at any time from the channel finder.",
					func() tea.Msg { return nil })
			},
			shown: func(a *App) bool { return a.confirmPrompt.IsVisible() },
		},
		{
			name: "help",
			open: func(b *testing.B, a *App) {
				a.help.SetEntries(help.FromKeyMap(a.keys))
				a.help.SetFooter("slk bench")
				a.help.Open()
				a.SetMode(ModeHelp)
			},
			shown: func(a *App) bool { return a.help.IsVisible() },
		},
	}
	sizes := []struct{ w, h int }{{120, 40}, {200, 60}}

	for _, sc := range scenarios {
		for _, sz := range sizes {
			b.Run(fmt.Sprintf("%s/%dx%d", sc.name, sz.w, sz.h), func(b *testing.B) {
				a := dialogBenchApp(b, sz.w, sz.h)
				sc.open(b, a)
				if v := a.View(); v.Content == "" || !sc.shown(a) {
					b.Fatalf("%s: dialog not open or empty frame", sc.name)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = a.View()
				}
			})
		}
	}
}
