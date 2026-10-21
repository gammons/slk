//go:build linux

package slackdesktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName  = "org.freedesktop.secrets"
	secretServicePath  = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceIface = "org.freedesktop.Secret.Service"
	secretSessionIface = "org.freedesktop.Secret.Session"
	secretItemIface    = "org.freedesktop.Secret.Item"

	// A Secret Service can block on an unlock prompt; don't hang the
	// terminal on it. Applied per call, like kwalletCallTimeout.
	secretCallTimeout = 10 * time.Second
)

// secretServiceConn is the part of org.freedesktop.Secret we need, as an
// interface so the lookup is testable without a session bus. Mirrors
// kwalletConn.
type secretServiceConn interface {
	SearchItems(attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error)
	GetSecret(item dbus.ObjectPath) ([]byte, error)
}

// dbusSecret is the Secret Service "Secret" struct, signature (oayays).
type dbusSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

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
//
// This talks to the Secret Service over D-Bus directly and only calls methods
// in the freedesktop spec (OpenSession, SearchItems, Item.GetSecret). Client
// libraries that eagerly load every item property also read the
// gnome-keyring-only Item.Type property, which spec-only providers such as
// oo7-daemon reject with "Unknown property 'Type'".
func secretServicePasswords() ([][]byte, error) {
	// Shared connection owned by the library; not ours to close. No bus means
	// no keyring to reach, same as kwalletPasswords.
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, ErrNoSecretService
	}
	return readSecretServicePasswords(conn)
}

// readSecretServicePasswords is secretServicePasswords on a given connection,
// so a test can point it at its own bus.
func readSecretServicePasswords(conn *dbus.Conn) ([][]byte, error) {
	service, err := openSecretService(conn)
	switch {
	case err == nil:
	case secretServiceAbsent(err):
		return nil, ErrNoSecretService
	default:
		// A daemon that is there but won't answer (hung, refusing "plain",
		// denied by a sandbox) is not "no keyring found": keep the detail so
		// the user is sent to the right place.
		return nil, fmt.Errorf("secret service: %w", err)
	}
	defer service.Close()

	pw, err := findKeyringPassword(service)
	if err != nil {
		return nil, err
	}
	return [][]byte{pw}, nil
}

// secretServiceAbsent reports whether err from a call to org.freedesktop.secrets
// says nobody provides it: no owner and nothing to activate, or an activation
// file whose daemon failed to start. The bus's own error name is the signal;
// the text varies between bus implementations.
func secretServiceAbsent(err error) bool {
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) {
		return false
	}
	switch dbusErr.Name {
	case "org.freedesktop.DBus.Error.ServiceUnknown",
		"org.freedesktop.DBus.Error.NameHasNoOwner":
		return true
	}
	return strings.HasPrefix(dbusErr.Name, "org.freedesktop.DBus.Error.Spawn.")
}

// findKeyringPassword runs each query in order and returns the first
// non-empty secret. A matching item that can't be read, or holds an empty
// value, doesn't stop the search: a later item or query may still have the
// key. Locked matches are only counted, since unlocking would raise a prompt.
//
// When nothing was found the precedence matches collectKeyringPasswords: a
// locked store the user can unlock, then an unexpected error, then not found.
func findKeyringPassword(c secretServiceConn) ([]byte, error) {
	var (
		foundLocked bool
		lastErr     error
	)
	for _, attrs := range slackSecretQueries {
		unlocked, locked, err := c.SearchItems(attrs)
		if err != nil {
			lastErr = err
			continue
		}
		foundLocked = foundLocked || len(locked) > 0

		for _, item := range unlocked {
			pw, err := c.GetSecret(item)
			switch {
			case err != nil:
				lastErr = err
			case len(pw) == 0:
				// An empty key would only fail later, at decrypt time, as
				// invalid_auth. Same rule as readKWalletPassword.
			default:
				return pw, nil
			}
		}
	}

	switch {
	case foundLocked:
		return nil, ErrKeyringLocked
	case lastErr != nil:
		return nil, lastErr
	default:
		return nil, ErrSecretNotFound
	}
}

// dbusSecretService implements secretServiceConn over a session opened with
// the "plain" algorithm. The spec only says plain is "almost always
// supported"; a provider that refuses it answers OpenSession with
// org.freedesktop.DBus.Error.NotSupported, which reaches the user as is.
type dbusSecretService struct {
	conn    *dbus.Conn
	service dbus.BusObject
	session dbus.ObjectPath
}

func openSecretService(conn *dbus.Conn) (*dbusSecretService, error) {
	s := &dbusSecretService{
		conn:    conn,
		service: conn.Object(secretServiceName, secretServicePath),
	}
	var algorithmOutput dbus.Variant
	err := s.call(s.service, secretServiceIface+".OpenSession",
		[]any{&algorithmOutput, &s.session}, "plain", dbus.MakeVariant(""))
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *dbusSecretService) SearchItems(attrs map[string]string) (unlocked, locked []dbus.ObjectPath, err error) {
	err = s.call(s.service, secretServiceIface+".SearchItems", []any{&unlocked, &locked}, attrs)
	return unlocked, locked, err
}

func (s *dbusSecretService) GetSecret(item dbus.ObjectPath) ([]byte, error) {
	var secret dbusSecret
	obj := s.conn.Object(secretServiceName, item)
	if err := s.call(obj, secretItemIface+".GetSecret", []any{&secret}, s.session); err != nil {
		return nil, err
	}
	return secret.Value, nil
}

// Close ends the session. It gets its own timeout, so a slow lookup that used
// up its budget can't leave the session open on the shared connection.
//
// An OpenSession or Close that itself times out does leave the session to the
// daemon, which drops it when this connection goes away (spec, "Sessions").
// Neither call prompts, so that only happens with a wedged daemon, where one
// stray session is the least of it; not worth a private connection here.
func (s *dbusSecretService) Close() {
	_ = s.call(s.conn.Object(secretServiceName, s.session), secretSessionIface+".Close", nil)
}

func (s *dbusSecretService) call(obj dbus.BusObject, method string, out []any, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), secretCallTimeout)
	defer cancel()

	call := obj.CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return call.Err
	}
	if len(out) == 0 {
		return nil
	}
	return call.Store(out...)
}
