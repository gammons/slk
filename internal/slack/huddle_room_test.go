package slackclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/slackhttp"
)

const fakeRoom = `{"created_by":"UFAKE0001","date_start":1700000000,"date_end":0,"has_ended":false}`

// roomTestClient serves body for every API call and returns a client whose
// room tap is in place (pointClientAtTestServer swaps BrowserTransport.Inner,
// so the tap is re-wrapped around the test transport).
func roomTestClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("xoxc-test", "d-cookie")
	bt := c.httpClient.Transport.(*slackhttp.BrowserTransport)
	addr := srv.Listener.Addr().String()
	// slack-go's typed calls use https://slack.com/api/; dial the test
	// server instead, keeping the tap NewClient installed around it.
	bt.Inner = c.rooms.wrap(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	})
	return c
}

func TestRoomTap_HistoryCapturesHuddleRoomOnly(t *testing.T) {
	c := roomTestClient(t, `{"ok":true,"messages":[
		{"type":"message","subtype":"huddle_thread","ts":"1.000100","room":`+fakeRoom+`},
		{"type":"message","ts":"1.000200","text":"hi","room":`+fakeRoom+`},
		{"type":"message","subtype":"huddle_thread","ts":"1.000300"},
		{"type":"message","subtype":"huddle_thread","ts":"1.000400","room":"oops"}]}`)
	if _, err := c.GetHistory(context.Background(), "CFAKE0001", 50, ""); err != nil {
		t.Fatal(err)
	}
	if got := string(c.HuddleRoom("CFAKE0001", "1.000100")); got != fakeRoom {
		t.Errorf("room = %s, want %s", got, fakeRoom)
	}
	for _, ts := range []string{"1.000200", "1.000300", "1.000400", "9.9"} {
		if got := c.HuddleRoom("CFAKE0001", ts); got != nil {
			t.Errorf("ts %s: room = %s, want none", ts, got)
		}
	}
}

func TestRoomTap_RepliesCapturesHuddleRoom(t *testing.T) {
	c := roomTestClient(t, `{"ok":true,"has_more":false,"messages":[
		{"type":"message","subtype":"huddle_thread","ts":"2.000100","thread_ts":"2.000100","room":`+fakeRoom+`}]}`)
	if _, err := c.GetReplies(context.Background(), "CFAKE0001", "2.000100"); err != nil {
		t.Fatal(err)
	}
	if got := string(c.HuddleRoom("CFAKE0001", "2.000100")); got != fakeRoom {
		t.Errorf("room = %s, want %s", got, fakeRoom)
	}
}

func TestRoomTap_MalformedBodyPassesThrough(t *testing.T) {
	s := newRoomStore()
	rt := s.wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: nopBody("not json")}, nil
	}))
	req, _ := http.NewRequest("POST", "https://slack.com/api/conversations.history", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 20)
	n, _ := resp.Body.Read(b)
	if string(b[:n]) != "not json" {
		t.Errorf("body = %q, want it handed on unchanged", b[:n])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func nopBody(s string) *nopCloser { return &nopCloser{strings.NewReader(s)} }

type nopCloser struct{ *strings.Reader }

func (*nopCloser) Close() error { return nil }

func TestRoomStore_Bounded(t *testing.T) {
	s := newRoomStore()
	for i := 0; i < roomStoreMax+10; i++ {
		s.put("C", string(rune('a'+i%26))+strings.Repeat("x", i), json.RawMessage(`{}`))
	}
	if len(s.rooms) > roomStoreMax {
		t.Errorf("store holds %d rooms, want <= %d", len(s.rooms), roomStoreMax)
	}
}

type roomMock struct {
	mockEventHandler
	rooms []string
}

func (m *roomMock) OnMessageRoom(channelID, ts string, room json.RawMessage) {
	m.rooms = append(m.rooms, channelID+"|"+ts+"|"+string(room))
}

func TestDispatch_MessageFrameCarriesRoomToHandler(t *testing.T) {
	h := &roomMock{}
	dispatchWebSocketEvent([]byte(`{"type":"message","subtype":"huddle_thread","channel":"CFAKE0001","ts":"3.1","room":`+fakeRoom+`}`), h)
	if len(h.rooms) != 1 || h.rooms[0] != "CFAKE0001|3.1|"+fakeRoom {
		t.Errorf("rooms = %v", h.rooms)
	}
	if len(h.messages) != 1 {
		t.Errorf("OnMessage calls = %d, want 1", len(h.messages))
	}
	// An edit (message_changed) carries the room on the inner message.
	h = &roomMock{}
	dispatchWebSocketEvent([]byte(`{"type":"message","subtype":"message_changed","channel":"CFAKE0001","message":{"subtype":"huddle_thread","ts":"3.1","room":`+fakeRoom+`}}`), h)
	if len(h.rooms) != 1 {
		t.Errorf("message_changed rooms = %v, want 1", h.rooms)
	}
	// No room, and a handler that is not a RoomObserver: nothing breaks.
	h = &roomMock{}
	dispatchWebSocketEvent([]byte(`{"type":"message","channel":"CFAKE0001","ts":"3.2","text":"x"}`), h)
	if len(h.rooms) != 0 {
		t.Errorf("rooms = %v, want none", h.rooms)
	}
	plain := &mockEventHandler{}
	dispatchWebSocketEvent([]byte(`{"type":"message","channel":"CFAKE0001","ts":"3.3","room":`+fakeRoom+`}`), plain)
}

func TestMergeRoom(t *testing.T) {
	got := MergeRoom([]byte(`{"ts":"1.1","type":"message"}`), json.RawMessage(fakeRoom))
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil || string(m["room"]) != fakeRoom || string(m["ts"]) != `"1.1"` {
		t.Errorf("merged = %s (%v)", got, err)
	}
	if string(MergeRoom([]byte(`{"a":1}`), json.RawMessage(`"x"`))) != `{"a":1}` {
		t.Error("non-object room must leave raw unchanged")
	}
}
