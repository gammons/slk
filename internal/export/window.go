// This file defines the date window a channel export covers. It parses
// the --since/--until calendar dates in the requested timezone, widens
// them by the --overlap margin, and answers whether a Slack timestamp
// falls inside the result. It is pure time arithmetic with no I/O.

package export

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// dateLayout is the calendar-date form --since and --until accept.
const dateLayout = "2006-01-02"

// ErrInvalidWindow is returned by NewWindow when the requested dates,
// timezone or overlap cannot describe a non-empty export window.
var ErrInvalidWindow = errors.New("invalid export window")

// Window is the half-open interval of a channel export. Since and Until
// are the dates the user asked for; Start and End are those bounds
// widened by the overlap margin, and are what messages are tested
// against. All four are midnights in Location.
type Window struct {
	Since       time.Time
	Until       time.Time
	Start       time.Time
	End         time.Time
	OverlapDays int
	Location    *time.Location
}

// NewWindow builds the export window from the raw flag values. since is
// inclusive and until is exclusive, both "2006-01-02" dates read in
// timezone (an IANA name; "" means the local zone). An empty until
// means the day after now, so the export runs through today. The
// overlap is added as calendar days on both sides, which keeps the
// bounds at local midnight across DST changes.
func NewWindow(since, until, timezone string, overlapDays int, now time.Time) (Window, error) {
	if overlapDays < 0 {
		return Window{}, fmt.Errorf("%w: overlap must not be negative, got %d", ErrInvalidWindow, overlapDays)
	}
	loc := time.Local
	if timezone != "" {
		var err error
		if loc, err = time.LoadLocation(timezone); err != nil {
			return Window{}, fmt.Errorf("%w: timezone %q: %w", ErrInvalidWindow, timezone, err)
		}
	}
	if since == "" {
		return Window{}, fmt.Errorf("%w: since date is required", ErrInvalidWindow)
	}
	sinceT, err := time.ParseInLocation(dateLayout, since, loc)
	if err != nil {
		return Window{}, fmt.Errorf("%w: since %q is not a YYYY-MM-DD date", ErrInvalidWindow, since)
	}
	var untilT time.Time
	if until == "" {
		y, m, d := now.In(loc).Date()
		untilT = time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	} else if untilT, err = time.ParseInLocation(dateLayout, until, loc); err != nil {
		return Window{}, fmt.Errorf("%w: until %q is not a YYYY-MM-DD date", ErrInvalidWindow, until)
	}
	if !sinceT.Before(untilT) {
		return Window{}, fmt.Errorf("%w: since %s must be before until %s", ErrInvalidWindow, sinceT.Format(dateLayout), untilT.Format(dateLayout))
	}
	return Window{
		Since:       sinceT,
		Until:       untilT,
		Start:       sinceT.AddDate(0, 0, -overlapDays),
		End:         untilT.AddDate(0, 0, overlapDays),
		OverlapDays: overlapDays,
		Location:    loc,
	}, nil
}

// Contains reports whether the Slack timestamp ts falls inside the
// widened window [Start, End). An unparseable ts is outside it.
func (w Window) Contains(ts string) bool {
	t, ok := TimeFromTS(ts)
	if !ok {
		return false
	}
	return !t.Before(w.Start) && t.Before(w.End)
}

// OldestTS returns Start as a Slack timestamp, for use as the oldest
// bound of a conversations.history or conversations.replies request.
func (w Window) OldestTS() string {
	return formatTS(w.Start)
}

// LatestTS returns End as a Slack timestamp, for use as the latest
// bound of a conversations.history or conversations.replies request.
func (w Window) LatestTS() string {
	return formatTS(w.End)
}

// TimeFromTS converts a Slack timestamp ("1700000001.000100") to a
// time.Time at whole-second precision. The fractional part is a
// per-second sequence number rather than a clock reading, so it is
// dropped. ok is false for empty or malformed input.
func TimeFromTS(ts string) (time.Time, bool) {
	secs, _, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// formatTS renders t as a Slack timestamp with a zero sequence number.
func formatTS(t time.Time) string {
	return fmt.Sprintf("%d.000000", t.Unix())
}
