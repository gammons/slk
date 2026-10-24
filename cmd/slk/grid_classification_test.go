package main

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

// TestGridPerWorkspaceFrames_ClassifiesEveryDispatchedType is the
// guard against the denylist drifting from the dispatcher: every frame
// type internal/slack/events.go dispatches is listed here exactly
// once, sorted into per-workspace (its handler writes WorkspaceContext
// or workspace-keyed rows, so it must run on every grid session) or
// shared (channel- or user-keyed state only, safe to dedupe). A new
// event type fails this test until someone sorts it - a deliberate
// choice rather than a silent default. Keep the list in sync with the
// case labels in events.go.
func TestGridPerWorkspaceFrames_ClassifiesEveryDispatchedType(t *testing.T) {
	perWorkspace := []string{
		"pref_change",
		"dnd_updated", "dnd_updated_user",
		"manual_presence_change",
		"channel_section_upserted", "channel_section_deleted",
		"channel_sections_channels_upserted", "channel_sections_channels_removed",
		"mpim_open", "im_created", "im_open", "group_joined", "channel_joined",
		"thread_subscribed", "thread_unsubscribed", "thread_marked",
		"member_joined_channel", "member_left_channel",
		"user_invalidated", "dnd_invalidated",
	}
	shared := []string{
		"ai_assistant_status",
		"channel_marked", "im_marked", "group_marked", "mpim_marked",
		"message",
		"presence_change",
		"reaction_added", "reaction_removed",
		"user_change", "user_status_changed",
		"view_opened",
	}
	// Connection-exempt: never deduped for reasons other than state
	// (per-connection frames, the typing indicator's repeat cadence,
	// frames that fail to parse - the last not a type at all).
	connectionExempt := []string{"hello", "reconnect_url", "user_typing"}

	// No type may be sorted twice.
	seen := map[string]bool{}
	for _, typ := range append(append(append([]string(nil), perWorkspace...), shared...), connectionExempt...) {
		if seen[typ] {
			t.Errorf("type %q listed twice in the classification", typ)
		}
		seen[typ] = true
	}

	// The classification must cover every type the dispatcher actually
	// routes: read the case labels out of events.go and require each
	// to be sorted. message_changed and message_deleted are subtypes
	// of message, not frame types, so they are absent here on purpose.
	// Labels that appear at one-tab depth OUTSIDE the dispatch switch:
	// MessageFrameDrivesOnMessage mirrors the message arm's subtype
	// rules in its own function-level switch, so its cases match the
	// regexp too. They are subtypes of message, not frame types.
	mirrored := map[string]bool{
		"message_changed": true, "message_deleted": true,
		"bot_message": true, "thread_broadcast": true,
		"file_share": true, "me_message": true,
	}
	dispatched := dispatchedEventTypes(t)
	for _, typ := range dispatched {
		if !seen[typ] && !mirrored[typ] {
			t.Errorf("events.go dispatches %q but the classification does not sort it; add it to per-workspace, shared, or the subtype exception list", typ)
		}
	}

	// The set under test must contain exactly the per-workspace types.
	got := make([]string, 0, len(gridPerWorkspaceFrames))
	for typ := range gridPerWorkspaceFrames {
		got = append(got, typ)
	}
	sort.Strings(got)
	want := append([]string(nil), perWorkspace...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("gridPerWorkspaceFrames has %d types; the classification lists %d.\nextra: %v\nmissing: %v",
			len(got), len(want), diff(got, want), diff(want, got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("gridPerWorkspaceFrames = %v; want exactly %v", got, want)
			break
		}
	}

	// None of the shared or connection-exempt types may leak into the
	// denylist.
	for _, typ := range append(shared, connectionExempt...) {
		if gridPerWorkspaceFrames[typ] {
			t.Errorf("type %q is classified shared but present in gridPerWorkspaceFrames", typ)
		}
	}
}

// dispatchedEventTypes extracts the frame-type case labels from
// internal/slack/events.go: every label of every `case` clause at the
// head of a dispatch arm, including the later labels of multi-label
// clauses (`case "user_change", "user_status_changed":`). Subtype
// labels inside the message arm (bot_message, file_share, me_message,
// thread_broadcast) are excluded by the one-tab indentation.
func dispatchedEventTypes(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile("../../internal/slack/events.go")
	if err != nil {
		t.Fatalf("reading events.go: %v", err)
	}
	// One tab of indentation: the outer dispatch switch's cases. The
	// message arm's subtype switch sits deeper, so its labels
	// (message_changed, message_deleted, bot_message, ...) are not
	// frame types and stay out of the classification. Group 1 is the
	// whole label list so every label of a multi-label clause is
	// captured, not just the first.
	re := regexp.MustCompile(`(?m)^\tcase ((?:"[a-z_]+"(?:, )?)+):`)
	labels := regexp.MustCompile(`"([a-z_]+)"`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		for _, lm := range labels.FindAllStringSubmatch(m[1], -1) {
			typ := lm[1]
			if !seen[typ] {
				seen[typ] = true
				out = append(out, typ)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no case labels found in events.go; the regexp drifted from the file")
	}
	return out
}

func diff(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}
