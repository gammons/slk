package core

import (
	"strings"

	"github.com/gammons/slk/internal/text"
)

// ChannelNameMatcher returns the test for "is this conversation the
// channel the user named": a leading "#" on name is ignored, the compare
// is case- and accent-insensitive, and only channels and private channels
// match (a DM named the same is not a channel). A name that is empty
// once "#" is dropped matches nothing. /open, /join and the
// per-workspace default_channel all resolve a typed name with it.
func ChannelNameMatcher(name string) func(convType, convName string) bool {
	want := text.Fold(strings.TrimPrefix(name, "#"))
	return func(convType, convName string) bool {
		return want != "" && (convType == "channel" || convType == "private") && text.Fold(convName) == want
	}
}
