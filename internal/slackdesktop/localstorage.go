package slackdesktop

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// localConfigKey is the localStorage key under which Slack's web/desktop client
// (client-v2) persists per-workspace session tokens. Modern Slack no longer
// embeds the xoxc token in page HTML, so this store — not page scraping — is
// the source of truth. See GitHub issue #5.
const localConfigKey = "localConfig_v2"

// localConfig is the shape of the localConfig_v2 JSON we care about: a map of
// team ID to that team's xoxc token. Other fields are ignored.
type localConfig struct {
	Teams map[string]struct {
		Token string `json:"token"`
	} `json:"teams"`
}

// Tokens reads the desktop app's Local Storage LevelDB and returns a map of
// team ID to xoxc token. Returns ErrNotSignedIn if no localConfig_v2 entry with
// any team token is found.
//
// The LevelDB is copied to a temp dir first (like the Cookies DB) so the live
// profile isn't locked or mutated while Slack is running.
func Tokens() (map[string]string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	ldbPath := filepath.Join(dir, "Local Storage", "leveldb")
	if info, err := os.Stat(ldbPath); err != nil || !info.IsDir() {
		return nil, ErrNotSignedIn
	}

	tmp, err := copyDirToTemp(ldbPath)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	return tokensFromLevelDB(tmp)
}

// tokensFromLevelDB opens a LevelDB directory read-only and extracts team
// tokens from every localConfig_v2 entry it contains (Chromium stores one per
// origin, e.g. https://app.slack.com). Tokens from all entries are merged;
// later non-empty values win.
func tokensFromLevelDB(dbDir string) (map[string]string, error) {
	db, err := leveldb.OpenFile(dbDir, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	out := map[string]string{}
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		// Chromium keys are "_<origin>\x00<key>" with a leading encoding byte
		// on the key part; match on the suffix rather than reconstructing the
		// exact origin, which we don't know (app.slack.com vs the workspace or
		// enterprise host).
		if !strings.HasSuffix(string(iter.Key()), localConfigKey) {
			continue
		}
		var cfg localConfig
		if err := json.Unmarshal([]byte(decodeLSValue(iter.Value())), &cfg); err != nil {
			continue
		}
		for teamID, t := range cfg.Teams {
			if t.Token != "" {
				out[teamID] = t.Token
			}
		}
	}
	if err := iter.Error(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotSignedIn
	}
	return out, nil
}

// decodeLSValue decodes a Chromium localStorage value. Chromium prefixes the
// stored bytes with a one-byte encoding tag: 0x00 → UTF-16LE, 0x01 → Latin-1
// (used for all-ASCII values like our JSON). Unknown/absent tag: return as-is.
func decodeLSValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		return decodeUTF16LE(v[1:])
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

func decodeUTF16LE(b []byte) string {
	u16 := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u16))
}

// copyDirToTemp recursively copies a directory's regular files into a fresh
// temp dir and returns its path. LevelDB directories are shallow (a handful of
// files), so a non-recursive-into-subdirs copy of regular files suffices; the
// caller removes the temp dir.
func copyDirToTemp(src string) (string, error) {
	dst, err := os.MkdirTemp("", "slk-ls-*")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		os.RemoveAll(dst)
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			os.RemoveAll(dst)
			return "", err
		}
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
