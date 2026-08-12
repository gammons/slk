package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/slack/boot"
	"github.com/gammons/slk/internal/slack/edge"
)

// --- fixtures ---------------------------------------------------------
//
// Every value below is distinct and non-zero, which is load-bearing
// rather than tidiness: Phase 2a lost 9 mutants to a fixture whose
// booleans were all false, and another to two string fields that
// happened to share a value. The specific hazards here are named on
// each fixture.

// withTopic sets edge.Channel's anonymous Topic struct, which has no
// spellable type at a call site.
func withTopic(c edge.Channel, topic string) edge.Channel {
	c.Topic.Value = topic
	return c
}

// edgeUser builds a users/info result. Same reason as withTopic:
// edge.User.Profile is an anonymous struct.
func edgeUser(id, name, display, real, avatar, teamID string, version int64, isBot bool) edge.User {
	u := edge.User{ID: id, Name: name, Version: version, IsBot: isBot, TeamID: teamID}
	u.Profile.DisplayName = display
	u.Profile.RealName = real
	u.Profile.ImageOriginal = avatar
	return u
}

// cannedChannelVersions is what the cache holds for channels.
//
// Three things are deliberate:
//
//   - C_PRIVATE and D_ALICE are MISSING even though both are in the
//     sidebar. They must still be sent, with version 0 — that is how
//     the protocol asks for a full record, and the captures show the
//     client doing it ({"C6M7U8DFF":0}). Omitting them would leave
//     those rows permanently unhydrated.
//   - C_CACHE_ONLY_* are rows the cache holds that userBoot never
//     mentioned. Sending them is the "sweep the whole cache"
//     regression this task exists to prevent.
//   - U_ALICE is a USER id sitting in the CHANNEL version map, and
//     C_GENERAL appears in cannedUserVersions the same way. Neither is
//     realistic; both exist so that looking channels up in the user map
//     (or the reverse) produces a WRONG version rather than the same
//     answer by luck. Without them that mutation is invisible, because
//     both maps would miss and both would send 0.
func cannedChannelVersions() map[string]int64 {
	return map[string]int64{
		"C_GENERAL":      1783337533019,
		"D_BOB":          1783337533022,
		"C_CACHE_ONLY_1": 1783337000001,
		"C_CACHE_ONLY_2": 1783337000002,
		"C_CACHE_ONLY_3": 1783337000003,
		"U_ALICE":        4444444444,
	}
}

// cannedUserVersions is what the cache holds for users. Same three
// properties as cannedChannelVersions, mirrored.
func cannedUserVersions() map[string]int64 {
	return map[string]int64{
		"U_ALICE":        1783337533030,
		"U_AUTHOR_ONLY":  1783337533034,
		"U_CACHE_ONLY_1": 1783337100001,
		"U_CACHE_ONLY_2": 1783337100002,
		"U_CACHE_ONLY_3": 1783337100003,
		"C_GENERAL":      5555555555,
	}
}

// wantChannelsInfoSent is the map a correct implementation posts to
// channels/info: exactly userBoot's channels + ims, each paired with
// the cached version or 0.
func wantChannelsInfoSent() map[string]int64 {
	return map[string]int64{
		"C_GENERAL": 1783337533019,
		"C_PRIVATE": 0,
		"D_ALICE":   0,
		"D_BOB":     1783337533022,
	}
}

// wantUsersInfoSent is the map a correct implementation posts to
// users/info once a channel has been opened: the view's authors plus
// the open-DM counterparties, and nothing else.
func wantUsersInfoSent() map[string]int64 {
	return map[string]int64{
		"U_ALICE":       1783337533030,
		"U_BOB":         0,
		"U_AUTHOR_ONLY": 1783337533034,
	}
}

// cannedChannelsInfo is the channels/info response.
//
// C_GENERAL and D_ALICE changed; C_PRIVATE did not (the normal case —
// all 5 observed responses carrying membership had "results":[]);
// D_BOB failed. The two returned channels differ in EVERY mapped
// field, including the type flags, so a Name→Topic transposition or a
// dropped type derivation is visible.
func cannedChannelsInfo() edge.ChannelsInfoResult {
	return edge.ChannelsInfoResult{
		Channels: []edge.Channel{
			withTopic(edge.Channel{
				ID: "C_GENERAL", Name: "general-renamed", Version: 1783337599001, IsChannel: true,
			}, "general topic renamed"),
			withTopic(edge.Channel{
				ID: "D_ALICE", Name: "d-alice-renamed", Version: 1783337599002, IsIM: true,
			}, "dm topic renamed"),
		},
		// A strict subset of MembershipQueried, and a different one
		// from it: passing MemberChannels as the queried set is the
		// silent-membership-loss bug, and it is only visible when the
		// two differ.
		MemberChannels:    []string{"C_GENERAL"},
		MembershipQueried: []string{"C_GENERAL", "C_PRIVATE", "D_ALICE"},
		// FailedIDs is deliberately EMPTY here, and the three tests
		// that need failures opt in with failedChannelIDs.
		//
		// Found by mutation testing. Failed ids are logged, so putting
		// them in the default fixture makes every run log something —
		// which silently defused every "and it was logged" assertion
		// in this package, mine and Tasks 4 and 5's alike. A silenced
		// users/info failure survived because a failed-id line from
		// the OTHER pass was already in the log.
	}
}

// failedChannelIDs is opted into by the tests about failed ids. D_BOB is
// in the sidebar set, so "mark the failures current" has something to
// mark.
var failedChannelIDs = []string{"D_BOB"}

// poisonedChannelsInfo is what the fake returns ALONGSIDE an error.
//
// A zero value there would make "use the result even though the call
// failed" an equivalent mutant. Slack answers ok:false with a fully
// populated body, so this is the realistic shape too.
func poisonedChannelsInfo() edge.ChannelsInfoResult {
	return edge.ChannelsInfoResult{
		Channels: []edge.Channel{
			withTopic(edge.Channel{ID: "C_POISON", Name: "poison", Version: 9999999999999}, "poison topic"),
		},
		MemberChannels:    []string{"C_POISON"},
		MembershipQueried: []string{"C_POISON"},
		FailedIDs:         []string{"C_POISON_FAILED"},
	}
}

// cannedUsersInfo is the users/info response.
//
// The two results disagree on every boolean and exercise both halves
// of the two conditionals in the mapping: U_ALICE has a display name,
// an avatar, is a bot and is local; U_AUTHOR_ONLY has neither display
// name nor avatar, is not a bot, and is external.
func cannedUsersInfo() []edge.User {
	return []edge.User{
		edgeUser("U_ALICE", "alice-renamed", "alice-display-renamed", "Alice Real Renamed",
			"https://example.invalid/alice-new.png", "T_HOME", 1783337599010, true),
		edgeUser("U_AUTHOR_ONLY", "author-only-renamed", "", "Author Only Real Renamed",
			"", "T_OTHER", 1783337599011, false),
	}
}

// poisonedUsersInfo is what the fake returns alongside a users/info
// error, for the reason poisonedChannelsInfo gives.
func poisonedUsersInfo() []edge.User {
	return []edge.User{
		edgeUser("U_POISON", "poison", "poison-display", "Poison Real",
			"https://example.invalid/poison.png", "T_POISON", 9999999999, true),
	}
}

// wantChannelUpdates is what a correct implementation writes through
// the PARTIAL channel writer.
func wantChannelUpdates() []cache.EdgeChannelUpdate {
	return []cache.EdgeChannelUpdate{
		{ID: "C_GENERAL", Name: "general-renamed", Type: "channel", Topic: "general topic renamed", Version: 1783337599001},
		{ID: "D_ALICE", Name: "d-alice-renamed", Type: "dm", Topic: "dm topic renamed", Version: 1783337599002},
	}
}

// wantUserUpdates is what a correct implementation writes through the
// PARTIAL user writer.
func wantUserUpdates() []cache.EdgeUserUpdate {
	return []cache.EdgeUserUpdate{
		{
			ID: "U_ALICE", Name: "alice-renamed", DisplayName: "alice-display-renamed",
			AvatarURL: "https://example.invalid/alice-new.png", IsBot: true, IsExternal: false,
			Version: 1783337599010,
		},
		{
			// display_name is empty on the wire, so the real name
			// stands in; avatar is empty, which UpdateUserFromEdge
			// reads as "preserve what is stored"; team_id differs from
			// the workspace, so external.
			ID: "U_AUTHOR_ONLY", Name: "author-only-renamed", DisplayName: "Author Only Real Renamed",
			AvatarURL: "", IsBot: false, IsExternal: true,
			Version: 1783337599011,
		},
	}
}

// --- fake -------------------------------------------------------------

// membershipCall records one ApplyMembership invocation. The snapshot
// is compared with reflect.DeepEqual against cache.MembershipReported
// / MembershipUnreported, since its fields are unexported by design.
type membershipCall struct {
	workspaceID string
	queriedIDs  []string
	snap        cache.MembershipSnapshot
}

// channelsInfoCall is one team-scoped channels/info request.
type channelsInfoCall struct {
	team string
	sent map[string]int64
}

// The revalidation half of fakeDeps. Declared here rather than in
// bootstrap_test.go so the recording sits next to the assertions.
type revalidateFake struct {
	channelVersions      map[string]int64
	channelVersionsErr   error
	channelVersionsFor   []string
	channelVersionsCalls int

	userVersions      map[string]int64
	userVersionsErr   error
	userVersionsFor   []string
	userVersionsCalls int

	channelsInfoRes    edge.ChannelsInfoResult
	channelsInfoErr    error
	channelsInfoSent   map[string]int64
	channelsInfoCalls  []channelsInfoCall
	channelsInfoErrFor map[string]error // per-team error injection

	usersInfoRes  []edge.User
	usersInfoErr  error
	usersInfoSent map[string]int64

	channelUpdates   []cache.EdgeChannelUpdate
	channelUpdateErr error
	userUpdates      []cache.EdgeUserUpdate
	userUpdateErr    error

	membershipCalls []membershipCall
	membershipErr   error
}

// poisonedVersionMap is returned beside a version-lookup error. Using
// it would send the server versions slk cannot vouch for, and the
// server would then withhold records slk does not have.
func poisonedVersionMap() map[string]int64 {
	return map[string]int64{"C_GENERAL": 9999999999999, "U_ALICE": 9999999999999}
}

func (f *fakeDeps) ChannelVersions(workspaceID string) (map[string]int64, error) {
	f.mu.Lock()
	f.channelVersionsCalls++
	f.channelVersionsFor = append(f.channelVersionsFor, workspaceID)
	f.mu.Unlock()
	if f.channelVersionsErr != nil {
		return poisonedVersionMap(), f.channelVersionsErr
	}
	return f.channelVersions, nil
}

func (f *fakeDeps) UserVersions(workspaceID string) (map[string]int64, error) {
	f.mu.Lock()
	f.userVersionsCalls++
	f.userVersionsFor = append(f.userVersionsFor, workspaceID)
	f.mu.Unlock()
	if f.userVersionsErr != nil {
		return poisonedVersionMap(), f.userVersionsErr
	}
	return f.userVersions, nil
}

func (f *fakeDeps) ChannelsInfo(_ context.Context, teamID string, updatedIDs map[string]int64) (edge.ChannelsInfoResult, error) {
	f.record(callChannelsInfo)
	f.mu.Lock()
	sent := make(map[string]int64, len(updatedIDs))
	for k, v := range updatedIDs {
		sent[k] = v
	}
	f.channelsInfoCalls = append(f.channelsInfoCalls, channelsInfoCall{team: teamID, sent: sent})
	if f.channelsInfoSent == nil {
		f.channelsInfoSent = map[string]int64{}
	}
	for k, v := range sent {
		f.channelsInfoSent[k] = v
	}
	err := f.channelsInfoErr
	if e, ok := f.channelsInfoErrFor[teamID]; ok {
		err = e
	}
	f.mu.Unlock()
	if err != nil {
		return poisonedChannelsInfo(), err
	}
	return filterChannelsInfo(f.channelsInfoRes, sent), nil
}

// filterChannelsInfo intersects a canned result with the ids one
// request actually asked about, the way the server scopes its answer
// to the request. A canned membership report that names no asked id
// decays to "this batch said nothing" (empty MembershipQueried), which
// is what the real absent-member_channels case already models.
func filterChannelsInfo(res edge.ChannelsInfoResult, sent map[string]int64) edge.ChannelsInfoResult {
	var out edge.ChannelsInfoResult
	for _, ch := range res.Channels {
		if _, ok := sent[ch.ID]; ok {
			out.Channels = append(out.Channels, ch)
		}
	}
	keep := func(ids []string) []string {
		var kept []string
		for _, id := range ids {
			if _, ok := sent[id]; ok {
				kept = append(kept, id)
			}
		}
		return kept
	}
	out.MemberChannels = keep(res.MemberChannels)
	out.MembershipQueried = keep(res.MembershipQueried)
	out.FailedIDs = keep(res.FailedIDs)
	return out
}

func (f *fakeDeps) UsersInfo(_ context.Context, updatedIDs map[string]int64) ([]edge.User, error) {
	f.record(callUsersInfo)
	f.mu.Lock()
	f.usersInfoSent = make(map[string]int64, len(updatedIDs))
	for k, v := range updatedIDs {
		f.usersInfoSent[k] = v
	}
	f.mu.Unlock()
	if f.usersInfoErr != nil {
		return poisonedUsersInfo(), f.usersInfoErr
	}
	return f.usersInfoRes, nil
}

func (f *fakeDeps) UpdateChannelFromEdge(u cache.EdgeChannelUpdate) error {
	f.mu.Lock()
	f.channelUpdates = append(f.channelUpdates, u)
	f.mu.Unlock()
	return f.channelUpdateErr
}

func (f *fakeDeps) UpdateUserFromEdge(u cache.EdgeUserUpdate) error {
	f.mu.Lock()
	f.userUpdates = append(f.userUpdates, u)
	f.mu.Unlock()
	return f.userUpdateErr
}

func (f *fakeDeps) ApplyMembership(workspaceID string, queriedIDs []string, snap cache.MembershipSnapshot) error {
	f.mu.Lock()
	f.membershipCalls = append(f.membershipCalls, membershipCall{
		workspaceID: workspaceID,
		queriedIDs:  append([]string(nil), queriedIDs...),
		snap:        snap,
	})
	f.mu.Unlock()
	return f.membershipErr
}

// openedFake is the common setup: a boot with a channel open, so that
// conversations.view's authors are in scope for the user pass.
func openedFake() *fakeDeps {
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_WANT"
	return f
}

// channelUpdateIDs lists the ids a run wrote through the partial
// channel writer, for failure messages and membership-free assertions.
func channelUpdateIDs(updates []cache.EdgeChannelUpdate) []string {
	out := make([]string, 0, len(updates))
	for _, u := range updates {
		out = append(out, u.ID)
	}
	return out
}

// loggedMatching reports whether any log line contains sub.
//
// Every log assertion in this file goes through it rather than through
// len(logged()) != 0, because a run can log for more than one reason at
// once — see cannedChannelsInfo. A count-based assertion then passes on
// somebody else's line, which is how the silenced-users/info mutant
// survived its first run.
func (f *fakeDeps) loggedMatching(sub string) bool {
	for _, l := range f.logged() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]int64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- scope ------------------------------------------------------------

func TestRevalidate_SendsOnlySidebarChannelsNotTheWholeCache(t *testing.T) {
	// The regression this task exists for, on the channel side. slk
	// used to walk conversations.list; the official client sends
	// {id: version} for what the sidebar renders and nothing else.
	// Sweeping the cache instead would restore the enumeration in a
	// new costume — and a fixed batch size over an unbounded id set is
	// a CLEANER machine-detectable signature than the ragged
	// demand-driven one it imitates.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(f.channelsInfoSent, wantChannelsInfoSent()) {
		t.Errorf("channels/info was sent %v; want %v — the id set is userBoot's channels + ims, not the cache",
			f.channelsInfoSent, wantChannelsInfoSent())
	}
	for _, id := range []string{"C_CACHE_ONLY_1", "C_CACHE_ONLY_2", "C_CACHE_ONLY_3"} {
		if _, ok := f.channelsInfoSent[id]; ok {
			t.Errorf("channels/info was sent %s, which the cache holds but userBoot never mentioned (sent: %v)", id, sortedKeys(f.channelsInfoSent))
		}
	}
}

func TestRevalidate_SendsZeroForAChannelTheCacheHasNeverSeen(t *testing.T) {
	// C_PRIVATE and D_ALICE are absent from cannedChannelVersions.
	// Omitting them would be the natural "only revalidate what we
	// hold" reading and would leave both rows permanently unhydrated:
	// a record never asked about is never returned. 0 is how the
	// protocol asks for a full record, and the captures show the
	// client doing exactly that.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, id := range []string{"C_PRIVATE", "D_ALICE"} {
		v, ok := f.channelsInfoSent[id]
		if !ok {
			t.Errorf("%s has no cached version and was not sent at all; it must be sent with 0 (sent: %v)", id, f.channelsInfoSent)
			continue
		}
		if v != 0 {
			t.Errorf("%s was sent version %d; want 0 — the cache holds nothing for it", id, v)
		}
	}
}

func TestRevalidate_DoesNotSendEveryCachedUser(t *testing.T) {
	// The users.list replacement, and the whole point of the phase.
	// 500 cached users stand in for the ~10k a Grid workspace has: at
	// a fixed batch of 80 that is 125 consecutive exactly-80-id
	// requests, which is precisely the shape that gets slk's users
	// signed out. Scoping the id set is the fix.
	f := openedFake()
	for i := 0; i < 500; i++ {
		f.userVersions[fmt.Sprintf("U_BYSTANDER_%03d", i)] = int64(1700000000 + i)
	}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Count first, and fatally: a swept id set prints 506 entries, and
	// a 506-entry map diff is not a failure message anybody reads.
	if n := len(f.usersInfoSent); n != 3 {
		t.Fatalf("users/info was sent %d ids; want 3. 500 unrelated users sit in the cache and none of them is in scope at boot", n)
	}
	if !reflect.DeepEqual(f.usersInfoSent, wantUsersInfoSent()) {
		t.Errorf("users/info was sent %v; want %v — the id set is the view's authors plus open-DM counterparties",
			f.usersInfoSent, wantUsersInfoSent())
	}
}

func TestRevalidate_SendsZeroForAUserTheCacheHasNeverSeen(t *testing.T) {
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	v, ok := f.usersInfoSent["U_BOB"]
	if !ok {
		t.Fatalf("U_BOB has no cached version and was not sent at all (sent: %v)", f.usersInfoSent)
	}
	if v != 0 {
		t.Errorf("U_BOB was sent version %d; want 0", v)
	}
}

func TestRevalidate_SendsTheOpenDMCounterparties(t *testing.T) {
	// IMs[].UserID is the person on the other side of the DM;
	// IMs[].ID is the conversation and belongs to the channel pass.
	// Sending the conversation id to users/info (or omitting the
	// counterparty) leaves every DM in the sidebar showing a raw
	// user id.
	f := newFakeDeps()
	f.deps.OpenChannelID = "" // no view, so the DMs are the only source

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := map[string]int64{"U_ALICE": 1783337533030, "U_BOB": 0}
	if !reflect.DeepEqual(f.usersInfoSent, want) {
		t.Errorf("users/info was sent %v; want %v — the DM counterparties, not the DM conversation ids", f.usersInfoSent, want)
	}
}

func TestRevalidate_RunsAfterTheChannelIsOpened(t *testing.T) {
	// The user id set is the authors conversations.view returned, so
	// revalidating before the open scopes users/info to the DM
	// counterparties alone and leaves every author in the channel
	// being rendered stale — which is what brings the per-author
	// users.info fan-out back at render time.
	//
	// U_AUTHOR_ONLY exists in cannedViewResult for exactly this: the
	// other two authors are also DM counterparties, so only this one
	// distinguishes "ran after the open" from "ran before it".
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := f.usersInfoSent["U_AUTHOR_ONLY"]; !ok {
		t.Errorf("users/info was sent %v; U_AUTHOR_ONLY is an author of the opened channel with no DM, so its absence means revalidation ran before the channel was opened", sortedKeys(f.usersInfoSent))
	}
}

func TestRevalidate_SkipsEmptyIDs(t *testing.T) {
	// An IM with no counterparty is malformed, not impossible, and
	// {"": 0} in updated_ids is a request shape nothing observed
	// produces.
	f := newFakeDeps()
	f.deps.OpenChannelID = ""
	f.bootRes.IMs = append(f.bootRes.IMs, boot.IM{ID: "D_BROKEN", UserID: "", IsIM: true})
	f.bootRes.Channels = append(f.bootRes.Channels, boot.Channel{ID: "", Name: "no-id"})

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := f.usersInfoSent[""]; ok {
		t.Errorf("users/info was sent an empty id (sent: %v)", f.usersInfoSent)
	}
	if _, ok := f.channelsInfoSent[""]; ok {
		t.Errorf("channels/info was sent an empty id (sent: %v)", f.channelsInfoSent)
	}
}

func TestRevalidate_MakesNoRequestWithNothingInScope(t *testing.T) {
	// An updated_ids-less revalidation is a round trip that can only
	// return nothing, and a stream of them is a shape the official
	// client never produces.
	f := newFakeDeps()
	f.deps.OpenChannelID = ""
	f.bootRes.Channels = nil
	f.bootRes.IMs = nil

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.called(callChannelsInfo) {
		t.Errorf("channels/info was called with no conversations in scope (sequence: %v)", f.calls)
	}
	if f.called(callUsersInfo) {
		t.Errorf("users/info was called with no users in scope (sequence: %v)", f.calls)
	}
}

func TestRevalidate_UsesTheWorkspaceIDForEveryCacheCall(t *testing.T) {
	// Every one of these is scoped to a workspace, and slk runs
	// several at once. A blank or wrong id reads another workspace's
	// versions, which makes every channel here look unchanged.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"T_HOME"}; !reflect.DeepEqual(f.channelVersionsFor, want) {
		t.Errorf("ChannelVersions called for %v; want %v", f.channelVersionsFor, want)
	}
	if want := []string{"T_HOME"}; !reflect.DeepEqual(f.userVersionsFor, want) {
		t.Errorf("UserVersions called for %v; want %v", f.userVersionsFor, want)
	}
	if len(f.membershipCalls) != 2 {
		t.Fatalf("ApplyMembership called %d times; want 2, one per context team (%#v)", len(f.membershipCalls), f.membershipCalls)
	}
	for _, c := range f.membershipCalls {
		if c.workspaceID != "T_HOME" {
			t.Errorf("ApplyMembership workspace = %q; want T_HOME", c.workspaceID)
		}
	}
}

func TestRevalidate_DoesNotCrossTheVersionMaps(t *testing.T) {
	// Both maps deliberately contain one id belonging to the other
	// (see cannedChannelVersions). Looking channels up in the user map
	// still compiles, still sends a plausible request, and quietly
	// tells the server slk holds a version it does not — so the server
	// withholds a record slk never received, forever.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := f.channelsInfoSent["C_GENERAL"]; got != 1783337533019 {
		t.Errorf("C_GENERAL was sent version %d; want 1783337533019 (the CHANNEL map's value; the user map holds 5555555555 for it)", got)
	}
	if got := f.usersInfoSent["U_ALICE"]; got != 1783337533030 {
		t.Errorf("U_ALICE was sent version %d; want 1783337533030 (the USER map's value; the channel map holds 4444444444 for it)", got)
	}
}

// --- writes -----------------------------------------------------------

func TestRevalidate_WritesChannelsThroughThePartialWriter(t *testing.T) {
	// Every mapped field at once, because each is independently
	// transposable while still compiling — Name into Topic being the
	// obvious one, since both are strings on both structs.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(f.channelUpdates, wantChannelUpdates()) {
		t.Errorf("channel updates = %#v; want %#v", f.channelUpdates, wantChannelUpdates())
	}
}

func TestRevalidate_WritesUsersThroughThePartialWriter(t *testing.T) {
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(f.userUpdates, wantUserUpdates()) {
		t.Errorf("user updates = %#v; want %#v", f.userUpdates, wantUserUpdates())
	}
}

func TestRevalidate_StampsTheNewVersions(t *testing.T) {
	// The conditional protocol is worthless without this half: a
	// record revalidated but never version-stamped comes back in full
	// on every single boot, forever, which is the bandwidth this task
	// is supposed to remove.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, u := range f.channelUpdates {
		if u.Version == 0 {
			t.Errorf("channel %s was written with version 0; the response carried one", u.ID)
		}
	}
	for _, u := range f.userUpdates {
		if u.Version == 0 {
			t.Errorf("user %s was written with version 0; the response carried one", u.ID)
		}
	}
}

func TestRevalidate_MapsTheChannelType(t *testing.T) {
	// The cache's type column drives the sidebar glyph and the DM/
	// group-DM notification rules. Order is load-bearing: an MPDM is
	// private too, and a DM is neither, so testing is_private first
	// files every group DM under "private".
	for _, tc := range []struct {
		name string
		ch   edge.Channel
		want string
	}{
		{"im", edge.Channel{ID: "C_GENERAL", IsIM: true}, "dm"},
		{"mpim", edge.Channel{ID: "C_GENERAL", IsMPIM: true, IsPrivate: true}, "group_dm"},
		{"private", edge.Channel{ID: "C_GENERAL", IsPrivate: true, IsGroup: true}, "private"},
		{"public", edge.Channel{ID: "C_GENERAL", IsChannel: true}, "channel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openedFake()
			f.channelsInfoRes.Channels = []edge.Channel{tc.ch}

			if _, err := Run(context.Background(), f.Deps()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(f.channelUpdates) != 1 {
				t.Fatalf("channel updates = %#v; want exactly 1", f.channelUpdates)
			}
			if got := f.channelUpdates[0].Type; got != tc.want {
				t.Errorf("type = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestRevalidate_UserDisplayNameFallsBack(t *testing.T) {
	// UpdateUserFromEdge writes display_name unconditionally, so an
	// empty one here blanks whatever the cache held and the sidebar
	// starts showing raw handles. resolveUser already uses this exact
	// chain (main.go:2432).
	for _, tc := range []struct {
		name    string
		display string
		real    string
		handle  string
		want    string
	}{
		{"display name wins", "the-display", "The Real", "the-handle", "the-display"},
		{"real name when no display name", "", "The Real", "the-handle", "The Real"},
		{"handle when neither", "", "", "the-handle", "the-handle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openedFake()
			f.usersInfoRes = []edge.User{edgeUser("U_ALICE", tc.handle, tc.display, tc.real, "", "T_HOME", 1783337599010, false)}

			if _, err := Run(context.Background(), f.Deps()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(f.userUpdates) != 1 {
				t.Fatalf("user updates = %#v; want exactly 1", f.userUpdates)
			}
			if got := f.userUpdates[0].DisplayName; got != tc.want {
				t.Errorf("display name = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestRevalidate_MarksForeignTeamsExternal(t *testing.T) {
	// Same test resolveUser applies (main.go:2440), empty guard
	// included: a result with no team_id is unknown, not foreign, and
	// marking it external puts a Slack Connect badge on a colleague.
	for _, tc := range []struct {
		name   string
		teamID string
		want   bool
	}{
		{"same team", "T_HOME", false},
		{"other team", "T_OTHER", true},
		{"no team id", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openedFake()
			f.usersInfoRes = []edge.User{edgeUser("U_ALICE", "alice", "alice-display", "Alice", "", tc.teamID, 1783337599010, false)}

			if _, err := Run(context.Background(), f.Deps()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(f.userUpdates) != 1 {
				t.Fatalf("user updates = %#v; want exactly 1", f.userUpdates)
			}
			if got := f.userUpdates[0].IsExternal; got != tc.want {
				t.Errorf("is external = %v; want %v (team %q, workspace T_HOME)", got, tc.want, tc.teamID)
			}
		})
	}
}

// --- membership -------------------------------------------------------

func TestRevalidate_AppliesMembershipToTheQueriedIDsNotTheReturnedOnes(t *testing.T) {
	// member_channels is a snapshot over the ids ASKED about, so an id
	// that was asked about and is absent from it is a genuine
	// non-membership and gets cleared. Passing the RETURNED members as
	// the queried set therefore tells ApplyMembership that every id it
	// was not handed is a non-member — silently dropping the user out
	// of every channel in the batch that did not happen to come back.
	f := openedFake()
	f.channelsInfoRes.FailedIDs = failedChannelIDs

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.membershipCalls) != 2 {
		t.Fatalf("ApplyMembership called %d times; want 2, one per context team (%#v)", len(f.membershipCalls), f.membershipCalls)
	}
	// Index alignment is the invariant: one membership call per
	// channels/info call, in the same order. It holds only because
	// every team's filtered MembershipQueried is non-empty — a team
	// whose batch reported nothing produces no membership call and
	// would shift the alignment.
	byTeam := map[string]membershipCall{}
	for i, c := range f.channelsInfoCalls {
		byTeam[c.team] = f.membershipCalls[i]
	}
	home := byTeam["T_HOME"]
	if want := []string{"C_GENERAL", "D_ALICE"}; !reflect.DeepEqual(home.queriedIDs, want) {
		t.Errorf("T_HOME queried set = %v; want %v (MembershipQueried, never MemberChannels %v)", home.queriedIDs, want, cannedChannelsInfo().MemberChannels)
	}
	if want := cache.MembershipReported([]string{"C_GENERAL"}, nil); !reflect.DeepEqual(home.snap, want) {
		t.Errorf("T_HOME snapshot = %#v; want %#v", home.snap, want)
	}
	other := byTeam["T_OTHER"]
	if want := []string{"C_PRIVATE"}; !reflect.DeepEqual(other.queriedIDs, want) {
		t.Errorf("T_OTHER queried set = %v; want %v", other.queriedIDs, want)
	}
	// The failed id rides in its own team's snapshot: omitting it
	// would clear is_member for an id the server explicitly could not
	// answer about.
	if want := cache.MembershipReported(nil, failedChannelIDs); !reflect.DeepEqual(other.snap, want) {
		t.Errorf("T_OTHER snapshot = %#v; want %#v", other.snap, want)
	}
}

func TestRevalidate_MakesNoMembershipCallWhenNoBatchReported(t *testing.T) {
	// member_channels is ABSENT from 13 of the 18 observed responses,
	// every one of which sent check_membership:true. Absent means no
	// information, so nothing is written — and an empty
	// MembershipQueried is how edge reports exactly that.
	//
	// The second case is the trap: MemberChannels non-empty with
	// MembershipQueried empty cannot happen, but an implementation
	// keying off len(MemberChannels) instead has no way to tell, and
	// would apply an authoritative snapshot against a set it was never
	// given.
	for _, tc := range []struct {
		name string
		res  edge.ChannelsInfoResult
	}{
		{"nothing reported", edge.ChannelsInfoResult{Channels: cannedChannelsInfo().Channels}},
		{"members without a queried set", edge.ChannelsInfoResult{
			Channels:       cannedChannelsInfo().Channels,
			MemberChannels: []string{"C_GENERAL"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openedFake()
			f.channelsInfoRes = tc.res

			if _, err := Run(context.Background(), f.Deps()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(f.membershipCalls) != 0 {
				t.Errorf("ApplyMembership called %#v; no batch reported membership, so nothing is known and nothing may be written", f.membershipCalls)
			}
		})
	}
}

func TestRevalidate_MembershipFailureIsNotFatal(t *testing.T) {
	f := openedFake()
	f.membershipErr = errors.New("database is locked")

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: applying membership must not fail the boot: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result")
	}
	if !f.loggedMatching("applying membership") {
		t.Errorf("applying membership failed and Run did not say so (logged: %v)", f.logged())
	}
}

// --- failed ids -------------------------------------------------------

func TestRevalidate_DoesNotAdvanceVersionsForFailedIDs(t *testing.T) {
	// A failed id is INDISTINGUISHABLE from an unchanged one: both are
	// simply absent from results. Stamping it as current keeps its
	// stale record forever, because its version never moves and so it
	// never comes back. Leaving the version alone means the next boot
	// asks again.
	f := openedFake()
	f.channelsInfoRes.FailedIDs = failedChannelIDs

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, failed := range failedChannelIDs {
		for _, u := range f.channelUpdates {
			if u.ID == failed {
				t.Errorf("channels/info could not resolve %s and it was written anyway, with version %d; that marks a stale record current forever", failed, u.Version)
			}
		}
	}
	if got := channelUpdateIDs(f.channelUpdates); !reflect.DeepEqual(got, []string{"C_GENERAL", "D_ALICE"}) {
		t.Errorf("channels written = %v; want only the two the server actually returned", got)
	}
}

func TestRevalidate_LogsFailedIDs(t *testing.T) {
	// Silently stale rows are undiagnosable: the UI shows old data and
	// nothing anywhere says why.
	f := openedFake()
	f.channelsInfoRes.FailedIDs = failedChannelIDs

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.loggedMatching("could not resolve") {
		t.Errorf("channels/info returned failed ids and Run did not say so (logged: %v)", f.logged())
	}
}

// --- failure modes ----------------------------------------------------

func TestRevalidate_ErrorIsNotFatal(t *testing.T) {
	// A stale cache renders. A workspace that failed to boot does not.
	// wantLog is per case, and specific, rather than "logged
	// something". A run can log for several reasons at once, so a
	// count-based check passes on a line from an unrelated step —
	// which is exactly how a silenced users/info failure survived its
	// first mutation run.
	for _, tc := range []struct {
		name    string
		prepare func(*fakeDeps)
		wantLog []string
	}{
		{"channels/info failed", func(f *fakeDeps) { f.channelsInfoErr = errors.New("ratelimited") },
			[]string{"channels/info for"}},
		{"users/info failed", func(f *fakeDeps) { f.usersInfoErr = errors.New("ratelimited") },
			[]string{"users/info for"}},
		{"both failed", func(f *fakeDeps) {
			f.channelsInfoErr = errors.New("ratelimited")
			f.usersInfoErr = errors.New("ratelimited")
		}, []string{"channels/info for", "users/info for"}},
		{"channel version lookup failed", func(f *fakeDeps) { f.channelVersionsErr = errors.New("database is locked") },
			[]string{"reading cached channel versions"}},
		{"user version lookup failed", func(f *fakeDeps) { f.userVersionsErr = errors.New("database is locked") },
			[]string{"reading cached user versions"}},
		{"channel write failed", func(f *fakeDeps) { f.channelUpdateErr = errors.New("database is locked") },
			[]string{"caching revalidated channel"}},
		{"user write failed", func(f *fakeDeps) { f.userUpdateErr = errors.New("database is locked") },
			[]string{"caching revalidated user"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openedFake()
			tc.prepare(f)

			res, err := Run(context.Background(), f.Deps())
			if err != nil {
				t.Fatalf("Run: revalidation failures must not fail the boot: %v", err)
			}
			if res == nil {
				t.Fatal("Run returned nil Result")
			}
			// Still a usable workspace, assembled from userBoot.
			if res.Self.ID != "U_SELF" || len(res.Channels) == 0 {
				t.Errorf("Result is not usable after a revalidation failure: Self.ID=%q, %d channels", res.Self.ID, len(res.Channels))
			}
			for _, want := range tc.wantLog {
				if !f.loggedMatching(want) {
					t.Errorf("nothing logged mentioning %q; a silently swallowed revalidation failure is a stale cache nobody can diagnose (logged: %v)", want, f.logged())
				}
			}
		})
	}
}

func TestRevalidate_ChannelsInfoErrorDiscardsTheBody(t *testing.T) {
	// Slack answers ok:false with a fully populated body, so "err !=
	// nil and the value looks fine" is the normal shape of a failure
	// here. Writing it caches a response the client already rejected.
	f := openedFake()
	f.channelsInfoErr = errors.New("ratelimited")

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.channelUpdates) != 0 {
		t.Errorf("channel updates = %#v; want none — they came out of a response Run rejected", f.channelUpdates)
	}
	if len(f.membershipCalls) != 0 {
		t.Errorf("ApplyMembership called %#v; the response it came from failed", f.membershipCalls)
	}
}

func TestRevalidate_UsersInfoErrorDiscardsTheBody(t *testing.T) {
	f := openedFake()
	f.usersInfoErr = errors.New("ratelimited")

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.userUpdates) != 0 {
		t.Errorf("user updates = %#v; want none — they came out of a response Run rejected", f.userUpdates)
	}
}

func TestRevalidate_ChannelsInfoFailureDoesNotSkipUsersInfo(t *testing.T) {
	// The two passes are independent, and losing the user pass to a
	// channel failure sends slk back to resolving every author one
	// users.info call at a time — the fan-out this phase deletes.
	f := openedFake()
	f.channelsInfoErr = errors.New("ratelimited")

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.called(callUsersInfo) {
		t.Errorf("channels/info failed and users/info was skipped with it (sequence: %v)", f.calls)
	}
	if !reflect.DeepEqual(f.userUpdates, wantUserUpdates()) {
		t.Errorf("user updates = %#v; want %#v", f.userUpdates, wantUserUpdates())
	}
}

func TestRevalidate_VersionLookupFailureDiscardsTheReturnedMap(t *testing.T) {
	// The map beside the error must not reach the wire. Vouching for a
	// version slk does not hold makes the server withhold a record slk
	// never received — the failure is then permanent and invisible,
	// since the version stays put and the record never arrives.
	// Sending 0 for everything costs bytes and is correct.
	f := openedFake()
	f.channelVersionsErr = errors.New("database is locked")
	f.userVersionsErr = errors.New("database is locked")

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.called(callChannelsInfo) {
		t.Fatalf("channels/info was skipped entirely (sequence: %v)", f.calls)
	}
	for id, v := range f.channelsInfoSent {
		if v != 0 {
			t.Errorf("channels/info was sent %s=%d after the version read FAILED; want 0", id, v)
		}
	}
	for id, v := range f.usersInfoSent {
		if v != 0 {
			t.Errorf("users/info was sent %s=%d after the version read FAILED; want 0", id, v)
		}
	}
	if !reflect.DeepEqual(sortedKeys(f.channelsInfoSent), sortedKeys(wantChannelsInfoSent())) {
		t.Errorf("channels/info was sent %v; want the same id set as a healthy read, %v", sortedKeys(f.channelsInfoSent), sortedKeys(wantChannelsInfoSent()))
	}
}

func TestRevalidate_WriteFailureDoesNotAbandonTheRestOfTheBatch(t *testing.T) {
	// The request is already spent. Abandoning the remaining rows
	// leaves versions unadvanced for channels the server did answer
	// about, so the next boot pays for them again.
	f := openedFake()
	f.channelUpdateErr = errors.New("database is locked")
	f.userUpdateErr = errors.New("database is locked")

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.channelUpdates) != len(wantChannelUpdates()) {
		t.Errorf("%d channel writes attempted; want %d — one failure must not cost the rest of the batch", len(f.channelUpdates), len(wantChannelUpdates()))
	}
	if len(f.userUpdates) != len(wantUserUpdates()) {
		t.Errorf("%d user writes attempted; want %d", len(f.userUpdates), len(wantUserUpdates()))
	}
}

func TestRevalidate_MissingDependenciesAreLoggedNotFatal(t *testing.T) {
	// The opposite of Run's other nil guards, deliberately: a missing
	// Revalidator leaves the cache as stale as it already was, which
	// is exactly what a failed revalidation request does, and that is
	// documented non-fatal. Refusing to boot over it would be worse
	// than the failure being reported. The log line is the only signal
	// a Task 7 wiring omission gets, so it has to exist.
	for _, tc := range []struct {
		name string
		nilf func(*Deps)
	}{
		{"Revalidate", func(d *Deps) { d.Revalidate = nil }},
		{"Store", func(d *Deps) { d.Store = nil }},
	} {
		wantLog := "Deps." + tc.name + " is nil"
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.deps.OpenChannelID = "" // Store is required to open a channel
			tc.nilf(&f.deps)

			res, err := Run(context.Background(), f.Deps())
			if err != nil {
				t.Fatalf("Run: a nil %s must not fail the boot: %v", tc.name, err)
			}
			if res == nil {
				t.Fatal("Run returned nil Result")
			}
			if f.called(callChannelsInfo) || f.called(callUsersInfo) {
				t.Errorf("revalidated with a nil %s (sequence: %v)", tc.name, f.calls)
			}
			if !f.loggedMatching(wantLog) {
				t.Errorf("Deps.%s was nil and nothing logged mentioning %q; a wiring omission would then silently disable the whole point of the phase (logged: %v)", tc.name, wantLog, f.logged())
			}
		})
	}
}

// --- team partitioning ------------------------------------------------

// TestRevalidate_PartitionsChannelsInfoByContextTeam is the Grid fix.
// The fixture spans two context teams (C_GENERAL/D_ALICE on T_HOME,
// C_PRIVATE/D_BOB on T_OTHER), so a correct run issues one
// channels/info per team, each carrying exactly that team's ids.
func TestRevalidate_PartitionsChannelsInfoByContextTeam(t *testing.T) {
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.channelsInfoCalls) != 2 {
		t.Fatalf("channels/info calls = %d; want 2, one per context team (calls: %+v)", len(f.channelsInfoCalls), f.channelsInfoCalls)
	}
	byTeam := map[string]map[string]int64{}
	for _, c := range f.channelsInfoCalls {
		byTeam[c.team] = c.sent
	}
	if want := map[string]int64{"C_GENERAL": 1783337533019, "D_ALICE": 0}; !reflect.DeepEqual(byTeam["T_HOME"], want) {
		t.Errorf("T_HOME was sent %v; want %v", byTeam["T_HOME"], want)
	}
	if want := map[string]int64{"C_PRIVATE": 0, "D_BOB": 1783337533022}; !reflect.DeepEqual(byTeam["T_OTHER"], want) {
		t.Errorf("T_OTHER was sent %v; want %v", byTeam["T_OTHER"], want)
	}
}

// TestRevalidate_EmptyContextTeamUsesTheWorkspaceTeam: a conversation
// userBoot gave no context_team_id for must behave exactly as
// pre-Grid code behaved — scoped to the workspace's own team.
//
// NOTE: this is a characterization test. It passes both before and
// after the partition lands, because the pre-partition code scopes
// everything to the workspace team anyway. Its job is to pin the
// fallback rule so a future refactor can't drop it.
func TestRevalidate_EmptyContextTeamUsesTheWorkspaceTeam(t *testing.T) {
	f := openedFake()
	f.bootRes.Channels = append(f.bootRes.Channels, boot.Channel{ID: "C_NO_TEAM", Name: "no-team"})

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range f.channelsInfoCalls {
		if _, ok := c.sent["C_NO_TEAM"]; ok {
			if c.team != "T_HOME" {
				t.Errorf("C_NO_TEAM was scoped to %q; want the workspace team T_HOME", c.team)
			}
			return
		}
	}
	t.Errorf("C_NO_TEAM was never sent (calls: %+v)", f.channelsInfoCalls)
}

// TestRevalidate_TeamFailureDoesNotSkipOtherTeams mirrors the existing
// channels/users independence guarantee one level down: one team's
// failure must not strand the other team's revalidation.
func TestRevalidate_TeamFailureDoesNotSkipOtherTeams(t *testing.T) {
	f := openedFake()
	f.channelsInfoErrFor = map[string]error{"T_OTHER": errors.New("ratelimited")}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.channelsInfoCalls) != 2 {
		t.Fatalf("channels/info calls = %d; want 2 — a failed team must not suppress the other team's call", len(f.channelsInfoCalls))
	}
	// T_HOME's results still land.
	if !reflect.DeepEqual(f.channelUpdates, wantChannelUpdates()) {
		t.Errorf("channel updates = %#v; want %#v from the healthy team", f.channelUpdates, wantChannelUpdates())
	}
	if !f.loggedMatching("team T_OTHER") {
		t.Errorf("the failed team was not named in the log; on Grid this line is the only diagnostic (logged: %v)", f.logged())
	}
}

// --- edge health ------------------------------------------------------

// bigTeamFixture builds a boot whose conversations partition into one
// 6-id T_BIG group and one 2-id T_SMALL group, all channels (no IMs
// unless added by the test). 6 of 8 ids is over the majority
// threshold; T_BIG sorts first by size.
func bigTeamFixture(f *fakeDeps) {
	f.bootRes.Channels = []boot.Channel{
		{ID: "C_BIG1", Name: "big1", ContextTeamID: "T_BIG"},
		{ID: "C_BIG2", Name: "big2", ContextTeamID: "T_BIG"},
		{ID: "C_BIG3", Name: "big3", ContextTeamID: "T_BIG"},
		{ID: "C_BIG4", Name: "big4", ContextTeamID: "T_BIG"},
		{ID: "C_BIG5", Name: "big5", ContextTeamID: "T_BIG"},
		{ID: "C_BIG6", Name: "big6", ContextTeamID: "T_BIG"},
		{ID: "C_SMALL1", Name: "small1", ContextTeamID: "T_SMALL"},
		{ID: "C_SMALL2", Name: "small2", ContextTeamID: "T_SMALL"},
	}
	f.bootRes.IMs = nil
}

func TestRevalidate_WholesaleFailureMarksDegradedAndAborts(t *testing.T) {
	// Measured on the first working Grid session (2026-08-05): one
	// enterprise-id group holding 79% of the user's conversations
	// resolved none of them, and the 16 foreign-team groups behind it
	// were all Unauthenticated — 23 wasted edge calls per boot. The
	// largest group failing wholesale IS the diagnosis; the rest of
	// the partition is not worth spending requests on.
	f := openedFake()
	bigTeamFixture(f)
	f.deps.Health = edge.NewHealth()
	f.channelsInfoRes.FailedIDs = []string{"C_BIG1", "C_BIG2", "C_BIG3", "C_BIG4", "C_BIG5", "C_BIG6"}
	// The canned response's Channels/queried sets name fixture ids
	// this boot does not have, so filterChannelsInfo reduces them to
	// nothing for these ids — only the failed ids apply.

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.deps.Health.Degraded() {
		t.Error("the majority group failed wholesale and edge was not marked degraded; the resolver will keep spending edge calls on a workspace where they resolve nothing")
	}
	if len(f.channelsInfoCalls) != 1 {
		t.Errorf("channels/info calls = %d; want 1 — after a wholesale failure of the majority group the remaining teams are aborted (calls: %+v)", len(f.channelsInfoCalls), f.channelsInfoCalls)
	}
	if f.channelsInfoCalls[0].team != "T_BIG" {
		t.Errorf("first call went to %q; want T_BIG — groups are processed largest-first so the diagnosis comes before the spend", f.channelsInfoCalls[0].team)
	}
	if !f.loggedMatching("degraded") {
		t.Errorf("a wholesale edge failure must say what it decided (logged: %v)", f.logged())
	}
}

func TestRevalidate_CallErrorOfTheMajorityGroupAlsoMarksDegraded(t *testing.T) {
	// The other wholesale shape: not failed_ids but an error —
	// ratelimited, or Grid's Unauthenticated.
	f := openedFake()
	bigTeamFixture(f)
	f.deps.Health = edge.NewHealth()
	f.channelsInfoErrFor = map[string]error{"T_BIG": errors.New("Unauthenticated")}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.deps.Health.Degraded() {
		t.Error("an errored majority group did not mark edge degraded")
	}
	if len(f.channelsInfoCalls) != 1 {
		t.Errorf("channels/info calls = %d; want 1", len(f.channelsInfoCalls))
	}
}

func TestRevalidate_IMOnlyFailureDoesNotMarkDegraded(t *testing.T) {
	// IMs ALWAYS land in failed_ids — 22 of 22 across the captures,
	// on healthy workspaces. A group whose only failures are IMs is
	// the normal case, and marking it degraded would disable edge
	// batching everywhere.
	f := openedFake()
	f.bootRes.Channels = nil
	f.bootRes.IMs = []boot.IM{
		{ID: "D_A", UserID: "U_ALICE", IsIM: true, IsOpen: true, ContextTeamID: "T_HOME"},
		{ID: "D_B", UserID: "U_BOB", IsIM: true, IsOpen: true, ContextTeamID: "T_HOME"},
	}
	f.deps.Health = edge.NewHealth()
	f.channelsInfoRes.FailedIDs = []string{"D_A", "D_B"}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.deps.Health.Degraded() {
		t.Error("IM-only failures marked edge degraded; IMs land in failed_ids on every healthy workspace")
	}
}

func TestRevalidate_HealthyRunDoesNotMarkDegraded(t *testing.T) {
	f := openedFake()
	f.deps.Health = edge.NewHealth()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.deps.Health.Degraded() {
		t.Error("a healthy revalidation marked edge degraded")
	}
}

func TestRevalidate_MinorityWholesaleFailureDoesNotAbort(t *testing.T) {
	// The threshold is the majority: a failing group that holds less
	// than half the ids is one team's problem, not a broken edge.
	// Here T_BIG holds 3 of 7 — wholesale-failed, but a minority —
	// so all three teams are still called and nothing is marked.
	f := openedFake()
	f.bootRes.Channels = []boot.Channel{
		{ID: "C_BIG1", Name: "big1", ContextTeamID: "T_BIG"},
		{ID: "C_BIG2", Name: "big2", ContextTeamID: "T_BIG"},
		{ID: "C_BIG3", Name: "big3", ContextTeamID: "T_BIG"},
		{ID: "C_MID1", Name: "mid1", ContextTeamID: "T_MID"},
		{ID: "C_MID2", Name: "mid2", ContextTeamID: "T_MID"},
		{ID: "C_SMALL1", Name: "small1", ContextTeamID: "T_SMALL"},
		{ID: "C_SMALL2", Name: "small2", ContextTeamID: "T_SMALL"},
	}
	f.bootRes.IMs = nil
	f.deps.Health = edge.NewHealth()
	f.channelsInfoErrFor = map[string]error{"T_BIG": errors.New("ratelimited")}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.deps.Health.Degraded() {
		t.Error("a minority group's failure marked edge degraded; the threshold is the majority of ids")
	}
	if len(f.channelsInfoCalls) != 3 {
		t.Errorf("channels/info calls = %d; want 3 — a minority failure aborts nothing", len(f.channelsInfoCalls))
	}
}

// --- budget -----------------------------------------------------------

func TestRevalidate_StaysInsideTheBootCallBudget(t *testing.T) {
	// Success criterion 1, on the full sequence: userBoot, counts,
	// conversations.view, channels/info per context team (two here),
	// users/info — six, against a budget of ten, replacing roughly
	// four hundred.
	f := openedFake()

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.calls) > 10 {
		t.Errorf("boot issued %d calls; budget is 10 (sequence: %v)", len(f.calls), f.calls)
	}
	if n := f.countPrefix(callChannelsInfo); n != 2 {
		t.Errorf("channels/info called %d times; want exactly 2 — the fixture spans two context teams (T_HOME, T_OTHER), and each gets one batched conditional request (sequence: %v)", n, f.calls)
	}
	if n := f.countPrefix(callUsersInfo); n != 1 {
		t.Errorf("users/info called %d times; want exactly 1 (sequence: %v)", n, f.calls)
	}
}
