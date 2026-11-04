package core

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseHuddleRoom_NumberAndStringDates(t *testing.T) {
	for name, raw := range map[string]string{
		"numbers": `{"created_by":"UFAKE0001","date_start":1700000000,"date_end":1700000600,"has_ended":true}`,
		"strings": `{"created_by":"UFAKE0001","date_start":"1700000000","date_end":"1700000600.25","has_ended":"true"}`,
	} {
		r := ParseHuddleRoom(json.RawMessage(raw))
		if r == nil || r.CreatedBy != "UFAKE0001" || r.DateStart != 1700000000 || r.DateEnd != 1700000600 || !r.HasEnded {
			t.Errorf("%s: got %+v", name, r)
		}
		if string(r.Raw) != raw {
			t.Errorf("%s: Raw not verbatim", name)
		}
	}
}

func TestParseHuddleRoom_LenientOnBadInput(t *testing.T) {
	for _, raw := range []string{``, `null`, `"x"`, `[1]`, `{bad`} {
		if r := ParseHuddleRoom(json.RawMessage(raw)); r != nil {
			t.Errorf("%q: got %+v, want nil", raw, r)
		}
	}
	r := ParseHuddleRoom(json.RawMessage(`{"created_by":5,"date_start":{"x":1},"date_end":"soon","has_ended":"maybe"}`))
	if r == nil || r.CreatedBy != "" || r.DateStart != 0 || r.DateEnd != 0 || r.HasEnded {
		t.Errorf("got %+v, want zero values", r)
	}
	if r := ParseHuddleRoom(json.RawMessage(`{}`)); r == nil {
		t.Error("empty object must still parse")
	}
}

// The participant field names are unverified guesses; these fixtures are
// synthetic.
func TestParseHuddleRoom_Participants(t *testing.T) {
	cases := map[string]struct {
		raw  string
		want []string
	}{
		"strings":      {`{"participants":["UFAKE0001","UFAKE0002"]}`, []string{"UFAKE0001", "UFAKE0002"}},
		"objects":      {`{"participant_history":[{"user":"UFAKE0001"},{"user_id":"UFAKE0002"},{"id":"UFAKE0003"},{"n":1}]}`, []string{"UFAKE0001", "UFAKE0002", "UFAKE0003"}},
		"attendees":    {`{"attendees":["UFAKE0004"]}`, []string{"UFAKE0004"}},
		"first wins":   {`{"attendees":["UFAKE0004"],"participants":["UFAKE0001"]}`, []string{"UFAKE0001"}},
		"not an array": {`{"participants":"UFAKE0001"}`, nil},
		"absent":       {`{"created_by":"UFAKE0001"}`, nil},
	}
	for name, c := range cases {
		r := ParseHuddleRoom(json.RawMessage(c.raw))
		if r == nil || !reflect.DeepEqual(r.Participants, c.want) {
			t.Errorf("%s: got %+v, want %v", name, r, c.want)
		}
	}
}
