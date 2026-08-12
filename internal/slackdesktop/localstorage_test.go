package slackdesktop

import (
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
)

func TestDecodeLSValue(t *testing.T) {
	// Latin-1 tag (0x01): rest is the bytes verbatim.
	if got := decodeLSValue(append([]byte{0x01}, []byte(`{"a":1}`)...)); got != `{"a":1}` {
		t.Errorf("latin1 decode = %q", got)
	}
	// UTF-16 tag (0x00): rest is UTF-16LE.
	u16 := utf16.Encode([]rune(`{"b":2}`))
	b := []byte{0x00}
	for _, c := range u16 {
		b = append(b, byte(c), byte(c>>8))
	}
	if got := decodeLSValue(b); got != `{"b":2}` {
		t.Errorf("utf16 decode = %q", got)
	}
}

func TestTokensFromLevelDB(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "leveldb")
	db, err := leveldb.OpenFile(dbDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Mimic Chromium's key layout ("_<origin>\x00\x01<key>") and Latin-1 value
	// tag; only the localConfig_v2 suffix and the 0x01 value tag matter here.
	key := append([]byte("_https://app.slack.com\x00\x01"), []byte(localConfigKey)...)
	val := append([]byte{0x01}, []byte(`{"teams":{"T1":{"token":"xoxc-aaa"},"T2":{"token":"xoxc-bbb"}}}`)...)
	if err := db.Put(key, val, nil); err != nil {
		t.Fatal(err)
	}
	// An unrelated key must be ignored.
	if err := db.Put([]byte("_https://app.slack.com\x00\x01other"), []byte{0x01, '{', '}'}, nil); err != nil {
		t.Fatal(err)
	}
	db.Close()

	tokens, err := tokensFromLevelDB(dbDir)
	if err != nil {
		t.Fatal(err)
	}
	if tokens["T1"] != "xoxc-aaa" || tokens["T2"] != "xoxc-bbb" {
		t.Fatalf("unexpected tokens: %+v", tokens)
	}
}

func TestTokensFromLevelDBNoConfig(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "leveldb")
	db, err := leveldb.OpenFile(dbDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := tokensFromLevelDB(dbDir); err != ErrNotSignedIn {
		t.Fatalf("want ErrNotSignedIn, got %v", err)
	}
}
