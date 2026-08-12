//go:build linux

package slackdesktop

import (
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

// keyringPassword fetches the "Slack Safe Storage" password from the
// Secret Service. Slack stores it using Chromium's schema with libsecret
// implementations and QtKeychain's schema with KDE Wallet.
func keyringPassword() ([]byte, error) {
	service, err := gosecret.NewService()
	if err != nil {
		return nil, ErrNoSecretService
	}
	defer service.Close()

	return findKeyringPassword(service.SearchItems)
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
	return nil, ErrNoSecretService
}
