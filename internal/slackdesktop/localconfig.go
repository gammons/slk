package slackdesktop

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// decodeLocalStorageValue strips the 1-byte encoding marker Chromium's Local
// Storage prepends to values: 0x00 => UTF-16LE, 0x01 => UTF-8/Latin-1.
func decodeLocalStorageValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0x00:
		b := v[1:]
		u16 := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u16 = append(u16, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return string(utf16.Decode(u16))
	case 0x01:
		return string(v[1:])
	default:
		return string(v)
	}
}

// parseLocalConfig parses the localConfig_v2 JSON into signed-in workspaces
// (teams with a usable token). Entries missing domain/id/token are skipped.
func parseLocalConfig(data []byte) ([]Workspace, error) {
	var cfg struct {
		Teams map[string]struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Domain string `json:"domain"`
			Token  string `json:"token"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	var out []Workspace
	for _, t := range cfg.Teams {
		if t.Domain == "" || t.ID == "" || t.Token == "" {
			continue
		}
		out = append(out, Workspace{Name: t.Name, Domain: t.Domain, TeamID: t.ID, Token: t.Token})
	}
	if len(out) == 0 {
		return nil, ErrNotSignedIn
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Workspaces reads the desktop app's signed-in teams (with tokens) from the
// localConfig_v2 value in its Local Storage leveldb.
func Workspaces() ([]Workspace, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	value, err := readLocalConfigValue(dir)
	if err != nil {
		return nil, err
	}
	return parseLocalConfig([]byte(decodeLocalStorageValue(value)))
}

// readLocalConfigValue returns the raw localConfig_v2 value bytes (including
// the 1-byte encoding marker) from the Local Storage leveldb. Slack keeps the
// DB open, so we read a temp copy.
func readLocalConfigValue(configDir string) ([]byte, error) {
	src := filepath.Join(configDir, "Local Storage", "leveldb")
	if _, err := os.Stat(src); err != nil {
		return nil, ErrNotSignedIn
	}
	tmp, err := os.MkdirTemp("", "slk-lvl-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := copyLevelDB(src, tmp); err != nil {
		return nil, err
	}

	db, err := leveldb.OpenFile(tmp, &opt.Options{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var value []byte
	iter := db.NewIterator(&util.Range{}, nil)
	for iter.Next() {
		k := string(iter.Key())
		if strings.HasSuffix(k, "localConfig_v2") && strings.Contains(k, "app.slack.com") {
			value = append([]byte(nil), iter.Value()...) // last write wins
		}
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, ErrNotSignedIn
	}
	return value, nil
}

// copyLevelDB copies the leveldb files (skipping the LOCK) into dst.
func copyLevelDB(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "LOCK" {
			continue
		}
		in, err := os.Open(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}
		out, err := os.Create(filepath.Join(dst, e.Name()))
		if err != nil {
			in.Close()
			return err
		}
		_, cerr := io.Copy(out, in)
		in.Close()
		out.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}
