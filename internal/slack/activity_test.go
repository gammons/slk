package slackclient

import (
	"os"
	"path/filepath"
	"testing"
)

func loadActivityFixture(t *testing.T) ActivityFeedResult {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "activity_feed.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	res, err := parseActivityFeed(body)
	if err != nil {
		t.Fatalf("parseActivityFeed: %v", err)
	}
	return res
}

func TestParseActivityFeed_Counts(t *testing.T) {
	res := loadActivityFixture(t)

	if got, want := len(res.Items), 30; got != want {
		t.Fatalf("item count = %d, want %d", got, want)
	}
	if res.NextCursor == "" {
		t.Error("NextCursor is empty; want the fixture's next_cursor")
	}

	counts := map[string]int{}
	for _, it := range res.Items {
		counts[it.Type]++
	}
	want := map[string]int{
		"thread_v2":        5,
		"at_user":          6,
		"at_channel":       6,
		"dm":               4,
		"bot_dm_bundle":    2,
		"message_reaction": 6,
		"at_user_group":    1,
	}
	for typ, n := range want {
		if counts[typ] != n {
			t.Errorf("type %q count = %d, want %d", typ, counts[typ], n)
		}
	}
}

func TestParseActivityFeed_ChannelIDPresent(t *testing.T) {
	res := loadActivityFixture(t)
	for _, it := range res.Items {
		if it.ChannelID == "" {
			t.Errorf("item key=%q type=%q has empty ChannelID", it.Key, it.Type)
		}
		if it.Key == "" {
			t.Errorf("item type=%q has empty Key", it.Type)
		}
		if it.FeedTS == "" {
			t.Errorf("item key=%q has empty FeedTS", it.Key)
		}
	}
}

func TestParseActivityFeed_PerTypeExtraction(t *testing.T) {
	res := loadActivityFixture(t)

	var sawReaction, sawThread, sawMention, sawDM, sawBotDM bool
	for _, it := range res.Items {
		switch it.Type {
		case "message_reaction":
			sawReaction = true
			if it.Reaction == "" {
				t.Errorf("message_reaction key=%q has empty Reaction", it.Key)
			}
			if it.TS == "" {
				t.Errorf("message_reaction key=%q has empty TS", it.Key)
			}
		case "thread_v2":
			sawThread = true
			if it.ThreadTS == "" {
				t.Errorf("thread_v2 key=%q has empty ThreadTS", it.Key)
			}
			if it.TS == "" {
				t.Errorf("thread_v2 key=%q has empty TS (latest_ts)", it.Key)
			}
		case "at_user", "at_channel", "at_user_group":
			sawMention = true
			if it.AuthorID == "" {
				t.Errorf("mention key=%q has empty AuthorID", it.Key)
			}
			if it.TS == "" {
				t.Errorf("mention key=%q has empty TS", it.Key)
			}
		case "dm":
			sawDM = true
			if it.TS == "" {
				t.Errorf("dm key=%q has empty TS", it.Key)
			}
		case "bot_dm_bundle":
			sawBotDM = true
			if it.TS == "" {
				t.Errorf("bot_dm_bundle key=%q has empty TS", it.Key)
			}
			if !it.IsBot {
				t.Errorf("bot_dm_bundle key=%q expected IsBot=true", it.Key)
			}
		}
	}

	if !sawReaction {
		t.Error("no message_reaction item seen")
	}
	if !sawThread {
		t.Error("no thread_v2 item seen")
	}
	if !sawMention {
		t.Error("no mention item seen")
	}
	if !sawDM {
		t.Error("no dm item seen")
	}
	if !sawBotDM {
		t.Error("no bot_dm_bundle item seen")
	}
}

// A single malformed item (Slack internal endpoints occasionally return
// an unexpected JSON type for a field) must be skipped, not fail the
// whole page — the good items around it still parse.
func TestParseActivityFeed_SkipsMalformedItem(t *testing.T) {
	body := []byte(`{"ok":true,"items":[
		{"is_unread":true,"feed_ts":"1.1","key":"good-1","item":{"type":"at_user","message":{"channel":"C1","ts":"1.1","author_user_id":"U1"}}},
		{"is_unread":false,"feed_ts":"2.2","key":"bad","item":{"type":"at_user","message":false}},
		{"is_unread":true,"feed_ts":"3.3","key":"good-2","item":{"type":"dm","bundle_info":{"payload":{"dm_entry":{"latest_message":{"channel":"C2","ts":"3.3"}}}}}}
	],"response_metadata":{"next_cursor":"cur"}}`)

	res, err := parseActivityFeed(body)
	if err != nil {
		t.Fatalf("parseActivityFeed should not error on one malformed item: %v", err)
	}
	if got, want := len(res.Items), 2; got != want {
		t.Fatalf("expected 2 surviving items, got %d", got)
	}
	if res.Items[0].Key != "good-1" || res.Items[1].Key != "good-2" {
		t.Fatalf("surviving items in wrong order/identity: %q, %q", res.Items[0].Key, res.Items[1].Key)
	}
	if res.NextCursor != "cur" {
		t.Fatalf("next cursor lost: %q", res.NextCursor)
	}
}
