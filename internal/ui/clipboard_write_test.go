package ui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDefaultClipboardWriterUsesBubbleTeaOSC52(t *testing.T) {
	const text = "copied through the terminal"
	gotCmd := defaultClipboardWriter(text)
	wantCmd := tea.SetClipboard(text)
	if gotCmd == nil {
		t.Fatal("defaultClipboardWriter returned nil")
	}

	got, want := gotCmd(), wantCmd()
	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		t.Fatalf("clipboard message type = %T, want %T from tea.SetClipboard", got, want)
	}
	value := reflect.ValueOf(got)
	if value.Kind() != reflect.String || value.String() != text {
		t.Fatalf("clipboard message payload = %#v, want %q", got, text)
	}
}
