package emoji

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestNormalizeEmojiPresentation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantWidth int // expected lipgloss.Width after normalization
	}{
		// Text-default EP with VS16 from kyokomi — should STRIP VS16, width becomes 1
		{"heart with VS16", "❤️", 1},
		{"scissors with VS16", "✂️", 1},
		{"sun with VS16", "☀️", 1},
		{"cloud with VS16", "☁️", 1},
		{"snowflake with VS16", "❄️", 1},
		{"warning with VS16", "⚠️", 1},
		{"shamrock with VS16", "☘️", 1},
		{"check with VS16", "✔️", 1},
		{"pencil with VS16", "✏️", 1},
		{"copyright with VS16", "©️", 1},
		{"tm with VS16", "™️", 1},
		{"arrow right with VS16", "➡️", 1},

		// Text-default EP without VS16 — already width 1, no change needed
		{"heart no VS16", "❤", 1},
		{"scissors no VS16", "✂", 1},
		{"sun no VS16", "☀", 1},
		{"copyright no VS16", "©", 1},

		// Supplemental symbols (U+1Fxxx) with VS16 — should strip
		{"hot pepper with VS16", "🌶️", 1},
		{"dagger with VS16", "🗡️", 1},
		{"dove with VS16", "🕊️", 1},
		{"desktop with VS16", "🖥️", 1},
		{"film projector with VS16", "📽️", 1},
		{"pen with VS16", "🖊️", 1},

		// Already wide (Emoji_Presentation=Yes) — should NOT be touched
		{"thumbsup", "👍", 2},
		{"fire", "🔥", 2},
		{"rocket", "🚀", 2},
		{"sparkles", "✨", 2},
		{"party", "🎉", 2},
		{"smile", "😊", 2},

		// Not emoji — should not change
		{"ascii", "hello", 5},
		{"empty", "", 0},
		{"number", "42", 2},

		// Emoji with trailing space (kyokomi ReplacePadding) — should strip VS16 from emoji
		{"heart VS16 space", "❤️ ", 2}, // 1 (stripped heart) + 1 (space)
		{"scissors VS16 space", "✂️ ", 2},

		// Multi-codepoint sequences — should not be affected
		{"flag", "🇺🇸", 2},
		{"family", "👨\u200d👩\u200d👧", 2},
		{"skin tone on wide base", "👍🏽", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeEmojiPresentation(tt.input)
			gotWidth := lipgloss.Width(result)
			if gotWidth != tt.wantWidth {
				t.Errorf("NormalizeEmojiPresentation(%q) → %q, lipgloss.Width = %d, want %d",
					tt.input, result, gotWidth, tt.wantWidth)
			}
		})
	}
}

func TestNormalizeStripsVS16(t *testing.T) {
	// Verify VS16 is actually removed from the string
	input := "❤️" // U+2764 U+FE0F
	result := NormalizeEmojiPresentation(input)
	expected := "❤" // U+2764 only
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	// Calling normalize twice should produce the same result
	input := "❤️"
	once := NormalizeEmojiPresentation(input)
	twice := NormalizeEmojiPresentation(once)
	if once != twice {
		t.Errorf("double normalize changed result: %q → %q", once, twice)
	}
}
