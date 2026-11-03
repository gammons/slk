//go:build unix

package slackclient

import (
	"errors"
	"os"
	"syscall"
)

func openAuthFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, ErrUnsafeAuthFile
	}
	return f, err
}

func importOwner(info os.FileInfo, allowRoot bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (stat.Uid == uint32(os.Geteuid()) || (allowRoot && stat.Uid == 0))
}
