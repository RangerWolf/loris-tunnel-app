//go:build !windows

package conf

import "os"

func protectPrivate(path string) error                { return os.Chmod(path, 0600) }
func replacePrivate(source, destination string) error { return os.Rename(source, destination) }
