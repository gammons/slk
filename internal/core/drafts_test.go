package core

import (
	"errors"
	"testing"
	"time"
)

func TestDraftWritable(t *testing.T) {
	base := Draft{ID: "Dr1", Key: DraftKey{TeamID: "T1", ChannelID: "C1"}}
	cases := []struct {
		name string
		mut  func(*Draft)
		want bool
	}{
		{"attached", func(*Draft) {}, true},
		{"thread", func(d *Draft) { d.Key.ThreadTS = "1.0" }, true},
		{"saved", func(d *Draft) { d.Saved = true }, false},
		{"no recipient", func(d *Draft) { d.Key.ChannelID = "" }, false},
		{"scheduled", func(d *Draft) { d.Scheduled = time.Unix(1, 0) }, false},
		{"files", func(d *Draft) { d.FileCount = 2 }, false},
	}
	for _, c := range cases {
		d := base
		c.mut(&d)
		if got := d.Writable(); got != c.want {
			t.Errorf("%s: Writable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDraftServiceNilFuncsAreUnsupported(t *testing.T) {
	s := NewDraftService(DraftServiceFuncs{})
	if _, err := s.Load("T1"); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Load err = %v", err)
	}
	if _, err := s.Save(DraftKey{}, "x", time.Time{}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Save err = %v", err)
	}
	if err := s.Discard(DraftKey{}); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Discard err = %v", err)
	}
	if err := s.Delete("T1", "Dr1"); !errors.Is(err, errors.ErrUnsupported) {
		t.Errorf("Delete err = %v", err)
	}
}

func TestDraftServicePassesArguments(t *testing.T) {
	key := DraftKey{TeamID: "T1", ChannelID: "C1", ThreadTS: "1.0"}
	at := time.Unix(100, 0)
	errDiscardSentinel := errors.New("discard sentinel")
	errDeleteSentinel := errors.New("delete sentinel")
	wantDraft := Draft{ID: "Dr1", Key: key}
	var got []string
	s := NewDraftService(DraftServiceFuncs{
		Load: func(teamID string) ([]Draft, error) {
			if teamID != "T1" {
				t.Errorf("Load(%q)", teamID)
			}
			got = append(got, "load")
			return []Draft{wantDraft}, nil
		},
		Save: func(k DraftKey, text string, edited time.Time) (DraftSaveResult, error) {
			if k != key || text != "hi" || !edited.Equal(at) {
				t.Errorf("Save(%v, %q, %v)", k, text, edited)
			}
			got = append(got, "save")
			return DraftSaveResult{Replaced: true}, nil
		},
		Discard: func(k DraftKey) error {
			if k != key {
				t.Errorf("Discard(%v)", k)
			}
			got = append(got, "discard")
			return errDiscardSentinel
		},
		Delete: func(teamID, draftID string) error {
			if teamID != "T1" || draftID != "Dr1" {
				t.Errorf("Delete(%q, %q)", teamID, draftID)
			}
			got = append(got, "delete")
			return errDeleteSentinel
		},
	})

	res, err := s.Save(key, "hi", at)
	if err != nil || !res.Replaced {
		t.Errorf("Save result=%+v err=%v", res, err)
	}

	drafts, err := s.Load("T1")
	if err != nil || len(drafts) != 1 || drafts[0] != wantDraft {
		t.Errorf("Load result=%+v err=%v", drafts, err)
	}

	if err := s.Discard(key); !errors.Is(err, errDiscardSentinel) {
		t.Errorf("Discard err = %v, want %v", err, errDiscardSentinel)
	}

	if err := s.Delete("T1", "Dr1"); !errors.Is(err, errDeleteSentinel) {
		t.Errorf("Delete err = %v, want %v", err, errDeleteSentinel)
	}

	if want := []string{"save", "load", "discard", "delete"}; len(got) != len(want) {
		t.Errorf("calls=%v, want %v", got, want)
	} else {
		for i, c := range want {
			if got[i] != c {
				t.Errorf("calls=%v, want %v", got, want)
				break
			}
		}
	}
}
