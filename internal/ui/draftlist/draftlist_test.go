package draftlist

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func rows() []Row {
	return []Row{
		{Destination: "#general", Age: "5m", Preview: "hello there"},
		{Destination: "#ops › thread", Age: "2h", Marker: "scheduled Dec 1 9:00 AM", Preview: "deploy"},
	}
}

func TestHandleKey(t *testing.T) {
	m := New()
	m.SetRows(rows())
	m.Open()
	if act, _ := m.HandleKey("j"); act != None {
		t.Errorf("j = %v", act)
	}
	if act, i := m.HandleKey("enter"); act != Jump || i != 1 {
		t.Errorf("enter = %v %d, want Jump 1", act, i)
	}
	m.HandleKey("k")
	if act, i := m.HandleKey("d"); act != Delete || i != 0 {
		t.Errorf("d = %v %d, want Delete 0", act, i)
	}
	m.HandleKey("esc")
	if m.IsVisible() {
		t.Error("esc did not close")
	}
}

func TestHandleKey_EmptyListActsOnNothing(t *testing.T) {
	m := New()
	m.Open()
	for _, k := range []string{"enter", "d"} {
		if act, _ := m.HandleKey(k); act != None {
			t.Errorf("%s on empty list = %v", k, act)
		}
	}
}

func TestView(t *testing.T) {
	m := New()
	m.SetRows(rows())
	m.Open()
	v := ansi.Strip(m.View(100, 30))
	for _, want := range []string{"Drafts", "#general", "5m", "hello there", "scheduled Dec 1 9:00 AM"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	m.SetRows(nil)
	if v := ansi.Strip(m.View(100, 30)); !strings.Contains(v, "No drafts") {
		t.Errorf("empty view:\n%s", v)
	}
}
