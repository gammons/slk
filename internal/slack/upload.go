package slackclient

import (
	"context"
	"fmt"
	"io"

	"github.com/slack-go/slack"
)

// FileUpload is one file in a batch that UploadFiles shares as a single
// Slack message. Reader is consumed during the upload.
type FileUpload struct {
	Filename string
	Reader   io.Reader
	Size     int64
}

// UploadFiles uploads every file and shares them as one Slack message.
//
// Each file is given its own URL from files.getUploadURLExternal and
// its bytes are uploaded there. One files.completeUploadExternal then
// carries every file ID, channelID, threadTS when the share is a thread
// reply, and caption as initial_comment. That single complete call is
// what makes Slack post one message instead of one per file.
//
// progress, when non-nil, reports how many files have had their bytes
// uploaded: it is called before each file with done equal to the number
// already finished, and once with done == total after the share
// succeeds. A failure leaves the final call unsent.
//
// An empty files slice shares nothing and returns nil.
//
// Size is int64 (matching os.FileInfo.Size()) and is narrowed to int
// for slack-go. Callers must enforce a reasonable upper bound; this
// method does not.
func (c *Client) UploadFiles(ctx context.Context, channelID, threadTS, caption string, files []FileUpload, progress func(done, total int)) error {
	_, err := c.shareFiles(ctx, channelID, threadTS, caption, files, progress)
	return err
}

func (c *Client) shareFiles(ctx context.Context, channelID, threadTS, caption string, files []FileUpload, progress func(done, total int)) ([]slack.FileSummary, error) {
	if len(files) == 0 {
		return nil, nil
	}
	summaries := make([]slack.FileSummary, 0, len(files))
	total := len(files)
	for i, f := range files {
		if progress != nil {
			progress(i, total)
		}
		u, err := c.api.GetUploadURLExternalContext(ctx, slack.GetUploadURLExternalParameters{
			FileName: f.Filename,
			FileSize: int(f.Size),
		})
		if err != nil {
			return nil, fmt.Errorf("uploading %s (%d/%d): %w", f.Filename, i+1, total, err)
		}
		if u == nil || u.FileID == "" || u.UploadURL == "" {
			return nil, fmt.Errorf("uploading %s (%d/%d): empty upload URL", f.Filename, i+1, total)
		}
		if err := c.api.UploadToURL(ctx, slack.UploadToURLParameters{
			UploadURL: u.UploadURL,
			Reader:    f.Reader,
			Filename:  f.Filename,
		}); err != nil {
			return nil, fmt.Errorf("uploading %s (%d/%d): %w", f.Filename, i+1, total, err)
		}
		summaries = append(summaries, slack.FileSummary{ID: u.FileID, Title: f.Filename})
	}
	params := slack.CompleteUploadExternalParameters{
		Files:   summaries,
		Channel: channelID,
	}
	if threadTS != "" {
		params.ThreadTimestamp = threadTS
	}
	if caption != "" {
		params.InitialComment = caption
	}
	resp, err := c.api.CompleteUploadExternalContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("completing upload: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("completing upload: empty response")
	}
	if progress != nil {
		progress(total, total)
	}
	return resp.Files, nil
}
