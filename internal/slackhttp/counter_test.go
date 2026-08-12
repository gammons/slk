package slackhttp

import (
	"sync"
	"testing"
)

func TestCounter_TalliesByEndpoint(t *testing.T) {
	var c Counter
	c.Record("https://slack.com/api/conversations.history")
	c.Record("https://slack.com/api/conversations.history")
	c.Record("https://slack.com/api/client.counts")
	c.Record("https://edgeapi.slack.com/cache/T1/users/info")

	got := c.Snapshot()
	for _, tc := range []struct {
		endpoint string
		want     int
	}{
		{"conversations.history", 2},
		{"client.counts", 1},
		{"edge:users/info", 1},
	} {
		if got[tc.endpoint] != tc.want {
			t.Errorf("Snapshot()[%q] = %d; want %d (full: %v)", tc.endpoint, got[tc.endpoint], tc.want, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("Snapshot() has %d endpoints; want 3 (%v)", len(got), got)
	}
}

func TestCounter_IgnoresNonAPIURLs(t *testing.T) {
	// Asset fetches are not API calls and must not inflate the tally
	// the success criteria are measured against.
	var c Counter
	c.Record("https://files.slack.com/files-tmb/T1-F2/image_360.png")
	c.Record("https://ca.slack-edge.com/T1-U2-abc/avatar")
	c.Record("https://emoji.slack-edge.com/T1/party/abc.gif")
	if got := c.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot() = %v; want empty — asset hosts are not API calls", got)
	}
}

func TestCounter_IgnoresAssetPathsThatContainAPI(t *testing.T) {
	// Found during manual QA: an image URL whose PATH contains /api/
	// was tallied as an API call, showing up in a boot report as
	// "1  v1/images/stellar/prod/card-20260730181521756.png".
	//
	// Slack Web API method names never contain a slash -- they are
	// users.info, conversations.history, client.userBoot. Anything
	// with a path separator after /api/ is something else, and the
	// whole point of this counter is that the numbers in the success
	// criteria can be quoted without qualification.
	var c Counter
	c.Record("https://slack.com/api/v1/images/stellar/prod/card-20260730181521756.png")
	c.Record("https://example.invalid/api/some/nested/thing")
	if got := c.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot() = %v; want empty -- a slash-bearing path after /api/ is not a Slack method", got)
	}

	// The real ones must still count.
	c.Record("https://slack.com/api/users.info")
	if got := c.Snapshot()["users.info"]; got != 1 {
		t.Errorf("users.info = %d; want 1 -- the exclusion must not swallow real methods", got)
	}
}

func TestCounter_SnapshotIsACopy(t *testing.T) {
	var c Counter
	c.Record("https://slack.com/api/client.counts")
	got := c.Snapshot()
	got["client.counts"] = 999
	if again := c.Snapshot(); again["client.counts"] != 1 {
		t.Errorf("mutating a Snapshot changed the counter: %d", again["client.counts"])
	}
}

func TestCounter_ConcurrentRecordIsSafe(t *testing.T) {
	// RoundTrip runs on many goroutines; a map write race here would
	// crash the process it is meant to be observing.
	var c Counter
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Record("https://slack.com/api/client.counts")
		}()
	}
	wg.Wait()
	if got := c.Snapshot()["client.counts"]; got != 50 {
		t.Errorf("client.counts = %d; want 50", got)
	}
}

func TestCounter_TotalAndZeroValueUsable(t *testing.T) {
	var c Counter // deliberately not constructed
	if c.Total() != 0 {
		t.Errorf("zero-value Total() = %d; want 0", c.Total())
	}
	// Deliberately a repeated endpoint plus a distinct one, so the
	// number of REQUESTS (3) differs from the number of ENDPOINTS (2).
	// With one call per endpoint the two are equal and this assertion
	// cannot tell "sum of counts" from "len(map)" — a Total that
	// returned the endpoint count would pass.
	c.Record("https://slack.com/api/a.b")
	c.Record("https://slack.com/api/a.b")
	c.Record("https://slack.com/api/c.d")
	if c.Total() != 3 {
		t.Errorf("Total() = %d; want 3 (requests, not distinct endpoints: %v)", c.Total(), c.Snapshot())
	}
}

func TestCounter_ReportOrdersByCountThenName(t *testing.T) {
	// Report's whole job is to be read and diffed by a human comparing
	// a before-run against an after-run, so both halves of its order
	// are load-bearing: highest-count-first puts the fan-out that
	// Phase 2b is deleting at the top, and the name tiebreak is what
	// makes two runs with the same shape produce byte-identical text.
	// Go randomizes map iteration, so without the tiebreak equal-count
	// rows shuffle between runs and every diff is noise.
	var c Counter
	for i := 0; i < 7; i++ {
		c.Record("https://slack.com/api/conversations.history")
	}
	for i := 0; i < 3; i++ {
		c.Record("https://slack.com/api/client.counts")
	}
	// zebra and alpha are deliberately tied at 3 with client.counts,
	// and inserted out of alphabetical order.
	for i := 0; i < 3; i++ {
		c.Record("https://slack.com/api/zebra.method")
	}
	for i := 0; i < 3; i++ {
		c.Record("https://slack.com/api/alpha.method")
	}
	c.Record("https://edgeapi.slack.com/cache/T1/users/info")

	want := "API requests: 17 total across 5 endpoints\n" +
		"      7  conversations.history\n" +
		"      3  alpha.method\n" +
		"      3  client.counts\n" +
		"      3  zebra.method\n" +
		"      1  edge:users/info\n"
	if got := c.Report(); got != want {
		t.Errorf("Report() =\n%s\nwant:\n%s", got, want)
	}
}

func TestCounter_ReportIsDeterministic(t *testing.T) {
	// The ordering test above uses one Counter, so a single unlucky
	// map iteration could make it pass by accident. Build the same
	// tally repeatedly in different insertion orders: every Report
	// must be byte-identical, or "diff two runs" does not work.
	build := func(order []string) string {
		var c Counter
		for _, name := range order {
			for i := 0; i < 2; i++ {
				c.Record("https://slack.com/api/" + name)
			}
		}
		return c.Report()
	}
	first := build([]string{"a.one", "b.two", "c.three", "d.four", "e.five"})
	for _, order := range [][]string{
		{"e.five", "d.four", "c.three", "b.two", "a.one"},
		{"c.three", "a.one", "e.five", "b.two", "d.four"},
		{"b.two", "e.five", "a.one", "d.four", "c.three"},
	} {
		if got := build(order); got != first {
			t.Errorf("Report() differs by insertion order %v:\ngot:\n%s\nfirst:\n%s", order, got, first)
		}
	}
	// And repeated calls on one Counter agree with each other.
	var c Counter
	c.Record("https://slack.com/api/x.y")
	c.Record("https://slack.com/api/z.w")
	if a, b := c.Report(), c.Report(); a != b {
		t.Errorf("two Report() calls on one Counter differ:\n%s\nvs\n%s", a, b)
	}
}

func TestCounter_ReportOnZeroValue(t *testing.T) {
	// Reported at the end of a boot that made no API calls at all —
	// which, post-Phase-2b, is close to the point. It must say so
	// rather than panic on the nil map.
	var c Counter
	want := "API requests: 0 total across 0 endpoints\n"
	if got := c.Report(); got != want {
		t.Errorf("Report() = %q; want %q", got, want)
	}
}

func TestEndpointName(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{"workspace API", "https://slack.com/api/conversations.history", "conversations.history", true},
		{"workspace API with query", "https://slack.com/api/client.counts?_x_id=1&fp=6e", "client.counts", true},
		{"edgeapi", "https://edgeapi.slack.com/cache/T1/users/info", "edge:users/info", true},

		// The dead port-strip in endpointName was deleted because
		// isEdgeAPIHost strips its own port. This is what says so: an
		// edgeapi URL with an explicit port must still classify.
		{"edgeapi with port", "https://edgeapi.slack.com:443/cache/T1/channels/info", "edge:channels/info", true},

		// A diagnostic that crashes the process it observes is the
		// worst possible failure mode, so the degenerate edgeapi
		// shapes get their own cases. "https://edgeapi.slack.com/"
		// splits to [""] — one element — and taking the
		// second-to-last segment of that indexes parts[-1].
		{"edgeapi root", "https://edgeapi.slack.com/", "", false},
		{"edgeapi host only", "https://edgeapi.slack.com", "", false},
		{"edgeapi one segment", "https://edgeapi.slack.com/cache", "", false},

		// /api/ is matched as a PREFIX, matching isWorkspaceAPIPath.
		// Substring matching would name this endpoint "bar" and count
		// a non-Slack host as a Slack API call.
		{"api not at path root", "https://evil.io/foo/api/bar", "", false},
		{"empty api path", "https://slack.com/api/", "", false},
		{"non-API slack path", "https://slack.com/messages/C123", "", false},
		{"asset host", "https://files.slack.com/files-tmb/T1-F2/image_360.png", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := endpointName(tc.url)
			if got != tc.want || ok != tc.ok {
				t.Errorf("endpointName(%q) = (%q, %v); want (%q, %v)", tc.url, got, ok, tc.want, tc.ok)
			}
		})
	}
}
