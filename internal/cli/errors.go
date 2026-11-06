package cli

import (
	"context"
	"errors"
	"fmt"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/slack-go/slack"
)

// Exit codes. They are part of the contract SKILL.md documents: a
// script branches on them, so a meaning never changes once released.
const (
	exitOK       = 0
	exitFailure  = 1 // network, Slack, cancelled, anything unexpected
	exitUsage    = 2 // the command line is wrong
	exitAuth     = 3 // no workspace configured, or Slack refused the login
	exitNotFound = 4 // a reference matched nothing
)

// Error codes: a closed list, each with its exit code. SKILL.md lists
// every one (TestSkillListsEveryErrorCode), because agents branch on
// them the same way scripts branch on exit codes.
const (
	codeUsage             = "usage"
	codeWorkspaceRequired = "workspace_required"
	codeNoWorkspace       = "no_workspace"
	codeAuthFailed        = "auth_failed"
	codeNotFound          = "not_found"
	codeSlackError        = "slack_error"
	codeCancelled         = "cancelled"
	codeFailed            = "failed"
)

// errorExits maps every error code to its exit code.
var errorExits = map[string]int{
	codeUsage:             exitUsage,
	codeWorkspaceRequired: exitUsage,
	codeNoWorkspace:       exitAuth,
	codeAuthFailed:        exitAuth,
	codeNotFound:          exitNotFound,
	codeSlackError:        exitFailure,
	codeCancelled:         exitFailure,
	codeFailed:            exitFailure,
}

// Error is a failure with a stable code. Under --json it is written to
// stderr as {"error": Error}; otherwise as "Error: <message>", followed
// by the candidates, if any.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// SlackError is Slack's own error string ("channel_not_found"),
	// when Slack refused the request, so an agent can act on a detail
	// the CLI does not map to a code of its own.
	SlackError string `json:"slack_error"`
	// Candidates lists what a reference could have meant: every
	// workspace, when none was chosen or the one named does not exist.
	Candidates []Candidate `json:"candidates"`
}

// Candidate is one thing a reference could have meant.
type Candidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (e *Error) Error() string { return e.Message }

// exit is the process exit code for e.
func (e *Error) exit() int {
	if code, ok := errorExits[e.Code]; ok {
		return code
	}
	return exitFailure
}

// usageError is a wrong command line: exit 2.
func usageError(format string, args ...any) *Error {
	return &Error{Code: codeUsage, Message: fmt.Sprintf(format, args...)}
}

// errorFrom classifies any error a command returns. An *Error passes
// through; a cancelled context, Slack refusing the login, and Slack
// refusing a request get their own codes; anything else is "failed".
// Candidates is never nil, so the JSON form has [] rather than null.
func errorFrom(err error) *Error {
	var out *Error
	var se slack.SlackErrorResponse
	switch {
	case errors.As(err, &out):
		out = &Error{Code: out.Code, Message: out.Message, SlackError: out.SlackError, Candidates: out.Candidates}
	case errors.Is(err, context.Canceled):
		out = &Error{Code: codeCancelled, Message: "cancelled"}
	case slackclient.IsAuthError(err):
		errors.As(err, &se)
		out = &Error{Code: codeAuthFailed, Message: err.Error(), SlackError: se.Err}
	case errors.As(err, &se):
		out = &Error{Code: codeSlackError, Message: err.Error(), SlackError: se.Err}
	default:
		out = &Error{Code: codeFailed, Message: err.Error()}
	}
	if out.Candidates == nil {
		out.Candidates = []Candidate{}
	}
	return out
}
