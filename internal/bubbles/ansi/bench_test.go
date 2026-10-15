package ansi

import (
	"fmt"
	"strings"
	"testing"
)

const (
	benchBg   = "\x1b[48;2;26;26;46m"
	benchBgFg = benchBg + "\x1b[38;2;224;224;224m"
)

// denseLine builds a line of about size bytes with an SGR reset every ~10
// bytes: "\x1b[1mword\x1b[m " repeated, alternating the short and explicit
// zero reset forms.
func denseLine(size int) string {
	var sb strings.Builder
	for i := 0; sb.Len() < size; i++ {
		reset := "\x1b[m"
		if i%2 == 1 {
			reset = "\x1b[0m"
		}
		sb.WriteString("\x1b[1mab\x1b[22m")
		sb.WriteString("c")
		sb.WriteString(reset)
	}
	return sb.String()
}

// shortLine is a typical message line with a few styled spans.
func shortLine() string {
	return "\x1b[1mAlice\x1b[m  said hello to \x1b[38;2;80;120;200m@bob\x1b[m about the \x1b[3mdeploy\x1b[m and `\x1b[7mcode\x1b[m` today"
}

func benchmarkReapply(b *testing.B, text, style string) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ReapplyAfterResets(text, style)
	}
}

func BenchmarkReapplyAfterResets(b *testing.B) {
	dense := denseLine(2048)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("%d:", i) + denseLine(2048)
	}
	noResets := strings.Repeat("plain text without any escapes ", 66)
	styles := []struct{ name, style string }{{"bg", benchBg}, {"bgfg", benchBgFg}}
	for _, s := range styles {
		b.Run("Dense2KB/"+s.name, func(b *testing.B) { benchmarkReapply(b, dense, s.style) })
		b.Run("Dense2KBx100/"+s.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, l := range lines {
					_ = ReapplyAfterResets(l, s.style)
				}
			}
		})
		b.Run("ShortMessage/"+s.name, func(b *testing.B) { benchmarkReapply(b, shortLine(), s.style) })
		b.Run("NoResets/"+s.name, func(b *testing.B) { benchmarkReapply(b, noResets, s.style) })
	}
}
