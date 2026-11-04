// Package newmessagepicker is the Ctrl+N modal that lets the user
// start a DM (one recipient) or group DM / MPIM (2-8 recipients).
//
// The model is a self-contained Bubble Tea sub-model mirroring the
// channelfinder package: a fuzzy filter over a user list with a
// pill-bar multi-select layered on top. Submission returns a Result
// carrying the chosen user IDs; the caller (the App's mode handler)
// is responsible for dispatching the conversations.open call.
package newmessagepicker

import (
	"unicode"
	"unicode/utf8"

	"github.com/gammons/slk/internal/bubbles/chrome"
	"github.com/gammons/slk/internal/ui/messages"
)

var _ interface {
	chrome.Dialog
	chrome.Clickable
} = (*Model)(nil)

// MaxRecipients is Slack's hard cap on the number of OTHER users in
// a multi-person direct message (MPIM). Slack itself caps total
// MPIM participants at 9, so up to 8 other users plus self.
const MaxRecipients = 8

// User is one row in the picker's list. DisplayName is the
// human-friendly name shown to the user; Username is the Slack handle
// (without the leading @). Recency is the unix-second timestamp of
// the most recent activity tied to this user; higher values sort
// earlier under empty-query and break ties under a query. IsExternal
// drives the [ext] tag in the rendered row.
type User struct {
	ID          string
	DisplayName string
	Username    string
	IsExternal  bool
	Recency     int64
}

// Result is returned by HandleKey when the user submits the picker.
// UserIDs is the list of recipients to pass to conversations.open;
// it always contains at least one ID when non-nil.
type Result struct {
	UserIDs []string
}

// Model is the picker's state. Constructed with New() and held on the
// App while ModeNewMessage is active.
type Model struct {
	users         []User
	filtered      []int // indices into users matching query + not-self
	query         string
	selected      map[string]struct{} // user IDs in the pill bar
	highlight     int                 // index into filtered
	visible       bool
	currentUserID string
}

// New constructs an empty picker. SetUsers and (optionally)
// SetCurrentUserID should be called before Open.
func New() Model {
	return Model{
		selected: map[string]struct{}{},
	}
}

// SetUsers replaces the user list the picker filters over.
// Does not trigger a re-filter; that happens on Open.
func (m *Model) SetUsers(users []User) {
	m.users = users
}

// Users returns the user list most recently set via SetUsers. Used
// by tests; not part of the picker's input API.
func (m *Model) Users() []User {
	return m.users
}

// SetCurrentUserID configures which user ID represents "self" so the
// picker can hide it from the list (a user cannot start a DM with
// themselves via this flow). May be called once at app start.
func (m *Model) SetCurrentUserID(userID string) {
	m.currentUserID = userID
}

// Open shows the picker and resets per-session state: query, pill
// selection, highlight, and recomputes the filtered list.
func (m *Model) Open() {
	m.visible = true
	m.query = ""
	m.selected = map[string]struct{}{}
	m.highlight = 0
	m.filter()
}

// Close hides the picker. Does not clear users; Open will re-filter.
func (m *Model) Close() {
	m.visible = false
}

// IsVisible reports whether the picker is currently open.
func (m Model) IsVisible() bool {
	return m.visible
}

// listTopOffset is the box-local row of the first result row.
func listTopOffset() int { return chrome.BodyTop(messages.ChromeStyles(), true) }

// maxVisibleRows is the height of the results scroll window.
const maxVisibleRows = 10

// boxWidth returns the modal's outer width for a given terminal width.
func boxWidth(termWidth int) int {
	w := termWidth / 2
	if w < 40 {
		w = 40
	}
	if w > 80 {
		w = 80
	}
	return w
}

// ClickRow moves the highlight cursor to the result row at box-local
// localY and returns true when the click lands on a visible row. Unlike
// the single-select finders this only moves the cursor; the caller
// synthesizes a toggle (Space) so multi-select semantics are preserved.
func (m *Model) ClickRow(termWidth, termHeight, localY int) bool {
	row := localY - listTopOffset()
	if row < 0 {
		return false
	}
	start, end := chrome.Window(m.highlight, len(m.filtered), maxVisibleRows)
	if row >= end-start {
		return false
	}
	m.highlight = start + row
	return true
}

// setQuery is a test helper: replaces the query and refilters. The
// real keystroke API in Task 4 will go through HandleKey.
func (m *Model) setQuery(q string) {
	m.query = q
	m.highlight = 0
	m.filter()
}

// HandleKey processes a single key event. The string form mirrors the
// other picker packages (channelfinder, mentionpicker): "up", "down",
// "ctrl+n", "ctrl+p", "backspace", "esc", "enter", "tab", " ", or
// any single printable character.
//
// Returns a non-nil Result when the user submits the picker; the
// caller (the mode handler) closes the picker and dispatches the
// conversations.open call.
//
// This task wires the navigation arms only. Selection (Space/Tab)
// lands in this task too; submit semantics (Enter) land in Task 5.
func (m *Model) HandleKey(keyStr string) *Result {
	switch keyStr {
	case "enter":
		return m.submit()
	case "esc":
		m.Close()
		return nil
	case "down", "ctrl+n":
		if m.highlight < len(m.filtered)-1 {
			m.highlight++
		}
		return nil
	case "up", "ctrl+p":
		if m.highlight > 0 {
			m.highlight--
		}
		return nil
	case " ", "tab":
		m.toggleHighlightedSelection()
		return nil
	case "backspace":
		m.handleBackspace()
		return nil
	}

	// Single printable rune -> append to query. Space never gets here:
	// the switch above takes it as toggle-selection.
	if r := []rune(keyStr); len(r) == 1 && unicode.IsPrint(r[0]) {
		m.query += keyStr
		m.highlight = 0
		m.filter()
	}
	return nil
}

// toggleHighlightedSelection adds or removes the currently-highlighted
// user from the pill bar. No-op when nothing is highlighted, when the
// pill bar is at MaxRecipients capacity, or when the highlighted user
// is already selected (in which case it removes them).
func (m *Model) toggleHighlightedSelection() {
	if m.highlight < 0 || m.highlight >= len(m.filtered) {
		return
	}
	userID := m.users[m.filtered[m.highlight]].ID
	if _, already := m.selected[userID]; already {
		delete(m.selected, userID)
		return
	}
	if len(m.selected) >= MaxRecipients {
		return
	}
	m.selected[userID] = struct{}{}
	// Clear the query on ADD so the user can immediately type the next
	// recipient. On REMOVE (handled above), keep the query — that path
	// is a course-correction, not a move to the next person.
	if m.query != "" {
		m.query = ""
		m.highlight = 0
		m.filter()
	}
}

// handleBackspace deletes the last rune of the query. If the query is
// already empty AND the pill bar is non-empty, it removes the LAST
// pill instead — letting the user erase a wrong recipient without
// reaching for the mouse.
func (m *Model) handleBackspace() {
	if len(m.query) > 0 {
		_, n := utf8.DecodeLastRuneInString(m.query)
		m.query = m.query[:len(m.query)-n]
		m.highlight = 0
		m.filter()
		return
	}
	if len(m.selected) == 0 {
		return
	}
	// No pill order is recorded; we use the last user ID inserted
	// into the selected set. Since Go map iteration is randomized, we
	// instead pop the one that appears last in m.users order — this
	// gives the user a stable, predictable "remove most recently
	// added" behavior tied to the user list order rather than a
	// hidden insertion order. (Alternative would be a slice of IDs;
	// YAGNI — a single backspace mid-flow is rare.)
	for i := len(m.users) - 1; i >= 0; i-- {
		id := m.users[i].ID
		if _, ok := m.selected[id]; ok {
			delete(m.selected, id)
			return
		}
	}
}

// submit returns a non-nil *Result when the picker has something to
// submit:
//   - If pills are present, the result carries the pill IDs.
//   - Otherwise, if a user is highlighted, the result carries that one ID.
//   - If neither is true (empty filter AND no pills), returns nil — Enter is a no-op.
//
// Pill order in the returned slice matches the user-list order so the
// caller sees a stable, predictable order across calls.
func (m *Model) submit() *Result {
	if len(m.selected) > 0 {
		ids := make([]string, 0, len(m.selected))
		for _, u := range m.users {
			if _, ok := m.selected[u.ID]; ok {
				ids = append(ids, u.ID)
			}
		}
		return &Result{UserIDs: ids}
	}
	if m.highlight < 0 || m.highlight >= len(m.filtered) {
		return nil
	}
	return &Result{UserIDs: []string{m.users[m.filtered[m.highlight]].ID}}
}
