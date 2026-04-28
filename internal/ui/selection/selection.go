package selection

// Anchor identifies one endpoint of a selection.
//
//	MessageID is the Slack TS of the anchored message, or "" when the
//	  anchor sits on a non-message row (e.g. a date separator). An anchor
//	  on a separator is treated as a line boundary, not a character
//	  position.
//	Line is the 0-indexed line within that message's rendered block
//	  (after wrapping).
//	Col is the display column inside that line; columns are 0-indexed
//	  and measured in display cells (wide chars occupy 2).
type Anchor struct {
	MessageID string
	Line      int
	Col       int
}

// Range is a half-open [Start, End) selection. Endpoints may be in any
// order; consumers must call Normalize before iterating.
//
// Active is true while the user is still dragging. Renderers use this
// to decide whether to draw the live highlight.
type Range struct {
	Start  Anchor
	End    Anchor
	Active bool
}

// LessOrEqual returns true when a precedes-or-equals b in document order.
// Order is (MessageID, Line, Col). MessageID is compared as a string —
// Slack timestamps sort correctly under string comparison because they
// are zero-padded fixed-width decimals.
func LessOrEqual(a, b Anchor) bool {
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Col <= b.Col
}

// Normalize returns the endpoints in document order: lo <= hi.
func (r Range) Normalize() (lo, hi Anchor) {
	if LessOrEqual(r.Start, r.End) {
		return r.Start, r.End
	}
	return r.End, r.Start
}

// IsEmpty reports whether the selection covers zero characters.
func (r Range) IsEmpty() bool {
	return r.Start == r.End
}

// Contains reports whether a falls within the half-open [lo, hi) interval.
func (r Range) Contains(a Anchor) bool {
	lo, hi := r.Normalize()
	return LessOrEqual(lo, a) && !LessOrEqual(hi, a)
}
