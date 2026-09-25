package thread

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	emojiutil "github.com/gammons/slk/internal/emoji"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// breadcrumbHint is the right-aligned close hint on the breadcrumb row.
const breadcrumbHint = "esc close"

// breadcrumbHintGap is the minimum run of spaces before the hint.
const breadcrumbHintGap = 2

// renderBreadcrumb renders the thread header row at exactly width
// cells:
//
//	# general › Thread from alice · 2 replies        esc close
//
// When width is short, parts drop in a fixed order: the hint, then
// "from <author>", then the channel name is truncated with "…". An
// empty channelName omits the channel and its separator; an empty
// author omits "from". "Thread · N replies" is always kept and is only
// hard-truncated when width is narrower than it.
func renderBreadcrumb(width int, channelName, channelType, author string, replyCount int) string {
	if width <= 0 {
		return ""
	}
	label := "replies"
	if replyCount == 1 {
		label = "reply"
	}
	const (
		sep    = " › "
		thread = "Thread"
	)
	count := fmt.Sprintf(" · %d %s", replyCount, label)
	channel := ""
	if channelName != "" {
		channel = messages.ChannelGlyph(channelType) + " " + channelName
	}
	from := ""
	if author != "" {
		from = " from " + author
	}
	leftWidth := func() int {
		w := emojiutil.Width(thread + from + count)
		if channel != "" {
			w += emojiutil.Width(channel + sep)
		}
		return w
	}

	showHint := leftWidth()+breadcrumbHintGap+emojiutil.Width(breadcrumbHint) <= width
	if leftWidth() > width {
		from = ""
	}
	if channel != "" && leftWidth() > width {
		room := width - emojiutil.Width(sep+thread+count)
		if room < 2 {
			channel = ""
		} else {
			channel = ansi.Truncate(channel, room, "…")
		}
	}

	bg := lipgloss.NewStyle().Background(styles.Background)
	var b strings.Builder
	if channel != "" {
		b.WriteString(bg.Foreground(styles.TextMuted).Render(channel + sep))
	}
	b.WriteString(bg.Foreground(styles.Accent).Bold(true).Render(thread + from))
	b.WriteString(bg.Foreground(styles.TextPrimary).Render(count))
	left := b.String()
	// left carries ANSI styling from Render above; emojiutil.Width
	// strips escapes before measuring (like ansi.StringWidth), so it
	// is safe to measure directly here.
	if emojiutil.Width(left) > width {
		left = ansi.Truncate(left, width, "")
	}
	used := emojiutil.Width(left)
	if showHint {
		gap := width - used - emojiutil.Width(breadcrumbHint)
		return left + bg.Render(strings.Repeat(" ", gap)) +
			bg.Foreground(styles.TextMuted).Render(breadcrumbHint)
	}
	return left + bg.Render(strings.Repeat(" ", width-used))
}
