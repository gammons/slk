package messages

import (
	"reflect"
	"testing"

	"github.com/gammons/slk/internal/ui/messages/blockkit"
)

func TestRemovablePreviews(t *testing.T) {
	msg := MessageItem{LegacyAttachments: []blockkit.LegacyAttachment{
		{ID: 1, Title: "From URL", FromURL: "https://a.example"},
		{ID: 2, Title: "A bot card"}, // neither URL: left alone
		{ID: 3, OriginalURL: "https://b.example"},
		{Title: "No id", FromURL: "https://c.example"}, // ID == 0: position 4
		{ID: 5, FromURL: "https://d.example", OriginalURL: "https://ignored.example"},
	}}
	want := []RemovablePreview{
		{AttachmentID: 1, URL: "https://a.example", Title: "From URL"},
		{AttachmentID: 3, URL: "https://b.example"},
		{AttachmentID: 4, URL: "https://c.example", Title: "No id"},
		{AttachmentID: 5, URL: "https://d.example"},
	}
	if got := RemovablePreviews(msg); !reflect.DeepEqual(got, want) {
		t.Errorf("RemovablePreviews = %+v, want %+v", got, want)
	}
	if got := RemovablePreviews(MessageItem{}); got != nil {
		t.Errorf("no attachments: got %+v, want nil", got)
	}
}

func TestRemovablePreviewLabel(t *testing.T) {
	if got := (RemovablePreview{URL: "https://a.example", Title: "A"}).Label(); got != "A" {
		t.Errorf("Label = %q, want the title", got)
	}
	if got := (RemovablePreview{URL: "https://a.example"}).Label(); got != "https://a.example" {
		t.Errorf("Label = %q, want the URL", got)
	}
}
