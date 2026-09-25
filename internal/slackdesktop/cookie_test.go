package slackdesktop

import (
	"errors"
	"fmt"
	"testing"
)

// wrongKeyPassingPadding returns a password that is NOT the one body was
// encrypted with, yet whose CBC decryption still satisfies PKCS#7: the last
// plaintext byte only has to land in 1..16, so a wrong key slips through
// roughly one time in sixteen. That accident is what let a garbage `d` cookie
// reach Slack — net/http dropped the non-ASCII bytes and the API answered
// invalid_auth, far away from the real cause.
func wrongKeyPassingPadding(t *testing.T, body []byte, rounds int) []byte {
	t.Helper()
	for i := range 512 {
		pw := []byte(fmt.Sprintf("wrong-key-%d", i))
		if _, err := decryptCBC(body, pw, rounds); err == nil {
			return pw
		}
	}
	t.Fatal("no wrong key produced valid PKCS#7 padding in 512 tries")
	return nil
}

func TestDecryptCookieValueRejectsUnprintableGarbage(t *testing.T) {
	enc := append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-real-cookie-value"), []byte("real-key"), 1003)...)
	bad := wrongKeyPassingPadding(t, enc[3:], 1003)

	got, err := decryptCookieValue("darwin", enc, func() ([][]byte, error) {
		return [][]byte{bad}, nil
	})
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("err = %v, want ErrDecryptFailed (returned %d bytes: %q)", err, len(got), got)
	}
}

// A macOS login keychain can hold more than one "Slack Safe Storage" item —
// the sandboxed App Store build and the standalone build each create their own,
// distinguished only by account name. Looking one up by service alone returns
// an arbitrary match, so the caller must be able to offer every candidate.
func TestDecryptCookieValueTriesEveryCandidate(t *testing.T) {
	want := []byte("xoxd-the-real-cookie")
	enc := append([]byte("v10"), cbcEncrypt(t, want, []byte("standalone-key"), 1003)...)

	calls := 0
	got, err := decryptCookieValue("darwin", enc, func() ([][]byte, error) {
		calls++
		return [][]byte{[]byte("app-store-key"), []byte("standalone-key")}, nil
	})
	if err != nil {
		t.Fatalf("err = %v, want the second candidate to succeed", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 1 {
		t.Errorf("password source called %d times, want 1", calls)
	}
}

func TestDecryptCookieValueNoCandidates(t *testing.T) {
	enc := append([]byte("v10"), cbcEncrypt(t, []byte("xoxd-x"), []byte("k"), 1003)...)
	if _, err := decryptCookieValue("darwin", enc, func() ([][]byte, error) {
		return nil, nil
	}); !errors.Is(err, ErrNoSecretService) {
		t.Errorf("err = %v, want ErrNoSecretService", err)
	}
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
	got, err := decryptCookieValue("linux", enc, func() ([][]byte, error) { return [][]byte{[]byte("keyring-pw")}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
