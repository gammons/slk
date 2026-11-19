package channelfinder

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/core"
)

const permalinkFixture = "https://acme.slack.com/archives/C054JFCBN69/p1779284733270139?thread_ts=1779284700.000100&cid=C054JFCBN69"

func TestPermalinkQueryShowsOnlyOpenMessage(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()

	for _, r := range permalinkFixture {
		if got := m.HandleKey(string(r)); got != nil {
			t.Fatalf("typing the permalink selected %+v", got)
		}
	}

	items := m.FilteredItems()
	if len(items) != 1 || items[0].Name != "Open message" || items[0].Type != PermalinkType || items[0].ID != permalinkFixture {
		t.Fatalf("filtered = %+v, want a single Open message for the permalink", items)
	}
	view := m.View(80)
	if !strings.Contains(view, "Open message") {
		t.Fatalf("finder view does not offer Open message:\n%s", view)
	}
	if strings.Contains(view, "No matching channels") {
		t.Fatalf("permalink query rendered as no match:\n%s", view)
	}
	if strings.Contains(view, "engineering") || strings.Contains(view, "marketing") {
		t.Fatalf("permalink query still lists channels:\n%s", view)
	}

	got := m.HandleKey("enter")
	if got == nil || got.Type != PermalinkType || got.ID != permalinkFixture || got.Name != "Open message" {
		t.Fatalf("enter = %+v, want the permalink result", got)
	}

	// One digit short of a timestamp is not a permalink, so the finder
	// goes back to channel matching.
	m.HandleKey("esc")
	m.Open()
	almost := "https://acme.slack.com/archives/C054JFCBN69/p1234567890"
	for _, r := range almost {
		m.HandleKey(string(r))
	}
	if items := m.FilteredItems(); len(items) == 1 && items[0].Name == "Open message" {
		t.Fatalf("10-digit p-value offered Open message: %+v", items)
	}
	m.HandleKey("1")
	items = m.FilteredItems()
	if len(items) != 1 || items[0].Name != "Open message" || items[0].ID != almost+"1" {
		t.Fatalf("11-digit p-value = %+v, want Open message", items)
	}
}

func TestNonPermalinkQueryKeepsChannelMatches(t *testing.T) {
	m := New()
	m.SetItems(testItems())
	m.Open()
	for _, r := range "eng" {
		m.HandleKey(string(r))
	}
	items := m.FilteredItems()
	if len(items) == 0 || items[0].ID != "C2" {
		t.Fatalf("filtered = %+v, want engineering first", items)
	}
}

func TestForwardingIgnoresPermalinkQuery(t *testing.T) {
	m := New()
	m.SetItems([]core.ChannelFinderItem{
		{ID: "C1", Name: "general", Type: "channel", Joined: true},
	})
	m.OpenForForwarding()
	for _, r := range permalinkFixture {
		m.HandleKey(string(r))
	}
	for _, item := range m.FilteredItems() {
		if item.Type == PermalinkType || item.Name == "Open message" {
			t.Fatalf("forwarding offered a permalink row: %+v", m.FilteredItems())
		}
	}
}
