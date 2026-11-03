package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// TestErrorFrom_MapsCodes is the table from what can go wrong to the
// error code and exit code a script sees.
func TestErrorFrom_MapsCodes(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      string
		exit      int
		slackCode string
	}{
		{"usage", usageError("bad"), codeUsage, exitUsage, ""},
		{"invalid auth bundle", &Error{Code: "invalid_auth_bundle", Message: "bad document"}, codeInvalidAuthBundle, exitUsage, ""},
		{"auth conflict", &Error{Code: "auth_conflict", Message: "existing login"}, codeAuthConflict, exitFailure, ""},
		{"unsafe auth file", &Error{Code: "unsafe_auth_file", Message: "unsafe permissions"}, codeUnsafeAuthFile, exitFailure, ""},
		{"wrapped *Error", fmt.Errorf("ctx: %w", &Error{Code: codeNotFound, Message: "x"}), codeNotFound, exitNotFound, ""},
		{"cancelled", fmt.Errorf("walk: %w", context.Canceled), codeCancelled, exitFailure, ""},
		{"invalid_auth", fmt.Errorf("auth test failed: %w", slack.SlackErrorResponse{Err: "invalid_auth"}), codeAuthFailed, exitAuth, "invalid_auth"},
		{"token_revoked", slack.SlackErrorResponse{Err: "token_revoked"}, codeAuthFailed, exitAuth, "token_revoked"},
		{"other Slack error", slack.SlackErrorResponse{Err: "channel_not_found"}, codeSlackError, exitFailure, "channel_not_found"},
		{"anything else", errors.New("dial tcp: no route to host"), codeFailed, exitFailure, ""},
	}
	for _, tc := range cases {
		e := errorFrom(tc.err)
		if e.Code != tc.code || e.exit() != tc.exit || e.SlackError != tc.slackCode {
			t.Errorf("%s: got code %q exit %d slack_error %q, want %q %d %q",
				tc.name, e.Code, e.exit(), e.SlackError, tc.code, tc.exit, tc.slackCode)
		}
		if e.Candidates == nil {
			t.Errorf("%s: Candidates is nil; the JSON form must have []", tc.name)
		}
	}
}

// TestErrorExits_CoverEveryCode keeps the code list and the exit table
// in step: a code without an exit would silently exit 1.
func TestErrorExits_CoverEveryCode(t *testing.T) {
	for _, code := range []string{codeUsage, codeWorkspaceRequired, codeNoWorkspace, codeAuthFailed,
		codeNotFound, codeSlackError, codeCancelled, codeFailed, codeInvalidAuthBundle, codeAuthConflict, codeUnsafeAuthFile} {
		if _, ok := errorExits[code]; !ok {
			t.Errorf("code %q has no exit code in errorExits", code)
		}
	}
}

func TestPrinter_JSONIsOneDocument(t *testing.T) {
	var out bytes.Buffer
	p := &printer{json: true, stdout: &out}
	doc := struct {
		Items []string `json:"items"`
	}{[]string{"a"}}
	if err := p.result(doc, nil); err != nil {
		t.Fatal(err)
	}
	checkJSONDocument(t, "stdout", out.String())
}

func TestPrinter_TextGoesThroughTheTextFunction(t *testing.T) {
	var out bytes.Buffer
	p := &printer{stdout: &out}
	p.result(struct{}{}, func(w io.Writer) error {
		_, err := w.Write([]byte("hello\n"))
		return err
	})
	if out.String() != "hello\n" {
		t.Errorf("stdout = %q, want the text form", out.String())
	}
}

func TestError_JSONGoesToStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	p := &printer{json: true, stdout: &out, stderr: &errOut}
	exit := p.fail(slack.SlackErrorResponse{Err: "channel_not_found"})
	if exit != exitFailure {
		t.Errorf("exit = %d, want %d", exit, exitFailure)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
	doc := checkJSONDocument(t, "stderr", errOut.String())
	var got struct{ Error Error }
	json.Unmarshal(errOut.Bytes(), &got)
	if got.Error.Code != codeSlackError || got.Error.SlackError != "channel_not_found" {
		t.Errorf("error document = %v", doc)
	}
}

func TestError_TextListsCandidates(t *testing.T) {
	var errOut bytes.Buffer
	p := &printer{stderr: &errOut}
	p.fail(&Error{Code: codeNotFound, Message: `workspace "x" not found`, Candidates: []Candidate{{ID: "T1", Name: "Acme"}}})
	want := "Error: workspace \"x\" not found\n  T1  Acme\n"
	if errOut.String() != want {
		t.Errorf("stderr = %q, want %q", errOut.String(), want)
	}
	if strings.Contains(errOut.String(), "{") {
		t.Error("text mode wrote JSON")
	}
}
