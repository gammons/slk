package main

import (
	"context"
	"errors"
	"testing"

	"github.com/slack-go/slack"
)

// orgFallbackConversationResolver retries a member workspace's failed
// conversation lookup through the same-user org session, so a grid
// conversation the org workspace owns still gets its sidebar row where
// the member session received the messages. The own resolver's
// success, the org fallback's success, and the no-org passthrough of
// the own failure are all pinned - and the org resolver returning the
// own error (what the wired closure does when it cannot do better)
// keeps the caller's retry-backoff classification intact.
func TestOrgFallbackConversationResolver(t *testing.T) {
	ownErr := errors.New("method_not_supported_for_this_token")
	channel := func(id string) *slack.Channel {
		return &slack.Channel{GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{ID: id},
		}}
	}
	orgCh := channel("D1")
	calls := 0
	own := func(ctx context.Context, channelID string) (*slack.Channel, error) {
		calls++
		if channelID == "C-own" {
			return channel("C-own"), nil
		}
		return nil, ownErr
	}
	orgCalls := 0
	org := func(ctx context.Context, channelID string, ownErr error) (*slack.Channel, error) {
		orgCalls++
		if ownErr == nil {
			t.Fatal("the org resolver was called without the own failure")
		}
		return orgCh, nil
	}

	// No org session: the own resolver alone, failures included.
	passthrough := orgFallbackConversationResolver(own, nil)
	if ch, err := passthrough(context.Background(), "D1"); ch != nil || err != ownErr {
		t.Fatalf("no-org passthrough = (%v, %v); want the own failure", ch, err)
	}

	resolve := orgFallbackConversationResolver(own, org)

	// The own resolver's answer wins without consulting the org.
	ch, err := resolve(context.Background(), "C-own")
	if err != nil || ch.ID != "C-own" {
		t.Fatalf("own-success path = (%v, %v)", ch, err)
	}
	if orgCalls != 0 {
		t.Fatal("the org resolver ran although the own resolver succeeded")
	}

	// The own resolver's failure falls through to the org session.
	ch, err = resolve(context.Background(), "D1")
	if err != nil || ch != orgCh {
		t.Fatalf("fallback path = (%v, %v); want the org session's channel", ch, err)
	}
	if calls != 3 || orgCalls != 1 {
		t.Fatalf("calls = own:%d org:%d; want own:3 org:1 (passthrough, own-success, fallback attempt)", calls, orgCalls)
	}

	// The wired closure's shape: when the org resolver returns the own
	// error (no org session, or the org lookup failed too), the caller
	// sees that error, not a wrapper one - the backoff classifies it.
	giveUp := orgFallbackConversationResolver(own, func(ctx context.Context, channelID string, ownErr error) (*slack.Channel, error) {
		return nil, ownErr
	})
	if _, err := giveUp(context.Background(), "D1"); err != ownErr {
		t.Fatalf("own-error passthrough = %v; want the own error itself", err)
	}
}
