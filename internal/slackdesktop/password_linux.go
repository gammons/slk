//go:build linux

package slackdesktop

import (
	"errors"

	"r00t2.io/gosecret"
)

type searchSecretItemsFunc func(map[string]string) (unlocked, locked []*gosecret.Item, err error)

var slackSecretQueries = []map[string]string{
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

// keyringPasswords returns every "Slack Safe Storage" key candidate on this
// machine. Electron picks its backend from the desktop session — kwallet on
// KDE, gnome-libsecret elsewhere — and KWallet is not reachable over
// org.freedesktop.secrets, so both stores have to be asked.
//
// decryptCookieValue tries each candidate and validates the result, so a store
// holding a stale key costs a failed decrypt rather than a hard error.
func keyringPasswords() ([][]byte, error) {
	return collectKeyringPasswords(secretServicePasswords, kwalletPasswords)
}

// collectKeyringPasswords merges the sources and, if none yielded a key, reports
// the most actionable failure: a locked store the user can unlock, then an
// unexpected error, then a store that answered and lacked the entry, then no
// store at all.
func collectKeyringPasswords(sources ...func() ([][]byte, error)) ([][]byte, error) {
	var (
		pws         [][]byte
		locked      bool
		notFound    bool
		unavailable bool
		lastErr     error
	)

	for _, source := range sources {
		got, err := source()
		pws = append(pws, got...)
		switch {
		case err == nil:
		case errors.Is(err, ErrKeyringLocked):
			locked = true
		case errors.Is(err, ErrSecretNotFound):
			notFound = true
		case errors.Is(err, ErrNoSecretService):
			unavailable = true
		default:
			lastErr = err
		}
	}

	if len(pws) > 0 {
		return pws, nil
	}
	switch {
	case locked:
		return nil, ErrKeyringLocked
	case lastErr != nil:
		return nil, lastErr
	case notFound:
		return nil, ErrSecretNotFound
	case unavailable:
		return nil, ErrNoSecretService
	default:
		return nil, ErrSecretNotFound
	}
}

// secretServicePasswords fetches the "Slack Safe Storage" password from the
// Secret Service: Chromium's schema for the gnome-libsecret backend, plus the
// QtKeychain one for setups where kwalletd bridges itself onto
// org.freedesktop.secrets.
//
// The queries are attribute-qualified, so unlike macOS there is no ambiguity:
// a single candidate is returned.
func secretServicePasswords() ([][]byte, error) {
	service, err := gosecret.NewService()
	if err != nil {
		return nil, ErrNoSecretService
	}
	defer service.Close()

	pw, err := findKeyringPassword(service.SearchItems)
	if err != nil {
		return nil, err
	}
	return [][]byte{pw}, nil
}

func findKeyringPassword(searchItems searchSecretItemsFunc) ([]byte, error) {
	foundLocked := false
	for _, attrs := range slackSecretQueries {
		unlocked, locked, err := searchItems(attrs)
		if err != nil {
			return nil, err
		}
		if len(unlocked) > 0 {
			return unlocked[0].Secret.Value, nil
		}
		foundLocked = foundLocked || len(locked) > 0
	}

	if foundLocked {
		return nil, ErrKeyringLocked
	}
	return nil, ErrSecretNotFound
}
