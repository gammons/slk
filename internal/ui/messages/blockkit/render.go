package blockkit

// Render produces a RenderResult for a slice of blocks at the given
// content width. Width is the available content width AFTER the
// caller has subtracted avatar gutter and border columns.
//
// This is currently a stub returning an empty RenderResult. Phase 2
// fills it in.
func Render(blocks []Block, ctx Context, width int) RenderResult {
	return RenderResult{}
}

// RenderLegacy produces a RenderResult for a slice of legacy
// attachments. Same width contract as Render. Phase 4 fills it in.
func RenderLegacy(atts []LegacyAttachment, ctx Context, width int) RenderResult {
	return RenderResult{}
}
