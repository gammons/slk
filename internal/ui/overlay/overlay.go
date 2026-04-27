package overlay

import (
	"charm.land/lipgloss/v2"
)

// DimmedOverlay composites a modal box on top of a dimmed background.
// The background string is rendered to a Canvas, all cell colors are
// darkened by dimPercent (0.0-1.0), then the modal box is placed centered
// on top by copying its cells.
func DimmedOverlay(width, height int, background string, box string, dimPercent float64) string {
	// Step 1: Render background to canvas and dim all cells
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewLayer(background))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := canvas.CellAt(x, y)
			if cell == nil {
				continue
			}
			if cell.Style.Bg != nil {
				cell.Style.Bg = lipgloss.Darken(cell.Style.Bg, dimPercent)
			}
			if cell.Style.Fg != nil {
				cell.Style.Fg = lipgloss.Darken(cell.Style.Fg, dimPercent)
			}
			canvas.SetCell(x, y, cell)
		}
	}

	// Step 2: Render dimmed canvas, create output canvas
	dimmedStr := canvas.Render()
	outCanvas := lipgloss.NewCanvas(width, height)
	outCanvas.Compose(lipgloss.NewLayer(dimmedStr))

	// Step 3: Render modal to its own canvas, compute centered position
	modalW := lipgloss.Width(box)
	modalH := lipgloss.Height(box)
	startX := (width - modalW) / 2
	startY := (height - modalH) / 2
	if startX < 0 {
		startX = 0
	}
	if startY < 0 {
		startY = 0
	}

	modalCanvas := lipgloss.NewCanvas(modalW, modalH)
	modalCanvas.Compose(lipgloss.NewLayer(box))

	// Step 4: Copy modal cells onto output canvas
	for my := 0; my < modalH; my++ {
		for mx := 0; mx < modalW; mx++ {
			cell := modalCanvas.CellAt(mx, my)
			if cell != nil {
				outCanvas.SetCell(startX+mx, startY+my, cell)
			}
		}
	}

	return outCanvas.Render()
}
