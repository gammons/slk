package messages

import "testing"

func TestCountUserRefs(t *testing.T) {
	msgs := []MessageItem{
		{UserID: "U1", Text: "hello"},
		{UserID: "U2", Text: "hi <@U1>"},
		{UserID: "U1", Text: "cc <@U1|alice> and <@U3>"},
		{UserID: "U3", Text: "<@U12> is someone else"},
	}
	cases := []struct {
		userID              string
		authored, mentioned int
	}{
		{"U1", 2, 2}, // <@U12> is not a mention of U1
		{"U2", 1, 0},
		{"U3", 1, 1},
		{"U9", 0, 0},
		{"", 0, 0},
	}
	for _, c := range cases {
		a, m := CountUserRefs(msgs, c.userID)
		if a != c.authored || m != c.mentioned {
			t.Errorf("CountUserRefs(%q) = (%d, %d); want (%d, %d)", c.userID, a, m, c.authored, c.mentioned)
		}
	}
}
