package slackclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gorilla/websocket"
	"github.com/slack-go/slack"
)

// The three captures behind this file are Grant's, from the official web
// client sending a Linear link in a DM (2026-10-26), anonymized: the URL,
// title, IDs, icon paths and channel are invented, the shapes are not.
// ws_unfurl_preview_updated.json is also trimmed: the real frame carries
// the whole unfurl card (blocks, fields, actions), none of which a chip
// uses.
const (
	chipURL   = "https://linear.app/example/issue/ENG-42/fix-the-flaky-login-test"
	chipTitle = "Fix the flaky login test"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// previewFrame is the captured unfurl_preview_updated frame, re-addressed
// to clientMsgID.
func previewFrame(t *testing.T, clientMsgID string) []byte {
	t.Helper()
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(readTestdata(t, "ws_unfurl_preview_updated.json"), &frame); err != nil {
		t.Fatal(err)
	}
	id, _ := json.Marshal(clientMsgID)
	frame["client_msg_id"] = id
	out, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func capturedChip(t *testing.T) linkChip {
	t.Helper()
	chip, ok := parseUnfurlPreview(readTestdata(t, "ws_unfurl_preview_updated.json"), chipURL)
	if !ok {
		t.Fatal("captured unfurl_preview_updated frame gave no chip")
	}
	return chip
}

func TestParseUnfurlPreview_ReadsTheCapturedWorkObject(t *testing.T) {
	got := capturedChip(t)
	want := linkChip{
		URL:         chipURL,
		Title:       chipTitle,
		EntityID:    "W:A0EXAMPLE01:lin1:00000000-0000-4000-8000-000000000001:00000000-0000-4000-8000-000000000002~issue",
		AppID:       "A0EXAMPLE01",
		IconURL:     "https://avatars.slack-edge.com/2024-04-02/0000000000000_00000000000000000000_36.png",
		IconName:    "brand-linear-filled",
		ProductName: "Linear",
	}
	if got != want {
		t.Errorf("chip =\n  %+v\nwant\n  %+v", got, want)
	}
}

// An unfurl with no Work Object (a plain link preview, or an app that
// unfurls the old way) is no chip; neither is one without a title.
func TestParseUnfurlPreview_NoWorkObjectIsNoChip(t *testing.T) {
	for name, frame := range map[string]string{
		"no work object": `{"type":"unfurl_preview_updated","client_msg_id":"x","attachments":{"` + chipURL + `":{"title":"A page","from_url":"` + chipURL + `"}}}`,
		"no title":       `{"type":"unfurl_preview_updated","client_msg_id":"x","attachments":{"` + chipURL + `":{"work_object_entity":{"entity_id":"W:1","app_id":"A1"}}}}`,
		"no entity id":   `{"type":"unfurl_preview_updated","client_msg_id":"x","attachments":{"` + chipURL + `":{"work_object_entity":{"app_id":"A1","layouts":{"expanded":{"title":{"text":"T"}}}}}}}`,
		"other url":      `{"type":"unfurl_preview_updated","client_msg_id":"x","attachments":{"https://a.example/1":{},"https://a.example/2":{}}}`,
		"not json":       `{`,
	} {
		if chip, ok := parseUnfurlPreview([]byte(frame), chipURL); ok {
			t.Errorf("%s: got chip %+v, want none", name, chip)
		}
	}
}

// Slack keys the attachments by URL. The lookup asks for one URL per
// client_msg_id, so a lone entry under a differently written key is
// still the answer.
func TestParseUnfurlPreview_LoneEntryUnderAnotherKey(t *testing.T) {
	frame := strings.Replace(string(readTestdata(t, "ws_unfurl_preview_updated.json")),
		`"https:\/\/linear.app\/example\/issue\/ENG-42\/fix-the-flaky-login-test": {`,
		`"https:\/\/linear.app\/example\/issue\/ENG-42\/fix-the-flaky-login-test\/": {`, 1)
	chip, ok := parseUnfurlPreview([]byte(frame), chipURL)
	if !ok || chip.URL != chipURL || chip.Title != chipTitle {
		t.Errorf("chip = %+v, %v; want %s titled %q", chip, ok, chipURL, chipTitle)
	}
}

func TestChipCandidates(t *testing.T) {
	cases := []struct {
		name, text string
		want       []string
	}{
		{"bare url", "see " + chipURL + " please", []string{chipURL}},
		{"wire form", "<" + chipURL + ">", []string{chipURL}},
		{"in a list", "- " + chipURL, []string{chipURL}},
		{"twice is one lookup", chipURL + " and " + chipURL, []string{chipURL}},
		{"labelled link stays", "[the issue](" + chipURL + ")", nil},
		{"wire label stays", "<" + chipURL + "|the issue>", nil},
		{"bold link stays", "**" + chipURL + "**", nil},
		{"code span stays", "`" + chipURL + "`", nil},
		{"code block stays", "```\n" + chipURL + "\n```", nil},
		{"email stays", "mail a@example.com", nil},
		{"no links", "hello", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, block := mrkdwn.Convert(tc.text)
			got := chipCandidates(block)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("chipCandidates(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestChipCandidates_Capped(t *testing.T) {
	var parts []string
	for i := 0; i < maxLinkChips+3; i++ {
		parts = append(parts, "https://a.example/"+string(rune('a'+i)))
	}
	_, block := mrkdwn.Convert(strings.Join(parts, " "))
	if got := chipCandidates(block); len(got) != maxLinkChips {
		t.Errorf("%d candidates, want %d", len(got), maxLinkChips)
	}
	if got := chipCandidates(nil); got != nil {
		t.Errorf("chipCandidates(nil) = %q", got)
	}
}

func TestChipMrkdwn(t *testing.T) {
	chips := map[string]linkChip{chipURL: {URL: chipURL, Title: "A & <B>"}}
	got := chipMrkdwn("see <"+chipURL+">, <"+chipURL+"|"+chipURL+"> and <"+chipURL+"|mine>", chips)
	titled := "<" + chipURL + "|A &amp; &lt;B&gt;>"
	want := "see " + titled + ", " + titled + " and <" + chipURL + "|mine>"
	if got != want {
		t.Errorf("chipMrkdwn =\n  %q\nwant\n  %q", got, want)
	}
}

var clientMsgIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewClientMsgID(t *testing.T) {
	a, b := newClientMsgID(), newClientMsgID()
	if !clientMsgIDRe.MatchString(a) || a == b {
		t.Errorf("newClientMsgID = %q, %q; want two distinct v4 UUIDs", a, b)
	}
}

func TestUnfurlLinkForm_MatchesCapture(t *testing.T) {
	want := capturedForm(t, "chat_unfurl_link.json")
	got := unfurlLinkForm("D0EXAMPLE01", chipURL, want.Get("client_msg_id"))
	if got.Encode() != want.Encode() {
		t.Errorf("form =\n  %s\nwant\n  %s", got.Encode(), want.Encode())
	}
}

// The capture's reply says only that the preview is pending; the title
// comes over the WebSocket.
func TestParseUnfurlLink_CapturedReplyIsPending(t *testing.T) {
	var c struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(readTestdata(t, "chat_unfurl_link.json"), &c); err != nil {
		t.Fatal(err)
	}
	if !parseUnfurlLink(c.Response, chipURL) {
		t.Error("captured reply not read as pending")
	}
	for _, body := range []string{
		`{"ok":false,"error":"invalid_url"}`,
		`{"ok":true,"attachments":{}}`,
		`{"ok":true,"attachments":{"` + chipURL + `":{"is_app_unfurl":false}}}`,
		`not json`,
	} {
		if parseUnfurlLink([]byte(body), chipURL) {
			t.Errorf("%s read as pending", body)
		}
	}
}

// What slk posts for a chip, field by field against the official client.
// Same channel, blocks (byte for byte) and unfurl. slk adds text, the
// mrkdwn fallback it sends with every message. It leaves out what only
// the web client's draft machinery uses (ts is a placeholder the client
// makes up, xArgs/draft_id name a server-side draft, type and
// client_context_team_id and include_channel_perm_error are not sent by
// slk's ordinary chat.postMessage either). client_msg_id is a fresh UUID.
func TestChipPostForm_MatchesCapture(t *testing.T) {
	want := capturedForm(t, "chat_post_message_chip.json")
	mr, block := mrkdwn.Convert(chipURL)
	chips := map[string]linkChip{chipURL: capturedChip(t)}
	applyChips(block, chips)
	mr = chipMrkdwn(mr, chips)
	got, err := chipPostForm("D0EXAMPLE01", "", false, mr, block, []string{chipURL}, newClientMsgID())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"channel", "blocks", "unfurl"} {
		if got.Get(k) != want.Get(k) {
			t.Errorf("%s =\n  %q\nwant\n  %q", k, got.Get(k), want.Get(k))
		}
	}
	if id := got.Get("client_msg_id"); !clientMsgIDRe.MatchString(id) {
		t.Errorf("client_msg_id = %q, want a v4 UUID", id)
	}
	if got.Get("text") != "<"+chipURL+"|"+chipTitle+">" {
		t.Errorf("text = %q", got.Get("text"))
	}
	notSent := map[string]bool{"ts": true, "type": true, "xArgs": true, "draft_id": true, "client_context_team_id": true, "include_channel_perm_error": true}
	for k := range want {
		if _, ok := got[k]; !ok && !notSent[k] {
			t.Errorf("captured field %s not sent", k)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok && k != "text" {
			t.Errorf("unexpected field %s=%q", k, got.Get(k))
		}
	}
}

func TestChipPostForm_Reply(t *testing.T) {
	_, block := mrkdwn.Convert(chipURL)
	got, err := chipPostForm("C1", "1700000000.000100", true, "x", block, []string{chipURL}, newClientMsgID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("thread_ts") != "1700000000.000100" || got.Get("reply_broadcast") != "true" {
		t.Errorf("thread_ts=%q reply_broadcast=%q", got.Get("thread_ts"), got.Get("reply_broadcast"))
	}
	got, _ = chipPostForm("C1", "1700000000.000100", false, "x", block, []string{chipURL}, newClientMsgID())
	if _, ok := got["reply_broadcast"]; ok {
		t.Error("reply_broadcast sent for a plain reply")
	}
}

// Only the element that is a bare link to a chip URL changes; the text
// around it and links without a chip stay.
func TestApplyChips_KeepsEverythingElse(t *testing.T) {
	_, block := mrkdwn.Convert("see " + chipURL + " and https://other.example/x\n- " + chipURL)
	applyChips(block, map[string]linkChip{chipURL: capturedChip(t)})
	raw, err := json.Marshal([]slack.Block{block})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if n := strings.Count(s, `"type":"attachment_mention"`); n != 2 {
		t.Errorf("%d chips, want 2 (section and list item): %s", n, s)
	}
	for _, want := range []string{`"text":"see "`, `"text":" and "`, `{"type":"link","url":"https://other.example/x"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}

func TestUnfurlWaiters_RoutesByClientMsgID(t *testing.T) {
	var w unfurlWaiters
	ch := w.wait("id-1")
	if w.deliver([]byte(`{"type":"message","client_msg_id":"id-1"}`)) {
		t.Error("a message frame was taken")
	}
	if !w.deliver(previewFrame(t, "id-2")) {
		t.Error("a preview for nobody waiting must still be taken off the event stream")
	}
	select {
	case <-ch:
		t.Fatal("id-1 got id-2's preview")
	default:
	}
	if !w.deliver(previewFrame(t, "id-1")) {
		t.Fatal("preview not taken")
	}
	// A second frame must not block the event owner.
	w.deliver(previewFrame(t, "id-1"))
	if chip, ok := parseUnfurlPreview(<-ch, chipURL); !ok || chip.Title != chipTitle {
		t.Errorf("delivered frame gave %+v, %v", chip, ok)
	}
	w.cancel("id-1")
	w.deliver(previewFrame(t, "id-1"))
}

// chipServer answers chat.unfurlLink with unfurlReply, and, when
// sendPreview is set, has the "WebSocket" deliver the captured preview
// for the lookup's client_msg_id shortly after. chat.postMessage answers
// with postReplies in turn. Every request's path and form is recorded.
type chipServer struct {
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	path string
	form url.Values
}

func (s *chipServer) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedRequest(nil), s.requests...)
}

func newChipServer(t *testing.T, unfurlReply string, sendPreview bool, postReplies ...string) (*Client, *chipServer) {
	t.Helper()
	s := &chipServer{}
	var c *Client
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		s.requests = append(s.requests, recordedRequest{r.URL.Path, r.PostForm})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/chat.unfurlLink":
			_, _ = w.Write([]byte(unfurlReply))
			if sendPreview {
				frame := previewFrame(t, r.PostForm.Get("client_msg_id"))
				go func() {
					time.Sleep(20 * time.Millisecond)
					c.unfurls.deliver(frame)
				}()
			}
		case "/api/chat.postMessage":
			s.mu.Lock()
			reply := `{"ok":true,"channel":"D0EXAMPLE01","ts":"1793057435.000200"}`
			if posts < len(postReplies) {
				reply = postReplies[posts]
			}
			posts++
			s.mu.Unlock()
			_, _ = w.Write([]byte(reply))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	c = NewClient("xoxc-test", "d-cookie")
	pointClientAtTestServer(t, c, srv)
	// slack-go's own chat.postMessage (the plain path) through the same
	// transport, as Connect rebuilds it.
	c.api = slack.New(c.token, slack.OptionHTTPClient(c.httpClient), slack.OptionAPIURL(c.apiBaseURL))
	c.wsLive.Store(true)
	return c, s
}

const pendingReply = `{"ok":true,"attachments":{"` + chipURL + `":{"is_preview_pending":true,"is_app_unfurl":true,"app_id":"A0EXAMPLE01"}}}`

func TestSendMessage_SendsAChipWhenTheAppPublishesAWorkObject(t *testing.T) {
	c, s := newChipServer(t, pendingReply, true)
	ts, mr, err := c.SendMessage(context.Background(), "D0EXAMPLE01", "see "+chipURL)
	if err != nil {
		t.Fatal(err)
	}
	if ts != "1793057435.000200" {
		t.Errorf("ts = %q", ts)
	}
	if want := "see <" + chipURL + "|" + chipTitle + ">"; mr != want {
		t.Errorf("sent mrkdwn = %q, want %q", mr, want)
	}
	reqs := s.recorded()
	if len(reqs) != 2 || reqs[0].path != "/api/chat.unfurlLink" || reqs[1].path != "/api/chat.postMessage" {
		t.Fatalf("requests = %+v, want chat.unfurlLink then chat.postMessage", reqs)
	}
	lookup, post := reqs[0].form, reqs[1].form
	if lookup.Get("url") != chipURL || lookup.Get("channel") != "D0EXAMPLE01" || lookup.Get("_x_reason") != "fetch-unfurl-preview" {
		t.Errorf("lookup form = %v", lookup)
	}
	if !strings.Contains(post.Get("blocks"), `"type":"attachment_mention"`) {
		t.Errorf("blocks carry no chip: %s", post.Get("blocks"))
	}
	if post.Get("unfurl") != `[{"url":"`+chipURL+`"}]` || post.Get("_x_reason") != "webapp_message_send" || post.Get("token") != "xoxc-test" {
		t.Errorf("post form = %v", post)
	}
}

// A link the user labelled with its own URL becomes a chip too, and the
// sender's row shows the title for it.
func TestSendMessage_URLLabelledWithItselfShowsTheTitle(t *testing.T) {
	for _, text := range []string{"[" + chipURL + "](" + chipURL + ")", "<" + chipURL + "|" + chipURL + ">"} {
		c, s := newChipServer(t, pendingReply, true)
		_, mr, err := c.SendMessage(context.Background(), "C1", text)
		if err != nil {
			t.Fatal(err)
		}
		if want := "<" + chipURL + "|" + chipTitle + ">"; mr != want {
			t.Errorf("%s: sent mrkdwn = %q, want %q", text, mr, want)
		}
		reqs := s.recorded()
		if !strings.Contains(reqs[len(reqs)-1].form.Get("blocks"), "attachment_mention") {
			t.Errorf("%s: no chip sent", text)
		}
	}
}

func TestSendReply_SendsAChipInTheThread(t *testing.T) {
	c, s := newChipServer(t, pendingReply, true)
	if _, _, err := c.SendReply(context.Background(), "C1", "1700000000.000100", chipURL, true); err != nil {
		t.Fatal(err)
	}
	reqs := s.recorded()
	post := reqs[len(reqs)-1].form
	if post.Get("thread_ts") != "1700000000.000100" || post.Get("reply_broadcast") != "true" ||
		!strings.Contains(post.Get("blocks"), `"type":"attachment_mention"`) {
		t.Errorf("reply form = %v", post)
	}
}

// Every way the lookup can come up empty sends the message as before:
// one chat.postMessage with the link as a link.
func TestSendMessage_PlainLinkWhenThereIsNoChip(t *testing.T) {
	defer func(d time.Duration) { linkChipWait = d }(linkChipWait)
	linkChipWait = 100 * time.Millisecond
	cases := []struct {
		name        string
		reply       string
		sendPreview bool
		live        bool
		lookups     int
	}{
		{"not an app unfurl", `{"ok":true,"attachments":{}}`, false, true, 1},
		{"lookup refused", `{"ok":false,"error":"invalid_url"}`, false, true, 1},
		{"no preview in time", pendingReply, false, true, 1},
		{"no websocket", pendingReply, true, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, s := newChipServer(t, tc.reply, tc.sendPreview)
			c.wsLive.Store(tc.live)
			_, mr, err := c.SendMessage(context.Background(), "D0EXAMPLE01", chipURL)
			if err != nil {
				t.Fatal(err)
			}
			if mr != "<"+chipURL+">" {
				t.Errorf("sent mrkdwn = %q", mr)
			}
			assertPlainPost(t, s.recorded(), tc.lookups)
		})
	}
}

func assertPlainPost(t *testing.T, reqs []recordedRequest, lookups int) {
	t.Helper()
	if len(reqs) != lookups+1 {
		t.Fatalf("%d requests, want %d lookups and one post: %+v", len(reqs), lookups, reqs)
	}
	post := reqs[len(reqs)-1]
	if post.path != "/api/chat.postMessage" {
		t.Fatalf("last request %s, want chat.postMessage", post.path)
	}
	if b := post.form.Get("blocks"); strings.Contains(b, "attachment_mention") || !strings.Contains(b, `"type":"link"`) {
		t.Errorf("blocks = %s, want a plain link", b)
	}
	if _, ok := post.form["unfurl"]; ok {
		t.Error("plain send carries unfurl")
	}
}

func TestSendMessage_NoLookupWithoutLinks(t *testing.T) {
	c, s := newChipServer(t, pendingReply, true)
	if _, _, err := c.SendMessage(context.Background(), "C1", "hello"); err != nil {
		t.Fatal(err)
	}
	if reqs := s.recorded(); len(reqs) != 1 || reqs[0].path != "/api/chat.postMessage" {
		t.Errorf("requests = %+v, want just chat.postMessage", reqs)
	}
}

// If Slack refuses the chip as invalid_blocks, nothing was posted, so the
// message goes again as a plain link.
func TestSendMessage_InvalidBlocksResendsPlain(t *testing.T) {
	c, s := newChipServer(t, pendingReply, true, `{"ok":false,"error":"invalid_blocks"}`)
	_, mr, err := c.SendMessage(context.Background(), "C1", chipURL)
	if err != nil {
		t.Fatal(err)
	}
	if mr != "<"+chipURL+">" {
		t.Errorf("sent mrkdwn = %q, want the plain link", mr)
	}
	reqs := s.recorded()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v, want lookup, chip post, plain post", reqs)
	}
	if !strings.Contains(reqs[1].form.Get("blocks"), "attachment_mention") {
		t.Error("first post was not the chip")
	}
	assertPlainPost(t, reqs[1:], 1)
}

// Any other refusal is the send's error, and is not retried.
func TestSendMessage_OtherRefusalIsTheError(t *testing.T) {
	c, s := newChipServer(t, pendingReply, true, `{"ok":false,"error":"restricted_action_read_only_channel"}`)
	_, _, err := c.SendMessage(context.Background(), "C1", chipURL)
	if err == nil || !strings.Contains(err.Error(), "restricted_action_read_only_channel") {
		t.Fatalf("err = %v", err)
	}
	if n := len(s.recorded()); n != 2 {
		t.Errorf("%d requests, want lookup and one post", n)
	}
}

// The event owner takes unfurl_preview_updated frames off the socket for
// the lookup and passes nothing on to the handler, and the client knows
// whether its socket is up.
func TestStartWebSocket_RoutesUnfurlPreviews(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	send := make(chan []byte)
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			select {
			case frame := <-send:
				if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}))
	defer srv.Close()
	defer close(stop)
	c := &Client{wsBaseURL: "ws://" + srv.Listener.Addr().String()}
	if c.wsLive.Load() {
		t.Fatal("live before StartWebSocket")
	}
	h := &mockEventHandler{}
	if err := c.StartWebSocket(h); err != nil {
		t.Fatal(err)
	}
	waiting := c.unfurls.wait("id-1")
	send <- previewFrame(t, "id-1")
	select {
	case frame := <-waiting:
		if _, ok := parseUnfurlPreview(frame, chipURL); !ok {
			t.Error("routed frame gave no chip")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preview was not routed")
	}
	if !c.wsLive.Load() {
		t.Error("not live while the socket is up")
	}
	_ = c.StopWebSocket()
	select {
	case <-c.WsDone():
	case <-time.After(5 * time.Second):
		t.Fatal("event owner did not stop")
	}
	if c.wsLive.Load() {
		t.Error("still live after the socket closed")
	}
}
