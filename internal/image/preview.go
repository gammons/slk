package image

import (
	"fmt"
	"image"
	"strings"

	"charm.land/lipgloss/v2"
)

// PreviewInput is the data needed to construct a Preview overlay.
type PreviewInput struct {
	Name   string      // display filename / caption
	FileID string      // Slack file ID, used for cache key
	Img    image.Image // the decoded image to render
	Path   string      // on-disk path; used for system-viewer launch on Enter
}

// Preview is a stateful full-screen image overlay sub-component. It is
// rendered by the App when non-nil; otherwise the messages+thread region
// is rendered normally.
type Preview struct {
	open bool
	name string
	fid  string
	img  image.Image
	path string
}

// NewPreview returns an open preview for the given image.
func NewPreview(in PreviewInput) Preview {
	return Preview{
		open: true,
		name: in.Name,
		fid:  in.FileID,
		img:  in.Img,
		path: in.Path,
	}
}

// IsClosed reports whether the preview is currently dismissed.
// Zero-value Preview is closed.
func (p Preview) IsClosed() bool { return !p.open }

// Close dismisses the preview.
func (p *Preview) Close() { p.open = false }

// Path returns the on-disk path of the previewed image. Used by the
// caller to launch a system viewer (xdg-open / open / start) on Enter.
func (p Preview) Path() string { return p.path }

// FileID returns the Slack file ID associated with this preview.
func (p Preview) FileID() string { return p.fid }

// View renders the preview into a string of size width × height. proto is
// the active rendering protocol (kitty / sixel / halfblock). Reserves
// 1 row top for the caption, 1 row bottom for the hint, and centers the
// image (aspect-preserved) in the remaining area.
func (p *Preview) View(width, height int, proto Protocol) string {
	if !p.open || width <= 0 || height <= 0 || p.img == nil {
		return ""
	}

	imgRows := height - 2
	if imgRows < 1 {
		// No room for image; just caption + hint.
		caption := fmt.Sprintf("%s  •  %dx%d", p.name, p.img.Bounds().Dx(), p.img.Bounds().Dy())
		captionStyle := lipgloss.NewStyle().Faint(true).Width(width)
		return captionStyle.Render(caption)
	}
	imgCols := width

	srcW, srcH := p.img.Bounds().Dx(), p.img.Bounds().Dy()
	target := fitInto(srcW, srcH, imgCols, imgRows)

	render := RenderImage(proto, p.img, target)

	// For kitty: write the upload APC escape directly to the terminal
	// side channel before the placeholder cells go into the View string.
	// Embedding the upload in the returned string would have it mangled
	// by lipgloss/bubbletea (same reason the messages-pane goes around
	// the frame buffer).
	if render.OnFlush != nil {
		_ = render.OnFlush(KittyOutput)
	}

	caption := fmt.Sprintf("%s  •  %dx%d", p.name, srcW, srcH)
	captionStyle := lipgloss.NewStyle().Faint(true).Width(width)

	var b strings.Builder
	b.WriteString(captionStyle.Render(caption))
	b.WriteByte('\n')

	leftPad := (width - target.X) / 2
	rightPad := width - target.X - leftPad
	pad := strings.Repeat(" ", leftPad)
	rpad := strings.Repeat(" ", rightPad)

	topGap := (imgRows - target.Y) / 2
	for i := 0; i < topGap; i++ {
		b.WriteString(strings.Repeat(" ", width))
		b.WriteByte('\n')
	}
	for _, line := range render.Lines {
		b.WriteString(pad)
		b.WriteString(line)
		b.WriteString(rpad)
		b.WriteByte('\n')
	}
	for i := 0; i < imgRows-target.Y-topGap; i++ {
		b.WriteString(strings.Repeat(" ", width))
		b.WriteByte('\n')
	}

	hint := lipgloss.NewStyle().Faint(true).Render("Esc/q close  •  Enter open in system viewer")
	b.WriteString(hint)
	return b.String()
}

// fitInto returns the largest (cols, rows) that preserve aspect ratio
// inside (maxCols, maxRows), accounting for the typical 2:1 cell aspect
// (cells are roughly twice as tall as wide).
func fitInto(srcW, srcH, maxCols, maxRows int) image.Point {
	const cellAspect = 2.0
	srcAspect := float64(srcW) / float64(srcH) / cellAspect
	w := maxCols
	h := int(float64(w) / srcAspect)
	if h > maxRows {
		h = maxRows
		w = int(float64(h) * srcAspect)
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return image.Pt(w, h)
}
