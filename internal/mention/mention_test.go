package mention

import "testing"

func TestInText(t *testing.T) {
	const self = "U123"
	tests := []struct {
		name string
		text string
		self string
		want bool
	}{
		{"direct mention mid-sentence", "hey <@U123> take a look", self, true},
		{"direct mention alone", "<@U123>", self, true},
		{"different user", "hey <@U999> take a look", self, false},
		// The closing ">" is load-bearing: without it a self ID of U123
		// would match every user whose ID starts with U123.
		{"longer id with self as prefix", "hey <@U123ABC> hi", self, false},
		{"bare id in prose is not a mention", "hey U123 look", self, false},
		{"here broadcast", "<!here> deploying now", self, true},
		{"channel broadcast", "<!channel> all hands", self, true},
		{"everyone broadcast", "<!everyone> notice", self, true},
		// Pipe-labeled forms are a KNOWN GAP, not desired behavior:
		// these pin what InText does today, inherited verbatim from
		// internal/notify. internal/ui/messages/flatten.go proves the
		// repo sees these forms. Changing this is a behavior change and
		// belongs in its own commit.
		{"labeled direct mention is not detected", "hi <@U123|alice> there", self, false},
		{"labeled here broadcast is not detected", "<!here|@here> deploy", self, false},
		// Usergroup mentions are deliberately out of scope: resolving
		// them needs the user's own group memberships, which slk lacks.
		{"usergroup labeled is not detected", "<!subteam^S1|@eng> ship it", self, false},
		{"usergroup bare is not detected", "<!subteam^S1> ship it", self, false},
		{"empty self id ignores direct mentions", "hello <@U123>", "", false},
		{"empty self id still sees broadcasts", "<!here> hi", "", true},
		{"empty text", "", self, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InText(tt.text, tt.self); got != tt.want {
				t.Errorf("InText(%q, %q) = %v, want %v", tt.text, tt.self, got, tt.want)
			}
		})
	}
}
