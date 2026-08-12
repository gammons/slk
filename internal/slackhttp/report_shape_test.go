package slackhttp

import (
	"strings"
	"testing"
)

// The shutdown hook in cmd/slk/main.go prints exactly this. main() has
// no test seam (recorded in the Phase 1 outcomes), so pin the thing it
// prints instead.
func TestDefaultCounterReport_ShapeForShutdownDump(t *testing.T) {
	c := &Counter{}
	c.Record("https://slack.com/api/users.list")
	c.Record("https://slack.com/api/users.list")
	c.Record("https://slack.com/api/client.userBoot")
	c.Record("https://edgeapi.slack.com/cache/T1/channels/info")

	got := c.Report()
	for _, want := range []string{
		"API requests: 4 total across 3 endpoints",
		"    2  users.list",
		"    1  client.userBoot",
		"    1  edge:channels/info",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Report() missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "\n") != 4 {
		t.Errorf("Report() = %d lines; want 4 (header + 3 endpoints):\n%s", strings.Count(got, "\n"), got)
	}
}
