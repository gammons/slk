//go:build linux

package slackdesktop

import (
	"context"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"
)

// Chromium's KWallet backend names the folder and entry after the product, so
// Slack's key is "Slack Keys" / "Slack Safe Storage" (Brave's is "Brave Keys",
// and so on). Same strings slackSecretQueries uses for the QtKeychain schema.
const (
	kwalletFolder = "Slack Keys"
	kwalletEntry  = "Slack Safe Storage"

	// Shown in KWallet's access prompt and recorded in its allow list. Passing
	// Slack's own id would inherit Slack's grant and misname who is asking.
	kwalletAppID = "slk"

	// kwalletd can block on a prompt; don't hang the terminal on it.
	kwalletCallTimeout = 10 * time.Second
)

// kwalletd6 also owns the kwalletd5 name, so the same process can answer twice.
type kwalletDaemon struct {
	busName string
	objPath dbus.ObjectPath
}

var kwalletDaemons = []kwalletDaemon{
	{busName: "org.kde.kwalletd6", objPath: "/modules/kwalletd6"},
	{busName: "org.kde.kwalletd5", objPath: "/modules/kwalletd5"},
	{busName: "org.kde.kwalletd", objPath: "/modules/kwalletd"},
}

// kwalletConn is the part of org.kde.KWallet we need, as an interface so the
// read sequence is testable without a session bus.
type kwalletConn interface {
	IsEnabled() (bool, error)
	NetworkWallet() (string, error)
	IsOpen(wallet string) (bool, error)
	Open(wallet, appID string) (int32, error)
	HasEntry(handle int32, folder, key, appID string) (bool, error)
	ReadPassword(handle int32, folder, key, appID string) (string, error)
	Close(handle int32, appID string) error
}

// kwalletPasswords reads Slack's Safe Storage key out of KWallet.
func kwalletPasswords() ([][]byte, error) {
	// Shared connection owned by the library; not ours to close.
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, ErrNoSecretService
	}

	conns, err := runningKWallets(conn)
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return nil, ErrNoSecretService
	}

	pw, err := findKWalletPassword(conns)
	if err != nil {
		return nil, err
	}
	return [][]byte{pw}, nil
}

// runningKWallets returns a connection per daemon that already owns its bus
// name. Calling an unowned name would D-Bus-activate kwalletd and prompt a user
// who has the package but no wallet; a daemon that isn't running also can't
// hold the key.
func runningKWallets(conn *dbus.Conn) ([]kwalletConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), kwalletCallTimeout)
	defer cancel()
	call := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0)
	if call.Err != nil {
		return nil, call.Err
	}
	var owned []string
	if err := call.Store(&owned); err != nil {
		return nil, err
	}

	running := make(map[string]bool, len(owned))
	for _, name := range owned {
		running[name] = true
	}

	var conns []kwalletConn
	for _, d := range kwalletDaemons {
		if running[d.busName] {
			conns = append(conns, dbusKWallet{obj: conn.Object(d.busName, d.objPath)})
		}
	}
	return conns, nil
}

// findKWalletPassword returns the first key any daemon can supply. One daemon
// lacking the entry says nothing about the next, so only report a failure once
// all have been asked, most actionable reason first.
func findKWalletPassword(conns []kwalletConn) ([]byte, error) {
	var (
		foundLocked bool
		lastErr     error
	)
	for _, c := range conns {
		pw, err := readKWalletPassword(c)
		switch {
		case err == nil:
			return pw, nil
		case errors.Is(err, ErrKeyringLocked):
			foundLocked = true
		case errors.Is(err, ErrSecretNotFound):
			// Try the next daemon.
		default:
			lastErr = err
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

func readKWalletPassword(c kwalletConn) ([]byte, error) {
	enabled, err := c.IsEnabled()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrSecretNotFound
	}

	// networkWallet is what Chromium asks for, so it's where the key went.
	wallet, err := c.NetworkWallet()
	if err != nil {
		return nil, err
	}
	if wallet == "" {
		return nil, ErrSecretNotFound
	}

	// open() on a closed wallet raises a modal unlock dialog, which over a
	// full-screen TUI reads as a hang. Plasma's PAM module unlocks at login, so
	// a closed wallet is a real "go unlock it".
	open, err := c.IsOpen(wallet)
	if err != nil {
		return nil, err
	}
	if !open {
		return nil, ErrKeyringLocked
	}

	handle, err := c.Open(wallet, kwalletAppID)
	if err != nil {
		return nil, err
	}
	// Denied access comes back as a negative handle, not a D-Bus error.
	if handle < 0 {
		return nil, ErrKeyringLocked
	}
	defer func() { _ = c.Close(handle, kwalletAppID) }()

	// readPassword returns "" for a missing entry too, hence the explicit check.
	has, err := c.HasEntry(handle, kwalletFolder, kwalletEntry, kwalletAppID)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrSecretNotFound
	}

	pw, err := c.ReadPassword(handle, kwalletFolder, kwalletEntry, kwalletAppID)
	if err != nil {
		return nil, err
	}
	// An empty key would reach the CBC path and yield garbage that only surfaces
	// later as invalid_auth.
	if pw == "" {
		return nil, ErrSecretNotFound
	}
	return []byte(pw), nil
}

type dbusKWallet struct {
	obj dbus.BusObject
}

func (d dbusKWallet) IsEnabled() (bool, error) {
	var out bool
	return out, d.call("isEnabled", &out)
}

func (d dbusKWallet) NetworkWallet() (string, error) {
	var out string
	return out, d.call("networkWallet", &out)
}

func (d dbusKWallet) IsOpen(wallet string) (bool, error) {
	var out bool
	return out, d.call("isOpen", &out, wallet)
}

// wId 0: no window to parent a prompt to. Matches Chromium.
func (d dbusKWallet) Open(wallet, appID string) (int32, error) {
	var out int32
	return out, d.call("open", &out, wallet, int64(0), appID)
}

func (d dbusKWallet) HasEntry(handle int32, folder, key, appID string) (bool, error) {
	var out bool
	return out, d.call("hasEntry", &out, handle, folder, key, appID)
}

func (d dbusKWallet) ReadPassword(handle int32, folder, key, appID string) (string, error) {
	var out string
	return out, d.call("readPassword", &out, handle, folder, key, appID)
}

// Close releases the handle without forcing other apps off the wallet.
func (d dbusKWallet) Close(handle int32, appID string) error {
	var out int32
	return d.call("close", &out, handle, false, appID)
}

func (d dbusKWallet) call(method string, out any, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), kwalletCallTimeout)
	defer cancel()

	call := d.obj.CallWithContext(ctx, "org.kde.KWallet."+method, 0, args...)
	if call.Err != nil {
		return call.Err
	}
	return call.Store(out)
}
