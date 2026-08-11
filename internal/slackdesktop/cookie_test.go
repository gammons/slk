package slackdesktop

import (
	"errors"
	"testing"
)

func singlePassword(pw string) func() ([][]byte, error) {
	return func() ([][]byte, error) { return [][]byte{[]byte(pw)}, nil }
}

func TestDecryptCookieValueLinuxPeanuts(t *testing.T) {
	// v10 on linux -> password "peanuts", rounds 1
	want := []byte("xoxd-linux-peanuts")
	enc := append([]byte("v10"), cbcEncrypt(t, want, []byte("peanuts"), 1)...)
	got, err := decryptCookieValue("linux", enc, func() ([][]byte, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecryptCookieValueLinuxKeyring(t *testing.T) {
	want := []byte("xoxd-linux-keyring")
	enc := append([]byte("v11"), cbcEncrypt(t, want, []byte("keyring-pw"), 1)...)
	got, err := decryptCookieValue("linux", enc, singlePassword("keyring-pw"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecryptCookieValueDarwinTriesEachPassword(t *testing.T) {
	// macOS keychain can hold several "Slack Safe Storage" items (stale ones
	// from older installs). The first candidate may be the wrong password;
	// slk must keep trying until one yields a real cookie.
	want := []byte("xoxd-current")
	enc := append([]byte("v10"), cbcEncrypt(t, want, []byte("current-pw"), 1003)...)
	getPasswords := func() ([][]byte, error) {
		return [][]byte{[]byte("stale-pw"), []byte("current-pw")}, nil
	}
	got, err := decryptCookieValue("darwin", enc, getPasswords)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecryptCookieValueAllPasswordsFail(t *testing.T) {
	enc := append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-current"), []byte("current-pw"), 1003)...)
	getPasswords := func() ([][]byte, error) {
		return [][]byte{[]byte("stale-pw"), []byte("other-pw")}, nil
	}
	if _, err := decryptCookieValue("darwin", enc, getPasswords); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("err = %v, want ErrDecryptFailed", err)
	}
}

func TestDecryptCookieValueRejectsNonCookieValue(t *testing.T) {
	// Decryption with a wrong key occasionally passes the padding check and
	// yields garbage. Values that don't look like a session cookie must be
	// rejected rather than returned.
	enc := append([]byte("v11"), cbcEncrypt(t, []byte("not-a-cookie"), []byte("keyring-pw"), 1)...)
	if _, err := decryptCookieValue("linux", enc, singlePassword("keyring-pw")); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("err = %v, want ErrDecryptFailed", err)
	}
}

func TestDecryptCookieValueNoPasswords(t *testing.T) {
	enc := append([]byte("v11"), cbcEncrypt(t, []byte("xoxd-x"), []byte("pw"), 1)...)
	getPasswords := func() ([][]byte, error) { return nil, nil }
	if _, err := decryptCookieValue("linux", enc, getPasswords); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("err = %v, want ErrDecryptFailed", err)
	}
}
