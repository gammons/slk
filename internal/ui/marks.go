// internal/ui/marks.go
//
// The merged marks storage the UI reads through: one in-memory
// per-workspace map for lowercase (session-only) marks, plus the
// SQLite-backed `marks` table for uppercase marks (and for lowercase
// marks when the persist_all option is on). No caller knows which tier
// a letter came from. Turning persist_all off stops loading lowercase
// rows — it never deletes them, so toggling the option is not
// destructive: flipping it back on restores the marks.
package ui

import "errors"

// Mark is one recorded location plus the preview snapshot taken at
// mark time. The snapshot exists so the overlay can render offline and
// immediately after a restart, with no network round trip; the jump
// resolves live and may land on newer content than the snapshot.
type Mark struct {
	Location
	// Letter is the mark name ('a'-'z' lowercase, 'A'-'Z' uppercase).
	Letter      string
	ChannelName string
	AuthorName  string
	Excerpt     string
}

// isUpper reports whether letter names an uppercase mark. Uppercase
// marks always persist; lowercase marks persist only with persist_all.
func isUpper(letter string) bool {
	return len(letter) == 1 && letter[0] >= 'A' && letter[0] <= 'Z'
}

// MarksPersistStore is the SQLite-backed half of the marks storage:
// the `marks` table in internal/cache. Wired from main via
// SetMarksPersistStore with an adapter over *cache.DB; nil until then
// makes the store session-only.
type MarksPersistStore interface {
	UpsertMark(teamID, letter string, m Mark) error
	ListMarks(teamID string) ([]Mark, error)
	DeleteMark(teamID, letter string) error
}

// MarksPersistStoreFuncs is the closure bundle for NewMarksPersistStore,
// mirroring ChannelServiceFuncs / ThreadServiceFuncs.
type MarksPersistStoreFuncs struct {
	Upsert func(teamID, letter string, m Mark) error
	List   func(teamID string) ([]Mark, error)
	Delete func(teamID, letter string) error
}

// marksPersistAdapter adapts a MarksPersistStoreFuncs bundle to the
// MarksPersistStore interface, mirroring channelAdapter.
type marksPersistAdapter struct{ fns MarksPersistStoreFuncs }

// NewMarksPersistStore returns a MarksPersistStore backed by the given
// closures. nil closures are no-ops.
func NewMarksPersistStore(fns MarksPersistStoreFuncs) MarksPersistStore {
	return marksPersistAdapter{fns: fns}
}

func (a marksPersistAdapter) UpsertMark(teamID, letter string, m Mark) error {
	if a.fns.Upsert == nil {
		return nil
	}
	return a.fns.Upsert(teamID, letter, m)
}

func (a marksPersistAdapter) ListMarks(teamID string) ([]Mark, error) {
	if a.fns.List == nil {
		return nil, nil
	}
	return a.fns.List(teamID)
}

func (a marksPersistAdapter) DeleteMark(teamID, letter string) error {
	if a.fns.Delete == nil {
		return nil
	}
	return a.fns.Delete(teamID, letter)
}

// marksStore merges the session tier and the persistence tier behind
// one set of accessors:
//
//   - session holds the per-workspace lowercase marks set this session;
//   - the table holds uppercase marks always and lowercase marks when
//     persistAll is on.
//
// Writes are synchronous and report failure through Set's error, which
// the key handler turns into a toast: a mark the user believes they set
// must not silently fail to exist. See Set for which cases can fail.
type marksStore struct {
	persist    MarksPersistStore // nil until wired (session-only)
	persistAll bool
	session    map[string]map[string]Mark // workspace -> letter -> mark
}

func newMarksStore() *marksStore {
	return &marksStore{session: make(map[string]map[string]Mark)}
}

// Set records m under letter in teamID. Overwriting a letter replaces
// the previous location without confirmation. Uppercase marks always
// reach the table; lowercase marks do only when persistAll is on, and
// are kept in the session tier either way.
//
// Returns an error when the mark did not end up where the caller asked
// for it. An uppercase mark has no session copy, so a missing backend or
// a failed write means it was not recorded at all. A lowercase mark has
// a session copy either way, so it returns nil by default — but under
// persist_all the user has explicitly asked lowercase marks to survive,
// and a failed write there means the mark silently disappears at the
// next restart, so the error is reported.
func (s *marksStore) Set(teamID, letter string, m Mark) error {
	if letter == "" {
		return nil
	}
	var persistErr error
	if isUpper(letter) || s.persistAll {
		if s.persist == nil {
			persistErr = errors.New("mark storage not wired")
		} else if err := s.persist.UpsertMark(teamID, letter, m); err != nil {
			persistErr = err
		}
	}
	if !isUpper(letter) {
		if s.session[teamID] == nil {
			s.session[teamID] = make(map[string]Mark)
		}
		s.session[teamID][letter] = m
	}
	if isUpper(letter) || s.persistAll {
		return persistErr
	}
	return nil
}

// Load returns the mark recorded under letter in teamID. The session
// tier wins for lowercase marks (it is fresher and the only copy when
// persist_all is off); everything else resolves through the table.
func (s *marksStore) Load(teamID, letter string) (Mark, bool) {
	if !isUpper(letter) {
		if m, ok := s.session[teamID][letter]; ok {
			return m, true
		}
		if !s.persistAll {
			return Mark{}, false
		}
	}
	if s.persist == nil {
		return Mark{}, false
	}
	marks, err := s.persist.ListMarks(teamID)
	if err != nil {
		return Mark{}, false
	}
	for _, m := range marks {
		if m.Letter == letter {
			return m, true
		}
	}
	return Mark{}, false
}

// List returns every mark in teamID, lowercase letters first then
// uppercase, both in alphabetical order. Lowercase rows that reached
// the table during a persist_all-on period are excluded while the
// option is off.
func (s *marksStore) List(teamID string) []Mark {
	merged := make(map[string]Mark)
	if s.persist != nil {
		if marks, err := s.persist.ListMarks(teamID); err == nil {
			for _, m := range marks {
				if isUpper(m.Letter) || s.persistAll {
					merged[m.Letter] = m
				}
			}
		}
	}
	for letter, m := range s.session[teamID] {
		merged[letter] = m
	}
	out := make([]Mark, 0, len(merged))
	for letter := 'a'; letter <= 'z'; letter++ {
		if m, ok := merged[string(letter)]; ok {
			out = append(out, m)
		}
	}
	for letter := 'A'; letter <= 'Z'; letter++ {
		if m, ok := merged[string(letter)]; ok {
			out = append(out, m)
		}
	}
	return out
}

// Delete removes letter from teamID, from the session tier and from
// the table. Always touching the table means a lowercase row left over
// from a persist_all-on period is really gone — it must not resurrect
// the mark if the option is turned back on.
func (s *marksStore) Delete(teamID, letter string) {
	delete(s.session[teamID], letter)
	if s.persist != nil {
		_ = s.persist.DeleteMark(teamID, letter)
	}
}

// SetMarksPersistStore wires the SQLite-backed half of the marks
// storage (an adapter over *cache.DB, from main).
func (a *App) SetMarksPersistStore(s MarksPersistStore) {
	a.marks.persist = s
}

// SetMarksPersistAll toggles whether lowercase marks are written to
// the marks table. Turning it off never deletes previously persisted
// lowercase rows; it only stops loading and writing them.
func (a *App) SetMarksPersistAll(b bool) {
	a.marks.persistAll = b
}
