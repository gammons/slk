package avatar

import (
	"fmt"
	"regexp"
)

// originalAvatarRE matches the two forms Slack uses for a user's
// original avatar upload: the S3 bucket URL that boot, edge users/info
// and bootstrap revalidation carry as image_original, and the same file
// on the avatar CDN. Anchored at both ends so a query string, a
// fragment, or a lookalike host never matches.
var originalAvatarRE = regexp.MustCompile(
	`^https://(?:s3-[a-z0-9-]+\.amazonaws\.com/slack-files2/avatars|avatars\.slack-edge\.com)` +
		`/(\d{4}-\d{2}-\d{2})/([^/?#]+)_original\.([A-Za-z0-9]+)$`)

// SizedURL rewrites a Slack original-avatar URL to the avatar CDN's
// px-sized variant of the same file, keeping the extension (the CDN
// 403s on a mismatched one). Any other URL, including Gravatar,
// already-sized and bot-icon URLs, is returned unchanged, so it is safe
// to call on every avatar URL.
func SizedURL(orig string, px int) string {
	if px <= 0 {
		return orig
	}
	m := originalAvatarRE.FindStringSubmatch(orig)
	if m == nil {
		return orig
	}
	return fmt.Sprintf("https://avatars.slack-edge.com/%s/%s_%d.%s", m[1], m[2], px, m[3])
}
