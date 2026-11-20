//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package filelock

import (
	"os"

	"golang.org/x/sys/unix"
)

// TryExclusive acquires a non-blocking lock until the file is closed.
func TryExclusive(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
