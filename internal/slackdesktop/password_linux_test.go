//go:build linux

package slackdesktop

import (
	"errors"
	"reflect"
	"testing"

	"r00t2.io/gosecret"
)

func TestFindKeyringPasswordSupportsKDEWallet(t *testing.T) {
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
			want: ErrNoSecretService,
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
