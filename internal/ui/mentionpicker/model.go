package mentionpicker

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/gammons/slk/internal/text"
	"github.com/gammons/slk/internal/ui/styles"
)

const MaxVisible = 7

type User struct {
	ID          string
	DisplayName string
	Username    string
	// InChannel indicates whether the user is a member of the active
	// channel. False sorts the user into the not-in-channel tier (below
	// in-channel users in the picker). The caller is responsible for
	// populating this field. Special mentions (@here / @channel /
	// @everyone) always have InChannel=true. For workspace users, the
	// compose layer computes the value from the active channel's member
	// set (see compose.rebuildMentionUsers); when no membership data
	// has been loaded yet, compose defaults it to true to preserve the
	// pre-channel-aware behavior.
	InChannel bool
	// IsExternal indicates a Slack Connect / shared-channel guest
	// whose home team_id differs from the workspace TeamID. Drives
	// the "(ext)" suffix in the picker.
	IsExternal bool
}

type MentionResult struct {
	UserID      string
	DisplayName string
}

var specialMentions = []User{
	{ID: "special:here", DisplayName: "here", Username: "here", InChannel: true},
	{ID: "special:channel", DisplayName: "channel", Username: "channel", InChannel: true},
	{ID: "special:everyone", DisplayName: "everyone", Username: "everyone", InChannel: true},
}

type Model struct {
	users    []User
	filtered []User
	query    string
	selected int
	visible  bool
}

func New() Model {
	return Model{}
}

func (m *Model) SetUsers(users []User) {
	m.users = users
}

// Users returns the user list most recently set via SetUsers.
// Used by tests; not part of the picker's input API.
func (m *Model) Users() []User {
	return m.users
}

func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = 0
	m.filter()
}

func (m *Model) Close() {
	m.visible = false
	m.query = ""
	m.selected = 0
	m.filtered = nil
}

func (m *Model) IsVisible() bool {
	return m.visible
}

func (m *Model) SetQuery(q string) {
	m.query = q
	m.selected = 0
	m.filter()
}

func (m *Model) Query() string {
	return m.query
}

func (m *Model) Filtered() []User {
	return m.filtered
}

func (m *Model) Selected() int {
	return m.selected
}

func (m *Model) MoveUp() {
	if m.selected > 0 {
		m.selected--
	}
}

func (m *Model) MoveDown() {
	if m.selected < len(m.filtered)-1 {
		m.selected++
	}
}

func (m *Model) Select() *MentionResult {
	if len(m.filtered) == 0 {
		return nil
	}
	if m.selected < 0 || m.selected >= len(m.filtered) {
		return nil
	}
	u := m.filtered[m.selected]
	return &MentionResult{
		UserID:      u.ID,
		DisplayName: u.DisplayName,
	}
}

// rankedUser pairs a candidate with its match rank so the sort can put
// stronger matches first within a membership tier.
type rankedUser struct {
	user User
	rank matchRank
}

// sortRanked orders candidates by match rank, then display name, then ID.
// sort.Slice is unstable, so the ID tie-break keeps the dropdown from
// reshuffling between identical keystrokes when two entries share a
// display name.
func sortRanked(s []rankedUser) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].rank != s[j].rank {
			return s[i].rank < s[j].rank
		}
		if s[i].user.DisplayName != s[j].user.DisplayName {
			return s[i].user.DisplayName < s[j].user.DisplayName
		}
		return s[i].user.ID < s[j].user.ID
	})
}

func (m *Model) filter() {
	q := text.Fold(m.query)
	sq := squash(q)

	var specials []User
	var inCh, notInCh []rankedUser
	for _, u := range specialMentions {
		if rankUser(u, q, sq) != rankNone {
			specials = append(specials, u)
		}
	}
	for _, u := range m.users {
		r := rankUser(u, q, sq)
		if r == rankNone {
			continue
		}
		if u.InChannel {
			inCh = append(inCh, rankedUser{user: u, rank: r})
		} else {
			notInCh = append(notInCh, rankedUser{user: u, rank: r})
		}
	}

	sortRanked(inCh)
	sortRanked(notInCh)

	results := make([]User, 0, len(specials)+len(inCh)+len(notInCh))
	results = append(results, specials...)
	for _, r := range inCh {
		results = append(results, r.user)
	}
	for _, r := range notInCh {
		results = append(results, r.user)
	}

	if len(results) > MaxVisible {
		results = results[:MaxVisible]
	}
	m.filtered = results
}

func (m *Model) View(width int) string {
	if !m.visible || len(m.filtered) == 0 {
		return ""
	}

	var rows []string
	for i, u := range m.filtered {
		selected := i == m.selected

		indicator := "  "
		if selected {
			indicator = lipgloss.NewStyle().Foreground(styles.Accent).Render("▌ ")
		}

		// Name color: muted for not-in-channel rows at rest;
		// upgraded to TextPrimary when selected (per spec).
		nameColor := styles.TextPrimary
		if !u.InChannel && !selected {
			nameColor = styles.TextMuted
		}
		nameStyle := lipgloss.NewStyle().Foreground(nameColor)
		if selected {
			nameStyle = nameStyle.Bold(true)
		}

		// Special mentions keep "(Username)" in TextPrimary (the
		// historical, intentional behavior). Regular users get no
		// parenthetical here (the dead empty-parens fix).
		label := u.DisplayName
		if u.Username != "" {
			label = fmt.Sprintf("%s (%s)", u.DisplayName, u.Username)
		}

		row := indicator + nameStyle.Render(label)

		// Suffix in TextMuted, always (even when row is selected).
		var suffix string
		switch {
		case !u.InChannel:
			suffix = " (not in channel)"
		case u.IsExternal:
			suffix = " (ext)"
		}
		if suffix != "" {
			row += lipgloss.NewStyle().Foreground(styles.TextMuted).Render(suffix)
		}

		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Primary).
		Background(styles.SurfaceDark).
		Width(width - 2).
		Render(content)

	return box
}
