package image

import (
	"image"
	"testing"
)

func TestRegistry_AssignsStableIDs(t *testing.T) {
	r := NewRegistry()
	id1, fresh1 := r.Lookup("file-A", image.Pt(40, 20))
	if !fresh1 {
		t.Error("expected fresh on first lookup")
	}
	if id1 == 0 {
		t.Error("expected non-zero ID")
	}

	id2, fresh2 := r.Lookup("file-A", image.Pt(40, 20))
	if fresh2 {
		t.Error("expected not fresh on repeat")
	}
	if id2 != id1 {
		t.Errorf("expected stable ID %d, got %d", id1, id2)
	}
}

func TestRegistry_DifferentSizesDifferentIDs(t *testing.T) {
	r := NewRegistry()
	a, _ := r.Lookup("file", image.Pt(40, 20))
	b, _ := r.Lookup("file", image.Pt(20, 10))
	if a == b {
		t.Error("different cell footprints should yield different IDs")
	}
}

func TestRegistry_IDsNonZero(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 10; i++ {
		id, _ := r.Lookup("k"+string(rune('A'+i)), image.Pt(1, 1))
		if id == 0 {
			t.Errorf("got zero ID at i=%d", i)
		}
	}
}
