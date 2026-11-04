package messages

import (
	"testing"
	"time"
)

func TestFlattenMrkdwn(t *testing.T) {
	resolveUser := func(id string) (string, bool) {
		if id == "U081FJDPAHF" {
			return "ayush", true
		}
		return "", false
	}
	resolveChannel := func(id string) (string, bool) {
		if id == "C123" {
			return "general", true
		}
		return "", false
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"user mention with label", "hi <@U0AQH6B2ULX|Ayush>!", "hi @Ayush!"},
		{"user mention resolved", "hi <@U081FJDPAHF>", "hi @ayush"},
		{"user mention unknown falls back to ID", "hi <@U0AQH6B2ULX>", "hi @U0AQH6B2ULX"},
		{"channel ref with name", "see <#C999|ops>", "see #ops"},
		{"channel ref resolved", "see <#C123>", "see #general"},
		{"channel ref unknown falls back to ID", "see <#C999>", "see #C999"},
		{"link with label", "ticket <https://linear.app/x/issue/ABC-1|ABC-1>", "ticket ABC-1"},
		{"bare link", "see <https://linear.app/x>", "see https://linear.app/x"},
		{"mailto drops scheme", "mail <mailto:a@b.co|a@b.co>", "mail a@b.co"},
		{"http bare", "see <http://example.com/x>", "see http://example.com/x"},
		{"file with label", "open <file:///tmp/a.txt|a.txt>", "open a.txt"},
		{"javascript untouched", "x <javascript:alert(1)>", "x <javascript:alert(1)>"},
		{"here", "<!here> deploy", "@here deploy"},
		{"channel broadcast", "<!channel> deploy", "@channel deploy"},
		{"everyone", "<!everyone> deploy", "@everyone deploy"},
		{"special with label", "<!here|@here> deploy", "@here deploy"},
		{"date token formats local date", "due <!date^1700000000^{date_num}|Nov 14>", "due " + time.Unix(1700000000, 0).Format("2006-01-02")},
		{"usergroup mention with label", "ping <!subteam^S0AB12CD3|@eng> please", "ping @eng please"},
		{"usergroup label without @ gets one", "ping <!subteam^S0AB12CD3|eng>", "ping @eng"},
		{"usergroup mention unlabeled falls back", "ping <!subteam^S0AB12CD3>", "ping @group"},
		{"html entities decoded", "a &amp; b &lt;c&gt;", "a & b <c>"},
		{"plain text untouched", "just words", "just words"},
		{"multiple entities", "<@U081FJDPAHF> in <#C123>: <https://x.co|x>", "@ayush in #general: x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FlattenMrkdwn(tt.in, resolveUser, resolveChannel); got != tt.want {
				t.Errorf("FlattenMrkdwn(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFlattenMrkdwnNilResolvers(t *testing.T) {
	got := FlattenMrkdwn("<@U1> <#C1>", nil, nil)
	if got != "@U1 #C1" {
		t.Errorf("got %q, want %q", got, "@U1 #C1")
	}
}
