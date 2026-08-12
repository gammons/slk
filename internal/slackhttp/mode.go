package slackhttp

// xModeExcludedMethods is the set of API methods whose form bodies the
// official web client sends WITHOUT _x_mode. Every other workspace-API
// body gets _x_mode=online.
//
// Measured across the 2026-07-30 captures: of 163 form-body API
// requests, 149 carry _x_mode and 14 do not. The split is clean
// per-endpoint — zero endpoints are mixed — and these are the seven
// that account for all 14, at n=2 each:
//
//	api.features, client.getWebSocketURL, client.shouldReload,
//	client.userBoot, conversations.view, experiments.getByUser,
//	features.access.policies.list
//
// Two of the seven are corroborated inside this repo without going
// back to the HARs: internal/slack/testdata/phase2-api-contracts.json
// holds two captured client.userBoot samples and two
// conversations.view samples, and none of the four has an _x_mode key,
// while its client.counts, conversations.history and users.prefs.get
// samples all do.
//
// HONEST CAVEAT — what the captures CANNOT tell us. All seven are
// boot-phase calls, and every observation of all seven is at boot
// time. So the data is equally consistent with two different rules:
//
//	(a) these specific endpoints never carry _x_mode; or
//	(b) nothing carries _x_mode until some boot event fires, and these
//	    seven are simply the only endpoints ever called before it.
//
// _x_mode=online reads like a liveness marker, which makes (b) the
// more plausible story — the client plausibly does not consider itself
// "online" until boot completes. But no capture separates them: there
// is no endpoint observed both before and after boot, which is the
// only observation that could. This table encodes (a) because it is
// what was actually measured, and because under (b) it still produces
// the right bytes for every endpoint we have evidence for. If a future
// capture shows one of these seven carrying _x_mode on a post-boot
// call, that settles it for (b) and this becomes a boot-state check on
// Envelope instead of a method lookup.
//
// Lookup is by exact method name, deliberately. A prefix match would
// be wrong in both directions here: "client." would strip _x_mode from
// client.counts, which the captures show sending it 6 times, and
// "conversations." would strip it from conversations.history (14),
// conversations.listPrefs (7) and conversations.bulkReacjiTriggers
// (7).
var xModeExcludedMethods = map[string]struct{}{
	"api.features":                  {},
	"client.getWebSocketURL":        {},
	"client.shouldReload":           {},
	"client.userBoot":               {},
	"conversations.view":            {},
	"experiments.getByUser":         {},
	"features.access.policies.list": {},
}

// sendsXMode reports whether a workspace-API form body for the given
// API method should carry _x_mode. method is the name produced by
// methodFromPath — the same key defaultReason is looked up by.
//
// An unknown method sends _x_mode, matching the 149/163 majority.
//
// The sendsXReason guard makes the captures' one empty cell
// unreachable BY CONSTRUCTION rather than by the two tables happening
// to nest: 0 of the 163 captured form bodies carry _x_mode without
// _x_reason, and "has _x_mode, lacks _x_reason" is the single-predicate
// separator reason.go's defaultReasons table exists to close. Today
// xReasonExcludedMethods is a strict subset of the map above, so the
// guard never fires and is pure redundancy — which is the point. It is
// here so that an edit adding an endpoint to one table and forgetting
// the other cannot put that shape on the wire. Pinned by
// TestSendsXModeImpliesSendsXReason and, at the wire level, by
// TestEnvelopeBody_NeverSendsXModeWithoutXReason.
func sendsXMode(method string) bool {
	if !sendsXReason(method) {
		return false
	}
	_, excluded := xModeExcludedMethods[method]
	return !excluded
}
