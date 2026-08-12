package slackhttp

import (
	"context"
	"slices"
	"testing"
)

func TestReasonRoundTrip(t *testing.T) {
	ctx := WithReason(context.Background(), "message-pane/requestHistory")
	if got := ReasonFrom(ctx); got != "message-pane/requestHistory" {
		t.Errorf("ReasonFrom = %q; want message-pane/requestHistory", got)
	}
}

func TestReasonDefaultsWhenAbsent(t *testing.T) {
	if got := ReasonFrom(context.Background()); got != "" {
		t.Errorf("ReasonFrom(empty ctx) = %q; want \"\"", got)
	}
}

func TestReasonIgnoresEmpty(t *testing.T) {
	ctx := WithReason(context.Background(), "")
	if got := ReasonFrom(ctx); got != "" {
		t.Errorf("ReasonFrom = %q; want \"\"", got)
	}
}

func TestReasonNilContext(t *testing.T) {
	// Defensive: some call sites in this codebase pass a nil ctx.
	//nolint:staticcheck // SA1012: passing nil is the behaviour under test.
	if got := ReasonFrom(nil); got != "" {
		t.Errorf("ReasonFrom(nil) = %q; want \"\"", got)
	}
}

func TestReasonEmptyDoesNotClobberOuter(t *testing.T) {
	// TestReasonIgnoresEmpty cannot tell "did not store" from "stored an
	// empty string" — both read back as "". This can: a caller that
	// derives a reason and comes up empty must leave an outer reason
	// intact rather than blanking it.
	ctx := WithReason(context.Background(), "message-pane/requestHistory")
	ctx = WithReason(ctx, "")
	if got := ReasonFrom(ctx); got != "message-pane/requestHistory" {
		t.Errorf("WithReason(ctx, \"\") clobbered the outer reason: %q", got)
	}
}

func TestReasonInnermostWins(t *testing.T) {
	ctx := WithReason(context.Background(), "outer")
	ctx = WithReason(ctx, "inner")
	if got := ReasonFrom(ctx); got != "inner" {
		t.Errorf("ReasonFrom = %q; want inner", got)
	}
}

func TestDefaultReasonMatchesCapture(t *testing.T) {
	// Each pair was read off the 2026-07-30 captures of the official
	// web client. These are the endpoints slk actually calls; the
	// values are what the real client tags them with.
	cases := map[string]string{
		"client.userBoot":            "initial-data",
		"client.shouldReload":        "boot",
		"client.counts":              "fetchClientCountsOnConnect",
		"conversations.history":      "message-pane/requestHistory",
		"conversations.mark":         "viewed",
		"conversations.genericInfo":  "fallback:fetchAndUpsertChannelsById",
		"users.prefs.get":            "fetch-frecency-prefs",
		"users.channelSections.list": "conditional-fetch-manager",
		"dnd.info":                   "fetchAndUpsertDndForUsers-getDndTimesFor:self",
	}
	for method, want := range cases {
		t.Run(method, func(t *testing.T) {
			if got := defaultReason(method); got != want {
				t.Errorf("defaultReason(%q) = %q; want %q", method, got, want)
			}
		})
	}
}

func TestDefaultReasonForUnmappedMethodIsNonEmpty(t *testing.T) {
	// Emitting NOTHING is the separator this exists to remove: a body
	// carrying _x_mode but no _x_reason matches ~6% of the official
	// client's traffic and would match 100% of slk's. A plausible but
	// unverified value is strictly better than a structurally absent
	// field.
	for _, method := range []string{"chat.postMessage", "reactions.add", "", "some.unknown.method"} {
		if got := defaultReason(method); got == "" {
			t.Errorf("defaultReason(%q) = \"\"; want a non-empty fallback", method)
		}
	}
}

// TestGenericReasonIsAValueTheOfficialClientSends pins the fallback's
// VALUE, which for a long time only had its presence pinned.
//
// TestDefaultReasonForUnmappedMethodIsNonEmpty above rejects "" and
// nothing else. That is not enough: genericReason covers every
// endpoint absent from defaultReasons, which is most of slk's traffic,
// so it is the single string that appears on the most requests slk
// makes. A reviewer set it to "slk-tui-fetch" — an obviously
// slk-specific value appearing zero times in the captures — and the
// entire suite passed.
//
// That is the exact failure reason.go's own comment argues against: a
// wrong-but-attested reason keeps slk inside the 94% of requests that
// carry the field, whereas an invented one is a signature on nearly
// every request. The nine values in defaultReasons are each pinned to
// their endpoint by TestDefaultReasonMatchesCapture; this pins the
// tenth.
//
// Two assertions, not one, because they fail differently. The literal
// says WHICH observed value the fallback is, so swapping it for
// another real one is a deliberate edit rather than a silent drift.
// Membership in the fixture's observed set says it is a string the
// official client actually sends — which is the property that matters
// and the one a hand-written literal cannot establish on its own.
func TestGenericReasonIsAValueTheOfficialClientSends(t *testing.T) {
	// The literal. reason.go's comment names this as the value the
	// captures show on users.channelSections.list, and the pairing
	// with other endpoints is an honest guess documented there — but
	// the STRING is not a guess, and this is what says so.
	if genericReason != "conditional-fetch-manager" {
		t.Errorf("genericReason = %q; want %q", genericReason, "conditional-fetch-manager")
	}

	// And it must be a reason the official client was actually
	// observed sending. This is the assertion that would have caught
	// "slk-tui-fetch".
	shape := loadRequestShape(t)
	observed := shape.WorkspaceAPI.XReasonObservedValues
	if !slices.Contains(observed, genericReason) {
		t.Errorf("genericReason = %q, which appears in NONE of the %d _x_reason values observed across the captures.\n"+
			"An slk-invented reason is a per-request signature on every endpoint not in defaultReasons — the opposite of what this table exists to do.\n"+
			"Pick a value from testdata/official-request-shape.json's x_reason_observed_values.",
			genericReason, len(observed))
	}

	// The same property for the nine table entries, which are pinned
	// to their endpoints elsewhere but never checked against the
	// observed set. A future entry invented rather than measured fails
	// here.
	for method, reason := range defaultReasons {
		if !slices.Contains(observed, reason) {
			t.Errorf("defaultReasons[%q] = %q, which was never observed in the captures", method, reason)
		}
	}

	// Guard against the fixture list silently shrinking to something
	// that makes the assertions above easy to satisfy. 48 distinct
	// values over 153 requests is what x_reason_evidence records.
	if len(observed) != 48 {
		t.Errorf("x_reason_observed_values has %d entries; want 48, matching x_reason_evidence's count in the same file", len(observed))
	}
}

func TestMethodFromPath(t *testing.T) {
	cases := map[string]string{
		"/api/conversations.history": "conversations.history",
		"/api/client.counts":         "client.counts",
		"/api/":                      "",
		"/files-tmb/x.png":           "",
		"":                           "",
		// Slack API paths are /api/<method> with nothing after, but a
		// trailing segment must not become part of the method name.
		"/api/conversations.history/extra": "conversations.history",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := methodFromPath(path); got != want {
				t.Errorf("methodFromPath(%q) = %q; want %q", path, got, want)
			}
		})
	}
}

func TestReasonDoesNotCollideWithOtherKeys(t *testing.T) {
	// The context key must be an unexported struct type, not a string,
	// so an unrelated package storing ctx.Value("reason") cannot be
	// mistaken for ours.
	ctx := context.WithValue(context.Background(), "reason", "not-ours") //nolint:staticcheck // deliberate
	if got := ReasonFrom(ctx); got != "" {
		t.Errorf("ReasonFrom picked up a foreign string key: %q", got)
	}
}
