package blockkit

import "testing"

func TestResolveAttachmentColorNamedTokensDistinctFromBorder(t *testing.T) {
	border := ResolveAttachmentColor("")
	for _, name := range []string{"good", "warning", "danger"} {
		got := ResolveAttachmentColor(name)
		if got == "" {
			t.Errorf("%q returned empty", name)
		}
		if got == border {
			t.Errorf("%q resolved to border fallback %q; expected a distinct theme color", name, got)
		}
	}
}

func TestResolveAttachmentColorPassthroughHex(t *testing.T) {
	cases := []string{"#FF0000", "#00ff00", "#1a1a1a"}
	for _, in := range cases {
		got := ResolveAttachmentColor(in)
		if got != in {
			t.Errorf("ResolveAttachmentColor(%q) = %q, want passthrough", in, got)
		}
	}
}

func TestResolveAttachmentColorEmptyFallsBackToBorder(t *testing.T) {
	got := ResolveAttachmentColor("")
	if got == "" {
		t.Error("expected non-empty fallback color for empty input")
	}
}

func TestResolveAttachmentColorInvalidFallsBackToBorder(t *testing.T) {
	got := ResolveAttachmentColor("not-a-color")
	if got == "" {
		t.Error("expected fallback for invalid input")
	}
	if got == "not-a-color" {
		t.Error("invalid input should not be returned verbatim")
	}
}
