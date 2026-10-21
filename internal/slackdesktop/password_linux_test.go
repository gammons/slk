//go:build linux

package slackdesktop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/godbus/dbus/v5"
)

// fakeSecretService scripts a Secret Service: what each query (keyed by its
// xdg:schema) matches, and what each item returns. It records the queries
// and reads so tests can assert what was asked as well as what came back.
type fakeSecretService struct {
	unlocked  map[string][]dbus.ObjectPath
	locked    map[string][]dbus.ObjectPath
	searchErr map[string]error

	secrets   map[dbus.ObjectPath][]byte
	secretErr map[dbus.ObjectPath]error

	queries []map[string]string
	reads   []dbus.ObjectPath
}

func (f *fakeSecretService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, error) {
	f.queries = append(f.queries, cloneStringMap(attrs))
	schema := attrs["xdg:schema"]
	if err := f.searchErr[schema]; err != nil {
		return nil, nil, err
	}
	return f.unlocked[schema], f.locked[schema], nil
}

func (f *fakeSecretService) GetSecret(item dbus.ObjectPath) ([]byte, error) {
	f.reads = append(f.reads, item)
	if err := f.secretErr[item]; err != nil {
		return nil, err
	}
	return f.secrets[item], nil
}

const (
	schemaV2 = "chrome_libsecret_os_crypt_password_v2"
	schemaV1 = "chrome_libsecret_os_crypt_password_v1"
	schemaQt = "org.qt.keychain"
)

// Covers the QtKeychain schema as served over the Secret Service, i.e. where
// kwalletd bridges onto org.freedesktop.secrets. Not coverage of KDE generally
// — see readKWalletPassword for the unbridged case.
func TestFindKeyringPasswordSupportsQtKeychainOverSecretService(t *testing.T) {
	wantPassword := []byte("kde-slack-safe-storage")
	svc := &fakeSecretService{
		unlocked: map[string][]dbus.ObjectPath{schemaQt: {"/item/qt"}},
		secrets:  map[dbus.ObjectPath][]byte{"/item/qt": wantPassword},
	}

	got, err := findKeyringPassword(svc)
	if err != nil {
		t.Fatalf("findKeyringPassword: %v", err)
	}
	if !reflect.DeepEqual(got, wantPassword) {
		t.Fatalf("password = %q, want %q", got, wantPassword)
	}

	wantQueries := []map[string]string{
		{
			"xdg:schema":  schemaV2,
			"application": "Slack",
		},
		{
			"xdg:schema":  schemaV1,
			"application": "Slack",
		},
		{
			"xdg:schema": schemaQt,
			"server":     "Slack Keys",
			"user":       "Slack Safe Storage",
		},
	}
	if !reflect.DeepEqual(svc.queries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", svc.queries, wantQueries)
	}
}

func TestFindKeyringPasswordPrefersUnlockedItemAcrossSchemas(t *testing.T) {
	wantPassword := []byte("unlocked-kde-secret")
	svc := &fakeSecretService{
		locked:   map[string][]dbus.ObjectPath{schemaV2: {"/item/locked"}},
		unlocked: map[string][]dbus.ObjectPath{schemaQt: {"/item/qt"}},
		secrets:  map[dbus.ObjectPath][]byte{"/item/qt": wantPassword},
	}

	got, err := findKeyringPassword(svc)
	if err != nil {
		t.Fatalf("findKeyringPassword: %v", err)
	}
	if !reflect.DeepEqual(got, wantPassword) {
		t.Fatalf("password = %q, want %q", got, wantPassword)
	}
}

// One unreadable item must not hide a key held by a later item or query.
func TestFindKeyringPasswordContinuesPastUnreadableItem(t *testing.T) {
	wantPassword := []byte("from-qt-keychain")
	svc := &fakeSecretService{
		unlocked: map[string][]dbus.ObjectPath{
			schemaV2: {"/item/broken"},
			schemaQt: {"/item/qt"},
		},
		secretErr: map[dbus.ObjectPath]error{"/item/broken": errors.New("GetSecret failed")},
		secrets:   map[dbus.ObjectPath][]byte{"/item/qt": wantPassword},
	}

	got, err := findKeyringPassword(svc)
	if err != nil {
		t.Fatalf("findKeyringPassword: %v", err)
	}
	if !reflect.DeepEqual(got, wantPassword) {
		t.Fatalf("password = %q, want %q", got, wantPassword)
	}
	if want := []dbus.ObjectPath{"/item/broken", "/item/qt"}; !reflect.DeepEqual(svc.reads, want) {
		t.Fatalf("reads = %v, want %v", svc.reads, want)
	}
}

// Within one query, a bad first match falls through to the next item.
func TestFindKeyringPasswordTriesEveryUnlockedMatch(t *testing.T) {
	wantPassword := []byte("second-item")
	svc := &fakeSecretService{
		unlocked: map[string][]dbus.ObjectPath{schemaV2: {"/item/empty", "/item/good"}},
		secrets: map[dbus.ObjectPath][]byte{
			"/item/empty": {},
			"/item/good":  wantPassword,
		},
	}

	got, err := findKeyringPassword(svc)
	if err != nil {
		t.Fatalf("findKeyringPassword: %v", err)
	}
	if !reflect.DeepEqual(got, wantPassword) {
		t.Fatalf("password = %q, want %q", got, wantPassword)
	}
	if want := []dbus.ObjectPath{"/item/empty", "/item/good"}; !reflect.DeepEqual(svc.reads, want) {
		t.Fatalf("reads = %v, want %v", svc.reads, want)
	}
}

func TestFindKeyringPasswordErrors(t *testing.T) {
	searchErr := errors.New("search failed")
	readErr := errors.New("GetSecret failed")

	tests := []struct {
		name string
		svc  *fakeSecretService
		want error
	}{
		{
			name: "no matching item",
			svc:  &fakeSecretService{},
			want: ErrSecretNotFound,
		},
		{
			name: "matching item is locked",
			svc: &fakeSecretService{
				locked: map[string][]dbus.ObjectPath{schemaQt: {"/item/locked"}},
			},
			want: ErrKeyringLocked,
		},
		{
			// Same rule as readKWalletPassword: an empty key would only fail
			// later, at decrypt time.
			name: "only match holds an empty secret",
			svc: &fakeSecretService{
				unlocked: map[string][]dbus.ObjectPath{schemaV2: {"/item/empty"}},
				secrets:  map[dbus.ObjectPath][]byte{"/item/empty": nil},
			},
			want: ErrSecretNotFound,
		},
		{
			name: "every search fails",
			svc: &fakeSecretService{
				searchErr: map[string]error{schemaV2: searchErr, schemaV1: searchErr, schemaQt: searchErr},
			},
			want: searchErr,
		},
		{
			name: "only match can't be read",
			svc: &fakeSecretService{
				unlocked:  map[string][]dbus.ObjectPath{schemaV1: {"/item/broken"}},
				secretErr: map[dbus.ObjectPath]error{"/item/broken": readErr},
			},
			want: readErr,
		},
		{
			// Matches collectKeyringPasswords: a store the user can unlock
			// is the more actionable report.
			name: "locked beats an unexpected error",
			svc: &fakeSecretService{
				locked:    map[string][]dbus.ObjectPath{schemaV2: {"/item/locked"}},
				searchErr: map[string]error{schemaQt: searchErr},
			},
			want: ErrKeyringLocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := findKeyringPassword(tt.svc)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// GetSecret's reply body is one (oayays) struct; godbus must fill dbusSecret
// from it field by field. Needs no bus.
func TestDBusSecretDecodesSpecStruct(t *testing.T) {
	body := []any{[]any{
		dbus.ObjectPath("/org/freedesktop/secrets/session/1"),
		[]byte{},
		[]byte("slack-safe-storage-key"),
		"text/plain",
	}}

	var secret dbusSecret
	if err := dbus.Store(body, &secret); err != nil {
		t.Fatalf("dbus.Store: %v", err)
	}
	if secret.Session != "/org/freedesktop/secrets/session/1" ||
		len(secret.Parameters) != 0 ||
		string(secret.Value) != "slack-safe-storage-key" ||
		secret.ContentType != "text/plain" {
		t.Fatalf("secret = %#v", secret)
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

func TestSecretServiceAbsent(t *testing.T) {
	busErr := func(name string) error {
		return dbus.Error{Name: name, Body: []any{"detail"}}
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"no owner and nothing to activate", busErr("org.freedesktop.DBus.Error.ServiceUnknown"), true},
		{"name has no owner", busErr("org.freedesktop.DBus.Error.NameHasNoOwner"), true},
		{"activation failed", busErr("org.freedesktop.DBus.Error.Spawn.ExecFailed"), true},
		{"wrapped", fmt.Errorf("open: %w", busErr("org.freedesktop.DBus.Error.ServiceUnknown")), true},
		{"provider refuses plain", busErr("org.freedesktop.DBus.Error.NotSupported"), false},
		{"sandbox denies the call", busErr("org.freedesktop.DBus.Error.AccessDenied"), false},
		{"provider never answered", busErr("org.freedesktop.DBus.Error.NoReply"), false},
		{"our timeout", context.DeadlineExceeded, false},
		{"not a bus error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secretServiceAbsent(tt.err); got != tt.want {
				t.Fatalf("secretServiceAbsent(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// specOnlySecretService is a Secret Service that offers nothing beyond the
// spec methods slk needs, served on a private dbus-daemon. A client that
// reads a property, or calls anything else, gets an error from godbus's
// default handler, so a lookup that succeeds against it stays inside the
// spec: the property read oo7-daemon rejected would fail here too.
type specOnlySecretService struct {
	session dbus.ObjectPath
	item    dbus.ObjectPath
	secret  []byte
	openErr *dbus.Error

	// godbus runs each method on its own goroutine.
	mu    sync.Mutex
	calls []string
}

func (s *specOnlySecretService) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *specOnlySecretService) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *specOnlySecretService) OpenSession(algorithm string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	s.record("OpenSession " + algorithm)
	if s.openErr != nil {
		return dbus.Variant{}, "/", s.openErr
	}
	return dbus.MakeVariant(""), s.session, nil
}

// Only the v2 Chromium schema holds the item; the other queries answer empty.
func (s *specOnlySecretService) SearchItems(attrs map[string]string) ([]dbus.ObjectPath, []dbus.ObjectPath, *dbus.Error) {
	s.record("SearchItems " + attrs["xdg:schema"])
	if attrs["xdg:schema"] == schemaV2 && attrs["application"] == "Slack" {
		return []dbus.ObjectPath{s.item}, nil, nil
	}
	return nil, nil, nil
}

type specOnlyItem struct{ svc *specOnlySecretService }

func (i *specOnlyItem) GetSecret(session dbus.ObjectPath) (dbusSecret, *dbus.Error) {
	i.svc.record("Item.GetSecret " + string(session))
	return dbusSecret{Session: session, Value: i.svc.secret, ContentType: "text/plain"}, nil
}

type specOnlySession struct{ svc *specOnlySecretService }

func (s *specOnlySession) Close() *dbus.Error {
	s.svc.record("Session.Close")
	return nil
}

// startTestBus runs a private dbus-daemon for one test and returns its
// address; skips when the daemon isn't installed. The config has no service
// directories, so calling a name nobody owns fails instead of D-Bus-activating
// the machine's real keyring daemon onto this bus.
func startTestBus(t *testing.T) string {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not installed")
	}

	// unix:tmpdir: a short /tmp socket path, so t.TempDir()'s long names
	// can't hit the sun_path length limit.
	config := filepath.Join(t.TempDir(), "bus.conf")
	body := `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>unix:tmpdir=/tmp</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*"/>
    <allow receive_sender="*"/>
    <allow own="*"/>
  </policy>
</busconfig>
`
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(daemon, "--config-file="+config, "--nofork", "--nopidfile", "--print-address")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start dbus-daemon: %v", err)
	}
	// SIGTERM, not Kill: the daemon unlinks its socket only on a clean exit,
	// and a killed one leaves a /tmp/dbus-* file behind per test.
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	})

	// Printed once the socket is bound, so a connect after this succeeds.
	addr, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("dbus-daemon printed no address: %v", err)
	}
	return strings.TrimSpace(addr)
}

func connectTestBus(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatalf("connect to test bus: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// serveSecretService exports svc, its session and its item on their own
// connection and takes the org.freedesktop.secrets name.
func serveSecretService(t *testing.T, addr string, svc *specOnlySecretService) {
	t.Helper()
	conn := connectTestBus(t, addr)
	objects := []struct {
		v     any
		path  dbus.ObjectPath
		iface string
	}{
		{svc, secretServicePath, secretServiceIface},
		{&specOnlySession{svc: svc}, svc.session, secretSessionIface},
		{&specOnlyItem{svc: svc}, svc.item, secretItemIface},
	}
	for _, o := range objects {
		if err := conn.Export(o.v, o.path, o.iface); err != nil {
			t.Fatalf("export %s: %v", o.iface, err)
		}
	}
	reply, err := conn.RequestName(secretServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		t.Fatalf("request name: %v", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name reply = %v, want primary owner", reply)
	}
}

func specOnlyService() *specOnlySecretService {
	return &specOnlySecretService{
		session: "/org/freedesktop/secrets/session/1",
		item:    "/org/freedesktop/secrets/collection/login/7",
		secret:  []byte("slack-safe-storage-key"),
	}
}

// The regression this guards: the old client library read the non-spec
// Item.Type property, which oo7-daemon rejects. On a real bus, against a
// service that serves only the spec, the key must come back through exactly
// OpenSession, SearchItems, Item.GetSecret and Session.Close, with the session
// OpenSession returned carried into GetSecret and closed at the end.
func TestSecretServiceLookupUsesOnlySpecMethods(t *testing.T) {
	addr := startTestBus(t)
	svc := specOnlyService()
	serveSecretService(t, addr, svc)

	got, err := readSecretServicePasswords(connectTestBus(t, addr))
	if err != nil {
		t.Fatalf("readSecretServicePasswords: %v", err)
	}
	if want := [][]byte{svc.secret}; !reflect.DeepEqual(got, want) {
		t.Fatalf("passwords = %q, want %q", got, want)
	}
	wantCalls := []string{
		"OpenSession plain",
		"SearchItems " + schemaV2,
		"Item.GetSecret " + string(svc.session),
		"Session.Close",
	}
	if calls := svc.recorded(); !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %q, want %q", calls, wantCalls)
	}
}

// Nobody owns org.freedesktop.secrets and nothing can activate it.
func TestSecretServiceLookupReportsNoProvider(t *testing.T) {
	addr := startTestBus(t)

	_, err := readSecretServicePasswords(connectTestBus(t, addr))
	if !errors.Is(err, ErrNoSecretService) {
		t.Fatalf("error = %v, want %v", err, ErrNoSecretService)
	}
}

// A provider that is there but won't open the session is not "no keyring
// found"; the bus error reaches the caller.
func TestSecretServiceLookupKeepsProviderRefusal(t *testing.T) {
	addr := startTestBus(t)
	svc := specOnlyService()
	svc.openErr = dbus.NewError("org.freedesktop.DBus.Error.NotSupported", []any{"plain is not supported"})
	serveSecretService(t, addr, svc)

	_, err := readSecretServicePasswords(connectTestBus(t, addr))
	if err == nil || errors.Is(err, ErrNoSecretService) {
		t.Fatalf("error = %v, want the provider's refusal", err)
	}
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) || dbusErr.Name != "org.freedesktop.DBus.Error.NotSupported" {
		t.Fatalf("error = %#v, want NotSupported from the bus", err)
	}
	if calls := svc.recorded(); !reflect.DeepEqual(calls, []string{"OpenSession plain"}) {
		t.Fatalf("calls = %q, want only the refused OpenSession", calls)
	}
}
