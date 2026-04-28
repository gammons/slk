package selection

import "testing"

func TestRange_NormalizeOrdersEndpoints(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	b := Anchor{MessageID: "1.0", Line: 0, Col: 10}
	r := Range{Start: b, End: a}
	lo, hi := r.Normalize()
	if lo != a || hi != b {
		t.Fatalf("Normalize did not order endpoints: lo=%+v hi=%+v", lo, hi)
	}
}

func TestRange_NormalizeAcrossLines(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 2, Col: 0}
	b := Anchor{MessageID: "1.0", Line: 0, Col: 99}
	lo, hi := Range{Start: a, End: b}.Normalize()
	if lo != b || hi != a {
		t.Fatalf("Line precedence wrong: lo=%+v hi=%+v", lo, hi)
	}
}

func TestRange_NormalizeAcrossMessages(t *testing.T) {
	// Earlier MessageID (Slack TS sorts lexicographically) wins regardless
	// of Line/Col.
	a := Anchor{MessageID: "1700000001.000100", Line: 5, Col: 5}
	b := Anchor{MessageID: "1700000000.000200", Line: 0, Col: 0}
	lo, hi := Range{Start: a, End: b}.Normalize()
	if lo != b || hi != a {
		t.Fatalf("MessageID precedence wrong: lo=%+v hi=%+v", lo, hi)
	}
}

func TestRange_IsEmpty(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 1, Col: 4}
	if !(Range{Start: a, End: a}).IsEmpty() {
		t.Fatal("equal endpoints should be empty")
	}
	b := Anchor{MessageID: "1.0", Line: 1, Col: 5}
	if (Range{Start: a, End: b}).IsEmpty() {
		t.Fatal("differing endpoints should not be empty")
	}
}

func TestRange_ContainsHalfOpen(t *testing.T) {
	lo := Anchor{MessageID: "1.0", Line: 0, Col: 2}
	hi := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	r := Range{Start: lo, End: hi}
	if !r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 2}) {
		t.Fatal("should contain lo (inclusive)")
	}
	if !r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 4}) {
		t.Fatal("should contain interior")
	}
	if r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 5}) {
		t.Fatal("should not contain hi (exclusive)")
	}
	if r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 1}) {
		t.Fatal("should not contain before lo")
	}
}

func TestAnchor_LessOrEqual(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	b := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	if !LessOrEqual(a, b) || !LessOrEqual(b, a) {
		t.Fatal("equal anchors must be <= each other")
	}
	c := Anchor{MessageID: "1.0", Line: 0, Col: 6}
	if !LessOrEqual(a, c) || LessOrEqual(c, a) {
		t.Fatal("col ordering wrong")
	}
}

func TestRange_ContainsEmptyRangeContainsNothing(t *testing.T) {
	a := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	r := Range{Start: a, End: a} // empty
	if r.Contains(a) {
		t.Fatal("empty half-open range must not contain its only point")
	}
}

func TestRange_ContainsAcceptsUnnormalizedInput(t *testing.T) {
	lo := Anchor{MessageID: "1.0", Line: 0, Col: 2}
	hi := Anchor{MessageID: "1.0", Line: 0, Col: 5}
	// Construct with End < Start; Contains must self-normalize.
	r := Range{Start: hi, End: lo}
	if !r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 3}) {
		t.Fatal("Contains must self-normalize unordered ranges")
	}
	if r.Contains(Anchor{MessageID: "1.0", Line: 0, Col: 5}) {
		t.Fatal("self-normalized range must still be half-open at hi")
	}
}

func TestRange_ContainsAcrossMessages(t *testing.T) {
	lo := Anchor{MessageID: "1700000000.000100", Line: 3, Col: 4}
	hi := Anchor{MessageID: "1700000002.000200", Line: 1, Col: 0}
	r := Range{Start: lo, End: hi}
	// Anchor in the middle message — any line/col, since the message is
	// strictly between lo.MessageID and hi.MessageID.
	mid := Anchor{MessageID: "1700000001.000050", Line: 0, Col: 0}
	if !r.Contains(mid) {
		t.Fatal("anchor in a strictly-interior message must be contained")
	}
	// Anchor in lo's message at lo's exact position is inclusive.
	if !r.Contains(lo) {
		t.Fatal("lo endpoint must be inclusive")
	}
	// Anchor in hi's message at hi's exact position is exclusive.
	if r.Contains(hi) {
		t.Fatal("hi endpoint must be exclusive")
	}
	// Anchor before lo (earlier message) excluded.
	before := Anchor{MessageID: "1699999999.999999", Line: 0, Col: 0}
	if r.Contains(before) {
		t.Fatal("anchor before lo must be excluded")
	}
}
