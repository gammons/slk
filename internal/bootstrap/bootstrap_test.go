package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/gammons/slk/internal/slack/boot"
)

// The names the fake records. They are the API method names rather than
// the Go method names on purpose: TestRun_NeverEnumerates asserts
// against method names ("users.list"), so recording anything else would
// make the guard unable to see the very thing it exists to catch.
// The two edgeapi names carry the "edge:" prefix slackhttp.Counter
// keys them by, so a call log read side by side with a counter report
// names the same things.
const (
	callUserBoot     = "client.userBoot"
	callCounts       = "client.counts"
	callView         = "conversations.view"
	callHistory      = "conversations.history"
	callChannelsInfo = "edge:channels/info"
	callUsersInfo    = "edge:users/info"
)

// cannedBootResult is the userBoot response every test runs against.
//
// EVERY field Run reads is populated with a DISTINCT, NON-ZERO value,
// and that is load-bearing rather than tidiness. Phase 2a lost 9
// mutants to a fixture whose booleans were all false and another to two
// string fields that happened to share a value: a field mapped from the
// wrong source, or dropped entirely, is invisible when the right and
// wrong answers are both the zero value.
//
// The same-typed neighbours are the specific hazard here, because
// swapping them still compiles:
//
//   - IsOpen, ReadOnlyChannels, NonThreadableChannels and
//     ThreadOnlyChannels are all []string on boot.Result. The three Run
//     does NOT map are populated anyway, so that mapping IsOpen from
//     one of them is detectable.
//   - Prefs.AllNotificationsPrefs and Prefs.MutedChannels are both
//     strings, and Run maps them to two different Result fields.
//   - EmojiCacheTS and DefaultWorkspace are both strings.
func cannedBootResult() *boot.Result {
	return &boot.Result{
		Channels: []boot.Channel{
			{
				ID:             "C_GENERAL",
				Name:           "general",
				NameNormalized: "general-normalized",
				Version:        1783337533019,
				Created:        1600000001,
				IsChannel:      true,
				IsGeneral:      true,
				IsShared:       true,
				ContextTeamID:  "T_HOME",
				Creator:        "U_CREATOR_1",
				SharedTeamIDs:  []string{"T_HOME", "T_GUEST"},
				Topic:          boot.TextBlock{Value: "topic one", Creator: "U_CREATOR_1", LastSet: 1600000011},
				Purpose:        boot.TextBlock{Value: "purpose one", Creator: "U_CREATOR_1", LastSet: 1600000012},
			},
			{
				ID:             "C_PRIVATE",
				Name:           "private",
				NameNormalized: "private-normalized",
				Version:        1783337533020,
				Created:        1600000002,
				IsGroup:        true,
				IsPrivate:      true,
				IsArchived:     true,
				IsMPIM:         true,
				IsOrgShared:    true,
				IsExtShared:    true,
				ContextTeamID:  "T_OTHER",
				Creator:        "U_CREATOR_2",
				SharedTeamIDs:  []string{"T_OTHER"},
				Topic:          boot.TextBlock{Value: "topic two", Creator: "U_CREATOR_2", LastSet: 1600000021},
				Purpose:        boot.TextBlock{Value: "purpose two", Creator: "U_CREATOR_2", LastSet: 1600000022},
			},
		},
		IMs: []boot.IM{
			{
				ID:            "D_ALICE",
				UserID:        "U_ALICE",
				Version:       1783337533021,
				Created:       1600000003,
				IsIM:          true,
				IsOpen:        true,
				ContextTeamID: "T_HOME",
			},
			{
				ID:            "D_BOB",
				UserID:        "U_BOB",
				Version:       1783337533022,
				Created:       1600000004,
				IsIM:          true,
				IsArchived:    true,
				IsOrgShared:   true,
				ContextTeamID: "T_OTHER",
			},
		},
		IsOpen:  []string{"C_GENERAL", "D_ALICE"},
		Starred: []json.RawMessage{json.RawMessage(`{"type":"channel","channel":"C_GENERAL"}`)},
		Subteams: boot.Subteams{
			Self: []json.RawMessage{json.RawMessage(`{"id":"S_TEAM"}`)},
		},
		DND: boot.DND{
			Enabled:       true,
			NextStartTS:   1783300000,
			NextEndTS:     1783330000,
			SnoozeEnabled: true,
		},
		Prefs: boot.Prefs{
			MutedChannels:         "C_LEGACY_MUTED,C_LEGACY_MUTED_2",
			AllNotificationsPrefs: `{"channels":{"C_PRIVATE":{"muted":true}}}`,
			Raw:                   json.RawMessage(`{"muted_channels":"C_LEGACY_MUTED,C_LEGACY_MUTED_2"}`),
		},
		Self: boot.Self{
			ID:       "U_SELF",
			Name:     "self-name",
			TeamID:   "T_HOME",
			RealName: "Self Realname",
			TZ:       "America/New_York",
			TZOffset: -14400,
			Version:  1783337533023,
			Profile: boot.SelfProfile{
				RealName:         "Profile Realname",
				DisplayName:      "profile-display",
				AvatarHash:       "abc123hash",
				ImageOriginal:    "https://example.invalid/avatar-original.png",
				Email:            "self@example.invalid",
				StatusText:       "status text",
				StatusEmoji:      ":wave:",
				StatusExpiration: 1783400000,
			},
		},
		Team: boot.Team{
			ID:            "T_HOME",
			Name:          "Home Team",
			Domain:        "home-domain",
			URL:           "https://home.example.invalid/",
			AvatarBaseURL: "https://avatars.example.invalid/",
		},
		ChannelsPriority: map[string]float64{"C_GENERAL": 0.75, "C_PRIVATE": 0.25},
		EmojiCacheTS:     "17833375330191742",

		// Populated but NOT mapped by Run. They exist so that mapping
		// Result.IsOpen from the wrong []string is detectable.
		ReadOnlyChannels:      []string{"C_READONLY"},
		NonThreadableChannels: []string{"C_NONTHREADABLE"},
		ThreadOnlyChannels:    []string{"C_THREADONLY"},

		DefaultWorkspace: "T_DEFAULT_WORKSPACE",
		HasMoreMPDMs:     true,
	}
}

// cannedCounts is the client.counts response every test runs against.
// Distinct, non-zero throughout for the same reason as
// cannedBootResult.
func cannedCounts() Counts {
	return Counts{
		Unreads: []Unread{
			{ChannelID: "C_GENERAL", Count: 7, HasUnread: true, LastRead: "1700000001.000100"},
			{ChannelID: "D_ALICE", Count: 3, HasUnread: true, LastRead: "1700000002.000200"},
		},
		Threads: Threads{HasUnreads: true, UnreadCount: 11, MentionCount: 5},
	}
}

// cannedViewResult is the conversations.view response the fake returns.
//
// Distinct, non-zero in every section for the reason cannedBootResult
// gives, and with one extra hazard of its own: Users, Channels and
// Emojis are three SEPARATE fields that openChannel copies in three
// separate statements, so a fixture where two of them shared a value
// would hide a dropped one. They have different Go types, so a swap
// would not compile — a DROP is the whole mutation space here, and a
// drop is only visible against a non-empty expectation.
//
// Channel.ID is left empty on purpose: the fake stamps it from
// f.viewChannelID, which is the knob every test in this file turns to
// say "the server honoured the channel param" or "it did not".
func cannedViewResult() *boot.ViewResult {
	return &boot.ViewResult{
		History: boot.History{
			Messages: []json.RawMessage{
				json.RawMessage(`{"ts":"1700000010.000100","user":"U_ALICE","text":"from conversations.view"}`),
				json.RawMessage(`{"ts":"1700000011.000100","user":"U_BOB","text":"also from conversations.view"}`),
			},
			HasMore:            true,
			MutationTimestamps: boot.MutationTimestamps{Latest: "1783337533019174", Updated: "1783337533019175", HistoryInvalid: "1783337533019176"},
			NextTS:             1700000009,
		},
		Users: []boot.User{
			{
				ID: "U_ALICE", TeamID: "T_HOME", Name: "alice", RealName: "Alice Toplevel",
				Version: 1783337533030, IsBot: true,
				Profile: boot.UserProfile{RealName: "Alice Profile", DisplayName: "alice-display", AvatarHash: "alicehash"},
			},
			{
				ID: "U_BOB", TeamID: "T_OTHER", Name: "bob", RealName: "Bob Toplevel",
				Version: 1783337533031, Deleted: true, IsAppUser: true,
				Profile: boot.UserProfile{RealName: "Bob Profile", DisplayName: "bob-display", AvatarHash: "bobhash"},
			},
			{
				// An author with NO open DM. U_ALICE and U_BOB are
				// both counterparties of cannedBootResult's IMs, so
				// without this third user "revalidation scoped only to
				// the open DMs" and "revalidation scoped to the DMs
				// plus the opened channel's authors" send the same id
				// set — and so does "revalidation ran before the
				// channel was opened", which is the mutation this
				// fixture exists to make visible.
				ID: "U_AUTHOR_ONLY", TeamID: "T_HOME", Name: "author-only", RealName: "Author Only Toplevel",
				Version: 1783337533034,
				Profile: boot.UserProfile{RealName: "Author Only Profile", DisplayName: "author-only-display", AvatarHash: "authorhash"},
			},
		},
		Bots: []json.RawMessage{json.RawMessage(`{"id":"B_BOT"}`)},
		Channels: []boot.ViewChannelEntry{
			{
				Channel:  boot.Channel{ID: "C_MENTIONED", Name: "mentioned", Version: 1783337533032},
				IsMember: true, LastRead: "1700000003.000300",
				UnreadCount: 4, UnreadCountDisplay: 2,
			},
			{
				Channel:  boot.Channel{ID: "C_MENTIONED_2", Name: "mentioned-two", Version: 1783337533033},
				LastRead: "1700000004.000400",
				Latest:   json.RawMessage(`{"ts":"1700000004.000400"}`),
				// Distinct from each other AND from the entry above:
				// all four unread ints are freely swappable if any two
				// of them share a value.
				UnreadCount: 9, UnreadCountDisplay: 6,
			},
		},
		Emojis: map[string]string{
			"party-parrot": "https://emoji.example.invalid/party-parrot.gif",
			"shipit":       "https://emoji.example.invalid/shipit.png",
		},
		ResponseMetadata: boot.ViewResponseMetadata{NextCursor: "next-cursor-value"},
	}
}

// poisonedViewResult is what the fake returns ALONGSIDE a view error,
// and its Channel.ID is the channel that was ASKED FOR on purpose.
//
// Same reasoning as poisonedCounts, sharpened: a nil result next to the
// error would make "ignore the view error" a mutant that dies on a nil
// dereference, which is a crash rather than a test failure and tells a
// reader nothing. A fully populated result whose channel id MATCHES
// makes the mutant take the success path and copy these messages onto
// the Result, where an assertion can see it. Slack returns a populated
// body next to ok:false, so this is the realistic shape too.
func poisonedViewResult(channelID string) *boot.ViewResult {
	res := cannedViewResult()
	res.Channel.ID = channelID
	res.History.Messages = []json.RawMessage{json.RawMessage(`{"ts":"9999999999.999999","text":"POISON from a failed conversations.view"}`)}
	return res
}

// cannedHistory is the conversations.history fallback's response.
//
// Its messages differ from cannedViewResult's so that "Result.Messages
// was filled from the other path" is a visible failure rather than a
// coincidence, and HasMore is true on BOTH fixtures so that a dropped
// assignment on either path is visible (a false there would be
// indistinguishable from never assigning it).
func cannedHistory() History {
	return History{
		Messages: []json.RawMessage{
			json.RawMessage(`{"ts":"1700000020.000200","user":"U_ALICE","text":"from conversations.history"}`),
		},
		UnchangedTS:   []string{"1700000001.000100", "1700000002.000100"},
		LatestUpdates: map[string]string{"1700000020.000200": "1783024685.163200"},
		HasMore:       true,
	}
}

// poisonedHistory is what the fake returns alongside a history error.
func poisonedHistory() History {
	return History{
		Messages:      []json.RawMessage{json.RawMessage(`{"ts":"9999999999.999999","text":"POISON from a failed conversations.history"}`)},
		UnchangedTS:   []string{"9999999999.999999"},
		LatestUpdates: map[string]string{"9999999999.999999": "9999999999.999999"},
		HasMore:       true,
	}
}

// poisonedVersions is what the fake's MessageVersions returns alongside
// an error: a map that must never reach the wire.
func poisonedVersions() map[string]string {
	return map[string]string{"9999999999.999999": "9999999999.999999"}
}

// rawStrings renders raw message bodies as text for failure messages.
//
// %#v on a []json.RawMessage prints every byte in hex, which turns a
// one-line "wrong path's messages" failure into 40 lines a reader has
// to decode by hand.
func rawStrings(msgs []json.RawMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, string(m))
	}
	return out
}

// fakeDeps is the whole test harness: it records an ordered call log,
// returns the canned responses above, and injects a per-dependency
// error.
//
// It is deliberately the ONLY thing the tests talk to. bootstrap exists
// because connectWorkspace builds a live *slack.Client, so a test that
// needed a server here would have reproduced the problem the package
// was created to solve.
type fakeDeps struct {
	mu    sync.Mutex
	calls []string

	bootRes     *boot.Result
	userBootErr error

	counts    Counts
	countsErr error

	// viewRes is the body conversations.view returns. Tests mutate it
	// in place (HasMore, say) before calling Run.
	viewRes *boot.ViewResult
	// viewChannelID is stamped onto viewRes.Channel.ID at call time:
	// equal to the requested channel means "the server honoured the
	// unverified param", anything else means it did not.
	viewChannelID string
	// viewRequestedChannel is what Run actually sent as the channel
	// param. "" is a real, distinct answer here — it is the captured
	// request — so it cannot be conflated with "not called".
	viewRequestedChannel string
	viewErr              error
	// viewNilResult makes the fake return (nil, nil), which is what a
	// broken adapter looks like from inside Run.
	viewNilResult bool

	history                 History
	historyErr              error
	historyRequestedChannel string
	historyCachedVersions   map[string]string

	// cachedVersions is what the Store hands back for the opened
	// channel, i.e. the messages slk already holds.
	cachedVersions    map[string]string
	cachedVersionsErr error
	// messageVersionsFor records the channel the Store was asked
	// about. Note MessageVersions is NOT recorded in calls: calls is
	// an API-request log that TestRun_BootCallBudget counts against a
	// 10-request budget, and a local cache read is not a request.
	messageVersionsFor   string
	messageVersionsCalls int

	// The revalidation half — canned edge responses, the id maps that
	// reached them, and the cache writes that came back out. Declared
	// in revalidate_test.go so the recording sits next to the
	// assertions that read it.
	revalidateFake

	logs []string

	deps Deps
}

func newFakeDeps() *fakeDeps {
	f := &fakeDeps{
		bootRes: cannedBootResult(),
		counts:  cannedCounts(),
		viewRes: cannedViewResult(),
		history: cannedHistory(),
		revalidateFake: revalidateFake{
			channelVersions: cannedChannelVersions(),
			userVersions:    cannedUserVersions(),
			channelsInfoRes: cannedChannelsInfo(),
			usersInfoRes:    cannedUsersInfo(),
		},
	}
	f.deps = Deps{
		WorkspaceID: "T_HOME",
		Boot:        f,
		Counts:      f,
		View:        f,
		History:     f,
		Revalidate:  f,
		Store:       f,
		Log:         f.log,
	}
	return f
}

// Deps returns the dependency set to hand Run. Tests mutate f.deps
// directly before calling it.
func (f *fakeDeps) Deps() Deps { return f.deps }

func (f *fakeDeps) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeDeps) log(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

func (f *fakeDeps) logged() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.logs...)
}

// called reports whether name was invoked at all.
func (f *fakeDeps) called(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == name {
			return true
		}
	}
	return false
}

// countPrefix counts invocations whose name starts with prefix. Used
// for the conversations.history fan-out guard, where the question is
// "how many" rather than "any at all".
func (f *fakeDeps) countPrefix(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

func (f *fakeDeps) UserBoot(context.Context) (*boot.Result, error) {
	f.record(callUserBoot)
	if f.userBootErr != nil {
		// Mirrors boot.UserBoot, which returns a nil Result on every
		// error path deliberately — a populated one would hand the
		// caller a plausible workspace built from a rejected response.
		return nil, f.userBootErr
	}
	return f.bootRes, nil
}

// poisonedCounts is what the fake returns ALONGSIDE an error.
//
// A zero value there would make "use the counts even though the call
// failed" an equivalent mutant — indistinguishable from the correct
// behaviour, since both leave Result.Counts zero. Returning something
// non-zero and obviously wrong is what makes that mutation killable,
// and the hazard is real: Slack's own endpoints return a fully
// populated body next to ok:false, which is why boot.UserBoot goes out
// of its way to return a nil Result on every error path.
func poisonedCounts() Counts {
	return Counts{
		Unreads: []Unread{{ChannelID: "C_POISON", Count: 999, HasUnread: true, LastRead: "9999999999.999999"}},
		Threads: Threads{HasUnreads: true, UnreadCount: 999, MentionCount: 999},
	}
}

func (f *fakeDeps) Counts(context.Context) (Counts, error) {
	f.record(callCounts)
	if f.countsErr != nil {
		return poisonedCounts(), f.countsErr
	}
	return f.counts, nil
}

func (f *fakeDeps) ConversationsView(_ context.Context, channelID string) (*boot.ViewResult, error) {
	f.record(callView)
	f.mu.Lock()
	f.viewRequestedChannel = channelID
	f.mu.Unlock()
	if f.viewErr != nil {
		return poisonedViewResult(channelID), f.viewErr
	}
	if f.viewNilResult {
		return nil, nil
	}
	// ViewChannel embeds boot.Channel, so .ID resolves through it.
	f.viewRes.Channel.ID = f.viewChannelID
	return f.viewRes, nil
}

func (f *fakeDeps) HistoryWithVersions(_ context.Context, channelID string, cached map[string]string) (History, error) {
	f.record(callHistory)
	f.mu.Lock()
	f.historyRequestedChannel = channelID
	f.historyCachedVersions = cached
	f.mu.Unlock()
	if f.historyErr != nil {
		return poisonedHistory(), f.historyErr
	}
	return f.history, nil
}

func (f *fakeDeps) MessageVersions(channelID string) (map[string]string, error) {
	f.mu.Lock()
	f.messageVersionsFor = channelID
	f.messageVersionsCalls++
	f.mu.Unlock()
	if f.cachedVersionsErr != nil {
		return poisonedVersions(), f.cachedVersionsErr
	}
	return f.cachedVersions, nil
}

// The remaining Store methods, and the whole Revalidator surface, live
// in revalidate_test.go alongside the tests that assert on them.

// prefixEqual reports whether got starts with want.
//
// A prefix rather than an equality check because later tasks append
// steps to the sequence; the ORDER of the first two is the invariant,
// not the total length.
func prefixEqual(got, want []string) bool {
	if len(got) < len(want) {
		return false
	}
	for i, w := range want {
		if got[i] != w {
			return false
		}
	}
	return true
}

func TestRun_CallsUserBootThenCounts(t *testing.T) {
	f := newFakeDeps()
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Order matters: counts is keyed by the conversations userBoot
	// returns, so calling it first would ask about channels we have
	// not learned about yet.
	want := []string{callUserBoot, callCounts}
	if !prefixEqual(f.calls, want) {
		t.Errorf("call sequence = %v; want it to start %v", f.calls, want)
	}
	if res == nil {
		t.Fatal("Run returned a nil Result with a nil error")
	}
	if res.Self.ID != "U_SELF" {
		t.Errorf("Result.Self.ID = %q; want U_SELF", res.Self.ID)
	}
}

func TestRun_NeverEnumerates(t *testing.T) {
	// The regression guard this whole package exists for. slk's
	// Enterprise Grid accounts get signed out for "data scraping",
	// and across 8 captures the official client issues ZERO
	// users.list, ZERO conversations.list, and zero per-channel
	// conversations.history at boot.
	f := newFakeDeps()
	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, forbidden := range []string{"users.list", "conversations.list", "users.conversations"} {
		if f.called(forbidden) {
			t.Errorf("boot called %s; the official client never does, and it is the signature that gets Grid users signed out (sequence: %v)", forbidden, f.calls)
		}
	}
	if n := f.countPrefix("conversations.history"); n > 1 {
		t.Errorf("boot made %d conversations.history calls; at most one (the opened channel's fallback) is allowed, never a per-channel fan-out (sequence: %v)", n, f.calls)
	}
}

func TestRun_BootCallBudget(t *testing.T) {
	// Success criterion 1: a boot issues <= 10 API calls. The fake
	// counts one per dependency invocation, which is the same unit
	// the slackhttp Counter measures.
	f := newFakeDeps()
	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.calls) > 10 {
		t.Errorf("boot issued %d calls; budget is 10 (sequence: %v)", len(f.calls), f.calls)
	}
}

func TestRun_UserBootFailureIsFatal(t *testing.T) {
	// Everything downstream is keyed by what userBoot returns. There
	// is no degraded mode worth having.
	f := newFakeDeps()
	f.userBootErr = errors.New("invalid_auth")
	if _, err := Run(context.Background(), f.Deps()); err == nil {
		t.Fatal("Run returned nil error when userBoot failed")
	}
}

func TestRun_UserBootFailureReturnsNoResult(t *testing.T) {
	// Same reasoning boot.UserBoot's own doc comment gives for
	// returning a nil *Result on every error path: a caller handed
	// both a Result and an error can use the Result, and a
	// half-populated workspace renders as a real one. The error must
	// be the only thing available.
	f := newFakeDeps()
	f.userBootErr = errors.New("invalid_auth")
	res, err := Run(context.Background(), f.Deps())
	if err == nil {
		t.Fatal("Run returned nil error when userBoot failed")
	}
	if res != nil {
		t.Errorf("Run returned a non-nil Result (%+v) alongside error %v; a caller can and will use it", res, err)
	}
}

func TestRun_UserBootErrorWrapsTheCause(t *testing.T) {
	// connectWorkspace distinguishes invalid_auth from every other
	// failure to decide whether to re-prompt for a token. A flattened
	// error string makes that impossible.
	f := newFakeDeps()
	cause := errors.New("invalid_auth")
	f.userBootErr = cause
	_, err := Run(context.Background(), f.Deps())
	if !errors.Is(err, cause) {
		t.Errorf("Run error = %v; want it to wrap %v", err, cause)
	}
}

func TestRun_CountsFailureIsNotFatal(t *testing.T) {
	// Unread badges are cosmetic; a workspace that boots without them
	// is far better than one that does not boot.
	f := newFakeDeps()
	f.countsErr = errors.New("ratelimited")
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: counts failure should not be fatal, got %v", err)
	}
	if res == nil {
		t.Fatal("Run returned nil Result")
	}
	if res.CountsOK {
		t.Error("CountsOK is true after a failed counts call; callers use it to tell 'everything is read' from 'we did not find out', and getting that backwards wipes every unread dot in the sidebar with no data to restore them from")
	}
}

func TestRun_CountsSuccessIsFlagged(t *testing.T) {
	// The other half of the same distinction: an empty Unreads slice
	// from a SUCCESSFUL call legitimately means everything is read,
	// and must be applied.
	f := newFakeDeps()
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.CountsOK {
		t.Error("CountsOK is false after a successful counts call; the caller would skip applying the snapshot and leave last session's dots on screen")
	}
}

func TestRun_CountsFailureDiscardsTheValue(t *testing.T) {
	// The value returned next to an error must not reach the Result.
	// Slack answers ok:false with a fully populated body, so "err !=
	// nil but the value looks fine" is the normal shape of a failure
	// here, not a corner case.
	f := newFakeDeps()
	f.countsErr = errors.New("ratelimited")
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(res.Counts, Counts{}) {
		t.Errorf("Result.Counts = %#v; want the zero value — counts failed and its return value must be discarded", res.Counts)
	}
}

func TestRun_MissingCountsDependencyIsAnError(t *testing.T) {
	// Same reasoning as the Boot check: a forgotten field in the Deps
	// literal is a nil interface, and calling through it panics.
	// Counts is documented required, so a nil one is a wiring bug to
	// report, not an unread-free workspace to render.
	f := newFakeDeps()
	f.deps.Counts = nil
	res, err := Run(context.Background(), f.Deps())
	if err == nil {
		t.Fatal("Run with no Counts dependency returned nil error")
	}
	if res != nil {
		t.Errorf("Run returned a non-nil Result (%+v) alongside error %v", res, err)
	}
}

func TestRun_CountsFailureIsLogged(t *testing.T) {
	// A silently swallowed counts failure is indistinguishable from a
	// workspace with nothing unread, which is the state an operator
	// would be debugging.
	f := newFakeDeps()
	f.countsErr = errors.New("ratelimited")
	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.logged()) == 0 {
		t.Error("counts failed and Run logged nothing")
	}
}

func TestRun_NilLogDoesNotPanic(t *testing.T) {
	// Deps.Log is documented optional. The only path that logs today
	// is the counts failure, so a missing nil-guard is invisible
	// until the day counts fails in production -- which is exactly
	// the day the process must not die.
	f := newFakeDeps()
	f.countsErr = errors.New("ratelimited")
	f.deps.Log = nil
	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_MissingBootDependencyIsAnError(t *testing.T) {
	// Deps is a struct of interfaces built at a call site in
	// cmd/slk/main.go, so a forgotten field is a nil interface and a
	// nil-interface method call is a panic that takes the whole TUI
	// down with a stack trace instead of a message.
	//
	// Every OTHER dependency is populated on purpose. An empty Deps{}
	// would leave Counts nil too, and the Counts guard alone would
	// satisfy this assertion — the test would pass with the Boot check
	// deleted, which is exactly what the mutation run caught it doing.
	f := newFakeDeps()
	f.deps.Boot = nil
	res, err := Run(context.Background(), f.Deps())
	if err == nil {
		t.Fatal("Run with no Boot dependency returned nil error")
	}
	if res != nil {
		t.Errorf("Run returned a non-nil Result (%+v) alongside error %v", res, err)
	}
}

func TestRun_CarriesEveryMappedFieldFromUserBoot(t *testing.T) {
	// Each of these is a field a mutant can drop or source from the
	// wrong place and still compile. The []string and string
	// neighbours in particular -- see cannedBootResult's comment.
	f := newFakeDeps()
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := cannedBootResult()

	for _, tc := range []struct {
		field string
		got   any
		want  any
	}{
		{"Self", res.Self, want.Self},
		{"Team", res.Team, want.Team},
		{"Channels", res.Channels, want.Channels},
		{"IMs", res.IMs, want.IMs},
		{"IsOpen", res.IsOpen, want.IsOpen},
		{"DND", res.DND, want.DND},
		{"ChannelsPriority", res.ChannelsPriority, want.ChannelsPriority},
		{"EmojiCacheTS", res.EmojiCacheTS, want.EmojiCacheTS},
		{"MutePrefsRaw", res.MutePrefsRaw, want.Prefs.AllNotificationsPrefs},
		{"LegacyMutedRaw", res.LegacyMutedRaw, want.Prefs.MutedChannels},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("Result.%s = %#v; want %#v", tc.field, tc.got, tc.want)
		}
	}
}

func TestRun_CarriesCountsOntoTheResult(t *testing.T) {
	// Fetching counts and then dropping them costs a request and
	// delivers nothing -- a failure mode that no other test in this
	// file can see, since they only assert counts was CALLED.
	f := newFakeDeps()
	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(res.Counts, cannedCounts()) {
		t.Errorf("Result.Counts = %#v; want %#v", res.Counts, cannedCounts())
	}
}

func TestRun_OpensTheRequestedChannel(t *testing.T) {
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_WANT" // server honoured the param

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.viewRequestedChannel != "C_WANT" {
		t.Errorf("conversations.view was sent channel=%q; want C_WANT", f.viewRequestedChannel)
	}
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT", res.OpenedChannelID)
	}
	if f.called(callHistory) {
		t.Error("fell back to conversations.history even though view honoured the channel param")
	}
	if len(res.Messages) == 0 {
		t.Error("no messages from conversations.view")
	}
}

func TestRun_ViewPathCarriesEverySection(t *testing.T) {
	// The point of conversations.view is that four sections arrive in
	// ONE response: history, the users it references, the channels it
	// references and the custom emoji it uses. Each is copied onto the
	// Result by its own statement, so each is independently droppable
	// while everything still compiles and every other test still
	// passes — and a dropped Users section means the very users.info
	// fan-out this phase exists to delete comes back at render time.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_WANT"

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := cannedViewResult()
	for _, tc := range []struct {
		field string
		got   any
		want  any
	}{
		{"Messages", rawStrings(res.Messages), rawStrings(want.History.Messages)},
		{"HasMore", res.HasMore, want.History.HasMore},
		{"Users", res.Users, want.Users},
		{"ViewChannels", res.ViewChannels, want.Channels},
		{"Emojis", res.Emojis, want.Emojis},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("Result.%s = %#v; want %#v", tc.field, tc.got, tc.want)
		}
	}
}

func TestRun_FallsBackWhenViewIgnoresTheChannelParam(t *testing.T) {
	// The unverified-param failure mode. The server answers 200 with a
	// perfectly good response for the WRONG conversation. Without the
	// id comparison slk renders someone else's channel and nothing
	// anywhere reports an error.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_LASTVIEWED" // param ignored

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.called(callHistory) {
		t.Error("view returned the wrong channel and Run did not fall back to conversations.history")
	}
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT — the fallback must open the channel that was asked for", res.OpenedChannelID)
	}
	// The other half of the same bug: falling back but still keeping
	// the wrong conversation's messages renders C_LASTVIEWED's history
	// under C_WANT's name, which is the exact outcome the comparison
	// exists to prevent.
	if !reflect.DeepEqual(res.Messages, cannedHistory().Messages) {
		t.Errorf("Result.Messages = %q; want the fallback's %q — the ignored view's messages must not survive", rawStrings(res.Messages), rawStrings(cannedHistory().Messages))
	}
	if f.historyRequestedChannel != "C_WANT" {
		t.Errorf("fallback asked conversations.history for %q; want C_WANT", f.historyRequestedChannel)
	}
}

func TestRun_ViewIgnoringTheChannelParamIsLogged(t *testing.T) {
	// A silent fallback is a Grid-only performance cliff nobody can
	// diagnose: the channel opens correctly, so the only symptom is an
	// extra request per open and a missing users/emoji section.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_LASTVIEWED"

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.logged()) == 0 {
		t.Error("view ignored the channel param and Run logged nothing")
	}
}

func TestRun_FallsBackWhenViewErrors(t *testing.T) {
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: view failure must fall back, not fail the boot: %v", err)
	}
	if !f.called(callHistory) {
		t.Error("view errored and Run did not fall back")
	}
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT", res.OpenedChannelID)
	}
	// The fake answers the error with a fully populated body whose
	// channel id MATCHES, exactly as Slack answers ok:false with a
	// populated body. A caller that checks only the id and not the
	// error would accept it.
	if !reflect.DeepEqual(res.Messages, cannedHistory().Messages) {
		t.Errorf("Result.Messages = %q; want the fallback's %q — a failed view's body must be discarded", rawStrings(res.Messages), rawStrings(cannedHistory().Messages))
	}
}

func TestRun_FallsBackWhenViewReturnsNoResult(t *testing.T) {
	// (nil, nil) is not something Slack can send — boot.ConversationsView
	// returns a nil result only alongside an error — but it IS what a
	// mis-written Task 7 adapter returns, and dereferencing it inside
	// a Bubble Tea program is a stack trace over a torn-down terminal.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewNilResult = true

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !f.called(callHistory) {
		t.Error("view returned no result and Run did not fall back")
	}
	if !reflect.DeepEqual(res.Messages, cannedHistory().Messages) {
		t.Errorf("Result.Messages = %q; want the fallback's %q", rawStrings(res.Messages), rawStrings(cannedHistory().Messages))
	}
}

func TestRun_FallbackCarriesEveryField(t *testing.T) {
	// On Enterprise Grid, where no capture of conversations.view
	// exists at all, this is plausibly the COMMON path. UnchangedTS
	// and LatestUpdates are the incremental-sync bookkeeping: drop
	// LatestUpdates and the next open vouches for nothing and
	// re-downloads the whole window, which is the behaviour this phase
	// removes.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := cannedHistory()
	for _, tc := range []struct {
		field string
		got   any
		want  any
	}{
		{"Messages", rawStrings(res.Messages), rawStrings(want.Messages)},
		{"HasMore", res.HasMore, want.HasMore},
		{"UnchangedTS", res.UnchangedTS, want.UnchangedTS},
		{"LatestUpdates", res.LatestUpdates, want.LatestUpdates},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("Result.%s = %#v; want %#v", tc.field, tc.got, tc.want)
		}
	}
}

func TestRun_FallbackDiscardsTheRejectedViewSections(t *testing.T) {
	// Found by mutation testing: an implementation can fall back
	// correctly for the messages and STILL keep the rejected
	// response's Users, Channels and Emojis, and every other test in
	// this file passes.
	//
	// It is the same bug as rendering the wrong channel's messages,
	// one field over. On the ignored-param path those users are the
	// authors of ANOTHER conversation, so slk would show a member list
	// and author avatars belonging to a channel the user is not
	// looking at — and on the error path they come from a body Slack
	// attached to a failure. A response rejected as untrustworthy has
	// to contribute nothing; picking the parts that look harmless out
	// of it is how "harmless" gets decided by whoever wrote the line.
	for _, tc := range []struct {
		name    string
		prepare func(*fakeDeps)
	}{
		{"channel param ignored", func(f *fakeDeps) { f.viewChannelID = "C_LASTVIEWED" }},
		{"view errored", func(f *fakeDeps) { f.viewErr = errors.New("unknown_method") }},
		{"view returned no result", func(f *fakeDeps) { f.viewNilResult = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.deps.OpenChannelID = "C_WANT"
			tc.prepare(f)

			res, err := Run(context.Background(), f.Deps())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(res.Users) != 0 {
				t.Errorf("Result.Users = %#v; want none — they were parsed out of a response Run rejected", res.Users)
			}
			if len(res.ViewChannels) != 0 {
				t.Errorf("Result.ViewChannels = %#v; want none — they were parsed out of a response Run rejected", res.ViewChannels)
			}
			if len(res.Emojis) != 0 {
				t.Errorf("Result.Emojis = %#v; want none — they were parsed out of a response Run rejected", res.Emojis)
			}
		})
	}
}

func TestRun_CarriesHasMoreFalse(t *testing.T) {
	// The companion to the two has-more assertions above, which both
	// expect true: without a false case, "out.HasMore = true" is a
	// surviving mutant on either path. has_more decides whether the UI
	// offers to page further back, so a hardcoded true means an
	// infinite scrollback on a channel with 3 messages in it.
	for _, tc := range []struct {
		name    string
		prepare func(*fakeDeps)
	}{
		{"view path", func(f *fakeDeps) { f.viewChannelID = "C_WANT"; f.viewRes.History.HasMore = false }},
		{"fallback path", func(f *fakeDeps) { f.viewErr = errors.New("unknown_method"); f.history.HasMore = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.deps.OpenChannelID = "C_WANT"
			tc.prepare(f)

			res, err := Run(context.Background(), f.Deps())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if res.HasMore {
				t.Error("Result.HasMore = true; the response said false")
			}
		})
	}
}

func TestRun_FallbackSendsCachedVersions(t *testing.T) {
	// The fallback is conversations.history WITH cached_latest_updates
	// — the incremental-sync primitive. Falling back to a plain
	// history fetch would re-download scrollback slk already holds,
	// which is the behaviour this phase removes.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")
	f.cachedVersions = map[string]string{"1700000001.000100": "1783024685.163100"}

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.historyCachedVersions) != 1 {
		t.Errorf("history was sent %d cached versions; want the 1 slk holds", len(f.historyCachedVersions))
	}
	if !reflect.DeepEqual(f.historyCachedVersions, f.cachedVersions) {
		t.Errorf("history was sent %#v; want %#v", f.historyCachedVersions, f.cachedVersions)
	}
	if f.messageVersionsFor != "C_WANT" {
		t.Errorf("cached versions were read for channel %q; want C_WANT — another channel's versions vouch for messages this request will not return", f.messageVersionsFor)
	}
}

func TestRun_ViewPathReadsNoCachedVersions(t *testing.T) {
	// conversations.view sends no cached_latest_updates, so reading
	// them on the success path is a cache scan whose result is thrown
	// away — and the Task 7 adapter's read is a bounded SQL query, not
	// a free one.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_WANT"

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.messageVersionsCalls != 0 {
		t.Errorf("view succeeded and Run still read cached message versions %d time(s)", f.messageVersionsCalls)
	}
}

func TestRun_CachedVersionsFailureIsNotFatal(t *testing.T) {
	// An empty map means "we vouch for nothing", which is exactly what
	// the client sends on a cold cache. A cache read failure must
	// degrade to a full window, not to no channel.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")
	f.cachedVersionsErr = errors.New("database is locked")

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: a cached-versions read failure must not fail the boot: %v", err)
	}
	if !f.called(callHistory) {
		t.Error("cached versions failed and Run skipped the history fallback entirely")
	}
	if len(f.historyCachedVersions) != 0 {
		t.Errorf("history was sent %#v; the read FAILED, so its return value must be discarded — vouching for versions we do not hold means the server withholds messages slk never received", f.historyCachedVersions)
	}
	if len(res.Messages) == 0 {
		t.Error("no messages after a degraded fallback")
	}
	if len(f.logged()) == 0 {
		t.Error("the cached-versions read failed and Run logged nothing")
	}
}

func TestRun_ChannelOpenFailureIsNotFatal(t *testing.T) {
	// Both paths to the opened channel have now failed, and the
	// workspace still connects.
	//
	// This REPLACES TestRun_FallbackFailureIsFatal, which asserted the
	// opposite. That test's reasoning was that a nil error next to no
	// messages renders an EMPTY channel, visually identical to a quiet
	// one — true, and not worth the price. The price is the whole
	// workspace: every other conversation, the sidebar, unread state
	// and the user's own identity are independent of this one
	// channel's scrollback, and failing the boot throws all of them
	// away to avoid one ambiguous message pane.
	//
	// The path also matters. conversations.view has never been
	// captured on Enterprise Grid — the environment this phase exists
	// for — so an unknown_method there followed by any history hiccup
	// is exactly how a Grid user would meet this. "This channel looks
	// empty" is a recoverable Tuesday; "slk will not connect" is not.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewErr = errors.New("unknown_method")
	f.historyErr = errors.New("channel_not_found")

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: one channel's scrollback failed to load and the whole workspace refused to connect: %v", err)
	}
	if res == nil {
		t.Fatal("Run returned a nil Result; the workspace cannot be built from it")
	}
	// Usable, not merely non-nil: everything userBoot returned has to
	// survive, or the caller has a Result it cannot render a sidebar
	// from and the nil error is a lie.
	if res.Self.ID != cannedBootResult().Self.ID {
		t.Errorf("Result.Self.ID = %q; want %q — the userBoot fields must survive a failed channel open", res.Self.ID, cannedBootResult().Self.ID)
	}
	if len(res.Channels) != len(cannedBootResult().Channels) {
		t.Errorf("Result.Channels has %d entries; want %d", len(res.Channels), len(cannedBootResult().Channels))
	}
	if !reflect.DeepEqual(res.Counts, cannedCounts()) {
		t.Errorf("Result.Counts = %#v; want %#v — unread state is independent of one channel's history", res.Counts, cannedCounts())
	}
	// Still the channel that was ASKED for. The caller keys its
	// message pane off this, and reporting "" would silently reopen
	// whatever the UI had before.
	if res.OpenedChannelID != "C_WANT" {
		t.Errorf("OpenedChannelID = %q; want C_WANT — the open was attempted and requested that channel", res.OpenedChannelID)
	}
	// The fallback answers its error with a fully populated poisoned
	// body, exactly as Slack answers ok:false with one. Empty here
	// means "no scrollback", which is the honest answer; the poison
	// would be another channel's messages under C_WANT's name.
	if len(res.Messages) != 0 {
		t.Errorf("Result.Messages = %q; want empty — a failed history's body must be discarded, not rendered", rawStrings(res.Messages))
	}
	if len(res.UnchangedTS) != 0 || len(res.LatestUpdates) != 0 {
		t.Errorf("Result.UnchangedTS = %v, LatestUpdates = %v; want both empty — a failed history vouches for nothing", res.UnchangedTS, res.LatestUpdates)
	}
	if res.HasMore {
		t.Error("Result.HasMore is true after a failed open; there is no window to page back from")
	}
	// Non-fatal but not silent. This is the only signal that a Grid
	// workspace is opening every channel empty.
	if len(f.logged()) == 0 {
		t.Error("both paths to the channel failed and Run logged nothing")
	}
}

func TestRun_NoOpenChannelSkipsBothCalls(t *testing.T) {
	// First run on a fresh workspace with nothing restored: there is
	// no channel to open, and inventing one would be an extra request.
	f := newFakeDeps()
	f.deps.OpenChannelID = ""

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if f.called(callView) || f.called(callHistory) {
		t.Errorf("opened a channel with none requested (sequence: %v)", f.calls)
	}
	if res.OpenedChannelID != "" {
		t.Errorf("OpenedChannelID = %q; want empty — nothing was opened", res.OpenedChannelID)
	}
}

func TestRun_OpensTheChannelAfterCounts(t *testing.T) {
	// counts is what tells the UI this channel has unreads, and the
	// history that lands next is what it renders against. Opening
	// first would paint the channel before its badge state exists.
	//
	// The two revalidation calls trail every sequence here, and their
	// position is asserted rather than tolerated: the user set they
	// send is the opened channel's authors, so revalidating before the
	// open would scope users/info to the DM counterparties alone. See
	// TestRevalidate_RunsAfterTheChannelIsOpened, which pins the
	// consequence rather than the order.
	for _, tc := range []struct {
		name    string
		prepare func(*fakeDeps)
		want    []string
	}{
		{"view honoured", func(f *fakeDeps) { f.viewChannelID = "C_WANT" }, []string{callUserBoot, callCounts, callView, callChannelsInfo, callChannelsInfo, callUsersInfo}},
		{"fallback", func(f *fakeDeps) { f.viewChannelID = "C_LASTVIEWED" }, []string{callUserBoot, callCounts, callView, callHistory, callChannelsInfo, callChannelsInfo, callUsersInfo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.deps.OpenChannelID = "C_WANT"
			tc.prepare(f)

			if _, err := Run(context.Background(), f.Deps()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !reflect.DeepEqual(f.calls, tc.want) {
				t.Errorf("call sequence = %v; want %v", f.calls, tc.want)
			}
		})
	}
}

func TestRun_BootCallBudgetWithAChannelOpen(t *testing.T) {
	// Success criterion 1 again, on the path that actually costs
	// requests: TestRun_BootCallBudget runs with no channel to open,
	// so it cannot see either of the calls this task adds. The
	// fallback path is the expensive one — two requests for one
	// channel — and it is the path Enterprise Grid may always take.
	for _, tc := range []struct {
		name    string
		prepare func(*fakeDeps)
	}{
		{"view honoured", func(f *fakeDeps) { f.viewChannelID = "C_WANT" }},
		{"fallback", func(f *fakeDeps) { f.viewChannelID = "C_LASTVIEWED" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.deps.OpenChannelID = "C_WANT"
			tc.prepare(f)

			if _, err := Run(context.Background(), f.Deps()); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(f.calls) > 10 {
				t.Errorf("boot issued %d calls; budget is 10 (sequence: %v)", len(f.calls), f.calls)
			}
		})
	}
}

func TestRun_OpeningAChannelIsStillNotAFanOut(t *testing.T) {
	// TestRun_NeverEnumerates allows at most one conversations.history
	// — the opened channel's fallback. Assert the fallback IS that
	// one, so a belt-and-braces "view AND history" double fetch on the
	// success path cannot hide inside the allowance.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_LASTVIEWED"

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n := f.countPrefix(callHistory); n != 1 {
		t.Errorf("boot made %d conversations.history calls; want exactly 1 (sequence: %v)", n, f.calls)
	}
	if n := f.countPrefix(callView); n != 1 {
		t.Errorf("boot made %d conversations.view calls; want exactly 1 (sequence: %v)", n, f.calls)
	}
}

func TestRun_MissingChannelOpenDependenciesAreErrors(t *testing.T) {
	// Same reasoning as the Boot and Counts guards: these three are
	// nil only because a Deps literal in cmd/slk/main.go forgot a
	// field, and calling through a nil interface panics.
	//
	// All three are checked BEFORE conversations.view is attempted,
	// including Store and History, which the view success path never
	// touches. A boot that works only while the unverified primary
	// path keeps working is the Grid failure this task exists to
	// prevent: the fallback must be wired before it is needed, not
	// discovered missing at the moment it is.
	for _, tc := range []struct {
		name string
		nilf func(*Deps)
	}{
		{"View", func(d *Deps) { d.View = nil }},
		{"History", func(d *Deps) { d.History = nil }},
		{"Store", func(d *Deps) { d.Store = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeDeps()
			f.deps.OpenChannelID = "C_WANT"
			f.viewChannelID = "C_WANT"
			tc.nilf(&f.deps)

			res, err := Run(context.Background(), f.Deps())
			if err == nil {
				t.Fatalf("Run with no %s dependency returned nil error", tc.name)
			}
			if res != nil {
				t.Errorf("Run returned a non-nil Result (%+v) alongside error %v", res, err)
			}
		})
	}
}

func TestRun_MissingChannelOpenDependenciesAreFineWithNoChannel(t *testing.T) {
	// The other half: nothing is opened, so nothing needs them. A
	// guard at the top of Run instead of inside the branch would make
	// a workspace with no restored channel refuse to boot.
	f := newFakeDeps()
	f.deps.OpenChannelID = ""
	f.deps.View, f.deps.History, f.deps.Store = nil, nil, nil

	if _, err := Run(context.Background(), f.Deps()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRun_ChannelOpenDoesNotDisturbTheUserBootFields(t *testing.T) {
	// openChannel writes into the same *Result the userBoot mapping
	// filled. A stray assignment there — or a rebuilt Result — loses
	// the channel list on every boot that opens a channel, i.e. every
	// real one.
	f := newFakeDeps()
	f.deps.OpenChannelID = "C_WANT"
	f.viewChannelID = "C_WANT"

	res, err := Run(context.Background(), f.Deps())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := cannedBootResult()
	if !reflect.DeepEqual(res.Channels, want.Channels) {
		t.Errorf("Result.Channels = %#v; want %#v", res.Channels, want.Channels)
	}
	if !reflect.DeepEqual(res.Counts, cannedCounts()) {
		t.Errorf("Result.Counts = %#v; want %#v", res.Counts, cannedCounts())
	}
}
