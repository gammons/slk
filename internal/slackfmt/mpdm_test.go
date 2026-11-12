package slackfmt

import "testing"

func TestFormatMPDMName(t *testing.T) {
	// Lookup table mapping handles to display names. Anything missing
	// returns "" so the formatter falls back to the handle itself.
	display := map[string]string{
		"grant": "Grant Ammons",
		"myles": "Myles Williamson",
		"ray":   "Ray Bradbury",
		"alice": "Alice A.",
		"bob.r": "Bob R.",
	}
	lookup := func(handle string) string { return display[handle] }

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "three handles, all resolved",
			in:   "mpdm-grant--myles--ray-1",
			want: "Grant Ammons, Myles Williamson, Ray Bradbury",
		},
		{
			name: "two handles, all resolved",
			in:   "mpdm-grant--myles-1",
			want: "Grant Ammons, Myles Williamson",
		},
		{
			name: "missing display name falls back to handle",
			in:   "mpdm-grant--unknown--ray-1",
			want: "Grant Ammons, unknown, Ray Bradbury",
		},
		{
			name: "handles containing dots are preserved",
			in:   "mpdm-bob.r--alice-1",
			want: "Bob R., Alice A.",
		},
		{
			name: "trailing index is multi-digit",
			in:   "mpdm-grant--myles--ray-42",
			want: "Grant Ammons, Myles Williamson, Ray Bradbury",
		},
		{
			name: "no trailing index (defensive)",
			in:   "mpdm-grant--myles--ray",
			want: "Grant Ammons, Myles Williamson, Ray Bradbury",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatMPDMName(tc.in, "", lookup)
			if got != tc.want {
				t.Errorf("FormatMPDMName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFormatMPDMName_FallsBackOnUnparseable returns the original
// string when the input doesn't match the mpdm naming convention.
func TestFormatMPDMName_FallsBackOnUnparseable(t *testing.T) {
	lookup := func(string) string { return "" }
	cases := []string{
		"general",             // not an mpdm name
		"mpdm-",               // empty body
		"mpdm",                // missing prefix dash
		"",                    // empty
		"mpim-grant--myles-1", // wrong prefix
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := FormatMPDMName(in, "", lookup)
			if got != in {
				t.Errorf("FormatMPDMName(%q) = %q, want unchanged %q", in, got, in)
			}
		})
	}
}

// TestFormatMPDMName_NilLookup uses the handles as display names when
// no lookup is provided.
func TestFormatMPDMName_NilLookup(t *testing.T) {
	got := FormatMPDMName("mpdm-grant--myles--ray-1", "", nil)
	want := "grant, myles, ray"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatMPDMName_DropsSelf leaves the current user out of the
// list: they are in every group DM they can see, so their name says
// nothing about which group DM it is.
func TestFormatMPDMName_DropsSelf(t *testing.T) {
	display := map[string]string{
		"grant": "Grant Ammons",
		"myles": "Myles Williamson",
		"ray":   "Ray Bradbury",
	}
	lookup := func(handle string) string { return display[handle] }

	cases := []struct {
		name string
		in   string
		self string
		want string
	}{
		{
			name: "self first",
			in:   "mpdm-grant--myles--ray-1",
			self: "grant",
			want: "Myles Williamson, Ray Bradbury",
		},
		{
			name: "self in the middle",
			in:   "mpdm-myles--grant--ray-1",
			self: "grant",
			want: "Myles Williamson, Ray Bradbury",
		},
		{
			name: "self last",
			in:   "mpdm-myles--ray--grant-1",
			self: "grant",
			want: "Myles Williamson, Ray Bradbury",
		},
		{
			name: "self not a participant",
			in:   "mpdm-myles--ray-1",
			self: "grant",
			want: "Myles Williamson, Ray Bradbury",
		},
		{
			name: "self is a prefix of another handle",
			in:   "mpdm-grant--grantm--ray-1",
			self: "grant",
			want: "grantm, Ray Bradbury",
		},
		{
			name: "self is the only participant left",
			in:   "mpdm-grant-1",
			self: "grant",
			want: "Grant Ammons",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatMPDMName(tc.in, tc.self, lookup)
			if got != tc.want {
				t.Errorf("FormatMPDMName(%q, %q) = %q, want %q", tc.in, tc.self, got, tc.want)
			}
		})
	}
}
