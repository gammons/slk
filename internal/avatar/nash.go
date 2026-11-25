package avatar

import "os"

// Nash (nashterm) fully supports the kitty graphics protocol since 0.8.1
// but reports TERM_PROGRAM=Nash rather than a kitty-identifying value.
// Set KITTY_WINDOW_ID so the existing kitty detection picks it up.
func init() {
	if os.Getenv("TERM_PROGRAM") == "Nash" {
		os.Setenv("KITTY_WINDOW_ID", "nash")
	}
}
