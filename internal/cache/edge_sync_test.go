package cache

import (
	"database/sql"
	"errors"
	"testing"
)

func openEdgeSyncTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedChannelFull writes a channel with every column non-zero, so any
// column a later partial update wrongly clears is visible.
func seedChannelFull(t *testing.T, db *DB, workspaceID, id string) {
	t.Helper()
	seedChannelMembership(t, db, workspaceID, id, true)
}

// seedChannelMembership is seedChannelFull with is_member chosen by the
// caller. "Preserved" is only distinguishable from "set to true" or
// "set to false" if the fixture holds rows of both kinds, so every
// preservation assertion needs this rather than seedChannelFull.
func seedChannelMembership(t *testing.T, db *DB, workspaceID, id string, isMember bool) {
	t.Helper()
	if err := db.UpsertChannel(Channel{
		ID: id, WorkspaceID: workspaceID, Name: "original-name",
		Type: "channel", Topic: "original topic",
		IsMember: isMember, IsStarred: true, UpdatedAt: 111,
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
}

func assertMembership(t *testing.T, db *DB, id string, want bool, why string) {
	t.Helper()
	if got := getChannelRow(t, db, id).IsMember; got != want {
		t.Errorf("%s is_member = %v; want %v — %s", id, got, want, why)
	}
}

func getChannelRow(t *testing.T, db *DB, id string) Channel {
	t.Helper()
	ch, err := db.GetChannel(id)
	if err != nil {
		t.Fatalf("GetChannel(%s): %v", id, err)
	}
	return ch
}

func getUserRow(t *testing.T, db *DB, id string) User {
	t.Helper()
	u, err := db.GetUser(id)
	if err != nil {
		t.Fatalf("GetUser(%s): %v", id, err)
	}
	return u
}

func TestUpdateChannelFromEdge_PreservesColumnsEdgeCannotSupply(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannelFull(t, db, "T1", "C1")

	// What channels/info actually returns: name, type, topic, version.
	// NOT is_member (0 of 36 observed results carry it) and NOT
	// is_starred (no edge endpoint returns it).
	if err := db.UpdateChannelFromEdge(EdgeChannelUpdate{
		ID: "C1", Name: "new-name", Type: "private", Topic: "new topic",
		Version: sampleChannelVersion,
	}); err != nil {
		t.Fatalf("UpdateChannelFromEdge: %v", err)
	}

	got := getChannelRow(t, db, "C1")
	if got.Name != "new-name" || got.Topic != "new topic" || got.Type != "private" {
		t.Errorf("edge-owned columns not written: %+v", got)
	}
	// The whole point of this method existing.
	if !got.IsMember {
		t.Error("is_member was cleared; channels/info does not carry it, so it must be preserved — clearing it drops the user out of their own channels")
	}
	if !got.IsStarred {
		t.Error("is_starred was cleared; no edge endpoint returns it, so it must be preserved")
	}

	vers, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if vers["C1"] != sampleChannelVersion {
		t.Errorf("version = %d; want %d", vers["C1"], sampleChannelVersion)
	}
}

func TestApplyMembership_SetsAndClearsOnlyTheIDsQueried(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	for _, id := range []string{"C1", "C2", "C3"} {
		seedChannelFull(t, db, "T1", id)
	}

	// A reported member_channels is a snapshot over the ids SENT, not a
	// delta: an id that was sent, reported on and absent is a
	// non-membership; an id never sent says nothing. C3 was not queried
	// and must be untouched.
	if err := db.ApplyMembership("T1", []string{"C1", "C2"}, MembershipReported([]string{"C1"}, nil)); err != nil {
		t.Fatalf("ApplyMembership: %v", err)
	}

	if !getChannelRow(t, db, "C1").IsMember {
		t.Error("C1 was in member_channels and must stay a member")
	}
	if getChannelRow(t, db, "C2").IsMember {
		t.Error("C2 was queried and absent from member_channels, so it is a non-membership and must be cleared")
	}
	if !getChannelRow(t, db, "C3").IsMember {
		t.Error("C3 was never queried; ApplyMembership must not touch it — treating unqueried as non-member drops every channel not in the batch")
	}
	if !getChannelRow(t, db, "C2").IsStarred {
		t.Error("ApplyMembership must only write is_member")
	}
}

func TestUpdateUserFromEdge_PreservesColumnsEdgeCannotSupply(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{
		ID: "U1", WorkspaceID: "T1", Name: "orig", DisplayName: "Orig",
		AvatarURL: "https://example.invalid/orig.png", Presence: "active",
		IsBot: false, IsExternal: true, UpdatedAt: 222,
	}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// No AvatarURL supplied: users/info carries image_original on 255
	// of 291 observed results, but a user with no custom image has
	// none, and blanking a good URL on that basis is the bug.
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "new", DisplayName: "New", IsBot: true,
		Version: sampleUserVersion,
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge: %v", err)
	}

	u := getUserRow(t, db, "U1")
	if u.Name != "new" || u.DisplayName != "New" || !u.IsBot {
		t.Errorf("edge-owned columns not written: %+v", u)
	}
	if u.AvatarURL != "https://example.invalid/orig.png" {
		t.Errorf("avatar_url = %q; want the original preserved — an empty AvatarURL means 'this source has none', not 'this user has none'", u.AvatarURL)
	}
	if u.Presence != "active" {
		t.Errorf("presence = %q; want active preserved — no edge endpoint returns presence", u.Presence)
	}
	// is_external IS edge-owned: it is derived from the team_id every
	// edge result carries, so a revalidation that stops writing it
	// leaves a guest flagged after they join, or vice versa.
	if u.IsExternal {
		t.Error("is_external = true; want the update's false written — is_external is derived from team_id, which every edge result carries")
	}
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U1"] != sampleUserVersion {
		t.Errorf("version = %d; want %d — an unstamped row is sent as 0 in updated_ids and refetched in full on every boot, forever", vers["U1"], sampleUserVersion)
	}
}

// The avatar-carrying branch is a second SQL statement, so every column
// assertion made against the other branch has to be made here too or a
// column silently dropped from one of them goes unnoticed.
func TestUpdateUserFromEdge_AvatarBranchWritesTheSameColumns(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{
		ID: "U1", WorkspaceID: "T1", Name: "orig", DisplayName: "Orig",
		AvatarURL: "https://example.invalid/orig.png", Presence: "active",
		IsBot: false, IsExternal: true, UpdatedAt: 222,
	}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "new", DisplayName: "New",
		AvatarURL: "https://example.invalid/new.png",
		IsBot:     true, IsExternal: false, Version: sampleUserVersion,
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge: %v", err)
	}

	u := getUserRow(t, db, "U1")
	if u.Name != "new" || u.DisplayName != "New" || !u.IsBot {
		t.Errorf("edge-owned columns not written: %+v", u)
	}
	if u.IsExternal {
		t.Error("is_external not written by the avatar branch")
	}
	if u.Presence != "active" {
		t.Errorf("presence = %q; the avatar branch must preserve it too", u.Presence)
	}
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U1"] != sampleUserVersion {
		t.Errorf("version = %d; want %d — the avatar branch must stamp it too", vers["U1"], sampleUserVersion)
	}
}

func TestUpdateUserFromEdge_WritesAvatarWhenSupplied(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	if err := db.UpsertUser(User{
		ID: "U1", WorkspaceID: "T1", Name: "orig",
		AvatarURL: "https://example.invalid/orig.png", Presence: "active",
	}); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "orig", AvatarURL: "https://example.invalid/new.png",
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge: %v", err)
	}
	u := getUserRow(t, db, "U1")
	if u.AvatarURL != "https://example.invalid/new.png" {
		t.Errorf("avatar_url = %q; want the new one — preserve must not mean ignore", u.AvatarURL)
	}
}

// UpsertUserFromEdge is the row-creating counterpart of
// UpdateUserFromEdge, for the batched user resolver whose misses are
// by definition rows that do not exist. It must keep every contract of
// the UPDATE — above all the empty-avatar preserve — while inserting
// when the row is missing.
func TestUpsertUserFromEdge(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")

	// 1. A user that does not exist is INSERTED, with every column the
	// update carries.
	if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{
		ID: "U1", Name: "alice", DisplayName: "Alice",
		AvatarURL: "https://example.invalid/a.png",
		IsBot:     true, IsExternal: true, Version: sampleUserVersion,
	}); err != nil {
		t.Fatalf("UpsertUserFromEdge: %v", err)
	}
	u := getUserRow(t, db, "U1")
	if u.Name != "alice" || u.DisplayName != "Alice" || !u.IsBot || !u.IsExternal {
		t.Errorf("inserted row missing columns: %+v", u)
	}
	if u.AvatarURL != "https://example.invalid/a.png" {
		t.Errorf("avatar_url = %q; want the supplied one", u.AvatarURL)
	}
	// 4. version is written and readable through UserVersions — the
	// resolver's inserts are what conditional revalidation later reads.
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U1"] != sampleUserVersion {
		t.Errorf("version = %d; want %d — an unstamped row is sent as 0 in updated_ids and refetched in full on every boot, forever", vers["U1"], sampleUserVersion)
	}

	// 2. Empty AvatarURL on an existing row preserves the stored avatar,
	// the same contract UpdateUserFromEdge holds.
	if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{
		ID: "U1", Name: "alice2", DisplayName: "Alice Two",
		Version: sampleUserVersion + 1,
	}); err != nil {
		t.Fatalf("UpsertUserFromEdge (empty avatar): %v", err)
	}
	u = getUserRow(t, db, "U1")
	if u.AvatarURL != "https://example.invalid/a.png" {
		t.Errorf("avatar_url = %q; want the original preserved — an empty AvatarURL means 'this source has none', not 'this user has none'", u.AvatarURL)
	}
	if u.Name != "alice2" || u.DisplayName != "Alice Two" {
		t.Errorf("conflict path did not write edge-owned columns: %+v", u)
	}

	// 3. A non-empty AvatarURL replaces the stored one.
	if err := db.UpsertUserFromEdge("T1", EdgeUserUpdate{
		ID: "U1", Name: "alice2", DisplayName: "Alice Two",
		AvatarURL: "https://example.invalid/new.png", Version: sampleUserVersion + 2,
	}); err != nil {
		t.Fatalf("UpsertUserFromEdge (avatar): %v", err)
	}
	if got := getUserRow(t, db, "U1").AvatarURL; got != "https://example.invalid/new.png" {
		t.Errorf("avatar_url = %q; want the new one — preserve must not mean ignore", got)
	}
}

func TestUpdateFromEdge_UnknownRowIsANoOpNotAnInsert(t *testing.T) {
	// These are revalidation writers. A row we have never seen is
	// hydrated through the normal Upsert path, which knows every
	// column; inserting a half-populated row here would create a user
	// with no avatar and a channel with is_member=false.
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")

	if err := db.UpdateChannelFromEdge(EdgeChannelUpdate{ID: "CNOPE", Name: "x"}); err != nil {
		t.Fatalf("UpdateChannelFromEdge on a missing row: %v", err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{ID: "UNOPE", Name: "x"}); err != nil {
		t.Fatalf("UpdateUserFromEdge on a missing row: %v", err)
	}
	if _, err := db.GetChannel("CNOPE"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetChannel(CNOPE) err = %v; want sql.ErrNoRows — UpdateChannelFromEdge inserted a row, it must only update", err)
	}
	if _, err := db.GetUser("UNOPE"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetUser(UNOPE) err = %v; want sql.ErrNoRows — UpdateUserFromEdge inserted a row, it must only update", err)
	}
}

// A revalidation pass calls these once per changed id. An UPDATE that
// lost its WHERE clause would rewrite every channel in the cache with
// one channel's name, type, topic and version — and because the values
// written are all valid, nothing downstream would notice.
func TestUpdateChannelFromEdge_TouchesOnlyTheTargetRow(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannelFull(t, db, "T1", "C1")
	seedChannelFull(t, db, "T1", "C2")
	if err := db.SetChannelVersion("C2", 42); err != nil {
		t.Fatalf("SetChannelVersion: %v", err)
	}

	if err := db.UpdateChannelFromEdge(EdgeChannelUpdate{
		ID: "C1", Name: "new-name", Type: "private", Topic: "new topic",
		Version: sampleChannelVersion,
	}); err != nil {
		t.Fatalf("UpdateChannelFromEdge: %v", err)
	}

	other := getChannelRow(t, db, "C2")
	if other.Name != "original-name" || other.Type != "channel" || other.Topic != "original topic" {
		t.Errorf("C2 was rewritten by an update aimed at C1: %+v", other)
	}
	vers, err := db.ChannelVersions("T1")
	if err != nil {
		t.Fatalf("ChannelVersions: %v", err)
	}
	if vers["C2"] != 42 {
		t.Errorf("C2 version = %d; want 42 — an update aimed at C1 restamped it", vers["C2"])
	}
}

func TestUpdateUserFromEdge_TouchesOnlyTheTargetRow(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	for _, id := range []string{"U1", "U2"} {
		if err := db.UpsertUser(User{
			ID: id, WorkspaceID: "T1", Name: "orig-" + id, DisplayName: "Orig",
			AvatarURL: "https://example.invalid/" + id + ".png", Presence: "active",
		}); err != nil {
			t.Fatalf("UpsertUser(%s): %v", id, err)
		}
	}
	if err := db.SetUserVersion("U2", 42); err != nil {
		t.Fatalf("SetUserVersion: %v", err)
	}

	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "new", DisplayName: "New", Version: sampleUserVersion,
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge: %v", err)
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{
		ID: "U1", Name: "new", DisplayName: "New",
		AvatarURL: "https://example.invalid/new.png", Version: sampleUserVersion,
	}); err != nil {
		t.Fatalf("UpdateUserFromEdge (avatar branch): %v", err)
	}

	other := getUserRow(t, db, "U2")
	if other.Name != "orig-U2" || other.AvatarURL != "https://example.invalid/U2.png" {
		t.Errorf("U2 was rewritten by an update aimed at U1: %+v", other)
	}
	vers, err := db.UserVersions("T1")
	if err != nil {
		t.Fatalf("UserVersions: %v", err)
	}
	if vers["U2"] != 42 {
		t.Errorf("U2 version = %d; want 42 — an update aimed at U1 restamped it", vers["U2"])
	}
}

// An id is only meaningful inside its workspace. Grid users have many,
// and dropping the workspace scope makes one workspace's membership
// snapshot rewrite another's.
func TestApplyMembership_ScopedToWorkspace(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	seedWorkspace(t, db, "T2")
	seedChannelFull(t, db, "T1", "C1")
	seedChannelFull(t, db, "T2", "C2")

	// C2 belongs to T2. Naming it in a T1 batch must not clear it.
	if err := db.ApplyMembership("T1", []string{"C1", "C2"}, MembershipReported(nil, nil)); err != nil {
		t.Fatalf("ApplyMembership: %v", err)
	}

	if getChannelRow(t, db, "C1").IsMember {
		t.Error("C1 is in T1, was queried and absent from member_channels; it must be cleared")
	}
	if !getChannelRow(t, db, "C2").IsMember {
		t.Error("C2 is in T2; a T1 membership snapshot must not reach across workspaces")
	}
}

// An ABSENT member_channels is not an answer. It is absent from 13 of
// 18 observed channels/info responses, every one of which sent
// check_membership:true — including one that asked about 11 ids and one
// that asked about 20. Reading absence as "none of these are joined"
// would mark the user a non-member of all 20 on an ordinary channel
// switch, silently.
func TestApplyMembership_AbsentMemberChannelsPreservesEveryQueriedID(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	// Both prior states are present so that "preserved" cannot be
	// confused with "set all true" or "set all false".
	seedChannelMembership(t, db, "T1", "C1", true)
	seedChannelMembership(t, db, "T1", "C2", false)
	seedChannelMembership(t, db, "T1", "C3", true)

	if err := db.ApplyMembership("T1", []string{"C1", "C2"}, MembershipUnreported()); err != nil {
		t.Fatalf("ApplyMembership: %v", err)
	}

	assertMembership(t, db, "C1", true,
		"member_channels was absent, so the response carries no membership information and every queried id keeps what it had")
	assertMembership(t, db, "C2", false,
		"member_channels was absent; preserving must not mean setting every queried id to true either")
	assertMembership(t, db, "C3", true,
		"C3 was never queried and must be untouched")
}

// An EXPLICITLY empty member_channels — the server reported, and named
// nobody — is a real answer: none of the ids asked about are joined.
// Collapsing it into the absent case leaves every channel the user left
// still marked as joined.
func TestApplyMembership_ExplicitlyEmptyMemberChannelsClearsEveryQueriedID(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannelFull(t, db, "T1", "C1")
	seedChannelFull(t, db, "T1", "C2")
	seedChannelFull(t, db, "T1", "C3")

	if err := db.ApplyMembership("T1", []string{"C1", "C2"}, MembershipReported(nil, nil)); err != nil {
		t.Fatalf("ApplyMembership: %v", err)
	}

	assertMembership(t, db, "C1", false,
		"member_channels was reported and empty, so C1 is a genuine non-membership and must be cleared")
	assertMembership(t, db, "C2", false,
		"member_channels was reported and empty, so C2 is a genuine non-membership and must be cleared")
	assertMembership(t, db, "C3", true,
		"C3 was never queried and must be untouched")
}

// A failed id is not an answer about membership: the server could not
// resolve the id at all. In 4 of the 5 observed responses that carried
// member_channels, member + failed exactly accounted for every id sent
// (52+11=63, 29+2=31, 20+0=20, 1+0=1), so folding failures into the
// clear set would clear the majority of a batch on the strength of a
// lookup error.
func TestApplyMembership_FailedIDsAreNeverNonMembers(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannelMembership(t, db, "T1", "C1", false) // reported as a member
	seedChannelMembership(t, db, "T1", "C2", true)  // failed, was a member
	seedChannelMembership(t, db, "T1", "C3", false) // failed, was not
	seedChannelMembership(t, db, "T1", "C4", true)  // reported on, named by neither
	seedChannelMembership(t, db, "T1", "C5", true)  // never queried

	if err := db.ApplyMembership("T1",
		[]string{"C1", "C2", "C3", "C4"},
		MembershipReported([]string{"C1"}, []string{"C2", "C3"}),
	); err != nil {
		t.Fatalf("ApplyMembership: %v", err)
	}

	assertMembership(t, db, "C1", true,
		"C1 was named in member_channels")
	assertMembership(t, db, "C2", true,
		"C2 is in failed_ids; the server never resolved it, so its membership is unknown and must be preserved, not cleared")
	assertMembership(t, db, "C3", false,
		"C3 is in failed_ids; unknown must be preserved, so a failure must not set is_member either")
	assertMembership(t, db, "C4", false,
		"C4 was queried, the server reported, and named it in neither member_channels nor failed_ids — that is a genuine non-membership")
	assertMembership(t, db, "C5", true,
		"C5 was never queried and must be untouched")
}

// These writers are called in a loop over a revalidation batch. A
// swallowed error turns a dead database into a silently stale cache
// that no caller can detect.
func TestEdgeWriters_PropagateDatabaseErrors(t *testing.T) {
	db := openEdgeSyncTestDB(t)
	seedWorkspace(t, db, "T1")
	seedChannelFull(t, db, "T1", "C1")
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := db.UpdateChannelFromEdge(EdgeChannelUpdate{ID: "C1", Name: "x"}); err == nil {
		t.Error("UpdateChannelFromEdge on a closed database returned nil")
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{ID: "U1", Name: "x"}); err == nil {
		t.Error("UpdateUserFromEdge on a closed database returned nil")
	}
	if err := db.UpdateUserFromEdge(EdgeUserUpdate{ID: "U1", Name: "x", AvatarURL: "y"}); err == nil {
		t.Error("UpdateUserFromEdge (avatar branch) on a closed database returned nil")
	}
	// A reported snapshot: an unreported one is a no-op by contract and
	// would return nil here for the right reason, testing nothing.
	if err := db.ApplyMembership("T1", []string{"C1"}, MembershipReported(nil, nil)); err == nil {
		t.Error("ApplyMembership on a closed database returned nil")
	}
}
