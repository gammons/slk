package slackclient

import (
	"reflect"
	"testing"
)

// TestSlackAPI_DeclaresNoWorkspaceEnumeration pins the deletions that
// stop slk's call pattern looking like a scraper.
//
// SlackAPI is the only route from slk into slack-go — Client.api is
// that interface and nothing else constructs a slack.Client outside
// this package — so a method absent from it cannot be called at all.
// That makes this a stronger guard than counting calls in a fake: it
// fails at the point someone re-adds the capability, not at the point
// they use it.
//
// Each entry names the endpoint and why it is gone, because the
// deletions look like lost functionality to anyone who did not measure
// them. The replacements are on the map values.
func TestSlackAPI_DeclaresNoWorkspaceEnumeration(t *testing.T) {
	forbidden := map[string]string{
		"GetUsersContext": "users.list, ~50 paginated pages on a 10k-user workspace " +
			"and zero occurrences across all 8 captures of the official web client. " +
			"Users now arrive from conversations.view's users array (the opened " +
			"channel's authors), edge.UsersInfo revalidation of the cache, and " +
			"on-demand users.info via resolveUser for the misses.",
		"GetUsers": "users.list; see GetUsersContext",
		"GetConversations": "conversations.list, which walked every public channel in the " +
			"workspace at Limit: 1000 -- 4 requests on a measured two-workspace boot, " +
			"growing with the workspace, run in the background whether or not the user " +
			"ever opened the channel finder. The finder now asks edgeapi's " +
			"channels/search for the query the user actually typed, debounced. Note " +
			"GetConversationsForUser (users.conversations, the joined list) is a " +
			"different endpoint and is still here.",
	}

	iface := reflect.TypeOf((*SlackAPI)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatal("SlackAPI has no methods; this test is reflecting on the wrong type")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if why, bad := forbidden[name]; bad {
			t.Errorf("SlackAPI declares %s, which issues %s", name, why)
		}
	}
}
