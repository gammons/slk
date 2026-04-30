package image

import (
	"image"
	"testing"
)

func TestPickThumb_SmallestThatFits(t *testing.T) {
	thumbs := []ThumbSpec{
		{URL: "u-360", W: 360, H: 360},
		{URL: "u-720", W: 720, H: 720},
		{URL: "u-1024", W: 1024, H: 1024},
	}
	// Target 400x400 — should pick 720.
	url, suffix := PickThumb(thumbs, image.Pt(400, 400))
	if url != "u-720" {
		t.Errorf("got %q, want u-720", url)
	}
	if suffix != "720" {
		t.Errorf("suffix got %q, want 720", suffix)
	}
}

func TestPickThumb_FallsBackToLargest(t *testing.T) {
	thumbs := []ThumbSpec{
		{URL: "u-360", W: 360, H: 360},
	}
	url, _ := PickThumb(thumbs, image.Pt(800, 800))
	if url != "u-360" {
		t.Errorf("got %q, want u-360 (largest available)", url)
	}
}

func TestPickThumb_EmptyReturnsEmpty(t *testing.T) {
	url, _ := PickThumb(nil, image.Pt(100, 100))
	if url != "" {
		t.Errorf("expected empty, got %q", url)
	}
}

func TestPickThumb_RequiresBothAxes(t *testing.T) {
	thumbs := []ThumbSpec{
		{URL: "u-wide", W: 1000, H: 100}, // wide enough but too short
		{URL: "u-square", W: 500, H: 500},
	}
	url, _ := PickThumb(thumbs, image.Pt(400, 400))
	if url != "u-square" {
		t.Errorf("got %q, want u-square (only one that fits both axes)", url)
	}
}
