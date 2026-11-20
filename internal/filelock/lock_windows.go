package filelock

import (
	"os"

	"golang.org/x/sys/windows"
)

// TryExclusive acquires a non-blocking lock until the file is closed.
func TryExclusive(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
}
