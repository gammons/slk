package image

import (
	"bytes"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// frameTestDone triggers a single Update cycle so the program renders
// exactly one meaningful frame before quitting.
type frameTestDone struct{}

type frameTestModel struct {
	frames *SixelFrameStore
}

func (m frameTestModel) Init() tea.Cmd {
	return func() tea.Msg { return frameTestDone{} }
}

func (m frameTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(frameTestDone); ok {
		return m, tea.Quit
	}
	return m, nil
}

func (m frameTestModel) View() tea.View {
	id := m.frames.Publish(SixelFrame{Placements: []SixelPlacement{{
		Key: "integration", Row: 2, Col: 1, Rows: 1, Cols: 1,
		Bytes: []byte("\x1bPqintegration\x1b\\"),
	}}})
	v := tea.NewView("visible-text")
	v.AltScreen = true
	v.WindowTitle = FrameTitle("slk-test", id)
	return v
}

// TestSixelFrameOutput_BubbleTeaFlushContract is the end-to-end
// regression for the frame-correlation design: a real Bubble Tea
// v2.0.6 program flushes a marked frame through FrameOutput, and the
// captured stream must show the text frame before the exact frame's
// sixel DCS, with the internal marker stripped and only the real title
// forwarded.
func TestSixelFrameOutput_BubbleTeaFlushContract(t *testing.T) {
	raw := new(bytes.Buffer)
	frames := NewSixelFrameStore()
	out := NewFrameOutput(raw, frames)
	p := tea.NewProgram(frameTestModel{frames: frames},
		tea.WithInput(nil),
		tea.WithOutput(out),
		tea.WithFPS(120),
		tea.WithWindowSize(80, 24),
	)
	if _, err := p.Run(); err != nil {
		t.Fatalf("program run: %v", err)
	}

	got := raw.String()
	textAt := strings.Index(got, "visible-text")
	paintAt := strings.Index(got, "\x1bPqintegration\x1b\\")
	if textAt < 0 || paintAt < 0 {
		t.Fatalf("output missing text or sixel DCS; got %q", got)
	}
	if textAt > paintAt {
		t.Fatalf("text must precede the sixel DCS for the exact flushed frame; got %q", got)
	}
	if strings.Contains(got, "slk-sixel-frame=") {
		t.Fatalf("internal marker leaked to the terminal writer: %q", got)
	}
	if !strings.Contains(got, "\x1b]2;slk-test\a") {
		t.Fatalf("real window title was not forwarded unchanged: %q", got)
	}
}
