// This file is the one way an slk process gets a new login after Slack
// refused its own: the TUI while it runs, and the scriptable CLI. Every
// process shares one login per workspace, the token store's. The first
// process to refresh a login saves it there, and the others take it from
// there instead of refreshing it again.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slackhttp"
)

// loginCheckTimeout bounds the auth.test that checks a candidate login, so
// a hung request cannot stop the TUI's reconnect loop or a command.
const loginCheckTimeout = 15 * time.Second

var (
	// errNoNewerLogin: neither the token store nor the desktop app has a
	// login that Slack accepts and has not refused before.
	errNoNewerLogin = errors.New("no newer login that Slack accepts")
	// errOtherAccount: Slack accepts the login, but as another user or
	// team than the running workspace is signed in as.
	errOtherAccount = errors.New("the login belongs to another account")
)

// loginKey identifies a login: its token and its d cookie.
type loginKey struct{ token, cookie string }

func keyOf(tok slackclient.Token) loginKey {
	return loginKey{tok.AccessToken, tok.Cookie}
}

// loginRefresher gets a new login after Slack refused one. It is the path
// every slk process takes for that:
//
//  1. The token store. Another slk process (a TUI launch, a CLI command,
//     another TUI's reconnect) can have saved a newer login there; taking
//     it needs no keychain read.
//  2. The Slack desktop app (desktopLogin), only for desktop-backed logins.
//     It is asked at most once per refused login that Slack answered about:
//     reading it can show a keychain prompt, and a login that Slack keeps
//     refusing must not ask again on every reconnect.
//
// Every candidate passes check (auth.test on a client of its own) before
// refresh returns it, so a caller never installs or sends a login that
// Slack refuses, or that check rejects as another account's. The desktop
// app's login is saved to the store, for the other processes, only after
// it passed, and only over the entry refresh read (SaveIf): a login the
// caller rejects never reaches the store, and a change that another
// process made meanwhile (the workspace added again, or removed) is
// kept. A refused login is remembered and never chosen again, even when
// a stale copy of it comes back into the store.
//
// A loginRefresher is not safe for concurrent use. The TUI keeps one per
// workspace, used only by that workspace's reconnect loop; a CLI command
// makes its own.
type loginRefresher struct {
	load func(teamID string) (slackclient.Token, error)
	// remint reads the desktop app's login. It must not save it; saveIf
	// does, after check.
	remint func(ctx context.Context, tok slackclient.Token) slackclient.Token
	// saveIf saves a login only over the entry the store held when it was
	// read (TokenStore.SaveIf), and reports whether it did.
	saveIf func(tok, expected slackclient.Token) (bool, error)
	check  func(ctx context.Context, tok slackclient.Token) error
	// refused holds every login that Slack refused.
	refused map[loginKey]bool
	// asked holds the refused logins the desktop app was asked about.
	asked map[loginKey]bool
}

// newLoginRefresher returns a loginRefresher on store and the desktop app
// whose candidates must pass check (acceptedLogin or sameAccountAs).
func newLoginRefresher(store *slackclient.TokenStore, check func(context.Context, slackclient.Token) error) *loginRefresher {
	return newLoginRefresherWith(store.Load, desktopLogin, store.SaveIf, check)
}

// newLoginRefresherWith is newLoginRefresher with the token store and the
// desktop app injected, for tests.
func newLoginRefresherWith(
	load func(teamID string) (slackclient.Token, error),
	remint func(ctx context.Context, tok slackclient.Token) slackclient.Token,
	saveIf func(tok, expected slackclient.Token) (bool, error),
	check func(ctx context.Context, tok slackclient.Token) error,
) *loginRefresher {
	return &loginRefresher{
		load:    load,
		remint:  remint,
		saveIf:  saveIf,
		check:   check,
		refused: map[loginKey]bool{},
		asked:   map[loginKey]bool{},
	}
}

// refresh records that Slack refused failed and returns a newer login that
// passed check. When it finds none, it returns failed and the reason:
// errNoNewerLogin, or the error check gave for a candidate that was not a
// refusal (no network, cancelled; errOtherAccount). Such an error ends the
// search, so the desktop app is not asked after a candidate of another
// account; a later refresh searches again.
func (r *loginRefresher) refresh(ctx context.Context, failed slackclient.Token) (slackclient.Token, error) {
	r.refused[keyOf(failed)] = true
	stored, loadErr := r.load(failed.TeamID)
	if loadErr == nil && !r.refused[keyOf(stored)] {
		accepted, err := r.try(ctx, stored)
		if err != nil {
			return failed, err
		}
		if accepted {
			return stored, nil
		}
	}
	if r.asked[keyOf(failed)] || (loadErr == nil && !stored.UsesDesktopLogin()) {
		return failed, errNoNewerLogin
	}
	// remint gives failed back when the desktop app has nothing newer, and
	// for a protected sign-in, which it never touches.
	fresh := r.remint(ctx, failed)
	if r.refused[keyOf(fresh)] {
		r.asked[keyOf(failed)] = true
		return failed, errNoNewerLogin
	}
	accepted, err := r.try(ctx, fresh)
	if err != nil && !errors.Is(err, errOtherAccount) {
		// Slack gave no answer (no network, cancelled), and the login is
		// not saved: the desktop app is asked again after the next
		// refusal.
		return failed, err
	}
	r.asked[keyOf(failed)] = true
	if err != nil {
		return failed, err
	}
	if !accepted {
		return failed, errNoNewerLogin
	}
	if loadErr != nil {
		// No entry was read, so there is none to replace: the workspace
		// can have been removed. fresh is used, and not saved.
		return fresh, nil
	}
	saved, err := r.saveIf(fresh, stored)
	if err != nil {
		log.Printf("workspace %s: saving the refreshed login: %v", fresh.TeamName, err)
	}
	if saved || err != nil {
		return fresh, nil
	}
	return r.reconsider(ctx, failed, fresh)
}

// reconsider runs when another slk process changed the store's entry
// while refresh checked the desktop app's login (fresh), so that SaveIf
// kept the change. The workspace can have been added again, as another
// user too, and that choice is kept: the new entry goes through check
// like any stored login.
//
//   - It passes: it is the answer.
//   - It is another account's: the search ends with errOtherAccount,
//     which the TUI reports.
//   - The caller gave up during its check (cancelled, deadline): that
//     cause is returned, not a login.
//   - Anything else (Slack refuses it, or could not be asked about it):
//     only a desktop-backed entry lets the caller keep fresh, which passed.
//     A protected entry ends the search with that error or errNoNewerLogin.
//
// The store keeps the new entry in every case.
func (r *loginRefresher) reconsider(ctx context.Context, failed, fresh slackclient.Token) (slackclient.Token, error) {
	now, err := r.load(failed.TeamID)
	if err != nil || keyOf(now) == keyOf(fresh) {
		return fresh, nil
	}
	if r.refused[keyOf(now)] {
		if !now.UsesDesktopLogin() {
			return failed, errNoNewerLogin
		}
		return fresh, nil
	}
	accepted, err := r.try(ctx, now)
	switch {
	case accepted:
		return now, nil
	case errors.Is(err, errOtherAccount):
		return failed, err
	case err != nil && (!now.UsesDesktopLogin() || ctx.Err() != nil):
		return failed, err
	case !now.UsesDesktopLogin():
		return failed, errNoNewerLogin
	}
	return fresh, nil
}

// try checks a candidate login. accepted is true when it passed check. A
// candidate that Slack refuses is recorded as refused, with no error; any
// other error is returned.
func (r *loginRefresher) try(ctx context.Context, cand slackclient.Token) (accepted bool, err error) {
	err = r.check(ctx, cand)
	if slackclient.IsAuthError(err) {
		r.refused[keyOf(cand)] = true
		return false, nil
	}
	return err == nil, err
}

// identifyFunc signs in with a login on a client of its own and returns the
// team and the user that Slack accepts it as.
type identifyFunc func(ctx context.Context, tok slackclient.Token) (teamID, userID string, err error)

// identifyLogin is the identifyFunc for Slack itself: auth.test on a client
// built only for the check, so that no running client sends a login before
// the login passed.
func identifyLogin(ctx context.Context, tok slackclient.Token) (teamID, userID string, err error) {
	ctx, cancel := context.WithTimeout(ctx, loginCheckTimeout)
	defer cancel()
	return slackclient.NewClient(tok.AccessToken, tok.Cookie).CheckLogin(ctx)
}

// acceptedLogin is the CLI's check: Slack accepts the login, as whichever
// account it belongs to. A command builds everything it shows from the
// account it signs in as, so it has no earlier account to keep.
func acceptedLogin(ctx context.Context, tok slackclient.Token) error {
	_, _, err := identifyLogin(ctx, tok)
	return err
}

// sameAccountAs returns the TUI's check for a workspace that is signed in
// as user userID of team teamID: Slack must accept the login as that same
// account. The user, the conversations and everything else the TUI built
// belong to it, so the login of any other account is errOtherAccount.
func sameAccountAs(teamID, userID string, identify identifyFunc) func(context.Context, slackclient.Token) error {
	return func(ctx context.Context, tok slackclient.Token) error {
		team, user, err := identify(ctx, tok)
		if err != nil {
			return err
		}
		if team != teamID || user != userID {
			return fmt.Errorf("%w: Slack accepts it as user %s of team %s, and the workspace is signed in as user %s of team %s",
				errOtherAccount, user, team, userID, teamID)
		}
		return nil
	}
}

// workspaceRelogin returns the ConnectionManager.Relogin of one workspace
// in the TUI. tok is the login the workspace connected with. After Slack
// refuses the login in use, it takes a newer login from refresher, whose
// check is sameAccountAs, and passes it to apply, which gives it to every
// copy the TUI holds (applyLogin).
//
// A login of another account is never installed: only a restart builds
// the workspace again for that account. The first time refresher finds
// one, the returned function tells the user (notify) and gives up for the
// rest of the session. It is called only from the workspace's reconnect
// loop, one call at a time.
func workspaceRelogin(tok slackclient.Token, refresher *loginRefresher, apply func(slackclient.Token), notify func(string)) func(context.Context) bool {
	current := tok
	otherAccount := false
	return func(ctx context.Context) bool {
		if otherAccount {
			return false
		}
		fresh, err := refresher.refresh(ctx, current)
		switch {
		case err == nil:
			current = fresh
			apply(fresh)
			log.Printf("workspace %s: Slack refused the login; reconnecting with a refreshed one", tok.TeamName)
			return true
		case errors.Is(err, errOtherAccount):
			otherAccount = true
			log.Printf("workspace %s: Slack refused the login, and the newer one is not usable: %v", tok.TeamName, err)
			notify(otherAccountNotice(tok.TeamName))
		default:
			log.Printf("workspace %s: Slack refused the login, and no newer one was found: %v", tok.TeamName, err)
		}
		return false
	}
}

// otherAccountNotice is the toast that tells the user what to do when the
// only newer login of a workspace belongs to another user. The status bar
// does not fit a toast to the width left over by its other segments, and a
// narrow terminal cuts the end of the line, so the instruction comes first
// and the text is short (TestOtherAccountNoticeShowsItsInstructionAt80Columns).
func otherAccountNotice(teamName string) string {
	return fmt.Sprintf("Restart slk: %s login is for another user", teamName)
}

// teamAuthSetter holds the credentials for a workspace's files: the image
// fetcher, and the file downloader's resolver.
type teamAuthSetter interface {
	SetTeamAuth(slackhttp.TeamAuth)
}

// applyLogin gives a workspace's refreshed login to every copy of it that
// the running TUI holds: the workspace's Slack client (which also changes
// the d cookie in the jar its edge client shares), its edge client, and
// files, the credentials for its files. Bootstrap's edge client needs
// nothing: it lives only while the workspace connects. A new holder of a
// workspace's login must be added here, or it keeps sending the refused
// login.
func applyLogin(wctx *WorkspaceContext, tok slackclient.Token, files ...teamAuthSetter) {
	wctx.Client.SetLogin(tok.AccessToken, tok.Cookie)
	if wctx.Edge != nil {
		wctx.Edge.SetToken(tok.AccessToken)
	}
	auth := slackhttp.TeamAuth{TeamID: tok.TeamID, Token: tok.AccessToken, DCookie: tok.Cookie}
	for _, f := range files {
		f.SetTeamAuth(auth)
	}
}
