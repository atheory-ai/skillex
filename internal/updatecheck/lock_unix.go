//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package updatecheck

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
