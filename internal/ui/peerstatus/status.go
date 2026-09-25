// Package peerstatus holds a user's custom status and DND state and
// decides how they render. The sidebar, message headers, channel finder
// and DM header all show the same state, so expiry and glyph resolution
// are decided here once.
package peerstatus

import (
	"strings"
	"time"

	"github.com/gammons/slk/internal/emoji"
)

// DNDGlyph marks a user in Do Not Disturb. It is a single symbol so it
// can replace the one-glyph presence dot without shifting the row.
const DNDGlyph = "⊘"

// FallbackGlyph stands in for a status emoji that has no Unicode glyph
// (a custom workspace emoji) and for a status with text but no emoji.
const FallbackGlyph = "💬"

// HuddleGlyph marks a user in a huddle. It takes the status emoji's
// place, as the headphones do in Slack.
const HuddleGlyph = "🎧"

// Status is a user's custom status and DND state.
type Status struct {
	// Emoji is Slack's shortcode, colons included, e.g. ":calendar:".
	Emoji string
	Text  string
	// Expires is when the custom status ends; zero means never.
	Expires time.Time
	DND     bool
	// DNDEnd is when DND ends; zero means unknown.
	DNDEnd time.Time
	// Huddle is Slack's huddle_state; see InHuddle.
	Huddle string
	// HuddleExpires is huddle_state_expiration_ts; zero means unset.
	HuddleExpires time.Time
}

// WithHuddle returns s with its huddle state replaced.
func (s Status) WithHuddle(state string, expires time.Time) Status {
	s.Huddle, s.HuddleExpires = state, expires
	return s
}

// HuddleActive is the huddle_state Slack reports while a user is in a
// huddle, captured from live users/info results. Out of one it is
// "default_unset".
const HuddleActive = "in_a_huddle"

// InHuddle reports whether the user is in a huddle at now.
func (s Status) InHuddle(now time.Time) bool {
	if s.Huddle != HuddleActive {
		return false
	}
	return s.HuddleExpires.IsZero() || now.Before(s.HuddleExpires)
}

// WithStatus returns s with its custom status replaced.
func (s Status) WithStatus(emoji, text string, expires time.Time) Status {
	s.Emoji, s.Text, s.Expires = emoji, text, expires
	return s
}

// WithDND returns s with its DND state replaced.
func (s Status) WithDND(on bool, end time.Time) Status {
	s.DND, s.DNDEnd = on, end
	if !on {
		s.DNDEnd = time.Time{}
	}
	return s
}

// HasStatus reports whether a custom status is set and unexpired at now.
func (s Status) HasStatus(now time.Time) bool {
	if s.Emoji == "" && s.Text == "" {
		return false
	}
	return s.Expires.IsZero() || now.Before(s.Expires)
}

// InDND reports whether the user is in DND at now.
func (s Status) InDND(now time.Time) bool {
	return s.DND && (s.DNDEnd.IsZero() || now.Before(s.DNDEnd))
}

// Glyph returns the one glyph shown next to the user's name: the huddle
// headphones while they are in a huddle, else their status emoji, else
// "".
func (s Status) Glyph(now time.Time) string {
	if s.InHuddle(now) {
		return HuddleGlyph
	}
	return s.statusGlyph(now)
}

// statusGlyph is the custom status emoji as a terminal glyph, or "" when
// there is no live custom status.
func (s Status) statusGlyph(now time.Time) string {
	if !s.HasStatus(now) {
		return ""
	}
	if g := shortcodeGlyph(s.Emoji); g != "" {
		return g
	}
	return FallbackGlyph
}

// GlyphWidth is the cell width of Glyph(now), 0 when there is none.
func (s Status) GlyphWidth(now time.Time) int {
	g := s.Glyph(now)
	if g == "" {
		return 0
	}
	return emoji.Width(g)
}

// Summary is the one-line form for a DM header: the glyph and status
// text, then DND with its end time when known, formatted with layout.
// It is "" when there is nothing to show.
func (s Status) Summary(now time.Time, layout string) string {
	var parts []string
	if s.InHuddle(now) {
		parts = append(parts, HuddleGlyph+" In a huddle")
	}
	if s.HasStatus(now) {
		parts = append(parts, strings.TrimSpace(s.statusGlyph(now)+" "+s.Text))
	}
	if s.InDND(now) {
		dnd := DNDGlyph + " Do not disturb"
		if !s.DNDEnd.IsZero() {
			dnd += " until " + s.DNDEnd.Local().Format(layout)
		}
		parts = append(parts, dnd)
	}
	return strings.Join(parts, " · ")
}

// Expired reports whether a deadline in s has passed by now, so a render
// made earlier shows state that is no longer true.
func (s Status) Expired(now time.Time) bool {
	statusGone := (s.Emoji != "" || s.Text != "") && !s.Expires.IsZero() && !now.Before(s.Expires)
	dndGone := s.DND && !s.DNDEnd.IsZero() && !now.Before(s.DNDEnd)
	huddleGone := s.Huddle != "" && !s.HuddleExpires.IsZero() && !now.Before(s.HuddleExpires)
	return statusGone || dndGone || huddleGone
}

// HasDeadline reports whether s holds a status, DND or huddle that ends
// at a known time, so something must re-check it later.
func (s Status) HasDeadline() bool {
	return (!s.Expires.IsZero() && (s.Emoji != "" || s.Text != "")) ||
		(s.DND && !s.DNDEnd.IsZero()) ||
		(s.Huddle != "" && !s.HuddleExpires.IsZero())
}

// Clear returns s with every part whose deadline has passed removed.
func (s Status) Clear(now time.Time) Status {
	if !s.Expires.IsZero() && !now.Before(s.Expires) {
		s = s.WithStatus("", "", time.Time{})
	}
	if s.DND && !s.DNDEnd.IsZero() && !now.Before(s.DNDEnd) {
		s = s.WithDND(false, time.Time{})
	}
	if s.Huddle != "" && !s.HuddleExpires.IsZero() && !now.Before(s.HuddleExpires) {
		s = s.WithHuddle("", time.Time{})
	}
	return s
}

func shortcodeGlyph(code string) string {
	name := strings.Trim(code, ":")
	if name == "" {
		return ""
	}
	return emoji.CodeMap()[":"+emoji.StripSkinTone(name)+":"]
}
