// Package slackclient wraps the slack-go library with token management,
// Socket Mode event handling, and a simplified Web API interface.
package slackclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gammons/slk/internal/atomicfile"
	"github.com/slack-go/slack"
)

// Token holds browser session credentials for a single Slack workspace.
type Token struct {
	AccessToken string `json:"access_token"` // xoxc-... token
	Cookie      string `json:"cookie"`       // d cookie value
	Domain      string `json:"domain"`       // workspace subdomain (to re-mint)
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	// Source is TokenSourceBrowser for a token signed in from a browser
	// session, empty for one read from the Slack desktop app.
	Source string `json:"source,omitempty"`
}

// TokenSourceBrowser marks a token the user signed in from a browser session
// (slk --add-workspace --browser). Startup re-minting leaves it alone: the
// desktop app may be signed in to the same workspace as someone else.
const TokenSourceBrowser = "browser"

// authErrorCodes are the Slack error strings that refuse the login
// itself rather than one request.
var authErrorCodes = map[string]bool{
	"invalid_auth":     true,
	"not_authed":       true,
	"token_expired":    true,
	"token_revoked":    true,
	"account_inactive": true,
}

// IsAuthError reports whether err is Slack refusing the session's
// login (invalid_auth, not_authed, token_expired, token_revoked or
// account_inactive), as opposed to refusing one request. A re-minted
// token can cure the first four; account_inactive means the account
// was deactivated, which nothing slk does can cure.
func IsAuthError(err error) bool {
	var se slack.SlackErrorResponse
	return errors.As(err, &se) && authErrorCodes[se.Err]
}

// TokenStore persists Slack tokens as JSON files in a directory,
// one file per workspace ({teamID}.json).
type TokenStore struct {
	dir string
}

// NewTokenStore creates a TokenStore that reads/writes to the given directory.
func NewTokenStore(dir string) *TokenStore {
	return &TokenStore{dir: dir}
}

// Save writes a token to disk, creating the directory if needed.
// A new file is owner-only (0600).
//
// The write is atomic (atomicfile.Write): another slk process that
// lists the store meanwhile (a TUI starting while a CLI command saves
// a refreshed login) sees the old token or the new one, never a
// half-written file, which List would skip as corrupt. It also holds
// the store's lock (lockTokenDir), so it never lands between the
// check and the write of another process's SaveIf.
func (s *TokenStore) Save(token Token) error {
	return s.withLock(func() error { return s.write(token) })
}

// SaveIf saves token only if the store still holds expected for
// token.TeamID: the entry the caller read before it decided to save. It
// reports whether it saved. A slk process that refreshes a login while
// it runs saves with it, so that a change another process made
// meanwhile (the workspace added again, perhaps as another user) is
// kept, not overwritten. The read, the comparison and the write run
// under the store's lock, which Save and Delete take too, so no other
// slk process can change the entry between them. An entry that is gone,
// or that cannot be read, never matches: a workspace removed meanwhile
// stays removed.
func (s *TokenStore) SaveIf(token, expected Token) (bool, error) {
	saved := false
	err := s.withLock(func() error {
		current, err := s.Load(token.TeamID)
		if err != nil || current != expected {
			return nil
		}
		if err := s.write(token); err != nil {
			return err
		}
		saved = true
		return nil
	})
	return saved, err
}

// write is Save without the lock.
func (s *TokenStore) write(token Token) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}

	path := filepath.Join(s.dir, token.TeamID+".json")
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return fmt.Errorf("writing token: %w", err)
	}
	return nil
}

// withLock runs fn while it holds the store's lock. It creates the
// directory first: the lock file lives in it.
func (s *TokenStore) withLock(fn func() error) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("creating token dir: %w", err)
	}
	unlock, err := lockTokenDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}

// Load reads a token for the given team ID from disk.
// Returns an error if the token file does not exist.
func (s *TokenStore) Load(teamID string) (Token, error) {
	path := filepath.Join(s.dir, teamID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Token{}, fmt.Errorf("token not found for team %s", teamID)
		}
		return Token{}, fmt.Errorf("reading token: %w", err)
	}

	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return Token{}, fmt.Errorf("unmarshaling token: %w", err)
	}
	return token, nil
}

// List returns all tokens stored on disk.
// Corrupted token files are silently skipped.
func (s *TokenStore) List() ([]Token, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing tokens: %w", err)
	}

	var tokens []Token
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		teamID := strings.TrimSuffix(entry.Name(), ".json")
		token, err := s.Load(teamID)
		if err != nil {
			continue // skip corrupted tokens
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// Delete removes a token file for the given team ID.
// Returns nil if the file does not exist. It holds the store's lock,
// so a SaveIf in another process cannot bring the file back.
func (s *TokenStore) Delete(teamID string) error {
	return s.withLock(func() error {
		path := filepath.Join(s.dir, teamID+".json")
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deleting token: %w", err)
		}
		return nil
	})
}

// tokenLockFile is the file in the token directory whose lock orders
// every change to the store across slk processes. List reads only
// .json files, so it never sees it.
const tokenLockFile = ".lock"

// lockTokenDir takes the token store's lock, waiting while another slk
// process holds it, and returns the function that releases it. The lock
// is the OS's (lockFile): the OS releases it when its process ends, so
// a crash cannot leave the store locked.
func lockTokenDir(dir string) (unlock func(), err error) {
	f, err := os.OpenFile(filepath.Join(dir, tokenLockFile), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening the token store's lock: %w", err)
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("locking the token store: %w", err)
	}
	return func() {
		_ = unlockFile(f)
		f.Close()
	}, nil
}
