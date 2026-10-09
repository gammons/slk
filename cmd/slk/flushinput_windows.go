//go:build windows

package main

import "golang.org/x/sys/windows"

// flushInput discards input the console has received but nobody has read.
func flushInput(fd int) error {
	return windows.FlushConsoleInputBuffer(windows.Handle(fd))
}

// inputPending returns how many console input events wait to be read.
func inputPending(fd int) (int, error) {
	var n uint32
	err := windows.GetNumberOfConsoleInputEvents(windows.Handle(fd), &n)
	return int(n), err
}
