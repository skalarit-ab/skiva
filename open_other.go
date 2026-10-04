//go:build !windows

package main

import "os"

// openShared opens a file to read; elsewhere than on Windows, an open
// file may be deleted or renamed already.
func openShared(path string) (*os.File, error) {
	return os.Open(path)
}
