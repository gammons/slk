package edge

import (
	"context"
	"maps"
	"slices"
)

// Batch sizes for the conditional-revalidation endpoints.
//
// These are not documented limits. They are caps chosen at or just
// under the largest batch the official web client has been observed
// sending, measured across 8 HAR captures of a live NON-Grid
// workspace (Rands, T04T4TH8W). No Grid capture of edgeapi exists;
// behaviour there is inferred from a user's log, not observed:
//
//	channels/info   18 requests, 1–63 ids per request
//	users/info      30 requests, 1–80 ids per request
//
// Read that distribution honestly: neither endpoint shows any
// client-side cap. Batch size there just tracks how many ids happened
// to need checking, so 63 and 80 are demand, not contract — and that
// cuts against us, not for us. A *fixed* batch size is itself a known
// residual divergence: the official client emits ragged,
// demand-driven sizes, while we emit a run of requests each carrying
// exactly batchSize ids followed by one short tail. A cold
// revalidation of a 10k-user workspace is 125 consecutive
// exactly-80-id requests, which is a cleaner machine-detectable
// signature than the ragged shape it is supposed to be imitating.
//
// Deliberately not "fixed" by jittering or randomising the size.
// Nothing in the captures says what a jittered distribution should
// look like, so inventing one is the Phase 1 failure mode again: a
// plausible-but-wrong shape is worse than an honestly-declared
// divergence, the same way a made-up sec-ch-ua is worse than none.
//
// The real fix is Phase 2b — scope revalidation to the ids that
// actually need checking instead of sweeping the whole cache, which
// makes our sizes demand-driven for the same reason the official
// client's are. Until then these constants are a necessary upper
// bound on request size, and the resulting uniformity is a divergence
// that is known and accepted rather than solved.
const (
	channelsInfoBatchSize = 60
	usersInfoBatchSize    = 80
)

// Channel is one entry in a channels/info response.
//
// This deliberately models a subset. A real result carries 43 fields
// (enterprise_id, shared_team_ids, properties{}, channel_agent_status,
// …); decoding ignores the rest, and must keep doing so — Slack adds
// fields to this response without notice.
//
// There is deliberately no IsMember field. An earlier draft asserted
// one; the captures disprove it. Across 8 HAR captures — 18
// channels/info requests, 7 responses carrying results, 36 result
// objects — `is_member` appears in 0 of 36, while the other 43 fields
// appear in 36 of 36. Membership does not travel on the result at all;
// it travels on the top-level member_channels array (see
// ChannelsInfoResult.MemberChannels). A bool field here would decode
// false forever and read as "not a member".
type Channel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Version is the `updated` stamp, the value that goes back out in
	// updated_ids on the next revalidation. Channels stamp it in
	// milliseconds (1783337533019), users in seconds; to us it is an
	// opaque monotonic version, never a timestamp.
	Version     int64  `json:"updated"`
	IsChannel   bool   `json:"is_channel"`
	IsGroup     bool   `json:"is_group"`
	IsIM        bool   `json:"is_im"`
	IsMPIM      bool   `json:"is_mpim"`
	IsPrivate   bool   `json:"is_private"`
	IsArchived  bool   `json:"is_archived"`
	ContextTeam string `json:"context_team_id"`
	Topic       struct {
		Value string `json:"value"`
	} `json:"topic"`
}

// User is one entry in a users/info response.
//
// Also a deliberate subset, and the avatar fields are the part worth
// stating precisely, because an earlier version of this comment got
// them wrong. Measured across all 291 users/info result objects in the
// 8 captures:
//
//	profile.avatar_hash      288/291
//	profile.image_original   255/291   (non-empty in all 255)
//	profile.is_custom_image  255/291
//	profile.image_32           0/291
//	profile.image_72           0/291
//	profile.image_192          0/291
//
// So the sized image_NN variants really are absent, and a field for
// one would decode empty forever and quietly hand callers a blank
// avatar. That much of the original reasoning stands.
//
// The rest of it did not: this comment used to claim there is no image
// URL anywhere in a users/info profile, and modelled none. There is.
// image_original is an absolute URL and it is present on 88% of
// results. users/search carries the same key at the same rate (42/60)
// — the two endpoints AGREE, and an earlier note on UsersSearch
// claiming they disagree was wrong for the same reason.
//
// The claim came from the committed fixture rather than the captures.
// internal/slack/testdata/phase2-api-contracts.json keeps samples[:3];
// two of the three users/info samples were `results: []`, and the one
// remaining sample's results[0] happened to be a user with no custom
// image. One user was generalised into a contract. A per-field claim
// about an array element needs a denominator.
//
// ImageOriginal is now modelled below. It is what cache.User's
// avatar_url column gets filled from; without it a revalidation pass
// has an avatar column and no avatar to put in it.
type User struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int64  `json:"updated"`
	Deleted bool   `json:"deleted"`
	IsBot   bool   `json:"is_bot"`
	TeamID  string `json:"team_id"`
	Profile struct {
		DisplayName string `json:"display_name"`
		RealName    string `json:"real_name"`
		// ImageOriginal is the user's avatar URL, an absolute one.
		// Present and non-empty on 255 of the 291 observed users/info
		// results, and on users/search too — 42 of 60 — so the two
		// endpoints agree and this one field serves both.
		//
		// The 36 without it are users who have never set a custom
		// image, which is why an empty value here means "this user has
		// no custom avatar" and never "this endpoint cannot tell you".
		// That is what makes it safe for a caller to treat empty as
		// "leave the stored avatar alone".
		//
		// The sized variants are genuinely absent — image_32,
		// image_72 and image_192 are each 0 of 291 — so a field for
		// one would decode empty forever and hand callers a blank
		// avatar. Do not add them.
		ImageOriginal string `json:"image_original"`
		// IsCustomImage tracks ImageOriginal exactly in the captures:
		// present on the same 255 of 291.
		IsCustomImage bool `json:"is_custom_image"`
	} `json:"profile"`
}

// ChannelsInfoResult is everything one ChannelsInfo call learned,
// accumulated across all of its batches.
//
// This is a struct rather than three return values because the three
// outputs are independent, accumulate separately, and are easy to
// transpose at a call site if they were positional; a fourth top-level
// key is also plausible, since Slack has already added two beyond
// `results`.
type ChannelsInfoResult struct {
	// Channels holds only the channels whose version moved, plus the
	// ones sent with version 0. Empty here is the normal case, not a
	// sign the call learned nothing: all 5 observed responses that
	// carried membership also had `"results":[]`.
	Channels []Channel
	// MemberChannels holds the ids the authenticated user belongs to.
	// This is what check_membership:true buys, and it is the reason
	// this endpoint can replace a conversations.list walk: membership
	// comes back regardless of whether any channel record changed —
	// all 5 observed responses carrying it had `"results":[]` — so it
	// is learned without ever enumerating.
	//
	// It is a snapshot, not a delta, and it is authoritative for
	// exactly the ids in MembershipQueried and no others. Within that
	// set, an id absent from here is one the user is not a member of.
	// An id outside it — never sent, or sent in a batch whose response
	// stayed silent — says nothing either way, and MemberChannels
	// being non-empty does not change that. Read the two fields
	// together or not at all.
	MemberChannels []string
	// MembershipQueried holds the ids that were sent in batches whose
	// response carried member_channels. Membership is authoritative
	// for exactly these ids and no others.
	//
	// This exists because member_channels is absent from 13 of the 18
	// observed channels/info responses, all of which requested it —
	// so "absent" must mean "no information", not "none are members".
	// Accumulating across batches would otherwise produce a result
	// that looks authoritative while holding no answer for the ids in
	// a batch that stayed silent, and a caller applying it against the
	// full queried set would clear membership for all of them.
	//
	// Empty here therefore means "this call learned nothing about
	// membership", which is the common outcome, not an error. Non-empty
	// alongside an empty MemberChannels is also a real answer — the
	// server looked and named nobody — and is exactly the case a
	// len(MemberChannels) > 0 test gets backwards.
	//
	// Ids appear in batch order; within a batch the order is whatever
	// ranging the id map produced, and carries no meaning.
	MembershipQueried []string
	// FailedIDs holds the ids the server could not resolve. Ignoring
	// this is a correctness hazard rather than a lost nicety: absence
	// from Channels otherwise means "unchanged, still fresh", so a
	// failed id would be marked current and its stale record kept
	// forever, because its version never advances. A caller should
	// retry these or leave their rows explicitly stale.
	FailedIDs []string
}

// channelsInfoResponse is one channels/info batch on the wire.
//
// member_channels and failed_ids are each absent from most responses
// (observed 5/18 and 4/18 respectively). Absence is not an error.
//
// MemberChannels is a *[]string rather than a []string, and that is
// the whole point of this type. encoding/json decodes an absent key
// and a literal [] to the identical nil slice, but they are opposite
// answers: absent means the server said nothing about membership and
// every queried id must keep what it had, while [] means the server
// looked and named nobody, so every queried id is a confirmed
// non-member. A plain []string cannot express that difference, so a
// caller is left inferring presence from len() — which reads an empty
// report as silence and quietly leaves stale join flags behind.
//
// A pointer is the smallest thing that distinguishes them: nil for
// absent, non-nil for present, with the element count carrying the
// answer. `"member_channels":null` also lands on nil, which is
// correct — a null is no more an answer than an absent key.
//
// FailedIDs stays a plain []string deliberately. Nothing is inferred
// from its emptiness: an id is either named as failed or it is not.
type channelsInfoResponse struct {
	Results        []Channel `json:"results"`
	MemberChannels *[]string `json:"member_channels"`
	FailedIDs      []string  `json:"failed_ids"`
}

// ChannelsInfo revalidates channels against the edge cache, scoped to
// teamID: the request path is /cache/<teamID>/channels/info.
//
// teamID is mandatory and per-call because of Enterprise Grid. A Grid
// user's conversations are owned by different teams within the org,
// and the edge cache keys records under the owning team; a request
// scoped to the auth.test team resolves only the conversations that
// team owns and fails the rest (gammons/slk#5: 217 of 217 failed).
// The caller partitions by userBoot's context_team_id; non-Grid
// callers pass the workspace's own team id, which is every
// conversation's context team there.
//
// updatedIDs maps channel id to the version last seen (0 for a channel
// we have never seen). Only the entries whose version moved, plus the
// unknown ones, come back in Channels, so a fully current cache costs
// one small response per batch instead of a full conversations.list
// walk. An empty or nil map makes no request at all.
//
// check_membership:true does not add a field to those results. It adds
// the top-level member_channels array, returned for every id in the
// batch whether or not that channel changed — see
// ChannelsInfoResult.MemberChannels.
func (c *Client) ChannelsInfo(ctx context.Context, teamID string, updatedIDs map[string]int64) (ChannelsInfoResult, error) {
	var out ChannelsInfoResult
	err := fetchInfo(ctx, c, teamID, "channels/info", map[string]any{
		"check_membership": true,
	}, updatedIDs, channelsInfoBatchSize, func(batch channelsInfoResponse, queried []string) {
		out.Channels = append(out.Channels, batch.Results...)
		// Accumulated above the membership guard, deliberately.
		// failed_ids and member_channels are independent keys — 4 of
		// 18 and 5 of 18 observed responses — so a batch can report
		// failures and no membership, and stranding these behind an
		// early return loses them silently. See
		// TestChannelsInfo_SurfacesFailedIDsWithoutMemberChannels.
		out.FailedIDs = append(out.FailedIDs, batch.FailedIDs...)
		// Guarded on presence, not on content. A batch that reported
		// an empty member_channels still answered about its ids, and
		// a batch that reported nothing must not have its ids claimed
		// — see ChannelsInfoResult.MembershipQueried.
		if batch.MemberChannels == nil {
			return
		}
		out.MemberChannels = append(out.MemberChannels, *batch.MemberChannels...)
		out.MembershipQueried = append(out.MembershipQueried, queried...)
	})
	if err != nil {
		return ChannelsInfoResult{}, err
	}
	return out, nil
}

// usersInfoResponse is one users/info batch on the wire.
//
// There is no membership analogue here. Across 30 observed users/info
// responses the only top-level keys are results (30/30), ok (30/30)
// and can_interact (12/30) — no member_channels, no failed_ids.
type usersInfoResponse struct {
	Results []User `json:"results"`
}

// UsersInfo revalidates users against the edge cache, with the same
// conditional semantics as ChannelsInfo.
//
// The response also carries a top-level can_interact object — a
// map[string]bool keyed by user id, produced by check_interaction:true
// — which this package deliberately does not model. Nothing in the
// client consumes it; it is per-batch, so exposing it would mean
// merging maps across batches and widening this signature for data no
// caller wants. Adding it later is a two-line change plus a merge.
func (c *Client) UsersInfo(ctx context.Context, updatedIDs map[string]int64) ([]User, error) {
	var out []User
	err := fetchInfo(ctx, c, c.teamID, "users/info", map[string]any{
		"check_interaction":          true,
		"include_profile_only_users": true,
	}, updatedIDs, usersInfoBatchSize, func(batch usersInfoResponse, _ []string) {
		// The queried id set is discarded. users/info has no
		// membership analogue, and nothing else on that response is
		// scoped to the ids it was asked about, so there is nothing
		// here to attribute — the parameter exists for channels/info's
		// sake and rides along on the shared helper.
		out = append(out, batch.Results...)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// fetchInfo posts updatedIDs to a cache endpoint in batches of at most
// batchSize, decoding each batch into Resp and handing it to merge.
//
// It is generic over the whole per-batch response rather than over the
// result element type, which is the change the captures forced: the
// two endpoints no longer share a response shape. channels/info
// carries member_channels and failed_ids alongside results;
// users/info carries neither and carries can_interact instead. A
// helper parameterised on the element type could only have grown a
// second return value that users/info always left empty, or the
// `endpoint == "channels/info"` string comparison that was rejected
// outright.
//
// Splitting this into two functions was the alternative and was
// rejected: what the endpoints still share is exactly the part that is
// easy to get subtly wrong and expensive to get wrong twice — the
// trailing-partial-batch guard, never sending an empty batch, a fresh
// batch map per flush, and abort-on-first-error. What differs is
// decoding and accumulation, which is now entirely in the caller's
// merge closure where it is plain to read. So the generic earns its
// place, just on a different axis than before.
//
// merge is called once per successful batch, in request order, with
// the decoded response and the ids that batch sent, and never after an
// error.
//
// That second argument is what lets a caller attribute a per-batch
// answer to the right ids. channels/info needs it: member_channels is
// scoped to the batch that asked, and is absent from 13 of 18 observed
// responses, so a caller accumulating across batches must be able to
// record which ids a reporting batch actually covered. Passing it to
// every merge keeps that out of a per-endpoint branch — the
// `endpoint == "channels/info"` comparison this design already
// rejected once — and users/info simply ignores it.
//
// The slice is freshly allocated per batch and handed over: fetchInfo
// keeps no reference and never mutates it, so merge may retain it. Its
// order is whatever ranging the id map produced and means nothing;
// only the set does.
//
// A failed batch fails the whole call and the
// caller discards what already merged: returning partial results with
// a nil error would be indistinguishable from "only these entries
// changed", so a caller would mark the unfetched ids current and never
// revalidate them again.
//
// The "never after an error" half is unobservable through ChannelsInfo
// and UsersInfo — both return the zero value on any error, so calling
// merge with a half-decoded response would look identical from
// outside. It is not harmless inside here: call's final
// json.Unmarshal can populate part of resp before returning an error,
// so a merge on a failed batch would splice fragments of a broken
// response into the accumulator. That is why it is pinned directly,
// against fetchInfo rather than through the exported methods — see
// TestFetchInfo_DoesNotMergeAnErroredBatch.
func fetchInfo[Resp any](
	ctx context.Context,
	c *Client,
	teamID string,
	endpoint string,
	flags map[string]any,
	updatedIDs map[string]int64,
	batchSize int,
	merge func(Resp, []string),
) error {
	batch := make(map[string]int64, min(batchSize, len(updatedIDs)))

	flush := func() error {
		payload := make(map[string]any, len(flags)+1)
		maps.Copy(payload, flags)
		payload["updated_ids"] = batch

		// A fresh slice of the batch's keys, taken before the request
		// goes out, for two reasons.
		//
		// Handing over batch itself would tie merge to a map whose
		// lifetime is tangled with the request body's — payload holds
		// the same map, and batch is replaced below.
		//
		// And slices.Collect allocates per batch rather than filling
		// one reused buffer, which matters more than it looks:
		// reusing a buffer is invisible today because both merge
		// implementations copy on receipt, and would silently corrupt
		// any future one that retains what it is handed.
		queried := slices.Collect(maps.Keys(batch))

		var resp Resp
		if err := c.call(ctx, teamID, endpoint, payload, &resp); err != nil {
			return err
		}
		merge(resp, queried)
		// A fresh map rather than clear(): payload still references
		// this one. clear() happens to be safe today only because
		// call marshals the body synchronously before returning — no
		// test can tell the two apart, which is the point. Allocating
		// removes the dependency on that timing.
		batch = make(map[string]int64, min(batchSize, len(updatedIDs)))
		return nil
	}

	for id, version := range updatedIDs {
		batch[id] = version
		if len(batch) == batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	// The trailing partial batch. This guard is the only thing
	// implementing two behaviours, so do not "simplify" it away: an id
	// count that is an exact multiple of batchSize must not send a
	// trailing empty batch, and an empty or nil updatedIDs must send
	// nothing at all. An updated_ids-less revalidation request is a
	// round trip that can only return nothing, and a stream of them is
	// a shape the official client never produces.
	//
	// (An early `if len(updatedIDs) == 0 { return }` at the top of this
	// function was removed: ranging a nil/empty map already falls
	// through to here, so it was unreachable-in-effect — no mutation of
	// it could fail a test.)
	if len(batch) > 0 {
		if err := flush(); err != nil {
			return err
		}
	}

	return nil
}
