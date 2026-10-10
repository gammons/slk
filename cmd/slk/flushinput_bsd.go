//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import "golang.org/x/sys/unix"

const (
	// fread is FREAD from <sys/fcntl.h>: TIOCFLUSH with it flushes input only.
	fread = 1
	// fionread is FIONREAD from <sys/filio.h>, _IOR('f', 127, int), the same
	// on all of these; x/sys/unix does not define it for them.
	fionread = 0x4004667f
)

// flushInput discards input the terminal has received but nobody has read.
func flushInput(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fread)
}

// inputPending returns how many bytes of input wait to be read.
func inputPending(fd int) (int, error) {
	return unix.IoctlGetInt(fd, fionread)
}
