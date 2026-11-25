package slackclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// uploadRecorder is a mockSlackAPI that records the V2 upload steps.
type uploadRecorder struct {
	mockSlackAPI
	gets        []slack.GetUploadURLExternalParameters
	uploads     []slack.UploadToURLParameters
	bodies      []string
	completes   []slack.CompleteUploadExternalParameters
	getErr      error
	getErrOn    int // 1-based; 0 means every get fails when getErr is set
	uploadErr   error
	uploadOn    int
	completeErr error
}

func (r *uploadRecorder) install() {
	n := 0
	r.getUploadURLExternalContextFn = func(ctx context.Context, params slack.GetUploadURLExternalParameters) (*slack.GetUploadURLExternalResponse, error) {
		r.gets = append(r.gets, params)
		n++
		if r.getErr != nil && (r.getErrOn == 0 || r.getErrOn == n) {
			return nil, r.getErr
		}
		return &slack.GetUploadURLExternalResponse{
			UploadURL: fmt.Sprintf("https://files.slack.com/upload/%d", n),
			FileID:    fmt.Sprintf("F%d", n),
		}, nil
	}
	r.uploadToURLFn = func(ctx context.Context, params slack.UploadToURLParameters) error {
		body, err := io.ReadAll(params.Reader)
		if err != nil {
			return err
		}
		r.bodies = append(r.bodies, string(body))
		params.Reader = nil
		r.uploads = append(r.uploads, params)
		if r.uploadErr != nil && (r.uploadOn == 0 || r.uploadOn == len(r.uploads)) {
			return r.uploadErr
		}
		return nil
	}
	r.completeUploadExternalContextFn = func(ctx context.Context, params slack.CompleteUploadExternalParameters) (*slack.CompleteUploadExternalResponse, error) {
		r.completes = append(r.completes, params)
		if r.completeErr != nil {
			return nil, r.completeErr
		}
		return &slack.CompleteUploadExternalResponse{Files: params.Files}, nil
	}
}

func twoFiles() []FileUpload {
	return []FileUpload{
		{Filename: "a.png", Reader: strings.NewReader("aaa"), Size: 3},
		{Filename: "b.pdf", Reader: strings.NewReader("bbbb"), Size: 4},
	}
}

func TestUploadFiles_OneCompleteCallCarriesEveryFileID(t *testing.T) {
	cases := []struct {
		name    string
		thread  string
		caption string
		files   int
	}{
		{name: "channel two files", files: 2},
		{name: "channel two files with caption", caption: "see both", files: 2},
		{name: "thread two files", thread: "1700000000.000100", files: 2},
		{name: "thread two files with caption", thread: "1700000000.000100", caption: "see both", files: 2},
		{name: "channel one file with caption", caption: "look at this", files: 1},
		{name: "thread one file", thread: "1700000000.000100", files: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &uploadRecorder{}
			rec.install()
			c := &Client{api: rec}

			files := twoFiles()[:tc.files]
			err := c.UploadFiles(context.Background(), "C1", tc.thread, tc.caption, files, nil)
			if err != nil {
				t.Fatalf("UploadFiles: %v", err)
			}
			if len(rec.gets) != tc.files {
				t.Fatalf("getUploadURLExternal calls = %d, want %d", len(rec.gets), tc.files)
			}
			if len(rec.uploads) != tc.files {
				t.Fatalf("uploads = %d, want %d", len(rec.uploads), tc.files)
			}
			if len(rec.completes) != 1 {
				t.Fatalf("completeUploadExternal calls = %d, want 1", len(rec.completes))
			}
			got := rec.completes[0]
			if got.Channel != "C1" {
				t.Errorf("channel_id = %q, want C1", got.Channel)
			}
			if got.ThreadTimestamp != tc.thread {
				t.Errorf("thread_ts = %q, want %q", got.ThreadTimestamp, tc.thread)
			}
			if got.InitialComment != tc.caption {
				t.Errorf("initial_comment = %q, want %q", got.InitialComment, tc.caption)
			}
			if len(got.Files) != tc.files {
				t.Fatalf("files in complete = %d, want %d", len(got.Files), tc.files)
			}
			for i := range tc.files {
				id := fmt.Sprintf("F%d", i+1)
				if got.Files[i].ID != id {
					t.Errorf("files[%d].id = %q, want %s", i, got.Files[i].ID, id)
				}
				if got.Files[i].Title != files[i].Filename {
					t.Errorf("files[%d].title = %q, want %s", i, got.Files[i].Title, files[i].Filename)
				}
				if rec.gets[i].FileName != files[i].Filename || rec.gets[i].FileSize != int(files[i].Size) {
					t.Errorf("get[%d] = %+v, want name %s size %d", i, rec.gets[i], files[i].Filename, files[i].Size)
				}
				if rec.uploads[i].UploadURL != fmt.Sprintf("https://files.slack.com/upload/%d", i+1) {
					t.Errorf("upload[%d] url = %q", i, rec.uploads[i].UploadURL)
				}
			}
			if tc.files == 2 && (rec.bodies[0] != "aaa" || rec.bodies[1] != "bbbb") {
				t.Errorf("uploaded bodies = %q, want aaa, bbbb", rec.bodies)
			}
		})
	}
}

func TestUploadFiles_SecondFileErrorSkipsComplete(t *testing.T) {
	rec := &uploadRecorder{uploadErr: errors.New("connection reset"), uploadOn: 2}
	rec.install()
	c := &Client{api: rec}

	err := c.UploadFiles(context.Background(), "C1", "", "caption", twoFiles(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "b.pdf") || !strings.Contains(err.Error(), "2/2") {
		t.Errorf("error = %q, want the failing filename and position", err.Error())
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error = %q, want the underlying error", err.Error())
	}
	if len(rec.completes) != 0 {
		t.Errorf("completeUploadExternal calls = %d, want 0 after a failed upload", len(rec.completes))
	}
}

func TestUploadFiles_GetURLErrorSkipsComplete(t *testing.T) {
	rec := &uploadRecorder{getErr: errors.New("file_uploads_disabled"), getErrOn: 2}
	rec.install()
	c := &Client{api: rec}

	err := c.UploadFiles(context.Background(), "C9", "1700000000.000100", "", twoFiles(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "b.pdf") || !strings.Contains(err.Error(), "file_uploads_disabled") {
		t.Errorf("error = %q, want filename and API error", err.Error())
	}
	if len(rec.uploads) != 1 {
		t.Errorf("uploads = %d, want 1 (the file that received a URL)", len(rec.uploads))
	}
	if len(rec.completes) != 0 {
		t.Errorf("completeUploadExternal calls = %d, want 0", len(rec.completes))
	}
}

func TestUploadFiles_CompleteErrorSurfaced(t *testing.T) {
	rec := &uploadRecorder{completeErr: errors.New("channel_not_found")}
	rec.install()
	c := &Client{api: rec}

	err := c.UploadFiles(context.Background(), "C1", "", "hi", twoFiles(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %q, want the complete error", err.Error())
	}
	if len(rec.completes) != 1 {
		t.Fatalf("complete calls = %d, want 1", len(rec.completes))
	}
	if len(rec.completes[0].Files) != 2 {
		t.Errorf("complete files = %d, want both ids even when the share is refused", len(rec.completes[0].Files))
	}
}

func TestUploadFiles_EmptyIsANoOp(t *testing.T) {
	rec := &uploadRecorder{}
	rec.install()
	c := &Client{api: rec}
	if err := c.UploadFiles(context.Background(), "C1", "", "", nil, nil); err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	if len(rec.gets) != 0 || len(rec.uploads) != 0 || len(rec.completes) != 0 {
		t.Errorf("empty batch still called the API: gets %d uploads %d completes %d", len(rec.gets), len(rec.uploads), len(rec.completes))
	}
}

func TestUploadFiles_ProgressCountsUploadedFiles(t *testing.T) {
	rec := &uploadRecorder{}
	rec.install()
	c := &Client{api: rec}
	var got []int
	err := c.UploadFiles(context.Background(), "C1", "", "", twoFiles(), func(done, total int) {
		if total != 2 {
			t.Errorf("total = %d, want 2", total)
		}
		got = append(got, done)
	})
	if err != nil {
		t.Fatalf("UploadFiles: %v", err)
	}
	if strings.Trim(fmt.Sprint(got), "[]") != "0 1 2" {
		t.Errorf("progress = %v, want [0 1 2]", got)
	}
}
