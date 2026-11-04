package core

import (
	"testing"

	"github.com/gammons/slk/internal/ids"
)

func TestMessageServiceRemoveAttachment(t *testing.T) {
	var gotTeam, gotCh, gotTS string
	var gotID int
	svc := NewMessageService(MessageServiceFuncs{
		RemoveAttachment: func(teamID string, channelID ids.ChannelID, ts ids.MessageTS, attachmentID int) Msg {
			gotTeam, gotCh, gotTS, gotID = teamID, string(channelID), string(ts), attachmentID
			return "result"
		},
	})
	if got := svc.RemoveAttachment("T1", "C1", "1.0", 3); got != "result" {
		t.Errorf("RemoveAttachment = %v, want the func's result", got)
	}
	if gotTeam != "T1" || gotCh != "C1" || gotTS != "1.0" || gotID != 3 {
		t.Errorf("func got %q %q %q %d", gotTeam, gotCh, gotTS, gotID)
	}
}

func TestMessageServiceRemoveAttachmentNilIsNoop(t *testing.T) {
	if got := NewMessageService(MessageServiceFuncs{}).RemoveAttachment("T1", "C1", "1.0", 1); got != nil {
		t.Errorf("nil RemoveAttachment returned %v, want nil", got)
	}
}
