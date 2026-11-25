package presencemenu

import "time"

// ClearAfter is when a status set from the status dialog clears. The
// choices and their order match Slack's own status dialog.
type ClearAfter int

const (
	DontClear ClearAfter = iota
	ClearIn30Minutes
	ClearIn1Hour
	ClearIn4Hours
	ClearToday
	ClearThisWeek
)

// ClearAfterChoices lists the choices in the order the dialog shows them.
var ClearAfterChoices = []ClearAfter{DontClear, ClearIn30Minutes, ClearIn1Hour, ClearIn4Hours, ClearToday, ClearThisWeek}

func (c ClearAfter) String() string {
	switch c {
	case ClearIn30Minutes:
		return "30 minutes"
	case ClearIn1Hour:
		return "1 hour"
	case ClearIn4Hours:
		return "4 hours"
	case ClearToday:
		return "Today"
	case ClearThisWeek:
		return "This week"
	default:
		return "Don't clear"
	}
}

// Expiry returns the Unix time a status set at now clears, or 0 for
// Don't clear, which is how Slack spells "no expiry".
func (c ClearAfter) Expiry(now time.Time) int64 {
	switch c {
	case ClearIn30Minutes:
		return now.Add(30 * time.Minute).Unix()
	case ClearIn1Hour:
		return now.Add(time.Hour).Unix()
	case ClearIn4Hours:
		return now.Add(4 * time.Hour).Unix()
	case ClearToday:
		return EndOfDay(now)
	case ClearThisWeek:
		return EndOfWeek(now)
	default:
		return 0
	}
}

// EndOfDay returns 23:59:59 local time on now's date, as a Unix time.
func EndOfDay(now time.Time) int64 {
	y, m, d := now.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, now.Location()).Unix()
}

// EndOfWeek returns 23:59:59 local time on the Sunday ending now's
// week (Monday to Sunday), as a Unix time.
// shortcut: Sunday is assumed, not yet checked against the expiry
// Slack's own client sends for "This week"; calibrate before the PR.
func EndOfWeek(now time.Time) int64 {
	daysToSunday := (7 - int(now.Weekday())) % 7
	return EndOfDay(now.AddDate(0, 0, daysToSunday))
}
