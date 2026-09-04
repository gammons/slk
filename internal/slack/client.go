package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slack/mrkdwn"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gorilla/websocket"
	"github.com/slack-go/slack"
)

// SlackAPI defines the subset of the Slack API we use.
// This interface enables mocking in tests.
type SlackAPI interface {
	GetConversationsForUser(params *slack.GetConversationsForUserParameters) ([]slack.Channel, string, error)
	GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	SearchMessagesContext(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error)
	// Deliberately absent: GetConversations (conversations.list) and
	// GetUsersContext (users.list). See
	// TestSlackAPI_DeclaresNoWorkspaceEnumeration — the whole
	// directory is never fetched, only individual users on demand via
	// GetUserInfo.
	GetUserGroupsContext(ctx context.Context, options ...slack.GetUserGroupsOption) ([]slack.UserGroup, error)
	GetUsersInConversationContext(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error)
	GetUserInfo(user string) (*slack.User, error)
	GetBotInfoContext(ctx context.Context, parameters slack.GetBotInfoParameters) (*slack.Bot, error)
	GetEmojiContext(ctx context.Context) (map[string]string, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	UpdateMessage(channelID, timestamp string, options ...slack.MsgOption) (string, string, string, error)
	DeleteMessage(channelID, timestamp string) (string, string, error)
	AddReaction(name string, item slack.ItemRef) error
	RemoveReaction(name string, item slack.ItemRef) error
	GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error)
	AuthTest() (*slack.AuthTestResponse, error)
	JoinConversation(channelID string) (*slack.Channel, string, []string, error)
	SetUserPresenceContext(ctx context.Context, presence string) error
	GetUserPresenceContext(ctx context.Context, user string) (*slack.UserPresence, error)
	SetSnoozeContext(ctx context.Context, minutes int) (*slack.DNDStatus, error)
	EndSnoozeContext(ctx context.Context) (*slack.DNDStatus, error)
	EndDNDContext(ctx context.Context) error
	GetDNDInfoContext(ctx context.Context, user *string, options ...slack.ParamOption) (*slack.DNDStatus, error)
	UploadFileContext(ctx context.Context, params slack.UploadFileParameters) (*slack.FileSummary, error)
	OpenConversationContext(ctx context.Context, params *slack.OpenConversationParameters) (*slack.Channel, bool, bool, error)
}

// defaultAPIBaseURL is the canonical Slack Web API root used as a fallback
// before Connect() has run, when auth.test returns no usable URL, or when a
// caller never goes through NewClient (e.g., tests). It mirrors slack-go's
// internal default (slack.APIURL).
const defaultAPIBaseURL = "https://slack.com/api/"

// defaultWSBaseURL is the scheme+host slk opens its event WebSocket
// against, matching the official web client. Only the scheme and host
// live here; StartWebSocket owns the query string, which is part of the
// protocol fingerprint being impersonated.
const defaultWSBaseURL = "wss://wss-primary.slack.com"

// Client wraps the slack-go library, providing RTM connectivity
// and a simplified Web API surface for the service layer.
// Uses browser cookie auth (xoxc token + d cookie).
type Client struct {
	api    SlackAPI
	wsConn *websocket.Conn
	wsMu   sync.Mutex
	wsDone chan struct{}
	teamID string
	userID string
	token  string
	cookie string

	// apiBaseURL is the workspace-specific Web API root, e.g.
	// "https://slack.com/api/" for non-grid workspaces or
	// "https://hackclub.enterprise.slack.com/api/" for enterprise grids.
	// Always ends in "/" so it can be concatenated with method names.
	// Discovered from auth.test's URL field on Connect; defaults to
	// defaultAPIBaseURL until then.
	apiBaseURL string

	// wsBaseURL is the scheme+host StartWebSocket dials, e.g.
	// "wss://wss-primary.slack.com". Never ends in "/" — StartWebSocket
	// appends "/?..." itself. Set to defaultWSBaseURL by NewClient;
	// overridden by tests so the upgrade handshake can be exercised
	// against a local server, the same way apiBaseURL is.
	wsBaseURL string

	// teamURL is the raw workspace URL from auth.test's response
	// (e.g. "https://truelist-workspace.slack.com/"). Used to derive
	// the workspace subdomain for in-app permalink routing.
	teamURL string

	// httpClient is the cookie-bearing HTTP client used by both the
	// inner slack-go client and the four hand-rolled endpoint calls.
	// Stored so Connect() can rebuild the slack-go client with the
	// discovered apiBaseURL via slack.OptionAPIURL. Nil for clients
	// constructed directly in tests (e.g., &Client{api: mock}); in
	// that case Connect() leaves the existing api field alone.
	httpClient *http.Client

	// envelope carries the per-session telemetry identity Slack's web
	// client puts on every API request (_x_id, _x_csid, slack_route,
	// _x_version_ts, ...). It is owned by the Client because its
	// post-boot phase is keyed on the team id Connect discovers, and
	// because httpClient's BrowserTransport holds the same pointer —
	// so SetTeamID here changes what every subsequent request emits.
	//
	// Nil for clients constructed directly in tests
	// (e.g. &Client{api: mock}); BrowserTransport treats a nil Env as
	// "send no envelope params", so those clients still work.
	envelope *slackhttp.Envelope
}

// NewClient creates a new Slack client using browser cookie auth.
// xoxcToken is the xoxc-... token from the browser.
// dCookie is the value of the 'd' cookie from slack.com.
func NewClient(xoxcToken, dCookie string) *Client {
	env := slackhttp.NewEnvelope()
	httpClient := newCookieHTTPClient(dCookie, env)

	api := slack.New(
		xoxcToken,
		slack.OptionHTTPClient(httpClient),
	)

	return &Client{
		api:        api,
		token:      xoxcToken,
		cookie:     dCookie,
		apiBaseURL: defaultAPIBaseURL,
		wsBaseURL:  defaultWSBaseURL,
		httpClient: httpClient,
		envelope:   env,
	}
}

// newCookieJar creates a cookie jar with the Slack 'd' cookie set.
func newCookieJar(dCookie string) http.CookieJar {
	jar, _ := cookiejar.New(nil)

	slackURL, _ := url.Parse("https://slack.com")
	jar.SetCookies(slackURL, []*http.Cookie{
		{
			Name:   "d",
			Value:  dCookie,
			Domain: ".slack.com",
			Path:   "/",
			Secure: true,
		},
	})

	return jar
}

// newCookieHTTPClient creates an http.Client with the Slack 'd' cookie set
// and a BrowserTransport that injects Chrome-like headers on every request
// to *.slack.com hosts. This keeps Enterprise Grid anomaly detectors from
// flagging slk's traffic as non-browser. See internal/slackhttp.
//
// env supplies the telemetry envelope (_x_id, _x_csid, slack_route, ...)
// added to workspace API calls. Pass nil for clients that fetch pages or
// assets rather than calling /api/ — BrowserTransport scopes the envelope
// to /api/ paths anyway, but nil states the intent.
//
// slackhttp.NewBrowserHTTPClient is deliberately not used here: it builds
// a BrowserTransport with no Env, which is right for asset clients (see
// internal/image) and wrong for the API client.
func newCookieHTTPClient(dCookie string, env *slackhttp.Envelope) *http.Client {
	return &http.Client{
		Transport: &slackhttp.BrowserTransport{
			Inner: http.DefaultTransport,
			Env:   env,
			// This client carries the bulk of slk's API traffic, so
			// without this the per-boot tally Phase 2b's success
			// criteria are stated in would miss almost everything.
			// NewBrowserHTTPClient attaches the same counter to the
			// clients it builds; this literal has to opt in by hand.
			Counter: slackhttp.DefaultCounter,
		},
		Jar: newCookieJar(dCookie),
	}
}

// apiHTTPClient returns the HTTP client every outbound request from
// this Client must go through.
//
// GetUnreadCounts, callChannelSectionsList and GetStarredChannels used
// to call newCookieHTTPClient directly instead. That was invisible in
// two ways. It made a dropped envelope argument undetectable — a
// mutation replacing their env argument with nil survived the whole
// suite — because no test could observe those three requests:
// pointClientAtTestServer redirects c.httpClient's transport, and a
// method that builds its own client ignores it and dials slack.com for
// real. It also built a fresh cookie jar per call.
//
// The nil branch exists only for Clients constructed directly in tests
// (&Client{api: mock}); NewClient always sets httpClient. It is pinned
// by TestAPIHTTPClient_FallbackCarriesEnvelope so it cannot lose the
// envelope either.
func (c *Client) apiHTTPClient() *http.Client {
	if c.httpClient != nil {
		return c.httpClient
	}
	return newCookieHTTPClient(c.cookie, c.envelope)
}

// HTTPClient returns the HTTP client this Client sends every request
// through — the same one apiHTTPClient hands the internal call sites.
//
// It exists for one caller: edge.New, which takes an *http.Client and
// documents that it must be one built with slackhttp.BrowserTransport.
// Handing it a plain &http.Client{} instead compiles, runs, and sends
// every edgeapi request with Go's default User-Agent, none of Chrome's
// header set and no _x_app_name/fp envelope — the exact divergence this
// phase exists to remove, visible nowhere in slk's own output. Routing
// through apiHTTPClient rather than returning c.httpClient directly
// also means a Client built as a literal in a test still gets an
// envelope-carrying client rather than nil.
//
// A second, wanted consequence: this client's transport holds
// slackhttp.DefaultCounter, so edgeapi calls appear in the shutdown
// request tally alongside the workspace API's.
func (c *Client) HTTPClient() *http.Client {
	return c.apiHTTPClient()
}

// PostForm is postForm, exported.
//
// boot.UserBoot and boot.ConversationsView take a boot.PostFunc, whose
// signature is exactly postForm's. internal/slack/boot is a
// stdlib-only parser that cannot import this package, and
// internal/bootstrap must not either (see that package's comment on
// import direction), so the wiring in cmd/slk needs a method value it
// can pass. This is that method value and nothing else: no defaulting,
// no reshaping, no second token injection. Anything added here would be
// a request-shape change invisible at the call sites that already use
// postForm.
func (c *Client) PostForm(ctx context.Context, method string, form url.Values) ([]byte, error) {
	return c.postForm(ctx, method, form)
}

// Envelope returns the client's Slack telemetry envelope, or nil for a
// Client constructed directly (tests). Callers use it to read or update
// session-scoped values such as the build timestamp; the same pointer is
// held by the HTTP transport, so updates take effect on the next request.
func (c *Client) Envelope() *slackhttp.Envelope {
	return c.envelope
}

// TeamID returns the authenticated workspace's team ID.
// Empty before Connect is called.
func (c *Client) TeamID() string {
	return c.teamID
}

// UserID returns the authenticated user's ID.
// Empty before Connect is called.
func (c *Client) UserID() string {
	return c.userID
}

// WsDone returns a channel that is closed when the WebSocket read loop exits.
func (c *Client) WsDone() <-chan struct{} {
	return c.wsDone
}

// Connect authenticates with Slack, populates the team/user IDs, and
// discovers the workspace-specific API base URL from auth.test's response.
//
// On enterprise grid workspaces, every API request must hit the
// grid-prefixed host (e.g. "https://hackclub.enterprise.slack.com/api/...")
// rather than the canonical "https://slack.com/api/..." — otherwise the
// requests are routed to a different team or fail outright. The official
// browser client learns the right host the same way: the URL field on
// auth.test's response carries it.
//
// If we own the inner slack-go client (NewClient set httpClient), rebuild
// it with the discovered API URL via slack.OptionAPIURL so subsequent
// slack-go calls also target the workspace host. If a test injected a
// mock api directly, leave it alone.
func (c *Client) Connect(ctx context.Context) error {
	resp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("auth test failed: %w", err)
	}
	c.teamID = resp.TeamID
	// Moves the envelope into its post-boot phase: from here on every
	// request carries _x_csid and slack_route, as the official client's
	// do once it knows the workspace. Nil for Clients built directly in
	// tests, which have no transport to decorate either.
	if c.envelope != nil {
		c.envelope.SetTeamID(c.teamID)
	}
	c.userID = resp.UserID
	c.apiBaseURL = deriveAPIBaseURL(resp.URL)
	c.teamURL = resp.URL

	if c.httpClient != nil {
		c.api = slack.New(
			c.token,
			slack.OptionHTTPClient(c.httpClient),
			slack.OptionAPIURL(c.apiBaseURL),
		)
	}

	return nil
}

// deriveAPIBaseURL turns the team URL returned by auth.test (e.g.
// "https://hackclub.enterprise.slack.com/") into the API base URL slk
// should use for subsequent requests ("https://hackclub.enterprise.slack.com/api/").
//
// We only trust hosts under .slack.com — anything else (empty input, garbage,
// a non-Slack host) falls back to the canonical defaultAPIBaseURL. This
// keeps a malformed auth.test response from accidentally routing tokens to
// an unintended host.
func deriveAPIBaseURL(authTestURL string) string {
	if authTestURL == "" {
		return defaultAPIBaseURL
	}
	u, err := url.Parse(authTestURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return defaultAPIBaseURL
	}
	host := u.Host
	if host != "slack.com" && !strings.HasSuffix(host, ".slack.com") {
		return defaultAPIBaseURL
	}
	return "https://" + host + "/api/"
}

// TeamSubdomain returns the workspace's subdomain under .slack.com
// (e.g. "truelist-workspace" for truelist-workspace.slack.com), or ""
// before Connect or when the auth.test URL was not a *.slack.com host.
func (c *Client) TeamSubdomain() string {
	return subdomainFromTeamURL(c.teamURL)
}

// subdomainFromTeamURL extracts the subdomain from a workspace URL.
// Only hosts strictly under .slack.com produce a non-empty result.
func subdomainFromTeamURL(teamURL string) string {
	if teamURL == "" {
		return ""
	}
	u, err := url.Parse(teamURL)
	if err != nil || u.Host == "" {
		return ""
	}
	if !strings.HasSuffix(u.Host, ".slack.com") {
		return ""
	}
	return strings.TrimSuffix(u.Host, ".slack.com")
}

// wsUpgradeHeaders returns the HTTP headers slk attaches to the WebSocket
// upgrade request.
//
// This is NOT the same set BrowserTransport adds to ordinary HTTP
// requests. Chrome sends a smaller set on a WS handshake: no Accept, no
// Sec-Fetch-*, no sec-ch-ua* client hints, no Priority. An earlier
// version of this function set Sec-Fetch-Dest: websocket on the belief
// that browsers send it; the 2026-07-30 captures show they send no
// Sec-Fetch-* header at all on an upgrade.
//
// gorilla/websocket's Dialer.Dial accepts arbitrary headers (except the
// protocol-managed Sec-WebSocket-* set, which it owns), so this is the
// right injection point. We can't reuse BrowserTransport here because
// the dialer doesn't go through http.RoundTripper.
func wsUpgradeHeaders() http.Header {
	return slackhttp.WebSocketHeaders()
}

// StartWebSocket connects to Slack's internal WebSocket using the xoxc token
// and d cookie, matching the protocol used by the browser client.
// Events are dispatched to the provided handler in a goroutine.
// Call this after Connect.
func (c *Client) StartWebSocket(handler EventHandler) error {
	// start_args matches the official client's, key for key, from the
	// 2026-08-02 coldboot capture. slk used to send only
	// agent/connect_only/ms_latest, and at some point user_typing
	// frames stopped arriving — typing indicators silently disappeared
	// while everything else on the socket kept working. Which key
	// gates typing delivery is undocumented; the capture is the
	// contract, so all of them go out. agent_version is the same
	// version_ts the envelope carries (_x_version_ts); without an
	// envelope it is simply empty, matching the no-envelope clients
	// the envelope doc describes.
	agentVersion := ""
	if c.envelope != nil {
		agentVersion = c.envelope.VersionTS()
	}
	startArgs := fmt.Sprintf(
		"?agent=client&org_wide_aware=true&agent_version=%s&eac_cache_ts=true&cache_ts=0&name_tagging=true&only_self_subteams=true&connect_only=true&ms_latest=true",
		agentVersion,
	)
	wsURL := fmt.Sprintf(
		"%s/?token=%s&sync_desync=1&slack_client=desktop&start_args=%s&no_query_on_subscribe=1&flannel=3&lazy_channels=1&gateway_server=%s-1&batch_presence_aware=1",
		c.wsBaseURL,
		url.QueryEscape(c.token),
		url.QueryEscape(startArgs),
		c.teamID,
	)

	jar := newCookieJar(c.cookie)
	dialer := &websocket.Dialer{Jar: jar}

	conn, _, err := dialer.Dial(wsURL, wsUpgradeHeaders())
	if err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}
	c.wsConn = conn
	c.wsDone = make(chan struct{})

	// Detect dead connections: set a read deadline that resets on every
	// incoming message or pong. Slack sends pings ~every 30s, so a 60s
	// deadline gives plenty of margin. Without this, ReadMessage blocks
	// forever on a silently-dropped TCP connection (e.g., wifi disconnect).
	const wsTimeout = 60 * time.Second
	conn.SetReadDeadline(time.Now().Add(wsTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsTimeout))
		return nil
	})
	conn.SetPingHandler(func(msg string) error {
		conn.SetReadDeadline(time.Now().Add(wsTimeout))
		c.wsMu.Lock()
		defer c.wsMu.Unlock()
		return conn.WriteControl(websocket.PongMessage, []byte(msg), time.Now().Add(10*time.Second))
	})

	go func() {
		defer close(c.wsDone)
		defer handler.OnDisconnect()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return
				}
				// Read error (timeout, connection closed, etc.) — exit loop
				return
			}
			// Reset deadline on every successful read
			conn.SetReadDeadline(time.Now().Add(wsTimeout))
			dispatchWebSocketEvent(message, handler)
		}
	}()

	return nil
}

// StopWebSocket disconnects the WebSocket connection.
func (c *Client) StopWebSocket() error {
	if c.wsConn != nil {
		return c.wsConn.Close()
	}
	return nil
}

// SendTyping sends a typing indicator to the given channel via WebSocket.
func (c *Client) SendTyping(channelID string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn == nil {
		return fmt.Errorf("websocket not connected")
	}
	msg := map[string]string{
		"type":    "typing",
		"channel": channelID,
	}
	return c.wsConn.WriteJSON(msg)
}

// SubscribePresence asks Slack to deliver presence_change events for the
// given user IDs. Sent over the existing WebSocket connection. Slack only
// emits presence_change for users you've explicitly subscribed to (the
// authenticated user is typically auto-subscribed at connect, but the
// explicit subscription is reliable across servers).
func (c *Client) SubscribePresence(userIDs []string) error {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	if c.wsConn == nil {
		return fmt.Errorf("websocket not connected")
	}
	msg := map[string]interface{}{
		"type": "presence_sub",
		"ids":  userIDs,
	}
	return c.wsConn.WriteJSON(msg)
}

// GetChannels retrieves conversations the user is a member of (channels, DMs,
// group DMs), paginating automatically. Uses users.conversations which returns
// only joined channels — much faster than conversations.list for large workspaces.
func (c *Client) GetChannels(ctx context.Context) ([]slack.Channel, error) {
	var allChannels []slack.Channel
	cursor := ""

	for {
		params := &slack.GetConversationsForUserParameters{
			Types:           []string{"public_channel", "private_channel", "mpim", "im"},
			Limit:           200,
			Cursor:          cursor,
			ExcludeArchived: true,
		}

		channels, nextCursor, err := c.api.GetConversationsForUser(params)
		if err != nil {
			// Handle rate limits gracefully
			if rlErr, ok := err.(*slack.RateLimitedError); ok {
				wait := rlErr.RetryAfter
				if wait == 0 {
					wait = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue // retry same page
			}
			return nil, fmt.Errorf("getting user conversations: %w", err)
		}

		allChannels = append(allChannels, channels...)

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allChannels, nil
}

// GetUsersInConversation returns all user IDs that are members of the
// given conversation (channel, DM, group DM, or shared channel). Paginates
// 1000 IDs per page. On 429 responses, sleeps the server-advised RetryAfter
// (defaulting to 30s if zero) and retries the same page; honors ctx
// cancellation both between iterations and during the rate-limit sleep.
// Mirrors the paginate-until-empty-cursor loop structure.
func (c *Client) GetUsersInConversation(ctx context.Context, channelID string) ([]string, error) {
	var all []string
	cursor := ""

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		params := &slack.GetUsersInConversationParameters{
			ChannelID: channelID,
			Cursor:    cursor,
			Limit:     1000,
		}

		users, next, err := c.api.GetUsersInConversationContext(ctx, params)
		if err != nil {
			if rlErr, ok := err.(*slack.RateLimitedError); ok {
				wait := rlErr.RetryAfter
				if wait == 0 {
					wait = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return nil, fmt.Errorf("listing conversation members: %w", err)
		}

		all = append(all, users...)

		if next == "" {
			return all, nil
		}
		cursor = next
	}
}

// JoinChannel joins a public channel via conversations.join. Returns nil on
// success. Idempotent: joining a channel you're already in is a no-op on
// Slack's side and returns no error here.
func (c *Client) JoinChannel(ctx context.Context, channelID string) error {
	_, _, _, err := c.api.JoinConversation(channelID)
	if err != nil {
		return fmt.Errorf("joining channel %s: %w", channelID, err)
	}
	return nil
}

// GetUserProfile fetches a single user's profile by ID.
func (c *Client) GetUserProfile(userID string) (*slack.User, error) {
	user, err := c.api.GetUserInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("getting user info: %w", err)
	}
	return user, nil
}

// GetBotInfo fetches a bot's name and icon by bot ID (the `bot_id` on a
// bot_message). Used to resolve the display name and avatar for messages
// posted by bots/apps, which carry no `user` field.
func (c *Client) GetBotInfo(ctx context.Context, botID string) (*slack.Bot, error) {
	bot, err := c.api.GetBotInfoContext(ctx, slack.GetBotInfoParameters{Bot: botID})
	if err != nil {
		return nil, fmt.Errorf("getting bot info: %w", err)
	}
	return bot, nil
}

// GetHistory retrieves message history for a channel.
// If oldest is set, returns messages newer than that timestamp.
func (c *Client) GetHistory(ctx context.Context, channelID string, limit int, oldest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}
	if oldest != "" {
		params.Oldest = oldest
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting history: %w", err)
	}

	return resp.Messages, nil
}

// GetOlderHistory retrieves messages older than the given timestamp.
func (c *Client) GetOlderHistory(ctx context.Context, channelID string, limit int, latest string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
		Latest:    latest,
	}

	resp, err := c.api.GetConversationHistory(params)
	if err != nil {
		return nil, fmt.Errorf("getting older history: %w", err)
	}

	return resp.Messages, nil
}

// GetHistoryAround fetches a window of channel history centered on ts:
// up to limit messages at-or-older than ts (inclusive), plus up to
// limit messages newer than ts — but the newer half is included only
// when it is complete. With only Oldest set, conversations.history
// anchors at the channel head: it returns the most recent limit
// messages in (ts, now], not the ones adjacent to ts. So when more
// than limit messages were posted after the target (has_more=true),
// the newer page is dropped entirely and the window ends at the
// target; a contiguous window beats a gapped one. Returned
// newest-first, matching Slack's conversations.history ordering. Used
// by jump-to-message navigation (search results, permalinks) when the
// target is outside the loaded buffer.
func (c *Client) GetHistoryAround(ctx context.Context, channelID, ts string, limit int) ([]slack.Message, error) {
	older, err := c.api.GetConversationHistory(&slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    ts,
		Inclusive: true,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("getting history around %s (older): %w", ts, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("getting history around %s: %w", ts, err)
	}
	newer, err := c.api.GetConversationHistory(&slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Oldest:    ts,
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("getting history around %s (newer): %w", ts, err)
	}
	if newer.HasMore {
		// The newer page is not adjacent to ts (see doc comment);
		// including it would leave a silent hole after the target.
		return older.Messages, nil
	}
	out := make([]slack.Message, 0, len(newer.Messages)+len(older.Messages))
	out = append(out, newer.Messages...)
	out = append(out, older.Messages...)
	return out, nil
}

// SearchMessages runs a workspace-wide message search via Slack's
// search.messages endpoint. The query string is passed through
// verbatim, so Slack-side modifiers (from:, in:, before:, ...) work
// unmodified. Results are relevance-sorted (Slack's default).
func (c *Client) SearchMessages(ctx context.Context, query string, count int) (*slack.SearchMessages, error) {
	params := slack.NewSearchParameters()
	// Non-positive counts would be forwarded to Slack as count=0;
	// keep slack-go's default (20) instead.
	if count > 0 {
		params.Count = count
	}
	res, err := c.api.SearchMessagesContext(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("searching messages: %w", err)
	}
	return res, nil
}

// GetHistorySince fetches all messages newer than `oldest` in the
// channel, paginating forward via response_metadata.next_cursor.
// Stops when has_more is false or when the cumulative message count
// reaches maxTotal (a hard cap that protects against runaway
// backfills after very long disconnects in busy channels).
//
// Returns messages in the order Slack delivered them (newest-first
// per page, oldest page first since pagination walks forward through
// time). Callers that need oldest-first order should reverse the
// slice.
//
// HistorySinceResult bundles the messages fetched by GetHistorySince
// with a "did we get everything?" flag. Capped == true means the
// caller hit maxTotal before the API ran out of pages, so there are
// still older-than-cap messages between (oldest, latest-fetched-ts)
// the caller didn't get. Callers that advance a sync watermark MUST
// gate the advance on Capped == false.
type HistorySinceResult struct {
	Messages []slack.Message
	Capped   bool
}

// GetHistorySince fetches all messages newer than `oldest` for the
// given channel, paginating through next_cursor up to a hard ceiling
// of maxTotal messages. Slack returns messages newest-first per page;
// pagination via next_cursor walks toward older pages within the
// (oldest, latest] window. When maxTotal is hit, the result's Capped
// field is set to true so callers can decide whether to advance a
// watermark (don't) or record a gap.
//
// If oldest == "", behaves like a single GetHistory call (no
// pagination) and returns at most maxTotal messages from the latest
// page, with Capped reflecting whether HasMore was true. This matches
// the "first-sync channel: just give me the latest page" pattern.
func (c *Client) GetHistorySince(ctx context.Context, channelID, oldest string, maxTotal int) (HistorySinceResult, error) {
	if maxTotal <= 0 {
		maxTotal = 500
	}

	// No prior sync — fetch latest page only.
	if oldest == "" {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Limit:     200,
		}
		resp, err := c.api.GetConversationHistory(params)
		if err != nil {
			return HistorySinceResult{}, fmt.Errorf("get history (no oldest): %w", err)
		}
		out := resp.Messages
		capped := resp.HasMore
		if len(out) > maxTotal {
			out = out[:maxTotal]
			capped = true
		}
		return HistorySinceResult{Messages: out, Capped: capped}, nil
	}

	var all []slack.Message
	cursor := ""
	for {
		params := &slack.GetConversationHistoryParameters{
			ChannelID: channelID,
			Oldest:    oldest,
			Limit:     200,
			Cursor:    cursor,
		}
		resp, err := c.api.GetConversationHistory(params)
		if err != nil {
			// Rate-limit retry mirrors GetChannels' pattern.
			if rlErr, ok := err.(*slack.RateLimitedError); ok {
				wait := rlErr.RetryAfter
				if wait == 0 {
					wait = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return HistorySinceResult{Messages: all, Capped: true}, ctx.Err()
				case <-time.After(wait):
				}
				continue
			}
			return HistorySinceResult{Messages: all, Capped: true}, fmt.Errorf("get history since %s: %w", oldest, err)
		}

		all = append(all, resp.Messages...)
		if len(all) >= maxTotal {
			return HistorySinceResult{Messages: all[:maxTotal], Capped: true}, nil
		}
		if !resp.HasMore || resp.ResponseMetaData.NextCursor == "" {
			return HistorySinceResult{Messages: all, Capped: false}, nil
		}
		cursor = resp.ResponseMetaData.NextCursor
	}
}

// GetUserGroups retrieves the workspace's usergroups (the @team
// handles behind <!subteam^S…> mentions) via usergroups.list.
func (c *Client) GetUserGroups(ctx context.Context) ([]slack.UserGroup, error) {
	groups, err := c.api.GetUserGroupsContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting usergroups: %w", err)
	}
	return groups, nil
}

// ListCustomEmoji fetches the workspace's custom emoji list via Slack's
// emoji.list API. Returns a map of emoji name -> URL or "alias:targetname".
// The map is empty if the workspace has no custom emojis.
func (c *Client) ListCustomEmoji(ctx context.Context) (map[string]string, error) {
	// GetEmojiContext, not GetEmoji: the latter hardcodes
	// context.Background() internally, so ctx was silently dropped and
	// a shutdown could not cancel this. Harmless while the call only
	// ran on the rare conversations.history fallback; it now runs on
	// every workspace connect.
	emojis, err := c.api.GetEmojiContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing custom emoji: %w", err)
	}
	if emojis == nil {
		emojis = map[string]string{}
	}
	return emojis, nil
}

// SendMessage posts a new message to the specified channel. Returns
// the timestamp and the converted mrkdwn text actually sent (callers
// use this for optimistic display so it matches what other Slack
// clients will render).
func (c *Client) SendMessage(ctx context.Context, channelID, text string) (string, string, error) {
	mr, block := mrkdwn.Convert(text)
	opts := []slack.MsgOption{slack.MsgOptionText(mr, false)}
	if block != nil {
		opts = append(opts, slack.MsgOptionBlocks(block))
	}
	_, ts, err := c.api.PostMessage(channelID, opts...)
	if err != nil {
		return "", "", fmt.Errorf("sending message: %w", err)
	}
	return ts, mr, nil
}

// OpenConversation opens (or returns) a direct message channel (1 user)
// or a multi-person direct message / MPIM (2-8 users). Idempotent:
// when the conversation already exists, Slack returns it with
// alreadyOpen=true.
//
// Defends inputs: rejects 0 users or more than 8. Slack's hard cap on
// MPIM size is 9 participants total, so up to 8 OTHER user IDs.
func (c *Client) OpenConversation(ctx context.Context, userIDs []string) (string, bool, error) {
	if len(userIDs) == 0 {
		return "", false, fmt.Errorf("opening conversation: at least one user ID required")
	}
	if len(userIDs) > 8 {
		return "", false, fmt.Errorf("opening conversation: at most 8 user IDs allowed (got %d)", len(userIDs))
	}
	ch, _, alreadyOpen, err := c.api.OpenConversationContext(ctx, &slack.OpenConversationParameters{
		Users:    userIDs,
		ReturnIM: true,
	})
	if err != nil {
		return "", false, fmt.Errorf("opening conversation: %w", err)
	}
	return ch.ID, alreadyOpen, nil
}

// UploadFile uploads a single file to a channel (and optional thread)
// using Slack's V2 external-upload flow. The slack-go library's
// UploadFileContext (named for the underlying file.upload.v2 API)
// handles the three internal steps:
// getUploadURLExternal -> PUT -> completeUploadExternal.
//
// caption, when non-empty, is attached as the file's initial_comment.
// For multi-file batches the caller should set caption on the LAST
// file only (Slack groups files completed in one share into one
// message; sequential single-file uploads can't be grouped).
//
// size is int64 (matching os.FileInfo.Size()) and is narrowed to int
// for slack-go. Callers must enforce a reasonable upper bound; this
// wrapper does not.
func (c *Client) UploadFile(
	ctx context.Context,
	channelID, threadTS, filename string,
	r io.Reader,
	size int64,
	caption string,
) (*slack.FileSummary, error) {
	params := slack.UploadFileParameters{
		Filename: filename,
		Reader:   r,
		FileSize: int(size),
		Channel:  channelID,
	}
	if threadTS != "" {
		params.ThreadTimestamp = threadTS
	}
	if caption != "" {
		params.InitialComment = caption
	}
	f, err := c.api.UploadFileContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("uploading file %q: %w", filename, err)
	}
	return f, nil
}

// UnreadInfo holds the unread state for a single channel.
type UnreadInfo struct {
	ChannelID string
	Count     int
	HasUnread bool
	LastRead  string // Slack message timestamp
	// Latest is the channel's newest message timestamp as Slack knows
	// it. It is what client.counts compares against LastRead to decide
	// HasUnread, and it can name a message conversations.history will
	// not return -- on a retention-limited plan the message that makes
	// a channel unread may have aged out of the fetchable window.
	// Empty when Slack omits it.
	Latest string
}

// ThreadsAggregate captures Slack's server-side notion of whether the
// user has any unread thread activity at all, plus the mention/unread
// counts. The local SQLite cache has no per-thread read state, so the
// threads-list "Unread" flag is computed by a heuristic that can
// produce false positives (e.g., a thread reply was read in another
// Slack client but the parent channel's last_read_ts predates it).
// HasUnreads from this struct is the authoritative signal that lets us
// suppress those false positives on startup.
type ThreadsAggregate struct {
	HasUnreads   bool
	UnreadCount  int
	MentionCount int
}

// GetUnreadCounts fetches unread counts for all channels using Slack's
// internal client.counts API (available with xoxc browser tokens).
// Also returns the threads aggregate (HasUnreads / counts) for the
// workspace; the threads block in client.counts is the only place
// Slack tells us whether per-thread unreads exist without us having to
// hit subscriptions.thread.* directly.
func (c *Client) GetUnreadCounts() ([]UnreadInfo, ThreadsAggregate, error) {
	reqURL := c.apiBaseURL + "client.counts"
	form := url.Values{"token": {c.token}}
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, ThreadsAggregate{}, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.apiHTTPClient().Do(req)
	if err != nil {
		return nil, ThreadsAggregate{}, fmt.Errorf("fetching unread counts: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ThreadsAggregate{}, fmt.Errorf("reading response: %w", err)
	}

	var result struct {
		OK       bool `json:"ok"`
		Channels []struct {
			ID                 string `json:"id"`
			HasUnreads         bool   `json:"has_unreads"`
			MentionCount       int    `json:"mention_count"`
			UnreadCountDisplay int    `json:"unread_count_display,omitempty"`
			LastRead           string `json:"last_read"`
			Latest             string `json:"latest"`
		} `json:"channels"`
		Mpims []struct {
			ID           string `json:"id"`
			HasUnreads   bool   `json:"has_unreads"`
			MentionCount int    `json:"mention_count"`
			LastRead     string `json:"last_read"`
			Latest       string `json:"latest"`
		} `json:"mpims"`
		Ims []struct {
			ID         string `json:"id"`
			HasUnreads bool   `json:"has_unreads"`
			LastRead   string `json:"last_read"`
			Latest     string `json:"latest"`
		} `json:"ims"`
		// Threads is the workspace-wide thread-subscription rollup.
		// Slack returns this top-level when the user has subscribed
		// threads (i.e., authored, replied to, or @-mentioned in any
		// thread). HasUnreads here is the authoritative "do I have any
		// unread thread activity?" signal.
		Threads struct {
			HasUnreads   bool `json:"has_unreads"`
			UnreadCount  int  `json:"unread_count"`
			MentionCount int  `json:"mention_count"`
		} `json:"threads"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, ThreadsAggregate{}, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		return nil, ThreadsAggregate{}, fmt.Errorf("client.counts API returned ok=false")
	}

	var unreads []UnreadInfo
	for _, ch := range result.Channels {
		info := UnreadInfo{
			ChannelID: ch.ID,
			LastRead:  ch.LastRead,
			HasUnread: ch.HasUnreads,
			Latest:    ch.Latest,
		}
		if ch.HasUnreads {
			info.Count = ch.MentionCount
			if info.Count == 0 {
				info.Count = 1 // has unreads but no mention count
			}
		}
		unreads = append(unreads, info)
	}
	for _, ch := range result.Mpims {
		info := UnreadInfo{
			ChannelID: ch.ID,
			LastRead:  ch.LastRead,
			HasUnread: ch.HasUnreads,
			Latest:    ch.Latest,
		}
		if ch.HasUnreads {
			info.Count = max(ch.MentionCount, 1)
		}
		unreads = append(unreads, info)
	}
	for _, ch := range result.Ims {
		info := UnreadInfo{
			ChannelID: ch.ID,
			LastRead:  ch.LastRead,
			HasUnread: ch.HasUnreads,
			Latest:    ch.Latest,
		}
		if ch.HasUnreads {
			info.Count = 1
		}
		unreads = append(unreads, info)
	}

	threads := ThreadsAggregate{
		HasUnreads:   result.Threads.HasUnreads,
		UnreadCount:  result.Threads.UnreadCount,
		MentionCount: result.Threads.MentionCount,
	}
	return unreads, threads, nil
}

// SendReply posts a threaded reply to the specified message.
// Returns the timestamp and the converted mrkdwn text actually sent.
func (c *Client) SendReply(ctx context.Context, channelID, threadTS, text string) (string, string, error) {
	mr, block := mrkdwn.Convert(text)
	opts := []slack.MsgOption{
		slack.MsgOptionText(mr, false),
		slack.MsgOptionTS(threadTS),
	}
	if block != nil {
		opts = append(opts, slack.MsgOptionBlocks(block))
	}
	_, ts, err := c.api.PostMessage(channelID, opts...)
	if err != nil {
		return "", "", fmt.Errorf("sending reply: %w", err)
	}
	return ts, mr, nil
}

// GetReplies retrieves all replies in a thread.
// The first message in the returned slice is the parent message.
func (c *Client) GetReplies(ctx context.Context, channelID, threadTS string) ([]slack.Message, error) {
	var allMessages []slack.Message
	cursor := ""

	for {
		msgs, hasMore, nextCursor, err := c.api.GetConversationReplies(&slack.GetConversationRepliesParameters{
			ChannelID: channelID,
			Timestamp: threadTS,
			Cursor:    cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("getting thread replies: %w", err)
		}
		allMessages = append(allMessages, msgs...)
		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return allMessages, nil
}

// EditMessage updates an existing message's text. Returns the
// converted mrkdwn text that was sent (callers may use it for
// optimistic display, but the message-changed WS echo is the
// authoritative source of truth for the displayed body).
func (c *Client) EditMessage(ctx context.Context, channelID, ts, text string) (string, error) {
	mr, block := mrkdwn.Convert(text)
	opts := []slack.MsgOption{slack.MsgOptionText(mr, false)}
	if block != nil {
		opts = append(opts, slack.MsgOptionBlocks(block))
	}
	_, _, _, err := c.api.UpdateMessage(channelID, ts, opts...)
	if err != nil {
		return "", fmt.Errorf("editing message: %w", err)
	}
	return mr, nil
}

// RemoveMessage deletes a message from the channel.
func (c *Client) RemoveMessage(ctx context.Context, channelID, ts string) error {
	_, _, err := c.api.DeleteMessage(channelID, ts)
	if err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

// AddReaction adds an emoji reaction to a message.
func (c *Client) AddReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.AddReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

// RemoveReaction removes an emoji reaction from a message.
func (c *Client) RemoveReaction(ctx context.Context, channelID, ts, emoji string) error {
	return c.api.RemoveReaction(emoji, slack.ItemRef{Channel: channelID, Timestamp: ts})
}

// SetUserPresence sets the authenticated user's presence. Accepts "auto"
// (let Slack determine activity) or "away" (force away). Note the write
// vocabulary differs from the read side — GetUserPresence and the
// presence_change WebSocket event return "active" or "away".
func (c *Client) SetUserPresence(ctx context.Context, presence string) error {
	if err := c.api.SetUserPresenceContext(ctx, presence); err != nil {
		return fmt.Errorf("setting presence: %w", err)
	}
	return nil
}

// GetUserPresence fetches a user's current presence ("active" or "away").
// Pass the authenticated user's ID to read your own state.
func (c *Client) GetUserPresence(ctx context.Context, userID string) (*slack.UserPresence, error) {
	p, err := c.api.GetUserPresenceContext(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("getting presence: %w", err)
	}
	return p, nil
}

// SetSnooze enables Do-Not-Disturb for `minutes` minutes.
func (c *Client) SetSnooze(ctx context.Context, minutes int) (*slack.DNDStatus, error) {
	st, err := c.api.SetSnoozeContext(ctx, minutes)
	if err != nil {
		return nil, fmt.Errorf("setting snooze: %w", err)
	}
	return st, nil
}

// EndSnooze ends the current snooze window. Does NOT end admin-scheduled DND.
func (c *Client) EndSnooze(ctx context.Context) (*slack.DNDStatus, error) {
	st, err := c.api.EndSnoozeContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("ending snooze: %w", err)
	}
	return st, nil
}

// EndDND ends the user's current scheduled DND session.
func (c *Client) EndDND(ctx context.Context) error {
	if err := c.api.EndDNDContext(ctx); err != nil {
		return fmt.Errorf("ending DND: %w", err)
	}
	return nil
}

// GetDNDInfo fetches DND/snooze status for a user.
func (c *Client) GetDNDInfo(ctx context.Context, userID string) (*slack.DNDStatus, error) {
	u := userID
	st, err := c.api.GetDNDInfoContext(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("getting DND info: %w", err)
	}
	return st, nil
}

// GetPermalink returns the Slack permalink for a message. For a thread reply,
// pass the reply's ts; Slack returns a thread-aware URL with thread_ts and cid
// query parameters.
func (c *Client) GetPermalink(ctx context.Context, channelID, ts string) (string, error) {
	url, err := c.api.GetPermalinkContext(ctx, &slack.PermalinkParameters{
		Channel: channelID,
		Ts:      ts,
	})
	if err != nil {
		return "", fmt.Errorf("getting permalink: %w", err)
	}
	return url, nil
}

// markChannel posts to conversations.mark with the given form values.
// Used by both MarkChannel (read up to ts) and MarkChannelUnread (roll the
// watermark backward to ts). Uses c.httpClient for the request so tests
// can substitute an httptest.NewServer; production wiring (NewClient) sets
// httpClient to a cookie-bearing client.
func (c *Client) markChannel(ctx context.Context, channelID, ts string) error {
	data := url.Values{
		"token":   {c.token},
		"channel": {channelID},
		"ts":      {ts},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiBaseURL+"conversations.mark",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("marking channel: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// markThread posts to subscriptions.thread.mark with the given args.
// Used by both MarkThread (read=true => "1") and MarkThreadUnread
// (read=false => "0"). channelID/threadTS empty is a no-op. ts defaults
// to threadTS when empty (parent has no replies yet).
func (c *Client) markThread(ctx context.Context, channelID, threadTS, ts string, read bool) error {
	if channelID == "" || threadTS == "" {
		return nil
	}
	if ts == "" {
		ts = threadTS
	}
	readVal := "0"
	if read {
		readVal = "1"
	}
	data := url.Values{
		"token":     {c.token},
		"channel":   {channelID},
		"thread_ts": {threadTS},
		"ts":        {ts},
		"read":      {readVal},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiBaseURL+"subscriptions.thread.mark",
		strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("creating thread mark request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("marking thread: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// MarkChannel marks a channel as read up to the given timestamp.
func (c *Client) MarkChannel(ctx context.Context, channelID, ts string) error {
	return c.markChannel(ctx, channelID, ts)
}

// MarkThread marks a thread as read up to the given timestamp using Slack's
// undocumented subscriptions.thread.mark endpoint (the same call the official
// web client makes when you view a thread). channelID is the parent channel,
// threadTS is the parent message ts, and ts is the latest reply ts the user
// has now seen (use threadTS itself when there are no replies). Best-effort:
// the endpoint is undocumented and may break if Slack changes its API.
func (c *Client) MarkThread(ctx context.Context, channelID, threadTS, ts string) error {
	return c.markThread(ctx, channelID, threadTS, ts, true)
}

// MarkChannelUnread rolls the channel's read watermark backward to ts,
// effectively making the message at ts and every newer message in the
// channel unread again. Pass ts == "" to mark the entire channel unread
// (Slack's "0" sentinel).
func (c *Client) MarkChannelUnread(ctx context.Context, channelID, ts string) error {
	if ts == "" {
		ts = "0"
	}
	return c.markChannel(ctx, channelID, ts)
}

// MarkThreadUnread marks a thread as unread starting at ts using Slack's
// subscriptions.thread.mark endpoint with read=0. Mirrors MarkThread but
// flips the read flag. channelID is the parent channel, threadTS is the
// parent message ts, and ts is the reply that should become the new
// "first unread" boundary (use threadTS to mark the entire thread
// unread when there are no replies). Best-effort.
func (c *Client) MarkThreadUnread(ctx context.Context, channelID, threadTS, ts string) error {
	return c.markThread(ctx, channelID, threadTS, ts, false)
}

// GetMutedChannels fetches the authenticated user's mute set by
// reading users.prefs.get and parsing the per-channel notification
// prefs blob. Returns the IDs of channels the user has muted.
//
// Slack does NOT ship a flat `muted_channels` pref anymore (it used
// to, and is still documented as such in some places, but live
// browser-protocol responses no longer include it). Mute state lives
// inside the JSON-encoded `all_notifications_prefs` string under
// channels[id].muted=true. After this initial fetch, pref_change WS
// events for `all_notifications_prefs` keep the set fresh.
//
// users.prefs.get is undocumented but is the same call the official
// browser client uses; may break if Slack changes the API. Returns an
// empty slice (not nil) when the user has no muted channels.
func (c *Client) GetMutedChannels(ctx context.Context) ([]string, error) {
	body, err := c.postForm(ctx, "users.prefs.get", nil)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Prefs struct {
			// Legacy — accept it if Slack ever ships it again, so this
			// code keeps working on older workspaces.
			MutedChannels        string `json:"muted_channels"`
			AllNotificationPrefs string `json:"all_notifications_prefs"`
		} `json:"prefs"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing users.prefs.get response: %w (body=%q)", err, truncateForLog(body))
	}
	if !parsed.OK {
		return nil, fmt.Errorf("users.prefs.get returned ok=false (error=%q, body=%q)", parsed.Error, truncateForLog(body))
	}

	merged := map[string]bool{}
	for _, id := range strings.Split(parsed.Prefs.MutedChannels, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			merged[id] = true
		}
	}
	for _, id := range ParseMutedFromAllNotificationsPrefs(parsed.Prefs.AllNotificationPrefs) {
		merged[id] = true
	}
	debuglog.WS("users.prefs.get: muted_channels=%d all_notifications_prefs_len=%d total_muted=%d",
		len(parsed.Prefs.MutedChannels), len(parsed.Prefs.AllNotificationPrefs), len(merged))
	out := make([]string, 0, len(merged))
	for id := range merged {
		out = append(out, id)
	}
	return out, nil
}

// ParseMutedFromAllNotificationsPrefs decodes the JSON-string value
// of the `all_notifications_prefs` pref and returns the channel IDs
// where channels[id].muted == true. The pref's value is itself a
// JSON-encoded string (Slack quirk), so callers should pass the raw
// string contents directly. Returns an empty slice on any decode
// failure — mute is best-effort UI sugar, not safety-critical.
func ParseMutedFromAllNotificationsPrefs(raw string) []string {
	if raw == "" {
		return nil
	}
	var parsed struct {
		Channels map[string]struct {
			Muted bool `json:"muted"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	out := make([]string, 0, len(parsed.Channels))
	for id, prefs := range parsed.Channels {
		if prefs.Muted {
			out = append(out, id)
		}
	}
	return out
}

// GetMutedChannelsRaw returns the raw users.prefs.get response body.
// Diagnostic only (--dump-prefs).
func (c *Client) GetMutedChannelsRaw(ctx context.Context) ([]byte, error) {
	return c.postForm(ctx, "users.prefs.get", nil)
}

// shouldReloadResponse is the subset of client.shouldReload's response
// slk reads. The full response also carries should_reload,
// build_version_enabled, client_min_version,
// build_manifest_last_modified and should_fetch_new_service_worker —
// all about whether a browser tab needs to refresh its JS bundle, which
// a TUI has no equivalent of.
type shouldReloadResponse struct {
	OK                      bool   `json:"ok"`
	Error                   string `json:"error"`
	RecommendedBuildVersion int64  `json:"recommended_build_version"`
}

// ShouldReload asks Slack which client build is current and returns
// that build timestamp as a decimal string, suitable for
// Envelope.SetVersionTS.
//
// _x_version_ts is Slack's build timestamp, and it moves: two captures
// taken hours apart on 2026-07-30 carried 1785403052 and 1785403654.
// A client that sends one hardcoded value forever is itself a signal —
// it stays pinned to a build the fleet has moved off of — so slk learns
// the current value the way the official client does.
//
// Why this endpoint rather than scraping the workspace page: slk used
// to fetch that page, and commit da6a7e1 deliberately removed the
// fetch. Issue #111 showed corporate proxies reject that navigation
// with 403, breaking startup outright. client.shouldReload is a plain
// form POST under /api/ — the same shape as every other call slk
// already makes, through the same proxy-tolerant path — and it appears
// in both official boot captures, so it is both safer and a closer
// match to real client traffic.
//
// The envelope is deliberately NOT mutated here: the caller decides
// whether to store the result, so a failed or nonsense lookup cannot
// clobber a good value mid-session.
func (c *Client) ShouldReload(ctx context.Context) (string, error) {
	// The official client sends _x_reason=boot on this call;
	// BrowserTransport reads it back off the context.
	ctx = slackhttp.WithReason(ctx, "boot")

	form := url.Values{}
	if c.teamID != "" {
		form.Set("team_ids", c.teamID)
	}
	if c.envelope != nil {
		// The build slk currently believes is current. The official
		// client sends the same, so the server can tell it whether to
		// reload.
		form.Set("build_version_ts", c.envelope.VersionTS())
	}

	raw, err := c.postForm(ctx, "client.shouldReload", form)
	if err != nil {
		return "", fmt.Errorf("client.shouldReload: %w", err)
	}

	var srr shouldReloadResponse
	if err := json.Unmarshal(raw, &srr); err != nil {
		return "", fmt.Errorf("client.shouldReload: parsing response: %w (body: %s)", err, truncateForLog(raw))
	}
	if !srr.OK {
		return "", fmt.Errorf("client.shouldReload: API error: %s", srr.Error)
	}
	if srr.RecommendedBuildVersion <= 0 {
		// Succeeding with a zero here would hand the caller "0" as a
		// build timestamp, which is worse than the stale default.
		return "", fmt.Errorf("client.shouldReload: no recommended_build_version in response (body: %s)", truncateForLog(raw))
	}
	return strconv.FormatInt(srr.RecommendedBuildVersion, 10), nil
}

// rateLimitError is what postForm returns on HTTP 429, carrying the
// server-advised Retry-After so callers can sleep and retry the same
// page instead of failing the whole sweep. Slack's internal endpoints
// answer 429 (not ok=false ratelimited) when a paginated loop outruns
// the limiter.
type rateLimitError struct {
	method     string
	retryAfter time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("calling %s: rate limited (HTTP 429), retry after %s", e.method, e.retryAfter)
}

// postForm performs a cookie-aware POST to an endpoint under
// c.apiBaseURL with form values. The xoxc token is injected into the
// form body — the same convention slack-go and the official browser
// client use. Returns the raw response body. Shared by hand-rolled
// undocumented endpoints.
func (c *Client) postForm(ctx context.Context, method string, form url.Values) ([]byte, error) {
	// Inject the xoxc token into the form body — the same convention
	// slack-go and the official browser client use. Using
	// Authorization: Bearer here is an OAuth/server-side pattern that
	// browsers never send to app.slack.com; combined with our browser-
	// shaped headers it forms a contradictory signature that triggers
	// Enterprise Grid anomaly detection (issue #5).
	//
	// Copy the caller's map so we don't mutate it across retries or
	// concurrent calls.
	body := url.Values{"token": {c.token}}
	for k, vs := range form {
		body[k] = vs
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiBaseURL+method, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.apiHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", method, err)
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	// Status is checked before the read error so a truncated body on a
	// 500 reports the 500, which is the useful half of the diagnosis.
	// Slack's Web API answers 200 even for {"ok":false,...}, so any
	// non-2xx here is a transport/proxy/edge failure whose body is HTML
	// at best: reporting it as a JSON parse failure at the call site
	// (as this used to) hides what actually happened.
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &rateLimitError{
			method:     method,
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), 5*time.Second),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("calling %s: HTTP %s: %s", method, resp.Status, truncateForLog(raw))
	}
	if readErr != nil {
		return nil, fmt.Errorf("reading %s response: %w", method, readErr)
	}
	return raw, nil
}

// truncateForLog clips a response body to a length safe to splat into
// an error message or log line. Hand-rolled endpoints occasionally
// return multi-KB HTML error pages; without truncation those would
// blow out a single log line.
func truncateForLog(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "...(truncated)"
}

// callChannelSectionsList performs the raw POST to users.channelSections.list
// using cookie-aware auth (xoxc token in the form body + d cookie) and an
// optional cursor for pagination through sections. Shared by both the typed
// and raw accessors.
func (c *Client) callChannelSectionsList(ctx context.Context, cursor string) ([]byte, error) {
	endpoint := c.apiBaseURL + "users.channelSections.list"

	form := url.Values{"token": {c.token}}
	if cursor != "" {
		form.Set("cursor", cursor)
	}
	body := strings.NewReader(form.Encode())

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.apiHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling channelSections API: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// GetChannelSectionsRaw calls users.channelSections.list with no cursor and
// returns the raw JSON response body. Diagnostic only (--dump-sections).
func (c *Client) GetChannelSectionsRaw(ctx context.Context) ([]byte, error) {
	return c.callChannelSectionsList(ctx, "")
}

// starsListResponse is the JSON shape returned by stars.list. Only the
// channel-typed items are relevant for the sidebar; message/file/IM stars
// are ignored.
type starsListResponse struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error"`
	Items  []starsListItem `json:"items"`
	Paging starsListPaging `json:"paging"`
}

type starsListItem struct {
	Type    string `json:"type"`
	Channel string `json:"channel"` // populated when Type == "channel" or "im"
}

type starsListPaging struct {
	Count int `json:"count"`
	Total int `json:"total"`
}

// GetStarredChannels calls stars.list and returns the IDs of channels the
// user has starred. Slack's users.channelSections.list returns the stars
// section with an empty channel_ids array (it doesn't populate built-in
// section types); stars.list is the authoritative source for starred
// channels. Only type=="channel" items are returned — message/file/IM stars
// don't belong in the channel sidebar.
//
// Best-effort: on error the caller proceeds with an empty star list and
// the stars section stays hidden (no regression vs pre-fix behavior).
func (c *Client) GetStarredChannels(ctx context.Context) ([]string, error) {
	form := url.Values{"token": {c.token}, "limit": {"1000"}}
	body := strings.NewReader(form.Encode())
	endpoint := c.apiBaseURL + "stars.list"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.apiHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling stars.list: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading stars.list response: %w", err)
	}
	var slr starsListResponse
	if err := json.Unmarshal(raw, &slr); err != nil {
		return nil, fmt.Errorf("parsing stars.list response: %w", err)
	}
	if !slr.OK {
		return nil, fmt.Errorf("stars.list API error: %s", slr.Error)
	}
	var ids []string
	for _, it := range slr.Items {
		if it.Type == "channel" && it.Channel != "" {
			ids = append(ids, it.Channel)
		}
	}
	return ids, nil
}

// GetChannelSections calls users.channelSections.list and returns the
// fully-paginated section list. Loops on the top-level cursor until the
// server reports no more sections. Per-section channel_ids_page pagination
// is NOT followed here — see ListSectionChannels.
//
// This endpoint is undocumented; may break if Slack changes the API.
func (c *Client) GetChannelSections(ctx context.Context) ([]SidebarSection, error) {
	var all []SidebarSection
	cursor := ""
	for {
		body, err := c.callChannelSectionsList(ctx, cursor)
		if err != nil {
			return nil, err
		}
		var resp channelSectionsListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}
		if !resp.OK {
			return nil, fmt.Errorf("API error: %s (response: %s)", resp.Error, string(body))
		}
		all = append(all, resp.Sections...)
		// Slack's documented contract is empty cursor on the last page.
		// Bail also when the server echoes back the same cursor we just
		// sent — defensive against server bugs that would otherwise infinite-loop
		// against an undocumented endpoint.
		if resp.Cursor == "" || resp.Cursor == cursor {
			break
		}
		cursor = resp.Cursor
	}
	return all, nil
}

// ThreadSubscription is the slk-side projection of one subscribed
// thread returned by subscriptions.thread.getView. The five fields
// here map cleanly onto cache.ThreadSubscription. The caller in
// cmd/slk/reconnect_backfill.go does the adapter cast.
type ThreadSubscription struct {
	ChannelID string
	ThreadTS  string
	LastRead  string
	Active    bool
}

// ThreadSubscriptionView is one item from subscriptions.thread.getView.
// It carries both the subscription-state projection (Subscription) and
// the full parent message Slack ships inside root_msg
// (RootMessage). The subscription-phase backfiller uses Subscription
// to upsert the thread_subscriptions row and RootMessage to upsert
// the parent into the messages cache, eliminating the need for a
// follow-up conversations.replies fetch when the parent isn't
// already cached.
type ThreadSubscriptionView struct {
	Subscription ThreadSubscription
	RootMessage  slack.Message
}

// listThreadSubscriptionsResponse decodes one page of
// subscriptions.thread.getView.
type listThreadSubscriptionsResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error"`
	Threads []struct {
		// RootMsg is decoded twice: once into the typed
		// slackThreadRootMsg shape for the channel/last_read/subscribed
		// fields we need, and once into a slack.Message via json.RawMessage
		// so the caller can re-marshal it for the messages cache.
		RootMsg json.RawMessage `json:"root_msg"`
	} `json:"threads"`
	HasMore bool   `json:"has_more"`
	MaxTS   string `json:"max_ts"`
}

// slackThreadRootMsg is the subset of root_msg fields the
// subscription-phase reconcile needs. The rest of root_msg flows
// through as slack.Message via the raw JSON re-parse.
type slackThreadRootMsg struct {
	Channel    string `json:"channel"`
	TS         string `json:"ts"`
	ThreadTS   string `json:"thread_ts"`
	LastRead   string `json:"last_read"`
	Subscribed bool   `json:"subscribed"`
}

// listThreadSubscriptionsHardCap bounds how many subscriptions
// ListThreadSubscriptions will return per call. Protects against
// runaway requests if Slack ships a buggy has_more flag.
const listThreadSubscriptionsHardCap = 1000

// ListThreadSubscriptions fetches the workspace's full subscribed-
// threads list via Slack's internal subscriptions.thread.getView
// endpoint (the same call the official web client makes when
// bootstrapping its Threads view). Paginates via the `current_ts`
// form field (set to the previous response's max_ts), terminated by
// has_more=false. Stops at listThreadSubscriptionsHardCap items.
//
// Items where root_msg.subscribed is false are filtered out —
// defensive, since the live endpoint hasn't been observed returning
// them.
//
// Returns (nil, err) on network failure or ok=false JSON. The caller
// (the reconnect backfill phase) treats any error as "subscriptions
// unavailable" and surfaces the UI banner.
// listThreadSubscriptionsMaxRateLimitRetries bounds how often one
// paginated sweep retries a 429ing page. The caller is a background
// sync; an endpoint that keeps limiting after a few server-advised
// waits is failed back to the UI banner and the caller's throttle
// window instead of sleeping forever.
const listThreadSubscriptionsMaxRateLimitRetries = 3

func (c *Client) ListThreadSubscriptions(ctx context.Context) ([]ThreadSubscriptionView, error) {
	var all []ThreadSubscriptionView
	currentTS := ""
	rateLimitRetries := 0
	for {
		body, err := c.callListThreadSubscriptions(ctx, currentTS)
		if err != nil {
			var rl *rateLimitError
			if errors.As(err, &rl) && rateLimitRetries < listThreadSubscriptionsMaxRateLimitRetries {
				rateLimitRetries++
				// Retry the SAME page: currentTS deliberately does
				// not advance.
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(rl.retryAfter):
				}
				continue
			}
			return nil, err
		}
		rateLimitRetries = 0
		var resp listThreadSubscriptionsResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing subscriptions.thread.getView: %w (body=%s)", err, truncateForLog(body))
		}
		if !resp.OK {
			return nil, fmt.Errorf("subscriptions.thread.getView: %s (body=%s)", resp.Error, truncateForLog(body))
		}
		for _, item := range resp.Threads {
			var sm slackThreadRootMsg
			if err := json.Unmarshal(item.RootMsg, &sm); err != nil {
				// Skip malformed items but keep paginating.
				debuglog.Backfill("ListThreadSubscriptions: skipping malformed root_msg: %v", err)
				continue
			}
			if !sm.Subscribed {
				continue
			}
			var raw slack.Message
			if err := json.Unmarshal(item.RootMsg, &raw); err != nil {
				// Couldn't decode the rich message; skip so we don't
				// corrupt the messages cache, but the subscription row
				// is still useful — fall back to a synthetic empty
				// slack.Message so the caller can still record the row.
				debuglog.Backfill("ListThreadSubscriptions: root_msg slack.Message decode err=%v; subscription kept without RootMessage", err)
				raw = slack.Message{}
			}
			all = append(all, ThreadSubscriptionView{
				Subscription: ThreadSubscription{
					ChannelID: sm.Channel,
					ThreadTS:  sm.ThreadTS,
					LastRead:  sm.LastRead,
					Active:    sm.Subscribed,
				},
				RootMessage: raw,
			})
			if len(all) >= listThreadSubscriptionsHardCap {
				debuglog.Backfill("ListThreadSubscriptions: hit hard cap %d, stopping", listThreadSubscriptionsHardCap)
				return all, nil
			}
		}
		if !resp.HasMore || resp.MaxTS == "" {
			break
		}
		// Note: unlike GetChannelSections we do NOT bail when the
		// server echoes back the same MaxTS — the
		// listThreadSubscriptionsHardCap above is the runaway-protection
		// mechanism for this endpoint, and the hard-cap test exercises
		// exactly that case (server returns has_more=true with an
		// unchanging max_ts forever).
		currentTS = resp.MaxTS
	}
	return all, nil
}

func (c *Client) callListThreadSubscriptions(ctx context.Context, currentTS string) ([]byte, error) {
	form := url.Values{}
	form.Set("limit", "100")
	form.Set("fetch_threads_state", "true")
	form.Set("priority_mode", "all")
	if currentTS != "" {
		form.Set("current_ts", currentTS)
	}
	return c.postForm(ctx, "subscriptions.thread.getView", form)
}

// historyMethod is the API method name. It is also the key slackhttp
// looks up in its _x_reason and _x_mode tables, and
// conversations.history is in NEITHER exclusion set — unlike
// client.userBoot and conversations.view, this endpoint carries both
// flags, on 14 of 14 captured requests.
const historyMethod = "conversations.history"

// historyReason is the _x_reason the official client sends on 11 of the
// 14 captured conversations.history requests. The other 3 carry
// "unread-counts/onLastReadUpdated", which reason.go documents as a
// caller-supplied override rather than a second table entry — hence the
// conditional in HistoryWithVersions.
const historyReason = "message-pane/requestHistory"

// defaultHistoryLimit is the page size the official web client asks
// for: 28, on 14 of 14 captured conversations.history requests. Not 50,
// not 200, not 500 — the three sizes slk's existing history paths use.
//
// It is 28 because that is what was measured, not because 28 is a good
// number, and it must not be "tuned".
const defaultHistoryLimit = 28

// emptyCachedLatestUpdates is what cached_latest_updates carries when
// the caller holds no cached messages: the literal two-character JSON
// object, on 11 of the 14 captured requests. The key is never omitted —
// omitting it is a request shape the official client emits zero times.
//
// A constant rather than json.Marshal of an empty map, because
// json.Marshal of a nil map[string]string produces "null", not "{}",
// and "null" is a third shape no capture shows.
const emptyCachedLatestUpdates = "{}"

// HistoryOpts are the caller-varying parameters of HistoryWithVersions.
// Everything not here is fixed by the captures and is not configurable.
type HistoryOpts struct {
	// Limit is the page size. Non-positive means defaultHistoryLimit
	// (28), the only value the official client was ever observed
	// sending.
	//
	// A non-zero Limit is sent VERBATIM. It is deliberately not
	// clamped to 28, and the reason is not timidity about changing
	// caller-visible behaviour:
	//
	//  1. Clamping lies about data. A caller that asks for 200 and
	//     silently receives 28 concludes the channel holds 28
	//     messages. That misinformation propagates into caches,
	//     watermarks and the UI, and nothing anywhere can detect it.
	//     A fingerprint divergence, by contrast, is visible in the
	//     source: somebody typed 200 at a call site, and a reviewer
	//     can see it.
	//
	//  2. Clamping does not even remove the fingerprint. A caller that
	//     needs 200 messages and gets 28 will loop eight times. That
	//     turns one anomalous request into a burst of eight
	//     well-shaped ones — and burst request volume is precisely
	//     what Enterprise Grid's anomaly detection scores. Clamping
	//     would trade the fingerprint this phase is removing for the
	//     one it is removing harder.
	//
	// The fingerprint concern is answered by the default instead:
	// every caller that does not think about page size gets 28.
	//
	// This mirrors edge.UsersList, which documents its count rather
	// than bounding it, on the grounds that a silent truncation lies
	// to the caller. The counter-argument is real here in a way it was
	// not there — 28 is 14/14, whereas UsersList's captures disagreed
	// 30/30/30/20 — but "the observed value is unanimous" argues for a
	// firm default, which this has, not for overriding an explicit
	// caller.
	//
	// The asymmetry with the non-positive case is the same one
	// edge.UsersList draws: `limit=0` or `limit=-5` is a knowably
	// wrong SHAPE that no capture shows and the server can only
	// reject, whereas an unusually large page is a judgement about
	// volume with no observed threshold behind it. Correcting the
	// first is enforcing the captures; bounding the second would be
	// enforcing a number invented here.
	Limit int

	// Latest and Oldest are the window anchors. Exactly one, or
	// neither, in every capture: oldest on 5 requests, latest on 4,
	// neither on 5, both on 0. Each is omitted entirely when empty —
	// `latest=` on the wire is a shape no capture shows.
	//
	// Setting both is not rejected, because the server's behaviour
	// with both is unmeasured and erroring would be enforcing a rule
	// invented here rather than one read off the captures. It is
	// nonetheless a shape the official client never emits; don't.
	Latest string
	Oldest string

	// CachedVersions is the {ts: version} map for the messages the
	// caller already holds — the whole point of this method. The
	// server answers with unchanged_messages, confirming which of them
	// are still current, plus the bodies of only those that actually
	// changed. It is what makes validating cached scrollback cheap
	// instead of a full re-download of every channel on every boot and
	// every reconnect.
	//
	// Nil and empty both serialise to "{}", which is what the client
	// sends when it holds nothing. The map is not retained or
	// mutated.
	CachedVersions map[string]string

	// IncludeDateJoined is a genuine caller parameter, not a constant:
	// true on 8 of 14 captured requests and false on 6. It is sent
	// either way — the key is present on 14 of 14, carrying the
	// literal string "false" when off, never omitted. The response's
	// `date_joined` object appears on exactly the 8 requests that set
	// it, and is not modelled here because nothing in slk consumes it.
	IncludeDateJoined bool
}

// HistoryResult is the subset of the conversations.history response
// this method models.
//
// Four of the eight keys present on 14 of 14 responses. pin_count,
// channel_actions_ts and channel_actions_count are unmodelled because
// nothing in slk consumes them; response_metadata is unmodelled because
// following its next_cursor in a loop is the enumeration this phase
// exists to stop, and a cursor sitting in the return values is an
// invitation to write that loop. Adding any of them later is a two-line
// change; inventing a consumer for them now is not.
type HistoryResult struct {
	// Messages are the message bodies the server actually sent. With a
	// populated CachedVersions this is only what CHANGED — in the
	// scroll capture, one cached ts came back in UnchangedTS and 27
	// bodies arrived here.
	Messages []slack.Message

	// UnchangedTS lists the timestamps from the request's
	// cached_latest_updates that the server confirms the caller still
	// holds correctly. These are ts strings only; the caller already
	// has the bodies. This is the response's `unchanged_messages`.
	UnchangedTS []string

	// LatestUpdates is {ts: version} for the returned messages — the
	// values to feed back as CachedVersions on the next call. Versions
	// are opaque 17-character ts-like strings; they are NOT
	// timestamps to be parsed or compared, only echoed.
	LatestUpdates map[string]string

	// HasMore reports whether the window the caller asked for was
	// truncated by Limit.
	HasMore bool
}

// historyWithVersionsResponse is the wire shape. Separate from
// HistoryResult so `ok` and `error` do not leak into the caller's type.
type historyWithVersionsResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`

	Messages      []slack.Message   `json:"messages"`
	UnchangedTS   []string          `json:"unchanged_messages"`
	LatestUpdates map[string]string `json:"latest_updates"`
	HasMore       bool              `json:"has_more"`
}

// HistoryWithVersions fetches channel history using Slack's incremental
// sync primitive: the caller sends {ts: version} for the messages it
// already holds, and the server replies with unchanged_messages — the
// subset it confirms are still current — plus only the bodies that
// actually changed.
//
// This is the fix for one of the three enumerations that get slk's
// Enterprise Grid accounts signed out for "data scraping". slk re-downloads
// conversations.history for every channel ever visited on every boot AND
// every reconnect, at page sizes of 50/200/500. With cached_latest_updates
// the same scrollback is VALIDATED rather than refetched, and at the page
// size the official client actually uses.
//
// This is additive. GetHistory, GetHistoryAround and GetHistorySince are
// untouched and still route through slack-go; nothing calls this yet.
// Phase 2b wires it.
//
// # Request shape
//
// Measured across all 14 conversations.history requests in the 8
// captures. Eleven business params on 14 of 14, plus `token` (injected
// by postForm) and the four _x_ envelope params (added by
// slackhttp.BrowserTransport — conversations.history is in neither of
// its exclusion sets), plus at most one of latest/oldest.
//
// # _x_reason
//
// slackhttp's defaultReasons table ALREADY maps conversations.history
// to historyReason, so the WithReason call below is, TODAY, byte-for-byte
// redundant on the wire. That is measured, not assumed: deleting the
// whole block is a surviving mutant against this file's full test suite,
// because defaultReason("conversations.history") then supplies the same
// string. It is kept as an explicit pin at the call site so the value
// this endpoint sends does not depend on a shared table it does not own,
// and so a method rename cannot silently drop it to slackhttp's
// genericReason fallback. Mutating historyReason itself IS caught.
//
// It is conditional rather than unconditional because the unconditional
// form would be actively wrong: 3 of the 14 captured requests carry
// "unread-counts/onLastReadUpdated" instead, and reason.go deliberately
// leaves that variant to a caller override. Clobbering a reason the
// caller already set would make the second variant unreachable — that
// mutation is caught.
//
// TRAP FOR PHASE 2b: the conditional cuts the other way too. A ctx that
// already carries some OTHER endpoint's reason — say one built for
// client.userBoot under WithReason(ctx, "initial-data") and then reused
// here — reaches the wire unchanged, and "initial-data" on
// conversations.history is a shape no capture shows. Derive the ctx for
// this call from a reason-free one, or set the reason you mean.
//
// # Errors
//
// Any error returns a zero HistoryResult. That is load-bearing rather
// than tidiness: encoding/json populates a struct as it goes and keeps
// decoding past the first type error, and `ok` is only inspected after
// the whole body is decoded — so at both failure points a populated
// response struct is sitting in a local. Handing it back would give the
// caller plausible-looking scrollback built from a response the server
// rejected or the decoder could not read.
func (c *Client) HistoryWithVersions(ctx context.Context, channelID string, opts HistoryOpts) (HistoryResult, error) {
	if channelID == "" {
		// `channel=` on the wire is a request shape none of the 14
		// captures show, and the server can only reject it or answer
		// uselessly.
		return HistoryResult{}, fmt.Errorf("%s: channelID is required", historyMethod)
	}

	limit := opts.Limit
	if limit <= 0 {
		// Non-positive is not a page size, it is caller error:
		// `limit=0` and `limit=-5` are wire shapes none of the 14
		// captures show, and the server can only reject them or answer
		// uselessly. Correcting a knowably-wrong shape is different
		// from clamping a large well-formed one, which this
		// deliberately does not do — see HistoryOpts.Limit.
		limit = defaultHistoryLimit
	}

	cached := emptyCachedLatestUpdates
	if len(opts.CachedVersions) > 0 {
		b, err := json.Marshal(opts.CachedVersions)
		if err != nil {
			// Unreachable for map[string]string, but returning the
			// empty object here would silently degrade an incremental
			// sync into a full refetch.
			return HistoryResult{}, fmt.Errorf("%s: encoding cached_latest_updates: %w", historyMethod, err)
		}
		cached = string(b)
	}

	// Strings, not booleans: this is a form body. Every value here was
	// read off the captures.
	form := url.Values{
		"channel":                          {channelID},
		"limit":                            {strconv.Itoa(limit)},
		"ignore_replies":                   {"true"},
		"include_pin_count":                {"true"},
		"inclusive":                        {"true"},
		"no_user_profile":                  {"true"},
		"include_stories":                  {"true"},
		"include_free_team_extra_messages": {"true"},
		"include_date_joined":              {strconv.FormatBool(opts.IncludeDateJoined)},
		"cached_latest_updates":            {cached},
	}
	// Set, not unconditional assignment: an unset anchor must leave
	// the key absent, not present-and-empty.
	if opts.Oldest != "" {
		form.Set("oldest", opts.Oldest)
	}
	if opts.Latest != "" {
		form.Set("latest", opts.Latest)
	}

	if slackhttp.ReasonFrom(ctx) == "" {
		ctx = slackhttp.WithReason(ctx, historyReason)
	}

	raw, err := c.postForm(ctx, historyMethod, form)
	if err != nil {
		return HistoryResult{}, fmt.Errorf("%s: %w", historyMethod, err)
	}

	var resp historyWithVersionsResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return HistoryResult{}, fmt.Errorf("%s: parsing response: %w (body: %s)",
			historyMethod, err, truncateForLog(raw))
	}
	if !resp.OK {
		apiErr := resp.Error
		if apiErr == "" {
			// Without this the message names no failure at all.
			apiErr = "ok=false with no error field"
		}
		return HistoryResult{}, fmt.Errorf("%s: API error: %s", historyMethod, apiErr)
	}

	return HistoryResult{
		Messages:      resp.Messages,
		UnchangedTS:   resp.UnchangedTS,
		LatestUpdates: resp.LatestUpdates,
		HasMore:       resp.HasMore,
	}, nil
}
