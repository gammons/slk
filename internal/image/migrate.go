package image

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MigrateAvatars moves files from oldDir/<userID>.img to
// newDir/avatar-<userID>.<ext> exactly once. The extension is sniffed from
// the file's first bytes (PNG or JPEG); unknown content is renamed as .png.
//
// Returns the number of files moved. If oldDir does not exist, returns
// (0, nil).
func MigrateAvatars(oldDir, newDir string) (int, error) {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.MkdirAll(newDir, 0700); err != nil {
		return 0, err
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".img") {
			continue
		}
		userID := strings.TrimSuffix(e.Name(), ".img")
		oldPath := filepath.Join(oldDir, e.Name())
		ext := sniffExt(oldPath)
		newPath := filepath.Join(newDir, "avatar-"+userID+"."+ext)
		if _, err := os.Stat(newPath); err == nil {
			os.Remove(oldPath)
			continue
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			if err := copyFile(oldPath, newPath); err != nil {
				return moved, err
			}
			os.Remove(oldPath)
		}
		moved++
	}
	return moved, nil
}

func sniffExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "png"
	}
	defer f.Close()
	var hdr [8]byte
	n, _ := f.Read(hdr[:])
	if n >= 4 && hdr[0] == 0xFF && hdr[1] == 0xD8 {
		return "jpg"
	}
	if n >= 8 && hdr[0] == 0x89 && hdr[1] == 'P' && hdr[2] == 'N' && hdr[3] == 'G' {
		return "png"
	}
	return "png"
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
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
