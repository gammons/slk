//go:build linux

package slackdesktop

import (
	"errors"
	"reflect"
	"testing"
)

// fakeKWallet scripts one daemon and records its calls, so tests can assert
// what was asked as well as what came back.
type fakeKWallet struct {
	enabled    bool
	enabledErr error

	wallet    string
	walletErr error

	isOpen    bool
	isOpenErr error

	handle  int32
	openErr error

	hasEntry    bool
	hasEntryErr error

	password    string
	passwordErr error

	closeErr error

	calls        []string
	openArgs     []any
	hasEntryArgs []any
	readArgs     []any
	closeArgs    []any
}

// workingKWallet holds the key, so each test varies only one thing.
func workingKWallet() *fakeKWallet {
	return &fakeKWallet{
		enabled:  true,
		wallet:   "kdewallet",
		isOpen:   true,
		handle:   7,
		hasEntry: true,
		password: "slack-safe-storage-key",
	}
}

func (f *fakeKWallet) IsEnabled() (bool, error) {
	f.calls = append(f.calls, "IsEnabled")
	return f.enabled, f.enabledErr
}

func (f *fakeKWallet) NetworkWallet() (string, error) {
	f.calls = append(f.calls, "NetworkWallet")
	return f.wallet, f.walletErr
}

func (f *fakeKWallet) IsOpen(wallet string) (bool, error) {
	f.calls = append(f.calls, "IsOpen")
	return f.isOpen, f.isOpenErr
}

func (f *fakeKWallet) Open(wallet, appID string) (int32, error) {
	f.calls = append(f.calls, "Open")
	f.openArgs = []any{wallet, appID}
	return f.handle, f.openErr
}

func (f *fakeKWallet) HasEntry(handle int32, folder, key, appID string) (bool, error) {
	f.calls = append(f.calls, "HasEntry")
	f.hasEntryArgs = []any{handle, folder, key, appID}
	return f.hasEntry, f.hasEntryErr
}

func (f *fakeKWallet) ReadPassword(handle int32, folder, key, appID string) (string, error) {
	f.calls = append(f.calls, "ReadPassword")
	f.readArgs = []any{handle, folder, key, appID}
	return f.password, f.passwordErr
}

func (f *fakeKWallet) Close(handle int32, appID string) error {
	f.calls = append(f.calls, "Close")
	f.closeArgs = []any{handle, appID}
	return f.closeErr
}

func (f *fakeKWallet) called(method string) bool {
	for _, c := range f.calls {
		if c == method {
			return true
		}
	}
	return false
}

// Chromium wrote the entry, so the coordinates must match it. Drift here is
// silent: the read finds nothing and we are back to the original bug.
func TestReadKWalletPasswordUsesSlackFolderAndEntry(t *testing.T) {
	f := workingKWallet()

	got, err := readKWalletPassword(f)
	if err != nil {
		t.Fatalf("readKWalletPassword: %v", err)
	}
	if want := []byte("slack-safe-storage-key"); !reflect.DeepEqual(got, want) {
		t.Fatalf("password = %q, want %q", got, want)
	}

	if want := []any{"kdewallet", "slk"}; !reflect.DeepEqual(f.openArgs, want) {
		t.Errorf("Open args = %#v, want %#v", f.openArgs, want)
	}
	want := []any{int32(7), "Slack Keys", "Slack Safe Storage", "slk"}
	if !reflect.DeepEqual(f.hasEntryArgs, want) {
		t.Errorf("HasEntry args = %#v, want %#v", f.hasEntryArgs, want)
	}
	if !reflect.DeepEqual(f.readArgs, want) {
		t.Errorf("ReadPassword args = %#v, want %#v", f.readArgs, want)
	}
}

// Reusing Slack's app id would inherit its grant and misname who is asking.
func TestReadKWalletPasswordIdentifiesAsSlk(t *testing.T) {
	f := workingKWallet()

	if _, err := readKWalletPassword(f); err != nil {
		t.Fatalf("readKWalletPassword: %v", err)
	}
	for _, args := range [][]any{f.openArgs, f.hasEntryArgs, f.readArgs, f.closeArgs} {
		if appID := args[len(args)-1]; appID != "slk" {
			t.Errorf("app id = %v, want %q", appID, "slk")
		}
	}
}

// A closed wallet must be reported, not opened: open() raises a modal dialog
// that over a TUI reads as a hang.
func TestReadKWalletPasswordDoesNotPromptWhenWalletClosed(t *testing.T) {
	f := workingKWallet()
	f.isOpen = false

	_, err := readKWalletPassword(f)
	if !errors.Is(err, ErrKeyringLocked) {
		t.Fatalf("error = %v, want %v", err, ErrKeyringLocked)
	}
	if f.called("Open") {
		t.Errorf("Open was called on a closed wallet; calls = %v", f.calls)
	}
}

// The handle is daemon-side state; leaking one per failed read accumulates.
func TestReadKWalletPasswordAlwaysClosesHandle(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*fakeKWallet)
		wantErr error
	}{
		{"success", func(*fakeKWallet) {}, nil},
		{"entry missing", func(f *fakeKWallet) { f.hasEntry = false }, ErrSecretNotFound},
		{"hasEntry fails", func(f *fakeKWallet) { f.hasEntryErr = errors.New("boom") }, nil},
		{"readPassword fails", func(f *fakeKWallet) { f.passwordErr = errors.New("boom") }, nil},
		{"password empty", func(f *fakeKWallet) { f.password = "" }, ErrSecretNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := workingKWallet()
			tt.mutate(f)

			_, err := readKWalletPassword(f)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if !f.called("Close") {
				t.Errorf("handle was not closed; calls = %v", f.calls)
			}
			if want := []any{int32(7), "slk"}; !reflect.DeepEqual(f.closeArgs, want) {
				t.Errorf("Close args = %#v, want %#v", f.closeArgs, want)
			}
		})
	}
}

// The key is already in hand, so a failed close is not a failed read.
func TestReadKWalletPasswordIgnoresCloseFailure(t *testing.T) {
	f := workingKWallet()
	f.closeErr = errors.New("close failed")

	got, err := readKWalletPassword(f)
	if err != nil {
		t.Fatalf("readKWalletPassword: %v", err)
	}
	if want := []byte("slack-safe-storage-key"); !reflect.DeepEqual(got, want) {
		t.Fatalf("password = %q, want %q", got, want)
	}
}

func TestReadKWalletPasswordFailures(t *testing.T) {
	transport := errors.New("dbus: connection closed")

	tests := []struct {
		name string
		// mutate turns the working daemon into the one under test.
		mutate func(*fakeKWallet)
		want   error
		// notCalled must not be reached once the daemon has answered.
		notCalled string
	}{
		{
			name:      "kwallet disabled",
			mutate:    func(f *fakeKWallet) { f.enabled = false },
			want:      ErrSecretNotFound,
			notCalled: "NetworkWallet",
		},
		{
			name:      "no wallet configured",
			mutate:    func(f *fakeKWallet) { f.wallet = "" },
			want:      ErrSecretNotFound,
			notCalled: "IsOpen",
		},
		{
			// Denied access is a negative handle, not a D-Bus error.
			name:      "access denied",
			mutate:    func(f *fakeKWallet) { f.handle = -1 },
			want:      ErrKeyringLocked,
			notCalled: "HasEntry",
		},
		{
			name:      "entry missing",
			mutate:    func(f *fakeKWallet) { f.hasEntry = false },
			want:      ErrSecretNotFound,
			notCalled: "ReadPassword",
		},
		{
			// An empty key would reach the CBC path and fail much later.
			name:   "entry present but empty",
			mutate: func(f *fakeKWallet) { f.password = "" },
			want:   ErrSecretNotFound,
		},
		{
			name:      "isEnabled fails",
			mutate:    func(f *fakeKWallet) { f.enabledErr = transport },
			want:      transport,
			notCalled: "NetworkWallet",
		},
		{
			name:   "networkWallet fails",
			mutate: func(f *fakeKWallet) { f.walletErr = transport },
			want:   transport,
		},
		{
			name:      "isOpen fails",
			mutate:    func(f *fakeKWallet) { f.isOpenErr = transport },
			want:      transport,
			notCalled: "Open",
		},
		{
			name:      "open fails",
			mutate:    func(f *fakeKWallet) { f.openErr = transport },
			want:      transport,
			notCalled: "HasEntry",
		},
		{
			name:   "hasEntry fails",
			mutate: func(f *fakeKWallet) { f.hasEntryErr = transport },
			want:   transport,
		},
		{
			name:   "readPassword fails",
			mutate: func(f *fakeKWallet) { f.passwordErr = transport },
			want:   transport,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := workingKWallet()
			tt.mutate(f)

			_, err := readKWalletPassword(f)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if tt.notCalled != "" && f.called(tt.notCalled) {
				t.Errorf("%s was called; calls = %v", tt.notCalled, f.calls)
			}
		})
	}
}

// kwalletd6 answers on the kwalletd5 name too, so one daemon missing the entry
// says nothing about the next.
func TestFindKWalletPasswordFallsThroughToLaterDaemon(t *testing.T) {
	empty := workingKWallet()
	empty.hasEntry = false
	holder := workingKWallet()
	holder.password = "from-the-second-daemon"

	got, err := findKWalletPassword([]kwalletConn{empty, holder})
	if err != nil {
		t.Fatalf("findKWalletPassword: %v", err)
	}
	if want := []byte("from-the-second-daemon"); !reflect.DeepEqual(got, want) {
		t.Fatalf("password = %q, want %q", got, want)
	}
}

// Stopping at the first key avoids a second, pointless access prompt.
func TestFindKWalletPasswordStopsAtFirstHit(t *testing.T) {
	first := workingKWallet()
	second := workingKWallet()

	if _, err := findKWalletPassword([]kwalletConn{first, second}); err != nil {
		t.Fatalf("findKWalletPassword: %v", err)
	}
	if len(second.calls) != 0 {
		t.Errorf("second daemon was queried; calls = %v", second.calls)
	}
}

func TestFindKWalletPasswordErrors(t *testing.T) {
	transport := errors.New("dbus: connection closed")

	missing := func() *fakeKWallet { f := workingKWallet(); f.hasEntry = false; return f }
	lockedWallet := func() *fakeKWallet { f := workingKWallet(); f.isOpen = false; return f }
	broken := func() *fakeKWallet { f := workingKWallet(); f.enabledErr = transport; return f }

	tests := []struct {
		name  string
		conns []kwalletConn
		want  error
	}{
		{
			name:  "no daemon running",
			conns: nil,
			want:  ErrSecretNotFound,
		},
		{
			name:  "no daemon has the entry",
			conns: []kwalletConn{missing(), missing()},
			want:  ErrSecretNotFound,
		},
		{
			// Locked is the one the user can act on.
			name:  "locked outranks missing",
			conns: []kwalletConn{missing(), lockedWallet()},
			want:  ErrKeyringLocked,
		},
		{
			name:  "locked outranks missing regardless of order",
			conns: []kwalletConn{lockedWallet(), missing()},
			want:  ErrKeyringLocked,
		},
		{
			// Flattening this to "not found" would point at Slack, not the bus.
			name:  "transport error outranks missing",
			conns: []kwalletConn{missing(), broken()},
			want:  transport,
		},
		{
			name:  "locked outranks a transport error",
			conns: []kwalletConn{broken(), lockedWallet()},
			want:  ErrKeyringLocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findKWalletPassword(tt.conns)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// Versioned names matter: contacting an unowned one would D-Bus-activate
// kwalletd and prompt a user who has the package but no wallet.
func TestKWalletDaemonsAreVersionedBusNames(t *testing.T) {
	want := []kwalletDaemon{
		{busName: "org.kde.kwalletd6", objPath: "/modules/kwalletd6"},
		{busName: "org.kde.kwalletd5", objPath: "/modules/kwalletd5"},
		{busName: "org.kde.kwalletd", objPath: "/modules/kwalletd"},
	}
	if !reflect.DeepEqual(kwalletDaemons, want) {
		t.Fatalf("kwalletDaemons = %#v, want %#v", kwalletDaemons, want)
	}
}
