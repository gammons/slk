package compose

import "github.com/gammons/slk/internal/slack/mrkdwn"

// PreviewOn reports whether the draft preview is showing for this box.
func (m *Model) PreviewOn() bool { return m.preview }

// TogglePreview shows or hides the draft preview and returns the new
// state.
func (m *Model) TogglePreview() bool {
	m.preview = !m.preview
	m.dirty()
	return m.preview
}

// SetPreview shows or hides the draft preview.
func (m *Model) SetPreview(on bool) {
	if m.preview == on {
		return
	}
	m.preview = on
	m.dirty()
}

// PreviewRender draws a draft's mrkdwn at a width: the caller's message
// renderer, so the preview is drawn the way a sent message is.
type PreviewRender func(mrkdwn string, width int) string

// RenderKey is everything a PreviewRender reads besides the mrkdwn and
// the width (the theme, the pane's names, its window). The caller
// changes a field whenever what it stands for changes.
type RenderKey struct{ Theme, Pane, Window int64 }

// previewTextCache holds the draft's send-path mrkdwn for the draft
// text and mention data it was made from.
type previewTextCache struct {
	text   string
	gen    uint64
	mrkdwn string
	ok     bool
}

// previewBodyCache holds the rendered body for the mrkdwn, width and
// key it was drawn with.
type previewBodyCache struct {
	mrkdwn string
	width  int
	key    RenderKey
	body   string
	ok     bool
}

// PreviewBody returns the draft as it will look once sent: translated
// and converted exactly as a send does (TranslateMentionsForSend, then
// mrkdwn.Convert), drawn by render. Both steps are cached, so an
// unchanged draft costs nothing and a change of names the draft does
// not use only re-translates.
func (m *Model) PreviewBody(width int, key RenderKey, render PreviewRender) string {
	text := m.Value()
	tc := &m.previewText
	if !tc.ok || tc.text != text || tc.gen != m.mentionGen {
		converted, _ := mrkdwn.Convert(m.TranslateMentionsForSend(text))
		*tc = previewTextCache{text: text, gen: m.mentionGen, mrkdwn: converted, ok: true}
	}
	bc := &m.previewBody
	if !bc.ok || bc.mrkdwn != tc.mrkdwn || bc.width != width || bc.key != key {
		*bc = previewBodyCache{mrkdwn: tc.mrkdwn, width: width, key: key, body: render(tc.mrkdwn, width), ok: true}
	}
	return bc.body
}
