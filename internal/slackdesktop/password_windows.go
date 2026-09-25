//go:build windows

package slackdesktop

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/billgraziano/dpapi"
)

// keyringPasswords returns the AES-256 key from Local State (Windows). On
// Windows this key feeds AES-GCM (not PBKDF2).
//
// Local State is read from the profile directory itself, so there is only ever
// one candidate — no keychain-style ambiguity to resolve as on macOS.
func keyringPasswords() ([][]byte, error) {
	key, err := localStateKey()
	if err != nil {
		return nil, err
	}
	return [][]byte{key}, nil
}

func localStateKey() ([]byte, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	bs, err := os.ReadFile(filepath.Join(dir, "Local State"))
	if err != nil {
		return nil, ErrNoSecretService
	}
	var ls struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(bs, &ls); err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(ls.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, err
	}
	if len(key) < 5 || string(key[:5]) != "DPAPI" {
		return nil, fmt.Errorf("%w: unexpected key header", ErrDecryptFailed)
	}
	decrypted, err := dpapi.DecryptBytes(key[5:])
	if err != nil {
		return nil, ErrNoSecretService
	}
	return decrypted, nil
}
