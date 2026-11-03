package slackclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

var ErrUnsafeAuthFile = errors.New("unsafe authentication storage; use trusted directories and owner-only file permissions")

var errReadAuthFile = errors.New("cannot read authentication file")
var errAuthStorageIO = errors.New("cannot access authentication storage")

func ReadAuthFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errReadAuthFile
	}
	if !safeImportFile(info) {
		return nil, ErrUnsafeAuthFile
	}
	return readAuthFile(path, info)
}

func readAuthFile(path string, info os.FileInfo) ([]byte, error) {
	f, err := openAuthFile(path)
	if err != nil {
		if errors.Is(err, ErrUnsafeAuthFile) {
			return nil, ErrUnsafeAuthFile
		}
		return nil, errReadAuthFile
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, errReadAuthFile
	}
	if !os.SameFile(info, opened) || !safeImportFile(opened) {
		return nil, ErrUnsafeAuthFile
	}
	data, err := io.ReadAll(io.LimitReader(f, AuthBundleLimit+1))
	if err != nil {
		return nil, errReadAuthFile
	}
	if len(data) > AuthBundleLimit {
		return nil, ErrInvalidAuthBundle
	}
	return data, nil
}

func (s *TokenStore) SnapshotForImport(teamID string) (*Token, error) {
	if !validAuthTeamID(teamID) {
		return nil, ErrInvalidAuthBundle
	}
	if err := ValidateAuthDirectory(s.dir); err != nil {
		return nil, err
	}
	if _, err := importFileInfo(filepath.Join(s.dir, tokenLockFile)); err != nil {
		return nil, err
	}
	path := filepath.Join(s.dir, teamID+".json")
	info, err := importFileInfo(path)
	if err != nil || info == nil {
		return nil, err
	}
	data, err := readAuthFile(path, info)
	if err != nil {
		return nil, err
	}
	var token Token
	if json.Unmarshal(data, &token) != nil || token.TeamID != teamID || !validAuthToken(token) {
		return nil, ErrInvalidAuthBundle
	}
	return &token, nil
}

func (s *TokenStore) ImportIf(ctx context.Context, candidate Token, expected *Token) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !validAuthToken(candidate) || (expected != nil && expected.TeamID != candidate.TeamID) {
		return false, ErrInvalidAuthBundle
	}
	if err := ValidateAuthDirectory(s.dir); err != nil {
		return false, err
	}
	if _, err := importFileInfo(filepath.Join(s.dir, tokenLockFile)); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	saved := false
	err := s.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := s.SnapshotForImport(candidate.TeamID)
		if err != nil {
			return err
		}
		if (current == nil) != (expected == nil) || (current != nil && *current != *expected) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.write(candidate); err != nil {
			return err
		}
		saved = true
		return nil
	})
	switch err {
	case nil, context.Canceled, context.DeadlineExceeded, ErrInvalidAuthBundle, ErrUnsafeAuthFile, errReadAuthFile:
		return saved, err
	default:
		return false, errAuthStorageIO
	}
}

func ValidateAuthDirectory(dir string) error {
	path, err := filepath.Abs(dir)
	if err != nil {
		return errAuthStorageIO
	}
	var child os.FileInfo
	for target := true; ; target = false {
		info, err := os.Lstat(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errAuthStorageIO
		}
		if err == nil {
			if !target && runtime.GOOS == "darwin" && (path == "/var" || path == "/tmp") && info.Mode()&os.ModeSymlink != 0 {
				if !importOwner(info, true) || (os.Geteuid() != 0 && importOwner(info, false)) {
					return ErrUnsafeAuthFile
				}
				link, err := os.Readlink(path)
				if err != nil {
					return errAuthStorageIO
				}
				if !filepath.IsAbs(link) {
					link = filepath.Join(filepath.Dir(path), link)
				}
				if link != filepath.Join("/private", filepath.Base(path)) {
					return ErrUnsafeAuthFile
				}
				path = link
				continue
			}
			if !info.IsDir() || !importOwner(info, !target) {
				return ErrUnsafeAuthFile
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
				if target || info.Mode()&os.ModeSticky == 0 || child == nil || !importOwner(child, true) {
					return ErrUnsafeAuthFile
				}
			}
		}
		child = info
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func importFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errAuthStorageIO
	}
	if !safeImportFile(info) {
		return nil, ErrUnsafeAuthFile
	}
	return info, nil
}

func safeImportFile(info os.FileInfo) bool {
	return info.Mode().IsRegular() && importOwner(info, false) && (runtime.GOOS == "windows" || info.Mode().Perm()&0o077 == 0)
}
