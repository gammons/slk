package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gammons/slk/internal/cli"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/slackhttp"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/statusbar"
)

// testLogin is a token of workspace T1 with the given token and cookie.
func testLogin(token, cookie string) slackclient.Token {
	return slackclient.Token{TeamID: "T1", TeamName: "Acme", AccessToken: token, Cookie: cookie}
}

// fakeLogins is the token store, the desktop app and Slack's answers
// behind a loginRefresher, with a record of what was asked.
type fakeLogins struct {
	stored  slackclient.Token
	loadErr error
	// desktop is what the desktop app has; empty means nothing newer, so
	// remint gives the refused login back, as remintToken does.
	desktop slackclient.Token
	// verdict is what checking a login answers, by token; a token that is
	// not in it passes. check, when set, replaces verdict.
	verdict map[string]error
	check   func(context.Context, slackclient.Token) error

	loads       int
	remintedFor []string
	checked     []string
	saved       []string
}

func (f *fakeLogins) refresher() *loginRefresher {
	return newLoginRefresherWith(
		func(string) (slackclient.Token, error) {
			f.loads++
			return f.stored, f.loadErr
		},
		func(_ context.Context, tok slackclient.Token) slackclient.Token {
			f.remintedFor = append(f.remintedFor, tok.AccessToken)
			if f.desktop.AccessToken == "" {
				return tok
			}
			return f.desktop
		},
		func(tok, expected slackclient.Token) (bool, error) {
			if f.stored != expected {
				return false, nil
			}
			f.stored = tok
			f.saved = append(f.saved, tok.AccessToken)
			return true, nil
		},
		func(ctx context.Context, tok slackclient.Token) error {
			f.checked = append(f.checked, tok.AccessToken)
			if f.check != nil {
				return f.check(ctx, tok)
			}
			return f.verdict[tok.AccessToken]
		},
	)
}

func TestLoginRefresher_TakesALoginAnotherProcessSaved(t *testing.T) {
	f := &fakeLogins{stored: testLogin("xoxc-saved", "d-saved")}
	got, err := f.refresher().refresh(context.Background(), testLogin("xoxc-old", "d-old"))
	if err != nil || keyOf(got) != keyOf(f.stored) {
		t.Errorf("refresh = %s, %v; want the stored login", got.AccessToken, err)
	}
	if !slices.Equal(f.checked, []string{"xoxc-saved"}) {
		t.Errorf("checked %q, want the stored login before it is returned", f.checked)
	}
	if len(f.remintedFor) != 0 {
		t.Errorf("the desktop app was asked about %q; a login in the store needs no keychain read", f.remintedFor)
	}
}

func TestLoginRefresher_AsksTheDesktopAppWhenTheStoreHasNothingNewer(t *testing.T) {
	refused := testLogin("xoxc-old", "d-old")
	for _, f := range []*fakeLogins{
		{stored: refused, desktop: testLogin("xoxc-desk", "d-desk")},
		{loadErr: errors.New("permission denied"), desktop: testLogin("xoxc-desk", "d-desk")},
	} {
		got, err := f.refresher().refresh(context.Background(), refused)
		if err != nil || keyOf(got) != keyOf(f.desktop) {
			t.Errorf("store %v: refresh = %s, %v; want the desktop app's login", f.loadErr, got.AccessToken, err)
		}
		if !slices.Equal(f.remintedFor, []string{"xoxc-old"}) {
			t.Errorf("store %v: the desktop app was asked about %q, want the refused login once", f.loadErr, f.remintedFor)
		}
	}
}

// TestLoginRefresher_ARefusedStoredLoginFallsBackToTheDesktopApp: a stored
// login that Slack refuses too must not end the search, or a CLI command,
// which retries once, never reaches the desktop app's working login.
func TestLoginRefresher_ARefusedStoredLoginFallsBackToTheDesktopApp(t *testing.T) {
	f := &fakeLogins{
		stored:  testLogin("xoxc-saved", "d-saved"),
		desktop: testLogin("xoxc-desk", "d-desk"),
		verdict: map[string]error{"xoxc-saved": errInvalidAuth},
	}
	got, err := f.refresher().refresh(context.Background(), testLogin("xoxc-old", "d-old"))
	if err != nil || got.AccessToken != "xoxc-desk" {
		t.Errorf("refresh = %s, %v; want the desktop app's login", got.AccessToken, err)
	}
	if !slices.Equal(f.checked, []string{"xoxc-saved", "xoxc-desk"}) {
		t.Errorf("checked %q, want the stored login, then the desktop app's", f.checked)
	}
}

func TestLoginRefresher_ProtectedStoredLoginNeverFallsBackToDesktop(t *testing.T) {
	for _, source := range []string{slackclient.TokenSourceImported, slackclient.TokenSourceBrowser, "future-source"} {
		for _, state := range []string{"same-pair", "already-refused", "refused-on-check"} {
			t.Run(source+"/"+state, func(t *testing.T) {
				failed := testLogin("xoxc-old", "d-old")
				stored := testLogin("xoxc-saved", "d-saved")
				if state == "same-pair" {
					stored = failed
				}
				stored.Source = source
				f := &fakeLogins{
					stored:  stored,
					desktop: testLogin("xoxc-desk", "d-desk"),
					verdict: map[string]error{stored.AccessToken: errInvalidAuth},
				}
				r := f.refresher()
				if state == "already-refused" {
					r.refused[keyOf(stored)] = true
				}
				for range 2 {
					got, err := r.refresh(context.Background(), failed)
					if !errors.Is(err, errNoNewerLogin) || got != failed {
						t.Fatalf("refresh = %s, %v; want the failed login and errNoNewerLogin", got.AccessToken, err)
					}
				}
				var wantChecked []string
				if state == "refused-on-check" {
					wantChecked = []string{stored.AccessToken}
				}
				if !slices.Equal(f.checked, wantChecked) {
					t.Errorf("checked %q, want %q", f.checked, wantChecked)
				}
				if len(f.remintedFor) != 0 || len(f.saved) != 0 || f.stored != stored {
					t.Error("desktop fallback read or replaced a protected stored login")
				}
				f.stored = testLogin("xoxc-reimported", "d-reimported")
				f.stored.Source = source
				got, err := r.refresh(context.Background(), failed)
				if err != nil || got != f.stored || f.loads != 3 {
					t.Errorf("refresh = %s, %v, loads = %d; want the newer protected login", got.AccessToken, err, f.loads)
				}
				if !slices.Equal(f.checked, append(wantChecked, "xoxc-reimported")) || len(f.remintedFor) != 0 || len(f.saved) != 0 {
					t.Error("the newer protected login was not checked without desktop fallback")
				}
			})
		}
	}
}

// TestConnectWithRemint_ARefusedStoredLoginFallsBackToTheDesktopApp is the
// same through the CLI's sign-in: A is refused, the store has B, Slack
// refuses B, the desktop app has C, and the command signs in with C.
func TestConnectWithRemint_ARefusedStoredLoginFallsBackToTheDesktopApp(t *testing.T) {
	f := &fakeLogins{
		stored:  testLogin("xoxc-saved", "d-saved"),
		desktop: testLogin("xoxc-desk", "d-desk"),
		verdict: map[string]error{"xoxc-saved": errInvalidAuth},
	}
	signIn := &fakeSignIn{errs: []error{errInvalidAuth, nil}}
	client, err := connectWithRemint(context.Background(), testLogin("xoxc-old", "d-old"), signIn.connect, f.refresher().refresh)
	if err != nil || client == nil {
		t.Fatalf("client %v, err %v", client, err)
	}
	if !slices.Equal(signIn.tokens, []string{"xoxc-old", "xoxc-desk"}) {
		t.Errorf("signed in with %q, want the stored login, then the desktop app's", signIn.tokens)
	}
}

// TestLoginRefresher_AsksTheDesktopAppOncePerRefusedLogin: reading the
// desktop app can show a keychain prompt, so each refused login is asked
// about once, even when refusals alternate (A, B, A). The store is still
// read every time, so a login that another process saves later is found.
func TestLoginRefresher_AsksTheDesktopAppOncePerRefusedLogin(t *testing.T) {
	a, b := testLogin("xoxc-a", "d-a"), testLogin("xoxc-b", "d-b")
	f := &fakeLogins{}
	r := f.refresher()
	for _, refused := range []slackclient.Token{a, b, a} {
		f.stored = refused // the store holds the refused login itself
		if got, err := r.refresh(context.Background(), refused); !errors.Is(err, errNoNewerLogin) || keyOf(got) != keyOf(refused) {
			t.Fatalf("refresh(%s) = %s, %v; want it back and errNoNewerLogin", refused.AccessToken, got.AccessToken, err)
		}
	}
	if !slices.Equal(f.remintedFor, []string{"xoxc-a", "xoxc-b"}) {
		t.Errorf("the desktop app was asked about %q, want each refused login once", f.remintedFor)
	}
	if f.loads != 3 {
		t.Errorf("the store was read %d times, want 3", f.loads)
	}
}

// TestLoginRefresher_NeverChoosesARefusedLoginAgain: A is refused and the
// store gives B; B is refused, and a stale copy of A is back in the store.
// A must not be chosen (or even checked) again; the desktop app's C is.
func TestLoginRefresher_NeverChoosesARefusedLoginAgain(t *testing.T) {
	a := testLogin("xoxc-a", "d-a")
	f := &fakeLogins{stored: testLogin("xoxc-b", "d-b"), desktop: testLogin("xoxc-c", "d-c")}
	r := f.refresher()
	b, err := r.refresh(context.Background(), a)
	if err != nil || b.AccessToken != "xoxc-b" {
		t.Fatalf("first refresh = %s, %v; want the stored B", b.AccessToken, err)
	}

	f.stored = a
	got, err := r.refresh(context.Background(), b)
	if err != nil || got.AccessToken != "xoxc-c" {
		t.Errorf("second refresh = %s, %v; want the desktop app's C", got.AccessToken, err)
	}
	if !slices.Equal(f.checked, []string{"xoxc-b", "xoxc-c"}) {
		t.Errorf("checked %q; the refused A must not be checked again", f.checked)
	}
}

// TestLoginRefresher_AnErrorThatIsNotARefusalEndsTheSearch: with no network
// the next candidate cannot be checked either, and a login of another
// account means the workspace was added again as someone else, which the
// desktop app's login must not overwrite. Neither asks the desktop app.
func TestLoginRefresher_AnErrorThatIsNotARefusalEndsTheSearch(t *testing.T) {
	for _, stop := range []error{
		errors.New("dial tcp: no route to host"),
		fmt.Errorf("%w: user U2", errOtherAccount),
	} {
		f := &fakeLogins{
			stored:  testLogin("xoxc-saved", "d-saved"),
			desktop: testLogin("xoxc-desk", "d-desk"),
			verdict: map[string]error{"xoxc-saved": stop},
		}
		refused := testLogin("xoxc-old", "d-old")
		got, err := f.refresher().refresh(context.Background(), refused)
		if !errors.Is(err, stop) || keyOf(got) != keyOf(refused) {
			t.Errorf("%v: refresh = %s, %v; want the refused login back and that error", stop, got.AccessToken, err)
		}
		if len(f.remintedFor) != 0 {
			t.Errorf("%v: the desktop app was asked about %q, want no question", stop, f.remintedFor)
		}
	}
}

func TestSameAccountAs(t *testing.T) {
	check := sameAccountAs("T1", "U1", func(_ context.Context, tok slackclient.Token) (string, string, error) {
		switch tok.AccessToken {
		case "xoxc-same":
			return "T1", "U1", nil
		case "xoxc-other-user":
			return "T1", "U2", nil
		case "xoxc-other-team":
			return "T2", "U1", nil
		}
		return "", "", errInvalidAuth
	})
	ctx := context.Background()
	if err := check(ctx, testLogin("xoxc-same", "d")); err != nil {
		t.Errorf("the same account: %v, want nil", err)
	}
	for _, token := range []string{"xoxc-other-user", "xoxc-other-team"} {
		if err := check(ctx, testLogin(token, "d")); !errors.Is(err, errOtherAccount) {
			t.Errorf("%s: %v, want errOtherAccount", token, err)
		}
	}
	if err := check(ctx, testLogin("xoxc-refused", "d")); !slackclient.IsAuthError(err) {
		t.Errorf("a refused login: %v, want the refusal", err)
	}
}

// TestWorkspaceRelogin_AppliesANewLoginAndRemembersIt: after a relogin,
// the login in use is the new one, so the next refusal is about it.
func TestWorkspaceRelogin_AppliesANewLoginAndRemembersIt(t *testing.T) {
	saved := testLogin("xoxc-saved", "d-saved")
	f := &fakeLogins{stored: saved}
	var applied []string
	relogin := workspaceRelogin(testLogin("xoxc-old", "d-old"), f.refresher(),
		func(tok slackclient.Token) { applied = append(applied, tok.AccessToken) },
		func(text string) { t.Errorf("notified %q", text) })

	if !relogin(context.Background()) {
		t.Fatal("relogin = false, want true: the store holds a newer login")
	}

	// Slack refuses the saved login too, and the store has nothing newer.
	f.desktop = testLogin("xoxc-desk", "d-desk")
	if !relogin(context.Background()) {
		t.Fatal("second relogin = false, want true: the desktop app has a newer login")
	}
	if !slices.Equal(f.remintedFor, []string{"xoxc-saved"}) {
		t.Errorf("the desktop app was asked about %q, want the saved login, the one in use", f.remintedFor)
	}
	if !slices.Equal(applied, []string{"xoxc-saved", "xoxc-desk"}) {
		t.Errorf("applied %q, want the saved login, then the desktop app's", applied)
	}
}

func TestWorkspaceRelogin_NothingNewerChangesNothing(t *testing.T) {
	connected := testLogin("xoxc-old", "d-old")
	f := &fakeLogins{stored: connected}
	applied := 0
	relogin := workspaceRelogin(connected, f.refresher(),
		func(slackclient.Token) { applied++ },
		func(text string) { t.Errorf("notified %q", text) })
	if relogin(context.Background()) {
		t.Error("relogin = true, want false: there is no newer login")
	}
	if applied != 0 {
		t.Errorf("applied a login %d times, want 0", applied)
	}
}

// TestWorkspaceRelogin_NeverInstallsAnotherAccount: the store holds a
// login that Slack accepts, but as another user of the same team (the
// workspace was added again as someone else). The running TUI was built
// for its own user, so it keeps every holder as it is, tells the user to
// restart, and stops trying.
func TestWorkspaceRelogin_NeverInstallsAnotherAccount(t *testing.T) {
	f := &fakeLogins{
		stored:  testLogin("xoxc-other", "d-other"),
		desktop: testLogin("xoxc-desk", "d-desk"),
	}
	f.check = sameAccountAs("T1", "U1", func(_ context.Context, tok slackclient.Token) (string, string, error) {
		if tok.AccessToken == "xoxc-other" {
			return "T1", "U2", nil
		}
		return "T1", "U1", nil
	})
	applied := 0
	var notes []string
	relogin := workspaceRelogin(testLogin("xoxc-old", "d-old"), f.refresher(),
		func(slackclient.Token) { applied++ },
		func(text string) { notes = append(notes, text) })

	for range 2 {
		if relogin(context.Background()) {
			t.Fatal("relogin = true, want false: the only newer login is another account's")
		}
	}
	if applied != 0 {
		t.Errorf("applied a login %d times, want 0: no holder may change", applied)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "Restart slk") {
		t.Errorf("notified %q, want one note that asks for a restart", notes)
	}
	if f.loads != 1 || len(f.remintedFor) != 0 || len(f.saved) != 0 {
		t.Errorf("after the other account: %d store reads, desktop asked about %q, saved %q; want 1, none and none",
			f.loads, f.remintedFor, f.saved)
	}
}

// TestWorkspaceRelogin_AnotherAccountFromTheDesktopAppIsNotSaved: the TUI's
// own login is refused, the store has nothing newer, and the desktop app
// now holds another user's login for the workspace. The TUI must not
// install it, and must not save it either: the token store is what every
// other slk process signs in with, so saving it would switch them all to
// the other user as a side effect of this recovery.
func TestWorkspaceRelogin_AnotherAccountFromTheDesktopAppIsNotSaved(t *testing.T) {
	store := slackclient.NewTokenStore(t.TempDir())
	connected := testLogin("xoxc-u1", "d-u1")
	if err := store.Save(connected); err != nil {
		t.Fatal(err)
	}
	identify := func(_ context.Context, tok slackclient.Token) (string, string, error) {
		if tok.AccessToken == "xoxc-u2" {
			return "T1", "U2", nil
		}
		return "", "", errInvalidAuth
	}
	desktop := func(context.Context, slackclient.Token) slackclient.Token {
		return testLogin("xoxc-u2", "d-u2")
	}
	refresher := newLoginRefresherWith(store.Load, desktop, store.SaveIf, sameAccountAs("T1", "U1", identify))
	applied := 0
	var notes []string
	relogin := workspaceRelogin(connected, refresher,
		func(slackclient.Token) { applied++ },
		func(text string) { notes = append(notes, text) })

	if relogin(context.Background()) {
		t.Fatal("relogin = true, want false: the desktop app holds another user's login")
	}
	if applied != 0 {
		t.Errorf("applied a login %d times, want 0: no holder may change", applied)
	}
	if got, err := store.Load("T1"); err != nil || keyOf(got) != keyOf(connected) {
		t.Errorf("the token store holds %s (%v), want the login it had", got.AccessToken, err)
	}
	if len(notes) != 1 {
		t.Errorf("notified %q, want one notice", notes)
	}
}

// replacedDuringCheck is a token store in a temporary directory with A
// saved, a desktop app that holds B, and an identify that runs meanwhile
// while it checks B, as another slk process would. It answers B as user
// U1, and c with answerC. It returns the store and a refresher on them
// that keeps the TUI's account, U1.
func replacedDuringCheck(t *testing.T, meanwhile func(dir string), c slackclient.Token,
	answerC func(context.Context) (string, string, error)) (*slackclient.TokenStore, *loginRefresher) {
	t.Helper()
	dir := t.TempDir()
	store := slackclient.NewTokenStore(dir)
	if err := store.Save(testLogin("xoxc-a", "d-a")); err != nil {
		t.Fatal(err)
	}
	identify := func(ctx context.Context, tok slackclient.Token) (string, string, error) {
		switch tok.AccessToken {
		case "xoxc-b":
			meanwhile(dir)
			return "T1", "U1", nil
		case c.AccessToken:
			return answerC(ctx)
		}
		return "", "", errInvalidAuth
	}
	desktop := func(context.Context, slackclient.Token) slackclient.Token {
		return testLogin("xoxc-b", "d-b")
	}
	return store, newLoginRefresherWith(store.Load, desktop, store.SaveIf, sameAccountAs("T1", "U1", identify))
}

// saveMeanwhile is a meanwhile for replacedDuringCheck: another slk
// process saves tok, as slk --add-workspace does.
func saveMeanwhile(t *testing.T, tok slackclient.Token) func(dir string) {
	return func(dir string) {
		if err := slackclient.NewTokenStore(dir).Save(tok); err != nil {
			t.Fatal(err)
		}
	}
}

// answerAs is an answerC for replacedDuringCheck: Slack accepts the login
// as the given user of team T1.
func answerAs(user string) func(context.Context) (string, string, error) {
	return func(context.Context) (string, string, error) { return "T1", user, nil }
}

// TestWorkspaceRelogin_KeepsAWorkspaceAddedAgainDuringTheCheck: while the
// desktop app's login B, of the TUI's own user, is checked, the user adds
// the workspace again as another user from a browser (C). Recovery must
// not save B over C: C, its Source too, stays in the store, and the TUI,
// built for its own user, asks for a restart.
func TestWorkspaceRelogin_KeepsAWorkspaceAddedAgainDuringTheCheck(t *testing.T) {
	c := testLogin("xoxc-c", "d-c")
	c.Source = slackclient.TokenSourceBrowser
	store, refresher := replacedDuringCheck(t, saveMeanwhile(t, c), c, answerAs("U2"))
	applied := 0
	var notes []string
	relogin := workspaceRelogin(testLogin("xoxc-a", "d-a"), refresher,
		func(slackclient.Token) { applied++ },
		func(text string) { notes = append(notes, text) })

	if relogin(context.Background()) {
		t.Fatal("relogin = true, want false: the workspace now belongs to another user")
	}
	if got, err := store.Load("T1"); err != nil || got != c {
		t.Errorf("the token store holds %+v (%v), want the login added meanwhile, %+v", got, err, c)
	}
	if applied != 0 || len(notes) != 1 {
		t.Errorf("applied %d logins and notified %q; want none and one notice", applied, notes)
	}
}

// TestLoginRefresher_TakesALoginAddedAgainDuringTheCheck: when the login
// added meanwhile belongs to the same user, it passes the check and is
// the answer, and it stays in the store.
func TestLoginRefresher_TakesALoginAddedAgainDuringTheCheck(t *testing.T) {
	c := testLogin("xoxc-c", "d-c")
	c.Source = slackclient.TokenSourceBrowser
	store, refresher := replacedDuringCheck(t, saveMeanwhile(t, c), c, answerAs("U1"))

	got, err := refresher.refresh(context.Background(), testLogin("xoxc-a", "d-a"))
	if err != nil || got != c {
		t.Errorf("refresh = %+v, %v; want the login added meanwhile", got, err)
	}
	if stored, err := store.Load("T1"); err != nil || stored != c {
		t.Errorf("the token store holds %+v (%v), want %+v", stored, err, c)
	}
}

// TestLoginRefresher_KeepsTheCheckedLoginWhenTheReplacementCannotBeChecked:
// C replaces A while the desktop app's login B passes its check, and then
// Slack cannot be asked about C (no network). B passed, and nothing else
// keeps it: it is not saved over C, and the desktop app is not asked again
// about A. So refresh returns B, and C stays in the store, to be checked
// when Slack next refuses B.
func TestLoginRefresher_KeepsTheCheckedLoginWhenTheReplacementCannotBeChecked(t *testing.T) {
	c := testLogin("xoxc-c", "d-c")
	offline := errors.New("dial tcp: no route to host")
	store, refresher := replacedDuringCheck(t, saveMeanwhile(t, c), c,
		func(context.Context) (string, string, error) { return "", "", offline })

	got, err := refresher.refresh(context.Background(), testLogin("xoxc-a", "d-a"))
	if err != nil || got.AccessToken != "xoxc-b" {
		t.Errorf("refresh = %s, %v; want the desktop app's login, which passed", got.AccessToken, err)
	}
	if stored, err := store.Load("T1"); err != nil || stored != c {
		t.Errorf("the token store holds %+v (%v), want the replacement, %+v", stored, err, c)
	}
}

func TestLoginRefresher_ReconsiderRespectsReplacementSource(t *testing.T) {
	offline := errors.New("dial tcp: no route to host")
	for _, source := range []string{"", slackclient.TokenSourceImported, slackclient.TokenSourceBrowser, "future-source"} {
		for _, tc := range []struct {
			name     string
			verdict  error
			refused  bool
			samePair bool
		}{
			{name: "accepted"},
			{name: "refused", verdict: errInvalidAuth},
			{name: "unreachable", verdict: offline},
			{name: "already-refused", refused: true},
			{name: "same-refused-pair", samePair: true},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				failed := testLogin("xoxc-old", "d-old")
				stored := testLogin("xoxc-replaced", "d-replaced")
				if tc.samePair {
					stored = failed
				}
				stored.Source, stored.UserID = source, "UREPLACED"
				f := &fakeLogins{stored: failed, desktop: testLogin("xoxc-desk", "d-desk")}
				f.check = func(_ context.Context, tok slackclient.Token) error {
					if keyOf(tok) == keyOf(f.desktop) {
						f.stored = stored
						return nil
					}
					if tok != stored {
						t.Error("checked an unexpected login")
					}
					return tc.verdict
				}
				r := f.refresher()
				if tc.refused {
					r.refused[keyOf(stored)] = true
				}
				want, wantErr := f.desktop, error(nil)
				wantChecked := []string{f.desktop.AccessToken}
				if !tc.refused && !tc.samePair {
					wantChecked = append(wantChecked, stored.AccessToken)
				}
				if !tc.refused && !tc.samePair && tc.verdict == nil {
					want = stored
				} else if !stored.UsesDesktopLogin() {
					want, wantErr = failed, tc.verdict
					if wantErr == nil || slackclient.IsAuthError(wantErr) {
						wantErr = errNoNewerLogin
					}
				}
				got, err := r.refresh(context.Background(), failed)
				if got != want || !errors.Is(err, wantErr) {
					t.Errorf("refresh = %s, %v; want %s, %v", got.AccessToken, err, want.AccessToken, wantErr)
				}
				if f.stored != stored || len(f.saved) != 0 {
					t.Error("the concurrently replaced login was overwritten")
				}
				if f.loads != 2 || !slices.Equal(f.checked, wantChecked) || !slices.Equal(f.remintedFor, []string{failed.AccessToken}) {
					t.Errorf("loads = %d, checked %q, reminted for %q; want a single reconsideration", f.loads, f.checked, f.remintedFor)
				}
			})
		}
	}
}

// TestLoginRefresher_ACancelDuringTheReplacementCheckIsReturned: when the
// caller gives up (Ctrl-C) while C is checked, refresh reports that, not
// the desktop app's login.
func TestLoginRefresher_ACancelDuringTheReplacementCheckIsReturned(t *testing.T) {
	c := testLogin("xoxc-c", "d-c")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, refresher := replacedDuringCheck(t, saveMeanwhile(t, c), c,
		func(context.Context) (string, string, error) {
			cancel()
			return "", "", fmt.Errorf(`Post "https://slack.com/api/auth.test": %w`, context.Canceled)
		})

	got, err := refresher.refresh(ctx, testLogin("xoxc-a", "d-a"))
	if !errors.Is(err, context.Canceled) || got.AccessToken != "xoxc-a" {
		t.Errorf("refresh = %s, %v; want the refused login back and the cancellation", got.AccessToken, err)
	}
	if stored, err := store.Load("T1"); err != nil || stored != c {
		t.Errorf("the token store holds %+v (%v), want the replacement, %+v", stored, err, c)
	}
}

// TestLoginRefresher_DoesNotBringBackAWorkspaceRemovedDuringTheCheck: a
// workspace removed (slk --remove-workspace) while the desktop app's login
// is checked stays removed. The caller still gets the login, which passed.
func TestLoginRefresher_DoesNotBringBackAWorkspaceRemovedDuringTheCheck(t *testing.T) {
	store, refresher := replacedDuringCheck(t, func(dir string) {
		if err := slackclient.NewTokenStore(dir).Delete("T1"); err != nil {
			t.Fatal(err)
		}
	}, slackclient.Token{}, answerAs(""))

	got, err := refresher.refresh(context.Background(), testLogin("xoxc-a", "d-a"))
	if err != nil || got.AccessToken != "xoxc-b" {
		t.Errorf("refresh = %s, %v; want the desktop app's login", got.AccessToken, err)
	}
	if stored, err := store.Load("T1"); err == nil {
		t.Errorf("the token store holds %+v again, want the workspace to stay removed", stored)
	}
}

// TestLoginRefresher_SavesADesktopLoginOnlyAfterItPassed: the desktop app's
// login reaches the token store, where the other slk processes take it,
// only once Slack accepted it. A login that Slack refuses is not saved. One
// that Slack could not be asked about is not saved either, and the desktop
// app is asked again after the next refusal.
func TestLoginRefresher_SavesADesktopLoginOnlyAfterItPassed(t *testing.T) {
	refused := testLogin("xoxc-old", "d-old")
	desktop := testLogin("xoxc-desk", "d-desk")
	ctx := context.Background()

	t.Run("accepted", func(t *testing.T) {
		f := &fakeLogins{stored: refused, desktop: desktop}
		if got, err := f.refresher().refresh(ctx, refused); err != nil || got.AccessToken != "xoxc-desk" {
			t.Fatalf("refresh = %s, %v; want the desktop app's login", got.AccessToken, err)
		}
		if !slices.Equal(f.saved, []string{"xoxc-desk"}) {
			t.Errorf("saved %q, want the desktop app's login", f.saved)
		}
	})
	t.Run("refused", func(t *testing.T) {
		f := &fakeLogins{stored: refused, desktop: desktop, verdict: map[string]error{"xoxc-desk": errInvalidAuth}}
		if _, err := f.refresher().refresh(ctx, refused); !errors.Is(err, errNoNewerLogin) {
			t.Fatalf("refresh err = %v, want errNoNewerLogin", err)
		}
		if len(f.saved) != 0 {
			t.Errorf("saved %q, want nothing", f.saved)
		}
	})
	t.Run("no answer", func(t *testing.T) {
		offline := errors.New("dial tcp: no route to host")
		f := &fakeLogins{stored: refused, desktop: desktop, verdict: map[string]error{"xoxc-desk": offline}}
		r := f.refresher()
		if _, err := r.refresh(ctx, refused); !errors.Is(err, offline) {
			t.Fatalf("refresh err = %v, want the network error", err)
		}
		if len(f.saved) != 0 {
			t.Errorf("saved %q while Slack could not answer, want nothing", f.saved)
		}
		delete(f.verdict, "xoxc-desk") // the network is back
		if got, err := r.refresh(ctx, refused); err != nil || got.AccessToken != "xoxc-desk" {
			t.Fatalf("second refresh = %s, %v; want the desktop app's login", got.AccessToken, err)
		}
		if !slices.Equal(f.remintedFor, []string{"xoxc-old", "xoxc-old"}) || !slices.Equal(f.saved, []string{"xoxc-desk"}) {
			t.Errorf("desktop asked about %q, saved %q; want it asked twice and the login saved once", f.remintedFor, f.saved)
		}
	})
}

// TestCLISignIn_ARefreshThatStopsKeepsItsCause drives a command through
// cli.Main with the sign-in that newCLIEnv builds (connectWithRemint on a
// loginRefresher), on fakes. Slack refuses the stored login A; checking
// the store's B then fails for a reason of its own. The command must
// report that reason, not tell the user to sign in again (auth_failed,
// exit 3). The last case, where Slack refuses B too, is that refusal.
func TestCLISignIn_ARefreshThatStopsKeepsItsCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		check error // what checking B answers
		code  string
		exit  int
	}{
		{"cancelled", fmt.Errorf(`Post "https://slack.com/api/auth.test": %w`, context.Canceled), "cancelled", 1},
		{"no network", errors.New("dial tcp: lookup slack.com: no such host"), "failed", 1},
		{"refused too", errInvalidAuth, "auth_failed", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeLogins{stored: testLogin("xoxc-b", "d-b"), verdict: map[string]error{"xoxc-b": tc.check}}
			signIn := &fakeSignIn{errs: []error{errInvalidAuth}}
			var stdout, stderr bytes.Buffer
			env := cli.Env{
				Stdout: &stdout,
				Stderr: &stderr,
				Getenv: func(string) string { return "" },
				Workspaces: func() ([]cli.Workspace, error) {
					return []cli.Workspace{{TeamID: "T1", Name: "Acme"}}, nil
				},
				Connect: func(ctx context.Context, ws cli.Workspace) (*cli.Session, error) {
					client, err := connectWithRemint(ctx, testLogin("xoxc-a", "d-a"), signIn.connect, f.refresher().refresh)
					if err != nil {
						return nil, err
					}
					return cliSession(ws, client), nil
				},
			}
			exit := cli.Main(context.Background(), []string{"workspace", "whoami", "--json"}, env)
			var doc struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(stderr.Bytes(), &doc); err != nil {
				t.Fatalf("stderr is not a JSON error: %v\n%s", err, stderr.String())
			}
			if exit != tc.exit || doc.Error.Code != tc.code {
				t.Errorf("exit %d, code %q; want %d, %q\n%s", exit, doc.Error.Code, tc.exit, tc.code, stderr.String())
			}
		})
	}
}

// TestOtherAccountNoticeShowsItsInstructionAt80Columns: the notice is a
// toast, shown once for three seconds, in a status bar that does not fit
// a toast to the width its other segments leave. On an 80-column terminal,
// with the segments of a disconnected workspace, the restart instruction
// must still be on screen.
func TestOtherAccountNoticeShowsItsInstructionAt80Columns(t *testing.T) {
	sb := statusbar.New()
	sb.SetMode(ui.ModeNormal)
	sb.SetChannel("general")
	sb.SetWorkspace("Acme Inc")
	sb.SetUnreadCount(12)
	sb.SetStatus("active", false, time.Time{})
	sb.SetConnectionState(statusbar.StateDisconnected)
	sb.SetToast(otherAccountNotice("Acme Inc"))

	// The terminal shows the first 80 columns of a wider line.
	line := ansi.Strip(ansi.Truncate(sb.View(80), 80, ""))
	if !strings.Contains(line, "Restart slk") {
		t.Errorf("the restart instruction is not within 80 columns:\n%q", line)
	}
}

// TestApplyLogin_GivesTheLoginToEveryHolder: after applyLogin, the
// workspace's Slack client and edge client send the new token and d
// cookie, and every holder of file credentials has the new auth.
func TestApplyLogin_GivesTheLoginToEveryHolder(t *testing.T) {
	type seen struct{ token, cookie string }
	var mu sync.Mutex
	got := map[string]seen{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var s seen
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			_ = r.ParseForm()
			s.token = r.PostForm.Get("token")
		} else { // edgeapi: a JSON body
			var body struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.token = body.Token
		}
		if c, err := r.Cookie("d"); err == nil {
			s.cookie = c.Value
		}
		mu.Lock()
		got[r.URL.Path] = s
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"results":[]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv) // signed in as xoxc-test / d-cookie
	wctx := &WorkspaceContext{Client: client, Edge: edge.New("xoxc-test", "T1", client.HTTPClient())}
	old := slackhttp.TeamAuth{TeamID: "T1", Token: "xoxc-test", DCookie: "d-cookie"}
	files := []*slackhttp.AuthResolver{
		slackhttp.NewAuthResolver([]slackhttp.TeamAuth{old}),
		slackhttp.NewAuthResolver([]slackhttp.TeamAuth{old}),
	}

	applyLogin(wctx, testLogin("xoxc-new", "d-new"), files[0], files[1])

	ctx := context.Background()
	if _, _, err := client.CheckLogin(ctx); err != nil {
		t.Fatalf("CheckLogin: %v", err)
	}
	if _, err := wctx.Edge.UsersInfo(ctx, map[string]int64{"U1": 0}); err != nil {
		t.Fatalf("UsersInfo: %v", err)
	}
	want := seen{token: "xoxc-new", cookie: "d-new"}
	for _, path := range []string{"/api/auth.test", "/cache/T1/users/info"} {
		mu.Lock()
		s, ok := got[path]
		mu.Unlock()
		if !ok {
			t.Errorf("no request to %s", path)
		} else if s != want {
			t.Errorf("%s sent %+v, want %+v", path, s, want)
		}
	}
	fresh := slackhttp.TeamAuth{TeamID: "T1", Token: "xoxc-new", DCookie: "d-new"}
	for i, r := range files {
		if auths := r.AuthsForURL("https://files.slack.com/files-pri/T1-F/x.png"); !slices.Equal(auths, []slackhttp.TeamAuth{fresh}) {
			t.Errorf("file credentials %d = %#v, want %#v", i, auths, fresh)
		}
	}
}
