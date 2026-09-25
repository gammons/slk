//go:build linux

package slackdesktop

import (
	"errors"
	"reflect"
	"testing"

	"r00t2.io/gosecret"
)

// Covers the QtKeychain schema as served over the Secret Service, i.e. where
// kwalletd bridges onto org.freedesktop.secrets. Not coverage of KDE generally
// — see readKWalletPassword for the unbridged case.
func TestFindKeyringPasswordSupportsQtKeychainOverSecretService(t *testing.T) {
	var queries []map[string]string
	wantPassword := []byte("kde-slack-safe-storage")

	got, err := findKeyringPassword(func(attrs map[string]string) ([]*gosecret.Item, []*gosecret.Item, error) {
		queries = append(queries, cloneStringMap(attrs))
		if attrs["xdg:schema"] == "org.qt.keychain" {
			return []*gosecret.Item{secretItem(wantPassword)}, nil, nil
		}
		return nil, nil, nil
	})
	if err != nil {
		t.Fatalf("findKeyringPassword: %v", err)
	}
	if !reflect.DeepEqual(got, wantPassword) {
		t.Fatalf("password = %q, want %q", got, wantPassword)
	}

	wantQueries := []map[string]string{
		{
			"xdg:schema":  "chrome_libsecret_os_crypt_password_v2",
			"application": "Slack",
		},
		{
			"xdg:schema":  "chrome_libsecret_os_crypt_password_v1",
			"application": "Slack",
		},
		{
			"xdg:schema": "org.qt.keychain",
			"server":     "Slack Keys",
			"user":       "Slack Safe Storage",
		},
	}
	if !reflect.DeepEqual(queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", queries, wantQueries)
	}
}

func TestFindKeyringPasswordPrefersUnlockedItemAcrossSchemas(t *testing.T) {
	wantPassword := []byte("unlocked-kde-secret")

	got, err := findKeyringPassword(func(attrs map[string]string) ([]*gosecret.Item, []*gosecret.Item, error) {
		switch attrs["xdg:schema"] {
		case "chrome_libsecret_os_crypt_password_v2":
			return nil, []*gosecret.Item{{}}, nil
		case "org.qt.keychain":
			return []*gosecret.Item{secretItem(wantPassword)}, nil, nil
		default:
			return nil, nil, nil
		}
	})
	if err != nil {
		t.Fatalf("findKeyringPassword: %v", err)
	}
	if !reflect.DeepEqual(got, wantPassword) {
		t.Fatalf("password = %q, want %q", got, wantPassword)
	}
}

func TestFindKeyringPasswordErrors(t *testing.T) {
	searchErr := errors.New("search failed")

	tests := []struct {
		name   string
		search searchSecretItemsFunc
		want   error
	}{
		{
			name: "no matching item",
			search: func(map[string]string) ([]*gosecret.Item, []*gosecret.Item, error) {
				return nil, nil, nil
			},
			want: ErrSecretNotFound,
		},
		{
			name: "matching item is locked",
			search: func(attrs map[string]string) ([]*gosecret.Item, []*gosecret.Item, error) {
				if attrs["xdg:schema"] == "org.qt.keychain" {
					return nil, []*gosecret.Item{{}}, nil
				}
				return nil, nil, nil
			},
			want: ErrKeyringLocked,
		},
		{
			name: "search failure",
			search: func(map[string]string) ([]*gosecret.Item, []*gosecret.Item, error) {
				return nil, nil, searchErr
			},
			want: searchErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findKeyringPassword(tt.search)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func secretItem(value []byte) *gosecret.Item {
	return &gosecret.Item{
		Secret: &gosecret.Secret{Value: gosecret.SecretValue(value)},
	}
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

// source is a keyring source with a fixed result.
func source(pws [][]byte, err error) func() ([][]byte, error) {
	return func() ([][]byte, error) { return pws, err }
}

// Both answers are kept: decryptCookieValue validates each, so an extra
// candidate costs a failed decrypt while dropping one can lose the right key.
func TestCollectKeyringPasswordsMergesEverySource(t *testing.T) {
	got, err := collectKeyringPasswords(
		source([][]byte{[]byte("from-secret-service")}, nil),
		source([][]byte{[]byte("from-kwallet")}, nil),
	)
	if err != nil {
		t.Fatalf("collectKeyringPasswords: %v", err)
	}
	want := [][]byte{[]byte("from-secret-service"), []byte("from-kwallet")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("passwords = %q, want %q", got, want)
	}
}

// The KDE bug: Secret Service answers and has nothing, key is in KWallet.
func TestCollectKeyringPasswordsSucceedsWhenOnlyKWalletHasTheKey(t *testing.T) {
	got, err := collectKeyringPasswords(
		source(nil, ErrSecretNotFound),
		source([][]byte{[]byte("from-kwallet")}, nil),
	)
	if err != nil {
		t.Fatalf("collectKeyringPasswords: %v", err)
	}
	if want := [][]byte{[]byte("from-kwallet")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("passwords = %q, want %q", got, want)
	}
}

// An absent or broken store must not mask a key another store produced.
func TestCollectKeyringPasswordsIgnoresFailuresWhenAKeyWasFound(t *testing.T) {
	for _, otherErr := range []error{ErrNoSecretService, ErrKeyringLocked, errors.New("boom")} {
		got, err := collectKeyringPasswords(
			source([][]byte{[]byte("found")}, nil),
			source(nil, otherErr),
		)
		if err != nil {
			t.Fatalf("collectKeyringPasswords with %v: %v", otherErr, err)
		}
		if want := [][]byte{[]byte("found")}; !reflect.DeepEqual(got, want) {
			t.Fatalf("passwords = %q, want %q", got, want)
		}
	}
}

func TestCollectKeyringPasswordsErrorPrecedence(t *testing.T) {
	boom := errors.New("dbus: connection closed")

	tests := []struct {
		name    string
		sources []func() ([][]byte, error)
		want    error
	}{
		{
			name:    "no sources at all",
			sources: nil,
			want:    ErrSecretNotFound,
		},
		{
			name: "neither store exists",
			sources: []func() ([][]byte, error){
				source(nil, ErrNoSecretService),
				source(nil, ErrNoSecretService),
			},
			want: ErrNoSecretService,
		},
		{
			// "Answered and empty" is more informative than "not installed".
			name: "answered and empty beats absent",
			sources: []func() ([][]byte, error){
				source(nil, ErrNoSecretService),
				source(nil, ErrSecretNotFound),
			},
			want: ErrSecretNotFound,
		},
		{
			// The only one the user can act on.
			name: "locked beats not found",
			sources: []func() ([][]byte, error){
				source(nil, ErrSecretNotFound),
				source(nil, ErrKeyringLocked),
			},
			want: ErrKeyringLocked,
		},
		{
			name: "locked beats a transport error",
			sources: []func() ([][]byte, error){
				source(nil, boom),
				source(nil, ErrKeyringLocked),
			},
			want: ErrKeyringLocked,
		},
		{
			// Points at the bus; "not found" would point at Slack.
			name: "transport error beats not found",
			sources: []func() ([][]byte, error){
				source(nil, ErrSecretNotFound),
				source(nil, boom),
			},
			want: boom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collectKeyringPasswords(tt.sources...)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if got != nil {
				t.Errorf("passwords = %q, want none alongside an error", got)
			}
		})
	}
}
