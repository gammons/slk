package slackclient

import "os"

func openAuthFile(path string) (*os.File, error) {
	return os.Open(path)
}

func importOwner(os.FileInfo, bool) bool {
	return true
}
