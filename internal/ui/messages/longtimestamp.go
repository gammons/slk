package messages

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/ui/styles"
)

// timeFromTS returns the local time of a Slack timestamp
// ("1700000000.000100"). ok is false when the seconds part is not an
// integer.
func timeFromTS(ts string) (time.Time, bool) {
	sec, err := strconv.ParseInt(strings.SplitN(ts, ".", 2)[0], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// LongTimestamp returns short prefixed with the message's local date
// ("Tue Sep 29, " or "Mon Sep 29 2025, " when not the current year).
// Returns short unchanged when ts is unparseable or short is empty.
func LongTimestamp(ts, short string) string {
	if short == "" {
		return short
	}
	t, ok := timeFromTS(ts)
	if !ok {
		return short
	}
	layout := "Mon Jan 2, "
	if t.Year() != nowFunc().Year() {
		layout = "Mon Jan 2 2006, "
	}
	return t.Format(layout) + short
}

// SelectedHeader returns rendered with the header's styled short
// timestamp replaced by the styled long one, provided the resulting
// header line is no wider than maxWidth. Otherwise returns rendered
// unchanged.
//
// The first occurrence of the styled short timestamp in rendered is the
// header's: the only earlier Timestamp-styled text is the
// thread-broadcast label, whose text differs.
func SelectedHeader(rendered, header, ts, short string, maxWidth int) string {
	return ReplaceHeaderTimestamp(rendered, header, short, LongTimestamp(ts, short), maxWidth)
}

// ReplaceHeaderTimestamp returns rendered with the header's styled
// short timestamp replaced by the styled replacement, provided the
// resulting header line is no wider than maxWidth. Otherwise returns
// rendered unchanged. SelectedHeader is this with LongTimestamp as the
// replacement; the thread pane uses it directly to date its parent.
func ReplaceHeaderTimestamp(rendered, header, short, replacement string, maxWidth int) string {
	if replacement == short {
		return rendered
	}
	styledShort := styles.Timestamp.Render(short)
	styledLong := styles.Timestamp.Render(replacement)
	longHeader := strings.Replace(header, styledShort, styledLong, 1)
	if longHeader == header || lipgloss.Width(longHeader) > maxWidth {
		return rendered
	}
	return strings.Replace(rendered, styledShort, styledLong, 1)
}
