package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/demo"
	emojiwidth "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/styles"
)

// runDemo runs slk against the made-up workspaces in internal/demo. It is
// the recording mode behind the hidden --demo flag: no config, tokens,
// cache, network or config writes. See
// docs/superpowers/specs/2026-09-27-demo-mode-design.md.
func runDemo(scenario string) error {
	d, err := demo.New(scenario, time.Now())
	if err != nil {
		return err
	}
	proto, err := demoImageProtocol(os.Getenv("SLK_DEMO_IMAGES"))
	if err != nil {
		return err
	}
	styles.Apply(d.InitialTheme(), core.Theme{})
	emojiwidth.SetImageMode(false, 2)

	var opts []tea.ProgramOption
	var frames *imgpkg.SixelFrameStore
	if proto == imgpkg.ProtoSixel {
		// Sixel is sized in pixels. ttyd (under VHS) reports no pixel
		// size, so the tapes pass COLORTERM_CELL_WIDTH/HEIGHT.
		pxW, pxH := imgpkg.CellPixels(int(os.Stdout.Fd()))
		imgpkg.SetCellPixels(pxW, pxH)
		d.UseImageProtocol(proto, image.Pt(pxW, pxH))
		// The same frame-correlated writer run() uses: sixel is painted
		// after each frame's text, in the frame it belongs to.
		frames = imgpkg.NewSixelFrameStore()
		opts = append(opts, tea.WithOutput(imgpkg.NewFrameOutput(os.Stdout, frames)))
	}
	app := newDemoApp(d)
	if frames != nil {
		app.SetSixelFrameStore(frames)
	}

	p := tea.NewProgram(app, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		d.Run(ctx, p.Send)
	}()
	// Send blocks until the program's event loop is running, so the
	// workspaces "connect" from their own goroutine, as in run().
	go func() {
		for _, msg := range d.StartupMsgs() {
			p.Send(msg)
		}
	}()
	_, err = p.Run()
	cancel()
	wg.Wait()
	return err
}

// demoImageProtocol reads SLK_DEMO_IMAGES. Half-block, the default,
// works in any terminal; sixel is for recording with a VHS that captures
// sixel (docs/assets/demo/settings.tape).
func demoImageProtocol(value string) (imgpkg.Protocol, error) {
	switch value {
	case "", "halfblock":
		return imgpkg.ProtoHalfBlock, nil
	case "sixel":
		return imgpkg.ProtoSixel, nil
	}
	return imgpkg.ProtoOff, fmt.Errorf("SLK_DEMO_IMAGES=%q: want halfblock or sixel", value)
}

// newDemoApp builds the demo's App. It takes no config, token store or
// cache handle, so demo mode cannot reach the user's real data.
func newDemoApp(d *demo.Demo) *ui.App {
	app := ui.NewApp()
	applyUISettings(app, uiSettings{
		TimestampFormat:      demo.TimestampFormat,
		TypingIndicators:     true,
		HideDeactivatedUsers: true,
		MouseWheelLines:      3,
		ColoredUsernames:     true,
	})
	d.Install(app)
	return app
}
