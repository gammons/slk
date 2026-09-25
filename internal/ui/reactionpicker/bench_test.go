package reactionpicker

import (
	"fmt"
	"testing"
)

func BenchmarkFilterByQuery(b *testing.B) {
	customs := make(map[string]string, 10_000)
	for i := 0; i < 10_000; i++ {
		name := fmt.Sprintf("candidate_%05d", i)
		customs[name] = "https://emoji.example.com/" + name + ".gif"
	}

	m := New()
	m.SetCustomEmoji(customs)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Open("C123", "1234.5678", nil)
		m.HandleKey("~")
	}
}
