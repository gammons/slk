// Update runs per keystroke, View once per frame while the prompt is open.
// Both report allocations so a later change to either path is visible.
//
// View holds no render cache, so every frame pays a full render: lipgloss
// measuring and padding a bordered box, unchanged from the pre-move
// implementation at 053d2a2.
//
// Update is slower than the string switch it replaced -- key.Matches formats
// the key name, where the caller used to pass a normalised string. One call
// per keystroke, so not worth optimising; noted so the ratio is not read as a
// defect.
package confirmprompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// themedStyles mirrors what a themed host pushes in, BaseANSI included.
func themedStyles() Styles {
	s := DefaultStyles(true)
	s.BaseANSI = "\x1b[48;2;26;26;46m\x1b[38;2;224;224;224m"
	return s
}

func benchModel(body string) Model {
	m := New(WithStyles(themedStyles()), WithWidth(120))
	m.Open("Delete message?", body, nil)
	return m
}

const benchBody = "the quick brown fox jumps over the lazy dog"

func BenchmarkView(b *testing.B) {
	m := benchModel(benchBody)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}

// Truncation adds a width measurement and a re-slice.
func BenchmarkViewTruncatedBody(b *testing.B) {
	m := benchModel(strings.Repeat("überlange nachricht ", 40))

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}

// Empty BaseANSI skips the reset fixup, isolating what it costs.
func BenchmarkViewWithoutBaseANSI(b *testing.B) {
	m := New(WithWidth(120))
	m.Open("Delete message?", benchBody, nil)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}

// Update copies the model, KeyMap included, on every key.
func BenchmarkUpdateConfirm(b *testing.B) {
	base := benchModel(benchBody)
	press := tea.KeyPressMsg{Code: 'y', Text: "y"}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m := base
		_, _ = m.Update(press)
	}
}

// Cancel is the path every non-confirm key takes.
func BenchmarkUpdateCancel(b *testing.B) {
	base := benchModel(benchBody)
	press := tea.KeyPressMsg{Code: tea.KeyEscape}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m := base
		_, _ = m.Update(press)
	}
}

// A non-key message reaches Update on every tick the host forwards.
func BenchmarkUpdateIgnoredMsg(b *testing.B) {
	m := benchModel(benchBody)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = m.Update(sentinelMsg{})
	}
}
