package config

import (
	"reflect"
	"testing"
)

func TestIsTeamIDKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"T01ABCDEF", true},
		{"E0123456", true},
		{"work", false},
		{"acme-corp", false},
		{"T01", false},       // only 2 chars after T; regex requires 6+
		{"T0", false},        // too short
		{"t01abcdef", false}, // lowercase not a Slack team ID
		{"", false},
	}
	for _, tc := range cases {
		if got := isTeamIDKey(tc.key); got != tc.want {
			t.Errorf("isTeamIDKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

// TestWorkspaceStateOnlyAllowList is the field inventory for the
// fold in resolveWorkspaceKeys. Workspace's fields, in struct order,
// are listed here on purpose: adding one fails the test until it is
// classified. State slk writes (version_ts, theme, sidebar_width, and
// a team_id that restates the key) may be folded. Anything else must
// refuse the fold, and mergeWorkspaceState must copy a state field
// the named block left empty.
func TestWorkspaceStateOnlyAllowList(t *testing.T) {
	want := []string{
		"TeamID", "Theme", "PermalinkDomains", "Order", "SidebarWidth",
		"UseSlackSections", "Sections", "VersionTS", "Suppress",
	}
	rt := reflect.TypeOf(Workspace{})
	got := make([]string, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		got[i] = rt.Field(i).Name
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Workspace fields = %v, want %v\nclassify the new field in workspaceStateOnly and mergeWorkspaceState", got, want)
	}

	const key = "T01ABCDEF"
	on := true
	cases := []struct {
		name string
		key  string
		w    Workspace
		want bool
	}{
		{name: "team id only", w: Workspace{TeamID: key}, want: true},
		{name: "version_ts", w: Workspace{VersionTS: "1795162663"}, want: true},
		{name: "theme", w: Workspace{Theme: "nord"}, want: true},
		{name: "sidebar width", w: Workspace{SidebarWidth: 28}, want: true},
		{name: "all state", w: Workspace{TeamID: key, VersionTS: "1795162663", Theme: "nord", SidebarWidth: 28}, want: true},
		{name: "order", w: Workspace{Order: 1}},
		{name: "use slack sections", w: Workspace{UseSlackSections: &on}},
		{name: "permalink domains", w: Workspace{PermalinkDomains: []string{"alias.slack.com"}}},
		{name: "sections", w: Workspace{Sections: map[string]SectionDef{"eng": {}}}},
		{name: "suppress", w: Workspace{Suppress: []SuppressRule{{Author: "U1"}}}},
		{name: "different team id", w: Workspace{TeamID: "T02OOOOOO"}},
		{name: "slug key", key: "my-company", w: Workspace{VersionTS: "1795162663"}},
	}
	for _, tc := range cases {
		k := key
		if tc.key != "" {
			k = tc.key
		}
		if got := workspaceStateOnly(k, tc.w); got != tc.want {
			t.Errorf("%s: state-only %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Acme Inc.", "acme-inc"},
		{"ACME", "acme"},
		{"  hello  world  ", "hello-world"},
		{"foo/bar_baz", "foo-bar-baz"},
		{"---trim---", "trim"},
		{"", ""},
		{"!!!", ""},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
