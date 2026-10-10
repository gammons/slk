package slackclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type pendingEventTestHandler struct {
	mockEventHandler
	pending         chan struct{}
	connectStarted  chan struct{}
	connectRelease  chan struct{}
	connectFinished chan struct{}
	repaired        chan struct{}
	serialized      chan bool
}

func (h *pendingEventTestHandler) PendingEvents() <-chan struct{} { return h.pending }
func (h *pendingEventTestHandler) OnPendingEvents() {
	select {
	case <-h.connectFinished:
		h.serialized <- true
	default:
		h.serialized <- false
	}
	close(h.repaired)
}
func (h *pendingEventTestHandler) OnConnect() {
	close(h.connectStarted)
	<-h.connectRelease
	close(h.connectFinished)
}

func TestStartWebSocketDispatchesPendingEventsWithoutIncomingFrames(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`)); err != nil {
			t.Errorf("hello: %v", err)
			return
		}
		// No more frames: a deferred repair must wake the dispatch loop itself.
		<-stop
	}))
	defer srv.Close()
	defer close(stop)
	c := &Client{wsBaseURL: "ws://" + srv.Listener.Addr().String()}
	h := &pendingEventTestHandler{
		pending: make(chan struct{}, 1), connectStarted: make(chan struct{}),
		connectRelease: make(chan struct{}), connectFinished: make(chan struct{}),
		repaired: make(chan struct{}), serialized: make(chan bool, 1),
	}
	if err := c.StartWebSocket(h); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = c.StopWebSocket()
		select {
		case <-c.WsDone():
		case <-time.After(5 * time.Second):
			t.Error("WebSocket event owner did not stop")
		}
	}()
	defer close(h.connectRelease)
	select {
	case <-h.connectStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("hello was not dispatched")
	}
	h.pending <- struct{}{}
	h.connectRelease <- struct{}{}
	select {
	case <-h.repaired:
	case <-time.After(5 * time.Second):
		t.Fatal("pending event was not dispatched on the idle connection")
	}
	if !<-h.serialized {
		t.Fatal("pending event was not serialized with OnConnect")
	}
}
