package notify

import (
	"testing"
)

func TestShouldNotify_SelfMessage(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnMention:       true,
		OnDM:            true,
	}
	if ShouldNotify(ctx, "C1", "U1", "hello", "dm") {
		t.Error("should not notify for self-messages")
	}
}

func TestShouldNotify_DM(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnDM:            true,
	}
	if !ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should notify for DM")
	}
	if !ShouldNotify(ctx, "C1", "U2", "hello", "group_dm") {
		t.Error("should notify for group DM")
	}
}

func TestShouldNotify_DM_Disabled(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnDM:            false,
	}
	if ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should not notify for DM when OnDM is false")
	}
}

func TestShouldNotify_Mention(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnMention:       true,
	}
	if !ShouldNotify(ctx, "C1", "U2", "hey <@U1> check this", "channel") {
		t.Error("should notify for mention")
	}
	if ShouldNotify(ctx, "C1", "U2", "hey <@U3> check this", "channel") {
		t.Error("should not notify for mention of another user")
	}
}

func TestShouldNotify_Mention_Disabled(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnMention:       false,
	}
	if ShouldNotify(ctx, "C1", "U2", "hey <@U1> check this", "channel") {
		t.Error("should not notify for mention when OnMention is false")
	}
}

func TestShouldNotify_Keyword(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C_OTHER",
		IsActiveWS:      true,
		OnKeyword:       []string{"deploy", "incident"},
	}
	if !ShouldNotify(ctx, "C1", "U2", "starting deploy now", "channel") {
		t.Error("should notify for keyword match")
	}
	if !ShouldNotify(ctx, "C1", "U2", "DEPLOY is done", "channel") {
		t.Error("should notify for case-insensitive keyword match")
	}
	if ShouldNotify(ctx, "C1", "U2", "nothing relevant", "channel") {
		t.Error("should not notify when no keyword matches")
	}
}

func TestShouldNotify_ActiveChannel_Suppressed(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C1",
		IsActiveWS:      true,
		OnDM:            true,
	}
	if ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should suppress notification for active channel")
	}
}

func TestShouldNotify_InactiveWorkspace_NotSuppressed(t *testing.T) {
	ctx := NotifyContext{
		CurrentUserID:   "U1",
		ActiveChannelID: "C1",
		IsActiveWS:      false,
		OnDM:            true,
	}
	if !ShouldNotify(ctx, "C1", "U2", "hello", "dm") {
		t.Error("should notify when workspace is inactive even if channel ID matches")
	}
}

func TestStripSlackMarkup(t *testing.T) {
	userNames := map[string]string{
		"U123": "Alice",
		"U456": "Bob",
	}
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain text", "hello world", "hello world"},
		{"known user mention", "hey <@U123>", "hey @Alice"},
		{"unknown user mention falls back to ID", "hey <@U999>", "hey @U999"},
		{"multiple user mentions", "<@U123> and <@U456>", "@Alice and @Bob"},
		{"channel mention", "see <#C123|general>", "see #general"},
		{"link with label", "visit <https://example.com|Example>", "visit Example"},
		{"bare link", "visit <https://example.com>", "visit https://example.com"},
		{"broadcast here", "<!here> heads up", "@here heads up"},
		{"broadcast channel", "<!channel> heads up", "@channel heads up"},
		{"broadcast everyone", "<!everyone> heads up", "@everyone heads up"},
		{"subteam mention", "ping <!subteam^S123|@platform> please", "ping @platform please"},
		{"markup chars stripped", "*bold* and _italic_ and ~strike~", "bold and italic and strike"},
		{"code", "`code`", "code"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripSlackMarkup(tt.input, userNames)
			if result != tt.expected {
				t.Errorf("StripSlackMarkup(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStripSlackMarkup_NilUserNames(t *testing.T) {
	// Nil map should not panic; mentions fall back to user ID.
	result := StripSlackMarkup("hi <@U123>", nil)
	if result != "hi @U123" {
		t.Errorf("got %q, want %q", result, "hi @U123")
	}
}

func TestStripSlackMarkup_Truncation(t *testing.T) {
	long := ""
	for i := 0; i < 120; i++ {
		long += "a"
	}
	result := StripSlackMarkup(long, nil)
	if len(result) > 103 {
		t.Errorf("expected truncation, got length %d", len(result))
	}
	if result[len(result)-3:] != "..." {
		t.Error("expected ... suffix")
	}
}
