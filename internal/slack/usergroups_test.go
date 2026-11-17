package slackclient

import (
	"context"
	"errors"
	"testing"

	"github.com/slack-go/slack"
)

func TestGetUserGroups(t *testing.T) {
	mock := &mockSlackAPI{
		getUserGroupsContextFn: func(ctx context.Context, options ...slack.GetUserGroupsOption) ([]slack.UserGroup, error) {
			// The members tell the UI which groups the user is in.
			var params slack.GetUserGroupsParams
			for _, o := range options {
				o(&params)
			}
			if !params.IncludeUsers {
				t.Error("usergroups.list called without include_users")
			}
			return []slack.UserGroup{
				{ID: "S0TESTGRP01", Handle: "platform-team", Name: "Platform Team"},
			}, nil
		},
	}
	c := &Client{api: mock}

	groups, err := c.GetUserGroups(context.Background())
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].Handle != "platform-team" {
		t.Errorf("groups = %+v; want one group with handle platform-team", groups)
	}
}

func TestGetUserGroupsError(t *testing.T) {
	mock := &mockSlackAPI{
		getUserGroupsContextFn: func(ctx context.Context, options ...slack.GetUserGroupsOption) ([]slack.UserGroup, error) {
			return nil, errors.New("missing_scope")
		},
	}
	c := &Client{api: mock}

	if _, err := c.GetUserGroups(context.Background()); err == nil {
		t.Error("expected error to propagate")
	}
}
