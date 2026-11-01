package slackclient

import (
	"context"
	"time"
)

// ConnectionManager manages the WebSocket connection lifecycle with
// automatic reconnection using exponential backoff.
type ConnectionManager struct {
	client  *Client
	handler EventHandler
	cancel  context.CancelFunc

	// Relogin, when set, gives the client a new login after Slack
	// refused the one it has. Run calls it when a connect fails and
	// CheckLogin says that Slack refuses the login itself (IsAuthError),
	// not that the network is down. It returns true when it changed the
	// client's login (Client.SetLogin); Run then connects again at once
	// instead of waiting out the backoff. Set it before Run; Run calls
	// it from its own goroutine only, one call at a time.
	Relogin func(ctx context.Context) bool
}

// loginCheckTimeout bounds the auth.test that classifies a failed
// connect, so a hung request cannot stop the reconnect loop.
const loginCheckTimeout = 15 * time.Second

// NewConnectionManager creates a new connection manager.
func NewConnectionManager(client *Client, handler EventHandler) *ConnectionManager {
	return &ConnectionManager{
		client:  client,
		handler: handler,
	}
}

// Run starts the connection loop. It connects, waits for disconnect,
// and reconnects with exponential backoff. Blocks until ctx is cancelled.
func (cm *ConnectionManager) Run(ctx context.Context) {
	ctx, cm.cancel = context.WithCancel(ctx)

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := cm.client.StartWebSocket(cm.handler)
		if err != nil {
			cm.handler.OnDisconnect()
			if cm.loginReplaced(ctx) {
				backoff = 1 * time.Second
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}

		// Connected — reset backoff
		backoff = 1 * time.Second

		// Wait for disconnect
		select {
		case <-ctx.Done():
			cm.client.StopWebSocket()
			return
		case <-cm.client.WsDone():
			// Disconnected — will reconnect after backoff
		}

		// Brief pause before reconnect
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

// loginReplaced runs after a failed connect. It reports whether Slack
// refused the login itself and Relogin gave the client a new one. A
// connect also fails while the network is down; auth.test then fails
// without a refusal, and Run waits and tries again, as it always did.
//
// It assumes that Slack refuses the WebSocket handshake of a refused
// login. A socket that opens and then closes is not checked here; the
// connect attempt after it is.
func (cm *ConnectionManager) loginReplaced(ctx context.Context) bool {
	if cm.Relogin == nil {
		return false
	}
	checkCtx, cancel := context.WithTimeout(ctx, loginCheckTimeout)
	_, _, err := cm.client.CheckLogin(checkCtx)
	cancel()
	return IsAuthError(err) && cm.Relogin(ctx)
}

// Stop cancels the connection loop and closes the WebSocket.
func (cm *ConnectionManager) Stop() {
	if cm.cancel != nil {
		cm.cancel()
	}
	cm.client.StopWebSocket()
}

func nextBackoff(current, max time.Duration) time.Duration {
	next := current * 2
	if next > max {
		return max
	}
	return next
}
