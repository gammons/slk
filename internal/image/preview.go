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
	// SiblingCount and SiblingIndex describe how this image relates to
	// other image attachments on the same message. When SiblingCount > 1
	// the caption shows "(i/N)" and h/l/arrow keys can cycle. Both
	// default to (1, 0) for single-image preview.
	SiblingCount int
	SiblingIndex int
}

// Preview is a stateful full-screen image overlay sub-component. It is
// rendered by the App when non-nil; otherwise the messages+thread region
// is rendered normally.
//
// A Preview can be open in either of two states:
//   - loading: opened immediately when the user requests a preview, so
//     they get visual feedback that the action registered. The View
//     shows a centered "Loading <filename>..." spinner. When the fetch
//     completes, the host calls SwapImage to swap in the decoded image.
//   - displaying: the image is decoded and being rendered.
type Preview struct {
	open         bool
	name         string
	fid          string
	img          image.Image
	path         string
	sibCount     int
	sibIndex     int
	loading      bool
	loadingFrame int
}

// NewPreview returns an open preview displaying the given image.
func NewPreview(in PreviewInput) Preview {
	count, idx := normalizeSiblings(in.SiblingCount, in.SiblingIndex)
	return Preview{
		open:     true,
		name:     in.Name,
		fid:      in.FileID,
		img:      in.Img,
		path:     in.Path,
		sibCount: count,
		sibIndex: idx,
	}
}

// NewLoadingPreview returns an open preview in the loading state. The
// image fetch happens asynchronously; the host calls SwapImage on the
// resulting Preview once the bytes are decoded.
func NewLoadingPreview(name string, sibCount, sibIndex int) Preview {
	count, idx := normalizeSiblings(sibCount, sibIndex)
	return Preview{
		open:     true,
		name:     name,
		sibCount: count,
		sibIndex: idx,
		loading:  true,
	}
}

func normalizeSiblings(count, idx int) (int, int) {
	if count < 1 {
		count = 1
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= count {
		idx = count - 1
	}
	return count, idx
}

// IsLoading reports whether the preview is currently waiting for image
// bytes to land.
func (p Preview) IsLoading() bool { return p.loading }

// AdvanceLoadingFrame steps the spinner used in the loading state. Call
// from a tea.Tick handler while IsLoading() is true.
func (p *Preview) AdvanceLoadingFrame() { p.loadingFrame++ }

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

// SiblingCount returns the total number of image attachments on the
// message this preview was opened from. Always >= 1.
func (p Preview) SiblingCount() int { return p.sibCount }

// SiblingIndex returns the 0-based index of the currently shown image
// among its siblings. 0 <= idx < SiblingCount().
func (p Preview) SiblingIndex() int { return p.sibIndex }

// SwapImage replaces the currently shown image (used when cycling via
// h/l, or when the initial fetch finishes for a loading preview). The
// sibling index is updated to the new position; the loading flag is
// cleared.
func (p *Preview) SwapImage(in PreviewInput) {
	p.name = in.Name
	p.fid = in.FileID
	p.img = in.Img
	p.path = in.Path
	if in.SiblingCount > 0 {
		p.sibCount = in.SiblingCount
	}
	if in.SiblingIndex >= 0 && in.SiblingIndex < p.sibCount {
		p.sibIndex = in.SiblingIndex
	}
	p.loading = false
}

// previewSpinnerFrames is the small set of braille glyphs used to
// animate the loading state. Cycled via AdvanceLoadingFrame.
var previewSpinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// View renders the preview into a string of size width × height. proto is
// the active rendering protocol (kitty / sixel / halfblock). Reserves
// 1 row top for the caption, 1 row bottom for the hint, and centers the
// image (aspect-preserved) in the remaining area.
//
// While loading, the image area shows a centered spinner + filename
// instead of an image. Caption and hint render the same way.
func (p *Preview) View(width, height int, proto Protocol) string {
	if !p.open || width <= 0 || height <= 0 {
		return ""
	}
	if p.img == nil && !p.loading {
		return ""
	}
	if p.loading {
		return p.viewLoading(width, height)
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
	if p.sibCount > 1 {
		caption = fmt.Sprintf("%s  •  %dx%d  •  (%d/%d)", p.name, srcW, srcH, p.sibIndex+1, p.sibCount)
	}
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

	hintText := "Esc/q close  •  Enter open in system viewer"
	if p.sibCount > 1 {
		hintText = "h/\u2190 prev  •  l/\u2192 next  •  " + hintText
	}
	hint := lipgloss.NewStyle().Faint(true).Render(hintText)
	b.WriteString(hint)
	return b.String()
}

// viewLoading renders the preview's loading state: spinner + filename
// centered in the image region, plus the standard caption and hint
// rows. Same overall layout as the normal View so the overlay doesn't
// shift when the image arrives.
func (p *Preview) viewLoading(width, height int) string {
	caption := p.name
	if p.sibCount > 1 {
		caption = fmt.Sprintf("%s  •  (%d/%d)", p.name, p.sibIndex+1, p.sibCount)
	}
	captionStyle := lipgloss.NewStyle().Faint(true).Width(width)

	imgRows := height - 2
	if imgRows < 1 {
		return captionStyle.Render(caption)
	}

	frame := previewSpinnerFrames[p.loadingFrame%len(previewSpinnerFrames)]
	loadingMsg := fmt.Sprintf("%s  Loading %s...", string(frame), p.name)
	if w := lipgloss.Width(loadingMsg); w > width {
		loadingMsg = fmt.Sprintf("%s  Loading...", string(frame))
	}
	loadingStyle := lipgloss.NewStyle().Faint(true)
	rendered := loadingStyle.Render(loadingMsg)
	rwidth := lipgloss.Width(rendered)
	leftPad := (width - rwidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := width - rwidth - leftPad
	if rightPad < 0 {
		rightPad = 0
	}
	loadingLine := strings.Repeat(" ", leftPad) + rendered + strings.Repeat(" ", rightPad)

	mid := imgRows / 2

	var b strings.Builder
	b.WriteString(captionStyle.Render(caption))
	b.WriteByte('\n')
	for i := 0; i < imgRows; i++ {
		if i == mid {
			b.WriteString(loadingLine)
		} else {
			b.WriteString(strings.Repeat(" ", width))
		}
		b.WriteByte('\n')
	}
	hintText := "Esc/q close"
	if p.sibCount > 1 {
		hintText = "h/\u2190 prev  •  l/\u2192 next  •  " + hintText
	}
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(hintText))
	return b.String()
}

// fitInto returns the largest (cols, rows) that preserve the source
// image's pixel aspect ratio when rendered into terminal cells.
//
// Terminal cells are roughly twice as tall as wide (typical font metric:
// 8×16 px). A square pixel image therefore covers twice as many columns
// as rows: e.g. a 100×100 image in 8×16 cells fills 12.5 cols × 6.25 rows.
// The cell aspect ratio in cell units is thus:
//
//	cols/rows = (srcW/srcH) × (cellH/cellW) = (srcW/srcH) × cellAspect
//
// Given maxCols and maxRows we pick the larger axis-fit that respects
// this ratio.
func fitInto(srcW, srcH, maxCols, maxRows int) image.Point {
	const cellAspect = 2.0 // cellH / cellW
	cellRatio := float64(srcW) / float64(srcH) * cellAspect

	// Try filling width; compute the height that preserves ratio.
	w := maxCols
	h := int(float64(w) / cellRatio)
	if h > maxRows {
		// Height-bound; fill rows instead.
		h = maxRows
		w = int(float64(h) * cellRatio)
	}
	if w > maxCols {
		w = maxCols
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return image.Pt(w, h)
}
