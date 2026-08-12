package cache

import "fmt"

// EdgeChannelUpdate is what edgeapi's channels/info can tell us about a
// channel — and nothing more.
//
// Deliberately not cache.Channel. The full struct has fields no edge
// response carries (IsMember, IsStarred), and a caller filling those
// with zero values and handing them to UpsertChannel is exactly the
// silent data loss this type exists to make impossible. If a field is
// absent here, the column it maps to is preserved.
type EdgeChannelUpdate struct {
	ID      string
	Name    string
	Type    string
	Topic   string
	Version int64
}

// UpdateChannelFromEdge applies a revalidation result, touching only
// the columns channels/info actually populates.
//
// is_member is NOT among them: 0 of 36 observed channels/info results
// carried it, because membership comes back as the response's
// top-level member_channels array instead. Use ApplyMembership for
// that. is_starred, last_read_ts, unread_count and has_unread come
// from other sources entirely and are likewise preserved.
//
// A row that does not exist is left alone rather than inserted: this
// is a revalidation writer, and an unknown channel must go through
// UpsertChannel, which knows every column.
func (db *DB) UpdateChannelFromEdge(u EdgeChannelUpdate) error {
	_, err := db.conn.Exec(`
		UPDATE channels
		SET name = ?, type = ?, topic = ?, version = ?
		WHERE id = ?`,
		u.Name, u.Type, u.Topic, u.Version, u.ID)
	if err != nil {
		return fmt.Errorf("updating channel %s from edge: %w", u.ID, err)
	}
	return nil
}

// MembershipSnapshot is one channels/info response's answer about
// membership — including the answer "none given".
//
// The fields are unexported and reachable only through
// MembershipReported and MembershipUnreported because the whole point
// of the type is a distinction a []string cannot carry: whether the
// server said anything at all. member_channels is ABSENT from 13 of 18
// observed channels/info responses, every one of which sent
// check_membership:true — one of them having asked about 11 ids, another
// about 20. Reading that absence as "none of these are members" marks
// the user a non-member of all 20 on an ordinary channel switch,
// silently. encoding/json cannot tell the two apart either: an absent
// key and a literal [] both decode to a nil slice.
//
// A constructor call rather than a struct literal with a bool field
// means a caller cannot forget the distinction — there is no field to
// leave unset — and cannot forget failed_ids, which is a positional
// argument of the only constructor that clears anything. The zero value
// is the unreported one, so the accident is always toward preserving.
type MembershipSnapshot struct {
	present   bool
	memberIDs []string
	failedIDs []string
}

// MembershipReported is the snapshot for a response that carried
// member_channels. Membership is then authoritative for the ids the
// request sent: one named here is a member, and one named neither here
// nor in failedIDs is a genuine non-member.
//
// failedIDs is the response's failed_ids. It is required rather than
// optional because a caller who omits it clears is_member for ids the
// server explicitly could not answer about — in 4 of the 5 observed
// responses carrying member_channels, members plus failures accounted
// for every id sent (52+11=63, 29+2=31, 20+0=20, 1+0=1), so the
// omission is not a rare edge.
//
// Both arguments may be nil. Reported-and-empty is a real answer: the
// server looked and named nobody, so every queried id that did not fail
// is a non-member and is cleared.
func MembershipReported(memberIDs, failedIDs []string) MembershipSnapshot {
	return MembershipSnapshot{present: true, memberIDs: memberIDs, failedIDs: failedIDs}
}

// MembershipUnreported is the snapshot for a response with no
// member_channels key at all: no membership information, so nothing is
// written and every queried id keeps what it had. This is the common
// case, not the exceptional one — 13 of 18 observed responses.
func MembershipUnreported() MembershipSnapshot { return MembershipSnapshot{} }

// ApplyMembership records the membership snapshot from a channels/info
// response.
//
// queriedIDs is every id the request sent. What happens to each of them
// depends on what the response actually said:
//
//   - snap unreported: nothing is written at all. See MembershipSnapshot.
//   - id in member_channels: is_member set.
//   - id in failed_ids: preserved. The server could not resolve the id,
//     which is not an answer about membership; clearing it would treat a
//     lookup error as a departure, and setting it would invent one.
//   - id queried, reported on, and in neither list: cleared. This is the
//     only genuine non-membership.
//
// An id that was never queried is untouched in every case —
// member_channels is a snapshot over what was asked, not a
// workspace-wide list, so treating unqueried ids as non-members would
// drop the user out of every channel outside the current batch.
//
// An id in both lists is treated as a member: an affirmative naming
// outranks a failure to resolve.
//
// # Building the snapshot from edge.ChannelsInfoResult
//
// ChannelsInfoResult carries MemberChannels and FailedIDs but does not
// record presence, and cannot as written: encoding/json gives a nil
// slice for both an absent member_channels and a literal []. A caller
// therefore has only one signal available today, and must use it:
//
//	snap := cache.MembershipUnreported()
//	if len(res.MemberChannels) > 0 {
//		snap = cache.MembershipReported(res.MemberChannels, res.FailedIDs)
//	}
//
// Passing MembershipReported unconditionally is the bug this type
// exists to prevent. The heuristic's one blind spot is a response that
// reported an explicitly empty member_channels: that reads as
// unreported, so a channel the user left stays marked joined until some
// later response names members. No observed response is in that state —
// all 5 that carried member_channels named at least one id — and the
// error direction is a stale join flag rather than dropping the user
// out of every channel in the batch.
//
// There is a second hazard the caller cannot work around, and it is why
// edge should expose presence itself. ChannelsInfo accumulates over
// 60-id batches, so one ChannelsInfoResult can span responses that
// disagree: if the first batch reports membership and the second does
// not, the accumulated result looks reported while holding no answer at
// all for the second batch's ids — and applying it against the full
// queried set clears every one of them. With member_channels absent
// from 13 of 18 responses, mixed batches are the expected case, not a
// corner. Until edge records which ids were covered by a reporting
// batch, a caller must keep each ApplyMembership call to a single
// batch's worth of ids.
func (db *DB) ApplyMembership(workspaceID string, queriedIDs []string, snap MembershipSnapshot) error {
	if !snap.present || len(queriedIDs) == 0 {
		return nil
	}
	members := make(map[string]bool, len(snap.memberIDs))
	for _, id := range snap.memberIDs {
		members[id] = true
	}
	failed := make(map[string]bool, len(snap.failedIDs))
	for _, id := range snap.failedIDs {
		failed[id] = true
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("applying membership: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`UPDATE channels SET is_member = ? WHERE id = ? AND workspace_id = ?`)
	if err != nil {
		return fmt.Errorf("applying membership: %w", err)
	}
	defer stmt.Close()

	for _, id := range queriedIDs {
		if !members[id] && failed[id] {
			continue
		}
		if _, err := stmt.Exec(boolToInt(members[id]), id, workspaceID); err != nil {
			return fmt.Errorf("applying membership for %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// EdgeUserUpdate is what edgeapi's users/info and users/search can tell
// us about a user. Same contract as EdgeChannelUpdate: absent field
// means "preserve the column", never "clear it".
type EdgeUserUpdate struct {
	ID          string
	Name        string
	DisplayName string
	// AvatarURL is empty when the source had none — users/info carries
	// image_original on 255 of 291 observed results, and the users
	// without it are the ones with no custom image. Empty therefore
	// means "this response says nothing", so the column is preserved.
	AvatarURL  string
	IsBot      bool
	IsExternal bool
	Version    int64
}

// UpdateUserFromEdge applies a revalidation result, touching only the
// columns an edge response populates. presence is never among them.
//
// avatar_url is written only when non-empty, for the reason on the
// field: blanking a good URL because this particular user has no
// custom image is the failure this whole file exists to prevent.
func (db *DB) UpdateUserFromEdge(u EdgeUserUpdate) error {
	var err error
	if u.AvatarURL != "" {
		_, err = db.conn.Exec(`
			UPDATE users
			SET name = ?, display_name = ?, avatar_url = ?, is_bot = ?, is_external = ?, version = ?
			WHERE id = ?`,
			u.Name, u.DisplayName, u.AvatarURL, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version, u.ID)
	} else {
		_, err = db.conn.Exec(`
			UPDATE users
			SET name = ?, display_name = ?, is_bot = ?, is_external = ?, version = ?
			WHERE id = ?`,
			u.Name, u.DisplayName, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version, u.ID)
	}
	if err != nil {
		return fmt.Errorf("updating user %s from edge: %w", u.ID, err)
	}
	return nil
}

// UpsertUserFromEdge is UpdateUserFromEdge with row-creating
// semantics, for callers whose whole case is that the row does not
// exist yet — the batched user resolver's cache misses. (Bootstrap
// uses UpdateUserFromEdge because hydrateFirstSight has already
// inserted placeholder rows; UPDATE-only there is a feature: it
// cannot invent rows.) workspaceID is taken explicitly because
// EdgeUserUpdate does not carry it.
//
// The preserve-on-empty avatar contract is identical to
// UpdateUserFromEdge, and for the same reason. presence and
// updated_at are never touched: on insert they take the column
// defaults, on conflict they keep what they had.
func (db *DB) UpsertUserFromEdge(workspaceID string, u EdgeUserUpdate) error {
	var err error
	if u.AvatarURL != "" {
		_, err = db.conn.Exec(`
			INSERT INTO users (id, workspace_id, name, display_name, avatar_url, is_bot, is_external, version)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name=excluded.name,
				display_name=excluded.display_name,
				avatar_url=excluded.avatar_url,
				is_bot=excluded.is_bot,
				is_external=excluded.is_external,
				version=excluded.version
		`, u.ID, workspaceID, u.Name, u.DisplayName, u.AvatarURL, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version)
	} else {
		_, err = db.conn.Exec(`
			INSERT INTO users (id, workspace_id, name, display_name, is_bot, is_external, version)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name=excluded.name,
				display_name=excluded.display_name,
				is_bot=excluded.is_bot,
				is_external=excluded.is_external,
				version=excluded.version
		`, u.ID, workspaceID, u.Name, u.DisplayName, boolToInt(u.IsBot), boolToInt(u.IsExternal), u.Version)
	}
	if err != nil {
		return fmt.Errorf("upserting user %s from edge: %w", u.ID, err)
	}
	return nil
}
