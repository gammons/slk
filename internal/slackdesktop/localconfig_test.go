package slackdesktop

import (
	"os"
	"testing"
	"unicode/utf16"
)

func TestParseLocalConfig(t *testing.T) {
	data, err := os.ReadFile("testdata/localconfig.json")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := parseLocalConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	// TBROKEN has an empty token and must be skipped; 2 remain, sorted by name.
	if len(ws) != 2 {
		t.Fatalf("got %d workspaces, want 2: %+v", len(ws), ws)
	}
	if ws[0].Name != "Truelist" || ws[0].Domain != "truelist-workspace" || ws[0].TeamID != "T054JFC9S2Z" || ws[0].Token != "xoxc-truelist" {
		t.Errorf("ws[0] = %+v", ws[0])
	}
}

func TestParseLocalConfigEmpty(t *testing.T) {
	if _, err := parseLocalConfig([]byte(`{"teams":{}}`)); err != ErrNotSignedIn {
		t.Errorf("got %v, want ErrNotSignedIn", err)
	}
}

func TestDecodeLocalStorageValue(t *testing.T) {
	// 0x01 marker → UTF-8/Latin-1 remainder.
	if got := decodeLocalStorageValue(append([]byte{0x01}, []byte(`{"a":1}`)...)); got != `{"a":1}` {
		t.Errorf("latin1 decode = %q", got)
	}
	// 0x00 marker → UTF-16LE remainder.
	u16 := utf16.Encode([]rune(`{"a":1}`))
	b := []byte{0x00}
	for _, c := range u16 {
		b = append(b, byte(c), byte(c>>8))
	}
	if got := decodeLocalStorageValue(b); got != `{"a":1}` {
		t.Errorf("utf16 decode = %q", got)
	}
}
