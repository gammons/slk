package confirmprompt

import (
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// An unchanged View returns the cached string without rendering: it does not
// allocate, which a lipgloss render always does.
func TestViewUnchangedReturnsCache(t *testing.T) {
	m := benchModel(benchBody)
	first := m.View()
	if first == "" {
		t.Fatal("View is empty")
	}
	if got := m.View(); got != first {
		t.Fatalf("second View differs:\n%q\n%q", first, got)
	}
	if n := testing.AllocsPerRun(50, func() { _ = m.View() }); n != 0 {
		t.Fatalf("warm View allocated %v times, want 0", n)
	}
}

// Every input that shapes the box invalidates the cache: after the change,
// View equals what a model that never cached would draw.
func TestViewCacheInvalidation(t *testing.T) {
	changes := map[string]func(m *Model){
		"width":     func(m *Model) { m.SetWidth(200) },
		"height":    func(m *Model) { m.SetHeight(3) }, // no room left: the body is dropped
		"highlight": func(m *Model) { m.Highlight(1) },
		"choice": func(m *Model) {
			c := m.choice
			c.Title, c.Body = "Other?", "another body"
			m.OpenChoice(c)
		},
		"label": func(m *Model) {
			c := m.choice
			c.Buttons = []Button{{Label: "Go", Keys: key.NewBinding(key.WithKeys("g"))}}
			m.OpenChoice(c)
		},
		"keymap": func(m *Model) { m.KeyMap.Confirm = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "sure")) },
		"styles": func(m *Model) {
			s := themedStyles()
			s.Title = lipgloss.NewStyle().Italic(true)
			m.SetStyles(s)
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			m := benchModel(benchBody)
			before := m.View()
			change(&m)
			fresh := m
			fresh.cache = renderCache{}
			want := fresh.View()
			if got := m.View(); got != want {
				t.Fatalf("View after change is stale:\n got %q\nwant %q", got, want)
			}
			if want == before {
				t.Fatalf("change %q did not alter the render; test is vacuous", name)
			}
		})
	}
}
