package cache

import "testing"

// Observed version formats, taken from real Slack captures. Keep the
// literal shapes: channels stamp a 13-digit millisecond int, users a
// 10-digit second int, messages an opaque ts-like string.
const (
	sampleChannelVersion int64 = 1783337533019
	sampleUserVersion    int64 = 1612802061
	sampleMessageVersion       = "1783024685.163100"
)

func openVersionsTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedWorkspace(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.UpsertWorkspace(Workspace{ID: id, Name: id}); err != nil {
		t.Fatalf("UpsertWorkspace(%s): %v", id, err)
	}
}

func seedChannel(t *testing.T, db *DB, workspaceID, id, name string) {
	t.Helper()
	if err := db.UpsertChannel(Channel{
		ID:          id,
		WorkspaceID: workspaceID,
		Name:        name,
		Type:        "channel",
	}); err != nil {
		t.Fatalf("UpsertChannel(%s): %v", id, err)
	}
}

func seedUser(t *testing.T, db *DB, workspaceID, id, name string) {
	t.Helper()
	if err := db.UpsertUser(User{
		ID:          id,
		WorkspaceID: workspaceID,
		Name:        name,
	}); err != nil {
		t.Fatalf("UpsertUser(%s): %v", id, err)
	}
}

func seedMessage(t *testing.T, db *DB, workspaceID, channelID, ts, text string) {
	t.Helper()
	if err := db.UpsertMessage(Message{
		TS:          ts,
		ChannelID:   channelID,
		WorkspaceID: workspaceID,
		UserID:      "U1",
		Text:        text,
	}); err != nil {
		t.Fatalf("UpsertMessage(%s/%s): %v", channelID, ts, err)
	}
}

// A channel row with no version yet must appear with version 0 — that
// is how we ask Slack for the full record. The stamped sibling in this
// test is deliberate: without it, an implementation that returned every
// id mapped to 0 unconditionally would still pass.
func TestChannelVersions_ReturnsZeroForUnknown(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "unstamped")
	seedChannel(t, db, "T1", "C2", "stamped")

	if err := db.SetChannelVersion("C2", sampleChannelVersion); err != nil {
		t.Fatalf("SetChannelVersion: %v", err)
	}

	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if v, ok := got["C1"]; !ok || v != 0 {
		t.Errorf("ChannelVersions()[C1] = %v, ok=%v; want 0, true", v, ok)
	}
	if v, ok := got["C2"]; !ok || v != sampleChannelVersion {
		t.Errorf("ChannelVersions()[C2] = %v, ok=%v; want %d, true", v, ok, sampleChannelVersion)
	}
}

func TestChannelVersions_RoundTrip(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "general")

	if err := db.SetChannelVersion("C1", sampleChannelVersion); err != nil {
		t.Fatalf("SetChannelVersion: %v", err)
	}
	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if got["C1"] != sampleChannelVersion {
		t.Errorf("version = %d; want %d", got["C1"], sampleChannelVersion)
	}
}

func TestChannelVersions_ScopedToWorkspace(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedWorkspace(t, db, "T2")
	seedChannel(t, db, "T1", "C1", "here")
	seedChannel(t, db, "T2", "C2", "elsewhere")

	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if _, leaked := got["C2"]; leaked {
		t.Error("ChannelVersions leaked a channel from another workspace")
	}
	if _, ok := got["C1"]; !ok {
		t.Error("ChannelVersions dropped a channel from the requested workspace")
	}
}

// A workspace with no rows must yield an empty map, never a nil one.
// The distinction is not cosmetic: these maps are marshalled straight
// into edgeapi's updated_ids / cached_latest_updates, and
// encoding/json renders a nil map as `null` but an empty map as `{}`.
// "I hold nothing" and "I am not telling you what I hold" are
// different assertions to send Slack, and only the second is a lie.
//
// Every helper is covered because `var out map[...]` reads identically
// to `make(map[...])` at every call site inside this package; nothing
// short of an assertion here distinguishes them.
func TestVersions_EmptyResultIsNonNilMap(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "general")

	chans, err := db.ChannelVersions("T-none")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if chans == nil {
		t.Error("ChannelVersions returned a nil map for a workspace with no channels; want empty, non-nil (nil marshals as JSON null)")
	}

	users, err := db.UserVersions("T-none")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if users == nil {
		t.Error("UserVersions returned a nil map for a workspace with no users; want empty, non-nil (nil marshals as JSON null)")
	}

	msgs, err := db.MessageVersions("C1", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions: %v", err)
	}
	if msgs == nil {
		t.Error("MessageVersions returned a nil map for a channel with no versioned messages; want empty, non-nil (nil marshals as JSON null)")
	}
}

func TestUserVersions_RoundTrip(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedUser(t, db, "T1", "U1", "alice")

	if err := db.SetUserVersion("U1", sampleUserVersion); err != nil {
		t.Fatalf("SetUserVersion: %v", err)
	}
	got, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if got["U1"] != sampleUserVersion {
		t.Errorf("version = %d; want %d", got["U1"], sampleUserVersion)
	}
}

func TestUserVersions_ReturnsZeroForUnknown(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedUser(t, db, "T1", "U1", "unstamped")
	seedUser(t, db, "T1", "U2", "stamped")

	if err := db.SetUserVersion("U2", sampleUserVersion); err != nil {
		t.Fatalf("SetUserVersion: %v", err)
	}

	got, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if v, ok := got["U1"]; !ok || v != 0 {
		t.Errorf("UserVersions()[U1] = %v, ok=%v; want 0, true", v, ok)
	}
	if v, ok := got["U2"]; !ok || v != sampleUserVersion {
		t.Errorf("UserVersions()[U2] = %v, ok=%v; want %d, true", v, ok, sampleUserVersion)
	}
}

func TestUserVersions_ScopedToWorkspace(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedWorkspace(t, db, "T2")
	seedUser(t, db, "T1", "U1", "here")
	seedUser(t, db, "T2", "U2", "elsewhere")

	got, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if _, leaked := got["U2"]; leaked {
		t.Error("UserVersions leaked a user from another workspace")
	}
	if _, ok := got["U1"]; !ok {
		t.Error("UserVersions dropped a user from the requested workspace")
	}
}

func TestMessageVersions_RoundTripAndScope(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "general")
	seedMessage(t, db, "T1", "C1", "1700000001.000100", "one")
	seedMessage(t, db, "T1", "C1", "1700000002.000200", "two")

	if err := db.SetMessageVersion("C1", "1700000001.000100", sampleMessageVersion); err != nil {
		t.Fatalf("SetMessageVersion: %v", err)
	}
	got, err := db.MessageVersions("C1", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions: %v", err)
	}
	if got["1700000001.000100"] != sampleMessageVersion {
		t.Errorf("version = %q; want %s", got["1700000001.000100"], sampleMessageVersion)
	}
	// Messages with no version must be omitted, not sent as empty —
	// cached_latest_updates only carries messages we can vouch for.
	if _, present := got["1700000002.000200"]; present {
		t.Error("MessageVersions included a message with no version")
	}
}

// The [oldestTS, latestTS] bounds scope the assertion we send in
// cached_latest_updates to the window we actually asked about. Claiming
// versions outside that window is a lie about what the request covers.
//
// The interval is CLOSED, matching the doc comment on MessageVersions
// and conversations.history's own inclusive oldest/latest. The
// exactly-on-the-boundary cases below are load-bearing: with only
// strictly-interior fixtures, `ts > ? AND ts < ?` passes, and each
// history window then silently drops the version of its first and last
// message.
func TestMessageVersions_RespectsTSRange(t *testing.T) {
	db := openVersionsTestDB(t)
	const (
		oldestTS = "1700000001.000000"
		latestTS = "1700000003.000000"
	)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "general")
	for _, ts := range []string{
		"1700000000.000000", // below the window
		oldestTS,            // exactly on the lower bound: inclusive
		"1700000002.000000", // strictly inside
		latestTS,            // exactly on the upper bound: inclusive
		"1700000009.000000", // above the window
	} {
		seedMessage(t, db, "T1", "C1", ts, "m")
		if err := db.SetMessageVersion("C1", ts, sampleMessageVersion); err != nil {
			t.Fatalf("SetMessageVersion(%s): %v", ts, err)
		}
	}

	got, err := db.MessageVersions("C1", oldestTS, latestTS)
	if err != nil {
		t.Fatalf("MessageVersions: %v", err)
	}
	if _, ok := got["1700000002.000000"]; !ok {
		t.Error("MessageVersions dropped a message inside the requested window")
	}
	if _, ok := got[oldestTS]; !ok {
		t.Errorf("MessageVersions dropped the message at exactly oldestTS (%s); the window is closed, not open", oldestTS)
	}
	if _, ok := got[latestTS]; !ok {
		t.Errorf("MessageVersions dropped the message at exactly latestTS (%s); the window is closed, not open", latestTS)
	}
	if _, ok := got["1700000000.000000"]; ok {
		t.Error("MessageVersions included a message older than oldestTS")
	}
	if _, ok := got["1700000009.000000"]; ok {
		t.Error("MessageVersions included a message newer than latestTS")
	}
}

// Both channels hold a message at the SAME ts. That collision is the
// whole point of the fixture and must not be "simplified" away to
// distinct timestamps: `messages` has the composite primary key
// (ts, channel_id), so ts alone identifies nothing, and Slack ts values
// do collide across channels. With distinct ts values a channel-blind
// query still returns the right rows by accident and the filter goes
// untested.
//
// Only C2's copy is stamped. If MessageVersions dropped its
// channel_id predicate it would hand C2's version back to a caller
// asking about C1 — we would then assert in cached_latest_updates that
// we hold a version of a C1 message we have never seen, and Slack would
// answer `unchanged_messages`. The symptom is a *missing* message, not
// a stale one.
func TestMessageVersions_ScopedToChannel(t *testing.T) {
	db := openVersionsTestDB(t)
	const collidingTS = "1700000001.000100"
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "here")
	seedChannel(t, db, "T1", "C2", "elsewhere")
	seedMessage(t, db, "T1", "C1", collidingTS, "mine")
	seedMessage(t, db, "T1", "C2", collidingTS, "theirs")
	if err := db.SetMessageVersion("C2", collidingTS, sampleMessageVersion); err != nil {
		t.Fatalf("SetMessageVersion: %v", err)
	}

	// Guard against a vacuous pass: prove the stamp landed somewhere,
	// so "C1 has nothing" is scoping and not a silent write failure.
	owner, err := db.MessageVersions("C2", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions(C2): %v", err)
	}
	if owner[collidingTS] != sampleMessageVersion {
		t.Fatalf("MessageVersions(C2)[%s] = %q; want %s", collidingTS, owner[collidingTS], sampleMessageVersion)
	}

	got, err := db.MessageVersions("C1", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions(C1): %v", err)
	}
	if v, leaked := got[collidingTS]; leaked {
		t.Errorf("MessageVersions(C1)[%s] = %q; want absent — that version belongs to C2's message at the same ts",
			collidingTS, v)
	}
}

// The write side of the same collision. SetMessageVersion takes a
// channelID and must use it: `UPDATE messages SET version = ? WHERE
// ts = ?` compiles, passes any fixture with distinct timestamps, and
// stamps every channel's message that happens to share the ts.
//
// Here C1 is stamped and C2 must stay unstamped. A channel-blind
// UPDATE writes C1's version onto C2's row, and MessageVersions would
// then vouch for a C2 message we never fetched.
func TestSetMessageVersion_ScopedToChannel(t *testing.T) {
	db := openVersionsTestDB(t)
	const collidingTS = "1700000001.000100"
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "here")
	seedChannel(t, db, "T1", "C2", "elsewhere")
	seedMessage(t, db, "T1", "C1", collidingTS, "mine")
	seedMessage(t, db, "T1", "C2", collidingTS, "theirs")

	if err := db.SetMessageVersion("C1", collidingTS, sampleMessageVersion); err != nil {
		t.Fatalf("SetMessageVersion: %v", err)
	}

	target, err := db.MessageVersions("C1", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions(C1): %v", err)
	}
	if target[collidingTS] != sampleMessageVersion {
		t.Fatalf("MessageVersions(C1)[%s] = %q; want %s — the stamp did not land",
			collidingTS, target[collidingTS], sampleMessageVersion)
	}

	bystander, err := db.MessageVersions("C2", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions(C2): %v", err)
	}
	if v, stamped := bystander[collidingTS]; stamped {
		t.Errorf("MessageVersions(C2)[%s] = %q; want absent — SetMessageVersion(C1, ...) stamped C2's message at the same ts",
			collidingTS, v)
	}
}

// UpsertChannel's ON CONFLICT clause deliberately does not list
// `version`, so a routine re-upsert (any ordinary cache write) must
// leave a stamped version alone.
//
// If this test ever fails, slk will report version 0 for every channel
// forever, Slack will return every record in full on every boot, and we
// will have silently reintroduced the enumeration behaviour that gets
// Grid users signed out. It is not a style test.
func TestChannelVersion_SurvivesReUpsert(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "original")

	if err := db.SetChannelVersion("C1", sampleChannelVersion); err != nil {
		t.Fatalf("SetChannelVersion: %v", err)
	}

	// A routine cache write: same row, changed fields.
	seedChannel(t, db, "T1", "C1", "renamed")

	// Guard against a vacuous pass: prove the upsert really wrote.
	ch, err := db.GetChannel("C1")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if ch.Name != "renamed" {
		t.Fatalf("re-upsert did not take effect: name = %q, want renamed", ch.Name)
	}

	got, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if got["C1"] != sampleChannelVersion {
		t.Errorf("version after re-upsert = %d; want %d (UpsertChannel must not reset version)",
			got["C1"], sampleChannelVersion)
	}
}

// Same hazard as TestChannelVersion_SurvivesReUpsert, for users:
// UpsertUser must not clobber a stamped version, or every user comes
// back fully hydrated on every boot.
func TestUserVersion_SurvivesReUpsert(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedUser(t, db, "T1", "U1", "original")

	if err := db.SetUserVersion("U1", sampleUserVersion); err != nil {
		t.Fatalf("SetUserVersion: %v", err)
	}

	seedUser(t, db, "T1", "U1", "renamed")

	u, err := db.GetUser("U1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u.Name != "renamed" {
		t.Fatalf("re-upsert did not take effect: name = %q, want renamed", u.Name)
	}

	got, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if got["U1"] != sampleUserVersion {
		t.Errorf("version after re-upsert = %d; want %d (UpsertUser must not reset version)",
			got["U1"], sampleUserVersion)
	}
}

// Same hazard as TestChannelVersion_SurvivesReUpsert, for messages. A
// message is re-upserted on every edit and on every overlapping history
// fetch, so clobbering here would blank versions constantly and make
// cached_latest_updates permanently empty.
func TestMessageVersion_SurvivesReUpsert(t *testing.T) {
	db := openVersionsTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannel(t, db, "T1", "C1", "general")
	seedMessage(t, db, "T1", "C1", "1700000001.000100", "original")

	if err := db.SetMessageVersion("C1", "1700000001.000100", sampleMessageVersion); err != nil {
		t.Fatalf("SetMessageVersion: %v", err)
	}

	seedMessage(t, db, "T1", "C1", "1700000001.000100", "edited")

	m, err := db.GetMessage("C1", "1700000001.000100")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.Text != "edited" {
		t.Fatalf("re-upsert did not take effect: text = %q, want edited", m.Text)
	}

	got, err := db.MessageVersions("C1", "1700000000.000000", "1700000003.000000")
	if err != nil {
		t.Fatalf("MessageVersions: %v", err)
	}
	if got["1700000001.000100"] != sampleMessageVersion {
		t.Errorf("version after re-upsert = %q; want %s (UpsertMessage must not reset version)",
			got["1700000001.000100"], sampleMessageVersion)
	}
}
