package emoji

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CacheVersion is the on-disk schema version. Bump when format changes.
const CacheVersion = 1

// Cache is the on-disk format for probed emoji widths.
type Cache struct {
	Version     int            `json:"version"`
	Terminal    string         `json:"terminal"`
	ProbedAt    string         `json:"probed_at"`
	CodemapHash string         `json:"codemap_hash"`
	Widths      map[string]int `json:"widths"`
}

// sanitizeFilenameKey replaces any character not in [A-Za-z0-9._-] with '_'.
// This ensures that terminal identity strings (which can come from
// environment variables) cannot inject path separators or other unfriendly
// characters into a cache filename.
func sanitizeFilenameKey(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}

// CachePath returns the absolute path to the cache file for the given
// terminal key. Honors XDG_CACHE_HOME, falls back to ~/.cache. The
// terminal key is sanitized to remove characters that would be illegal
// or surprising in a filename.
func CachePath(terminalKey string) string {
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home := os.Getenv("HOME")
		cacheHome = filepath.Join(home, ".cache")
	}
	safeKey := sanitizeFilenameKey(terminalKey)
	return filepath.Join(cacheHome, "slk", "emoji-widths-"+safeKey+".json")
}

// SaveCache writes the cache as JSON, creating directories as needed.
func SaveCache(path string, c *Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadCache reads a cache file. Returns os.IsNotExist error if absent.
func LoadCache(path string) (*Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// codemapHash produces a stable hex hash of a name→unicode emoji map.
// The hash is order-independent so it's stable across Go map iteration.
func codemapHash(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(m[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
