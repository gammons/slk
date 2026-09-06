package cache

import (
	"testing"
)

func TestUpsertThreadSubscription_Insert(t *testing.T) {
	db := setupDBWithWorkspace(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "1700000000.000100", "1700000000.000200", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}
	got, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].ChannelID != "C1" || got[0].ThreadTS != "1700000000.000100" ||
		got[0].LastRead != "1700000000.000200" || !got[0].Active {
		t.Fatalf("row mismatch: %+v", got[0])
	}
	if got[0].UpdatedAt == 0 {
		t.Fatalf("UpdatedAt not stamped: %+v", got[0])
	}
}

func TestUpsertThreadSubscription_UpdateBumpsLastRead(t *testing.T) {
	db := setupDBWithWorkspace(t)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000200", true)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000900", true)

	got := mustList(t, db, "T1")
	if len(got) != 1 {
		t.Fatalf("want 1 row after upsert, got %d", len(got))
	}
	if got[0].LastRead != "1700000000.000900" {
		t.Fatalf("LastRead not updated: %s", got[0].LastRead)
	}
}

func TestUpsertThreadSubscription_ToggleActive(t *testing.T) {
	db := setupDBWithWorkspace(t)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000200", true)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000200", false)

	got := mustList(t, db, "T1")
	if len(got) != 0 {
		t.Fatalf("inactive row should be filtered out, got %d", len(got))
	}
}

func TestUpsertThreadSubscription_PreservesLastReadAcrossReactivation(t *testing.T) {
	db := setupDBWithWorkspace(t)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000500", true)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000500", false) // tombstone
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000600", true)  // re-subscribe

	got := mustList(t, db, "T1")
	if len(got) != 1 {
		t.Fatalf("want 1 active row after re-subscribe, got %d", len(got))
	}
	if got[0].LastRead != "1700000000.000600" {
		t.Fatalf("LastRead not updated on reactivation: %s", got[0].LastRead)
	}
}

func TestDeleteThreadSubscription_HardRemoves(t *testing.T) {
	db := setupDBWithWorkspace(t)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000200", true)
	mustUpsert(t, db, "T1", "C1", "1700000000.000300", "1700000000.000400", true)

	if err := db.DeleteThreadSubscription("T1", "C1", "1700000000.000100"); err != nil {
		t.Fatalf("DeleteThreadSubscription: %v", err)
	}

	got := mustList(t, db, "T1")
	if len(got) != 1 {
		t.Fatalf("want 1 row after delete, got %d", len(got))
	}
	if got[0].ThreadTS != "1700000000.000300" {
		t.Fatalf("wrong row survived delete: %+v", got[0])
	}
}

// --- test helpers ---

func mustUpsert(t *testing.T, db *DB, ws, ch, ts, lastRead string, active bool) {
	t.Helper()
	if err := db.UpsertThreadSubscription(ws, ch, ts, lastRead, active); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}
}

func mustList(t *testing.T, db *DB, ws string) []ThreadSubscription {
	t.Helper()
	got, err := db.ListActiveThreadSubscriptions(ws)
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	return got
}

func TestReconcileThreadSubscriptions_InsertsNew(t *testing.T) {
	db := setupDBWithWorkspace(t)
	fresh := []ThreadSubscription{
		{WorkspaceID: "T1", ChannelID: "C1", ThreadTS: "1700000000.000100", LastRead: "1700000000.000200", Active: true},
		{WorkspaceID: "T1", ChannelID: "C2", ThreadTS: "1700000001.000100", LastRead: "1700000001.000200", Active: true},
	}
	if err := db.ReconcileThreadSubscriptions("T1", fresh); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := mustList(t, db, "T1")
	if len(got) != 2 {
		t.Fatalf("want 2 rows, got %d", len(got))
	}
}

func TestReconcileThreadSubscriptions_TombstonesMissing(t *testing.T) {
	db := setupDBWithWorkspace(t)
	// Pre-existing local row that's no longer in the fresh list.
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000500", true)
	// Fresh list contains a different thread only.
	fresh := []ThreadSubscription{
		{WorkspaceID: "T1", ChannelID: "C2", ThreadTS: "1700000001.000100", LastRead: "1700000001.000200", Active: true},
	}
	if err := db.ReconcileThreadSubscriptions("T1", fresh); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := mustList(t, db, "T1")
	if len(got) != 1 {
		t.Fatalf("want 1 active row after reconcile, got %d", len(got))
	}
	if got[0].ChannelID != "C2" {
		t.Fatalf("wrong row survived reconcile: %+v", got[0])
	}
	// The tombstoned row should still exist with active=0 and its LastRead preserved.
	var lastRead string
	var active int
	err := db.conn.QueryRow(
		`SELECT last_read, active FROM thread_subscriptions WHERE workspace_id=? AND channel_id=? AND thread_ts=?`,
		"T1", "C1", "1700000000.000100",
	).Scan(&lastRead, &active)
	if err != nil {
		t.Fatalf("tombstone row missing: %v", err)
	}
	if active != 0 {
		t.Fatalf("expected tombstone (active=0), got active=%d", active)
	}
	if lastRead != "1700000000.000500" {
		t.Fatalf("LastRead not preserved on tombstone: %q", lastRead)
	}
}

func TestReconcileThreadSubscriptions_UpdatesExisting(t *testing.T) {
	db := setupDBWithWorkspace(t)
	mustUpsert(t, db, "T1", "C1", "1700000000.000100", "1700000000.000200", true)
	fresh := []ThreadSubscription{
		{WorkspaceID: "T1", ChannelID: "C1", ThreadTS: "1700000000.000100", LastRead: "1700000000.000900", Active: true},
	}
	if err := db.ReconcileThreadSubscriptions("T1", fresh); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := mustList(t, db, "T1")
	if len(got) != 1 || got[0].LastRead != "1700000000.000900" {
		t.Fatalf("Reconcile didn't update LastRead: %+v", got)
	}
}

func TestReconcileThreadSubscriptions_PerWorkspaceIsolation(t *testing.T) {
	db := setupDBWithWorkspace(t)
	// Seed both workspaces. T2 will be ignored entirely by Reconcile("T1").
	if err := db.UpsertWorkspace(Workspace{ID: "T2", Name: "T2"}); err != nil {
		t.Fatalf("UpsertWorkspace T2: %v", err)
	}
	mustUpsert(t, db, "T2", "C9", "1700000000.000100", "1700000000.000200", true)

	fresh := []ThreadSubscription{
		{WorkspaceID: "T1", ChannelID: "C1", ThreadTS: "1700000000.000100", LastRead: "1700000000.000200", Active: true},
	}
	if err := db.ReconcileThreadSubscriptions("T1", fresh); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := mustList(t, db, "T2"); len(got) != 1 {
		t.Fatalf("T2 should be unaffected, got %d active rows", len(got))
	}
}

func TestUpdateThreadLastRead_PreservesTombstone(t *testing.T) {
	db := setupDBWithWorkspace(t)
	// Tombstone the row: active=false.
	if err := db.UpsertThreadSubscription("T1", "C1", "P1", "R1", false); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}

	if err := db.UpdateThreadLastRead("T1", "C1", "P1", "R9"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("UpdateThreadLastRead must not resurrect a tombstoned row, got %d active", len(active))
	}
	got, err := db.GetThreadLastRead("T1", "C1", "P1")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if got != "R9" {
		t.Errorf("GetThreadLastRead = %q, want %q", got, "R9")
	}
}

func TestUpdateThreadLastRead_PreservesActive(t *testing.T) {
	db := setupDBWithWorkspace(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "P1", "R1", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}

	if err := db.UpdateThreadLastRead("T1", "C1", "P1", "R9"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("want 1 active row, got %d", len(active))
	}
	if active[0].LastRead != "R9" {
		t.Errorf("LastRead = %q, want %q", active[0].LastRead, "R9")
	}
}

func TestUpdateThreadLastRead_InsertsActiveRow(t *testing.T) {
	db := setupDBWithWorkspace(t)
	// No prior row: Slack only sends thread_marked for threads the user
	// is subscribed to, so an inserted row starts active.
	if err := db.UpdateThreadLastRead("T1", "C1", "P1", "R5"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 1 || active[0].LastRead != "R5" {
		t.Fatalf("want one active row with LastRead=R5, got %+v", active)
	}
}

func TestGetThreadLastRead_MissingRowReturnsEmpty(t *testing.T) {
	db := setupDBWithWorkspace(t)
	got, err := db.GetThreadLastRead("T1", "C1", "P1")
	if err != nil {
		t.Fatalf("GetThreadLastRead on missing row: %v", err)
	}
	if got != "" {
		t.Errorf("GetThreadLastRead = %q, want empty", got)
	}
}

func TestUpdateThreadLastRead_RejectsEmptyKey(t *testing.T) {
	db := setupDBWithWorkspace(t)
	if err := db.UpdateThreadLastRead("", "C1", "P1", "R5"); err == nil {
		t.Error("want error for empty workspaceID, got nil")
	}
	if err := db.UpdateThreadLastRead("T1", "", "P1", "R5"); err == nil {
		t.Error("want error for empty channelID, got nil")
	}
	if err := db.UpdateThreadLastRead("T1", "C1", "", "R5"); err == nil {
		t.Error("want error for empty threadTS, got nil")
	}
}

// latest_reply is the authoritative "newest activity" watermark set by
// the getView reconcile. A read cursor update must not clobber it, or
// the thread would look read-with-no-activity until the next reconcile.
func TestUpdateThreadLastRead_PreservesLatestReply(t *testing.T) {
	db := setupDBWithWorkspace(t)
	fresh := []ThreadSubscription{{
		WorkspaceID: "T1", ChannelID: "C1", ThreadTS: "1700000100.000000",
		LastRead: "1700000150.000000", LatestReply: "1700000200.000000", Active: true,
	}}
	if err := db.ReconcileThreadSubscriptions("T1", fresh); err != nil {
		t.Fatalf("ReconcileThreadSubscriptions: %v", err)
	}

	if err := db.UpdateThreadLastRead("T1", "C1", "1700000100.000000", "1700000200.000000"); err != nil {
		t.Fatalf("UpdateThreadLastRead: %v", err)
	}

	var latestReply string
	if err := db.conn.QueryRow(
		`SELECT latest_reply FROM thread_subscriptions WHERE workspace_id=? AND channel_id=? AND thread_ts=?`,
		"T1", "C1", "1700000100.000000",
	).Scan(&latestReply); err != nil {
		t.Fatalf("query latest_reply: %v", err)
	}
	if latestReply != "1700000200.000000" {
		t.Errorf("latest_reply = %q, want it preserved as %q", latestReply, "1700000200.000000")
	}
}

// The local mark path fires for ANY thread the user opens, including
// threads opened from the messages pane that they were never subscribed
// to. Inserting a row there would fabricate active=1 and put a phantom
// entry in the Threads list (ListSubscribedThreads filters active=1)
// until the next getView reconcile tombstoned it.
func TestUpdateThreadLastReadIfExists_MissingRowIsNotCreated(t *testing.T) {
	db := setupDBWithWorkspace(t)

	if err := db.UpdateThreadLastReadIfExists("T1", "C1", "P1", "R5"); err != nil {
		t.Fatalf("a missing row must not be an error: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("must not fabricate a subscription row, got %+v", active)
	}
	got, err := db.GetThreadLastRead("T1", "C1", "P1")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if got != "" {
		t.Errorf("GetThreadLastRead = %q, want empty; no row should exist at all", got)
	}
}

func TestUpdateThreadLastReadIfExists_AdvancesExistingRow(t *testing.T) {
	db := setupDBWithWorkspace(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "P1", "R1", true); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}

	if err := db.UpdateThreadLastReadIfExists("T1", "C1", "P1", "R9"); err != nil {
		t.Fatalf("UpdateThreadLastReadIfExists: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("want 1 active row, got %d", len(active))
	}
	if active[0].LastRead != "R9" {
		t.Errorf("LastRead = %q, want %q", active[0].LastRead, "R9")
	}
}

// `active` is owned solely by thread_subscribed, thread_unsubscribed and
// the getView reconcile. Advancing a cursor must neither resurrect a
// tombstone nor refuse to record the read.
func TestUpdateThreadLastReadIfExists_PreservesTombstone(t *testing.T) {
	db := setupDBWithWorkspace(t)
	if err := db.UpsertThreadSubscription("T1", "C1", "P1", "R1", false); err != nil {
		t.Fatalf("UpsertThreadSubscription: %v", err)
	}

	if err := db.UpdateThreadLastReadIfExists("T1", "C1", "P1", "R9"); err != nil {
		t.Fatalf("UpdateThreadLastReadIfExists: %v", err)
	}

	active, err := db.ListActiveThreadSubscriptions("T1")
	if err != nil {
		t.Fatalf("ListActiveThreadSubscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("must not resurrect a tombstoned row, got %d active", len(active))
	}
	got, err := db.GetThreadLastRead("T1", "C1", "P1")
	if err != nil {
		t.Fatalf("GetThreadLastRead: %v", err)
	}
	if got != "R9" {
		t.Errorf("GetThreadLastRead = %q, want the cursor still advanced to R9", got)
	}
}

func TestUpdateThreadLastReadIfExists_PreservesLatestReply(t *testing.T) {
	db := setupDBWithWorkspace(t)
	fresh := []ThreadSubscription{{
		WorkspaceID: "T1", ChannelID: "C1", ThreadTS: "1700000100.000000",
		LastRead: "1700000150.000000", LatestReply: "1700000200.000000", Active: true,
	}}
	if err := db.ReconcileThreadSubscriptions("T1", fresh); err != nil {
		t.Fatalf("ReconcileThreadSubscriptions: %v", err)
	}

	if err := db.UpdateThreadLastReadIfExists("T1", "C1", "1700000100.000000", "1700000200.000000"); err != nil {
		t.Fatalf("UpdateThreadLastReadIfExists: %v", err)
	}

	var latestReply string
	if err := db.conn.QueryRow(
		`SELECT latest_reply FROM thread_subscriptions WHERE workspace_id=? AND channel_id=? AND thread_ts=?`,
		"T1", "C1", "1700000100.000000",
	).Scan(&latestReply); err != nil {
		t.Fatalf("reading latest_reply: %v", err)
	}
	if latestReply != "1700000200.000000" {
		t.Errorf("latest_reply = %q, want it preserved", latestReply)
	}
}

func TestUpdateThreadLastReadIfExists_RejectsEmptyKey(t *testing.T) {
	db := setupDBWithWorkspace(t)
	if err := db.UpdateThreadLastReadIfExists("", "C1", "P1", "R5"); err == nil {
		t.Error("want error for empty workspaceID, got nil")
	}
	if err := db.UpdateThreadLastReadIfExists("T1", "", "P1", "R5"); err == nil {
		t.Error("want error for empty channelID, got nil")
	}
	if err := db.UpdateThreadLastReadIfExists("T1", "C1", "", "R5"); err == nil {
		t.Error("want error for empty threadTS, got nil")
	}
}
