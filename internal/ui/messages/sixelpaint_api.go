package messages

import (
	imgpkg "github.com/gammons/slk/internal/image"
)

// SixelPlacements returns the placements the most recent View wants
// painted, in this pane's local coordinate frame (see
// imgpkg.SixelPaint). Valid until the next View call. The App converts
// them to absolute terminal coordinates and paints them out-of-band
// after the matching Bubble Tea frame flushes.
func (m *Model) SixelPlacements() []imgpkg.SixelPaint { return m.sixelPaints }
