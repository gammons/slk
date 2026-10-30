package slackclient

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/gammons/slk/internal/debuglog"
)

// A huddle_thread message carries a top-level `room` object (who started
// the huddle, when it began and ended). slack-go's Msg has no field for
// it, so the typed history and replies calls drop it. roomTap reads it
// off the raw response on its way to slack-go, and the live WebSocket
// path reads it off the frame (see RoomObserver).

const (
	// roomStoreMax bounds the remembered rooms. Only huddle_thread messages
	// are kept, so this is far more than one history page ever holds.
	roomStoreMax = 512
	// roomTapMaxBody is the largest response the tap will buffer to look
	// for rooms; a larger one passes through unread.
	roomTapMaxBody = 16 << 20
)

// RoomObserver is an optional EventHandler extension. dispatchWebSocketEvent
// calls OnMessageRoom just before OnMessage for a message frame that carries
// a `room` object, with the object verbatim. It is a second interface, not
// an OnMessage parameter, so handlers that do not care (and every test
// double) are untouched.
type RoomObserver interface {
	OnMessageRoom(channelID, ts string, room json.RawMessage)
}

// roomStore remembers the room of recently fetched huddle messages, keyed
// by channel and ts. Safe for concurrent use; it hands out copies only.
type roomStore struct {
	mu    sync.Mutex
	rooms map[string]json.RawMessage
	order []string
}

func newRoomStore() *roomStore {
	return &roomStore{rooms: map[string]json.RawMessage{}}
}

func roomKey(channelID, ts string) string { return channelID + "/" + ts }

func (s *roomStore) put(channelID, ts string, room json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := roomKey(channelID, ts)
	if _, ok := s.rooms[k]; !ok {
		s.order = append(s.order, k)
		if len(s.order) > roomStoreMax {
			delete(s.rooms, s.order[0])
			s.order = s.order[1:]
		}
	}
	s.rooms[k] = append(json.RawMessage(nil), room...)
}

func (s *roomStore) get(channelID, ts string) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A response whose request channel could not be read is stored under "".
	for _, k := range []string{roomKey(channelID, ts), roomKey("", ts)} {
		if r, ok := s.rooms[k]; ok {
			return append(json.RawMessage(nil), r...)
		}
	}
	return nil
}

// HuddleRoom returns the room captured for a huddle_thread message from a
// conversations.history or conversations.replies response, verbatim, or
// nil when none was seen. The result is the caller's.
func (c *Client) HuddleRoom(channelID, ts string) json.RawMessage {
	if c == nil || c.rooms == nil {
		return nil
	}
	return c.rooms.get(channelID, ts)
}

// wrap returns a RoundTripper that records rooms from the history and
// replies responses of inner.
func (s *roomStore) wrap(inner http.RoundTripper) http.RoundTripper {
	return &roomTap{inner: inner, store: s}
}

type roomTap struct {
	inner http.RoundTripper
	store *roomStore
}

func isRoomEndpoint(u *url.URL) bool {
	if u == nil {
		return false
	}
	return strings.HasSuffix(u.Path, "/api/conversations.history") ||
		strings.HasSuffix(u.Path, "/api/conversations.replies")
}

func (t *roomTap) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	resp, err := inner.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || resp.StatusCode != http.StatusOK || !isRoomEndpoint(req.URL) {
		return resp, err
	}
	channelID := requestChannel(req)
	buf, err := io.ReadAll(io.LimitReader(resp.Body, roomTapMaxBody+1))
	if err != nil || len(buf) > roomTapMaxBody {
		// Hand the body on whole, read or not.
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(buf), resp.Body), resp.Body}
		return resp, nil
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(buf))
	t.record(channelID, buf)
	return resp, nil
}

// record stores the room of every huddle_thread message in a response body.
func (t *roomTap) record(channelID string, body []byte) {
	var env struct {
		Messages []struct {
			TS      string          `json:"ts"`
			Subtype string          `json:"subtype"`
			Room    json.RawMessage `json:"room"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &env) != nil {
		return
	}
	for _, m := range env.Messages {
		if m.Subtype != "huddle_thread" || m.TS == "" || !isJSONObject(m.Room) {
			continue
		}
		t.store.put(channelID, m.TS, m.Room)
		LogHuddleRoom(m.TS, m.Room)
	}
}

// requestChannel reads the `channel` parameter of a request, from the
// query or an urlencoded body (when it can be re-read); "" if absent.
func requestChannel(req *http.Request) string {
	if req.URL != nil {
		if c := req.URL.Query().Get("channel"); c != "" {
			return c
		}
	}
	if req.GetBody == nil {
		return ""
	}
	rc, err := req.GetBody()
	if err != nil {
		return ""
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	if err != nil {
		return ""
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		return ""
	}
	return form.Get("channel")
}

func isJSONObject(raw json.RawMessage) bool {
	return len(raw) > 0 && bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{"))
}

// LogHuddleRoom writes the key names of a captured room (never values) to
// the debug log, so the shape of the object can be learned from a real
// workspace without logging IDs or text.
func LogHuddleRoom(ts string, room json.RawMessage) {
	if !debuglog.Enabled() {
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(room, &obj) != nil {
		return
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	debuglog.WS("huddle room: ts=%s keys=[%s]", ts, strings.Join(keys, " "))
}

// MergeRoom returns raw (a message's JSON object) with its top-level
// `room` key set to room. raw is returned unchanged when room is not a
// JSON object or raw is not an object.
func MergeRoom(raw []byte, room json.RawMessage) []byte {
	if !isJSONObject(room) {
		return raw
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return raw
	}
	obj["room"] = room
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}
