package messages

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Slack's <!date^TIMESTAMP^FORMAT|FALLBACK> token (formatting docs:
// https://api.slack.com/reference/surfaces/formatting#date-formatting).
// Slack renders the FORMAT string — Slack-specific {tokens} expanded
// against the timestamp in the viewer's timezone — and keeps the
// precomputed FALLBACK only for unparseable timestamps. Slack's wire
// form is `<!date^TS^FORMAT|FALLBACK|LINK>`, but some senders put the
// link in a caret segment (`<!date^TS^FORMAT^LINK|FALLBACK>`); parsing
// below tolerates both.
//
// The calendar integrations (Outlook Calendar, Google Calendar) are the
// visible case: their event lines carry the times only in the FORMAT
// string, so rendering the FALLBACK (slk's previous behavior) drops the
// times entirely.

// slackDateFormatRe matches Slack's mrkdwn date tokens. Longer forms
// come first so {date_num} / {date_short} are never clipped to
// {date}, and {time_secs} to {time}.
var slackDateFormatRe = regexp.MustCompile(`\{date_num\}|\{date_short_pretty\}|\{date_long_pretty\}|\{date_pretty\}|\{date_short\}|\{date_long\}|\{date\}|\{time_secs\}|\{time\}`)

// slackDateTokens maps each Slack token to the Go time layout that
// reproduces Slack's rendering. Pretty variants need ordinal suffixes
// and are handled separately.
var slackDateTokens = map[string]string{
	"{date_num}":   "2006-01-02",
	"{date}":       "January 2, 2006",
	"{date_short}": "Jan 2, 2006",
	"{date_long}":  "Monday, January 2, 2006",
}

// dateTokenTimeLayout is the Go time layout {time} and {time_secs}
// render with. It mirrors the appearance.timestamp_format config
// setting, seeded once at startup by SetDateTokenTimeLayout, so clock
// renderings from <!date tokens match the clock renderings of slk's
// own message stamps. Package-level rather than threaded through
// RenderSlackMarkdownOpts because every render path (messages pane,
// thread pane, blockkit blocks, activity/threads previews, search
// snippets) must agree, and the styles package already holds theme
// state the same way. The default matches timestamp_format's default.
var dateTokenTimeLayout = "3:04 PM"

// SetDateTokenTimeLayout overrides the {time} clock layout. Empty
// strings are ignored so a missing config value keeps the default.
func SetDateTokenTimeLayout(layout string) {
	if layout != "" {
		dateTokenTimeLayout = layout
	}
}

// TimestampLayout is the clock layout every clock in the TUI formats
// with: the seeded appearance.timestamp_format value, or its default
// "3:04 PM" when nothing was set. {time} and {time_secs} render with it
// internally; surfaces outside this package (the profile dialog's
// local-time and DND-end clocks, the DM header's DND suffix) read it
// here so one setting drives every clock.
func TimestampLayout() string {
	return dateTokenTimeLayout
}

// timeLayoutWithSeconds returns the layout with a seconds component
// inserted after the minutes token unless one is already present, so
// {time_secs} stays a superset of {time} for any configured layout.
// Go's seconds token is "05" wherever it appears (":05", ".05", …), so
// its presence anywhere means the layout already renders seconds.
func timeLayoutWithSeconds(layout string) string {
	if strings.Contains(layout, "05") {
		return layout
	}
	if strings.Contains(layout, "04") {
		return strings.Replace(layout, "04", "04:05", 1)
	}
	// Zero-suppressed minutes ("15:4"); anything with neither a
	// minute token nor seconds is not a clock layout, so leave it.
	if strings.Contains(layout, "4") {
		return strings.Replace(layout, "4", "4:05", 1)
	}
	return layout
}

// renderDateToken formats one whole <!date^...> token match. The token
// body between <!date^ and > is split on | into the TS^FORMAT head, the
// FALLBACK, and (optionally) a LINK, which is dropped like the official
// clients do. On any malformed input it returns the FALLBACK when one
// exists, else the raw token, so no data is silently eaten.
func renderDateToken(match string) string {
	body := dateTokenRe.FindStringSubmatch(match)[1]

	parts := strings.SplitN(body, "|", 3)
	fallback := ""
	if len(parts) > 1 {
		fallback = parts[1]
	}

	comps := strings.SplitN(parts[0], "^", 3)
	if len(comps) < 2 {
		if fallback != "" {
			return fallback
		}
		return match
	}
	// comps[2], when present, is a caret-form link; display ignores it.
	return formatSlackDateToken(comps[0], comps[1], fallback, match)
}

// formatSlackDateToken renders ts (unix seconds) in the local timezone
// with Slack's FORMAT tokens. Unknown or missing tokens fall back:
// without any recognized {token} the format is probably display text
// (the FALLBACK's job), and expanding it would produce worse output
// than the precomputed fallback.
func formatSlackDateToken(ts, format, fallback, raw string) string {
	return formatSlackDateTokenIn(ts, format, fallback, raw, time.Local)
}

// formatSlackDateTokenIn is formatSlackDateToken with an explicit
// location so tests can pin a timezone.
func formatSlackDateTokenIn(ts, format, fallback, raw string, loc *time.Location) string {
	sec, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil || format == "" || !slackDateFormatRe.MatchString(format) {
		if fallback != "" {
			return fallback
		}
		return raw
	}

	t := time.Unix(sec, 0).In(loc)
	out := slackDateFormatRe.ReplaceAllStringFunc(format, func(tok string) string {
		return slackDateFormat(t, tok)
	})
	if out == "" {
		if fallback != "" {
			return fallback
		}
		return raw
	}
	return out
}

// slackDateFormat expands one Slack date token at t.
func slackDateFormat(t time.Time, tok string) string {
	if layout, ok := slackDateTokens[tok]; ok {
		return t.Format(layout)
	}
	day := t.Day()
	switch tok {
	case "{time}":
		return t.Format(dateTokenTimeLayout)
	case "{time_secs}":
		return t.Format(timeLayoutWithSeconds(dateTokenTimeLayout))
	case "{date_short_pretty}":
		return t.Format("Jan") + fmt.Sprintf(" %d%s, ", day, ordinalSuffix(day)) + t.Format("2006")
	case "{date_pretty}":
		return t.Format("January") + fmt.Sprintf(" %d%s, ", day, ordinalSuffix(day)) + t.Format("2006")
	case "{date_long_pretty}":
		return t.Format("Monday, January") + fmt.Sprintf(" %d%s, ", day, ordinalSuffix(day)) + t.Format("2006")
	}
	return tok
}

// ordinalSuffix returns the English ordinal suffix for a day number.
func ordinalSuffix(day int) string {
	if day%100 >= 11 && day%100 <= 13 {
		return "th"
	}
	switch day % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	default:
		return "th"
	}
}
