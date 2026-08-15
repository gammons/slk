package slackfmt

import "testing"

func TestStripMarkup_SubteamResolution(t *testing.T) {
	// bare <!subteam^ID> resolves via the usergroup map
	if got := StripMarkupWithUserGroups("hi <!subteam^S123>", nil, map[string]string{"S123": "eng-team"}); got != "hi @eng-team" {
		t.Errorf("bare subteam: got %q", got)
	}
	// labeled form uses the embedded label
	if got := StripMarkup("hi <!subteam^S999|design>", nil); got != "hi @design" {
		t.Errorf("labeled subteam: got %q", got)
	}
	// bare with no map falls back to @group
	if got := StripMarkup("hi <!subteam^S123>", nil); got != "hi @group" {
		t.Errorf("unresolved subteam: got %q", got)
	}
}
